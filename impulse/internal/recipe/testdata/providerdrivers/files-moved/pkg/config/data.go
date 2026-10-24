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
	database "github.com/cccteam/ccc/resource/database/spanner"
	"github.com/cccteam/ccc/resource/filestore"
	"github.com/cccteam/ccc/resource/live"
	liveservice "github.com/cccteam/ccc/resource/live/firestore"
	"github.com/cccteam/session/mail"
	"github.com/cccteam/session/mail/workspace"
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
// Firestore database or the emulator, and the staff auth (its permission engine, which
// signals policy changes through the live service, and its session manager).
type DataConfiguration struct {
	*coreConfiguration
	env *dataConfig
	// database is the database driver: the Spanner client over the database and the
	// resource client over it.
	database  *database.Driver
	cursorKey *resource.CursorKey
	staff     *staff.Auth
	live      *liveservice.Service
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
	db, err := database.Open(ctx, env.Database.Settings, fileStoreOptions(files)...)
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

	// The live service first: the auth's permission engine signals policy changes
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

	// The sender every auth mails through: the Workspace relay where the deployment names
	// a mailbox, the log in development.
	sender, err := openMailSender(env)
	if err != nil {
		return nil, err
	}
	// The auth's default roles validate against the generated collection when its engine
	// opens; the collection is passed in here, since the auth package imports no router.
	// The auth opens its stores on the driver's Spanner client: the one vendor-specific
	// seam left in the level, pending the PostgreSQL client (cccteam/ccc#852).
	staffAuth, err := staff.New(ctx, db.SpannerClient, env.staffSettings(router.Collection(), liveService, cookieKey, sender))
	if err != nil {
		return nil, errors.Wrap(err, "staff.New()")
	}

	return &DataConfiguration{
		coreConfiguration: core,
		env:               env,
		database:          db,
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
		log.Print(errors.Wrap(err, "liveservice.Service.Close()"))
	}
	if c.files != nil {
		if err := c.files.Close(); err != nil {
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
	return c.env.Live.BrowserOrigins()
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
	Database DatabaseSettings
	Live     LiveSettings

	// SessionTimeout is the idle timeout of a browser session.
	SessionTimeout time.Duration `env:"APP_DEFAULT_SESSION_TIMEOUT,default=10m"`

	// CookieKey signs session cookies: a Base64-encoded string of at least 32 bytes of
	// cryptographically secure random data. Unset, an ephemeral key is generated at
	// startup.
	CookieKey string `env:"APP_COOKIE_KEY" secret:"generated"`

	// PublicURL is the application's address as a browser reaches it, scheme and host
	// with no path: every link the auths mail (an invitation, an address verification, a
	// sign-up) opens the login page under it. The dev proxy in development; a deployment
	// sets the served hostname, which the stack derives from the site's canonical one.
	PublicURL string `env:"APP_PUBLIC_URL,required"`

	// MailMailbox and MailAppPassword send the auths' mail through the Google Workspace
	// SMTP relay as the mailbox (a no-reply address of the organization's domain, with
	// two-step verification and an app password, the secret); MailFrom is the address the
	// mail is from when it is not the mailbox, an alias of its account. Unset, as in
	// development, the mail is written to the log and nothing is sent.
	MailMailbox     string `env:"APP_MAIL_MAILBOX"`
	MailFrom        string `env:"APP_MAIL_FROM"`
	MailAppPassword string `env:"APP_MAIL_APP_PASSWORD" secret:"true"`

	// FileStores are the file stores' URLs (files.go).
	FileStores FileStoreSettings
}

// staffSettings builds the staff auth's settings: the generated collection its default
// roles validate against, the live service its engine signals policy changes through, the
// cookie key its sessions are signed with, the session timeout, and the sender its mail
// goes through with the login page its links open. A directory registration's settings
// join the literal here (impulse add method), read from the environment under the
// registration's variables, so the constructor stays one call per auth.
func (env *dataConfig) staffSettings(collection access.PermissionCollection, signals staff.Signals, cookieKey string, sender mail.Sender) *staff.Settings {
	return &staff.Settings{
		Collection:     collection,
		Signals:        signals,
		CookieKey:      cookieKey,
		SessionTimeout: env.SessionTimeout,
		Mail:           sender,
		MailLoginURL:   env.PublicURL + "/login",
	}
}

// openMailSender opens the sender every auth mails through: the Google Workspace SMTP
// relay as the mailbox the deployment names (APP_MAIL_MAILBOX, with its app password),
// or the log where none is named, as in development, where nothing is sent and a
// developer follows the link from the log.
func openMailSender(env *dataConfig) (mail.Sender, error) {
	if env.MailMailbox == "" {
		return mail.Log{}, nil
	}
	sender, err := workspace.New(&workspace.Settings{Mailbox: env.MailMailbox, From: env.MailFrom, AppPassword: env.MailAppPassword, Name: staff.Application})
	if err != nil {
		return nil, errors.Wrap(err, "workspace.New()")
	}

	return sender, nil
}
