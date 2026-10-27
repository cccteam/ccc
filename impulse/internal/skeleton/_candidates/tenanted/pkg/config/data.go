package config

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"log"
	"time"

	"github.com/cccteam/access"
	"github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/tenanted/app"
	"github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/tenanted/pkg/auth/staff"
	"github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/tenanted/pkg/router"
	"github.com/cccteam/ccc/resource"
	database "github.com/cccteam/ccc/resource/database/spanner"
	"github.com/cccteam/ccc/resource/live"
	liveservice "github.com/cccteam/ccc/resource/live/firestore"
	"github.com/cccteam/ccc/resource/sessionusers"
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
// Firestore database or the emulator, the permission engine (which signals policy
// changes through the live service), the session manager, and the tenant roster.
type DataConfiguration struct {
	*coreConfiguration
	env *dataConfig
	// database is the database driver: the Spanner client over the database and the
	// resource client over it.
	database  *database.Driver
	cursorKey *resource.CursorKey
	staff     *staff.Auth
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

	conf := &DataConfiguration{
		coreConfiguration: core,
		env:               env,
		database:          db,
		cursorKey:         cursorKey,
		staff:             staffAuth,
		live:              liveService,
	}
	if err := conf.startTenants(ctx); err != nil {
		return nil, errors.Wrap(err, "startTenants()")
	}

	return conf, nil
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

// StaffUserStores returns the stores the staff auth's user-management surface serves over:
// the staff session's accounts (sessionusers.Accounts over its API) and the staff
// engine's memberships. A seat written through the surface is a policy write the engine
// announces to every instance.
func (c *DataConfiguration) StaffUserStores() (resource.UserAccounts, resource.UserSeats) {
	return sessionusers.Accounts(c.staff.Session().API()), c.staff.Access().UserManager()
}

// StaffAdministratorPassword returns the password the staff auth's first administrator is
// seeded with, the stack's generated secret; empty where the deployment carries none.
func (c *DataConfiguration) StaffAdministratorPassword() string {
	return c.env.StaffAdministratorPassword
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
// once, on every instance, without a restart.
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

// TenantDeclarations reads what a tenant declares about who may enter it from the
// roster's copy, reloaded on every write of the Tenant record or its directories: the
// request-time half of the declaration, which filters a session's tenant list. The
// sign-in half reads the same declaration in the sign-in's transaction, through the
// generated reader the staff auth's settings carry.
func (c *DataConfiguration) TenantDeclarations() resource.TenantDeclarations {
	return c.tenants.Declarations
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

	// StaffAdministratorPassword is the password of the staff auth's first administrator
	// (staff.Administrator), which the migrate command seeds while the auth's users table
	// holds no rows: a secret the stack generates, read like any other setting, that
	// nobody types and the first sign-in changes. Unset in development, where the
	// bootstrap creates the development logins instead.
	StaffAdministratorPassword string `env:"APP_STAFF_ADMINISTRATOR_PASSWORD" secret:"generated"`
	// The staff auth's google registration: how the application is known to Google. Under
	// the session library's skipAuth build tag (the Procfile's) Google is simulated and
	// only the redirect URL is read.
	StaffGoogleClientID     string `env:"APP_STAFF_GOOGLE_CLIENT_ID"`
	StaffGoogleClientSecret string `env:"APP_STAFF_GOOGLE_CLIENT_SECRET" secret:"true"`
	StaffGoogleRedirectURL  string `env:"APP_STAFF_GOOGLE_REDIRECT_URL"`
}

// staffSettings builds the staff auth's settings: the generated collection its default
// roles validate against, the live service its engine signals policy changes through, the
// cookie key its sessions are signed with, the session timeout, the sender its mail goes
// through with the login page its links open, the console's login page, where a refused
// or waiting directory sign-in returns to, the generated reader of a tenant's
// declaration, which the sign-in rule reads in the sign-in's transaction, and the google
// registration's settings, read from the environment under the registration's variables,
// so the constructor stays one call per auth.
func (env *dataConfig) staffSettings(collection access.PermissionCollection, signals staff.Signals, cookieKey string, sender mail.Sender) *staff.Settings {
	return &staff.Settings{
		Collection:         collection,
		Signals:            signals,
		CookieKey:          cookieKey,
		TenantDeclarations: app.NewTenantDeclarationReader(),
		TenantDirectories:  app.NewTenantDirectoryReader(),
		SessionTimeout:     env.SessionTimeout,
		Mail:               sender,
		MailLoginURL:       env.PublicURL + "/login",
		LoginURL:           "/login",
		Google: staff.GoogleSettings{
			ClientID:     env.StaffGoogleClientID,
			ClientSecret: env.StaffGoogleClientSecret,
			RedirectURL:  env.StaffGoogleRedirectURL,
		},
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
