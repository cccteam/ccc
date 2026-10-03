// Demonstrates: tenancy.run-time-tenant, tenancy.tenant-record.
package integration

// This suite pins the tenant roster keeping up without a restart, a migrate job or a new
// login. Two instances of the application run over one database, the real engines
// provisioned from the shipped role files, one in-memory live service (live.Fake, which
// fans a signal out to every subscription) and a started roster per instance, the shape a
// deployment has. The surveyor, who holds Create and Update on Sectors and SectorCrew in
// every sector, charts sector Dawn through the first instance's consolidated patch: the
// instance that wrote adds Dawn to its roster after the commit and signals the tenants
// kind, her star chart lights Dawn and Dawn's wings answer her; the marshal's chart does
// not light it and Dawn's routes answer her 404, a sector that does not exist for her; the
// portal's client, whose directory role is held in every sector, opens Dawn's portal at
// once, as the governor and the archivist see Dawn with nothing written for them; and the
// second instance serves Dawn at its next request, its roster reloaded on the signal. The
// steps run in order, each named in its failure, since every one reads what the ones
// before it wrote. The walkthrough plays the same scenario against the Firestore emulator
// with a second server process.

import (
	"encoding/json"
	"net/http"
	"slices"
	"testing"

	"github.com/cccteam/access"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/live"
	"github.com/cccteam/ccc/resource/lodestar/app"
	"github.com/cccteam/ccc/resource/lodestar/pkg/auth/crew"
	"github.com/cccteam/ccc/resource/lodestar/pkg/auth/members"
	initiator "github.com/cccteam/db-initiator"
)

// The suite's cast, the shipped personas: the surveyor charts sectors; the marshal holds
// Anvil alone; the governor and the archivist are held in every sector, as the client's
// directory role is.
const (
	tenantSurveyor  accesstypes.User = "surveyor"
	tenantMarshal   accesstypes.User = "marshal"
	tenantGovernor  accesstypes.User = "governor"
	tenantArchivist accesstypes.User = "archivist"

	// dawn is the sector the surveyor charts: a slug the schema's CHECK constraint accepts.
	dawn = "dawn"

	userDomainsRoute       = consoleAPI + "/user-domains"
	portalUserDomainsRoute = portalAPI + "/user-domains"
	sectorsRoute           = consoleAPI + "/sectors"
	resourcesRoute         = consoleAPI + "/resources"

	// chartDawn is the consolidated patch that creates the sector: the key rides in the
	// operation's path, since Sectors supplies its own slug.
	chartDawn = `[{"op":"add","path":"/sectors/dawn","value":{"name":"Dawn","region":"Outer frontier","established":"2226-10-02"}}]`
	// surveyDawn corrects the sector after it is charted: Update on Sectors.
	surveyDawn = `[{"op":"patch","path":"/sectors/dawn","value":{"region":"Far frontier"}}]`
)

// startTenantInstance builds an App over the database, the real engines, the shared live
// service and a roster of its own started over that service, as a served instance's data
// configuration starts it, and starts the App.
func startTenantInstance(t *testing.T, name string, db *initiator.SpannerDB, crewEngine, membersEngine access.Controller, svc live.Service) *flagInstance {
	t.Helper()

	roster := app.NewSectorRoster(resource.NewSpannerClient(db.Client), resource.WithTenantSignals(svc))
	if err := roster.Start(t.Context()); err != nil {
		t.Fatalf("resource.TenantRoster.Start() error = %v", err)
	}
	a := app.New(&testConfigurer{db: db, access: crewEngine, membersAccess: membersEngine, live: svc, tenants: roster})
	if err := a.Start(t.Context()); err != nil {
		t.Fatalf("app.Start() error = %v", err)
	}

	return &flagInstance{name: name, h: withHandWrittenRoutes(a)}
}

// assertDomains pins the star chart a user-domains answer draws, in the sorted order the
// route lists.
func assertDomains(want ...accesstypes.Domain) func(t *testing.T, step string, body []byte) {
	return func(t *testing.T, step string, body []byte) {
		t.Helper()

		var got []accesstypes.Domain
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatalf("%s: decoding the domains: %v: %s", step, err, body)
		}
		if !slices.Equal(got, want) {
			t.Errorf("%s: domains = %v, want %v", step, got, want)
		}
	}
}

// assertNoRows pins a list answered empty: Dawn is charted, not yet crewed or flown.
func assertNoRows(t *testing.T, step string, body []byte) {
	t.Helper()

	if got := len(decodeRows(t, body)); got != 0 {
		t.Errorf("%s: rows = %d, want none: %s", step, got, body)
	}
}

// assertSectorNames pins the sector list by name, the record's declared order.
func assertSectorNames(want ...string) func(t *testing.T, step string, body []byte) {
	return func(t *testing.T, step string, body []byte) {
		t.Helper()

		var got []string
		for _, row := range decodeRows(t, body) {
			got = append(got, cell[string](t, row, "name"))
		}
		if !slices.Equal(got, want) {
			t.Errorf("%s: sectors = %v, want %v", step, got, want)
		}
	}
}

// TestRunTimeTenant_chartedSectorReachesSecondInstance plays the scenario in order over
// two instances.
func TestRunTimeTenant_chartedSectorReachesSecondInstance(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	db, err := prepareDatabase(ctx, t, migrationsSource, demoSeedSource)
	if err != nil {
		t.Fatal(err)
	}

	// The engines are the shipped role files over this database, provisioned with the
	// personas' memberships, so the surveyor's authority is the Surveyor role as it ships
	// and SectorCrew held in every sector, nothing scripted.
	crewEngine := newAccessClient(t, db, crew.Roles())
	membersEngine, err := openEngine(db, members.TablePrefix, members.Roles())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := membersEngine.Close(); err != nil {
			t.Errorf("access.Client.Close() error = %v", err)
		}
	})
	if err := provisionDemoAccess(ctx, crewEngine, membersEngine); err != nil {
		t.Fatal(err)
	}

	// One live service, two instances, each with a roster started over it: Start loads
	// the seeded sectors and subscribes to the tenants kind.
	fake := live.NewFake()
	first := startTenantInstance(t, "the first instance", db, crewEngine, membersEngine, fake)
	second := startTenantInstance(t, "the second instance", db, crewEngine, membersEngine, fake)
	if got := fake.Subscribers(live.KindTenants); got != 2 {
		t.Fatalf("rosters subscribed to the %s kind = %d, want 2: Start follows the signals", live.KindTenants, got)
	}

	seeded := []accesstypes.Domain{anvil, bastion, cinder}
	charted := []accesstypes.Domain{anvil, bastion, cinder, dawn}
	steps := []flagStep{
		{name: "before: the surveyor's chart lights the three seeded sectors, SectorCrew held in every one", on: first, user: tenantSurveyor, method: http.MethodGet, target: userDomainsRoute, wantStatus: http.StatusOK, check: assertDomains(seeded...)},
		{name: "before: Dawn does not exist on the first instance", on: first, user: tenantSurveyor, method: http.MethodGet, target: sectorPath(dawn, "wings"), wantStatus: http.StatusNotFound},
		{name: "before: nor on the second", on: second, user: tenantSurveyor, method: http.MethodGet, target: sectorPath(dawn, "wings"), wantStatus: http.StatusNotFound},
		{name: "the marshal holds no Create on Sectors", on: first, user: tenantMarshal, method: http.MethodPatch, target: resourcesRoute, body: chartDawn, wantStatus: http.StatusForbidden},
		{
			name: "the surveyor charts sector Dawn: Create on Sectors, the key in the path, and one signal of the tenants kind", on: first, user: tenantSurveyor, method: http.MethodPatch, target: resourcesRoute, body: chartDawn, wantStatus: http.StatusOK,
			check: func(t *testing.T, step string, _ []byte) {
				t.Helper()
				if got := fake.Signals(); !slices.Equal(got, []live.Kind{live.KindTenants}) {
					t.Errorf("%s: signals = %v, want [%s]", step, got, live.KindTenants)
				}
			},
		},
		{name: "charted: the sector list carries Dawn, in name order", on: first, user: tenantSurveyor, method: http.MethodGet, target: sectorsRoute, wantStatus: http.StatusOK, check: assertSectorNames("Anvil", "Bastion", "Cinder", "Dawn")},
		{name: "charted: her star chart lights Dawn with no restart, no migrate job and no new login", on: first, user: tenantSurveyor, method: http.MethodGet, target: userDomainsRoute, wantStatus: http.StatusOK, check: assertDomains(charted...)},
		{name: "charted: Dawn's wings answer her on the instance that wrote, an empty list", on: first, user: tenantSurveyor, method: http.MethodGet, target: sectorPath(dawn, "wings"), wantStatus: http.StatusOK, check: assertNoRows},
		{name: "charted: the surveyor corrects Dawn's region, Update on Sectors", on: first, user: tenantSurveyor, method: http.MethodPatch, target: resourcesRoute, body: surveyDawn, wantStatus: http.StatusOK},
		{name: "the marshal's chart does not light Dawn", on: first, user: tenantMarshal, method: http.MethodGet, target: userDomainsRoute, wantStatus: http.StatusOK, check: assertDomains(anvil)},
		{name: "Dawn's routes answer the marshal 404: a sector that does not exist for her", on: first, user: tenantMarshal, method: http.MethodGet, target: sectorPath(dawn, "wings"), wantStatus: http.StatusNotFound},
		{name: "the governor, marshal in every sector, sees Dawn", on: first, user: tenantGovernor, method: http.MethodGet, target: userDomainsRoute, wantStatus: http.StatusOK, check: assertDomains(charted...)},
		{name: "Dawn's flight deck answers the governor, empty", on: first, user: tenantGovernor, method: http.MethodGet, target: sectorPath(dawn, "missions"), wantStatus: http.StatusOK, check: assertNoRows},
		{name: "the archivist, held in every sector, sees Dawn", on: first, user: tenantArchivist, method: http.MethodGet, target: userDomainsRoute, wantStatus: http.StatusOK, check: assertDomains(charted...)},
		{name: "the client's portal lists Dawn at once: the directory's role is held in every sector", on: first, user: clientUser, method: http.MethodGet, target: portalUserDomainsRoute, wantStatus: http.StatusOK, check: assertDomains(charted...)},
		{name: "Dawn's tracker opens for her, empty", on: first, user: clientUser, method: http.MethodGet, target: portalPath(dawn, "missions"), wantStatus: http.StatusOK, check: assertNoRows},
		{name: "the second instance serves Dawn at its next request: its roster reloaded on the tenants kind's signal, not the backstop", on: second, user: tenantSurveyor, method: http.MethodGet, target: sectorPath(dawn, "wings"), wantStatus: http.StatusOK, settle: true, check: assertNoRows},
		{name: "and her chart there lights Dawn", on: second, user: tenantSurveyor, method: http.MethodGet, target: userDomainsRoute, wantStatus: http.StatusOK, check: assertDomains(charted...)},
		{name: "and Dawn stays a sector that does not exist for the marshal there", on: second, user: tenantMarshal, method: http.MethodGet, target: sectorPath(dawn, "wings"), wantStatus: http.StatusNotFound},
	}
	for i := range steps {
		runFlagStep(t, &steps[i])
	}
}
