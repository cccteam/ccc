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
// Three ways in, one population. The password method checks a persona's password, and the
// personas' roles are the bootstrap's (cmd/bootstrap/users.json). The Google method
// provisions whoever the directory verifies under the verified email and reconciles that
// person's roles to the directory's groups at every sign-in (google.RoleSync): the two
// methods meet in one users table, one session table and one cookie, so a session opened
// either way is a crew session. In development the directory is simulated and
// APP_GOOGLE_USERNAME and APP_GOOGLE_ROLES in .envrc say who the Google sign-in is. The
// sign-up registration is the third way, for a person on no manifest and in no directory:
// any Google account, open sign-up, so the first sign-in makes a crew login holding no
// role and no sector, and the roles stay the application's (google.DisableRoleSync). Such
// a person's one way to a sector is to chart their own, the Sector record's self-service
// creation shape; in development APP_SIGN_UP_USERNAME says who the simulated sign-up is.
//
// Demonstrates: auth.password, auth.two-populations, auth.multi-method, auth.directory-roles, auth.open-sign-up.
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
	// SignUpRegistration is the name the open sign-up method is registered under: any
	// Google account, through user/sign-up/login, and in development the registration
	// whose simulated person APP_SIGN_UP_USERNAME names.
	SignUpRegistration = "sign-up"

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
	// SignUpRedirectURL is the browser-facing callback of the sign-up registration, the
	// crew's third way in: any Google account, under the same client as the directory,
	// returned to its own callback (user/sign-up/callback).
	SignUpRedirectURL string
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
	signUp   *google.Method
}

// PasswordLogin is the password method's login: {"username", "password"} in, the session
// cookie out.
func (h *Handlers) PasswordLogin() http.HandlerFunc {
	return h.password.Login()
}

// PasswordChange is the password method's change: {"currentPassword", "newPassword"} in
// from a signed-in person, who may be one whose operator-set password must change before
// anything else is served. It validates the session itself, so the generated router
// mounts it outside ValidateSession, behind the XSRF guard.
func (h *Handlers) PasswordChange() http.HandlerFunc {
	return h.password.ChangeUserPassword()
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

// SignUpLogin sends the browser to Google to sign up with any Google account: the
// crew's third way in, for a person the manifest and the directory do not know.
func (h *Handlers) SignUpLogin() http.HandlerFunc {
	return h.signUp.Login()
}

// SignUpCallback receives the browser back from Google, makes a crew login of a person
// it has never seen (open sign-up) holding no role, and starts the session.
func (h *Handlers) SignUpCallback() http.HandlerFunc {
	return h.signUp.Callback()
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

	// One Auth session that both methods establish. The crew has no sign-in policy of
	// its own: the library's checks decide, and a valid credential signs in.
	crewSession, err := session.NewAuth[session.NoCustomData, session.NoCustomData](
		sessionstorage.NewSpannerAuth(db, sessionstorage.WithImpersonation(impersonation), sessionstorage.WithAuthEvents(events)),
		settings.CookieKey,
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
	// as the configured lookup reaches. A group is an email address, so the directory
	// names a role in lowercase (crew-pilot@ names pilot); the sync resolves each name
	// at the store without regard to case and writes the role file's spelling (Pilot).
	// Under the session library's skipAuth build tag the lookup is simulated: the
	// sign-in is in the groups APP_GOOGLE_ROLES names, in any case.
	lookup, err := google.ParseGroupLookup(settings.Directory.GroupLookup)
	if err != nil {
		return nil, errors.Wrap(err, "google.ParseGroupLookup()")
	}

	// The methods, registered before the first request: the password method under the
	// library's name, and the Google method under GoogleRegistration. The Google method's
	// organization slot is the crew's one Workspace, named by the connection setting
	// (the name every directory identity's link carries) and checked against the hosted
	// domain. Its account rule is the library's standard one, members of the directory:
	// a person the directory verifies for the first time becomes a crew member under the
	// verified email, with no password (the directory is their way in) and the roles
	// the sync writes at this sign-in. A verified email that is a username a password
	// persona already holds links the identity to that persona once their password
	// confirms it (the directory's word alone is not enough), so the development login
	// APP_GOOGLE_USERNAME names is one that is not on the manifest.
	password := session.PasswordSignIn(crewSession)
	googleMethod := google.SignIn(crewSession, GoogleRegistration, nil, &google.Config{
		ClientID:     settings.Directory.ClientID,
		ClientSecret: settings.Directory.ClientSecret,
		RedirectURL:  settings.Directory.RedirectURL,
		Workspaces:   google.Workspace(settings.Directory.Connection, settings.Directory.HostedDomain),
		RoleSync:     google.RoleSync(accessClient.UserManager(), settings.Directory.GroupPrefix, lookup),
		LoginURL:     settings.LoginURL,
	})
	// The sign-up registration, under SignUpRegistration: any Google account
	// (google.AnyGoogleAccount), the open sign-up rule (signin.OpenSignUp: a person the
	// auth has never seen becomes a crew login under the verified email), and the
	// application as the authority for roles (google.DisableRoleSync: the sign-in neither
	// reads nor writes roles, so the person holds nothing until they chart a sector of
	// their own or an operator seats them). It shares the directory's client; only the
	// callback is its own.
	signUpMethod := google.SignIn(crewSession, SignUpRegistration, nil, &google.Config{
		ClientID:     settings.Directory.ClientID,
		ClientSecret: settings.Directory.ClientSecret,
		RedirectURL:  settings.SignUpRedirectURL,
		Workspaces:   google.AnyGoogleAccount(),
		RoleSync:     google.DisableRoleSync(),
		Accounts:     signin.OpenSignUp,
		LoginURL:     settings.LoginURL,
	})

	return &Auth{access: accessClient, handlers: &Handlers{Session: crewSession, password: password, google: googleMethod, signUp: signUpMethod}}, nil
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
