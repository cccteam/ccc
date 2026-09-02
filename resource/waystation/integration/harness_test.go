package integration

import (
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"cloud.google.com/go/spanner"
	"github.com/cccteam/access"
	"github.com/cccteam/ccc"
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

// The suites run against the shipped demo world: schema/demoseed is the fixture, so
// the data a human sees in the running product is the data the regression suite pins.
const (
	migrationsSource = "file://../schema/migrations"
	demoSeedSource   = "file://../schema/demoseed"
)

// Seeded row identifiers, matching schema/demoseed. The values are patterned and
// stable so tests can address rows directly.
const (
	wsAlpha = "ws-alpha"
	wsBeta  = "ws-beta"
	wsCeres = "ws-ceres"

	supplierRedlineID = "10000000-0000-4000-8000-000000000003" // inactive

	catScrubberID = "20000000-0000-4000-8000-000000000001" // 120.50
	catPumpID     = "20000000-0000-4000-8000-000000000002" // 890.00
	catTorchID    = "20000000-0000-4000-8000-000000000003" // 445.25

	moduleHabitatID = "40000000-0000-4000-8000-000000000001"

	facilityHydroponicsID = "50000000-0000-4000-8000-000000000001"
	facilityReactorCtlID  = "50000000-0000-4000-8000-000000000005"

	assetRecyclerID = "60000000-0000-4000-8000-000000000001" // habitat zone
	assetManifoldID = "60000000-0000-4000-8000-000000000005" // reactor zone
	assetBetaAirID  = "60000000-0000-4000-8000-000000000006" // ws-beta

	teamAlphaMechID = "70000000-0000-4000-8000-000000000001"
	teamAlphaLifeID = "70000000-0000-4000-8000-000000000002"
	teamBetaMaintID = "70000000-0000-4000-8000-000000000003"

	woScrubberID = "80000000-0000-4000-8000-000000000001" // in_progress, team mech
	woOvenID     = "80000000-0000-4000-8000-000000000002" // scheduled, team life
	woManifoldID = "80000000-0000-4000-8000-000000000003" // draft, unassigned
	woCraneID    = "80000000-0000-4000-8000-000000000004" // completed, team mech
	woBetaAirID  = "80000000-0000-4000-8000-000000000005" // ws-beta, scheduled

	reqScrubberID = "90000000-0000-4000-8000-000000000001" // submitted, 361.50, foreman
	reqPumpID     = "90000000-0000-4000-8000-000000000002" // draft, 0, foreman
	reqOverhaulID = "90000000-0000-4000-8000-000000000003" // submitted, 7120.00, quartermaster
	reqTorchID    = "90000000-0000-4000-8000-000000000004" // approved, 445.25, foreman
	reqBetaID     = "90000000-0000-4000-8000-000000000005" // ws-beta, submitted, 241.00, commander

	lotFreshID    = "a0000000-0000-4000-8000-000000000001" // expires 2027-03-01
	lotExpiredID  = "a0000000-0000-4000-8000-000000000002" // expired 2026-05-01
	lotNoExpiryID = "a0000000-0000-4000-8000-000000000003" // NULL expiry

	shipmentPendingID = "b0000000-0000-4000-8000-000000000001" // in transit
	shipmentArrivedID = "b0000000-0000-4000-8000-000000000002" // arrived

	incidentCoolantID = "c0000000-0000-4000-8000-000000000001"
)

// grants is a static permission table: the set of resources granted for each permission.
type grants map[accesstypes.Permission][]accesstypes.Resource

// staticAccess scripts access.Controller's permission checks over a grants table.
// Every Controller method the pipeline does not consume panics through the embedded
// nil interface, keeping the fake honest about what the pipeline actually draws on.
type staticAccess struct {
	access.Controller
	g grants
}

func (s *staticAccess) ForUser(user accesstypes.User) *access.UserChecker {
	return access.NewUserChecker(s, user)
}

// UserHasGrants answers the concealed-tenancy foothold question over the static
// grant table. The table is domain-blind, so any grant at all is a foothold in
// every known station — the domain-guard suite passes empty grants to probe the
// no-foothold collapse.
func (s *staticAccess) UserHasGrants(_ context.Context, _ accesstypes.User, _ accesstypes.Scope) (bool, error) {
	return len(s.g) > 0, nil
}

func (s *staticAccess) CheckUserResources(_ context.Context, _ accesstypes.Environment, _ accesstypes.User, _ accesstypes.Scope, perm accesstypes.Permission, resources ...accesstypes.Resource) (accesstypes.Decisions, error) {
	decisions := make(accesstypes.Decisions, len(resources))
	for _, res := range resources {
		if slices.Contains(s.g[perm], res) {
			decisions[res] = accesstypes.Granted()
		} else {
			decisions[res] = accesstypes.Denied()
		}
	}

	return decisions, nil
}

// testConfigurer implements app.Configurer over the test dependencies, so the App is
// assembled through the same seam main assembles it through. The permission engine
// and tenancy source are injectable: each suite scripts them through the same
// Configurer methods production wires to the real engine and the tenant table.
// Session is nil: the App owns no router, these suites compose the API surface
// through router.NewTestRouter, and nothing on that path touches the session.
type testConfigurer struct {
	db            *initiator.SpannerDB
	access        access.Controller
	domainVisible func(ctx context.Context, user accesstypes.User, domain accesstypes.Domain) (bool, error)
}

func (c *testConfigurer) ResourceClient() resource.Client {
	return resource.NewSpannerClient(c.db.Client)
}

func (c *testConfigurer) RPCClient() *rpc.Client {
	return rpc.NewClient()
}

func (c *testConfigurer) Access() access.Controller {
	return c.access
}

func (c *testConfigurer) Session() *session.PasswordAuth[session.NoCustomData, session.NoCustomData] {
	return nil
}

func (c *testConfigurer) Validator() *validator.Validate {
	return validator.New()
}

func (c *testConfigurer) GuiDist() string { return "" }

// AutomationAPIKey is unused by these suites: they drive the bare test router, which
// carries no outlet middleware.
func (c *testConfigurer) AutomationAPIKey() string { return "integration-automation-key" }

func (c *testConfigurer) DomainVisible(ctx context.Context, user accesstypes.User, domain accesstypes.Domain) (bool, error) {
	return c.domainVisible(ctx, user, domain)
}

// Domains mirrors the production tenancy roster; only SessionData consumes it, which
// these suites do not drive.
func (c *testConfigurer) Domains(_ context.Context) ([]accesstypes.Domain, error) {
	return []accesstypes.Domain{wsAlpha, wsBeta, wsCeres}, nil
}

// demoDomainsExist is the production tenancy roster over the demo world: the three
// seeded waystations are the domains.
func demoDomainsExist(domain accesstypes.Domain) bool {
	return domain == wsAlpha || domain == wsBeta || domain == wsCeres
}

// domainVisibleVia composes the demo roster with the engine's foothold answer —
// the same composition production's Configuration.DomainVisible performs.
// Waystation existence is concealed: a caller with no grants in a station is
// answered as if the station did not exist.
func domainVisibleVia(controller access.Controller) func(ctx context.Context, user accesstypes.User, domain accesstypes.Domain) (bool, error) {
	return func(ctx context.Context, user accesstypes.User, domain accesstypes.Domain) (bool, error) {
		if !demoDomainsExist(domain) {
			return false, nil
		}

		return controller.UserHasGrants(ctx, user, accesstypes.DomainScope(domain))
	}
}

// newTestApp builds the application with the given permission table backing every
// request, served through the generated test router.
func newTestApp(db *initiator.SpannerDB, g grants) http.Handler {
	return newTestAppWithAccess(db, &staticAccess{g: g})
}

// newTestAppWithAccess builds the application over an arbitrary permission engine —
// the conditions suite passes the real one.
func newTestAppWithAccess(db *initiator.SpannerDB, controller access.Controller) http.Handler {
	return router.NewTestRouter(app.New(&testConfigurer{
		db:            db,
		access:        controller,
		domainVisible: domainVisibleVia(controller),
	}))
}

// doRequest performs a request against the app as the suite's default user and
// returns the status code and body.
func doRequest(t *testing.T, h http.Handler, method, target, body string) (statusCode int, respBody []byte) {
	t.Helper()

	return doRequestAs(t, h, "integration-test-user", method, target, body)
}

// doRequestAs performs a request against the app as the given user. The conditions
// suite runs against the real permission engine, where the acting user decides
// outcomes; every other suite scripts its checks and uses the default user.
func doRequestAs(t *testing.T, h http.Handler, user accesstypes.User, method, target, body string) (statusCode int, respBody []byte) {
	t.Helper()

	reader := strings.NewReader(body)

	// Generated mutation handlers derive their change-event source from the session,
	// which production apps establish via session middleware. The harness injects a
	// synthetic one.
	sessionID, err := ccc.NewUUID()
	if err != nil {
		t.Fatalf("ccc.NewUUID: %v", err)
	}
	ctx := context.WithValue(t.Context(), sessioninfo.CtxSessionInfo, &sessioninfo.SessionData{
		SessionInfo: &sessioninfo.SessionInfo{
			ID:       sessionID,
			Username: string(user),
		},
	})

	req := httptest.NewRequestWithContext(ctx, method, target, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	return rr.Code, rr.Body.Bytes()
}

// decodeRows decodes a list response body into rows.
func decodeRows(t *testing.T, body []byte) []map[string]any {
	t.Helper()

	var rows []map[string]any
	if err := json.Unmarshal(body, &rows); err != nil {
		t.Fatalf("decodeRows: %v: %s", err, body)
	}

	return rows
}

// decodeRow decodes a single-resource response body into a row.
func decodeRow(t *testing.T, body []byte) map[string]any {
	t.Helper()

	var row map[string]any
	if err := json.Unmarshal(body, &row); err != nil {
		t.Fatalf("decodeRow: %v: %s", err, body)
	}

	return row
}

// rowsByID indexes list rows by their id key.
func rowsByID(t *testing.T, rows []map[string]any, key string) map[string]map[string]any {
	t.Helper()

	indexed := make(map[string]map[string]any, len(rows))
	for _, row := range rows {
		id, _ := row[key].(string)
		indexed[id] = row
	}

	return indexed
}

// assertKeys asserts that a row contains exactly the wanted JSON keys.
func assertKeys(t *testing.T, row map[string]any, wantKeys []string) {
	t.Helper()

	got := slices.Sorted(maps.Keys(row))
	want := slices.Sorted(slices.Values(wantKeys))
	if !slices.Equal(got, want) {
		t.Errorf("response keys = %v, want %v", got, want)
	}
}

// assertStatus asserts the response status code, printing the body on mismatch.
func assertStatus(t *testing.T, gotStatus, wantStatus int, body []byte) {
	t.Helper()

	if gotStatus != wantStatus {
		t.Fatalf("status = %d, want %d: %s", gotStatus, wantStatus, body)
	}
}

// readColumn reads one column of one row directly from the database.
func readColumn[T any](ctx context.Context, t *testing.T, db *initiator.SpannerDB, table string, key spanner.Key, column string) T {
	t.Helper()

	row, err := db.Single().ReadRow(ctx, table, key, []string{column})
	if err != nil {
		t.Fatalf("ReadRow(%s): %v", table, err)
	}

	var value T
	if err := row.Columns(&value); err != nil {
		t.Fatalf("row.Columns(%s.%s): %v", table, column, err)
	}

	return value
}
