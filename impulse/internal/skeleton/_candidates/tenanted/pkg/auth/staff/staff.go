// Package staff is the staff auth: the people who sign in to the console with a password
// and hold their roles in the staff permission store.
//
// An auth is a package. It owns its session manager, in its login flavor and with its own
// session and user tables and cookie; its permission store, with its own table prefix; and
// its role configuration. A site or an outlet binds to an auth by composing its handlers,
// and two auths on one database and one host never collide, because everything an auth
// names carries its name. The package is named for the population, never for the flavor:
// the staff can move from a password to a directory without the package moving.
package staff

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
	Name = "staff"
	// TablePrefix is the name's PascalCase form, which prefixes every table the auth owns.
	TablePrefix = "Staff"

	// RolesPath is the committed role configuration MigrateRoles reconciles into this
	// auth's store, relative to the module root. The Administrator role at each scope is
	// implicit: it carries every permission registered there.
	RolesPath = "schema/roles/" + Name + ".json"

	// The auth's tables, which schema/migrations creates.
	sessionsTable = TablePrefix + "Sessions"
	usersTable    = TablePrefix + "SessionUsers"
)

// Settings are the auth's environment-derived settings.
type Settings struct {
	// CookieKey signs session cookies: a Base64-encoded string of at least 32 bytes of
	// cryptographically secure random data.
	CookieKey string
	// SessionTimeout is the idle timeout of a browser session.
	SessionTimeout time.Duration
}

// Auth is the staff auth: its permission store and its session manager.
type Auth struct {
	access  *access.Client
	session *session.PasswordAuth[session.NoCustomData, session.NoCustomData]
}

// New opens the auth's permission store and session manager over the database. The
// permission engine blocks until its first policy snapshot is loaded.
func New(ctx context.Context, db *cloudspanner.Client, settings Settings) (*Auth, error) {
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

	passwordAuth, err := session.NewPasswordAuth[session.NoCustomData, session.NoCustomData](
		sessionstorage.NewSpannerPasswordAuth(db),
		settings.CookieKey,
		session.WithSessionTableName(sessionsTable),
		session.WithUserTableName(usersTable),
		session.WithCookieName(Name),
		session.WithSessionTimeout(settings.SessionTimeout),
	)
	if err != nil {
		return nil, errors.Wrap(err, "session.NewPasswordAuth()")
	}

	return &Auth{access: accessClient, session: passwordAuth}, nil
}

// Access returns the auth's permission engine: the handlers check against it, and its
// UserManager writes roles, grants, and role assignments.
func (a *Auth) Access() *access.Client {
	return a.access
}

// Session returns the auth's session manager.
func (a *Auth) Session() *session.PasswordAuth[session.NoCustomData, session.NoCustomData] {
	return a.session
}

// Close releases the permission engine.
func (a *Auth) Close() error {
	if err := a.access.Close(); err != nil {
		return errors.Wrap(err, "access.Client.Close()")
	}

	return nil
}
