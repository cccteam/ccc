package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/cccteam/access"
	"github.com/cccteam/access/spannerstore"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/outlets/app"
	"github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/outlets/pkg/router"
	"github.com/cccteam/ccc/resource"
	initiator "github.com/cccteam/db-initiator"
	"github.com/cccteam/logger"
	"github.com/cccteam/session"
	"github.com/cccteam/session/sessionstorage"
	"github.com/go-playground/errors/v5"
	"github.com/go-playground/validator/v10"
)

const (
	migrationsSource = "file://../../schema/migrations"
	devSeedSource    = "file://../../schema/devseed"
	rolesPath        = "../../schema/roles.json"

	// The development tenants, matching schema/devseed.
	north = "north"
	south = "south"

	// The development identities the served suites act as: admin holds Administrator
	// everywhere, member and the portal's client only in north, and the machines
	// service account — no login, bound by the API key — in north.
	adminUser     = "admin"
	memberUser    = "member"
	clientUser    = "client"
	machinesUser  = "machines"
	adminPassword = "password"

	// The machines outlet's API key in the suites.
	machinesAPIKey = "integration-machines-key"

	// The browser outlets' API prefixes.
	consoleAPI = "/api"
	portalAPI  = "/portal/api"
)

// servedConfigurer implements app.Configurer over the real dependencies: the test
// database, the real permission engine, and a real session manager, so the suites
// exercise the same served stack main composes.
type servedConfigurer struct {
	db      *initiator.SpannerDB
	access  *access.Client
	session *session.PasswordAuth[session.NoCustomData, session.NoCustomData]
}

// DomainVisible composes the development roster with the engine's foothold answer — the
// same composition production's DataConfiguration.DomainVisible performs.
func (c *servedConfigurer) DomainVisible(ctx context.Context, user accesstypes.User, domain accesstypes.Domain) (bool, error) {
	if domain != north && domain != south {
		return false, nil
	}

	visible, err := c.access.UserHasGrants(ctx, user, accesstypes.DomainScope(domain))
	if err != nil {
		return false, errors.Wrap(err, "access.Client.UserHasGrants()")
	}

	return visible, nil
}

func (c *servedConfigurer) ResourceClient() resource.Client {
	return resource.NewSpannerClient(c.db.Client)
}

func (c *servedConfigurer) Access() access.Controller { return c.access }

func (c *servedConfigurer) Session() *session.PasswordAuth[session.NoCustomData, session.NoCustomData] {
	return c.session
}

func (c *servedConfigurer) Validator() *validator.Validate { return validator.New() }

func (c *servedConfigurer) LogExporter() logger.Exporter { return logger.NewConsoleExporter() }

func (c *servedConfigurer) ConsoleDist() string { return "" }

func (c *servedConfigurer) PortalDist() string { return "" }

func (c *servedConfigurer) MachinesAPIKey() string { return machinesAPIKey }

// served is one running instance of the application under test.
type served struct {
	server *httptest.Server
	access *access.Client
}

// newServed provisions the database the way the deployment does (schema, then the
// committed roles), creates the development login, and serves the full router.
func newServed(ctx context.Context, t *testing.T) *served {
	t.Helper()

	db, err := prepareDatabase(ctx, t, migrationsSource, devSeedSource)
	if err != nil {
		t.Fatal(err)
	}

	store, err := spannerstore.New(db.Client)
	if err != nil {
		t.Fatalf("spannerstore.New() error = %v", err)
	}
	accessClient, err := access.New(store)
	if err != nil {
		t.Fatalf("access.New() error = %v", err)
	}
	t.Cleanup(func() {
		if err := accessClient.Close(); err != nil {
			t.Errorf("access.Client.Close() error = %v", err)
		}
	})

	roles := loadRoles(t)
	if err := access.MigrateRoles(ctx, accessClient.UserManager(), router.Collection(), roles, north, south); err != nil {
		t.Fatalf("access.MigrateRoles() error = %v", err)
	}

	passwordAuth, err := session.NewPasswordAuth[session.NoCustomData, session.NoCustomData](
		sessionstorage.NewSpannerPasswordAuth(db.Client), testCookieKey)
	if err != nil {
		t.Fatalf("session.NewPasswordAuth() error = %v", err)
	}
	password := adminPassword
	for _, user := range []string{adminUser, memberUser, clientUser} {
		if _, err := passwordAuth.API().CreateSessionUser(ctx, &session.CreateUserRequest{Username: user, Password: &password}); err != nil {
			t.Fatalf("CreateSessionUser(%s) error = %v", user, err)
		}
	}
	assignments := []struct {
		user  accesstypes.User
		scope accesstypes.Scope
	}{
		{adminUser, accesstypes.GlobalScope()},
		{adminUser, accesstypes.DomainScope(north)},
		{adminUser, accesstypes.DomainScope(south)},
		{memberUser, accesstypes.DomainScope(north)},
		{clientUser, accesstypes.DomainScope(north)},
		{machinesUser, accesstypes.DomainScope(north)},
	}
	for _, a := range assignments {
		if err := accessClient.UserManager().AddUserRoles(ctx, a.scope, a.user, "Administrator"); err != nil {
			t.Fatalf("AddUserRoles(%s, %v) error = %v", a.user, a.scope, err)
		}
	}

	// The snapshot swap is asynchronous: wait until the last-provisioned assignment is
	// visible to the engine before serving.
	waitForDomains(ctx, t, accessClient, machinesUser, []accesstypes.Domain{north})

	handler := router.New(app.New(&servedConfigurer{db: db, access: accessClient, session: passwordAuth}))
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	return &served{server: server, access: accessClient}
}

// waitForDomains blocks until the engine's snapshot reports the user's expected tenant
// membership: the store writes signal a reload, but the swap is asynchronous.
func waitForDomains(ctx context.Context, t *testing.T, client *access.Client, user accesstypes.User, want []accesstypes.Domain) {
	t.Helper()

	deadline := time.Now().Add(15 * time.Second)
	for {
		domains, err := client.UserDomains(ctx, user)
		if err == nil && slices.Equal(domains, want) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("policy snapshot never became visible; last domains for %s: %v (err %v)", user, domains, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// testCookieKey signs session cookies in the suites; any 32 bytes will do.
const testCookieKey = "dGVzdC1jb29raWUta2V5LXRlc3QtY29va2llLWtleS0xMjM0NTY="

func loadRoles(t *testing.T) *access.RoleConfig {
	t.Helper()

	raw, err := os.ReadFile(rolesPath)
	if err != nil {
		t.Fatalf("reading %s: %v", rolesPath, err)
	}
	var roles access.RoleConfig
	if err := json.Unmarshal(raw, &roles); err != nil {
		t.Fatalf("parsing %s: %v", rolesPath, err)
	}

	return &roles
}

// browser is one browser's view of the served application on one session-serving
// outlet: a cookie jar and the XSRF token the session middleware issued into it.
type browser struct {
	t      *testing.T
	base   string
	prefix string
	client *http.Client
}

func newBrowser(t *testing.T, s *served, prefix string) *browser {
	t.Helper()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}

	return &browser{t: t, base: s.server.URL, prefix: prefix, client: &http.Client{Jar: jar}}
}

// login posts the credentials to the outlet's login route and returns the status.
func (b *browser) login(ctx context.Context, user, password string) (status int, body []byte) {
	b.t.Helper()

	credentials, err := json.Marshal(map[string]string{"username": user, "password": password})
	if err != nil {
		b.t.Fatal(err)
	}

	return b.do(ctx, http.MethodPost, b.prefix+"/user/login", credentials)
}

// machine issues one request as a machine client carrying the bearer key (empty for an
// unauthenticated probe).
func machine(ctx context.Context, t *testing.T, s *served, key, method, path string) (status int, body []byte) {
	t.Helper()

	req, err := http.NewRequestWithContext(ctx, method, s.server.URL+path, http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	body, err = io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}

	return resp.StatusCode, body
}

// do issues one request as this browser, carrying its cookies and XSRF token.
func (b *browser) do(ctx context.Context, method, path string, body []byte) (status int, respBody []byte) {
	b.t.Helper()

	req, err := http.NewRequestWithContext(ctx, method, b.base+path, bytes.NewReader(body))
	if err != nil {
		b.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token := b.xsrfToken(); token != "" {
		req.Header.Set("X-XSRF-TOKEN", token)
	}

	resp, err := b.client.Do(req)
	if err != nil {
		b.t.Fatal(err)
	}
	defer resp.Body.Close()

	respBody, err = io.ReadAll(resp.Body)
	if err != nil {
		b.t.Fatal(err)
	}

	return resp.StatusCode, respBody
}

// xsrfToken returns the XSRF cookie the session middleware issued, or empty before the
// first response.
func (b *browser) xsrfToken() string {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, b.base+b.prefix+"/user/session", http.NoBody)
	if err != nil {
		b.t.Fatal(err)
	}
	for _, cookie := range b.client.Jar.Cookies(req.URL) {
		if cookie.Name == "XSRF-TOKEN" {
			return cookie.Value
		}
	}

	return ""
}
