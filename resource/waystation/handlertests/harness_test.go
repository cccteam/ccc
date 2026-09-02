package handlertests

import (
	"context"
	"net/http"
	"testing"

	"github.com/cccteam/access"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/waystation/app"
	"github.com/cccteam/ccc/resource/waystation/pkg/router"
	"github.com/cccteam/ccc/resource/waystation/pkg/rpc"
	initiator "github.com/cccteam/db-initiator"
	"github.com/cccteam/session"
	"github.com/cccteam/session/sessioninfo"
	"github.com/go-playground/validator/v10"
)

// testUser is the assumed identity every request carries; the suites script what it is
// permitted to do per test via grants.
const testUser = "handlertests-user"

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

func (f *fakeAccess) CheckUserResources(_ context.Context, _ accesstypes.Environment, _ accesstypes.User, _ accesstypes.Scope, perm accesstypes.Permission, resources ...accesstypes.Resource) (accesstypes.Decisions, error) {
	decisions := make(accesstypes.Decisions, len(resources))
	for _, res := range resources {
		if f.g[perm] {
			decisions[res] = accesstypes.Granted()
		} else {
			decisions[res] = accesstypes.Denied()
		}
	}

	return decisions, nil
}

// testConfigurer implements app.Configurer over the test dependencies, so the App is
// assembled through the same seam main assembles it through. Session is nil: the App
// owns no router, these suites compose the API surface through router.NewTestRouter,
// and nothing on that path touches the session.
type testConfigurer struct {
	db *initiator.SpannerDB
	g  grants
}

func (c *testConfigurer) ResourceClient() resource.Client {
	return resource.NewSpannerClient(c.db.Client)
}

func (c *testConfigurer) RPCClient() *rpc.Client {
	return rpc.NewClient()
}

func (c *testConfigurer) Access() access.Controller {
	return &fakeAccess{g: c.g}
}

func (c *testConfigurer) Session() *session.PasswordAuth[session.NoCustomData, session.NoCustomData] {
	return nil
}

func (c *testConfigurer) Validator() *validator.Validate {
	return validator.New()
}

func (c *testConfigurer) GuiDist() string { return "" }

// AutomationAPIKey is unused by these suites: the matrix drives the bare test router,
// which carries no outlet middleware.
func (c *testConfigurer) AutomationAPIKey() string { return "handlertests-automation-key" }

// DomainVisible recognizes the generated matrix's domain value and honors the
// scripted grants, per the generated suite's concealed-domain contract: a case
// carrying no grants has no foothold and is answered as if the domain did not
// exist. The empty test schema holds no waystation rows, so the tenancy roster is
// scripted rather than read from the table.
func (c *testConfigurer) DomainVisible(_ context.Context, _ accesstypes.User, domain accesstypes.Domain) (bool, error) {
	return domain == "testDomain" && len(c.g) > 0, nil
}

// Domains returns the scripted tenancy roster; nothing in the matrix consumes it.
func (c *testConfigurer) Domains(_ context.Context) ([]accesstypes.Domain, error) {
	return []accesstypes.Domain{"testDomain"}, nil
}

// newTestHandler composes the pipeline under test: the application's generated
// handlers served through the generated test router, behind an identity-seeding
// wrapper that stands in for the session middleware.
func newTestHandler(t *testing.T, db *initiator.SpannerDB, g grants) http.Handler {
	t.Helper()

	return withIdentity(testUser, router.NewTestRouter(app.New(&testConfigurer{db: db, g: g})))
}

// withIdentity seeds the session identity the way the session middleware would,
// making the pipeline's identity assumption explicit: these suites test
// authorization, never authentication.
func withIdentity(user string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), sessioninfo.CtxSessionInfo, &sessioninfo.SessionData{
			SessionInfo: &sessioninfo.SessionInfo{Username: user},
		})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
