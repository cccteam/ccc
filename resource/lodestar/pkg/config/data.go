package config

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"log"
	"time"

	"github.com/cccteam/access"
	"github.com/cccteam/ccc/resource"
	database "github.com/cccteam/ccc/resource/database/spanner"
	"github.com/cccteam/ccc/resource/filestore"
	"github.com/cccteam/ccc/resource/live"
	liveservice "github.com/cccteam/ccc/resource/live/firestore"
	"github.com/cccteam/ccc/resource/lodestar/app"
	"github.com/cccteam/ccc/resource/lodestar/pkg/auth"
	"github.com/cccteam/ccc/resource/lodestar/pkg/auth/crew"
	"github.com/cccteam/ccc/resource/lodestar/pkg/auth/members"
	"github.com/cccteam/ccc/resource/lodestar/pkg/router"
	"github.com/cccteam/httpio"
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
// its declaration. The database id is how bedrock hands the database to an application
// (APP_FIRESTORE_DATABASE), with its project (GOOGLE_CLOUD_FIRESTORE_PROJECT); the
// emulator host is how the development stack does. One of the two is required: every
// application wires the live service, and the driver refuses to open without a database
// for it.
type LiveSettings struct {
	liveservice.Settings
}

// DataConfiguration is the second level: every process that opens the database. It
// owns the database driver (the Spanner client over the database, and the resource
// client over it with the file stores wired), the file stores, the live service over the
// Firestore database, the tenant roster (the Sectors table's keys, loaded here and kept
// current through the live service's tenants signal), and the two auths (each its
// permission engine and session manager), whose engines announce and watch policy changes
// through the live service.
type DataConfiguration struct {
	*coreConfiguration
	env *dataConfig
	// database is the database driver: the Spanner client over the database and the
	// resource client over it, the file stores wired on the client.
	database *database.Driver
	// files is the default store, the refit photos'; documents the Documents store, the
	// mission documents'. Either is nil when its variable is unset.
	files     filestore.Store
	documents filestore.Store
	cursorKey *resource.CursorKey
	crew      *crew.Auth
	tenants   *resource.TenantRoster
	members   *members.Auth
	live      *liveservice.Service
}

// NewDataConfiguration loads the core and data levels and opens their clients. Each
// auth's permission engine validates its role file against the generated permission
// collection and blocks until its first policy snapshot is loaded, and the tenant roster
// is loaded once before the level is handed out, so a process never serves on a roster
// it could not fill.
func NewDataConfiguration(ctx context.Context) (*DataConfiguration, error) {
	core, err := newCoreConfiguration(ctx)
	if err != nil {
		return nil, err
	}

	env := &dataConfig{}
	if err := envconfig.ProcessWith(ctx, &envconfig.Config{Target: env, Lookuper: envconfig.OsLookuper()}); err != nil {
		return nil, errors.Wrap(err, "envconfig.ProcessWith()")
	}

	// The file stores belong beside the database: the database driver builds the
	// resource client over them, the default store under resource.WithFileStore and the
	// Documents store under resource.WithNamedFileStore, so the generated handlers read
	// each store off the client and a transaction that deletes a document or a photo, or
	// points a row at another file, has the old object deleted from its store once the
	// commit lands. Each store opens from its own URL (APP_FILE_STORE,
	// APP_FILE_STORE_DOCUMENTS): a directory in development, a bucket on Cloud Run. A
	// store whose variable is unset is not opened: the migrate and bootstrap commands
	// build this level without the stores and never touch files, and the server refuses
	// to start without the stores its routes use (router.New).
	//
	// Demonstrates: @file.released, filestore.named.
	files, documents, err := openFileStores(ctx, env.FileStores)
	if err != nil {
		return nil, err
	}

	// The database driver: the Spanner client over the database the settings name, and
	// the resource client over it with the stores wired.
	db, err := database.Open(ctx, env.Database.Settings, fileStoreOptions(files, documents)...)
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

	// The live service: the live driver opens the Firestore database the live pages'
	// subscriptions and change sets live in, beside the Spanner database, and the signals
	// document every instance holds one listener on; against the emulator with no
	// project named, the driver's own project id stands in, since the emulator takes any
	// project id. Every application wires one: the generated handlers subscribe and publish
	// through it, the feature flags follow it, and both permission engines announce and
	// watch the policy kind through it, so it opens before the auths, and a configuration
	// naming no database for it, or a database without its project, does not start.
	liveService, err := liveservice.Open(ctx, env.Live.Settings)
	if err != nil {
		return nil, errors.Wrap(err, "liveservice.Open()")
	}

	// The one change signal both engines take: a policy write on any instance reaches
	// every engine on every instance through the policy kind of the signals document.
	policySignal := auth.PolicySignal(liveService)

	// The crew auth: the console's people, whose default roles ride in the binary and
	// validate against the generated collection, which the router package generates and
	// the auth package cannot import. The auth opens its stores on the driver's Spanner
	// client: the one vendor-specific seam left in the level, pending the PostgreSQL client
	// (cccteam/ccc#852).
	crewAuth, err := crew.New(ctx, db.SpannerClient, crew.Settings{
		CookieKey:      cookieKey,
		SessionTimeout: env.SessionTimeout,
		Collection:     router.Collection(),
		ChangeSignal:   policySignal,
	})
	if err != nil {
		return nil, errors.Wrap(err, "crew.New()")
	}

	// The tenant roster: every instance's copy of the Sectors table's keys, built by the
	// generated constructor (Sector is the @tenant record) over the driver's resource
	// client and started here beside the engines, so the start fails when the first read
	// does. It subscribes to the tenants kind on the live service, the one channel the
	// engines and the feature flags ride: a sector charted on any instance is served by
	// this one at its next request, with the library's five-minute reread as the
	// backstop; no restart, no migrate job and no new login is needed, since a role held
	// in every sector reaches the new one with nothing written. Tenancy is data, not a
	// compiled-in list.
	//
	// Demonstrates: tenancy.run-time-tenant.
	tenants := app.NewSectorRoster(db.ResourceClient, resource.WithTenantSignals(liveService))
	if err := tenants.Start(ctx); err != nil {
		return nil, errors.Wrap(err, "resource.TenantRoster.Start()")
	}

	conf := &DataConfiguration{
		coreConfiguration: core,
		env:               env,
		database:          db,
		files:             files,
		documents:         documents,
		cursorKey:         cursorKey,
		crew:              crewAuth,
		tenants:           tenants,
		live:              liveService,
	}

	// The members auth: the portal's people, whose roles are the directory's. Its role
	// synchronization writes a global role in the global partition and a domain role in
	// every sector, so a client's membership reaches each sector with one row; the
	// portal's grants then narrow by company. The portal's login page is where a refused
	// directory login returns to.
	membersAuth, err := members.New(ctx, db.SpannerClient, &members.Settings{
		CookieKey:      cookieKey,
		SessionTimeout: env.SessionTimeout,
		LoginURL:       "/portal/login",
		Collection:     router.Collection(),
		Directory: members.Directory{
			ClientID:     env.MembersClientID,
			ClientSecret: env.MembersClientSecret,
			RedirectURL:  env.MembersRedirectURL,
			HostedDomain: env.MembersHostedDomain,
			GroupPrefix:  env.MembersGroupPrefix,
			GroupLookup:  env.MembersGroupLookup,
		},
		ChangeSignal: policySignal,
	})
	if err != nil {
		return nil, errors.Wrap(err, "members.New()")
	}
	conf.members = membersAuth

	return conf, nil
}

// Close releases the level's clients, the engines before the live service they announce
// and watch through, the file stores before the database driver they are wired on, then
// the levels below it.
func (c *DataConfiguration) Close() {
	if err := c.crew.Close(); err != nil {
		log.Print(errors.Wrap(err, "crew.Auth.Close()"))
	}
	if err := c.members.Close(); err != nil {
		log.Print(errors.Wrap(err, "members.Auth.Close()"))
	}
	if err := c.live.Close(); err != nil {
		log.Print(errors.Wrap(err, "liveservice.Service.Close()"))
	}
	for _, store := range []filestore.Store{c.documents, c.files} {
		if store == nil {
			continue
		}
		if err := store.Close(); err != nil {
			log.Print(errors.Wrap(err, "filestore.Store.Close()"))
		}
	}
	c.database.Close()
	c.coreConfiguration.Close()
}

// Database returns the database identity.
func (c *DataConfiguration) Database() DatabaseSettings {
	return c.env.Database
}

// Live returns the live service the generated handlers subscribe through and publish
// to, the feature flags follow and the permission engines signal through: the Firestore
// service over the configured database or the emulator.
func (c *DataConfiguration) Live() live.Service {
	return c.live
}

// LiveOrigins returns the origins the browser reaches the change feed at, for the
// content security policy: the Firestore emulator in development, Firebase's hosts in
// production.
func (c *DataConfiguration) LiveOrigins() []string {
	return c.env.Live.BrowserOrigins()
}

// ResourceClient returns the database client the resource layer uses, the driver's, with
// the file stores wired on it: the generated handlers read each store off it, and a
// committed transaction's released objects are deleted from their store.
func (c *DataConfiguration) ResourceClient() resource.Client {
	return c.database.ResourceClient
}

// CursorKey returns the key that seals list cursors.
func (c *DataConfiguration) CursorKey() *resource.CursorKey {
	return c.cursorKey
}

// Access returns the permission engine the handlers check against: the crew auth's,
// which is the auth the console binds to.
func (c *DataConfiguration) Access() access.Controller {
	return c.crew.Access()
}

// UserManager returns the crew auth's permission writer: roles, grants, and role
// assignments.
func (c *DataConfiguration) UserManager() access.UserManager {
	return c.crew.Access().UserManager()
}

// Crew returns the crew auth: the one the console binds to.
func (c *DataConfiguration) Crew() *crew.Auth {
	return c.crew
}

// UserManagement returns the crew engine's user-management handlers (access.Handlers),
// which the console's role-membership routes delegate to behind their own permission
// checks: the crew's roles are the application's, so seating crew in a sector is the
// application's to serve.
func (c *DataConfiguration) UserManagement() access.Handlers {
	return c.crew.Access().Handlers(httpio.Log)
}

// TenantRoster returns the tenant roster: the sectors this instance serves, loaded when
// the level opened and kept current until the context the level opened under ends.
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

// dataConfig holds the environment every database-opening process reads: the two
// drivers' settings under their own names, and the level's own variables.
type dataConfig struct {
	Database DatabaseSettings
	Live     LiveSettings

	// SessionTimeout is the idle timeout of a browser session.
	SessionTimeout time.Duration `env:"APP_DEFAULT_SESSION_TIMEOUT,default=10m"`

	// CookieKey signs session cookies: a Base64-encoded string of at least 32 bytes of
	// cryptographically secure random data. Unset, an ephemeral key is generated at
	// startup.
	CookieKey string `env:"APP_COOKIE_KEY"`
	// The members auth's directory registration (pkg/auth/members): the application's client
	// credentials, the callback Google returns the browser to, the Workspace domain logins are
	// restricted to, the prefix of the Google Groups that carry roles, and how far the groups
	// lookup reaches: direct (the default), or nested for a directory that nests its role groups.
	// Under the session library's skipAuth build tag only the redirect URL, the hosted domain, and the group prefix are read.
	MembersClientID     string `env:"APP_MEMBERS_OIDC_CLIENT_ID"`
	MembersClientSecret string `env:"APP_MEMBERS_OIDC_CLIENT_SECRET"`
	MembersRedirectURL  string `env:"APP_MEMBERS_OIDC_REDIRECT_URL"`
	MembersHostedDomain string `env:"APP_MEMBERS_OIDC_HOSTED_DOMAIN"`
	MembersGroupPrefix  string `env:"APP_MEMBERS_OIDC_GROUP_PREFIX"`
	MembersGroupLookup  string `env:"APP_MEMBERS_OIDC_GROUP_LOOKUP"`

	// FileStores are the file stores' URLs.
	FileStores FileStoreSettings
}
