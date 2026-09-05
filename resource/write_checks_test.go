package resource

// These tests pin the mutations' stage-3 live check (design plan §05): the
// check-SELECT's shape per patch type, the image-binding contract (insert has
// one proposed image and no target-row FROM; update overlays proposed
// parameters on the existing row; delete is a plain read carried by the
// base-resource decision), covering-set grouping, and the fail-closed edges
// (upsert with conditions, insert conditions over unset columns).

import (
	"strings"
	"testing"
	"time"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/google/go-cmp/cmp"
)

func writeCheckCollection(t *testing.T) *GeneratedCollection {
	t.Helper()

	g, err := NewGeneratedCollection(CollectionData{Resources: []CollectionResource{{
		Name:        enforcedResource,
		Scope:       accesstypes.DomainPermissionScope,
		Permissions: []accesstypes.Permission{accesstypes.Read},
		Attributes: []AttributeData{
			{Name: "owner", Column: "Owner", Type: AttributeTypeString},
			{Name: "pub", Column: "Public", Type: AttributeTypeString},
		},
		Domain: &DomainBindingData{Column: "Station"},
	}}})
	if err != nil {
		t.Fatalf("NewGeneratedCollection() error = %v", err)
	}

	return g
}

func TestPatchSet_writeCheckStatement(t *testing.T) {
	t.Parallel()

	id := mustUUIDFromString("8a6570c8-1e51-4870-9def-3f68d0447d09")
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name       string
		patchType  PatchType
		permission accesstypes.Permission
		set        map[accesstypes.Field]any
		decisions  accesstypes.Decisions
		wantGroups int
		wantSQL    string
		wantParams map[string]any
		wantErr    string
	}{
		{
			name:       "update overlays proposed values on the existing row",
			patchType:  UpdatePatchType,
			permission: accesstypes.Update,
			set:        map[accesstypes.Field]any{"Public": "proposed"},
			decisions: accesstypes.Decisions{
				enforcedResource + ".public": conditionalOn(enforcedResource+".public", "owner = subject AND new.pub != 'locked'"),
			},
			wantGroups: 1,
			wantSQL: "SELECT ((`enforcementResources`.`Owner` = @subject AND @_c1 <> @_c2)) AS g1 " +
				"FROM enforcementResources WHERE `Id` = @_id AND (`enforcementResources`.`Station` = @domain)",
			wantParams: map[string]any{"subject": "u1", "_c1": "proposed", "_c2": "locked", "_id": id, "domain": "testDomain"},
		},
		{
			name:       "one covering set shares one boolean across touched columns",
			patchType:  UpdatePatchType,
			permission: accesstypes.Update,
			set:        map[accesstypes.Field]any{"Public": "a", "Tagged": "b"},
			decisions: accesstypes.Decisions{
				enforcedResource + ".public": conditionalOn(enforcedResource+".public", "owner = subject"),
				enforcedResource + ".tagged": conditionalOn(enforcedResource+".tagged", "owner = subject"),
			},
			wantGroups: 1,
			wantSQL: "SELECT (`enforcementResources`.`Owner` = @subject) AS g1 " +
				"FROM enforcementResources WHERE `Id` = @_id AND (`enforcementResources`.`Station` = @domain)",
			wantParams: map[string]any{"subject": "u1", "_id": id, "domain": "testDomain"},
		},
		{
			name:       "distinct covering sets AND as separate booleans",
			patchType:  UpdatePatchType,
			permission: accesstypes.Update,
			set:        map[accesstypes.Field]any{"Public": "a", "Tagged": "b"},
			decisions: accesstypes.Decisions{
				enforcedResource + ".public": conditionalOn(enforcedResource+".public", "owner = subject"),
				enforcedResource + ".tagged": conditionalOn(enforcedResource+".tagged", "pub = 'open'"),
			},
			wantGroups: 2,
			wantSQL: "SELECT (`enforcementResources`.`Owner` = @subject) AS g1, (`enforcementResources`.`Public` = @_c1) AS g2 " +
				"FROM enforcementResources WHERE `Id` = @_id AND (`enforcementResources`.`Station` = @domain)",
			wantParams: map[string]any{"subject": "u1", "_c1": "open", "_id": id, "domain": "testDomain"},
		},
		{
			// The old-vs-new form (§05, decided 2026-09-03): the post-image
			// side binds the proposed value, the right side reads the same
			// row's pre-image column — "may lower, never raise" in one term.
			name:       "old-vs-new compares the proposed value against the pre-image",
			patchType:  UpdatePatchType,
			permission: accesstypes.Update,
			set:        map[accesstypes.Field]any{"Public": "proposed"},
			decisions: accesstypes.Decisions{
				enforcedResource + ".public": conditionalOn(enforcedResource+".public", "new.pub <= pub"),
			},
			wantGroups: 1,
			wantSQL: "SELECT (@_c1 <= `enforcementResources`.`Public`) AS g1 " +
				"FROM enforcementResources WHERE `Id` = @_id AND (`enforcementResources`.`Station` = @domain)",
			wantParams: map[string]any{"_c1": "proposed", "_id": id, "domain": "testDomain"},
		},
		{
			// An untouched new. column reads the existing value (overlay
			// semantics), so the old-vs-new term degenerates to the tautology
			// and the mutation is judged only by what it changes.
			name:       "old-vs-new over an untouched column is the tautology",
			patchType:  UpdatePatchType,
			permission: accesstypes.Update,
			set:        map[accesstypes.Field]any{"Tagged": "b"},
			decisions: accesstypes.Decisions{
				enforcedResource + ".tagged": conditionalOn(enforcedResource+".tagged", "new.pub <= pub"),
			},
			wantGroups: 1,
			wantSQL: "SELECT (`enforcementResources`.`Public` <= `enforcementResources`.`Public`) AS g1 " +
				"FROM enforcementResources WHERE `Id` = @_id AND (`enforcementResources`.`Station` = @domain)",
			wantParams: map[string]any{"_id": id, "domain": "testDomain"},
		},
		{
			name:       "insert binds one proposed image with no target-row FROM",
			patchType:  CreatePatchType,
			permission: accesstypes.Create,
			set:        map[accesstypes.Field]any{"Public": "fresh"},
			decisions: accesstypes.Decisions{
				enforcedResource + ".public": conditionalOn(enforcedResource+".public", "pub != 'locked'"),
			},
			wantGroups: 1,
			wantSQL:    "SELECT (@_c1 <> @_c2) AS g1",
			wantParams: map[string]any{"_c1": "fresh", "_c2": "locked"},
		},
		{
			name:       "insert condition over an unset column fails loud",
			patchType:  CreatePatchType,
			permission: accesstypes.Create,
			set:        map[accesstypes.Field]any{"Public": "fresh"},
			decisions: accesstypes.Decisions{
				enforcedResource + ".public": conditionalOn(enforcedResource+".public", "owner = subject"),
			},
			wantErr: "which the insert does not set",
		},
		{
			name:       "delete's condition arrives on the base-resource decision",
			patchType:  DeletePatchType,
			permission: accesstypes.Delete,
			decisions: accesstypes.Decisions{
				enforcedResource: conditionalOn(enforcedResource, "owner = subject"),
			},
			wantGroups: 1,
			wantSQL: "SELECT (`enforcementResources`.`Owner` = @subject) AS g1 " +
				"FROM enforcementResources WHERE `Id` = @_id AND (`enforcementResources`.`Station` = @domain)",
			wantParams: map[string]any{"subject": "u1", "_id": id, "domain": "testDomain"},
		},
		{
			name:       "upsert with conditional grants fails closed",
			patchType:  CreateOrUpdatePatchType,
			permission: accesstypes.Update,
			set:        map[accesstypes.Field]any{"Public": "a"},
			decisions: accesstypes.Decisions{
				enforcedResource + ".public": conditionalOn(enforcedResource+".public", "owner = subject"),
			},
			wantErr: "cannot enforce an insert-or-update",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rSet, err := NewSet[enforcementResource, enforcementPatchRequest](accesstypes.Create, accesstypes.Update, accesstypes.Delete)
			if err != nil {
				t.Fatalf("NewSet() error = %v", err)
			}

			p := NewPatchSet(NewMetadata[enforcementResource]()).
				SetKey("ID", id).
				SetPatchType(tt.patchType)
			p.querySet.env = accesstypes.EnvironmentAt(now)
			p.querySet.collection = writeCheckCollection(t)
			p.EnableUserPermissionEnforcement(rSet, renderStubPermissions{}, testScope, tt.permission)
			p.querySet.conditionalDecisions = tt.decisions
			for field, value := range tt.set {
				p.Set(field, value)
			}
			if tt.patchType == CreatePatchType {
				// The decoder stamps the bare-column tenant key on create;
				// hand-built fixtures mirror it.
				p.Set("Station", "testDomain")
			}

			groups, err := p.writeConditionGroups()
			if err == nil && len(groups) > 0 {
				var tenancy *mutationTenancy
				tenancy, err = p.mutationTenancy()
				if err != nil {
					t.Fatalf("mutationTenancy() error = %v", err)
				}
				var stmt *Statement
				stmt, err = p.writeCheckStatement(SpannerDBType, groups, tenancy)
				if err == nil {
					if got := normalizeSQL(stmt.SQL); got != tt.wantSQL {
						t.Errorf("writeCheckStatement() SQL =\n%s\nwant\n%s", got, tt.wantSQL)
					}
					if diff := cmp.Diff(tt.wantParams, stmt.Params); diff != "" {
						t.Errorf("writeCheckStatement() params mismatch (-want +got):\n%s", diff)
					}
				}
			}

			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("unexpected error = %v", err)
			}
			if len(groups) != tt.wantGroups {
				t.Fatalf("writeConditionGroups() = %d groups, want %d", len(groups), tt.wantGroups)
			}
		})
	}
}
