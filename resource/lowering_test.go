package resource

import (
	"math/big"
	"strings"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/accesstypes/condition"
	"github.com/google/go-cmp/cmp"
)

// loweringFixtureCollection is the running example's vocabulary (design plan
// §02): MaintenanceTasks with column and join-path attributes and a domain
// binding; CrewMembers anchoring the crews subject set (domain-scoped, so its
// subquery derives a tenancy filter); UserProfiles anchoring the
// approvalLimit subject value (global — no filter), the dotted homeSector, and
// the timestamp-typed clearedUntil now can compare against. Every subject entry
// carries the comparison type of the column it yields, as generation derives
// it.
func loweringFixtureCollection(t *testing.T) *GeneratedCollection {
	t.Helper()

	g, err := NewGeneratedCollection(CollectionData{Resources: []CollectionResource{
		{
			Name:  "MaintenanceTasks",
			Scope: accesstypes.DomainPermissionScope,
			Attributes: []AttributeData{
				{Name: "crew", Column: "CrewId", Type: AttributeTypeString},
				{Name: "state", Column: "State", Type: AttributeTypeString},
				{Name: "estimatedCost", Column: "EstimatedCost", Type: AttributeTypeNumber},
				{Name: "shipClass", Column: "ShipId", Type: AttributeTypeString, Path: []BindingHop{{Table: "Ships", JoinColumn: "Id", Column: "Class"}}},
				{Name: "sector", Column: "BerthId", Type: AttributeTypeString, Path: []BindingHop{
					{Table: "Berths", JoinColumn: "Id", Column: "StationId"},
					{Table: "Stations", JoinColumn: "Id", Column: "Sector"},
				}},
				{Name: "assignee", Column: "Assignee", Type: AttributeTypeString},
				{Name: "wing", Column: "WingId", Type: AttributeTypeString},
			},
			Domain: &DomainBindingData{Column: "StationId"},
		},
		{
			Name:  "CrewMembers",
			Scope: accesstypes.DomainPermissionScope,
			SubjectSets: []SubjectBindingData{
				{Name: "crews", UserColumn: "UserId", Column: "CrewId", Type: AttributeTypeString},
				{Name: "wings", UserColumn: "UserId", Column: "CrewId", Type: AttributeTypeString, Path: []BindingHop{{Table: "Crews", JoinColumn: "Id", Column: "WingId"}}},
			},
			Domain: &DomainBindingData{Column: "StationId"},
		},
		{
			Name:  "UserProfiles",
			Scope: accesstypes.GlobalPermissionScope,
			SubjectValues: []SubjectBindingData{
				{Name: "approvalLimit", UserColumn: "UserId", Column: "ApprovalLimit", Type: AttributeTypeNumber},
				{Name: "homeSector", UserColumn: "UserId", Column: "StationId", Type: AttributeTypeString, Path: []BindingHop{{Table: "Stations", JoinColumn: "Id", Column: "Sector"}}},
				{Name: "clearedUntil", UserColumn: "UserId", Column: "ClearedUntil", Type: AttributeTypeTimestamp},
			},
		},
	}})
	if err != nil {
		t.Fatalf("NewGeneratedCollection() error = %v", err)
	}

	return g
}

// ratComparer compares bound NUMERIC parameters by value: big.Rat carries no
// Equal method and hides its fields from cmp.
var ratComparer = cmp.Comparer(func(a, b *big.Rat) bool {
	return a.Cmp(b) == 0
})

// TestLowerCondition_rendering pins the §05 rendering shapes over the running
// example: column comparisons bind literals as parameters (a decimal as
// NUMERIC), join paths render one scalar subquery per hop, subject sets
// render the anchor-table EXISTS under the attribute's null guard with the
// derived tenancy filter, subject values render scalar subqueries, facts bind
// as the reserved named parameters, and the post-write overlay reads the
// proposed value's parameter for touched columns and the existing column
// otherwise.
func TestLowerCondition_rendering(t *testing.T) {
	t.Parallel()

	collection := loweringFixtureCollection(t)
	bindings, ok := collection.Bindings(accesstypes.DomainPermissionScope, "MaintenanceTasks")
	if !ok {
		t.Fatal("fixture bindings missing")
	}

	tests := []struct {
		name           string
		source         string
		partitioned    bool
		proposed       map[string]any
		wantSQL        string
		wantParams     []QueryParam
		wantNamed      []string
		wantErrContain string
	}{
		{
			name:        "the shared write group: subject set OR state",
			source:      "crew IN subject.crews OR state = 'open'",
			partitioned: true,
			wantSQL:     "(CASE WHEN `t`.`CrewId` IS NULL THEN NULL ELSE EXISTS (SELECT 1 FROM `CrewMembers` `ca1` WHERE `ca1`.`UserId` = @subject AND `ca1`.`CrewId` = `t`.`CrewId` AND `ca1`.`StationId` = @domain) END OR `t`.`State` = @_c1)",
			wantParams:  []QueryParam{{Name: "_c1", Value: "open"}},
			wantNamed:   []string{"domain", "subject"},
		},
		{
			name:    "global request derives no tenancy filter",
			source:  "crew IN subject.crews",
			wantSQL: "CASE WHEN `t`.`CrewId` IS NULL THEN NULL ELSE EXISTS (SELECT 1 FROM `CrewMembers` `ca1` WHERE `ca1`.`UserId` = @subject AND `ca1`.`CrewId` = `t`.`CrewId`) END",

			wantNamed: []string{"subject"},
		},
		{
			name:        "dotted subject set continues through the anchor's join path",
			source:      "wing IN subject.wings",
			partitioned: true,
			wantSQL:     "CASE WHEN `t`.`WingId` IS NULL THEN NULL ELSE EXISTS (SELECT 1 FROM `CrewMembers` `ca1` WHERE `ca1`.`UserId` = @subject AND EXISTS (SELECT 1 FROM `Crews` `ca2` WHERE `ca2`.`Id` = `ca1`.`CrewId` AND `ca2`.`WingId` = `t`.`WingId`) AND `ca1`.`StationId` = @domain) END",
			wantNamed:   []string{"domain", "subject"},
		},
		{
			name:      "dotted subject value nests one scalar subquery per hop",
			source:    "crew = subject.homeSector",
			wantSQL:   "`t`.`CrewId` = (SELECT `ca2`.`Sector` FROM `Stations` `ca2` WHERE `ca2`.`Id` = (SELECT `ca1`.`StationId` FROM `UserProfiles` `ca1` WHERE `ca1`.`UserId` = @subject))",
			wantNamed: []string{"subject"},
		},
		{
			name:       "join-path attribute reads the related row's column as a scalar subquery",
			source:     "shipClass = 'Freighter'",
			wantSQL:    "(SELECT `ca1`.`Class` FROM `Ships` `ca1` WHERE `ca1`.`Id` = `t`.`ShipId`) = @_c1",
			wantParams: []QueryParam{{Name: "_c1", Value: "Freighter"}},
		},
		{
			name:       "two-hop path nests one scalar subquery per hop",
			source:     "sector = 'Kepler'",
			wantSQL:    "(SELECT `ca2`.`Sector` FROM `Stations` `ca2` WHERE `ca2`.`Id` = (SELECT `ca1`.`StationId` FROM `Berths` `ca1` WHERE `ca1`.`Id` = `t`.`BerthId`)) = @_c1",
			wantParams: []QueryParam{{Name: "_c1", Value: "Kepler"}},
		},
		{
			// A NULL foreign key or a NULL terminal makes the scalar NULL, so
			// the null test and the negated comparison read as SQL's UNKNOWN
			// on a missing value, never as TRUE.
			name:    "join-path null test reads the scalar",
			source:  "shipClass IS NULL OR NOT (shipClass = 'Freighter')",
			wantSQL: "((SELECT `ca1`.`Class` FROM `Ships` `ca1` WHERE `ca1`.`Id` = `t`.`ShipId`) IS NULL OR NOT ((SELECT `ca2`.`Class` FROM `Ships` `ca2` WHERE `ca2`.`Id` = `t`.`ShipId`) = @_c1))",

			wantParams: []QueryParam{{Name: "_c1", Value: "Freighter"}},
		},
		{
			name:       "join-path attribute in a literal list",
			source:     "shipClass IN ('Freighter', 'Tug')",
			wantSQL:    "(SELECT `ca1`.`Class` FROM `Ships` `ca1` WHERE `ca1`.`Id` = `t`.`ShipId`) IN (@_c1, @_c2)",
			wantParams: []QueryParam{{Name: "_c1", Value: "Freighter"}, {Name: "_c2", Value: "Tug"}},
		},
		{
			name:       "threshold: proposed value against a subject value",
			source:     "new.estimatedCost <= subject.approvalLimit",
			proposed:   map[string]any{"EstimatedCost": 1200.0},
			wantSQL:    "@_c1 <= (SELECT `ca1`.`ApprovalLimit` FROM `UserProfiles` `ca1` WHERE `ca1`.`UserId` = @subject)",
			wantParams: []QueryParam{{Name: "_c1", Value: 1200.0}},
			wantNamed:  []string{"_c1", "subject"},
		},
		{
			name:      "untouched post-write column reads the existing value",
			source:    "new.estimatedCost <= subject.approvalLimit",
			proposed:  map[string]any{},
			wantSQL:   "`t`.`EstimatedCost` <= (SELECT `ca1`.`ApprovalLimit` FROM `UserProfiles` `ca1` WHERE `ca1`.`UserId` = @subject)",
			wantNamed: []string{"subject"},
		},
		{
			name:       "capture guard: null test and subject fact",
			source:     "assignee IS NULL AND new.assignee = subject",
			proposed:   map[string]any{"Assignee": "u1"},
			wantSQL:    "(`t`.`Assignee` IS NULL AND @_c1 = @subject)",
			wantParams: []QueryParam{{Name: "_c1", Value: "u1"}},
			wantNamed:  []string{"_c1", "subject"},
		},
		{
			// A decimal literal binds as NUMERIC, the exact value it spells, so
			// a NUMERIC column is never compared through a double; an integer
			// literal stays INT64.
			name:       "literal list membership binds typed values",
			source:     "state IN ('open', 'approved') AND estimatedCost < 10.5 AND estimatedCost > 3",
			wantSQL:    "(`t`.`State` IN (@_c1, @_c2) AND `t`.`EstimatedCost` < @_c3 AND `t`.`EstimatedCost` > @_c4)",
			wantParams: []QueryParam{{Name: "_c1", Value: "open"}, {Name: "_c2", Value: "approved"}, {Name: "_c3", Value: big.NewRat(21, 2)}, {Name: "_c4", Value: int64(3)}},
		},
		{
			name:      "residual environment fact binds the reserved parameter",
			source:    "now < subject.clearedUntil",
			wantSQL:   "@now < ",
			wantNamed: []string{"now", "subject"},
		},
		{
			// The guard makes the membership test UNKNOWN on a NULL attribute,
			// where the bare NOT EXISTS would be TRUE.
			name:    "negated subject set",
			source:  "crew NOT IN subject.crews",
			wantSQL: "CASE WHEN `t`.`CrewId` IS NULL THEN NULL ELSE NOT (EXISTS (SELECT 1 FROM `CrewMembers` `ca1` WHERE `ca1`.`UserId` = @subject AND `ca1`.`CrewId` = `t`.`CrewId`)) END",

			wantNamed: []string{"subject"},
		},
		{
			name:           "unknown binding name",
			source:         "mystery = 'x'",
			wantErrContain: "not an attribute",
		},
		{
			name:           "post-write reference outside a write context",
			source:         "new.estimatedCost <= 5",
			wantErrContain: "outside a write context",
		},
		{
			name:           "unknown subject set",
			source:         "crew IN subject.teams",
			wantErrContain: "not a declared subject set",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			expr, err := condition.Parse(tt.source)
			if err != nil {
				t.Fatalf("condition.Parse(%q) error = %v", tt.source, err)
			}

			ctx := &loweringContext{
				outer:       "t",
				bindings:    bindings,
				collection:  collection,
				partitioned: tt.partitioned,
			}
			if tt.proposed != nil {
				ctx.proposed = newProposedOverlay(tt.proposed)
			}
			registry := newParamRegistry()

			node, err := lowerCondition(expr, ctx, registry)
			if tt.wantErrContain != "" {
				if err == nil {
					t.Fatalf("lowerCondition(%q) expected an error containing %q, got nil", tt.source, tt.wantErrContain)
				}
				if !strings.Contains(err.Error(), tt.wantErrContain) {
					t.Errorf("lowerCondition(%q) error = %q, want containing %q", tt.source, err, tt.wantErrContain)
				}

				return
			}
			if err != nil {
				t.Fatalf("lowerCondition(%q) error = %v", tt.source, err)
			}

			sql, err := newSQLGenerator(Spanner).generateLowered(node, registry)
			if err != nil {
				t.Fatalf("generateLowered(%q) error = %v", tt.source, err)
			}

			if tt.name == "residual environment fact binds the reserved parameter" {
				if !strings.HasPrefix(sql, tt.wantSQL) {
					t.Errorf("SQL = %q, want prefix %q", sql, tt.wantSQL)
				}
			} else if sql != tt.wantSQL {
				t.Errorf("SQL mismatch:\n got %q\nwant %q", sql, tt.wantSQL)
			}

			if diff := cmp.Diff(tt.wantParams, registry.boundParams(), ratComparer); diff != "" {
				t.Errorf("bound params mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.wantNamed, registry.referencedNames()); diff != "" {
				t.Errorf("referenced named params mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestLowerCondition_statementScope pins the registry contract: fragments of
// one statement share the namespace, so parameters and aliases never collide
// across two lowered conditions.
func TestLowerCondition_statementScope(t *testing.T) {
	t.Parallel()

	collection := loweringFixtureCollection(t)
	bindings, _ := collection.Bindings(accesstypes.DomainPermissionScope, "MaintenanceTasks")
	ctx := &loweringContext{outer: "t", bindings: bindings, collection: collection}
	registry := newParamRegistry()
	generator := newSQLGenerator(Spanner)

	first, err := condition.Parse("state = 'open'")
	if err != nil {
		t.Fatal(err)
	}
	second, err := condition.Parse("shipClass = 'Freighter'")
	if err != nil {
		t.Fatal(err)
	}

	firstNode, err := lowerCondition(first, ctx, registry)
	if err != nil {
		t.Fatalf("lowerCondition(first) error = %v", err)
	}
	secondNode, err := lowerCondition(second, ctx, registry)
	if err != nil {
		t.Fatalf("lowerCondition(second) error = %v", err)
	}

	firstSQL, err := generator.generateLowered(firstNode, registry)
	if err != nil {
		t.Fatal(err)
	}
	secondSQL, err := generator.generateLowered(secondNode, registry)
	if err != nil {
		t.Fatal(err)
	}

	if want := "`t`.`State` = @_c1"; firstSQL != want {
		t.Errorf("first fragment = %q, want %q", firstSQL, want)
	}
	if want := "(SELECT `ca1`.`Class` FROM `Ships` `ca1` WHERE `ca1`.`Id` = `t`.`ShipId`) = @_c2"; secondSQL != want {
		t.Errorf("second fragment = %q, want %q", secondSQL, want)
	}
	want := []QueryParam{{Name: "_c1", Value: "open"}, {Name: "_c2", Value: "Freighter"}}
	if diff := cmp.Diff(want, registry.boundParams()); diff != "" {
		t.Errorf("bound params mismatch (-want +got):\n%s", diff)
	}
}

// TestLoweredNodes_requireRegistry pins the constructor gate: a lowered node
// reaching the filter-path generator (no statement registry) is an error,
// never silent SQL.
func TestLoweredNodes_requireRegistry(t *testing.T) {
	t.Parallel()

	_, _, err := NewSpannerGenerator().GenerateSQL(&truthNode{value: true})
	if err == nil {
		t.Fatal("GenerateSQL(lowered node) expected an error outside a statement registry, got nil")
	}
	if !strings.Contains(err.Error(), "registry") {
		t.Errorf("GenerateSQL(lowered node) error = %q, want the registry gate", err)
	}
}
