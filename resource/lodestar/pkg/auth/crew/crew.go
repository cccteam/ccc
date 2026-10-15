// Package crew is the crew auth: the people who sign in to the console, with a password
// or through the fleet's Google directory, and hold their roles in the crew permission
// store.
//
// An auth is a package. It owns its session manager, one Auth session with the sign-in
// methods registered on it, with its own session, user, identity, auth event and pending
// sign-in tables and cookie; its permission store, with its own table prefix; and its
// role configuration. A site or an outlet binds to an auth by composing its handlers, and
// two auths on one database and one host never collide, because everything an auth names
// carries its name. The package is named for the population, never for the method: the
// crew gained a directory sign-in beside the password without the package moving.
//
// Two ways in, one population. The password method checks a persona's password, and the
// personas' roles are the bootstrap's (cmd/bootstrap/users.json). The Google method
// provisions whoever the directory verifies under the verified email and reconciles that
// person's roles to the directory's groups at every sign-in (google.RoleSync): the two
// methods meet in one users table, one session table and one cookie, so a session opened
// either way is a crew session. In development the directory is simulated and
// APP_GOOGLE_USERNAME and APP_GOOGLE_ROLES in .envrc say who the Google sign-in is.
//
// Demonstrates: auth.password, auth.two-populations, auth.multi-method, auth.directory-roles.
package crew

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
	Name = "crew"
	// TablePrefix is the name's PascalCase form, which prefixes every table the auth owns.
	TablePrefix = "Crew"

	// XSRFCookie is the cookie the auth issues its XSRF token in. It carries the auth's name
	// so two auths on one host never overwrite each other's token; the browser echoes it in
	// the X-XSRF-TOKEN header, so the web app that binds to this auth names the same cookie.
	XSRFCookie = Name + "-xsrf"

	// GoogleRegistration is the name the Google method is registered under on the Auth:
	// the segment of its login and callback routes (user/google/login) and, in
	// development, the registration whose simulated person APP_GOOGLE_USERNAME names.
	GoogleRegistration = "google"

	// The auth's tables, which schema/migrations creates: the sessions and users of the
	// Auth session, the identity links of its Google sign-ins, the auth events each
	// session records, the pending sign-ins that wait for a confirmation or an MFA step,
	// and the impersonation record the library joins into every session read, so a
	// view-as or act-as-role session carries who established it. The names are spelled
	// out in full (not TablePrefix + "SessionUsers") so the tool's session-tables check,
	// which reads the literals, finds their migrations.
	sessionsTable   = "CrewSessions"
	usersTable      = "CrewSessionUsers"
	identitiesTable = "CrewSessionIdentities"
	eventsTable     = "CrewSessionAuthEvents"
	pendingTable    = "CrewSessionPendingSignIns"
	// ImpersonationsTable is exported: the watch desk names it.
	ImpersonationsTable = "CrewSessionImpersonations"

	// impersonationTimeout is the configured hard cap on an impersonated session; the
	// mint route shortens every view-as below it with MaxDuration.
	impersonationTimeout = 4 * time.Hour
)

// rolesFile is the auth's default roles: the release's own policy for the crew, which
// travels with the binary and is handed to the permission engine at New. The store holds
// no row for these roles; a login holds one by name alone, and the file is the complete
// statement of what each role may do. A global role is held in the global partition and a
// domain role in every sector.
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
	// LoginURL is the browser page a refused or waiting Google sign-in returns to, with
	// the reason as a code in the query (?code=, a sessioninfo.LoginRefusalCode, never
	// text) or the pending step (?pending=): the console's login page, which holds the
	// sentence for each code.
	LoginURL string
	// Directory identifies the application to the fleet's Google directory, the crew's
	// second way in.
	Directory auth.Directory
	// Collection is the generated permission collection the role file validates against:
	// the resources, fields and conditions the release declares. The router package
	// generates it and imports this one, so the configuration passes it in.
	Collection access.PermissionCollection
	// ChangeSignal carries the engine's policy-change hints between the application's
	// instances (auth.PolicySignal over the live service): the engine announces after
	// every policy write it makes and rereads on every hint it receives, the heartbeat
	// left as the backstop. Required.
	ChangeSignal access.ChangeSignal
}

// Session is the auth's session manager: one Auth session over the Spanner store, whose
// hooks are typed to the store's transaction.
type Session = session.Auth[session.NoCustomData, session.NoCustomData, *cloudspanner.ReadWriteTransaction]

// Auth is the crew auth: its permission store and its session manager with the sign-in
// methods registered on it.
type Auth struct {
	access   *access.Client
	handlers *Handlers
}

// Handlers is the auth's served surface, the shape the generated router names for the
// console outlet (router.DefaultSession): the Auth's middleware and handlers, what the
// login page is told, the pending sign-in handlers, and the handlers of each sign-in
// method under its registration name. The App embeds it.
type Handlers struct {
	*Session
	password *session.PasswordMethod
	google   *google.Method
}

// PasswordLogin is the password method's login: {"username", "password"} in, the session
// cookie out.
func (h *Handlers) PasswordLogin() http.HandlerFunc {
	return h.password.Login()
}

// GoogleLogin sends the browser to the fleet's Google directory.
func (h *Handlers) GoogleLogin() http.HandlerFunc {
	return h.google.Login()
}

// GoogleCallback receives the browser back from the directory, provisions or finds the
// person, reconciles their roles and starts the session.
func (h *Handlers) GoogleCallback() http.HandlerFunc {
	return h.google.Callback()
}

// PendingStatus reports the sign-in that waits for a password confirmation or an MFA
// step, if any.
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
		return nil, errors.New("crew.Settings.ChangeSignal is required: the engine announces and watches policy changes through it")
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

	// The impersonation record rides beside the sessions, so StartImpersonatedSession
	// can mint view-as and act-as-role sessions, and the auth events table records how
	// each session was authenticated.
	impersonation, err := sessionstorage.NewImpersonationTable(ImpersonationsTable)
	if err != nil {
		return nil, errors.Wrap(err, "sessionstorage.NewImpersonationTable()")
	}
	events, err := sessionstorage.NewAuthEventsTable(eventsTable)
	if err != nil {
		return nil, errors.Wrap(err, "sessionstorage.NewAuthEventsTable()")
	}

	// One Auth session that both methods establish. Every valid credential signs in
	// (signin.AllowAll): the crew has no sign-in policy beyond the directory's and the
	// password's own checks.
	crewSession, err := session.NewAuth[session.NoCustomData, session.NoCustomData](
		sessionstorage.NewSpannerAuth(db, sessionstorage.WithImpersonation(impersonation), sessionstorage.WithAuthEvents(events)),
		settings.CookieKey,
		signin.AllowAll,
		session.WithSessionTableName(sessionsTable),
		session.WithUserTableName(usersTable),
		session.WithIdentitiesTableName(identitiesTable),
		session.WithPendingSignInsTableName(pendingTable),
		session.WithCookieName(Name),
		session.WithXSRFCookieName(XSRFCookie),
		session.WithSessionTimeout(settings.SessionTimeout),
		session.WithImpersonationTimeout(impersonationTimeout),
	)
	if err != nil {
		return nil, errors.Wrap(err, "session.NewAuth()")
	}

	// The directory's groups are the source of a Google sign-in's role membership, read
	// through the Cloud Identity Groups API with the person's own sign-in token, as far
	// as the configured lookup reaches, and written through the writer that spells the
	// groups' lowercase names the role file's way. Under the session library's skipAuth
	// build tag the lookup is simulated: the sign-in is in the groups APP_GOOGLE_ROLES
	// names.
	lookup, err := google.ParseGroupLookup(settings.Directory.GroupLookup)
	if err != nil {
		return nil, errors.Wrap(err, "google.ParseGroupLookup()")
	}
	roleWriter, err := newDirectoryRoles(accessClient.UserManager(), Roles())
	if err != nil {
		return nil, errors.Wrap(err, "newDirectoryRoles()")
	}

	// The methods, registered before the first request: the password method under the
	// library's name, and the Google method under GoogleRegistration, provisioning a
	// person the directory verifies for the first time under the verified email.
	password := session.PasswordSignIn(crewSession)
	googleMethod := google.SignIn(crewSession, GoogleRegistration, provisionCrew, &google.Config{
		ClientID:     settings.Directory.ClientID,
		ClientSecret: settings.Directory.ClientSecret,
		RedirectURL:  settings.Directory.RedirectURL,
		HostedDomain: settings.Directory.HostedDomain,
		RoleSync:     google.RoleSync(roleWriter, settings.Directory.GroupPrefix, lookup),
		LoginURL:     settings.LoginURL,
	})

	return &Auth{access: accessClient, handlers: &Handlers{Session: crewSession, password: password, google: googleMethod}}, nil
}

// provisionCrew is the Google method's user resolver: a Google identity that is linked
// to no crew member yet becomes a new member under the verified email, with no password
// (the directory is their way in) and the roles the sync writes at this sign-in. A
// username a password persona already holds refuses the sign-in with a conflict, so
// the development login APP_GOOGLE_USERNAME names is one that is not on the manifest.
//
// Demonstrates: auth.multi-method.
func provisionCrew(_ context.Context, _ *cloudspanner.ReadWriteTransaction, identity *google.Identity) (*signin.Resolution, error) {
	return signin.Provision(&signin.NewUser{Username: identity.Email}, nil), nil
}

// Access returns the auth's permission engine: the handlers check against it, and its
// UserManager writes roles, grants, and role assignments.
func (a *Auth) Access() *access.Client {
	return a.access
}

// Session returns the auth's session manager, for the bootstrap's user creation and the
// impersonation routes' API.
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
