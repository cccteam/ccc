package resource

// These tests pin the E-phase read rendering (design plan §05, Evaluation):
// rule 3's row predicate in the WHERE, rule 2's per-column CASE masking with
// the reserved masked-names column, implication pruning, the primary-key
// exemption, and the reserved named parameters binding the same values the
// check folded with. The no-conditions path is pinned byte-identical to RBAC
// by every other stmt-level test in the package.

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/google/go-cmp/cmp"
)

// renderStubPermissions answers Check from a fixed decision table; unlisted
// resources are Granted so fixtures only spell what each case is about.
type renderStubPermissions struct {
	decisions accesstypes.Decisions
}

func (s renderStubPermissions) Check(_ context.Context, _ accesstypes.Environment, _ accesstypes.Scope, _ accesstypes.Permission, resources ...accesstypes.Resource) (accesstypes.Decisions, error) {
	out := make(accesstypes.Decisions, len(resources))
	for _, res := range resources {
		if d, ok := s.decisions[res]; ok {
			out[res] = d
		} else {
			out[res] = accesstypes.Granted()
		}
	}

	return out, nil
}

func (renderStubPermissions) User() accesstypes.User { return "u1" }

// renderCollection declares the fixture vocabulary the conditions reference.
func renderCollection(t *testing.T) *GeneratedCollection {
	t.Helper()

	g, err := NewGeneratedCollection(CollectionData{Resources: []CollectionResource{{
		Name:        enforcedResource,
		Scope:       accesstypes.DomainPermissionScope,
		Permissions: []accesstypes.Permission{accesstypes.Read},
		Attributes: []AttributeData{
			{Name: "owner", Column: "Owner", Type: AttributeTypeString},
			{Name: "priority", Column: "Priority", Type: AttributeTypeNumber},
			{Name: "expires", Column: "Expires", Type: AttributeTypeTimestamp},
		},
	}}})
	if err != nil {
		t.Fatalf("NewGeneratedCollection() error = %v", err)
	}

	return g
}

func conditionalOn(res accesstypes.Resource, source string) accesstypes.Decision {
	return accesstypes.Conditional(accesstypes.ConditionGroup{
		Resources: []accesstypes.Resource{res},
		Condition: mustCondition(source),
	})
}

var collapseWhitespace = regexp.MustCompile(`\s+`)

func normalizeSQL(sql string) string {
	return strings.TrimSpace(collapseWhitespace.ReplaceAllString(sql, " "))
}

func TestQuerySet_stmt_readRendering(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name          string
		fields        []accesstypes.Field
		decisions     accesstypes.Decisions
		noCollection  bool
		wantSQL       string
		wantParams    map[string]any
		wantMaskedCol bool
		wantErr       string
	}{
		{
			name:   "one condition on every column prunes every CASE",
			fields: []accesstypes.Field{"ID", "Public", "Tagged"},
			decisions: accesstypes.Decisions{
				enforcedResource + ".public": conditionalOn(enforcedResource+".public", "owner = subject"),
				enforcedResource + ".tagged": conditionalOn(enforcedResource+".tagged", "owner = subject"),
			},
			wantSQL:    "SELECT Id, Public, Tagged FROM enforcementResources WHERE (`enforcementResources`.`Owner` = @subject)",
			wantParams: map[string]any{"subject": "u1"},
		},
		{
			name:   "partial pruning keeps the narrower column's CASE and mask term",
			fields: []accesstypes.Field{"ID", "Public", "Tagged"},
			decisions: accesstypes.Decisions{
				enforcedResource + ".public": conditionalOn(enforcedResource+".public", "owner = subject OR priority = 3"),
				enforcedResource + ".tagged": conditionalOn(enforcedResource+".tagged", "owner = subject"),
			},
			wantSQL: "SELECT Id, Public, " +
				"CASE WHEN `enforcementResources`.`Owner` = @subject THEN Tagged ELSE @_c2 END AS Tagged, " +
				"IF(`enforcementResources`.`Owner` = @subject, ARRAY<STRING>[], ['tagged']) AS zzMaskedFields " +
				"FROM enforcementResources " +
				"WHERE (`enforcementResources`.`Owner` = @subject OR `enforcementResources`.`Priority` = @_c1)",
			wantParams:    map[string]any{"subject": "u1", "_c1": int64(3), "_c2": ""},
			wantMaskedCol: true,
		},
		{
			name:   "an unconditional column makes the row predicate TRUE and disables pruning",
			fields: []accesstypes.Field{"ID", "Public", "Tagged"},
			decisions: accesstypes.Decisions{
				enforcedResource + ".tagged": conditionalOn(enforcedResource+".tagged", "owner = subject"),
			},
			wantSQL: "SELECT Id, Public, " +
				"CASE WHEN `enforcementResources`.`Owner` = @subject THEN Tagged ELSE @_c1 END AS Tagged, " +
				"IF(`enforcementResources`.`Owner` = @subject, ARRAY<STRING>[], ['tagged']) AS zzMaskedFields " +
				"FROM enforcementResources",
			wantParams:    map[string]any{"subject": "u1", "_c1": ""},
			wantMaskedCol: true,
		},
		{
			name:   "a now condition binds the environment's decision instant",
			fields: []accesstypes.Field{"ID", "Tagged"},
			decisions: accesstypes.Decisions{
				enforcedResource + ".tagged": conditionalOn(enforcedResource+".tagged", "expires > now"),
			},
			wantSQL:    "SELECT Id, Tagged FROM enforcementResources WHERE (`enforcementResources`.`Expires` > @now)",
			wantParams: map[string]any{"now": now},
		},
		{
			name:   "conditional decisions without a wired collection fail loud",
			fields: []accesstypes.Field{"ID", "Tagged"},
			decisions: accesstypes.Decisions{
				enforcedResource + ".tagged": conditionalOn(enforcedResource+".tagged", "owner = subject"),
			},
			noCollection: true,
			wantErr:      "no generated collection",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rSet, err := NewSet[enforcementResource, enforcementReadRequest](accesstypes.Read)
			if err != nil {
				t.Fatalf("NewSet() error = %v", err)
			}

			q := NewQuerySet(NewMetadata[enforcementResource]())
			q.env = accesstypes.EnvironmentAt(now)
			q.jsonNames = map[accesstypes.Field]string{"ID": "id", "Public": "public", "Tagged": "tagged"}
			if !tt.noCollection {
				q.collection = renderCollection(t)
			}
			q.EnableUserPermissionEnforcement(rSet, renderStubPermissions{decisions: tt.decisions}, testScope, accesstypes.Read)
			for _, field := range tt.fields {
				q.AddField(field)
			}

			if err := q.checkPermissions(t.Context(), SpannerDBType); err != nil {
				t.Fatalf("QuerySet.checkPermissions() error = %v", err)
			}

			stmt, err := q.stmt(SpannerDBType)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("QuerySet.stmt() error = %v, want containing %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("QuerySet.stmt() error = %v", err)
			}

			if got := normalizeSQL(stmt.SQL); got != tt.wantSQL {
				t.Errorf("QuerySet.stmt() SQL =\n%s\nwant\n%s", got, tt.wantSQL)
			}
			if diff := cmp.Diff(tt.wantParams, stmt.Params); diff != "" {
				t.Errorf("QuerySet.stmt() params mismatch (-want +got):\n%s", diff)
			}
			if gotMasked := stmt.maskedNamesColumn != ""; gotMasked != tt.wantMaskedCol {
				t.Errorf("QuerySet.stmt() maskedNamesColumn = %q, want set: %v", stmt.maskedNamesColumn, tt.wantMaskedCol)
			}
		})
	}
}
