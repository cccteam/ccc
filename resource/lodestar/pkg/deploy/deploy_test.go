package deploy_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/cccteam/access"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource/lodestar/pkg/auth/crew"
	"github.com/cccteam/ccc/resource/lodestar/pkg/auth/members"
	"github.com/cccteam/ccc/resource/lodestar/pkg/router"
)

// The archivist's Missions grants: the row fields on every closed state, the money
// fields on completed only. Completed is one of the closed states, so the renderer
// proves the second grant implies the first (condition.Implies) and every key the
// closed-states grant covers prunes its CASE; the fee, granted on the narrower
// condition, is the genuine case and keeps its own.
const (
	closedStates = "state IN ('completed', 'failed', 'stood_down')"
	completed    = "state = 'completed'"
)

// archivistFee is the one warning the deploy prints: the fee is a filter key of
// Missions the archivist lists under completed, and the closed-states rows she also
// lists are not all completed, so the row filter does not prove the fee's condition.
var archivistFee = access.ConcealingKeyWarning{
	Role:       "Archivist",
	Scope:      accesstypes.DomainPermissionScope,
	Resource:   "Missions",
	Field:      "fee",
	Conditions: []string{completed},
	Uncovered:  []string{closedStates},
}

// TestRoles_validateAgainstTheCollection runs each auth's committed role file
// through the validation MigrateRoles performs before it touches the store:
// the grammar holds, every condition resolves against the generated
// collection, and the warnings the deploy would print are exactly the ones
// pinned here. No role holds a conditional write on a row it can neither Read
// nor List. The archivist's Missions grants raise the concealing-key warning on
// the fee filter key alone: her completed grant implies her closed-states
// grant, so the six indexed keys granted on the closed states are covered by
// implication and prune their CASE, while the fee's condition is the narrower
// one and a page she sorts or filters by fee sorts the partition. Deadline,
// the default order, is declared positional and raises nothing: her every
// page orders on the real column.
//
// Demonstrates: warning.concealing-key, masking.positional.
func TestRoles_validateAgainstTheCollection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		rolesPath    string
		wantWarnings []access.Warning
	}{
		{name: "crew", rolesPath: crew.RolesPath, wantWarnings: []access.Warning{archivistFee}},
		{name: "members", rolesPath: members.RolesPath},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			raw, err := os.ReadFile(filepath.Join("..", "..", tt.rolesPath))
			if err != nil {
				t.Fatalf("os.ReadFile() error = %v", err)
			}
			var roles access.RoleConfig
			if err := json.Unmarshal(raw, &roles); err != nil {
				t.Fatalf("json.Unmarshal() error = %v", err)
			}

			warnings, err := access.ValidateRoles(router.Collection(), &roles)
			if err != nil {
				t.Fatalf("access.ValidateRoles() error = %v", err)
			}
			if len(warnings) == 0 && len(tt.wantWarnings) == 0 {
				return
			}
			if !reflect.DeepEqual(tt.wantWarnings, warnings) {
				t.Errorf("access.ValidateRoles() warnings = %d, want %d:", len(warnings), len(tt.wantWarnings))
				for _, w := range warnings {
					t.Logf("got:  %s", w)
				}
				for _, w := range tt.wantWarnings {
					t.Logf("want: %s", w)
				}
			}
		})
	}
}
