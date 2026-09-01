// Package config loads the waystation application configuration from the environment
// and constructs the shared clients the served application depends on.
//
// The waystation is a demo application that only ever runs against a Spanner emulator,
// so New refuses to start without SPANNER_EMULATOR_HOST, mirroring the safety rail in
// cmd/bootstrap. All other settings are optional and default to the values the
// bootstrap provisions.
package config

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	stderrors "errors"
	"fmt"
	"os"
	"time"

	cloudspanner "cloud.google.com/go/spanner"
	"github.com/cccteam/access"
	"github.com/cccteam/access/spannerstore"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/waystation/pkg/rpc"
	"github.com/cccteam/session"
	"github.com/cccteam/session/sessionstorage"
	"github.com/go-playground/errors/v5"
	"github.com/go-playground/validator/v10"
	"github.com/sethvargo/go-envconfig"
	"google.golang.org/api/iterator"
)

// Configuration holds the application configuration and the clients built from it.
type Configuration struct {
	domains          []accesstypes.Domain
	domainSet        map[accesstypes.Domain]bool
	spannerClient    *cloudspanner.Client
	resourceClient   *resource.SpannerClient
	access           *access.Client
	session          *session.PasswordAuth[session.NoCustomData, session.NoCustomData]
	rpcClient        *rpc.Client
	validator        *validator.Validate
	automationAPIKey string
	envVars          appConfig
}

// New loads the configuration from the environment and constructs the application's
// clients: the Spanner client, the resource client, the access engine (blocking until
// its first policy snapshot is loaded), and the password-auth session manager over the
// demo personas the bootstrap seeds.
func New(ctx context.Context) (*Configuration, error) {
	if os.Getenv("SPANNER_EMULATOR_HOST") == "" {
		return nil, errors.New("SPANNER_EMULATOR_HOST must be set: the waystation only runs against a Spanner emulator")
	}

	var envVars appConfig
	if err := envconfig.ProcessWith(ctx, &envconfig.Config{
		Target:   &envVars,
		Lookuper: envconfig.OsLookuper(),
	}); err != nil {
		return nil, errors.Wrap(err, "envconfig.ProcessWith()")
	}

	spannerClient, err := cloudspanner.NewClient(ctx, envVars.Spanner.DatabasePath())
	if err != nil {
		return nil, errors.Wrap(err, "spanner.NewClient()")
	}

	store, err := spannerstore.New(spannerClient)
	if err != nil {
		return nil, errors.Wrap(err, "spannerstore.New()")
	}

	accessClient, err := access.New(store)
	if err != nil {
		return nil, errors.Wrap(err, "access.New()")
	}
	if err := accessClient.WaitReady(ctx); err != nil {
		return nil, errors.Wrap(err, "access.Client.WaitReady()")
	}

	cookieKey, err := cookieKey(envVars.CookieKey)
	if err != nil {
		return nil, err
	}

	automationAPIKey := envVars.AutomationAPIKey
	if automationAPIKey == "" {
		// An ephemeral key keeps the automation outlet fail-closed: nothing knows it,
		// so nothing authenticates until WAYSTATION_AUTOMATION_API_KEY is configured.
		automationAPIKey, err = EphemeralCookieKey()
		if err != nil {
			return nil, err
		}
	}

	passwordAuth, err := session.NewPasswordAuth[session.NoCustomData, session.NoCustomData](
		sessionstorage.NewSpannerPasswordAuth(spannerClient),
		cookieKey,
		session.WithSessionTimeout(envVars.SessionTimeout),
	)
	if err != nil {
		return nil, errors.Wrap(err, "session.NewPasswordAuth()")
	}

	conf := &Configuration{
		spannerClient:    spannerClient,
		resourceClient:   resource.NewSpannerClient(spannerClient),
		access:           accessClient,
		session:          passwordAuth,
		rpcClient:        rpc.NewClient(),
		validator:        validator.New(),
		automationAPIKey: automationAPIKey,
		envVars:          envVars,
	}
	if err := conf.loadDomains(ctx); err != nil {
		return nil, errors.Wrap(err, "loadDomains()")
	}

	return conf, nil
}

// cookieKey returns the configured session cookie key, or generates an ephemeral one
// when none is configured. An ephemeral key means sessions do not survive a restart,
// which is acceptable for the demo.
func cookieKey(configured string) (string, error) {
	if configured != "" {
		return configured, nil
	}

	return EphemeralCookieKey()
}

// EphemeralCookieKey generates a single-process session cookie key: 32 bytes of
// cryptographically secure random data, Base64 encoded.
func EphemeralCookieKey() (string, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return "", errors.Wrap(err, "rand.Read()")
	}

	return base64.StdEncoding.EncodeToString(key), nil
}

// Close releases the resources held by the configuration.
func (c *Configuration) Close() {
	_ = c.access.Close() // stops background policy reloading; nothing to recover at shutdown
	c.spannerClient.Close()
}

// Addr returns the http address the server listens on.
//
//	"hostname:port"
func (c *Configuration) Addr() string {
	return c.envVars.Host + ":" + c.envVars.Port
}

// ResourceClient returns the database client used by the resource layer.
func (c *Configuration) ResourceClient() resource.Client {
	return c.resourceClient
}

// Access returns the access engine controller.
func (c *Configuration) Access() access.Controller {
	return c.access
}

// Session returns the session manager.
func (c *Configuration) Session() *session.PasswordAuth[session.NoCustomData, session.NoCustomData] {
	return c.session
}

// RPCClient returns the dependencies for RPC method implementations.
func (c *Configuration) RPCClient() *rpc.Client {
	return c.rpcClient
}

// Validator returns the request payload validator.
func (c *Configuration) Validator() *validator.Validate {
	return c.validator
}

// GuiDist returns the directory the built Angular application is served from.
func (c *Configuration) GuiDist() string {
	return c.envVars.GuiDist
}

// AutomationAPIKey returns the bearer key the automation outlet's API-key middleware
// validates machine clients against.
func (c *Configuration) AutomationAPIKey() string {
	return c.automationAPIKey
}

// loadDomains reads the tenancy roster from the Waystations tenant table once at
// startup. Unlike starport's fixed in-code list, the waystation derives tenancy from
// its own data — but it caches the roster rather than querying per check: the
// generated consolidated handler consults DomainExists inside the mutation
// transaction, where opening another Spanner read is illegal on the emulator. A demo
// restart picks up new tenants; a production application would refresh the cache.
func (c *Configuration) loadDomains(ctx context.Context) error {
	iter := c.spannerClient.Single().Query(ctx, cloudspanner.Statement{
		SQL: "SELECT Id FROM Waystations ORDER BY Id",
	})
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

// DomainExists reports whether the domain is a known waystation, answered from the
// startup tenancy cache. Test suites supply their own implementation through the same
// Configurer seam.
func (c *Configuration) DomainExists(_ context.Context, domain accesstypes.Domain) (bool, error) {
	return c.domainSet[domain], nil
}

// Domains lists the known waystations as permission domains, from the startup
// tenancy cache.
func (c *Configuration) Domains(_ context.Context) ([]accesstypes.Domain, error) {
	return c.domains, nil
}

// appConfig holds the environment variables used by the served application. The
// Spanner settings share their names and defaults with cmd/bootstrap, so a database
// provisioned by the bootstrap is served without further configuration.
type appConfig struct {
	// Host is the hostname the server binds to.
	Host string `env:"WAYSTATION_HOST"`

	// Port is the port the server listens on.
	Port string `env:"WAYSTATION_PORT,default=8082"`

	// SessionTimeout is the idle timeout for demo sessions.
	SessionTimeout time.Duration `env:"WAYSTATION_SESSION_TIMEOUT,default=1h"`

	// CookieKey is a Base64-encoded string of at least 32 bytes of cryptographically
	// secure random data used to sign session cookies. When unset an ephemeral key is
	// generated at startup.
	CookieKey string `env:"WAYSTATION_COOKIE_KEY"`

	// GuiDist is the directory holding the built Angular application.
	GuiDist string `env:"WAYSTATION_GUI_DIST,default=gui/dist"`

	// AutomationAPIKey is the bearer key machine clients present on the automation
	// outlet (/automation/...). When unset an ephemeral key is generated at startup,
	// which keeps the surface fail-closed but unreachable until a key is configured.
	AutomationAPIKey string `env:"WAYSTATION_AUTOMATION_API_KEY"`

	Spanner SpannerConfig
}

// SpannerConfig identifies the emulator database the waystation serves.
type SpannerConfig struct {
	ProjectID    string `env:"WAYSTATION_SPANNER_PROJECT_ID,default=waystation-demo"`
	InstanceID   string `env:"WAYSTATION_SPANNER_INSTANCE_ID,default=waystation"`
	DatabaseName string `env:"WAYSTATION_SPANNER_DATABASE,default=waystation"`
}

// DatabasePath returns the fully qualified Spanner database path.
func (s SpannerConfig) DatabasePath() string {
	return fmt.Sprintf("projects/%s/instances/%s/databases/%s", s.ProjectID, s.InstanceID, s.DatabaseName)
}
