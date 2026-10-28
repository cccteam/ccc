// Package staff is the staff auth: the people who sign in through the organization's
// directory (OpenID Connect against Google) and hold their roles in the staff permission
// store.
//
// The directory proves who someone is and says what they may do. Role membership is the
// directory's (google.RoleSync): every login reconciles the person's roles to the Google
// Groups the directory places them in and removes any it does not name, so nothing in
// the application assigns roles in this store and the bootstrap seeds none.
//
// An auth is a package. It owns its session manager, one Auth session with the sign-in
// methods registered on it (here directory), with its own session, user, identity, auth
// event and pending sign-in tables and cookie; its permission store, with its own table
// prefix; and its role configuration. Each method is its own file in this package, named
// for its registration. A site or an outlet binds to an auth by composing its handlers,
// and two auths on one database and one host never collide, because everything an auth
// names carries its name. The same name in this auth and in another is two unrelated
// principals.
package staff

import (
	"context"
	"net/http"
	"time"

	cloudspanner "cloud.google.com/go/spanner"
	"github.com/cccteam/access"
	"github.com/cccteam/access/spannerstore"
	"github.com/cccteam/session"
	"github.com/cccteam/session/google"
	"github.com/cccteam/session/sessionstorage"
	"github.com/go-playground/errors/v5"
)

const (
	// Name is the auth's name: the cookie its sessions ride in and the stem of its roles
	// file.
	Name = "staff"
	// TablePrefix is the name's PascalCase form, which prefixes every table the auth owns.
	TablePrefix = "Staff"

	// XSRFCookie is the cookie the auth issues its XSRF token in. It carries the auth's name
	// so two auths on one host never overwrite each other's token; the browser echoes it in
	// the X-XSRF-TOKEN header, so the web app that binds to this auth names the same cookie.
	XSRFCookie = Name + "-xsrf"

	// RolesPath is the committed role configuration MigrateRoles reconciles into this
	// auth's store, relative to the module root. The file is the complete statement of
	// the auth's roles: a login holds only what it declares.
	RolesPath = "schema/roles/" + Name + ".json"

	// The auth's tables, which schema/migrations creates: the sessions and users of the
	// Auth session, the identities the directory verifies, the auth events each session
	// records and the pending sign-ins that wait for a confirmation.
	sessionsTable   = "StaffSessions"
	usersTable      = "StaffSessionUsers"
	identitiesTable = "StaffSessionIdentities"
	eventsTable     = "StaffSessionAuthEvents"
	pendingTable    = "StaffSessionPendingSignIns"
)

// Settings are the auth's environment-derived settings, the settings of each sign-in
// method among them.
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
	// Directory is the directory registration's settings: how the application is known
	// to Google, read from the environment (APP_STAFF_DIRECTORY_CLIENT_ID,
	// APP_STAFF_DIRECTORY_CLIENT_SECRET, APP_STAFF_DIRECTORY_REDIRECT_URL,
	// APP_STAFF_DIRECTORY_HOSTED_DOMAIN, APP_STAFF_DIRECTORY_ADMINISTERED_WORKSPACES,
	// APP_STAFF_DIRECTORY_GROUP_PREFIX, APP_STAFF_DIRECTORY_GROUP_LOOKUP).
	Directory DirectorySettings
}

// Session is the auth's session manager: one Auth session over the Spanner store, whose
// hooks are typed to the store's transaction.
type Session = session.Auth[session.NoCustomData, session.NoCustomData, *cloudspanner.ReadWriteTransaction]

// Auth is the staff auth: its permission store and its session manager with the sign-in
// methods registered on it.
type Auth struct {
	access   *access.Client
	handlers *Handlers
}

// Handlers is the auth's served surface, the shape the generated router names for the
// outlet bound to it: the Auth's middleware and handlers, the pending sign-in handlers,
// and each method's handlers under its registration name (in the method's own file).
type Handlers struct {
	*Session
	directory *google.Method
}

// PendingStatus reports the sign-in that waits for a password confirmation, if any.
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

// New opens the auth's permission store and session manager over the database and
// registers the sign-in methods. The permission engine blocks until its first policy
// snapshot is loaded.
func New(ctx context.Context, db *cloudspanner.Client, settings *Settings) (*Auth, error) {
	store, err := spannerstore.New(db, spannerstore.WithPrefix(TablePrefix))
	if err != nil {
		return nil, errors.Wrap(err, "spannerstore.New()")
	}
	accessClient, err := access.New(store)
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

	// One Auth session every method establishes: one cookie, one session table, one
	// middleware chain. The methods are registered on it below, before the first request.
	authSession, err := session.NewAuth[session.NoCustomData, session.NoCustomData](
		sessionstorage.NewSpannerAuth(db, sessionstorage.WithAuthEvents(events)),
		settings.CookieKey,
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

	handlers := &Handlers{Session: authSession}
	handlers.directory, err = registerDirectory(authSession, accessClient, settings)
	if err != nil {
		return nil, err
	}

	return &Auth{access: accessClient, handlers: handlers}, nil
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
