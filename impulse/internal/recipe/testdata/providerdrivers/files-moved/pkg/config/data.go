package config

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"log"
	"time"

	"example.com/acme/beacon/pkg/auth/staff"
	"example.com/acme/beacon/pkg/router"
	"github.com/cccteam/access"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/database/spanner"
	"github.com/cccteam/ccc/resource/filestore"
	"github.com/cccteam/ccc/resource/live"
	livefirestore "github.com/cccteam/ccc/resource/live/firestore"
	"github.com/go-playground/errors/v5"
	"github.com/sethvargo/go-envconfig"
)

// SpannerSettings is the database driver's variables (spanner.Settings: the project, the
// instance and the database), embedded so the driver declares them and the tools read its
// declaration. It is the first half of the data level, loadable on its own so
// cmd/bootstrap can create the instance and database before any client opens against
// them. A struct of its own, since the live driver's settings sit beside it in the data
// level and one struct cannot embed two types named Settings.
type SpannerSettings struct {
	spanner.Settings
}

// LoadSpannerSettings reads the database identity from the environment without opening
// anything.
func LoadSpannerSettings(ctx context.Context) (SpannerSettings, error) {
	var settings SpannerSettings
	if err := envconfig.ProcessWith(ctx, &envconfig.Config{Target: &settings, Lookuper: envconfig.OsLookuper()}); err != nil {
		return SpannerSettings{}, errors.Wrap(err, "envconfig.ProcessWith()")
	}

	return settings, nil
}

// FirestoreSettings is the live driver's variables (livefirestore.Settings: the project
// and the database the live service runs on, beside the Spanner database, the web API key
// the browser initializes the SDK with, and the emulator host), embedded so the driver
// declares them and the tools read its declaration. The database id is how a deployment
// hands the database to the application (APP_FIRESTORE_DATABASE), with its project
// (GOOGLE_CLOUD_FIRESTORE_PROJECT); the emulator host is how the development stack does.
// The live service is required: the driver refuses to open with neither the database nor
// the emulator set.
type FirestoreSettings struct {
	livefirestore.Settings
}

// DataConfiguration is the second level: every process that opens the database. It
// owns the database driver (the Spanner client over the database and the resource
// client over it, the one the handlers read and write through), the live service over the
// Firestore database or the emulator, and the staff auth (its permission engine, which
// signals policy changes through the live service, and its session manager).
type DataConfiguration struct {
	*coreConfiguration
	env *dataConfig
	// database is the database driver: the Spanner client over the database and the
	// resource client over it.
	database  *spanner.Driver
	cursorKey *resource.CursorKey
	staff     *staff.Auth
	live      *livefirestore.Service
	// files is the file store the resource client is built over (files.go), nil when
	// APP_FILE_STORE is unset.
	files filestore.Store
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

	// The file store belongs beside the database: the database driver builds the resource
	// client over it (resource.WithFileStore), so the generated handlers read and write files
	// through the client, and a committed transaction's released objects are deleted from the
	// store. It opens from APP_FILE_STORE (files.go): a directory in development, a bucket on
	// Cloud Run. A process whose variable is unset runs without a store, and the server
	// refuses to start when its routes need one.
	files, err := openFileStore(ctx, env.FileStores)
	if err != nil {
		return nil, err
	}

	// The database driver: the Spanner client over the database the settings name, and
	// the resource client over it.
	database, err := spanner.Open(ctx, env.Spanner.Settings, fileStoreOptions(files)...)
	if err != nil {
		return nil, errors.Wrap(err, "spanner.Open()")
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

	// The live service first: the auth's permission engine signals policy changes
	// through it. The live driver opens the Firestore database the settings name, or the
	// emulator, under the Spanner project where the settings name none, since the emulator
	// takes any project id. The live service is required: the generated handlers serve the
	// live pages through it, and the application's instances signal each other through its
	// signals document (a feature flag flip, a policy write), so a configuration naming
	// neither, or naming the database without its project, fails the start here.
	liveService, err := livefirestore.Open(ctx, env.Firestore.Settings, env.Spanner.ProjectID)
	if err != nil {
		return nil, errors.Wrap(err, "firestore.Open()")
	}

	// The auth's default roles validate against the generated collection when its engine
	// opens; the collection is passed in here, since the auth package imports no router.
	staffAuth, err := staff.New(ctx, database.SpannerClient, staff.Settings{Collection: router.Collection(), Signals: liveService, CookieKey: cookieKey, SessionTimeout: env.SessionTimeout})
	if err != nil {
		return nil, errors.Wrap(err, "staff.New()")
	}

	return &DataConfiguration{
		coreConfiguration: core,
		env:               env,
		database:          database,
		cursorKey:         cursorKey,
		staff:             staffAuth,
		live:              liveService,
		files:             files,
	}, nil
}

// Close releases the level's clients in the reverse of their opening (the auth before
// the live service its engine signals through), then the levels below it.
func (c *DataConfiguration) Close() {
	if err := c.staff.Close(); err != nil {
		log.Print(errors.Wrap(err, "staff.Auth.Close()"))
	}
	if err := c.live.Close(); err != nil {
		log.Print(errors.Wrap(err, "firestore.Service.Close()"))
	}
	if c.files != nil {
		if err := c.files.Close(); err != nil {
			log.Print(errors.Wrap(err, "filestore.Store.Close()"))
		}
	}

	c.database.Close()
	c.coreConfiguration.Close()
}

// Spanner returns the database identity.
func (c *DataConfiguration) Spanner() SpannerSettings {
	return c.env.Spanner
}

// ResourceClient returns the database client the resource layer uses: the driver's.
func (c *DataConfiguration) ResourceClient() resource.Client {
	return c.database.ResourceClient
}

// CursorKey returns the key that seals list cursors.
func (c *DataConfiguration) CursorKey() *resource.CursorKey {
	return c.cursorKey
}

// Access returns the permission engine the handlers check against: the staff auth's,
// which is the auth the console binds to.
func (c *DataConfiguration) Access() access.Controller {
	return c.staff.Access()
}

// UserManager returns the staff auth's permission writer: roles, grants, and role
// assignments.
func (c *DataConfiguration) UserManager() access.UserManager {
	return c.staff.Access().UserManager()
}

// Staff returns the staff auth: the one the console binds to.
func (c *DataConfiguration) Staff() *staff.Auth {
	return c.staff
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
	return c.env.Firestore.BrowserOrigins()
}

// cookieKey returns the configured session cookie key, or an ephemeral one when none is
// configured. An ephemeral key means sessions do not survive a restart.
func cookieKey(configured string) (string, error) {
	if configured != "" {
		return configured, nil
	}

	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return "", errors.Wrap(err, "rand.Read()")
	}

	return base64.StdEncoding.EncodeToString(key), nil
}

// dataConfig holds the environment every database-opening process reads.
type dataConfig struct {
	Spanner   SpannerSettings
	Firestore FirestoreSettings

	// SessionTimeout is the idle timeout of a browser session.
	SessionTimeout time.Duration `env:"APP_DEFAULT_SESSION_TIMEOUT,default=10m"`

	// CookieKey signs session cookies: a Base64-encoded string of at least 32 bytes of
	// cryptographically secure random data. Unset, an ephemeral key is generated at
	// startup.
	CookieKey string `env:"APP_COOKIE_KEY" secret:"true"`

	// FileStores are the file stores' URLs (files.go).
	FileStores FileStoreSettings
}
