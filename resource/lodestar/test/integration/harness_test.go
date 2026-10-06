package integration

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"cloud.google.com/go/spanner"
	"github.com/cccteam/access"
	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/filestore"
	"github.com/cccteam/ccc/resource/jobs"
	"github.com/cccteam/ccc/resource/live"
	"github.com/cccteam/ccc/resource/lodestar/app"
	"github.com/cccteam/ccc/resource/lodestar/pkg/auth"
	"github.com/cccteam/ccc/resource/lodestar/pkg/auth/crew"
	"github.com/cccteam/ccc/resource/lodestar/pkg/auth/members"
	"github.com/cccteam/ccc/resource/lodestar/pkg/resources"
	"github.com/cccteam/ccc/resource/lodestar/pkg/router"
	"github.com/cccteam/ccc/resource/scheduled"
	initiator "github.com/cccteam/db-initiator"
	"github.com/cccteam/httpio"
	"github.com/cccteam/logger"
	"github.com/cccteam/session"
	"github.com/cccteam/session/sessioninfo"
	"github.com/go-chi/chi/v5"
	"github.com/go-playground/validator/v10"
)

// The suites run against the shipped demo world: schema/devseed is the fixture, so the
// data a human sees in the running product is the data the regression suite pins.
const (
	migrationsSource = "file://../../schema/migrations"
	demoSeedSource   = "file://../../schema/devseed"
	devSeedSource    = demoSeedSource
	usersPath        = "../../cmd/bootstrap/users.json"

	// The browser outlets' API prefixes: each sits under its application's mount path.
	consoleAPI = "/console/api"
	portalAPI  = "/portal/api"

	// The droids outlet's API key in the suites.
	droidsAPIKey = "integration-droids-key"

	// Every persona's password.
	personaPassword = "lodestar"

	// The portal's client: a member of the members auth who signs in through the
	// simulated directory; its roles are the groups the directory names (APP_ROLES).
	clientUser = "client"
	droidUser  = "droid-r7"

	// The served world's idle session timeout, the application's default. A test signs in
	// in its body and reads in parallel subtests, which start when a slot frees; on a loaded
	// runner that was over a minute later, and a shorter timeout read as an expired session.
	servedSessionTimeout = 10 * time.Minute
)

// Seeded row identifiers, matching schema/devseed. The values are patterned and stable
// so tests can address rows directly. The comments beside each mission are the facts the
// §7 conditions read.
const (
	anvil   = "anvil"
	bastion = "bastion"
	cinder  = "cinder"

	clientHalvardID      = "10000000-0000-4000-8000-000000000001" // trusted, insured; Cleo's company
	clientMeridianID     = "10000000-0000-4000-8000-000000000002" // trusted, cover unknown (Insured NULL)
	clientBastionRelayID = "10000000-0000-4000-8000-000000000003" // NOT trusted, cover unknown (Insured NULL)
	clientVellumID       = "10000000-0000-4000-8000-000000000004" // trusted, NOT insured

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
	shipTinWhistleID    = "70000000-0000-4000-8000-000000000008" // Anvil Dock One
	shipPatientHeronID  = "70000000-0000-4000-8000-000000000009" // Anvil Dock One
	shipSecondChanceID  = "70000000-0000-4000-8000-000000000010" // Anvil Dock One
	shipBrassCompassID  = "70000000-0000-4000-8000-000000000011" // Anvil Dock One
	shipSlowBoatID      = "70000000-0000-4000-8000-000000000012" // Quarantine Bay

	missionHaulerID     = "80000000-0000-4000-8000-000000000001" // anvil open, hazard 2, fee 8000, no cert, Halvard, booking, deadline seed+15d
	missionCorvidID     = "80000000-0000-4000-8000-000000000002" // anvil claimed (Hammer), hazard 3, fee 24000, salvage, Meridian, booking, deadline now+3m
	missionConvoyID     = "80000000-0000-4000-8000-000000000003" // anvil underway (Hammer), hazard 4, fee 15000, escort, Meridian, marshal, deadline seed+25d
	missionCourierID    = "80000000-0000-4000-8000-000000000004" // anvil on_hold (Tongs), hazard 1, fee 3000, no cert, Vellum, booking, deadline seed+20d
	missionPodID        = "80000000-0000-4000-8000-000000000005" // anvil completed (Hammer), hazard 5, fee 40000, hazmat, Halvard, governor, settlement 38500
	missionTowID        = "80000000-0000-4000-8000-000000000006" // anvil failed (Tongs), hazard 1, fee 2000, no cert, Halvard, dispatcher
	missionBullionID    = "80000000-0000-4000-8000-000000000007" // anvil stood_down, hazard 3, fee 12000, escort, Bastion Relay, booking
	missionQuarantineID = "80000000-0000-4000-8000-000000000008" // anvil open, hazard 2, fee 6000, no cert, Halvard, booking, deadline seed-16d (overdue)
	missionBeaconID     = "80000000-0000-4000-8000-000000000009" // bastion open, hazard 2, fee 5000, Bastion Relay, dispatcher
	missionPatrolID     = "80000000-0000-4000-8000-000000000010" // bastion claimed (Portcullis), hazard 4, fee 20000, escort, governor
	missionSweepID      = "80000000-0000-4000-8000-000000000011" // cinder completed, hazard 5, fee 60000
	missionHesperID     = "80000000-0000-4000-8000-000000000015" // anvil underway (Tongs), hazard 4, fee 11000, escort, Bastion Relay, booking (filler)

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
	refitHeronID        = "a0000000-0000-4000-8000-000000000008" // anvil in_refit, estimate 4000, the live-pages suite's
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

// sectors is the seeded roster.
var sectors = []accesstypes.Domain{anvil, bastion, cinder}

// testCursorKey is the one cursor key the integration suites seal and open with.
var testCursorKey = func() *resource.CursorKey {
	key, err := resource.NewCursorKey(base64.StdEncoding.EncodeToString([]byte("lodestar-integration-cursor-key!")))
	if err != nil {
		panic(err)
	}

	return key
}()

// testCookieKey signs session cookies in the suites; any 32 bytes will do.
const testCookieKey = "dGVzdC1jb29raWUta2V5LXRlc3QtY29va2llLWtleS0xMjM0NTY="

// grants is a static permission table: the set of resources granted for each permission.
type grants map[accesstypes.Permission][]accesstypes.Resource

// staticAccess scripts access.Controller's permission checks over a grants table. Every
// Controller method the pipeline does not consume panics through the embedded nil
// interface, keeping the fake honest about what the pipeline actually draws on.
type staticAccess struct {
	access.Controller
	g grants
}

func (s *staticAccess) ForUser(user accesstypes.User) *access.UserChecker {
	return access.NewUserChecker(s, user)
}

func (s *staticAccess) ForRole(role accesstypes.Role) *access.RoleChecker {
	return access.NewRoleChecker(s, role)
}

// UserHasGrants answers the concealed-tenancy foothold question over the static grant
// table. The table is domain-blind, so any grant at all is a foothold in every known
// sector; the domain-guard suite passes empty grants to probe the no-foothold collapse.
func (s *staticAccess) UserHasGrants(_ context.Context, _ accesstypes.User, _ accesstypes.Scope) (bool, error) {
	return len(s.g) > 0, nil
}

func (s *staticAccess) CheckUserResources(_ context.Context, _ accesstypes.Environment, _ accesstypes.User, _ accesstypes.Scope, perm accesstypes.Permission, names ...accesstypes.Resource) (accesstypes.Decisions, error) {
	return s.decide(perm, names), nil
}

func (s *staticAccess) CheckRoleResources(_ context.Context, _ accesstypes.Environment, _ accesstypes.Role, _ accesstypes.Scope, perm accesstypes.Permission, names ...accesstypes.Resource) (accesstypes.Decisions, error) {
	return s.decide(perm, names), nil
}

func (s *staticAccess) decide(perm accesstypes.Permission, names []accesstypes.Resource) accesstypes.Decisions {
	decisions := make(accesstypes.Decisions, len(names))
	for _, res := range names {
		if slices.Contains(s.g[perm], res) {
			decisions[res] = accesstypes.Granted()
		} else {
			decisions[res] = accesstypes.Denied()
		}
	}

	return decisions
}

// testConfigurer implements app.Configurer over the test dependencies, so the App is
// assembled through the same seam main assembles it through. The permission engines and
// the tenancy source are injectable: each suite scripts them through the same Configurer
// methods production wires to the real engines and the sector table. The auths are nil
// unless a suite serves the full router (the served suites do): the test router composes
// the API surface alone, and nothing on that path touches a session.
type testConfigurer struct {
	db            *initiator.SpannerDB
	access        access.Controller
	membersAccess access.Controller
	crewAuth      *crew.Auth
	membersAuth   *members.Auth
	stores        *testStores
	live          live.Service
	management    access.Handlers
	tenants       *resource.TenantRoster
	// version is the release the served stack reports as its own (AppVersion); empty
	// is dev, under which the version check answers every release.
	version string
	// scheduler is the guard the scheduled routes sit behind on the served stack, over
	// a fake of Google's keys; nil elsewhere, which refuses every scheduled call.
	scheduler *scheduled.Guard
	// jobs is the job process's starter, a fake the served stack's suites read the
	// starts from; nil gets a fake of its own.
	jobs jobs.Starter
}

// TenantRoster is the application's tenant roster as production's DataConfiguration
// starts it: the generated constructor over the test client, holding the seeded sectors.
// A suite that passed none gets one holding the seed by Add, never read and never
// started; the run-time tenant suite passes a started roster of its own, so a sector it
// charts reaches the roster the way a served instance's does. The guard's foothold
// question is the engine's, through UserPermissions.HasGrants.
func (c *testConfigurer) TenantRoster() *resource.TenantRoster {
	if c.tenants == nil {
		c.tenants = app.NewSectorRoster(c.ResourceClient())
		for _, sector := range sectors {
			c.tenants.Add(sector)
		}
	}

	return c.tenants
}

// Stores are the two memory stores the App is wired over, the default store (the refit
// photos') and the Documents store (the mission documents'); a suite that asserts on the
// objects passes its own through newAppWithStores, every other suite gets a fresh pair.
func (c *testConfigurer) Stores() *testStores {
	if c.stores == nil {
		c.stores = newTestStores()
	}

	return c.stores
}

// testStores are the memory stores (mem://) an App under test is wired over, standing
// where production's data level opens APP_FILE_STORE and APP_FILE_STORE_DOCUMENTS.
type testStores struct {
	files     *filestore.Mem
	documents *filestore.Mem
}

func newTestStores() *testStores {
	return &testStores{files: filestore.NewMem(), documents: filestore.NewMem()}
}

// options wires the stores on a resource client as production's data level does: the
// default under resource.WithFileStore, the Documents store under its type.
func (s *testStores) options() []resource.ClientOption {
	return []resource.ClientOption{resource.WithFileStore(s.files), resource.WithNamedFileStore[resources.Documents](s.documents)}
}

// CursorKey seals the cursors the suites walk; one key per process is enough, since
// every suite's requests go through the same App.
func (c *testConfigurer) CursorKey() *resource.CursorKey { return testCursorKey }

// ResourceClient is the client over the test database and the two stores, as
// production's is: the generated frames read each store off it, and a committed
// transaction's released objects are deleted from their store.
func (c *testConfigurer) ResourceClient() resource.Client {
	return resource.NewSpannerClient(c.db.Client, c.Stores().options()...)
}

func (c *testConfigurer) Access() access.Controller { return c.access }

func (c *testConfigurer) MembersAccess() access.Controller { return c.membersAccess }

func (c *testConfigurer) Crew() *crew.Auth { return c.crewAuth }

func (c *testConfigurer) Members() *members.Auth { return c.membersAuth }

func (c *testConfigurer) Validator() *validator.Validate { return validator.New() }

func (c *testConfigurer) LogExporter() logger.Exporter { return logger.NewConsoleExporter() }

// AppVersion is the release the suite gave the stack (newServedAt), else dev, under
// which the served stack's version check answers every release, so no suite's request
// is refused for the release it carries.
func (c *testConfigurer) AppVersion() string {
	if c.version == "" {
		return "dev"
	}

	return c.version
}

func (c *testConfigurer) ConsoleDist() string { return "" }

func (c *testConfigurer) PortalDist() string { return "" }

// DroidsAPIKey is the droids outlet's key on the served stack; the test router carries no
// outlet middleware.
func (c *testConfigurer) DroidsAPIKey() string { return droidsAPIKey }

// Scheduler is the served stack's guard over the scheduled routes (newServedAt), which
// admits the tokens its fake issues for schedulerInvoker; nil for a suite on the test
// router, which mounts no scheduled route.
func (c *testConfigurer) Scheduler() *scheduled.Guard {
	return c.scheduler
}

// Jobs is the fake starter the suite passed (newServed), which records the starts the
// scheduled cleanup makes; a suite that passed none gets a fake of its own.
func (c *testConfigurer) Jobs() jobs.Starter {
	if c.jobs == nil {
		return jobs.NewFake()
	}

	return c.jobs
}

// Live is the live service the suite passed (newTestAppWithLive), an in-memory fake;
// a suite that passed none gets a fake of its own, since the live service is required
// in every application and the App's Start follows the feature flags through it.
func (c *testConfigurer) Live() live.Service {
	if c.live == nil {
		c.live = live.NewFake()
	}

	return c.live
}

// LiveOrigins names no change feed origin: the suites drive the test router, which
// carries no security headers.
func (c *testConfigurer) LiveOrigins() []string { return nil }

// UserManagement is the crew engine's user-management handlers where a suite mounts the
// role-membership routes (the served stack and the policy-signal suite); nil elsewhere,
// since the test router never mounts them.
func (c *testConfigurer) UserManagement() access.Handlers { return c.management }

// newTestApp builds the application with the given permission table backing every
// request, served through the generated test router.
func newTestApp(db *initiator.SpannerDB, g grants) http.Handler {
	return newTestAppWithAccess(db, &staticAccess{g: g})
}

// newTestAppWithAccess builds the application over an arbitrary permission engine; the
// bootstrap-parity suites pass the real crew engine.
func newTestAppWithAccess(db *initiator.SpannerDB, controller access.Controller) http.Handler {
	return withHandWrittenRoutes(newApp(db, controller, nil))
}

// newTestAppWithLive builds the application with the given permission table and a live
// service, so a suite can watch what the generated handlers register and publish.
func newTestAppWithLive(db *initiator.SpannerDB, g grants, svc live.Service) http.Handler {
	return withHandWrittenRoutes(app.New(&testConfigurer{db: db, access: &staticAccess{g: g}, live: svc}))
}

// newTestAppWithEngines builds the application over both real engines: the crew store
// for console requests and the members store for requests bound to the members auth.
func newTestAppWithEngines(db *initiator.SpannerDB, crewEngine, membersEngine access.Controller) http.Handler {
	return withHandWrittenRoutes(app.New(&testConfigurer{db: db, access: crewEngine, membersAccess: membersEngine}))
}

// withHandWrittenRoutes mounts the application's hand-written routes beside the generated
// test router, where production's router.New mounts them: the ship's log and the portal's
// client statement. The mission document download is generated, so it rides the test
// router with every other generated route.
func withHandWrittenRoutes(a *app.App) http.Handler {
	r := chi.NewRouter()
	r.Use(httpio.WithParams)
	r.Get(router.ShipsLogEntriesRoute, a.DomainGuard()(a.ShipsLogEntries()))
	r.Get(router.ClientStatementsRoute, a.DomainGuard()(a.ClientStatements()))
	r.Mount("/", router.NewTestRouter(a))

	return r
}

// newApp assembles the App over the test database, a permission engine, and the file
// stores (nil for a fresh pair).
func newApp(db *initiator.SpannerDB, controller access.Controller, stores *testStores) *app.App {
	return app.New(&testConfigurer{db: db, access: controller, membersAccess: membersEngineFor(controller), stores: stores})
}

// membersEngineFor pairs the shared crew engine with the shared members engine, so a
// portal user's request under an app built over the parity engines answers from the
// members store; a scripted engine pairs with nothing.
func membersEngineFor(controller access.Controller) access.Controller {
	sharedMu.RLock()
	defer sharedMu.RUnlock()

	if sharedCrew != nil && controller == access.Controller(sharedCrew) {
		return sharedMembers
	}

	return nil
}

// missionID names a seeded mission by its ordinal in schema/devseed.
func missionID(n int) string {
	return fmt.Sprintf("80000000-0000-4000-8000-%012d", n)
}

// deadline is a mission deadline a suite writes: days from now, in RFC 3339. The seed
// writes every mission deadline relative to seed time (schema/devseed), so a suite's
// written deadline keeps its distance from the seeded one on any calendar day.
func deadline(days int) string {
	return time.Now().UTC().AddDate(0, 0, days).Format(time.RFC3339)
}

// newAppWithStores assembles the App over the given stores, for the suites that assert
// on the objects the upload frame writes.
func newAppWithStores(db *initiator.SpannerDB, controller access.Controller, stores *testStores) *app.App {
	return newApp(db, controller, stores)
}

// portalUsers are the identities that reach the application through the portal's session
// group, bound to the members auth: their checks answer from the members store.
var portalUsers = map[accesstypes.User]bool{clientUser: true}

// doRequest performs a request against the app as the suite's default user and returns
// the status code and body.
func doRequest(t *testing.T, h http.Handler, method, target, body string) (statusCode int, respBody []byte) {
	t.Helper()

	return doRequestAs(t, h, "integration-test-user", method, target, body)
}

// doRequestAs performs a request against the app as the given user. The bootstrap-parity
// suites run against the real engines, where the acting user decides outcomes; every
// other suite scripts its checks and uses the default user. A portal user's request is
// bound to the members auth, as the portal's session group binds it.
func doRequestAs(t *testing.T, h http.Handler, user accesstypes.User, method, target, body string) (statusCode int, respBody []byte) {
	t.Helper()

	rr := doRequestRecordedAs(t, h, user, method, target, body)

	return rr.Code, rr.Body.Bytes()
}

// doRequestRecorded performs a GET as the suite's default user and returns the full
// recorded response, headers included, for the suites that read the paging headers.
func doRequestRecorded(t *testing.T, h http.Handler, target string) *httptest.ResponseRecorder {
	t.Helper()

	return doRequestRecordedAs(t, h, "integration-test-user", http.MethodGet, target, "")
}

// doRequestRecordedAs performs a request as the given user and returns the recorded
// response.
func doRequestRecordedAs(t *testing.T, h http.Handler, user accesstypes.User, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()

	return doRequestRecordedWithHeaders(t, h, user, method, target, body, nil)
}

// doRequestRecordedWithHeaders performs a request as the given user with the given
// headers set on it, and returns the recorded response.
func doRequestRecordedWithHeaders(t *testing.T, h http.Handler, user accesstypes.User, method, target, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()

	// Generated mutation handlers derive their change-event source from the session,
	// which production apps establish via session middleware. The harness injects a
	// synthetic one.
	sessionID, err := ccc.NewUUID()
	if err != nil {
		t.Fatalf("ccc.NewUUID: %v", err)
	}
	ctx := context.WithValue(t.Context(), sessioninfo.CtxSessionInfo, &sessioninfo.SessionData{
		SessionInfo: &sessioninfo.SessionInfo{ID: sessionID, Username: string(user)},
	})
	if portalUsers[user] {
		ctx = auth.Bind(ctx, members.Name)
	}

	req := httptest.NewRequestWithContext(ctx, method, target, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for name, value := range headers {
		req.Header.Set(name, value)
	}

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	return rr
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

// cell reads one cell of a list row as T. A missing key or a value of another type
// is fatal, naming the key, the value and the row: it means the wire shape changed,
// and every later row would fail the same way, so the first message is the useful one.
func cell[T any](t *testing.T, row map[string]any, key string) T {
	t.Helper()

	raw, ok := row[key]
	if !ok {
		t.Fatalf("cell %q is missing from row %v", key, row)
	}
	value, ok := raw.(T)
	if !ok {
		t.Fatalf("cell %q = %v (%T), want %T, in row %v", key, raw, raw, value, row)
	}

	return value
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

// sectorPath builds a sector-scoped console API path.
func sectorPath(sector, rest string) string {
	return consoleAPI + "/sectors/" + sector + "/" + rest
}

// portalPath builds a sector-scoped portal API path.
func portalPath(sector, rest string) string {
	return portalAPI + "/sectors/" + sector + "/" + rest
}

// opPath builds a consolidated-operation path under a sector.
func opPath(sector, rest string) string {
	return "/sectors/" + sector + "/" + rest
}

// The served stack: the full router over real auths, for the suites that need sessions,
// cookies, the XSRF handshake, the outlets' prefixes, or the droids key.

// served is one running instance of the application under test.
type served struct {
	server *httptest.Server
	// access is the crew auth's engine, members the members auth's.
	access  *access.Client
	members *access.Client
	stores  *testStores
	db      *initiator.SpannerDB
	// scheduler stands in for Google's keys behind the scheduled routes: a token it
	// mints for schedulerInvoker and a route's URL is what Cloud Scheduler presents.
	scheduler *scheduled.Fake
	// jobs is the job process's fake starter: what the scheduled cleanup started.
	jobs *jobs.Fake
}

// newServed provisions the database the way the bootstrap does (schema, the demo world,
// the personas and the droid with their memberships; both auths' roles are the embedded
// files the engines validate when the auths open), and serves the full router. Under the
// skipAuth build the members auth's directory is simulated and its login returns straight
// to the callback; without the tag the suite skips.
func newServed(ctx context.Context, t *testing.T) *served {
	t.Helper()

	return newServedAt(ctx, t, "")
}

// newServedAt is the served stack reporting version as the release it was built from
// (the configuration's APP_VERSION): a release such as 1.4.0 arms the session outlets'
// version check against the oldest answered release the generator program declares,
// and "" is dev, which checks nothing.
func newServedAt(ctx context.Context, t *testing.T, version string) *served {
	t.Helper()
	if !simulatedDirectory() {
		t.Skip("the portal signs in through a directory: run this suite with -tags skipAuth to simulate it")
	}

	db, err := prepareDatabase(ctx, t, migrationsSource, demoSeedSource)
	if err != nil {
		t.Fatal(err)
	}

	// One in-memory live service for the stack: both engines announce and watch the
	// policy kind through it, and the App subscribes and publishes through it.
	fake := live.NewFake()
	policySignal := auth.PolicySignal(fake)
	crewAuth, err := crew.New(ctx, db.Client, crew.Settings{
		CookieKey:      testCookieKey,
		SessionTimeout: servedSessionTimeout,
		Collection:     router.Collection(),
		ChangeSignal:   policySignal,
	})
	if err != nil {
		t.Fatalf("crew.New() error = %v", err)
	}
	t.Cleanup(func() {
		if err := crewAuth.Close(); err != nil {
			t.Errorf("crew.Auth.Close() error = %v", err)
		}
	})

	// The server is allocated before the members auth so the auth's directory callback
	// can name the server's own address.
	server := httptest.NewUnstartedServer(nil)
	t.Cleanup(server.Close)

	membersAuth, err := members.New(ctx, db.Client, &members.Settings{
		CookieKey:      testCookieKey,
		SessionTimeout: servedSessionTimeout,
		LoginURL:       "/portal/login",
		Collection:     router.Collection(),
		Directory: members.Directory{
			RedirectURL:  "http://" + server.Listener.Addr().String() + portalAPI + "/user/callback",
			HostedDomain: "example.com",
			GroupPrefix:  "members-",
		},
		ChangeSignal: policySignal,
	})
	if err != nil {
		t.Fatalf("members.New() error = %v", err)
	}
	t.Cleanup(func() {
		if err := membersAuth.Close(); err != nil {
			t.Errorf("members.Auth.Close() error = %v", err)
		}
	})

	if err := provisionDemoAccess(ctx, crewAuth.Access(), membersAuth.Access()); err != nil {
		t.Fatal(err)
	}
	passwordAuth := crewAuth.Session()
	for _, user := range demoUsers(t) {
		password := user.Password
		if _, err := passwordAuth.API().CreateSessionUser(ctx, &session.CreateUserRequest{Username: string(user.User), Password: &password}); err != nil {
			t.Fatalf("CreateSessionUser(%s) error = %v", user.User, err)
		}
	}

	stores := newTestStores()
	keys := scheduled.NewFake()
	starter := jobs.NewFake()
	a := app.New(&testConfigurer{
		db:            db,
		access:        crewAuth.Access(),
		membersAccess: membersAuth.Access(),
		crewAuth:      crewAuth,
		membersAuth:   membersAuth,
		stores:        stores,
		live:          fake,
		management:    crewAuth.Access().Handlers(httpio.Log),
		version:       version,
		scheduler:     scheduled.NewGuard(schedulerInvoker, keys),
		jobs:          starter,
	})
	server.Config.Handler = router.New(a, router.AppHooks(a))
	server.Start()

	return &served{server: server, access: crewAuth.Access(), members: membersAuth.Access(), stores: stores, db: db, scheduler: keys, jobs: starter}
}

// schedulerInvoker is the invoker identity the served stack's scheduled routes admit, as
// the stack names it in APP_SCHEDULER_INVOKER on Cloud Run.
const schedulerInvoker = "lodestar-scheduler@lodestar.iam.gserviceaccount.com"

// outletXSRF names the XSRF cookie each session-serving outlet's auth issues: the browser
// on an outlet echoes that auth's cookie, as the outlet's web app does.
var outletXSRF = map[string]string{
	consoleAPI: crew.XSRFCookie,
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

// signIn primes the XSRF handshake and signs a persona in on the console, failing the
// test on refusal.
func (b *browser) signIn(ctx context.Context, user string) {
	b.t.Helper()

	if status, body := b.do(ctx, http.MethodGet, b.prefix+"/user/session", nil); status != http.StatusOK {
		b.t.Fatalf("GET %s/user/session before login: status %d: %s", b.prefix, status, body)
	}
	if status, body := b.login(ctx, user, personaPassword); status != http.StatusOK {
		b.t.Fatalf("login %s: status %d: %s", user, status, body)
	}
}

// session reads the session route as this browser.
func (b *browser) session(ctx context.Context) map[string]any {
	b.t.Helper()

	status, body := b.do(ctx, http.MethodGet, b.prefix+"/user/session", nil)
	if status != http.StatusOK {
		b.t.Fatalf("session: status = %d: %s", status, body)
	}
	var info map[string]any
	if err := json.Unmarshal(body, &info); err != nil {
		b.t.Fatalf("decoding session: %v", err)
	}

	return info
}

// droid issues one request as a droid carrying the bearer key (empty for an
// unauthenticated probe).
func droid(ctx context.Context, t *testing.T, s *served, key, method, path string, body []byte) (status int, respBody []byte) {
	t.Helper()

	req, err := http.NewRequestWithContext(ctx, method, s.server.URL+path, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	respBody, err = io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}

	return resp.StatusCode, respBody
}

// do issues one request as this browser, carrying its cookies and XSRF token.
func (b *browser) do(ctx context.Context, method, path string, body []byte) (status int, respBody []byte) {
	b.t.Helper()

	resp := b.doResponse(ctx, method, path, body)
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		b.t.Fatal(err)
	}

	return resp.StatusCode, respBody
}

// doResponse issues one request as this browser and returns the whole response, for
// suites that read headers; the caller closes the body.
func (b *browser) doResponse(ctx context.Context, method, path string, body []byte) *http.Response {
	b.t.Helper()

	return b.doWith(ctx, method, path, body, nil)
}

// doWith is doResponse with extra request headers, for a suite that speaks as a browser
// application built from a release (X-Api-Version); the caller closes the body.
func (b *browser) doWith(ctx context.Context, method, path string, body []byte, headers map[string]string) *http.Response {
	b.t.Helper()

	req, err := http.NewRequestWithContext(ctx, method, b.base+path, bytes.NewReader(body))
	if err != nil {
		b.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token := b.xsrfToken(); token != "" {
		req.Header.Set("X-XSRF-TOKEN", token)
	}
	for name, value := range headers {
		req.Header.Set(name, value)
	}

	resp, err := b.client.Do(req)
	if err != nil {
		b.t.Fatal(err)
	}

	return resp
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

// provesGrant names the conditional grant a test case proves. It parses the auth's
// embedded role file (crew.Roles() or members.Roles()) and fails unless a grant for the
// role, permission, and resource carries exactly that condition text, so a case whose
// grant is gone or reworded fails here even when nobody ran impulse check, whose
// conditions-proven check reads these calls to find the conditional grants no case names.
// Write the arguments as literals: the check reads them from the source.
func provesGrant(t *testing.T, roles access.RoleFile, role accesstypes.Role, permission accesstypes.Permission, res accesstypes.Resource, condition string) {
	t.Helper()

	config, err := roles.Parse()
	if err != nil {
		t.Fatalf("access.RoleFile.Parse() error = %v", err)
	}
	var conditions []string
	for _, r := range slices.Concat(config.Roles.Global, config.Roles.Domain) {
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
	t.Fatalf("no %s grant of the %s role on %s carries the condition %q (the role file's conditions there: %q); the case proves a grant the file no longer carries", permission, role, res, condition, conditions)
}
