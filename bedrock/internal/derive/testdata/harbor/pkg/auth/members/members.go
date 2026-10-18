// Package members is the members auth: the portal's people, who sign in through the
// organization's directory (OpenID Connect against Microsoft Entra) and hold their roles
// in the members permission store, which the application assigns: the directory proves
// who someone is, and the deployment seeds the first administrator's membership under the
// sign-in name the placement names (APP_MEMBERS_ADMINISTRATOR) while the auth has no users.
//
// An auth is a package. It owns its session manager, one Auth session with the sign-in
// methods registered on it (here directory), with its own session, user,
// identity, auth event and pending sign-in tables and cookie; its permission store, with
// its own table prefix; and its role configuration. Each method is its own file in this
// package, named for its registration. The package is named for the population, never
// for how it signs in: the members can gain or lose a sign-in method without the package
// moving.
package members

import (
	"context"
	"net/http"
	"time"

	cloudspanner "cloud.google.com/go/spanner"
	"github.com/cccteam/access"
	"github.com/cccteam/access/spannerstore"
	"github.com/cccteam/session"
	"github.com/cccteam/session/azure"
	"github.com/cccteam/session/sessionstorage"
	"github.com/go-playground/errors/v5"
)

const (
	// Name is the auth's name: the cookie its sessions ride in and the stem of its roles
	// file.
	Name = "members"
	// TablePrefix is the name's PascalCase form, which prefixes every table the auth owns.
	TablePrefix = "Members"

	// XSRFCookie is the cookie the auth issues its XSRF token in.
	XSRFCookie = Name + "-xsrf"

	// RolesPath is the committed role configuration MigrateRoles reconciles into this
	// auth's store, relative to the module root.
	RolesPath = "schema/roles/" + Name + ".json"

	// The auth's tables, which schema/migrations creates.
	sessionsTable   = "MembersSessions"
	usersTable      = "MembersSessionUsers"
	identitiesTable = "MembersSessionIdentities"
	eventsTable     = "MembersSessionAuthEvents"
	pendingTable    = "MembersSessionPendingSignIns"
)

// Settings are the auth's environment-derived settings, the settings of each sign-in
// method among them.
type Settings struct {
	// CookieKey signs session cookies: a Base64-encoded string of at least 32 bytes of
	// cryptographically secure random data.
	CookieKey string
	// SessionTimeout is the idle timeout of a browser session.
	SessionTimeout time.Duration
	// LoginURL is the browser page a refused or waiting sign-in returns to.
	LoginURL string
	// Directory is the directory registration's settings: how the application is known
	// to Microsoft, read from the environment (APP_MEMBERS_DIRECTORY_TENANT_ID,
	// APP_MEMBERS_DIRECTORY_CLIENT_ID, APP_MEMBERS_DIRECTORY_CLIENT_SECRET,
	// APP_MEMBERS_DIRECTORY_REDIRECT_URL).
	Directory DirectorySettings
}

// Session is the auth's session manager: one Auth session over the Spanner store, whose
// hooks are typed to the store's transaction.
type Session = session.Auth[session.NoCustomData, session.NoCustomData, *cloudspanner.ReadWriteTransaction]

// Auth is the members auth: its permission store and its session manager with the
// sign-in methods registered on it.
type Auth struct {
	access   *access.Client
	handlers *Handlers
}

// Handlers is the auth's served surface, the shape the generated router names for the
// outlet bound to it: the Auth's middleware and handlers, the pending sign-in handlers,
// and each method's handlers under its registration name (in the method's own file).
type Handlers struct {
	*Session
	directory *azure.Method
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
