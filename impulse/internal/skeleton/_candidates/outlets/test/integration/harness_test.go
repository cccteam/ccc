package integration

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cccteam/access"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/outlets/app"
	"github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/outlets/pkg/auth"
	"github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/outlets/pkg/auth/members"
	"github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/outlets/pkg/auth/staff"
	"github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/outlets/pkg/router"
	"github.com/cccteam/ccc/resource"
	initiator "github.com/cccteam/db-initiator"
	"github.com/cccteam/logger"
	"github.com/cccteam/session"
	"github.com/go-playground/errors/v5"
	"github.com/go-playground/validator/v10"
)

const (
	migrationsSource = "file://../../schema/migrations"
	devSeedSource    = "file://../../schema/devseed"
	staffRolesPath   = "../../" + staff.RolesPath
	membersRolesPath = "../../" + members.RolesPath

	// The development tenants, matching schema/devseed.
	north = "north"
	south = "south"

	// The development identities the served suites act as: the staff logins admin
	// (Administrator everywhere) and member (north only); the portal's client, a member
	// of the members auth who signs in through the simulated directory and holds
	// Administrator in north; and the machines service account — no login, bound by the
	// API key — in north.
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
	auth    *staff.Auth
	members *members.Auth
}

// DomainVisible composes the development roster with the foothold answer of the engine
// of the auth the request came through — the same composition production's
// DataConfiguration.DomainVisible performs.
func (c *servedConfigurer) DomainVisible(ctx context.Context, user accesstypes.User, domain accesstypes.Domain) (bool, error) {
	if domain != north && domain != south {
		return false, nil
	}

	engine := c.auth.Access()
	if auth.Name(ctx) == members.Name {
		engine = c.members.Access()
	}
	visible, err := engine.UserHasGrants(ctx, user, accesstypes.DomainScope(domain))
	if err != nil {
		return false, errors.Wrap(err, "access.Client.UserHasGrants()")
	}

	return visible, nil
}

func (c *servedConfigurer) ResourceClient() resource.Client {
	return resource.NewSpannerClient(c.db.Client)
}

// CursorKey seals the cursors the suites' paged lists issue; any key serves a test process.
func (c *servedConfigurer) CursorKey() *resource.CursorKey {
	key, err := resource.NewCursorKey(base64.StdEncoding.EncodeToString([]byte("skeleton-test-cursor-key-material!!")))
	if err != nil {
		panic(err)
	}

	return key
}

func (c *servedConfigurer) Access() access.Controller { return c.auth.Access() }

func (c *servedConfigurer) Staff() *staff.Auth { return c.auth }

func (c *servedConfigurer) Members() *members.Auth { return c.members }

func (c *servedConfigurer) Validator() *validator.Validate { return validator.New() }

func (c *servedConfigurer) LogExporter() logger.Exporter { return logger.NewConsoleExporter() }

func (c *servedConfigurer) ConsoleDist() string { return "" }

func (c *servedConfigurer) PortalDist() string { return "" }

func (c *servedConfigurer) MachinesAPIKey() string { return machinesAPIKey }

// served is one running instance of the application under test.
type served struct {
	server *httptest.Server
	// access is the staff auth's engine, members the members auth's.
	access  *access.Client
	members *access.Client
}

// newServed provisions the database the way the deployment does (schema, then the
// committed roles of both auths), creates the development identities, and serves the full
// router. The server is allocated before the members auth so the auth's directory
// callback can name the server's own address.
func newServed(ctx context.Context, t *testing.T) *served {
	t.Helper()

	db, err := prepareDatabase(ctx, t, migrationsSource, devSeedSource)
	if err != nil {
		t.Fatal(err)
	}

	staffAuth, err := staff.New(ctx, db.Client, staff.Settings{CookieKey: testCookieKey, SessionTimeout: time.Minute})
	if err != nil {
		t.Fatalf("staff.New() error = %v", err)
	}
	t.Cleanup(func() {
		if err := staffAuth.Close(); err != nil {
			t.Errorf("staff.Auth.Close() error = %v", err)
		}
	})
	accessClient := staffAuth.Access()

	if err := access.MigrateRoles(ctx, accessClient.UserManager(), router.Collection(), loadRoles(t, staffRolesPath), north, south); err != nil {
		t.Fatalf("access.MigrateRoles(staff) error = %v", err)
	}

	server := httptest.NewUnstartedServer(nil)
	t.Cleanup(server.Close)

	// The members auth: under the skipAuth build tag the directory is simulated and the
	// login returns straight to the callback, so the callback is the only registration
	// detail that matters.
	membersAuth, err := members.New(ctx, db.Client, &members.Settings{
		CookieKey:      testCookieKey,
		SessionTimeout: time.Minute,
		LoginURL:       "/portal/login",
		Directory:      members.Directory{RedirectURL: "http://" + server.Listener.Addr().String() + portalAPI + "/user/callback"},
	})
	if err != nil {
		t.Fatalf("members.New() error = %v", err)
	}
	t.Cleanup(func() {
		if err := membersAuth.Close(); err != nil {
			t.Errorf("members.Auth.Close() error = %v", err)
		}
	})
	membersAccess := membersAuth.Access()
	if err := access.MigrateRoles(ctx, membersAccess.UserManager(), router.Collection(), loadRoles(t, membersRolesPath), north, south); err != nil {
		t.Fatalf("access.MigrateRoles(members) error = %v", err)
	}
	// The portal's client is a member: no login to create, since the directory presents
	// the name; only roles, in the members store.
	if err := membersAccess.UserManager().AddUserRoles(ctx, accesstypes.DomainScope(north), clientUser, "Administrator"); err != nil {
		t.Fatalf("AddUserRoles(%s, north) error = %v", clientUser, err)
	}

	passwordAuth := staffAuth.Session()
	password := adminPassword
	for _, user := range []string{adminUser, memberUser} {
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
	waitForDomains(ctx, t, membersAccess, clientUser, []accesstypes.Domain{north})

	server.Config.Handler = router.New(app.New(&servedConfigurer{db: db, auth: staffAuth, members: membersAuth}))
	server.Start()

	return &served{server: server, access: accessClient, members: membersAccess}
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

// loadRoles reads one auth's committed role configuration.
func loadRoles(t *testing.T, path string) *access.RoleConfig {
	t.Helper()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	var roles access.RoleConfig
	if err := json.Unmarshal(raw, &roles); err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}

	return &roles
}

// outletXSRF names the XSRF cookie each session-serving outlet's auth issues: the browser
// on an outlet echoes that auth's cookie, as the outlet's web app does.
var outletXSRF = map[string]string{
	consoleAPI: staff.XSRFCookie,
	portalAPI:  members.XSRFCookie,
}

// browser is one browser's view of the served application on one session-serving
// outlet: a cookie jar and the XSRF token the session middleware issued into it.
type browser struct {
	t      *testing.T
	base   string
	prefix string
	xsrf   string
	client *http.Client
}

func newBrowser(t *testing.T, s *served, prefix string) *browser {
	t.Helper()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}

	xsrf, ok := outletXSRF[prefix]
	if !ok {
		t.Fatalf("no auth issues an XSRF cookie on %s", prefix)
	}

	return &browser{t: t, base: s.server.URL, prefix: prefix, xsrf: xsrf, client: &http.Client{Jar: jar}}
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

// redirect issues one GET as this browser without following redirects and returns the
// status and the Location header. target is a path on the server or an absolute URL.
func (b *browser) redirect(ctx context.Context, target string) (status int, location string) {
	b.t.Helper()

	if !strings.HasPrefix(target, "http") {
		target = b.base + target
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, http.NoBody)
	if err != nil {
		b.t.Fatal(err)
	}
	client := &http.Client{Jar: b.client.Jar, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.Do(req)
	if err != nil {
		b.t.Fatal(err)
	}
	defer resp.Body.Close()
	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		b.t.Fatal(err)
	}

	return resp.StatusCode, resp.Header.Get("Location")
}

// xsrfToken returns the XSRF cookie the session middleware issued, or empty before the
// first response.
func (b *browser) xsrfToken() string {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, b.base+b.prefix+"/user/session", http.NoBody)
	if err != nil {
		b.t.Fatal(err)
	}
	for _, cookie := range b.client.Jar.Cookies(req.URL) {
		if cookie.Name == b.xsrf {
			return cookie.Value
		}
	}

	return ""
}
