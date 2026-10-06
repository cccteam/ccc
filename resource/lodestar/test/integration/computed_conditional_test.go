// Demonstrates: computed.conditional-grant, condition.now, @computed.
package integration

// computed_conditional_test (design plan §9): the hazard board answers under the
// analyst's row-free `now < '2099-01-01T00:00:00Z'` grant before the certification
// instant and refuses after it (pinned through the engine at two instants), and a
// row-bearing condition on the same grant is refused when the role file is validated.

import (
	"net/http"
	"testing"
	"time"

	"github.com/cccteam/access"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource/lodestar/pkg/auth/crew"
	"github.com/cccteam/ccc/resource/lodestar/pkg/router"
)

func TestComputedConditionalGrant(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	_, h, client := sharedWorld(t)

	// The grant this suite proves, pinned to the roles file (conditions-proven).
	provesGrant(t, crew.Roles(), "HazardAnalyst", "List", "SectorHazardBoards", "now < '2099-01-01T00:00:00Z'")

	t.Run("the board answers today", func(t *testing.T) {
		t.Parallel()

		status, body := doRequestAs(t, h, "hazards", http.MethodGet, sectorPath(anvil, "sector-hazard-boards?limit=all"), "")
		assertStatus(t, status, http.StatusOK, body)
		rows := decodeRows(t, body)
		if len(rows) != 32 { // eight Anvil ships with readings on up to four subsystems; the Bastion Watch reading stays at Bastion
			t.Fatalf("rows = %d, want 32: %s", len(rows), body)
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
		before, err := checker.Check(ctx, accesstypes.EnvironmentAt(time.Date(2098, 12, 31, 23, 59, 0, 0, time.UTC)),
			accesstypes.DomainScope(anvil), accesstypes.List, "SectorHazardBoards")
		if err != nil {
			t.Fatalf("Check() before error = %v", err)
		}
		if !before["SectorHazardBoards"].IsGranted() {
			t.Errorf("before the instant: %v, want granted (the row-free term folds true)", before["SectorHazardBoards"])
		}
		after, err := checker.Check(ctx, accesstypes.EnvironmentAt(time.Date(2099, 1, 1, 0, 0, 1, 0, time.UTC)),
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
// no data layer to evaluate a row term against, so the validation a role file goes
// through (access.ValidateRoles, the check access.New runs on the file an auth opens
// with) refuses a row-referencing condition on its grant, and a release carrying one
// does not start.
func TestComputedRowConditionRefused(t *testing.T) {
	t.Parallel()

	conf := &access.RoleConfig{Roles: access.ScopedRoles{Domain: []*access.Role{{
		Name: "BadAnalyst",
		Permissions: map[accesstypes.Permission][]access.Grant{
			accesstypes.List: {{Resource: "SectorHazardBoards", Fields: []accesstypes.Tag{"shipName"}, Condition: "worstReading > 0.5"}},
		},
	}}}}
	if _, err := access.ValidateRoles(router.Collection(), conf); err == nil {
		t.Fatal("access.ValidateRoles() accepted a row-bearing condition on a computed resource; want a refusal")
	}
}
