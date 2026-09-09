package config

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log"
	"time"

	cloudspanner "cloud.google.com/go/spanner"
	"github.com/cccteam/access"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/lodestar/pkg/auth"
	"github.com/cccteam/ccc/resource/lodestar/pkg/auth/crew"
	"github.com/cccteam/ccc/resource/lodestar/pkg/auth/members"
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

// DataConfiguration is the second level: every process that opens the database. It
// owns the Spanner client, the resource client over it, and the crew auth (its
// permission engine and session manager).
type DataConfiguration struct {
	*coreConfiguration
	env            *dataConfig
	spannerClient  *cloudspanner.Client
	resourceClient *resource.SpannerClient
	cursorKey      *resource.CursorKey
	crew           *crew.Auth
	tenants        tenantRoster
	members        *members.Auth
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

	crewAuth, err := crew.New(ctx, spannerClient, crew.Settings{CookieKey: cookieKey, SessionTimeout: env.SessionTimeout})
	if err != nil {
		return nil, errors.Wrap(err, "crew.New()")
	}

	conf := &DataConfiguration{
		coreConfiguration: core,
		env:               env,
		spannerClient:     spannerClient,
		resourceClient:    resource.NewSpannerClient(spannerClient),
		cursorKey:         cursorKey,
		crew:              crewAuth,
	}
	if err := conf.loadTenants(ctx); err != nil {
		return nil, errors.Wrap(err, "loadTenants()")
	}

	// The members auth: the portal's people, whose roles are the directory's. Its role
	// synchronization sweeps the global scope and every sector of the roster, so a
	// client's roles land in each sector; the portal's grants then narrow by company.
	// The portal's login page is where a refused directory login returns to.
	membersAuth, err := members.New(ctx, spannerClient, &members.Settings{
		CookieKey:      cookieKey,
		SessionTimeout: env.SessionTimeout,
		LoginURL:       "/portal/login",
		Domains:        conf.Domains,
		Directory: members.Directory{
			ClientID:         env.MembersClientID,
			ClientSecret:     env.MembersClientSecret,
			RedirectURL:      env.MembersRedirectURL,
			HostedDomain:     env.MembersHostedDomain,
			GroupPrefix:      env.MembersGroupPrefix,
			AdminCredentials: env.MembersAdminCredentials,
			AdminSubject:     env.MembersAdminSubject,
		},
	})
	if err != nil {
		return nil, errors.Wrap(err, "members.New()")
	}
	conf.members = membersAuth

	return conf, nil
}

// Close releases the level's clients, then the levels below it.
func (c *DataConfiguration) Close() {
	if err := c.crew.Close(); err != nil {
		log.Print(errors.Wrap(err, "crew.Auth.Close()"))
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

// engine returns the permission engine of the auth the request came through: the members
// auth's for a request its session group bound, the crew auth's otherwise.
func (c *DataConfiguration) engine(ctx context.Context) *access.Client {
	if auth.Name(ctx) == members.Name {
		return c.members.Access()
	}

	return c.crew.Access()
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
	// The members auth's directory registration (pkg/auth/members): the application's client
	// credentials, the callback Google returns the browser to, the Workspace domain logins are
	// restricted to, the prefix of the Google Groups that carry roles, and the Admin SDK service
	// account (a key with domain-wide delegation, and the admin it impersonates) that reads them.
	// Under the session library's skipAuth build tag only the redirect URL, the hosted domain, and the group prefix are read.
	MembersClientID         string `env:"APP_MEMBERS_OIDC_CLIENT_ID"`
	MembersClientSecret     string `env:"APP_MEMBERS_OIDC_CLIENT_SECRET"`
	MembersRedirectURL      string `env:"APP_MEMBERS_OIDC_REDIRECT_URL"`
	MembersHostedDomain     string `env:"APP_MEMBERS_OIDC_HOSTED_DOMAIN"`
	MembersGroupPrefix      string `env:"APP_MEMBERS_OIDC_GROUP_PREFIX"`
	MembersAdminCredentials []byte `env:"APP_MEMBERS_OIDC_ADMIN_CREDENTIALS"`
	MembersAdminSubject     string `env:"APP_MEMBERS_OIDC_ADMIN_SUBJECT"`
}
