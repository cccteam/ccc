package config

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"log"
	"time"

	"github.com/cccteam/access"
	"github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/outlets/app"
	"github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/outlets/pkg/auth/members"
	"github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/outlets/pkg/auth/staff"
	"github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/outlets/pkg/router"
	"github.com/cccteam/ccc/resource"
	database "github.com/cccteam/ccc/resource/database/spanner"
	"github.com/cccteam/ccc/resource/live"
	liveservice "github.com/cccteam/ccc/resource/live/firestore"
	"github.com/go-playground/errors/v5"
	"github.com/sethvargo/go-envconfig"
)

// DatabaseSettings is the database driver's variables (database.Settings, whichever
// driver the import names: the Spanner driver's project, instance, database and emulator
// host), embedded so the driver declares them and the tools read its declaration. It is
// the first half of the data level, loadable on its own so cmd/bootstrap can create the
// instance and database before any client opens against them. A struct of its own, since
// the live driver's settings sit beside it in the data level and one struct cannot embed
// two types named Settings.
type DatabaseSettings struct {
	database.Settings
}

// LoadDatabaseSettings reads the database identity from the environment without opening
// anything.
func LoadDatabaseSettings(ctx context.Context) (DatabaseSettings, error) {
	var settings DatabaseSettings
	if err := envconfig.ProcessWith(ctx, &envconfig.Config{Target: &settings, Lookuper: envconfig.OsLookuper()}); err != nil {
		return DatabaseSettings{}, errors.Wrap(err, "envconfig.ProcessWith()")
	}

	return settings, nil
}

// LiveSettings is the live driver's variables (liveservice.Settings, whichever driver the
// import names: the Firestore driver's project and database the live service runs on,
// beside the application's database, the web API key the browser initializes the SDK
// with, and the emulator host), embedded so the driver declares them and the tools read
// its declaration. The database id is how a deployment hands the database to the
// application (APP_FIRESTORE_DATABASE), with its project (GOOGLE_CLOUD_FIRESTORE_PROJECT);
// the emulator host is how the development stack does. The live service is required: the
// driver refuses to open with neither the database nor the emulator set.
type LiveSettings struct {
	liveservice.Settings
}

// DataConfiguration is the second level: every process that opens the database. It
// owns the database driver (the Spanner client over the database and the resource
// client over it, the one the handlers read and write through), the live service over the
// Firestore database or the emulator, the two auths (the staff auth the console binds to
// and the members auth the portal binds to, each its own permission engine signaling
// policy changes through the live service, and its own session manager), and the tenant
// roster.
type DataConfiguration struct {
	*coreConfiguration
	env *dataConfig
	// database is the database driver: the Spanner client over the database and the
	// resource client over it.
	database  *database.Driver
	cursorKey *resource.CursorKey
	staff     *staff.Auth
	members   *members.Auth
	tenants   *resource.TenantRoster
	live      *liveservice.Service
}

// NewDataConfiguration loads the core and data levels and opens their clients. The
// permission engine blocks until its first policy snapshot is loaded.
func NewDataConfiguration(ctx context.Context) (*DataConfiguration, error) {
	core, err := newCoreConfiguration(ctx)
	if err != nil {
		return nil, err
	}

	env := &dataConfig{}
	if err := envconfig.ProcessWith(ctx, &envconfig.Config{Target: env, Lookuper: envconfig.OsLookuper()}); err != nil {
		return nil, errors.Wrap(err, "envconfig.ProcessWith()")
	}

	// The database driver: the Spanner client over the database the settings name, and
	// the resource client over it.
	db, err := database.Open(ctx, env.Database.Settings)
	if err != nil {
		return nil, errors.Wrap(err, "database.Open()")
	}

	cookieKey, err := cookieKey(env.CookieKey)
	if err != nil {
		return nil, err
	}

	// The same key material seals list cursors under its own derivation, so a
	// rotation ends outstanding cursors as it ends sessions.
	cursorKey, err := resource.NewCursorKey(cookieKey)
	if err != nil {
		return nil, errors.Wrap(err, "resource.NewCursorKey()")
	}

	// The live service first: the auths' permission engines signal policy changes
	// through it. The live driver opens the Firestore database the settings name, or the
	// emulator, under the driver's own project id where the settings name none, since the
	// emulator takes any project id. The live service is required: the generated handlers
	// serve the live pages through it, and the application's instances signal each other
	// through its signals document (a feature flag flip, a policy write), so a configuration
	// naming neither, or naming the database without its project, fails the start here.
	liveService, err := liveservice.Open(ctx, env.Live.Settings)
	if err != nil {
		return nil, errors.Wrap(err, "liveservice.Open()")
	}

	// The auth's default roles validate against the generated collection when its engine
	// opens; the collection is passed in here, since the auth package imports no router.
	// The auth opens its stores on the driver's Spanner client: the one vendor-specific
	// seam left in the level, pending the PostgreSQL client (cccteam/ccc#852).
	staffAuth, err := staff.New(ctx, db.SpannerClient, staff.Settings{Collection: router.Collection(), Signals: liveService, CookieKey: cookieKey, SessionTimeout: env.SessionTimeout})
	if err != nil {
		return nil, errors.Wrap(err, "staff.New()")
	}

	// The portal's login page is where a refused directory login returns to; the data
	// level knows it because the auth's error redirects are configured here.
	membersAuth, err := members.New(ctx, db.SpannerClient, &members.Settings{
		Collection:     router.Collection(),
		Signals:        liveService,
		CookieKey:      cookieKey,
		SessionTimeout: env.SessionTimeout,
		LoginURL:       "/portal/login",
		Directory: members.Directory{
			IssuerURL:    env.MembersIssuerURL,
			ClientID:     env.MembersClientID,
			ClientSecret: env.MembersClientSecret,
			RedirectURL:  env.MembersRedirectURL,
		},
	})
	if err != nil {
		return nil, errors.Wrap(err, "members.New()")
	}

	conf := &DataConfiguration{
		coreConfiguration: core,
		env:               env,
		database:          db,
		cursorKey:         cursorKey,
		staff:             staffAuth,
		members:           membersAuth,
		live:              liveService,
	}
	if err := conf.startTenants(ctx); err != nil {
		return nil, errors.Wrap(err, "startTenants()")
	}

	return conf, nil
}

// Close releases the level's clients in the reverse of their opening (the auths before
// the live service their engines signal through), then the levels below it.
func (c *DataConfiguration) Close() {
	if err := c.staff.Close(); err != nil {
		log.Print(errors.Wrap(err, "staff.Auth.Close()"))
	}
	if err := c.members.Close(); err != nil {
		log.Print(errors.Wrap(err, "members.Auth.Close()"))
	}
	if err := c.live.Close(); err != nil {
		log.Print(errors.Wrap(err, "liveservice.Service.Close()"))
	}
	c.database.Close()
	c.coreConfiguration.Close()
}

// Database returns the database identity.
func (c *DataConfiguration) Database() DatabaseSettings {
	return c.env.Database
}

// ResourceClient returns the database client the resource layer uses: the driver's.
func (c *DataConfiguration) ResourceClient() resource.Client {
	return c.database.ResourceClient
}

// CursorKey returns the key that seals list cursors.
func (c *DataConfiguration) CursorKey() *resource.CursorKey {
	return c.cursorKey
}

// Access returns the permission engine the handlers check against.
func (c *DataConfiguration) Access() access.Controller {
	return c.staff.Access()
}

// UserManager returns the permission engine's writer: roles, grants, and role
// assignments.
func (c *DataConfiguration) UserManager() access.UserManager {
	return c.staff.Access().UserManager()
}

// Staff returns the staff auth: the one the console binds to.
func (c *DataConfiguration) Staff() *staff.Auth {
	return c.staff
}

// Members returns the members auth: the one the portal binds to.
func (c *DataConfiguration) Members() *members.Auth {
	return c.members
}

// Live returns the live service the generated handlers subscribe through and publish
// to, and the application's instances signal each other through: the Firestore service
// over the database the deployment names or the emulator, never nil, since the live
// service is required.
func (c *DataConfiguration) Live() live.Service {
	return c.live
}

// LiveOrigins returns the origins the browser reaches the change feed at, for the
// content security policy: the Firestore emulator in development, Firebase's hosts in
// production.
func (c *DataConfiguration) LiveOrigins() []string {
	return c.env.Live.BrowserOrigins()
}

// startTenants builds the tenant roster over the Tenant record with the generated
// constructor and starts it: the roster reads the Tenants table once, fails the start
// when it cannot (the schema is behind the release), and keeps the set current until
// ctx ends, rereading on the tenants signal the record's generated write paths publish
// through the live service (a tenant created or deleted on any instance) and at its
// backstop. The generated DomainGuard and the consolidated dispatcher ask it before a
// tenant-scoped request runs, with no read and no wait, so a new tenant is usable at
// once, on every instance and on every outlet, without a restart. The foothold half of
// the concealed-domain answer comes from the session's permissions, in the store of the
// auth the request came through.
func (c *DataConfiguration) startTenants(ctx context.Context) error {
	c.tenants = app.NewTenantRoster(c.database.ResourceClient, resource.WithTenantSignals(c.live))
	if err := c.tenants.Start(ctx); err != nil {
		return errors.Wrap(err, "resource.TenantRoster.Start()")
	}

	return nil
}

// TenantRoster returns the application's tenant roster: the tenants as the generated
// guard knows them, and the roster a session's tenant list is filtered from.
func (c *DataConfiguration) TenantRoster() *resource.TenantRoster {
	return c.tenants
}

// cookieKey returns the configured session cookie key, or an ephemeral one when none is
// configured. An ephemeral key means sessions do not survive a restart.
func cookieKey(configured string) (string, error) {
	if configured != "" {
		return configured, nil
	}

	return ephemeralKey()
}

// ephemeralKey returns 32 bytes of cryptographically secure random data, Base64
// encoded: a single-process secret nothing else knows.
func ephemeralKey() (string, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return "", errors.Wrap(err, "rand.Read()")
	}

	return base64.StdEncoding.EncodeToString(key), nil
}

// dataConfig holds the environment every database-opening process reads.
type dataConfig struct {
	Database DatabaseSettings
	Live     LiveSettings

	// SessionTimeout is the idle timeout of a browser session.
	SessionTimeout time.Duration `env:"APP_DEFAULT_SESSION_TIMEOUT,default=10m"`

	// CookieKey signs session cookies: a Base64-encoded string of at least 32 bytes of
	// cryptographically secure random data. Unset, an ephemeral key is generated at
	// startup.
	CookieKey string `env:"APP_COOKIE_KEY" secret:"true"`

	// The members auth's directory registration (pkg/auth/members): the OpenID Connect
	// issuer, the application's client credentials, and the callback the directory
	// returns the browser to. Under the session library's skipAuth build tag the
	// directory is simulated and only the redirect URL is read.
	MembersIssuerURL    string `env:"APP_MEMBERS_OIDC_ISSUER_URL"`
	MembersClientID     string `env:"APP_MEMBERS_OIDC_CLIENT_ID"`
	MembersClientSecret string `env:"APP_MEMBERS_OIDC_CLIENT_SECRET" secret:"true"`
	MembersRedirectURL  string `env:"APP_MEMBERS_OIDC_REDIRECT_URL"`
}
