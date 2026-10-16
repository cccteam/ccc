// Package members is the members auth: the people who sign in through the organization's
// directory (OpenID Connect against Google) and hold their roles in the members permission
// store.
//
// The directory proves who someone is and says what they may do. Role membership is the
// directory's (google.RoleSync): every sign-in reconciles the person's roles to the
// Google Groups the directory places them in and removes any it does not name, so nothing
// in the application assigns roles in this store and the bootstrap seeds none. A global
// role is held in the global partition and a domain role in every sector, so one
// membership reaches every sector, the ones seeded today and the ones created later. The
// application-run alternative, google.DisableRoleSync, leaves role membership to the
// application, so one registration is never both.
//
// An auth is a package. It owns its session manager, one Auth session with the sign-in
// methods registered on it, here the directory alone, with its own session, user,
// identity, auth event and pending sign-in tables and cookie; its permission store, with
// its own table prefix; and its role configuration. A site or an outlet binds to an auth
// by composing its handlers, and two auths on one database and one host never collide,
// because everything an auth names carries its name. The same name in this auth and in
// another is two unrelated principals.
//
// Demonstrates: auth.directory-roles, auth.two-populations, auth.skipauth-directory, auth.login-refusal-code.
package members

import (
	"context"
	_ "embed" // the role file rides in the binary
	"net/http"
	"time"

	cloudspanner "cloud.google.com/go/spanner"
	"github.com/cccteam/access"
	"github.com/cccteam/access/spannerstore"
	"github.com/cccteam/ccc/resource/lodestar/pkg/auth"
	"github.com/cccteam/session"
	"github.com/cccteam/session/google"
	"github.com/cccteam/session/sessionstorage"
	"github.com/cccteam/session/signin"
	"github.com/go-playground/errors/v5"
)

const (
	// Name is the auth's name: the cookie its sessions ride in.
	Name = "members"
	// TablePrefix is the name's PascalCase form, which prefixes every table the auth owns.
	TablePrefix = "Members"

	// XSRFCookie is the cookie the auth issues its XSRF token in. It carries the auth's name
	// so two auths on one host never overwrite each other's token; the browser echoes it in
	// the X-XSRF-TOKEN header, so the web app that binds to this auth names the same cookie.
	XSRFCookie = Name + "-xsrf"

	// DirectoryRegistration is the name the directory is registered under on the Auth:
	// the segment of its login and callback routes (user/directory/login). In
	// development the simulated person is the shared APP_USERNAME, since the
	// registration sets no pair of its own.
	DirectoryRegistration = "directory"

	// The auth's tables, which schema/migrations creates: the sessions and users of the
	// Auth session (a user keyed by the directory's immutable subject through the
	// identities table, so a renamed account stays the same person), the auth events
	// each session records and the pending sign-ins that wait for a confirmation or an
	// MFA step. The names are spelled out in full so the tool's session-tables check,
	// which reads the literals, finds their migrations.
	sessionsTable   = "MembersSessions"
	usersTable      = "MembersSessionUsers"
	identitiesTable = "MembersSessionIdentities"
	eventsTable     = "MembersSessionAuthEvents"
	pendingTable    = "MembersSessionPendingSignIns"
)

// rolesFile is the auth's default roles: what a client may do, in the lowercase names the
// directory's groups carry. The file travels with the binary and is handed to the
// permission engine at New; the store holds no row for these roles, and who holds them is
// the directory's business.
//
//go:embed roles.json
var rolesFile []byte

// Roles returns the auth's role file.
func Roles() access.RoleFile {
	return access.RoleFile(rolesFile)
}

// Settings are the auth's environment-derived settings.
type Settings struct {
	// CookieKey signs session cookies: a Base64-encoded string of at least 32 bytes of
	// cryptographically secure random data.
	CookieKey string
	// SessionTimeout is the idle timeout of a browser session.
	SessionTimeout time.Duration
	// LoginURL is the browser page a refused login returns to, with the reason as a code
	// in the query (?code=, a sessioninfo.LoginRefusalCode, never text): the login page of
	// the surface that binds to this auth, which holds the sentence for each code.
	LoginURL string
	// Directory identifies the application to the directory that verifies its logins.
	Directory auth.Directory
	// Collection is the generated permission collection the role file validates against:
	// the resources, fields and conditions the release declares. The router package
	// generates it and imports this one, so the configuration passes it in.
	Collection access.PermissionCollection
	// ChangeSignal carries the engine's policy-change hints between the application's
	// instances (auth.PolicySignal over the live service): the engine announces after
	// every policy write it makes (the directory sync's at login) and rereads on every
	// hint it receives, the heartbeat left as the backstop. Required.
	ChangeSignal access.ChangeSignal
}

// Session is the auth's session manager: one Auth session over the Spanner store, whose
// hooks are typed to the store's transaction.
type Session = session.Auth[session.NoCustomData, session.NoCustomData, *cloudspanner.ReadWriteTransaction]

// Auth is the members auth: its permission store and its session manager with the
// directory registered on it.
type Auth struct {
	access   *access.Client
	handlers *Handlers
}

// Handlers is the auth's served surface, the shape the generated router names for the
// portal outlet (router.PortalSession): the Auth's middleware and handlers, what the
// login page is told, the pending sign-in handlers, and the directory's handlers under
// its registration name.
type Handlers struct {
	*Session
	directory *google.Method
}

// DirectoryLogin sends the browser to the company's directory.
func (h *Handlers) DirectoryLogin() http.HandlerFunc {
	return h.directory.Login()
}

// DirectoryCallback receives the browser back from the directory, provisions or finds
// the person, reconciles their roles and starts the session.
func (h *Handlers) DirectoryCallback() http.HandlerFunc {
	return h.directory.Callback()
}

// PendingStatus reports the sign-in that waits for a password confirmation or an MFA
// step, if any; a member has no password, so none waits today.
func (h *Handlers) PendingStatus() http.HandlerFunc {
	return h.Pending().Status()
}

// PendingConfirmWithPassword completes a waiting sign-in with the user's password.
func (h *Handlers) PendingConfirmWithPassword() http.HandlerFunc {
	return h.Pending().ConfirmWithPassword()
}

// PendingCancel discards a waiting sign-in.
func (h *Handlers) PendingCancel() http.HandlerFunc {
	return h.Pending().Cancel()
}

// New opens the auth's permission store and session manager over the database. The
// permission engine validates the role file against the collection and refuses to start
// on a file that does not parse or grants what the release does not declare; it then
// blocks until its first policy snapshot is loaded. It announces its policy writes and
// watches the other instances' through the change signal, which is required.
func New(ctx context.Context, db *cloudspanner.Client, settings *Settings) (*Auth, error) {
	if settings.ChangeSignal == nil {
		return nil, errors.New("members.Settings.ChangeSignal is required: the engine announces and watches policy changes through it")
	}
	store, err := spannerstore.New(db, spannerstore.WithPrefix(TablePrefix))
	if err != nil {
		return nil, errors.Wrap(err, "spannerstore.New()")
	}
	accessClient, err := access.New(store, access.WithDefaultRoles(settings.Collection, Roles()), access.WithChangeSignal(settings.ChangeSignal))
	if err != nil {
		return nil, errors.Wrap(err, "access.New()")
	}
	if err := accessClient.WaitReady(ctx); err != nil {
		return nil, errors.Wrap(err, "access.Client.WaitReady()")
	}

	events, err := sessionstorage.NewAuthEventsTable(eventsTable)
	if err != nil {
		return nil, errors.Wrap(err, "sessionstorage.NewAuthEventsTable()")
	}

	// One Auth session the directory establishes. Every verified sign-in the hosted
	// domain admits goes ahead (signin.AllowAll); the directory's groups then decide
	// what the person may do.
	membersSession, err := session.NewAuth[session.NoCustomData, session.NoCustomData](
		sessionstorage.NewSpannerAuth(db, sessionstorage.WithAuthEvents(events)),
		settings.CookieKey,
		signin.AllowAll,
		session.WithSessionTableName(sessionsTable),
		session.WithUserTableName(usersTable),
		session.WithIdentitiesTableName(identitiesTable),
		session.WithPendingSignInsTableName(pendingTable),
		session.WithCookieName(Name),
		session.WithXSRFCookieName(XSRFCookie),
		session.WithSessionTimeout(settings.SessionTimeout),
	)
	if err != nil {
		return nil, errors.Wrap(err, "session.NewAuth()")
	}

	// The directory's groups are the source of role membership, read through the Cloud
	// Identity Groups API with the person's own sign-in token, as far as the configured
	// lookup reaches: the groups they are a direct member of, or with the nested lookup
	// the groups above those too, level by level. Under the session library's skipAuth
	// build tag the lookup is simulated: every sign-in is in the groups APP_ROLES names.
	lookup, err := google.ParseGroupLookup(settings.Directory.GroupLookup)
	if err != nil {
		return nil, errors.Wrap(err, "google.ParseGroupLookup()")
	}

	// The directory, registered before the first request. The directory is the authority
	// for role membership: every sign-in reconciles the person's roles to the Google
	// Groups the directory places them in (those named by the group prefix), removing
	// any it does not name, and a sign-in naming no known role is refused. A global role
	// is written in the global partition and a domain role in every sector, so the sync
	// keeps no list of sectors. Hand-assigned roles do not survive it, so nothing in the
	// application assigns roles in this store.
	directory := google.SignIn(membersSession, DirectoryRegistration, provisionMember, &google.Config{
		ClientID:      settings.Directory.ClientID,
		ClientSecret:  settings.Directory.ClientSecret,
		RedirectURL:   settings.Directory.RedirectURL,
		Connection:    settings.Directory.Connection,
		HostedDomains: []string{settings.Directory.HostedDomain},
		RoleSync:      google.RoleSync(accessClient.UserManager(), settings.Directory.GroupPrefix, lookup),
		LoginURL:      settings.LoginURL,
	})

	return &Auth{access: accessClient, handlers: &Handlers{Session: membersSession, directory: directory}}, nil
}

// provisionMember is the directory's user resolver: an identity the directory verifies
// for the first time becomes a member under the verified email, with no password (the
// directory is their only way in), linked by the directory's immutable subject so a
// renamed account stays the same person at the next sign-in.
func provisionMember(_ context.Context, _ *cloudspanner.ReadWriteTransaction, identity *google.Identity) (*signin.Resolution, error) {
	return signin.Provision(&signin.NewUser{Username: identity.Email}, nil), nil
}

// Access returns the auth's permission engine: the handlers check against it, and its
// UserManager writes roles, grants, and role assignments.
func (a *Auth) Access() *access.Client {
	return a.access
}

// Session returns the auth's session manager.
func (a *Auth) Session() *Session {
	return a.handlers.Session
}

// Handlers returns the auth's served surface.
func (a *Auth) Handlers() *Handlers {
	return a.handlers
}

// Close releases the permission engine.
func (a *Auth) Close() error {
	if err := a.access.Close(); err != nil {
		return errors.Wrap(err, "access.Client.Close()")
	}

	return nil
}
