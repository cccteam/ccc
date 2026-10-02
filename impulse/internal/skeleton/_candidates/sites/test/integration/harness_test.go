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
	"slices"
	"testing"
	"time"

	"github.com/cccteam/access"
	"github.com/cccteam/ccc/accesstypes"
	consoleapp "github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/sites/apps/console/app"
	consolerouter "github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/sites/apps/console/pkg/router"
	portalapp "github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/sites/apps/portal/app"
	portalrouter "github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/sites/apps/portal/pkg/router"
	"github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/sites/pkg/auth/staff"
	"github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/sites/pkg/config"
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

	// The development tenants, matching schema/devseed.
	north = "north"
	south = "south"

	// The development logins the served suites sign in as: admin holds
	// Administrator_Global and Administrator_Domain in every tenant, member and the
	// portal's client hold Administrator_Domain in north only.
	adminUser     = "admin"
	memberUser    = "member"
	clientUser    = "client"
	adminPassword = "password"
)

// servedConfigurer implements both sites' Configurer over the real dependencies: the
// test database, the real permission engine, and a real session manager, so the suites
// exercise the same served stack each site's main composes.
type servedConfigurer struct {
	db   *initiator.SpannerDB
	auth *staff.Auth
}

// Domains lists the development tenants: the roster the served stack filters by the
// engine's foothold answer, as production's DataConfiguration.Domains lists the table.
func (c *servedConfigurer) Domains(_ context.Context) ([]accesstypes.Domain, error) {
	return []accesstypes.Domain{north, south}, nil
}

// DomainVisible composes the development roster with the engine's foothold answer — the
// same composition production's DataConfiguration.DomainVisible performs.
func (c *servedConfigurer) DomainVisible(ctx context.Context, user accesstypes.User, domain accesstypes.Domain) (bool, error) {
	if domain != north && domain != south {
		return false, nil
	}

	visible, err := c.auth.Access().UserHasGrants(ctx, user, accesstypes.DomainScope(domain))
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

func (c *servedConfigurer) Validator() *validator.Validate { return validator.New() }

func (c *servedConfigurer) LogExporter() logger.Exporter { return logger.NewConsoleExporter() }

func (c *servedConfigurer) Dist() string { return "" }

// served is one running instance of the application under test: both sites over one
// database, one policy store, and one session store.
type served struct {
	console *httptest.Server
	portal  *httptest.Server
	access  *access.Client
}

// newServed provisions the database the way the deployment does (the schema and the
// development tenants, then the auth opened over it with its embedded role file validated
// against the union of the sites' collections), creates the development logins, and
// serves both sites' full routers.
func newServed(ctx context.Context, t *testing.T) *served {
	t.Helper()

	db, err := prepareDatabase(ctx, t, migrationsSource, devSeedSource)
	if err != nil {
		t.Fatal(err)
	}

	collection, err := config.Collection()
	if err != nil {
		t.Fatalf("config.Collection() error = %v", err)
	}
	auth, err := staff.New(ctx, db.Client, staff.Settings{Collection: collection, CookieKey: testCookieKey, SessionTimeout: time.Minute})
	if err != nil {
		t.Fatalf("staff.New() error = %v", err)
	}
	t.Cleanup(func() {
		if err := auth.Close(); err != nil {
			t.Errorf("staff.Auth.Close() error = %v", err)
		}
	})
	accessClient := auth.Access()

	passwordAuth := auth.Session()
	password := adminPassword
	for _, user := range []string{adminUser, memberUser, clientUser} {
		if _, err := passwordAuth.API().CreateSessionUser(ctx, &session.CreateUserRequest{Username: user, Password: &password}); err != nil {
			t.Fatalf("CreateSessionUser(%s) error = %v", user, err)
		}
	}
	// The memberships as the bootstrap identities hold them: the administrator's domain
	// role in every tenant domain, one membership, and the member's in north alone.
	assignments := []struct {
		user  accesstypes.User
		scope accesstypes.PolicyScope
		role  accesstypes.Role
	}{
		{adminUser, accesstypes.GlobalPolicyScope(), "Administrator_Global"},
		{adminUser, accesstypes.EveryDomainPolicyScope(), "Administrator_Domain"},
		{memberUser, accesstypes.DomainPolicyScope(north), "Administrator_Domain"},
		{clientUser, accesstypes.DomainPolicyScope(north), "Administrator_Domain"},
	}
	for _, a := range assignments {
		if err := accessClient.UserManager().AddUserRoles(ctx, a.scope, a.user, a.role); err != nil {
			t.Fatalf("AddUserRoles(%s, %v) error = %v", a.user, a.scope, err)
		}
	}

	// The snapshot swap is asynchronous: wait until the last-provisioned assignment is
	// visible to the engine before serving.
	waitForDomains(ctx, t, accessClient, clientUser, []accesstypes.Domain{north})

	conf := &servedConfigurer{db: db, auth: auth}
	console := httptest.NewServer(consolerouter.New(consoleapp.New(conf), consolerouter.Hooks{}))
	t.Cleanup(console.Close)
	portal := httptest.NewServer(portalrouter.New(portalapp.New(conf), portalrouter.Hooks{}))
	t.Cleanup(portal.Close)

	return &served{console: console, portal: portal, access: accessClient}
}

// waitForDomains blocks until the engine's snapshot reports the user's footholds in
// exactly the expected tenants, asked the way the served stack asks (the development
// roster filtered by UserHasGrants): the store writes signal a reload, but the swap is
// asynchronous.
func waitForDomains(ctx context.Context, t *testing.T, client *access.Client, user accesstypes.User, want []accesstypes.Domain) {
	t.Helper()

	deadline := time.Now().Add(15 * time.Second)
	for {
		domains, err := footholds(ctx, client, user)
		if err == nil && slices.Equal(domains, want) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("policy snapshot never became visible; last domains for %s: %v (err %v)", user, domains, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// footholds lists the development tenants where the user holds at least one grant.
func footholds(ctx context.Context, client *access.Client, user accesstypes.User) ([]accesstypes.Domain, error) {
	var domains []accesstypes.Domain
	for _, domain := range []accesstypes.Domain{north, south} {
		has, err := client.UserHasGrants(ctx, user, accesstypes.DomainScope(domain))
		if err != nil {
			return nil, errors.Wrap(err, "access.Client.UserHasGrants()")
		}
		if has {
			domains = append(domains, domain)
		}
	}

	return domains, nil
}

// testCookieKey signs session cookies in the suites; any 32 bytes will do.
const testCookieKey = "dGVzdC1jb29raWUta2V5LXRlc3QtY29va2llLWtleS0xMjM0NTY="

// browser is one browser's view of the served application: a cookie jar and the XSRF
// token the session middleware issued into it.
type browser struct {
	t      *testing.T
	base   string
	client *http.Client
}

// newBrowser opens a browser on one site.
func newBrowser(t *testing.T, site *httptest.Server) *browser {
	t.Helper()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}

	return &browser{t: t, base: site.URL, client: &http.Client{Jar: jar}}
}

// login posts the credentials to the login route and returns the status.
func (b *browser) login(ctx context.Context, user, password string) (status int, body []byte) {
	b.t.Helper()

	credentials, err := json.Marshal(map[string]string{"username": user, "password": password})
	if err != nil {
		b.t.Fatal(err)
	}

	return b.do(ctx, http.MethodPost, "/api/user/login", credentials)
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
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, b.base+"/api/user/session", http.NoBody)
	if err != nil {
		b.t.Fatal(err)
	}
	for _, cookie := range b.client.Jar.Cookies(req.URL) {
		if cookie.Name == staff.XSRFCookie {
			return cookie.Value
		}
	}

	return ""
}

// provesGrant names the conditional grant a test case proves. It parses the auth's role
// file (<auth>.Roles(), the file embedded in the auth package) and fails unless a grant for
// the role, permission, and resource carries exactly that condition text, so a case whose
// grant is gone or reworded fails here even when nobody ran impulse check, whose
// conditions-proven check reads these calls to find the conditional grants no case names.
// Write the file as the auth package's Roles() call and the coordinates as literals: the
// check reads them from the source.
func provesGrant(t *testing.T, roles access.RoleFile, role accesstypes.Role, permission accesstypes.Permission, res accesstypes.Resource, condition string) {
	t.Helper()

	parsed, err := roles.Parse()
	if err != nil {
		t.Fatalf("parsing the role file: %v", err)
	}
	var conditions []string
	for _, r := range slices.Concat(parsed.Roles.Global, parsed.Roles.Domain) {
		if r.Name != role {
			continue
		}
		for _, g := range r.Permissions[permission] {
			if g.Resource != res {
				continue
			}
			if g.Condition == condition {
				return
			}
			conditions = append(conditions, g.Condition)
		}
	}
	t.Fatalf("no %s grant of the %s role on %s carries the condition %q (the file's conditions there: %q); the case proves a grant the file no longer carries", permission, role, res, condition, conditions)
}
