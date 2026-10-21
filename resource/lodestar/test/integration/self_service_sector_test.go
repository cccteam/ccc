// Demonstrates: tenancy.self-service.
package integration

// This suite pins the self-service half of the Sector record's creation shape
// (@tenant(creation: both)) over two instances with the real engines, the shape a
// deployment has. The pioneer, a crew login holding no role and no sector (the sign-up
// registration's person, on no manifest), charts sector Ember through the generated
// create-sector route: the route writes the row, seats her as Ember's marshal through the
// crew auth's user-management surface (the engine answers the seat before the route
// answers), adds Ember to the writing instance's roster and signals the tenants kind.
// Her star chart lights Ember and Ember's flight deck answers her at once; a second
// sector is refused her, as the route is refused the marshal and the governor, who hold
// sectors already (an administrator of one sector never creates another here; the
// surveyor's way is the record's own create path, run_time_tenant_test.go); the governor
// and the client, held in every sector, see Ember with nothing written for them; and the
// second instance serves Ember at its next request. The steps run in order, each named in
// its failure, since every one reads what the ones before it wrote.

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
	"github.com/cccteam/ccc/resource/lodestar/pkg/router"
	initiator "github.com/cccteam/db-initiator"
)

const (
	// pioneerUser is the sign-up registration's person: a crew login on no manifest,
	// holding nothing, whose one way to a sector is to chart her own.
	pioneerUser accesstypes.User = "pioneer"

	// ember is the sector the pioneer charts: a slug the schema's CHECK constraint accepts.
	ember = "ember"

	createSectorRoute = consoleAPI + "/create-sector"

	// chartEmber is the create-sector body: the record's fields with its key.
	chartEmber = `{"id":"ember","name":"Ember","region":"Near frontier","established":"2226-10-20"}`
	// chartEmberAgain is a second sector of the pioneer's, refused: one sector per person.
	chartEmberAgain = `{"id":"ember-two","name":"Ember Two","region":"Near frontier","established":"2226-10-21"}`
)

// startSelfServiceInstance is startTenantInstance with the crew auth's user-management
// surface over the real crew engine's seats, the way the served stack builds it, so the
// seat the create-sector route writes reaches the engine every instance checks against.
func startSelfServiceInstance(t *testing.T, name string, db *initiator.SpannerDB, crewEngine, membersEngine *access.Client, svc live.Service) *flagInstance {
	t.Helper()

	roster := app.NewSectorRoster(resource.NewSpannerClient(db.Client), resource.WithTenantSignals(svc))
	if err := roster.Start(t.Context()); err != nil {
		t.Fatalf("resource.TenantRoster.Start() error = %v", err)
	}
	crewUsers := router.NewCrewUserSurface(resource.NewMemUserStore(), crewEngine.UserManager())
	a := app.New(&testConfigurer{db: db, access: crewEngine, membersAccess: membersEngine, live: svc, tenants: roster, crewUsers: crewUsers})
	if err := a.Start(t.Context()); err != nil {
		t.Fatalf("app.Start() error = %v", err)
	}

	return &flagInstance{name: name, h: withHandWrittenRoutes(a)}
}

// assertCreatedKey pins the create-sector answer: the new sector's key.
func assertCreatedKey(want string) func(t *testing.T, step string, body []byte) {
	return func(t *testing.T, step string, body []byte) {
		t.Helper()

		var got struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(body, &got); err != nil || got.ID != want {
			t.Errorf("%s: answered %s (err %v), want the key %q", step, body, err, want)
		}
	}
}

// TestSelfServiceSector_pioneerChartsHerOwn plays the scenario in order over two instances.
func TestSelfServiceSector_pioneerChartsHerOwn(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	db, err := prepareDatabase(ctx, t, migrationsSource, demoSeedSource)
	if err != nil {
		t.Fatal(err)
	}

	// The engines are the shipped role files over this database, provisioned with the
	// personas' memberships; the pioneer is on no manifest and holds nothing.
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

	fake := live.NewFake()
	first := startSelfServiceInstance(t, "the first instance", db, crewEngine, membersEngine, fake)
	second := startSelfServiceInstance(t, "the second instance", db, crewEngine, membersEngine, fake)

	charted := []accesstypes.Domain{anvil, bastion, cinder, ember}
	steps := []flagStep{
		{name: "before: the pioneer's chart is dark, a crew login holding nothing", on: first, user: pioneerUser, method: http.MethodGet, target: userDomainsRoute, wantStatus: http.StatusOK, check: assertDomains()},
		{name: "before: Ember does not exist on the first instance", on: first, user: tenantGovernor, method: http.MethodGet, target: sectorPath(ember, "missions"), wantStatus: http.StatusNotFound},
		{name: "before: nor on the second", on: second, user: tenantGovernor, method: http.MethodGet, target: sectorPath(ember, "missions"), wantStatus: http.StatusNotFound},
		{name: "the marshal holds Anvil and is refused: only a person holding no sector charts one here", on: first, user: tenantMarshal, method: http.MethodPost, target: createSectorRoute, body: chartEmber, wantStatus: http.StatusForbidden},
		{name: "the governor, marshal in every sector, is refused too: an administrator of one sector never creates another here", on: first, user: tenantGovernor, method: http.MethodPost, target: createSectorRoute, body: chartEmber, wantStatus: http.StatusForbidden},
		{name: "a body without the name is refused the pioneer, and writes nothing", on: first, user: pioneerUser, method: http.MethodPost, target: createSectorRoute, body: `{"id":"ember","region":"Near frontier","established":"2226-10-20"}`, wantStatus: http.StatusBadRequest},
		{name: "a key a sector holds already is refused her", on: first, user: pioneerUser, method: http.MethodPost, target: createSectorRoute, body: `{"id":"anvil","name":"Anvil","region":"Home","established":"2226-01-01"}`, wantStatus: http.StatusConflict},
		{name: "still dark after the refusals", on: first, user: pioneerUser, method: http.MethodGet, target: userDomainsRoute, wantStatus: http.StatusOK, check: assertDomains()},
		{
			name: "the pioneer charts sector Ember: the row, her marshal's seat and the roster in one call, and one signal of the tenants kind", on: first, user: pioneerUser, method: http.MethodPost, target: createSectorRoute, body: chartEmber, wantStatus: http.StatusOK,
			check: func(t *testing.T, step string, body []byte) {
				t.Helper()
				assertCreatedKey(ember)(t, step, body)
				if got := fake.Signals(); !slices.Equal(got, []live.Kind{live.KindTenants}) {
					t.Errorf("%s: signals = %v, want [%s]", step, got, live.KindTenants)
				}
			},
		},
		{name: "charted: her chart lights Ember at once, seated as its marshal before the route answered", on: first, user: pioneerUser, method: http.MethodGet, target: userDomainsRoute, wantStatus: http.StatusOK, check: assertDomains(ember)},
		{name: "charted: Ember's flight deck answers her, empty", on: first, user: pioneerUser, method: http.MethodGet, target: sectorPath(ember, "missions"), wantStatus: http.StatusOK, check: assertNoRows},
		{name: "charted: the sector list carries Ember, in name order", on: first, user: tenantSurveyor, method: http.MethodGet, target: sectorsRoute, wantStatus: http.StatusOK, check: assertSectorNames("Anvil", "Bastion", "Cinder", "Ember")},
		{name: "a second sector is refused her: one sector per person", on: first, user: pioneerUser, method: http.MethodPost, target: createSectorRoute, body: chartEmberAgain, wantStatus: http.StatusForbidden},
		{name: "the marshal's chart does not light Ember", on: first, user: tenantMarshal, method: http.MethodGet, target: userDomainsRoute, wantStatus: http.StatusOK, check: assertDomains(anvil)},
		{name: "Ember's routes answer the marshal 404: a sector that does not exist for her", on: first, user: tenantMarshal, method: http.MethodGet, target: sectorPath(ember, "missions"), wantStatus: http.StatusNotFound},
		{name: "the governor, marshal in every sector, sees Ember with nothing written for him", on: first, user: tenantGovernor, method: http.MethodGet, target: userDomainsRoute, wantStatus: http.StatusOK, check: assertDomains(charted...)},
		{name: "the client's portal lists Ember at once: the directory's role is held in every sector", on: first, user: clientUser, method: http.MethodGet, target: portalUserDomainsRoute, wantStatus: http.StatusOK, check: assertDomains(charted...)},
		{name: "the surveyor's chart lights Ember too, SectorCrew held in every sector", on: first, user: tenantSurveyor, method: http.MethodGet, target: userDomainsRoute, wantStatus: http.StatusOK, check: assertDomains(charted...)},
		{name: "the second instance serves Ember at its next request: its roster reloaded on the tenants kind's signal", on: second, user: pioneerUser, method: http.MethodGet, target: sectorPath(ember, "missions"), wantStatus: http.StatusOK, settle: true, check: assertNoRows},
		{name: "and her chart there lights Ember", on: second, user: pioneerUser, method: http.MethodGet, target: userDomainsRoute, wantStatus: http.StatusOK, check: assertDomains(ember)},
		{name: "and the seeded sectors stay dark for her there: SectorMarshal is hers in Ember alone", on: second, user: pioneerUser, method: http.MethodGet, target: sectorPath(anvil, "missions"), wantStatus: http.StatusNotFound},
	}
	for i := range steps {
		runFlagStep(t, &steps[i])
	}
}
