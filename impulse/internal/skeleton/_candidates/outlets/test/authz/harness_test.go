package authz

import (
	"context"
	"encoding/base64"
	"net/http"
	"testing"

	"github.com/cccteam/access"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/outlets/app"
	"github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/outlets/pkg/auth/members"
	"github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/outlets/pkg/auth/staff"
	"github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/outlets/pkg/router"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/live"
	initiator "github.com/cccteam/db-initiator"
	"github.com/cccteam/logger"
	"github.com/cccteam/session/sessioninfo"
	"github.com/go-playground/validator/v10"
)

// testUser is the assumed identity every request carries; the suites script what it is
// permitted to do per test via grants. testDomain is the generated matrix's one domain
// value, which newTestHandler adds to the test application's tenant roster.
const (
	testUser   = "authz-user"
	testDomain = "testDomain"
)

// fakeAccess scripts access.Controller's permission checks. Every Controller method
// the pipeline does not consume panics through the embedded nil interface, keeping the
// fake honest about what the pipeline actually draws on.
type fakeAccess struct {
	access.Controller
	g grants
}

func (f *fakeAccess) ForUser(user accesstypes.User) *access.UserChecker {
	return access.NewUserChecker(f, user)
}

func (f *fakeAccess) ForRole(role accesstypes.Role) *access.RoleChecker {
	return access.NewRoleChecker(f, role)
}

// UserHasGrants answers the concealed-domain foothold question the generated DomainGuard
// and the consolidated dispatcher ask after the tenant roster: the scripted table is
// domain-blind, so any grant at all is a foothold in the matrix's domain, and a case
// carrying no grants has no foothold and is answered as if the domain did not exist, per
// the generated suite's contract.
func (f *fakeAccess) UserHasGrants(_ context.Context, _ accesstypes.User, _ accesstypes.Scope) (bool, error) {
	return len(f.g) > 0, nil
}

func (f *fakeAccess) CheckUserResources(_ context.Context, _ accesstypes.Environment, _ accesstypes.User, _ accesstypes.Scope, perm accesstypes.Permission, resources ...accesstypes.Resource) (accesstypes.Decisions, error) {
	return f.decide(perm, resources), nil
}

func (f *fakeAccess) CheckRoleResources(_ context.Context, _ accesstypes.Environment, _ accesstypes.Role, _ accesstypes.Scope, perm accesstypes.Permission, resources ...accesstypes.Resource) (accesstypes.Decisions, error) {
	return f.decide(perm, resources), nil
}

func (f *fakeAccess) decide(perm accesstypes.Permission, resources []accesstypes.Resource) accesstypes.Decisions {
	decisions := make(accesstypes.Decisions, len(resources))
	for _, res := range resources {
		if f.g[perm] {
			decisions[res] = accesstypes.Granted()
		} else {
			decisions[res] = accesstypes.Denied()
		}
	}

	return decisions
}

// testConfigurer implements app.Configurer over the test dependencies, so the App is
// assembled through the same seam main assembles it through. Session is nil: the App
// owns no router, these suites compose the API surface through router.NewTestRouter,
// and nothing on that path touches the session.
type testConfigurer struct {
	db      *initiator.SpannerDB
	g       grants
	live    *live.Fake
	tenants *resource.TenantRoster
}

func (c *testConfigurer) ResourceClient() resource.Client {
	return resource.NewSpannerClient(c.db.Client)
}

// CursorKey seals the cursors the suites' paged lists issue; any key serves a test process.
func (c *testConfigurer) CursorKey() *resource.CursorKey {
	key, err := resource.NewCursorKey(base64.StdEncoding.EncodeToString([]byte("skeleton-test-cursor-key-material!!")))
	if err != nil {
		panic(err)
	}

	return key
}

func (c *testConfigurer) Access() access.Controller {
	return &fakeAccess{g: c.g}
}

func (c *testConfigurer) Staff() *staff.Auth { return nil }

func (c *testConfigurer) Members() *members.Auth { return nil }

func (c *testConfigurer) Validator() *validator.Validate {
	return validator.New()
}

func (c *testConfigurer) LogExporter() logger.Exporter {
	return logger.NewConsoleExporter()
}

// AppVersion is dev: the matrix drives the bare test router, which checks no version.
func (c *testConfigurer) AppVersion() string {
	return "dev"
}

func (c *testConfigurer) ConsoleDist() string { return "" }

func (c *testConfigurer) PortalDist() string { return "" }

// MachinesAPIKey is unused by these suites: the matrix drives the bare test router,
// which carries no outlet middleware.
func (c *testConfigurer) MachinesAPIKey() string { return "authz-machines-key" }

// Live is an in-memory live service: the live service is required in every
// application, so the suites' App starts with a fake nothing subscribes through.
func (c *testConfigurer) Live() live.Service {
	if c.live == nil {
		c.live = live.NewFake()
	}

	return c.live
}

// LiveOrigins names no change feed origin: the suites serve no live pages.
func (c *testConfigurer) LiveOrigins() []string {
	return nil
}

// TenantRoster is the suites' tenant roster: the generated matrix's one domain value,
// added rather than read, since the empty test schema holds no tenant row and nothing
// starts the roster. resource.SessionPermissions filters it by the case's grants, and
// under concealed domains a case carrying no grants has no foothold and is answered as
// if the domain did not exist, which the scripted engine's UserHasGrants decides.
func (c *testConfigurer) TenantRoster() *resource.TenantRoster {
	return c.tenants
}

// newTestHandler composes the pipeline under test: the application's generated
// handlers served through the generated test router, behind an identity-seeding
// wrapper that stands in for the session middleware.
func newTestHandler(t *testing.T, db *initiator.SpannerDB, g grants) http.Handler {
	t.Helper()

	roster := app.NewTenantRoster(resource.NewSpannerClient(db.Client))
	roster.Add(testDomain)
	a := app.New(&testConfigurer{db: db, g: g, tenants: roster})
	// The App reads its feature flags as it is built, so a suite that flips a flag
	// before newTestHandler drives the App in that state; Start reports a copy that
	// could not be read and follows the table until the test ends.
	if err := a.Start(t.Context()); err != nil {
		t.Fatalf("app.Start() error = %v", err)
	}

	return withIdentity(testUser, router.NewTestRouter(a))
}

// withIdentity seeds the session identity the way the session middleware would, making
// the pipeline's identity assumption explicit: these suites test authorization, never
// authentication.
func withIdentity(user string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), sessioninfo.CtxSessionInfo, &sessioninfo.SessionData{
			SessionInfo: &sessioninfo.SessionInfo{Username: user},
		})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
