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
	"testing"
	"time"

	"github.com/cccteam/access"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/solo/app"
	"github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/solo/pkg/auth/staff"
	"github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/solo/pkg/router"
	"github.com/cccteam/ccc/resource"
	initiator "github.com/cccteam/db-initiator"
	"github.com/cccteam/logger"
	"github.com/cccteam/session"
	"github.com/go-playground/validator/v10"
)

const (
	migrationsSource = "file://../../schema/migrations"
	rolesPath        = "../../" + staff.RolesPath

	// The development login the served suites sign in as.
	adminUser     = "admin"
	adminPassword = "password"
)

// servedConfigurer implements app.Configurer over the real dependencies: the test
// database, the real permission engine, and a real session manager, so the suites
// exercise the same served stack main composes.
type servedConfigurer struct {
	db   *initiator.SpannerDB
	auth *staff.Auth
}

func (c *servedConfigurer) ResourceClient() resource.Client {
	return resource.NewSpannerClient(c.db.Client)
}

func (c *servedConfigurer) Access() access.Controller { return c.auth.Access() }

func (c *servedConfigurer) Staff() *staff.Auth { return c.auth }

func (c *servedConfigurer) Validator() *validator.Validate { return validator.New() }

func (c *servedConfigurer) LogExporter() logger.Exporter { return logger.NewConsoleExporter() }

func (c *servedConfigurer) ConsoleDist() string { return "" }

// served is one running instance of the application under test.
type served struct {
	server *httptest.Server
	access *access.Client
}

// newServed provisions the database the way the deployment does (schema, then the
// committed roles), creates the development login, and serves the full router.
func newServed(ctx context.Context, t *testing.T) *served {
	t.Helper()

	db, err := prepareDatabase(ctx, t, migrationsSource)
	if err != nil {
		t.Fatal(err)
	}

	auth, err := staff.New(ctx, db.Client, staff.Settings{CookieKey: testCookieKey, SessionTimeout: time.Minute})
	if err != nil {
		t.Fatalf("staff.New() error = %v", err)
	}
	t.Cleanup(func() {
		if err := auth.Close(); err != nil {
			t.Errorf("staff.Auth.Close() error = %v", err)
		}
	})
	accessClient := auth.Access()

	roles := loadRoles(t)
	if err := access.MigrateRoles(ctx, accessClient.UserManager(), router.Collection(), roles); err != nil {
		t.Fatalf("access.MigrateRoles() error = %v", err)
	}

	passwordAuth := auth.Session()
	password := adminPassword
	if _, err := passwordAuth.API().CreateSessionUser(ctx, &session.CreateUserRequest{Username: adminUser, Password: &password}); err != nil {
		t.Fatalf("CreateSessionUser() error = %v", err)
	}
	if err := accessClient.UserManager().AddUserRoles(ctx, accesstypes.GlobalScope(), adminUser, "Administrator"); err != nil {
		t.Fatalf("AddUserRoles() error = %v", err)
	}

	handler := router.New(app.New(&servedConfigurer{db: db, auth: auth}))
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	return &served{server: server, access: accessClient}
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

// browser is one browser's view of the served application: a cookie jar and the XSRF
// token the session middleware issued into it.
type browser struct {
	t      *testing.T
	base   string
	client *http.Client
}

func newBrowser(t *testing.T, s *served) *browser {
	t.Helper()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}

	return &browser{t: t, base: s.server.URL, client: &http.Client{Jar: jar}}
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
