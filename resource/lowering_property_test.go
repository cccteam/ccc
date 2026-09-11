package resource

// Property tests over the compiler → fold → lowering → SQL renderer pipeline
// (design plan §11): for randomly generated conditions across the lowering
// fixture's vocabulary, the residue the engine's fold leaves lowers and
// renders without failing, renders deterministically, allocates each bound
// parameter exactly once and references it from the SQL, and references no
// named parameter outside the reserved set. The conditions come from the
// shared conditiontest generator, so a grammar addition reaches the lowering
// through the same source as every other property; the generator is seeded,
// so a failure reproduces and each failing case prints its source text.

import (
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"
	"time"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/accesstypes/condition"
	"github.com/cccteam/ccc/accesstypes/condition/conditiontest"
)

// loweringFixtureVocabulary is the fixture's vocabulary as the generator sees
// it: MaintenanceTasks' column and join-path attributes with their types, the
// subject sets and values the anchors declare, and the post-write overlay
// (the property renders in an update context, so new. is legal).
func loweringFixtureVocabulary(t *testing.T, collection *GeneratedCollection) conditiontest.Vocabulary {
	t.Helper()

	bindings, ok := collection.Bindings(accesstypes.DomainPermissionScope, "MaintenanceTasks")
	if !ok {
		t.Fatal("fixture bindings missing")
	}

	vocab := conditiontest.Vocabulary{
		SubjectSets:   []string{"crews", "wings"},
		SubjectValues: []string{"approvalLimit", "homeSector"},
		PostImage:     true,
	}
	for _, attr := range bindings.Attributes {
		vocab.Attributes = append(vocab.Attributes, conditiontest.Attribute{Name: attr.Name, Type: attr.Type, JoinPath: len(attr.Path) > 0})
	}

	return vocab
}

// renderOnce lowers and renders one folded condition with a fresh registry.
func renderOnce(t *testing.T, expr condition.Expr, collection *GeneratedCollection, proposed map[string]any) (sql string, bound []QueryParam, named []string) {
	t.Helper()

	bindings, ok := collection.Bindings(accesstypes.DomainPermissionScope, "MaintenanceTasks")
	if !ok {
		t.Fatal("fixture bindings missing")
	}

	lctx := &loweringContext{
		outer:       "t",
		bindings:    bindings,
		collection:  collection,
		partitioned: true,
		proposed:    newProposedOverlay(proposed),
	}
	registry := newParamRegistry()

	sql, err := lowerToSQL(expr, lctx, newSQLGenerator(Spanner), registry)
	if err != nil {
		t.Fatalf("lowering %q error = %v", expr.String(), err)
	}

	return sql, registry.boundParams(), registry.referencedNames()
}

func TestLowering_renderProperty(t *testing.T) {
	t.Parallel()

	collection := loweringFixtureCollection(t)
	proposed := map[string]any{"CrewId": "c1", "State": "open", "EstimatedCost": 12.5, "Assignee": "u2"}
	reserved := map[string]struct{}{subjectParamName: {}, nowParamName: {}, domainParamName: {}}

	// The engine folds the environment facts before the residue reaches the
	// resource layer: temporal terms and now-vs-literal comparisons settle
	// here, and only what the database must evaluate lowers.
	facts := condition.NewFacts().
		WithNow(time.Date(2026, 9, 11, 15, 0, 0, 0, time.UTC)).
		WithZone(time.UTC)

	vocab := loweringFixtureVocabulary(t, collection)
	gen := conditiontest.New(rand.New(rand.NewPCG(20260901, 5)), &vocab)
	for i := range 1000 {
		expr := gen.Expr(3)
		source := expr.String()

		residue, err := condition.Fold(expr, facts)
		if err != nil {
			t.Fatalf("case %d (%s): Fold() error = %v", i, source, err)
		}

		sql, params, named := renderOnce(t, residue, collection, proposed)
		sql2, params2, named2 := renderOnce(t, residue, collection, proposed)

		// Rendering is deterministic: same SQL, same parameters, same
		// referenced names, across independent registries.
		if sql != sql2 {
			t.Fatalf("case %d (%s): render diverged:\n%s\n%s", i, source, sql, sql2)
		}
		if fmt.Sprint(params) != fmt.Sprint(params2) || fmt.Sprint(named) != fmt.Sprint(named2) {
			t.Fatalf("case %d (%s): parameters diverged:\n%v / %v\n%v / %v", i, source, params, named, params2, named2)
		}

		// Every bound parameter is unique and referenced by the SQL.
		seen := map[string]struct{}{}
		for _, param := range params {
			if _, dup := seen[param.Name]; dup {
				t.Fatalf("case %d (%s): parameter %s bound twice", i, source, param.Name)
			}
			seen[param.Name] = struct{}{}
			if !strings.Contains(sql, "@"+param.Name) {
				t.Fatalf("case %d (%s): bound parameter @%s absent from SQL:\n%s", i, source, param.Name, sql)
			}
		}

		// Referenced named parameters stay inside the reserved vocabulary
		// plus the bound overlay names.
		for _, name := range named {
			if _, ok := reserved[name]; ok {
				continue
			}
			if _, ok := seen[name]; !ok {
				t.Fatalf("case %d (%s): referenced name %q is neither reserved nor bound", i, source, name)
			}
		}
	}
}
