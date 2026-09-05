package integration

// computed_conditional_test (design plan §9): the hazard board answers under the
// analyst's row-free `now < '2027-01-01T00:00:00Z'` grant before the certification
// instant and refuses after it (pinned through the engine at two instants), and a
// row-bearing condition on the same grant is refused by MigrateRoles.

import (
	"net/http"
	"testing"
	"time"

	"github.com/cccteam/access"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource/lodestar/pkg/router"
)

func TestComputedConditionalGrant(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	_, h, client := sharedWorld(t)

	t.Run("the board answers today", func(t *testing.T) {
		t.Parallel()

		status, body := doRequestAs(t, h, "hazards", http.MethodGet, sectorPath(anvil, "sector-hazard-boards"), "")
		assertStatus(t, status, http.StatusOK, body)
		rows := decodeRows(t, body)
		if len(rows) != 3 { // Kingfisher hull + reactor, Stubborn Mule hull; the Bastion Watch reading stays at Bastion
			t.Fatalf("rows = %d, want 3: %s", len(rows), body)
		}
		worst := map[string]float64{}
		for _, row := range rows {
			ship, _ := row["shipName"].(string)
			sub, _ := row["subsystem"].(string)
			reading, _ := row["worstReading"].(float64)
			worst[ship+"/"+sub] = reading
		}
		if worst["Kingfisher/hull"] != 0.61 {
			t.Errorf("Kingfisher hull worst = %v, want the higher reading 0.61", worst["Kingfisher/hull"])
		}
	})

	t.Run("the board is dark in a sector where the analyst holds no role", func(t *testing.T) {
		t.Parallel()

		status, body := doRequestAs(t, h, "hazards", http.MethodGet, sectorPath(bastion, "sector-hazard-boards"), "")
		assertStatus(t, status, http.StatusNotFound, body)
	})

	t.Run("the certification lapses at the instant", func(t *testing.T) {
		t.Parallel()

		checker := client.ForUser("hazards")
		before, err := checker.Check(ctx, accesstypes.EnvironmentAt(time.Date(2026, 12, 31, 23, 59, 0, 0, time.UTC)),
			accesstypes.DomainScope(anvil), accesstypes.List, "SectorHazardBoards")
		if err != nil {
			t.Fatalf("Check() before error = %v", err)
		}
		if !before["SectorHazardBoards"].IsGranted() {
			t.Errorf("before the instant: %v, want granted (the row-free term folds true)", before["SectorHazardBoards"])
		}
		after, err := checker.Check(ctx, accesstypes.EnvironmentAt(time.Date(2027, 1, 1, 0, 0, 1, 0, time.UTC)),
			accesstypes.DomainScope(anvil), accesstypes.List, "SectorHazardBoards")
		if err != nil {
			t.Fatalf("Check() after error = %v", err)
		}
		if !after["SectorHazardBoards"].IsDenied() {
			t.Errorf("after the instant: %v, want denied", after["SectorHazardBoards"])
		}
	})
}

// TestComputedRowConditionRefused pins the deploy invariant: a computed resource has
// no data layer to evaluate a row term against, so MigrateRoles refuses a
// row-referencing condition on its grant.
func TestComputedRowConditionRefused(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	db, err := prepareDatabase(ctx, t, migrationsSource, demoSeedSource)
	if err != nil {
		t.Fatal(err)
	}
	client := newAccessClient(t, db)

	conf := &access.RoleConfig{Roles: access.ScopedRoles{Domain: []*access.Role{{
		Name: "BadAnalyst",
		Permissions: map[accesstypes.Permission][]access.Grant{
			accesstypes.List: {{Resource: "SectorHazardBoards", Fields: []accesstypes.Tag{"shipName"}, Condition: "worstReading > 0.5"}},
		},
	}}}}
	if err := access.MigrateRoles(ctx, client.UserManager(), router.Collection(), conf, anvil); err == nil {
		t.Fatal("MigrateRoles accepted a row-bearing condition on a computed resource; want a refusal")
	}
}
