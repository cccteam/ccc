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
// fields on completed only. The renderer cannot see that the second implies the
// first, so every concealing key under either grant keeps its CASE.
const (
	closedStates = "state IN ('completed', 'failed', 'stood_down')"
	completed    = "state = 'completed'"
)

// archivistKey is the warning the deploy prints for one of the archivist's Missions
// keys: a key the closed-states grant covers is left uncovered by the completed
// grant, and the fee the other way round.
func archivistKey(field accesstypes.Tag, under, uncovered string) access.ConcealingKeyWarning {
	return access.ConcealingKeyWarning{
		Role:       "Archivist",
		Scope:      accesstypes.DomainPermissionScope,
		Resource:   "Missions",
		Field:      field,
		Conditions: []string{under},
		Uncovered:  []string{uncovered},
	}
}

// TestRoles_validateAgainstTheCollection runs each auth's committed role file
// through the validation MigrateRoles performs before it touches the store:
// the grammar holds, every condition resolves against the generated
// collection, and the warnings the deploy would print are exactly the ones
// pinned here. No role holds a conditional write on a row it can neither Read
// nor List. The archivist's Missions grants raise the concealing-key warning on
// every indexed key and on the fee filter key: her two conditions differ, so a
// page she sorts or filters by one of them sorts the partition. Deadline, the
// default order, is declared positional and raises nothing: her every page
// orders on the real column. The six indexed keys warn only because the
// covering test is syntactic (the completed grant implies the closed-states
// grant); the fee is the genuine case, its condition being the narrower one.
//
// Demonstrates: warning.concealing-key, masking.positional.
func TestRoles_validateAgainstTheCollection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		rolesPath    string
		wantWarnings []access.Warning
	}{
		{
			name:      "crew",
			rolesPath: crew.RolesPath,
			wantWarnings: []access.Warning{
				archivistKey("assignedSquadronId", closedStates, completed),
				archivistKey("clientId", closedStates, completed),
				archivistKey("fee", completed, closedStates),
				archivistKey("kindId", closedStates, completed),
				archivistKey("requiredCertId", closedStates, completed),
				archivistKey("sectorId", closedStates, completed),
				archivistKey("statusId", closedStates, completed),
			},
		},
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
