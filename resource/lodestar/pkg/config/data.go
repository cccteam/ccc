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
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/live"
	livefirestore "github.com/cccteam/ccc/resource/live/firestore"
	"github.com/cccteam/ccc/resource/lodestar/app"
	"github.com/cccteam/ccc/resource/lodestar/pkg/auth"
	"github.com/cccteam/ccc/resource/lodestar/pkg/auth/crew"
	"github.com/cccteam/ccc/resource/lodestar/pkg/auth/members"
	"github.com/cccteam/ccc/resource/lodestar/pkg/router"
	"github.com/cccteam/ccc/resource/lodestar/pkg/store"
	"github.com/cccteam/httpio"
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
// the Spanner database: the live pages' subscription record and change sets, and the
// signals document the instances notify each other through. The database id is how
// bedrock hands the database to an application (APP_FIRESTORE_DATABASE); the emulator
// host is how the development stack does. One of the two is required: every application
// wires the live service, and the data level refuses to start without a database for it.
type FirestoreSettings struct {
	// ProjectID is the Google Cloud project the database belongs to. Empty, the Spanner
	// project is used: the database lives beside the Spanner database in the
	// application's project unless this says otherwise.
	ProjectID string `env:"GOOGLE_CLOUD_FIRESTORE_PROJECT"`
	// DatabaseID is the Firestore database, by id.
	DatabaseID string `env:"APP_FIRESTORE_DATABASE"`
	// APIKey is the Firebase web API key the browser initializes the SDK with; unused
	// against the emulator.
	APIKey string `env:"APP_FIREBASE_API_KEY"`
	// EmulatorHost is the Firestore emulator's host:port, the development stack's.
	EmulatorHost string `env:"FIRESTORE_EMULATOR_HOST"`
}

// Configured reports whether the live service has a database: one is named, or the
// emulator is.
func (s FirestoreSettings) Configured() bool {
	return s.DatabaseID != "" || s.EmulatorHost != ""
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
	if s.EmulatorHost != "" {
		return []string{"http://" + s.EmulatorHost}
	}

	return slices.Clone(firebaseOrigins)
}

// DataConfiguration is the second level: every process that opens the database. It
// owns the Spanner client, the document store, the resource client over both, the live
// service over the Firestore database, the tenant roster (the Sectors table's keys,
// loaded here and kept current through the live service's tenants signal), and the two
// auths (each its permission engine and session manager), whose engines announce and
// watch policy changes through the live service.
type DataConfiguration struct {
	*coreConfiguration
	env            *dataConfig
	spannerClient  *cloudspanner.Client
	documents      *store.DirStore
	resourceClient *resource.SpannerClient
	cursorKey      *resource.CursorKey
	crew           *crew.Auth
	tenants        *resource.TenantRoster
	members        *members.Auth
	live           *livefirestore.Service
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

	// The live service: the Firestore database the live pages' subscriptions and change
	// sets live in, beside the Spanner database, and the signals document every instance
	// holds one listener on. Every application wires one: the generated handlers
	// subscribe and publish through it, the feature flags follow it, and both permission
	// engines announce and watch the policy kind through it, so it opens before the
	// auths, and a configuration naming no database for it does not start.
	if !env.Firestore.Configured() {
		return nil, errors.New("the live service needs a Firestore database: set APP_FIRESTORE_DATABASE (the database id) or FIRESTORE_EMULATOR_HOST (the emulator)")
	}
	project := env.Firestore.ProjectID
	if project == "" {
		project = env.Spanner.ProjectID
	}
	liveService, err := livefirestore.New(ctx, livefirestore.Config{
		ProjectID:    project,
		DatabaseID:   env.Firestore.DatabaseID,
		APIKey:       env.Firestore.APIKey,
		EmulatorHost: env.Firestore.EmulatorHost,
	})
	if err != nil {
		return nil, errors.Wrap(err, "firestore.New()")
	}

	// The one change signal both engines take: a policy write on any instance reaches
	// every engine on every instance through the policy kind of the signals document.
	policySignal := auth.PolicySignal(liveService)

	// The crew auth: the console's people, whose default roles ride in the binary and
	// validate against the generated collection, which the router package generates and
	// the auth package cannot import.
	crewAuth, err := crew.New(ctx, spannerClient, crew.Settings{
		CookieKey:      cookieKey,
		SessionTimeout: env.SessionTimeout,
		Collection:     router.Collection(),
		ChangeSignal:   policySignal,
	})
	if err != nil {
		return nil, errors.Wrap(err, "crew.New()")
	}

	// The document store belongs beside the database: the resource client is
	// constructed over both, so a transaction that deletes a document, or points one at
	// another file, has the old object deleted from the store once the commit lands.
	//
	// Demonstrates: @file.released.
	documents, err := store.NewDirStore(env.UploadDir)
	if err != nil {
		return nil, errors.Wrap(err, "store.NewDirStore()")
	}

	resourceClient := resource.NewSpannerClient(spannerClient, resource.WithFileStore(documents))

	// The tenant roster: every instance's copy of the Sectors table's keys, built by the
	// generated constructor (Sector is the @tenant record) and started here beside the
	// engines, so the start fails when the first read does. It subscribes to the tenants
	// kind on the live service, the one channel the engines and the feature flags ride:
	// a sector charted on any instance is served by this one at its next request, with
	// the library's five-minute reread as the backstop; no restart, no migrate job and no
	// new login is needed, since a role held in every sector reaches the new one with
	// nothing written. Tenancy is data, not a compiled-in list.
	//
	// Demonstrates: tenancy.run-time-tenant.
	tenants := app.NewSectorRoster(resourceClient, resource.WithTenantSignals(liveService))
	if err := tenants.Start(ctx); err != nil {
		return nil, errors.Wrap(err, "resource.TenantRoster.Start()")
	}

	conf := &DataConfiguration{
		coreConfiguration: core,
		env:               env,
		spannerClient:     spannerClient,
		documents:         documents,
		resourceClient:    resourceClient,
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
	membersAuth, err := members.New(ctx, spannerClient, &members.Settings{
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
// and watch through, then the levels below it.
func (c *DataConfiguration) Close() {
	if err := c.crew.Close(); err != nil {
		log.Print(errors.Wrap(err, "crew.Auth.Close()"))
	}
	if err := c.members.Close(); err != nil {
		log.Print(errors.Wrap(err, "members.Auth.Close()"))
	}
	if err := c.live.Close(); err != nil {
		log.Print(errors.Wrap(err, "firestore.Service.Close()"))
	}
	if err := c.documents.Close(); err != nil {
		log.Print(errors.Wrap(err, "store.DirStore.Close()"))
	}
	c.spannerClient.Close()
	c.coreConfiguration.Close()
}

// Spanner returns the database identity.
func (c *DataConfiguration) Spanner() SpannerSettings {
	return c.env.Spanner
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
	return c.env.Firestore.BrowserOrigins()
}

// ResourceClient returns the database client the resource layer uses, constructed over
// the document store so a committed transaction's released objects are deleted from it.
func (c *DataConfiguration) ResourceClient() resource.Client {
	return c.resourceClient
}

// Documents returns the store the upload frames stream mission documents into, the
// document route reads them back from, and the resource client releases them from.
func (c *DataConfiguration) Documents() *store.DirStore {
	return c.documents
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

// dataConfig holds the environment every database-opening process reads.
type dataConfig struct {
	Spanner   SpannerSettings
	Firestore FirestoreSettings

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

	// UploadDir is the directory the document store keeps mission documents in: the
	// upload frames stream into it, the file route reads from it, and the resource
	// client deletes a released object from it after the commit that released it.
	UploadDir string `env:"APP_UPLOAD_DIR,default=uploads"`
}
