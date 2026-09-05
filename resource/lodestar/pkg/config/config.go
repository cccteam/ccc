// Package config loads the Lodestar application configuration from the environment
// and constructs the shared clients the served application depends on.
//
// Lodestar is a demo application that only ever runs against a Spanner emulator, so
// New refuses to start without SPANNER_EMULATOR_HOST, mirroring the safety rail in
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
	"github.com/cccteam/ccc/resource/lodestar/pkg/rpc"
	"github.com/cccteam/session"
	"github.com/cccteam/session/sessionstorage"
	"github.com/go-playground/errors/v5"
	"github.com/go-playground/validator/v10"
	"github.com/sethvargo/go-envconfig"
	"google.golang.org/api/iterator"
)

// ImpersonationTable is the impersonation record table the session library joins
// into every session read; its DDL ships in schema/migrations.
const ImpersonationTable = "SessionImpersonations"

// Configuration holds the application configuration and the clients built from it.
type Configuration struct {
	domains        []accesstypes.Domain
	domainSet      map[accesstypes.Domain]bool
	spannerClient  *cloudspanner.Client
	resourceClient *resource.SpannerClient
	access         *access.Client
	session        *session.PasswordAuth[session.NoCustomData, session.NoCustomData]
	rpcClient      *rpc.Client
	validator      *validator.Validate
	droidAPIKey    string
	envVars        appConfig
}

// New loads the configuration from the environment and constructs the application's
// clients: the Spanner client, the resource client, the access engine (blocking until
// its first policy snapshot is loaded), and the password-auth session manager over the
// demo personas the bootstrap seeds, with impersonated sessions enabled.
func New(ctx context.Context) (*Configuration, error) {
	if os.Getenv("SPANNER_EMULATOR_HOST") == "" {
		return nil, errors.New("SPANNER_EMULATOR_HOST must be set: Lodestar only runs against a Spanner emulator")
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

	droidAPIKey := envVars.DroidAPIKey
	if droidAPIKey == "" {
		// An ephemeral key keeps the droid outlet fail-closed: nothing knows it, so
		// nothing authenticates until LODESTAR_DROID_API_KEY is configured.
		droidAPIKey, err = EphemeralCookieKey()
		if err != nil {
			return nil, err
		}
	}

	passwordAuth, err := NewPasswordAuth(spannerClient, cookieKey, session.WithSessionTimeout(envVars.SessionTimeout))
	if err != nil {
		return nil, err
	}

	conf := &Configuration{
		spannerClient:  spannerClient,
		resourceClient: resource.NewSpannerClient(spannerClient),
		access:         accessClient,
		session:        passwordAuth,
		rpcClient:      rpc.NewClient(),
		validator:      validator.New(),
		droidAPIKey:    droidAPIKey,
		envVars:        envVars,
	}
	if err := conf.loadDomains(ctx); err != nil {
		return nil, errors.Wrap(err, "loadDomains()")
	}

	return conf, nil
}

// NewPasswordAuth constructs the session manager the way the served application and
// the bootstrap both need it: Spanner-backed password auth with the impersonation
// record table attached, so StartImpersonatedSession can mint view-as and
// act-as-role sessions (design plan §3).
func NewPasswordAuth(spannerClient *cloudspanner.Client, cookieKey string, opts ...session.PasswordOption) (*session.PasswordAuth[session.NoCustomData, session.NoCustomData], error) {
	impersonation, err := sessionstorage.NewImpersonationTable(ImpersonationTable)
	if err != nil {
		return nil, errors.Wrap(err, "sessionstorage.NewImpersonationTable()")
	}

	passwordAuth, err := session.NewPasswordAuth[session.NoCustomData, session.NoCustomData](
		sessionstorage.NewSpannerPasswordAuth(spannerClient, sessionstorage.WithImpersonation(impersonation)),
		cookieKey,
		opts...,
	)
	if err != nil {
		return nil, errors.Wrap(err, "session.NewPasswordAuth()")
	}

	return passwordAuth, nil
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

// ConsoleDist returns the directory the built crew-console Angular application is
// served from.
func (c *Configuration) ConsoleDist() string {
	return c.envVars.ConsoleDist
}

// PortalDist returns the directory the built client-portal Angular application is
// served from.
func (c *Configuration) PortalDist() string {
	return c.envVars.PortalDist
}

// DroidAPIKey returns the bearer key the droid outlet's API-key middleware validates
// machine clients against.
func (c *Configuration) DroidAPIKey() string {
	return c.droidAPIKey
}

// loadDomains reads the tenancy roster from the Sectors tenant table once at
// startup. Lodestar derives tenancy from its own data rather than a fixed in-code
// list — but it caches the roster rather than querying per check: the generated
// consolidated handler consults DomainVisible inside the mutation transaction, where
// opening another Spanner read is illegal on the emulator. A demo restart picks up
// new sectors; a production application would refresh the cache.
func (c *Configuration) loadDomains(ctx context.Context) error {
	iter := c.spannerClient.Single().Query(ctx, cloudspanner.Statement{
		SQL: "SELECT Id FROM Sectors ORDER BY Id",
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

// DomainVisible reports whether the domain is a known sector AND the user holds at
// least one grant in it — existence from the startup tenancy cache, foothold from the
// access engine's in-memory policy snapshot (no store read, safe inside the
// consolidated handler's mutation transaction). Sector existence is concealed: a
// caller with no foothold is answered exactly like the sector does not exist, which
// is how Cinder stays dark on most personas' star charts.
func (c *Configuration) DomainVisible(ctx context.Context, user accesstypes.User, domain accesstypes.Domain) (bool, error) {
	if !c.domainSet[domain] {
		return false, nil
	}

	visible, err := c.access.UserHasGrants(ctx, user, accesstypes.DomainScope(domain))
	if err != nil {
		return false, errors.Wrap(err, "access.Client.UserHasGrants()")
	}

	return visible, nil
}

// Domains lists the known sectors as permission domains, from the startup tenancy
// cache.
func (c *Configuration) Domains(_ context.Context) ([]accesstypes.Domain, error) {
	return c.domains, nil
}

// appConfig holds the environment variables used by the served application. The
// Spanner settings share their names and defaults with cmd/bootstrap, so a database
// provisioned by the bootstrap is served without further configuration.
type appConfig struct {
	// Host is the hostname the server binds to.
	Host string `env:"LODESTAR_HOST"`

	// Port is the port the server listens on. It differs from waystation's default so
	// both demos can run side by side.
	Port string `env:"LODESTAR_PORT,default=8083"`

	// SessionTimeout is the idle timeout for demo sessions.
	SessionTimeout time.Duration `env:"LODESTAR_SESSION_TIMEOUT,default=1h"`

	// CookieKey is a Base64-encoded string of at least 32 bytes of cryptographically
	// secure random data used to sign session cookies. When unset an ephemeral key is
	// generated at startup.
	CookieKey string `env:"LODESTAR_COOKIE_KEY"`

	// ConsoleDist is the directory holding the built crew-console Angular application.
	ConsoleDist string `env:"LODESTAR_CONSOLE_DIST,default=web/dist/console"`

	// PortalDist is the directory holding the built client-portal Angular application.
	PortalDist string `env:"LODESTAR_PORTAL_DIST,default=web/dist/portal"`

	// DroidAPIKey is the bearer key machine clients present on the droids outlet
	// (/droids/...). When unset an ephemeral key is generated at startup, which keeps
	// the surface fail-closed but unreachable until a key is configured.
	DroidAPIKey string `env:"LODESTAR_DROID_API_KEY"`

	Spanner SpannerConfig
}

// SpannerConfig identifies the emulator database Lodestar serves.
type SpannerConfig struct {
	ProjectID    string `env:"LODESTAR_SPANNER_PROJECT_ID,default=lodestar-demo"`
	InstanceID   string `env:"LODESTAR_SPANNER_INSTANCE_ID,default=lodestar"`
	DatabaseName string `env:"LODESTAR_SPANNER_DATABASE,default=lodestar"`
}

// DatabasePath returns the fully qualified Spanner database path.
func (s SpannerConfig) DatabasePath() string {
	return fmt.Sprintf("projects/%s/instances/%s/databases/%s", s.ProjectID, s.InstanceID, s.DatabaseName)
}
