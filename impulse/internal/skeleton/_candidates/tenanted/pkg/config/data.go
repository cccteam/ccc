package config

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	stderrors "errors"
	"fmt"
	"log"
	"slices"
	"time"

	cloudspanner "cloud.google.com/go/spanner"
	"github.com/cccteam/access"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/tenanted/pkg/auth/staff"
	"github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/tenanted/pkg/router"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/live"
	livefirestore "github.com/cccteam/ccc/resource/live/firestore"
	"github.com/go-playground/errors/v5"
	"github.com/sethvargo/go-envconfig"
	"google.golang.org/api/iterator"
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

// FirestoreSettings identifies the Firestore database the live pages run on, beside the
// Spanner database: the subscriptions the generated handlers register and the change
// sets the browser listens to. The database id is how a deployment hands the database to
// the application (APP_FIRESTORE_DATABASE); the emulator host is how the development
// stack does. Neither set, the application serves no live pages.
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

// Enabled reports whether live pages are served: a database is named, or the emulator
// is.
func (s FirestoreSettings) Enabled() bool {
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
// same host), Firebase's hosts in production, none when live pages are not served.
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
// owns the Spanner client, the resource client over it, the permission engine, the
// session manager, the tenant roster, and the live service when a Firestore database
// is configured.
type DataConfiguration struct {
	*coreConfiguration
	env            *dataConfig
	spannerClient  *cloudspanner.Client
	resourceClient *resource.SpannerClient
	cursorKey      *resource.CursorKey
	staff          *staff.Auth
	domains        []accesstypes.Domain
	domainSet      map[accesstypes.Domain]bool
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

	// The auth's default roles validate against the generated collection when its engine
	// opens; the collection is passed in here, since the auth package imports no router.
	staffAuth, err := staff.New(ctx, spannerClient, staff.Settings{Collection: router.Collection(), CookieKey: cookieKey, SessionTimeout: env.SessionTimeout})
	if err != nil {
		return nil, errors.Wrap(err, "staff.New()")
	}

	conf := &DataConfiguration{
		coreConfiguration: core,
		env:               env,
		spannerClient:     spannerClient,
		resourceClient:    resource.NewSpannerClient(spannerClient),
		cursorKey:         cursorKey,
		staff:             staffAuth,
	}
	if err := conf.loadDomains(ctx); err != nil {
		return nil, errors.Wrap(err, "loadDomains()")
	}
	if err := conf.openLive(ctx); err != nil {
		return nil, errors.Wrap(err, "openLive()")
	}

	return conf, nil
}

// openLive opens the live service over the Firestore database the configuration names,
// or the emulator; with neither the application serves no live pages and the service
// stays nil.
func (c *DataConfiguration) openLive(ctx context.Context) error {
	if !c.env.Firestore.Enabled() {
		return nil
	}

	project := c.env.Firestore.ProjectID
	if project == "" {
		project = c.env.Spanner.ProjectID
	}
	service, err := livefirestore.New(ctx, livefirestore.Config{
		ProjectID:    project,
		DatabaseID:   c.env.Firestore.DatabaseID,
		APIKey:       c.env.Firestore.APIKey,
		EmulatorHost: c.env.Firestore.EmulatorHost,
	})
	if err != nil {
		return errors.Wrap(err, "firestore.New()")
	}
	c.live = service

	return nil
}

// Close releases the level's clients, then the levels below it.
func (c *DataConfiguration) Close() {
	if c.live != nil {
		if err := c.live.Close(); err != nil {
			log.Print(errors.Wrap(err, "firestore.Service.Close()"))
		}
	}
	if err := c.staff.Close(); err != nil {
		log.Print(errors.Wrap(err, "staff.Auth.Close()"))
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

// Live returns the live service the generated handlers subscribe through and publish
// to: the Firestore service when a Firestore database or the emulator is configured,
// nil otherwise. A nil interface is what the App's accessor hands the generated code,
// never a typed nil.
func (c *DataConfiguration) Live() live.Service {
	if c.live == nil {
		return nil
	}

	return c.live
}

// LiveOrigins returns the origins the browser reaches the change feed at, for the
// content security policy: the Firestore emulator in development, Firebase's hosts in
// production, none when no live pages are served.
func (c *DataConfiguration) LiveOrigins() []string {
	return c.env.Firestore.BrowserOrigins()
}

// Domains lists the tenants as permission domains, from the roster read at startup.
func (c *DataConfiguration) Domains(_ context.Context) ([]accesstypes.Domain, error) {
	return c.domains, nil
}

// DomainVisible reports whether the domain is a known tenant AND the user holds at
// least one grant in it — existence from the startup roster, foothold from the
// permission engine's in-memory policy snapshot (no store read, so it is safe inside
// the consolidated handler's mutation transaction). Tenant existence is concealed: a
// caller with no foothold is answered exactly like the tenant does not exist.
func (c *DataConfiguration) DomainVisible(ctx context.Context, user accesstypes.User, domain accesstypes.Domain) (bool, error) {
	if !c.domainSet[domain] {
		return false, nil
	}

	visible, err := c.staff.Access().UserHasGrants(ctx, user, accesstypes.DomainScope(domain))
	if err != nil {
		return false, errors.Wrap(err, "access.Client.UserHasGrants()")
	}

	return visible, nil
}

// loadDomains reads the tenant roster from the Tenants table once at startup. The
// roster is cached rather than queried per check: the generated consolidated handler
// consults DomainVisible inside the mutation transaction, where opening another read
// is illegal on the emulator. A process restart picks up new tenants.
func (c *DataConfiguration) loadDomains(ctx context.Context) error {
	iter := c.spannerClient.Single().Query(ctx, cloudspanner.Statement{SQL: "SELECT Id FROM Tenants ORDER BY Id"})
	defer iter.Stop()

	c.domainSet = make(map[accesstypes.Domain]bool)
	for {
		row, err := iter.Next()
		if err != nil {
			if stderrors.Is(err, iterator.Done) {
				return nil
			}

			return errors.Wrap(err, "spanner.RowIterator.Next()")
		}

		var id string
		if err := row.Columns(&id); err != nil {
			return errors.Wrap(err, "spanner.Row.Columns()")
		}
		c.domains = append(c.domains, accesstypes.Domain(id))
		c.domainSet[accesstypes.Domain(id)] = true
	}
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
}
