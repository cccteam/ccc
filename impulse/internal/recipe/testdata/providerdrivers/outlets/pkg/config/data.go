package config

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log"
	"slices"
	"time"

	cloudspanner "cloud.google.com/go/spanner"
	"github.com/cccteam/access"
	"github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/outlets/app"
	"github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/outlets/pkg/auth/members"
	"github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/outlets/pkg/auth/staff"
	"github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/outlets/pkg/router"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/live"
	livefirestore "github.com/cccteam/ccc/resource/live/firestore"
	"github.com/cccteam/ccc/resource/sessionusers"
	"github.com/cccteam/session/mail"
	"github.com/cccteam/session/mail/workspace"
	"github.com/go-playground/errors/v5"
	"github.com/sethvargo/go-envconfig"
)

// SpannerSettings identifies the application's database. It is the first half of the
// data level, loadable on its own so cmd/bootstrap can create the instance and database
// before any client opens against them.
type SpannerSettings struct {
	ProjectID    string `env:"GOOGLE_CLOUD_SPANNER_PROJECT,required"`
	InstanceID   string `env:"GOOGLE_CLOUD_SPANNER_INSTANCE_ID,required"`
	DatabaseName string `env:"GOOGLE_CLOUD_SPANNER_DATABASE_NAME,required"`
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

// DatabasePath returns the fully qualified Spanner database path.
func (s SpannerSettings) DatabasePath() string {
	return fmt.Sprintf("projects/%s/instances/%s/databases/%s", s.ProjectID, s.InstanceID, s.DatabaseName)
}

// FirestoreSettings identifies the Firestore database the live service runs on, beside
// the Spanner database: the subscriptions the generated handlers register, the change
// sets the browser listens to, and the one signals document the application's instances
// tell each other through (a feature flag flip, a policy write). The database id is how
// a deployment hands the database to the application (APP_FIRESTORE_DATABASE), with its
// project (GOOGLE_CLOUD_FIRESTORE_PROJECT); the emulator host is how the development
// stack does. The live service is required, so with neither the database nor the
// emulator set the data level refuses to start.
type FirestoreSettings struct {
	// ProjectID is the Google Cloud project the database belongs to. A deployment that
	// names the database names its project too, or the data level refuses to start: the
	// database is not assumed to be in the Spanner project, which, where environments
	// share a Spanner instance, is the shared instance's and not the environment's.
	// Against the emulator it may stay empty, and the Spanner project stands in, since
	// the emulator takes any project id.
	ProjectID string `env:"GOOGLE_CLOUD_FIRESTORE_PROJECT"`
	// DatabaseID is the Firestore database, by id.
	DatabaseID string `env:"APP_FIRESTORE_DATABASE"`
	// APIKey is the Firebase web API key the browser initializes the SDK with; unused
	// against the emulator.
	APIKey string `env:"APP_FIREBASE_API_KEY"`
	// EmulatorHost is the Firestore emulator's host:port, the development stack's.
	EmulatorHost string `env:"FIRESTORE_EMULATOR_HOST"`
}

// Configured reports whether the live service has a database to open: a database is
// named, or the emulator is.
func (s FirestoreSettings) Configured() bool {
	return s.DatabaseID != "" || s.EmulatorHost != ""
}

// Project is the project the live service opens the database in: ProjectID, or, against
// the emulator with ProjectID empty, the Spanner project, which the emulator takes as it
// takes any project id. A database named without its project is refused, naming both
// variables, as is a configuration naming neither a database nor the emulator, since the
// live service is required.
func (s FirestoreSettings) Project(spannerProject string) (string, error) {
	switch {
	case !s.Configured():
		return "", errors.New("no Firestore database and no emulator is configured: set APP_FIRESTORE_DATABASE (a deployment) or FIRESTORE_EMULATOR_HOST (development), since the live service is required")
	case s.ProjectID != "":
		return s.ProjectID, nil
	case s.EmulatorHost != "":
		return spannerProject, nil
	default:
		return "", errors.New("APP_FIRESTORE_DATABASE names a Firestore database and GOOGLE_CLOUD_FIRESTORE_PROJECT names no project for it: set GOOGLE_CLOUD_FIRESTORE_PROJECT to the project the database is in, which is not assumed to be the Spanner project")
	}
}

// firebaseOrigins are the hosts the Firebase JS SDK reaches in production: Firestore's
// endpoint, which the change feed listens through, and Firebase Auth's two, which the
// custom token is signed in through and refreshed at.
var firebaseOrigins = []string{
	"https://firestore.googleapis.com",
	"https://identitytoolkit.googleapis.com",
	"https://securetoken.googleapis.com",
}

// BrowserOrigins returns the origins the browser connects to for the change feed, which
// the content security policy's connect-src must name beside the application itself:
// the emulator over plain HTTP in development (the token route hands the browser the
// same host), Firebase's hosts in production.
func (s FirestoreSettings) BrowserOrigins() []string {
	switch {
	case s.EmulatorHost != "":
		return []string{"http://" + s.EmulatorHost}
	case s.DatabaseID != "":
		return slices.Clone(firebaseOrigins)
	default:
		return nil
	}
}

// DataConfiguration is the second level: every process that opens the database. It
// owns the Spanner client, the resource client over it, the live service over the
// Firestore database or the emulator, the two auths (the staff auth the console binds to
// and the members auth the portal binds to, each its own permission engine signaling
// policy changes through the live service, and its own session manager), and the tenant
// roster.
type DataConfiguration struct {
	*coreConfiguration
	env            *dataConfig
	spannerClient  *cloudspanner.Client
	resourceClient *resource.SpannerClient
	cursorKey      *resource.CursorKey
	staff          *staff.Auth
	members        *members.Auth
	tenants        *resource.TenantRoster
	live           *livefirestore.Service
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

	spannerClient, err := cloudspanner.NewClient(ctx, env.Spanner.DatabasePath())
	if err != nil {
		return nil, errors.Wrap(err, "spanner.NewClient()")
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
	// through it.
	liveService, err := openLive(ctx, env)
	if err != nil {
		return nil, err
	}

	// The sender every auth mails through: the Workspace relay where the deployment names
	// a mailbox, the log in development.
	sender, err := openMailSender(env)
	if err != nil {
		return nil, err
	}
	// The auth's default roles validate against the generated collection when its engine
	// opens; the collection is passed in here, since the auth package imports no router.
	staffAuth, err := staff.New(ctx, spannerClient, env.staffSettings(router.Collection(), liveService, cookieKey, sender))
	if err != nil {
		return nil, errors.Wrap(err, "staff.New()")
	}

	membersAuth, err := members.New(ctx, spannerClient, env.membersSettings(router.Collection(), liveService, cookieKey, sender))
	if err != nil {
		return nil, errors.Wrap(err, "members.New()")
	}

	conf := &DataConfiguration{
		coreConfiguration: core,
		env:               env,
		spannerClient:     spannerClient,
		resourceClient:    resource.NewSpannerClient(spannerClient),
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

// openLive opens the live service over the Firestore database the configuration names,
// or the emulator. The live service is required: the generated handlers serve the live
// pages through it, and the application's instances signal each other through its
// signals document (a feature flag flip, a policy write in either auth's store), so a
// configuration naming neither, or naming the database without its project, fails the
// start here. One service serves both browser
// outlets: a subscription is the principal's, and the token route answers the session
// principal's id whichever auth signed it in.
func openLive(ctx context.Context, env *dataConfig) (*livefirestore.Service, error) {
	project, err := env.Firestore.Project(env.Spanner.ProjectID)
	if err != nil {
		return nil, err
	}
	service, err := livefirestore.New(ctx, livefirestore.Config{
		ProjectID:    project,
		DatabaseID:   env.Firestore.DatabaseID,
		APIKey:       env.Firestore.APIKey,
		EmulatorHost: env.Firestore.EmulatorHost,
	})
	if err != nil {
		return nil, errors.Wrap(err, "firestore.New()")
	}

	return service, nil
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
		log.Print(errors.Wrap(err, "firestore.Service.Close()"))
	}
	c.spannerClient.Close()
	c.coreConfiguration.Close()
}

// Spanner returns the database identity.
func (c *DataConfiguration) Spanner() SpannerSettings {
	return c.env.Spanner
}

// ResourceClient returns the database client the resource layer uses.
func (c *DataConfiguration) ResourceClient() resource.Client {
	return c.resourceClient
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

// Members returns the members auth: the one the portal binds to.
func (c *DataConfiguration) Members() *members.Auth {
	return c.members
}

// MembersUserStores returns the stores the members auth's user-management surface serves over:
// the members session's accounts (sessionusers.Accounts over its API) and the members
// engine's memberships. A seat written through the surface is a policy write the engine
// announces to every instance.
func (c *DataConfiguration) MembersUserStores() (resource.UserAccounts, resource.UserSeats) {
	return sessionusers.Accounts(c.members.Session().API()), c.members.Access().UserManager()
}

// MembersAdministrator returns the sign-in name the members auth's first administrator is
// seeded under, which the placement names; empty where the deployment names none.
func (c *DataConfiguration) MembersAdministrator() string {
	return c.env.MembersAdministrator
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
	c.tenants = app.NewTenantRoster(c.resourceClient, resource.WithTenantSignals(c.live))
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
// generated reader each auth's settings carry.
func (c *DataConfiguration) TenantDeclarations() resource.TenantDeclarations {
	return c.tenants.Declarations
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
	Spanner   SpannerSettings
	Firestore FirestoreSettings

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

	// The members auth's directory registration (pkg/auth/members, user/directory/login):
	// the tenant whose accounts it admits, the app registration's client credentials, and
	// the callback the directory returns the browser to, under the registration's
	// variables (APP_<AUTH>_<REGISTRATION>_*). Under the session library's skipAuth build
	// tag the directory is simulated and only the redirect URL is read.
	MembersDirectoryTenantID     string `env:"APP_MEMBERS_DIRECTORY_TENANT_ID"`
	MembersDirectoryClientID     string `env:"APP_MEMBERS_DIRECTORY_CLIENT_ID"`
	MembersDirectoryClientSecret string `env:"APP_MEMBERS_DIRECTORY_CLIENT_SECRET" secret:"true"`
	MembersDirectoryRedirectURL  string `env:"APP_MEMBERS_DIRECTORY_REDIRECT_URL"`

	// MembersAdministrator is the sign-in name of the members auth's first administrator,
	// as the directory presents it (the user principal name): the migrate command seeds
	// the administrator role memberships under it while the auth's users table holds no
	// rows, so the first person to sign in holds them. Named per environment by the
	// placement; unset in development, where the bootstrap seeds the development members.
	MembersAdministrator string `env:"APP_MEMBERS_ADMINISTRATOR"`
}

// staffSettings builds the staff auth's settings: the generated collection its default
// roles validate against, the live service its engine signals policy changes through, the
// cookie key its sessions are signed with, the session timeout, the sender its mail goes
// through with the login page its links open, and the generated reader of a tenant's
// declaration, which the sign-in rule reads in the sign-in's transaction. A directory
// registration's settings join the literal here (impulse add method), read from the
// environment under the registration's variables, so the constructor stays one call per
// auth.
func (env *dataConfig) staffSettings(collection access.PermissionCollection, signals staff.Signals, cookieKey string, sender mail.Sender) *staff.Settings {
	return &staff.Settings{
		Collection:         collection,
		Signals:            signals,
		CookieKey:          cookieKey,
		SessionTimeout:     env.SessionTimeout,
		Mail:               sender,
		MailLoginURL:       env.PublicURL + "/console/login",
		TenantDeclarations: app.NewTenantDeclarationReader(),
	}
}

// membersSettings builds the members auth's settings: the generated collection, the live
// service, the cookie key, the session timeout and the sender its mail goes through as
// the staff auth's, the portal's login page, where a refused directory sign-in returns to
// (the data level knows it because the auth's error redirects are configured here), and
// the directory registration's settings, read from the environment under the
// registration's variables.
func (env *dataConfig) membersSettings(collection access.PermissionCollection, signals members.Signals, cookieKey string, sender mail.Sender) *members.Settings {
	return &members.Settings{
		Collection:     collection,
		Signals:        signals,
		CookieKey:      cookieKey,
		SessionTimeout: env.SessionTimeout,
		Mail:           sender,
		MailLoginURL:   env.PublicURL + "/portal/login",
		LoginURL:       "/portal/login",
		Directory: members.DirectorySettings{
			TenantID:     env.MembersDirectoryTenantID,
			ClientID:     env.MembersDirectoryClientID,
			ClientSecret: env.MembersDirectoryClientSecret,
			RedirectURL:  env.MembersDirectoryRedirectURL,
		},
		TenantDeclarations: app.NewTenantDeclarationReader(),
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
