package config

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	stderrors "errors"
	"fmt"
	"log"
	"time"

	cloudspanner "cloud.google.com/go/spanner"
	"github.com/cccteam/access"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/outlets/pkg/auth"
	"github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/outlets/pkg/auth/members"
	"github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/outlets/pkg/auth/staff"
	"github.com/cccteam/ccc/resource"
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

// DataConfiguration is the second level: every process that opens the database. It
// owns the Spanner client, the resource client over it, the two auths (the staff auth the
// console binds to and the members auth the portal binds to, each its own permission
// engine and session manager), and the tenant roster.
type DataConfiguration struct {
	*coreConfiguration
	env            *dataConfig
	spannerClient  *cloudspanner.Client
	resourceClient *resource.SpannerClient
	cursorKey      *resource.CursorKey
	staff          *staff.Auth
	members        *members.Auth
	domains        []accesstypes.Domain
	domainSet      map[accesstypes.Domain]bool
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

	staffAuth, err := staff.New(ctx, spannerClient, staff.Settings{CookieKey: cookieKey, SessionTimeout: env.SessionTimeout})
	if err != nil {
		return nil, errors.Wrap(err, "staff.New()")
	}

	// The portal's login page is where a refused directory login returns to; the data
	// level knows it because the auth's error redirects are configured here.
	membersAuth, err := members.New(ctx, spannerClient, &members.Settings{
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
		spannerClient:     spannerClient,
		resourceClient:    resource.NewSpannerClient(spannerClient),
		cursorKey:         cursorKey,
		staff:             staffAuth,
		members:           membersAuth,
	}
	if err := conf.loadDomains(ctx); err != nil {
		return nil, errors.Wrap(err, "loadDomains()")
	}

	return conf, nil
}

// Close releases the level's clients, then the levels below it.
func (c *DataConfiguration) Close() {
	if err := c.staff.Close(); err != nil {
		log.Print(errors.Wrap(err, "staff.Auth.Close()"))
	}
	if err := c.members.Close(); err != nil {
		log.Print(errors.Wrap(err, "members.Auth.Close()"))
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

// engine returns the permission engine of the auth the request came through: the
// members auth's for a request its session group bound, the staff auth's otherwise.
func (c *DataConfiguration) engine(ctx context.Context) *access.Client {
	if auth.Name(ctx) == members.Name {
		return c.members.Access()
	}

	return c.staff.Access()
}

// Members returns the members auth: the one the portal binds to.
func (c *DataConfiguration) Members() *members.Auth {
	return c.members
}

// Domains lists the tenants as permission domains, from the roster read at startup.
func (c *DataConfiguration) Domains(_ context.Context) ([]accesstypes.Domain, error) {
	return c.domains, nil
}

// DomainVisible reports whether the domain is a known tenant AND the user holds at
// least one grant in it — existence from the startup roster, foothold from the
// permission engine's in-memory policy snapshot (no store read, so it is safe inside
// the consolidated handler's mutation transaction). The engine is the one of the auth
// the request came through (auth.Name): a member's foothold is in the members store.
// Tenant existence is concealed: a caller with no foothold is answered exactly like the
// tenant does not exist.
func (c *DataConfiguration) DomainVisible(ctx context.Context, user accesstypes.User, domain accesstypes.Domain) (bool, error) {
	if !c.domainSet[domain] {
		return false, nil
	}

	visible, err := c.engine(ctx).UserHasGrants(ctx, user, accesstypes.DomainScope(domain))
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
	Spanner SpannerSettings

	// SessionTimeout is the idle timeout of a browser session.
	SessionTimeout time.Duration `env:"APP_DEFAULT_SESSION_TIMEOUT,default=10m"`

	// CookieKey signs session cookies: a Base64-encoded string of at least 32 bytes of
	// cryptographically secure random data. Unset, an ephemeral key is generated at
	// startup.
	CookieKey string `env:"APP_COOKIE_KEY"`

	// The members auth's directory registration (pkg/auth/members): the OpenID Connect
	// issuer, the application's client credentials, and the callback the directory
	// returns the browser to. Under the session library's skipAuth build tag the
	// directory is simulated and only the redirect URL is read.
	MembersIssuerURL    string `env:"APP_MEMBERS_OIDC_ISSUER_URL"`
	MembersClientID     string `env:"APP_MEMBERS_OIDC_CLIENT_ID"`
	MembersClientSecret string `env:"APP_MEMBERS_OIDC_CLIENT_SECRET"`
	MembersRedirectURL  string `env:"APP_MEMBERS_OIDC_REDIRECT_URL"`
}
