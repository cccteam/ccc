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
	"github.com/cccteam/ccc/resource/lodestar/app"
	"github.com/cccteam/ccc/resource/lodestar/pkg/router"
	"github.com/cccteam/ccc/resource/lodestar/pkg/rpc"
	initiator "github.com/cccteam/db-initiator"
	"github.com/cccteam/session"
	"github.com/cccteam/session/sessioninfo"
	"github.com/go-playground/validator/v10"
)

// The suites run against the shipped demo world: schema/demoseed is the fixture, so
// the data a human sees in the running product is the data the regression suite pins.
const (
	migrationsSource = "file://../../schema/migrations"
	demoSeedSource   = "file://../../schema/demoseed"
)

// Seeded row identifiers, matching schema/demoseed. The values are patterned and
// stable so tests can address rows directly. The comments beside each mission are
// the facts the §7 conditions read.
const (
	anvil   = "anvil"
	bastion = "bastion"
	cinder  = "cinder"

	clientHalvardID  = "10000000-0000-4000-8000-000000000001" // trusted; Cleo's company
	clientMeridianID = "10000000-0000-4000-8000-000000000002" // trusted
	clientBastionID  = "10000000-0000-4000-8000-000000000003" // NOT trusted
	clientVellumID   = "10000000-0000-4000-8000-000000000004" // trusted

	shipClassKestrelID = "20000000-0000-4000-8000-000000000001"

	wingForgeID = "40000000-0000-4000-8000-000000000001" // anvil

	squadronHammerID     = "50000000-0000-4000-8000-000000000001" // Forge Wing, anvil: lead, veteran, wingco, dispatcher
	squadronTongsID      = "50000000-0000-4000-8000-000000000002" // Forge Wing, anvil: dispatcher, pilot
	squadronPortcullisID = "50000000-0000-4000-8000-000000000003" // Rampart Wing, bastion: dispatcher, pilot
	squadronAshfallID    = "50000000-0000-4000-8000-000000000004" // Ember Wing, cinder

	hangarAnvilDockID  = "60000000-0000-4000-8000-000000000001" // zone dock
	hangarQuarantineID = "60000000-0000-4000-8000-000000000002" // zone quarantine

	shipKingfisherID    = "70000000-0000-4000-8000-000000000001" // Anvil Dock One
	shipStubbornMuleID  = "70000000-0000-4000-8000-000000000002" // Anvil Dock One
	shipLanternID       = "70000000-0000-4000-8000-000000000003" // Quarantine Bay
	shipGoodSamaritanID = "70000000-0000-4000-8000-000000000004" // Anvil Dock One
	shipRustyAnchorID   = "70000000-0000-4000-8000-000000000005" // Anvil Dock One, scrapped refit
	shipBastionWatchID  = "70000000-0000-4000-8000-000000000006" // bastion
	shipCinderMothID    = "70000000-0000-4000-8000-000000000007" // cinder

	missionHaulerID     = "80000000-0000-4000-8000-000000000001" // anvil open, hazard 2, fee 8000, no cert, Halvard, booking
	missionCorvidID     = "80000000-0000-4000-8000-000000000002" // anvil claimed (Hammer), hazard 3, fee 24000, salvage, Meridian, booking, deadline now+3m
	missionConvoyID     = "80000000-0000-4000-8000-000000000003" // anvil underway (Hammer), hazard 4, fee 15000, escort, Meridian, marshal
	missionCourierID    = "80000000-0000-4000-8000-000000000004" // anvil on_hold (Tongs), hazard 1, fee 3000, no cert, Vellum, booking
	missionPodID        = "80000000-0000-4000-8000-000000000005" // anvil completed (Hammer), hazard 5, fee 40000, hazmat, Halvard, governor, settlement 38500
	missionTowID        = "80000000-0000-4000-8000-000000000006" // anvil failed (Tongs), hazard 1, fee 2000, no cert, Halvard, dispatcher
	missionBullionID    = "80000000-0000-4000-8000-000000000007" // anvil stood_down, hazard 3, fee 12000, escort, Bastion Relay, booking
	missionQuarantineID = "80000000-0000-4000-8000-000000000008" // anvil open, hazard 2, fee 6000, no cert, Halvard, booking, deadline 2026-08-20 (overdue)
	missionBeaconID     = "80000000-0000-4000-8000-000000000009" // bastion open, hazard 2, fee 5000, Bastion Relay, dispatcher
	missionPatrolID     = "80000000-0000-4000-8000-000000000010" // bastion claimed (Portcullis), hazard 4, fee 20000, escort, governor
	missionSweepID      = "80000000-0000-4000-8000-000000000011" // cinder completed, hazard 5, fee 60000

	sortieConvoyID  = "90000000-0000-4000-8000-000000000001" // on the underway convoy, Kingfisher, lead, open
	sortiePodID     = "90000000-0000-4000-8000-000000000002" // on the completed pod mission, returned
	sortieCourierID = "90000000-0000-4000-8000-000000000003" // on the on_hold courier, Good Samaritan, pilot, open
	sortieTowID     = "90000000-0000-4000-8000-000000000004" // on the failed tow, returned

	expenseConvoyFuelID  = "91000000-0000-4000-8000-000000000001" // 1200 on the convoy sortie (underway)
	expenseConvoyMedID   = "91000000-0000-4000-8000-000000000002" // 300 on the convoy sortie
	expensePodTowGearID  = "91000000-0000-4000-8000-000000000003" // 1500 on the pod sortie (completed)
	expenseCourierFuelID = "91000000-0000-4000-8000-000000000004" // 400 on the courier sortie (on_hold)

	refitLanternID      = "a0000000-0000-4000-8000-000000000001" // anvil docked, InspectedAt NULL, estimate NULL
	refitMuleID         = "a0000000-0000-4000-8000-000000000002" // anvil inspected, estimate 12000
	refitSamaritanID    = "a0000000-0000-4000-8000-000000000003" // anvil in_refit, estimate 8000
	refitRustyAnchorID  = "a0000000-0000-4000-8000-000000000004" // anvil scrapped
	refitBastionWatchID = "a0000000-0000-4000-8000-000000000005" // bastion flight_test
	refitCinderMothID   = "a0000000-0000-4000-8000-000000000006" // cinder cleared

	consignmentPodID     = "b0000000-0000-4000-8000-000000000001" // anvil, Halvard, expires 2026-12-01, in bond
	consignmentDronesID  = "b0000000-0000-4000-8000-000000000002" // anvil, Meridian, expired 2026-08-01, in bond
	consignmentBullionID = "b0000000-0000-4000-8000-000000000003" // anvil, Halvard, released
	consignmentRelayID   = "b0000000-0000-4000-8000-000000000004" // bastion

	callBeaconID = "d0000000-0000-4000-8000-000000000001" // anvil, filed by client (Cleo), contact set
	callDebrisID = "d0000000-0000-4000-8000-000000000002" // anvil, filed by cadet, contact NULL
	callRelayID  = "d0000000-0000-4000-8000-000000000003" // bastion, filed by dispatcher
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
// every known sector — the domain-guard suite passes empty grants to probe the
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
// Session is nil unless a suite supplies one (the impersonation suite does): the App
// owns no router, these suites compose the API surface through router.NewTestRouter,
// and nothing on that path touches the session.
type testConfigurer struct {
	db            *initiator.SpannerDB
	access        access.Controller
	session       *session.PasswordAuth[session.NoCustomData, session.NoCustomData]
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
	return c.session
}

func (c *testConfigurer) Validator() *validator.Validate {
	return validator.New()
}

func (c *testConfigurer) ConsoleDist() string { return "" }

func (c *testConfigurer) PortalDist() string { return "" }

// DroidAPIKey is unused by these suites: they drive the bare test router, which
// carries no outlet middleware.
func (c *testConfigurer) DroidAPIKey() string { return "integration-droid-key" }

func (c *testConfigurer) DomainVisible(ctx context.Context, user accesstypes.User, domain accesstypes.Domain) (bool, error) {
	return c.domainVisible(ctx, user, domain)
}

// Domains mirrors the production tenancy roster.
func (c *testConfigurer) Domains(_ context.Context) ([]accesstypes.Domain, error) {
	return []accesstypes.Domain{anvil, bastion, cinder}, nil
}

// demoDomainsExist is the production tenancy roster over the demo world: the three
// seeded sectors are the domains.
func demoDomainsExist(domain accesstypes.Domain) bool {
	return domain == anvil || domain == bastion || domain == cinder
}

// domainVisibleVia composes the demo roster with the engine's foothold answer —
// the same composition production's Configuration.DomainVisible performs. Sector
// existence is concealed: a caller with no grants in a sector is answered as if the
// sector did not exist.
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
// the bootstrap-parity suites pass the real one.
func newTestAppWithAccess(db *initiator.SpannerDB, controller access.Controller) http.Handler {
	return router.NewTestRouter(newApp(db, controller))
}

// newApp assembles the App over the test database and a permission engine.
func newApp(db *initiator.SpannerDB, controller access.Controller) *app.App {
	return app.New(&testConfigurer{
		db:            db,
		access:        controller,
		domainVisible: domainVisibleVia(controller),
	})
}

// doRequest performs a request against the app as the suite's default user and
// returns the status code and body.
func doRequest(t *testing.T, h http.Handler, method, target, body string) (statusCode int, respBody []byte) {
	t.Helper()

	return doRequestAs(t, h, "integration-test-user", method, target, body)
}

// doRequestAs performs a request against the app as the given user. The
// bootstrap-parity suites run against the real permission engine, where the acting
// user decides outcomes; every other suite scripts its checks and uses the default
// user.
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

// idsOf returns the id values of list rows, sorted.
func idsOf(t *testing.T, rows []map[string]any) []string {
	t.Helper()

	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		id, _ := row["id"].(string)
		ids = append(ids, id)
	}
	slices.Sort(ids)

	return ids
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

// fieldResource names a field-level permission target.
func fieldResource(res accesstypes.Resource, tag string) accesstypes.Resource {
	return accesstypes.Resource(string(res) + "." + tag)
}

// withFields expands a resource grant to the resource plus the named fields.
func withFields(res accesstypes.Resource, fields ...string) []accesstypes.Resource {
	out := []accesstypes.Resource{res}
	for _, f := range fields {
		out = append(out, fieldResource(res, f))
	}

	return out
}

// sectorPath builds a sector-scoped API path.
func sectorPath(sector, rest string) string {
	return "/api/sectors/" + sector + "/" + rest
}

// opPath builds a consolidated-operation path under a sector.
func opPath(sector, rest string) string {
	return "/sectors/" + sector + "/" + rest
}
