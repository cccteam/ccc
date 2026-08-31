package resource

import (
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
// approvalLimit subject value (global — no filter).
func loweringFixtureCollection(t *testing.T) *GeneratedCollection {
	t.Helper()

	g, err := NewGeneratedCollection(CollectionData{Resources: []CollectionResource{
		{
			Name:  "MaintenanceTasks",
			Scope: accesstypes.DomainPermissionScope,
			Attributes: []AttributeData{
				{Name: "crew", Column: "CrewId"},
				{Name: "state", Column: "State"},
				{Name: "estimatedCost", Column: "EstimatedCost"},
				{Name: "shipClass", Column: "ShipId", Path: []BindingHop{{Table: "Ships", JoinColumn: "Id", Column: "Class"}}},
				{Name: "sector", Column: "BerthId", Path: []BindingHop{
					{Table: "Berths", JoinColumn: "Id", Column: "StationId"},
					{Table: "Stations", JoinColumn: "Id", Column: "Sector"},
				}},
				{Name: "assignee", Column: "Assignee"},
			},
			Domain: &DomainBindingData{Column: "StationId"},
		},
		{
			Name:        "CrewMembers",
			Scope:       accesstypes.DomainPermissionScope,
			SubjectSets: []SubjectBindingData{{Name: "crews", UserColumn: "UserId", Column: "CrewId"}},
			Domain:      &DomainBindingData{Column: "StationId"},
		},
		{
			Name:          "UserProfiles",
			Scope:         accesstypes.GlobalPermissionScope,
			SubjectValues: []SubjectBindingData{{Name: "approvalLimit", UserColumn: "UserId", Column: "ApprovalLimit"}},
		},
	}})
	if err != nil {
		t.Fatalf("NewGeneratedCollection() error = %v", err)
	}

	return g
}

// TestLowerCondition_rendering pins the §05 rendering shapes over the running
// example: column comparisons bind literals as parameters, join paths render
// nested correlated EXISTS, subject sets render the anchor-table EXISTS with
// the derived tenancy filter, subject values render scalar subqueries, facts
// bind as the reserved named parameters, and the post-write overlay reads the
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
		newValueParams map[string]string
		wantSQL        string
		wantParams     []QueryParam
		wantNamed      []string
		wantErrContain string
	}{
		{
			name:        "the shared write group: subject set OR state",
			source:      "crew IN subject.crews OR state = 'open'",
			partitioned: true,
			wantSQL:     "(EXISTS (SELECT 1 FROM `CrewMembers` `ca1` WHERE `ca1`.`UserId` = @subject AND `ca1`.`CrewId` = `t`.`CrewId` AND `ca1`.`StationId` = @domain) OR `t`.`State` = @_c1)",
			wantParams:  []QueryParam{{Name: "_c1", Value: "open"}},
			wantNamed:   []string{"domain", "subject"},
		},
		{
			name:    "global request derives no tenancy filter",
			source:  "crew IN subject.crews",
			wantSQL: "EXISTS (SELECT 1 FROM `CrewMembers` `ca1` WHERE `ca1`.`UserId` = @subject AND `ca1`.`CrewId` = `t`.`CrewId`)",

			wantNamed: []string{"subject"},
		},
		{
			name:       "join-path attribute renders a correlated EXISTS",
			source:     "shipClass = 'Freighter'",
			wantSQL:    "EXISTS (SELECT 1 FROM `Ships` `ca1` WHERE `ca1`.`Id` = `t`.`ShipId` AND `ca1`.`Class` = @_c1)",
			wantParams: []QueryParam{{Name: "_c1", Value: "Freighter"}},
		},
		{
			name:       "two-hop path nests EXISTS per hop",
			source:     "sector = 'Kepler'",
			wantSQL:    "EXISTS (SELECT 1 FROM `Berths` `ca1` WHERE `ca1`.`Id` = `t`.`BerthId` AND EXISTS (SELECT 1 FROM `Stations` `ca2` WHERE `ca2`.`Id` = `ca1`.`StationId` AND `ca2`.`Sector` = @_c1))",
			wantParams: []QueryParam{{Name: "_c1", Value: "Kepler"}},
		},
		{
			name:           "threshold: proposed value against a subject value",
			source:         "new.estimatedCost <= subject.approvalLimit",
			newValueParams: map[string]string{"EstimatedCost": "new_EstimatedCost"},
			wantSQL:        "@new_EstimatedCost <= (SELECT `ca1`.`ApprovalLimit` FROM `UserProfiles` `ca1` WHERE `ca1`.`UserId` = @subject)",
			wantNamed:      []string{"new_EstimatedCost", "subject"},
		},
		{
			name:           "untouched post-write column reads the existing value",
			source:         "new.estimatedCost <= subject.approvalLimit",
			newValueParams: map[string]string{},
			wantSQL:        "`t`.`EstimatedCost` <= (SELECT `ca1`.`ApprovalLimit` FROM `UserProfiles` `ca1` WHERE `ca1`.`UserId` = @subject)",
			wantNamed:      []string{"subject"},
		},
		{
			name:           "capture guard: null test and subject fact",
			source:         "assignee IS NULL AND new.assignee = subject",
			newValueParams: map[string]string{"Assignee": "new_Assignee"},
			wantSQL:        "(`t`.`Assignee` IS NULL AND @new_Assignee = @subject)",
			wantNamed:      []string{"new_Assignee", "subject"},
		},
		{
			name:       "literal list membership binds typed values",
			source:     "state IN ('open', 'approved') AND estimatedCost < 10.5",
			wantSQL:    "(`t`.`State` IN (@_c1, @_c2) AND `t`.`EstimatedCost` < @_c3)",
			wantParams: []QueryParam{{Name: "_c1", Value: "open"}, {Name: "_c2", Value: "approved"}, {Name: "_c3", Value: 10.5}},
		},
		{
			name:      "residual environment fact binds the reserved parameter",
			source:    "now < subject.approvalLimit",
			wantSQL:   "@now < ",
			wantNamed: []string{"now", "subject"},
		},
		{
			name:    "negated subject set",
			source:  "crew NOT IN subject.crews",
			wantSQL: "NOT (EXISTS (SELECT 1 FROM `CrewMembers` `ca1` WHERE `ca1`.`UserId` = @subject AND `ca1`.`CrewId` = `t`.`CrewId`))",

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
				outer:          "t",
				bindings:       bindings,
				collection:     collection,
				partitioned:    tt.partitioned,
				newValueParams: tt.newValueParams,
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

			if diff := cmp.Diff(tt.wantParams, registry.boundParams()); diff != "" {
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
	if want := "EXISTS (SELECT 1 FROM `Ships` `ca1` WHERE `ca1`.`Id` = `t`.`ShipId` AND `ca1`.`Class` = @_c2)"; secondSQL != want {
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
