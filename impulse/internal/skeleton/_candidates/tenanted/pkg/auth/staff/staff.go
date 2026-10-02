// Package staff is the staff auth: the people who sign in to the console with a password
// and hold their roles in the staff permission store.
//
// An auth is a package. It owns its session manager, in its login flavor and with its own
// session and user tables and cookie; its permission store, with its own table prefix; and
// its role file, embedded beside it, so the release's default roles travel with the binary.
// A site or an outlet binds to an auth by composing its handlers, and two auths on one
// database and one host never collide, because everything an auth names carries its name.
// The package is named for the population, never for the flavor: the staff can move from a
// password to a directory without the package moving.
package staff

import (
	"context"
	_ "embed"
	"time"

	cloudspanner "cloud.google.com/go/spanner"
	"github.com/cccteam/access"
	"github.com/cccteam/access/spannerstore"
	"github.com/cccteam/session"
	"github.com/cccteam/session/sessionstorage"
	"github.com/go-playground/errors/v5"
)

const (
	// Name is the auth's name: the cookie its sessions ride in.
	Name = "staff"
	// TablePrefix is the name's PascalCase form, which prefixes every table the auth owns.
	TablePrefix = "Staff"

	// XSRFCookie is the cookie the auth issues its XSRF token in. It carries the auth's name
	// so two auths on one host never overwrite each other's token; the browser echoes it in
	// the X-XSRF-TOKEN header, so the web app that binds to this auth names the same cookie.
	XSRFCookie = Name + "-xsrf"

	// The auth's tables, which schema/migrations creates.
	sessionsTable = TablePrefix + "Sessions"
	usersTable    = TablePrefix + "SessionUsers"
)

// roleFile is the auth's default roles: the role file beside this package, embedded in the
// binary so the roles travel with the release. The file is the complete statement of the
// default roles; the store holds only the custom roles and the memberships written at run
// time, and a login holds what its memberships name. A global role is held in the global
// partition and a domain role in every tenant domain, so nothing provisions the file per
// tenant.
//
//go:embed roles.json
var roleFile access.RoleFile

// Roles returns the auth's role file, which New hands to the permission engine and the
// tests validate and read.
func Roles() access.RoleFile {
	return roleFile
}

// Settings are the auth's settings: the collection the role file validates against and the
// environment-derived values.
type Settings struct {
	// Collection is the generated permission collection: the resources, permissions and
	// fields the release declares, which the role file validates against when the engine
	// opens. The package that constructs the auth passes it in; this package imports no
	// router.
	Collection access.PermissionCollection
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
// permission engine parses and validates the role file against the collection before it
// opens, so a release whose default roles are wrong does not start, and blocks until its
// first policy snapshot is loaded.
func New(ctx context.Context, db *cloudspanner.Client, settings Settings) (*Auth, error) {
	store, err := spannerstore.New(db, spannerstore.WithPrefix(TablePrefix))
	if err != nil {
		return nil, errors.Wrap(err, "spannerstore.New()")
	}
	accessClient, err := access.New(store, access.WithDefaultRoles(settings.Collection, Roles()))
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
		session.WithXSRFCookieName(XSRFCookie),
		session.WithSessionTimeout(settings.SessionTimeout),
	)
	if err != nil {
		return nil, errors.Wrap(err, "session.NewPasswordAuth()")
	}

	return &Auth{access: accessClient, session: passwordAuth}, nil
}

// Access returns the auth's permission engine: the handlers check against it, and its
// UserManager writes custom roles, their grants, and role memberships.
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
