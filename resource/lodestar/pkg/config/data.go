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

	// The one mail driver both auths send through: the invitations the user-management
	// surfaces mail, the verification of an address a person adds, and the crew's
	// sign-up links. Every link leads to the login page of the surface the auth binds
	// to, at the application's public address.
	//
	// Demonstrates: auth.invitation.
	sender, err := openMailSender(env)
	if err != nil {
		return nil, err
	}

	crewAuth, err := openCrewAuth(ctx, db, env, cookieKey, policySignal, sender)
	if err != nil {
		return nil, err
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

	membersAuth, err := openMembersAuth(ctx, db, env, cookieKey, policySignal, sender)
	if err != nil {
		return nil, err
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

// CrewUserSurface returns the crew auth's user-management surface: the crew session's
// accounts (sessionusers.Accounts over its API) and the crew engine's memberships, which
// the generated crew user handlers serve through. A seat written here is a policy write
// the engine announces to every instance through the live service. The surface reads
// each sector's declaration from the roster's copy, so a sector's marshal may seat a
// person directly, without an invitation, when the person entered through a directory
// the sector declares (resource.WithTenantDirectories), and knows the crew's
// administrator roles (resource.WithAdministratorRoles), so a self-unseat is judged by
// the last-owner rule: a marshal leaves a sector once another marshal holds it.
func (c *DataConfiguration) CrewUserSurface() *resource.UserSurface[router.CrewAuth] {
	return router.NewCrewUserSurface(sessionusers.Accounts(c.crew.Session().API()), c.crew.Access().UserManager(), resource.WithTenantDirectories(c.tenants.Declarations), resource.WithAdministratorRoles(crew.AdministratorRoles()))
}

// MembersUserSurface returns the members auth's surface over the members session's
// accounts and the members engine's memberships, served on the portal and, by the
// generator program's Manages, on the console, with the same directory-vouched direct
// seat the crew's offers.
func (c *DataConfiguration) MembersUserSurface() *resource.UserSurface[router.MembersAuth] {
	return router.NewMembersUserSurface(sessionusers.Accounts(c.members.Session().API()), c.members.Access().UserManager(), resource.WithTenantDirectories(c.tenants.Declarations))
}

// openCrewAuth opens the crew auth: the console's people, whose default roles ride in
// the binary and validate against the generated collection, which the router package
// generates and the auth package cannot import. They sign in with a password or through
// the fleet's Google directory, whose refused logins return to the console's login
// page, the auth mails its links to that page at the application's public address, and
// a command-line client asking for the droids outlet is sent to the console's device
// page for a crew member's approval.
// The auth opens its stores on the driver's Spanner client: the one vendor-specific
// seam left in the level, pending the PostgreSQL client (cccteam/ccc#852).
func openCrewAuth(ctx context.Context, db *database.Driver, env *dataConfig, cookieKey string, policySignal access.ChangeSignal, sender mail.Sender) (*crew.Auth, error) {
	crewAuth, err := crew.New(ctx, db.SpannerClient, &crew.Settings{
		CookieKey:      cookieKey,
		SessionTimeout: env.SessionTimeout,
		LoginURL:       "/console/login",
		Collection:     router.Collection(),
		Directory: auth.Directory{
			ClientID:     env.CrewClientID,
			ClientSecret: env.CrewClientSecret,
			RedirectURL:  env.CrewRedirectURL,
			Connection:   env.CrewConnection,
			HostedDomain: env.CrewHostedDomain,
		},
		SignUpRedirectURL: env.CrewSignUpRedirectURL,
		DevicePage:        "/console/device",
		ChangeSignal:      policySignal,
		// The sign-in rule reads what each sector declares about who may enter it in
		// the sign-in's transaction, through the generated reader; the directory
		// admission reads what the sectors declare about one directory, with its
		// default seat, through the other.
		TenantDeclarations: app.NewSectorDeclarationReader(),
		TenantDirectories:  app.NewSectorDirectoryReader(),
		Mail:               auth.Mail{Sender: sender, LoginURL: env.PublicURL + "/console/login"},
	})
	if err != nil {
		return nil, errors.Wrap(err, "crew.New()")
	}

	return crewAuth, nil
}

// openMembersAuth opens the members auth: the portal's people, whose roles are the
// directory's. Its role synchronization writes a global role in the global partition
// and a domain role in every sector, so a client's membership reaches each sector with
// one row; the portal's grants then narrow by company. The portal's login page is where
// a refused directory login returns to and where the auth's mailed links lead.
func openMembersAuth(ctx context.Context, db *database.Driver, env *dataConfig, cookieKey string, policySignal access.ChangeSignal, sender mail.Sender) (*members.Auth, error) {
	membersAuth, err := members.New(ctx, db.SpannerClient, &members.Settings{
		CookieKey:      cookieKey,
		SessionTimeout: env.SessionTimeout,
		LoginURL:       "/portal/login",
		Collection:     router.Collection(),
		Directory: auth.Directory{
			ClientID:     env.MembersClientID,
			ClientSecret: env.MembersClientSecret,
			RedirectURL:  env.MembersRedirectURL,
			Connection:   env.MembersConnection,
			HostedDomain: env.MembersHostedDomain,
			GroupPrefix:  env.MembersGroupPrefix,
			GroupLookup:  env.MembersGroupLookup,
		},
		ChangeSignal:       policySignal,
		TenantDeclarations: app.NewSectorDeclarationReader(),
		Mail:               auth.Mail{Sender: sender, LoginURL: env.PublicURL + "/portal/login"},
	})
	if err != nil {
		return nil, errors.Wrap(err, "members.New()")
	}

	return membersAuth, nil
}

// openMailSender opens the mail driver the auths send through: the Google Workspace
// SMTP relay as the configured mailbox with its app password (APP_MAIL_MAILBOX,
// APP_MAIL_APP_PASSWORD), or, with no mailbox configured, the session library's
// logging stand-in, which writes each message to the log, link included, and sends
// nothing: development follows the link from the log. A deployed environment configures
// the mailbox, since the links are secrets.
func openMailSender(env *dataConfig) (mail.Sender, error) {
	if env.MailMailbox == "" {
		return mail.Log{}, nil
	}
	sender, err := workspace.New(&workspace.Settings{Mailbox: env.MailMailbox, From: env.MailFrom, AppPassword: env.MailAppPassword, Name: crew.Application})
	if err != nil {
		return nil, errors.Wrap(err, "workspace.New()")
	}

	return sender, nil
}

// TenantRoster returns the tenant roster: the sectors this instance serves, loaded when
// the level opened and kept current until the context the level opened under ends.
func (c *DataConfiguration) TenantRoster() *resource.TenantRoster {
	return c.tenants
}

// TenantDeclarations reads what a sector declares about who may enter it from the
// roster's copy, reloaded on every write of the Sector record or its directories: the
// request-time half of the declaration, which filters a session's chart. The sign-in
// half reads the same declaration in the sign-in's transaction, through the generated
// reader each auth's settings carry.
//
// Demonstrates: tenancy.declaration.
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
	// Each auth's Google registration (pkg/auth/crew, pkg/auth/members): the application's
	// client credentials, the callback Google returns the browser to, the Workspace domain
	// logins are restricted to, the prefix of the Google Groups that carry roles, and how
	// far the groups lookup reaches: direct (the default), or nested for a directory that
	// nests its role groups. Under the session library's skipAuth build tag only the
	// redirect URL, the connection, the hosted domain, and the group prefix are read.
	CrewClientID     string `env:"APP_CREW_OIDC_CLIENT_ID"`
	CrewClientSecret string `env:"APP_CREW_OIDC_CLIENT_SECRET"`
	CrewRedirectURL  string `env:"APP_CREW_OIDC_REDIRECT_URL"`
	CrewConnection   string `env:"APP_CREW_OIDC_CONNECTION"`
	CrewHostedDomain string `env:"APP_CREW_OIDC_HOSTED_DOMAIN"`
	// The crew auth's sign-up registration (any Google account, open sign-up): its own
	// callback under the console's API, beside the directory's; the client is the
	// directory's. Under the skipAuth tag the registration's simulated person is
	// APP_SIGN_UP_USERNAME.
	CrewSignUpRedirectURL string `env:"APP_CREW_SIGN_UP_REDIRECT_URL"`
	MembersClientID       string `env:"APP_MEMBERS_OIDC_CLIENT_ID"`
	MembersClientSecret   string `env:"APP_MEMBERS_OIDC_CLIENT_SECRET"`
	MembersRedirectURL    string `env:"APP_MEMBERS_OIDC_REDIRECT_URL"`
	MembersConnection     string `env:"APP_MEMBERS_OIDC_CONNECTION"`
	MembersHostedDomain   string `env:"APP_MEMBERS_OIDC_HOSTED_DOMAIN"`
	MembersGroupPrefix    string `env:"APP_MEMBERS_OIDC_GROUP_PREFIX"`
	MembersGroupLookup    string `env:"APP_MEMBERS_OIDC_GROUP_LOOKUP"`

	// PublicURL is the application's address as a browser reaches it, scheme and host
	// with no path (https://lodestar.example.com; the dev proxy in development): every
	// link the auths mail leads to a login page under it. Required, since a mailed link
	// is absolute.
	PublicURL string `env:"APP_PUBLIC_URL,required"`
	// The mail driver: the Google Workspace mailbox the application sends as, through
	// the Workspace SMTP relay, the address the mail is from when it is not the mailbox
	// (an alias of its account), and the mailbox's app password, a secret the deployment
	// reads from its store. Unset, the session library's logging stand-in writes each
	// message to the log instead of sending it, for development alone.
	MailMailbox     string `env:"APP_MAIL_MAILBOX"`
	MailFrom        string `env:"APP_MAIL_FROM"`
	MailAppPassword string `env:"APP_MAIL_APP_PASSWORD" secret:"true"`

	// FileStores are the file stores' URLs.
	FileStores FileStoreSettings
}
