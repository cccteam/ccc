// Package config loads the starport application configuration from the environment
// and constructs the shared clients the served application depends on.
//
// The starport is a demo application that only ever runs against a Spanner emulator,
// so New refuses to start without SPANNER_EMULATOR_HOST, mirroring the safety rail in
// cmd/bootstrap. All other settings are optional and default to the values the
// bootstrap provisions.
package config

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"time"

	cloudspanner "cloud.google.com/go/spanner"
	"github.com/cccteam/access"
	"github.com/cccteam/access/spannerstore"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/starport/pkg/rpc"
	"github.com/cccteam/ccc/resource/starport/pkg/stations"
	"github.com/cccteam/session"
	"github.com/cccteam/session/sessionstorage"
	"github.com/go-playground/errors/v5"
	"github.com/go-playground/validator/v10"
	"github.com/sethvargo/go-envconfig"
)

// Configuration holds the application configuration and the clients built from it.
type Configuration struct {
	spannerClient  *cloudspanner.Client
	resourceClient *resource.SpannerClient
	access         *access.Client
	session        *session.PasswordAuth[session.NoCustomData, session.NoCustomData]
	rpcClient      *rpc.Client
	validator      *validator.Validate
	envVars        appConfig
}

// New loads the configuration from the environment and constructs the application's
// clients: the Spanner client, the resource client, the access engine (blocking until
// its first policy snapshot is loaded), and the password-auth session manager over the
// demo users the bootstrap seeds.
func New(ctx context.Context) (*Configuration, error) {
	if os.Getenv("SPANNER_EMULATOR_HOST") == "" {
		return nil, errors.New("SPANNER_EMULATOR_HOST must be set: the starport only runs against a Spanner emulator")
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

	passwordAuth, err := session.NewPasswordAuth[session.NoCustomData, session.NoCustomData](
		sessionstorage.NewSpannerPasswordAuth(spannerClient),
		cookieKey,
		session.WithSessionTimeout(envVars.SessionTimeout),
	)
	if err != nil {
		return nil, errors.Wrap(err, "session.NewPasswordAuth()")
	}

	return &Configuration{
		spannerClient:  spannerClient,
		resourceClient: resource.NewSpannerClient(spannerClient),
		access:         accessClient,
		session:        passwordAuth,
		rpcClient:      rpc.NewClient(),
		validator:      validator.New(),
		envVars:        envVars,
	}, nil
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

// DomainExists reports whether the domain is a known station: the production tenancy
// source behind the app's unknown-domain 404 guard. Test suites supply their own
// implementation through the same Configurer seam.
func (c *Configuration) DomainExists(_ context.Context, domain accesstypes.Domain) (bool, error) {
	return stations.Exists(domain), nil
}

// appConfig holds the environment variables used by the served application. The
// Spanner settings share their names and defaults with cmd/bootstrap, so a database
// provisioned by the bootstrap is served without further configuration.
type appConfig struct {
	// Host is the hostname the server binds to.
	Host string `env:"STARPORT_HOST"`

	// Port is the port the server listens on.
	Port string `env:"STARPORT_PORT,default=8080"`

	// SessionTimeout is the idle timeout for demo sessions.
	SessionTimeout time.Duration `env:"STARPORT_SESSION_TIMEOUT,default=1h"`

	// CookieKey is a Base64-encoded string of at least 32 bytes of cryptographically
	// secure random data used to sign session cookies. When unset an ephemeral key is
	// generated at startup.
	CookieKey string `env:"STARPORT_COOKIE_KEY"`

	// GuiDist is the directory holding the built Angular application.
	GuiDist string `env:"STARPORT_GUI_DIST,default=gui/dist"`

	Spanner SpannerConfig
}

// SpannerConfig identifies the emulator database the starport serves.
type SpannerConfig struct {
	ProjectID    string `env:"STARPORT_SPANNER_PROJECT_ID,default=starport-demo"`
	InstanceID   string `env:"STARPORT_SPANNER_INSTANCE_ID,default=starport"`
	DatabaseName string `env:"STARPORT_SPANNER_DATABASE,default=starport"`
}

// DatabasePath returns the fully qualified Spanner database path.
func (s SpannerConfig) DatabasePath() string {
	return fmt.Sprintf("projects/%s/instances/%s/databases/%s", s.ProjectID, s.InstanceID, s.DatabaseName)
}
