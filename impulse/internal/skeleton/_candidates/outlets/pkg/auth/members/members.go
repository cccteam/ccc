// Package members is the members auth: the people who sign in through the organization's
// directory (OpenID Connect against Azure) and hold their roles in the members permission
// store. In this application they are the portal's people.
//
// The directory proves who someone is; the application decides what they may do. Role
// membership is the application's (session.DisableRoleSync): the bootstrap assigns the
// development identities their roles, and a deployed application assigns them through its
// administration surface. The directory-run alternative, session.RoleSync, makes the
// directory's role claims the authority and removes every hand-assigned role at the next
// login, so one auth is never both.
//
// An auth is a package. It owns its session manager, in its login flavor and with its own
// session and user tables and cookie; its permission store, with its own table prefix; and
// its role configuration. A site or an outlet binds to an auth by composing its handlers,
// and two auths on one database and one host never collide, because everything an auth
// names carries its name. The same name in this auth and in another is two unrelated
// principals.
package members

import (
	"context"
	"time"

	cloudspanner "cloud.google.com/go/spanner"
	"github.com/cccteam/access"
	"github.com/cccteam/access/spannerstore"
	"github.com/cccteam/session"
	"github.com/cccteam/session/sessionstorage"
	"github.com/go-playground/errors/v5"
)

const (
	// Name is the auth's name: the cookie its sessions ride in and the stem of its roles
	// file.
	Name = "members"
	// TablePrefix is the name's PascalCase form, which prefixes every table the auth owns.
	TablePrefix = "Members"

	// XSRFCookie is the cookie the auth issues its XSRF token in. It carries the auth's name
	// so two auths on one host never overwrite each other's token; the browser echoes it in
	// the X-XSRF-TOKEN header, so the web app that binds to this auth names the same cookie.
	XSRFCookie = Name + "-xsrf"

	// RolesPath is the committed role configuration MigrateRoles reconciles into this
	// auth's store, relative to the module root. The Administrator role at each scope is
	// implicit: it carries every permission registered there.
	RolesPath = "schema/roles/" + Name + ".json"

	// The auth's tables, which schema/migrations creates: the sessions, and the user
	// anchor keyed by the directory's immutable (tenant, object) identifier pair, so a
	// renamed account stays the same person.
	sessionsTable = TablePrefix + "Sessions"
	usersTable    = TablePrefix + "OIDCUsers"
)

// Settings are the auth's environment-derived settings.
type Settings struct {
	// CookieKey signs session cookies: a Base64-encoded string of at least 32 bytes of
	// cryptographically secure random data.
	CookieKey string
	// SessionTimeout is the idle timeout of a browser session.
	SessionTimeout time.Duration
	// LoginURL is the browser page a refused login returns to, with the reason in the
	// query (?message=): the login page of the surface that binds to this auth.
	LoginURL string
	// Directory identifies the application to the directory that verifies its logins.
	Directory Directory
}

// Directory is the application's OpenID Connect registration with the directory: the
// issuer, the client credentials, and the callback the directory returns the browser to.
// Under the session library's skipAuth build tag the directory is simulated, every login
// is APP_USERNAME, and only RedirectURL is read: it is where the simulated login returns.
type Directory struct {
	IssuerURL    string
	ClientID     string
	ClientSecret string
	RedirectURL  string
}

// Auth is the members auth: its permission store and its session manager.
type Auth struct {
	access  *access.Client
	session *session.OIDCAzure[session.NoCustomData, session.NoCustomData]
}

// New opens the auth's permission store and session manager over the database. The
// permission engine blocks until its first policy snapshot is loaded.
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

	oidcAuth, err := session.NewOIDCAzure[session.NoCustomData, session.NoCustomData](
		sessionstorage.NewSpannerOIDC(db, sessionstorage.WithOIDCUsers()),
		// The application is the authority for role membership: a login neither reads
		// nor changes the person's roles.
		session.DisableRoleSync(),
		settings.CookieKey,
		settings.Directory.IssuerURL,
		settings.Directory.ClientID,
		settings.Directory.ClientSecret,
		settings.Directory.RedirectURL,
		session.WithSessionTableName(sessionsTable),
		session.WithOIDCUserTableName(usersTable),
		session.WithCookieName(Name),
		session.WithXSRFCookieName(XSRFCookie),
		session.WithSessionTimeout(settings.SessionTimeout),
		session.WithLoginURL(settings.LoginURL),
	)
	if err != nil {
		return nil, errors.Wrap(err, "session.NewOIDCAzure()")
	}

	return &Auth{access: accessClient, session: oidcAuth}, nil
}

// Access returns the auth's permission engine: the handlers check against it, and its
// UserManager writes roles, grants, and role assignments.
func (a *Auth) Access() *access.Client {
	return a.access
}

// Session returns the auth's session manager.
func (a *Auth) Session() *session.OIDCAzure[session.NoCustomData, session.NoCustomData] {
	return a.session
}

// Close releases the permission engine.
func (a *Auth) Close() error {
	if err := a.access.Close(); err != nil {
		return errors.Wrap(err, "access.Client.Close()")
	}

	return nil
}
