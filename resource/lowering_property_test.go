package resource

// Property tests over the compiler → lowering → SQL renderer pair (design
// plan §11): for randomly generated conditions across the fixture vocabulary,
// lowering plus rendering never fails, renders deterministically, allocates
// each bound parameter exactly once and references it from the SQL, and
// references no named parameter outside the reserved set. The generator is
// seeded, so a failure reproduces; each failing case prints its source text.

import (
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/accesstypes/condition"
)

// genRenderableCondition builds a random condition source over the lowering
// fixture's vocabulary: column and join-path attributes, the crews subject
// set, the approvalLimit subject value, facts, and the post-write overlay
// (the property renders in an update context, so new. is always legal).
func genRenderableCondition(rng *rand.Rand, depth int) string {
	if depth <= 0 {
		return genRenderableLeaf(rng)
	}
	switch rng.IntN(6) {
	case 0:
		return fmt.Sprintf("(%s AND %s)", genRenderableCondition(rng, depth-1), genRenderableCondition(rng, depth-1))
	case 1:
		return fmt.Sprintf("(%s OR %s)", genRenderableCondition(rng, depth-1), genRenderableCondition(rng, depth-1))
	case 2:
		return fmt.Sprintf("NOT (%s)", genRenderableCondition(rng, depth-1))
	default:
		return genRenderableLeaf(rng)
	}
}

func genRenderableLeaf(rng *rand.Rand) string {
	attrs := []string{"crew", "state", "estimatedCost", "shipClass", "sector", "assignee"}
	columnAttrs := []string{"crew", "state", "estimatedCost", "assignee"}
	ops := []string{"=", "!=", "<", "<=", ">", ">="}

	attr := attrs[rng.IntN(len(attrs))]
	op := ops[rng.IntN(len(ops))]

	switch rng.IntN(8) {
	case 0:
		return fmt.Sprintf("%s %s '%s'", attr, op, []string{"open", "it''s", "x y"}[rng.IntN(3)])
	case 1:
		return fmt.Sprintf("%s %s %s", attr, op, []string{"0", "42", "10.5", "-3"}[rng.IntN(4)])
	case 2:
		return attr + " = subject"
	case 3:
		return fmt.Sprintf("%s %s subject.approvalLimit", attr, op)
	case 4:
		return fmt.Sprintf("%s IN ('a', 'b', %d)", columnAttrs[rng.IntN(len(columnAttrs))], rng.IntN(100))
	case 5:
		negate := ""
		if rng.IntN(2) == 0 {
			negate = "NOT "
		}

		return fmt.Sprintf("%s %sIN subject.crews", columnAttrs[rng.IntN(len(columnAttrs))], negate)
	case 6:
		null := "IS NULL"
		if rng.IntN(2) == 0 {
			null = "IS NOT NULL"
		}

		return fmt.Sprintf("%s %s", columnAttrs[rng.IntN(len(columnAttrs))], null)
	default:
		return fmt.Sprintf("new.%s %s '%s'", columnAttrs[rng.IntN(len(columnAttrs))], op, "v")
	}
}

// renderOnce lowers and renders one condition with a fresh registry.
func renderOnce(t *testing.T, source string, collection *GeneratedCollection, proposed map[string]any) (sql string, bound []QueryParam, named []string) {
	t.Helper()

	expr, err := condition.Parse(source)
	if err != nil {
		t.Fatalf("condition.Parse(%q) error = %v", source, err)
	}

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

	sql, err = lowerToSQL(expr, lctx, newSQLGenerator(Spanner), registry)
	if err != nil {
		t.Fatalf("lowering %q error = %v", source, err)
	}

	return sql, registry.boundParams(), registry.referencedNames()
}

func TestLowering_renderProperty(t *testing.T) {
	t.Parallel()

	collection := loweringFixtureCollection(t)
	proposed := map[string]any{"CrewId": "c1", "State": "open", "EstimatedCost": 12.5, "Assignee": "u2"}
	reserved := map[string]struct{}{subjectParamName: {}, nowParamName: {}, domainParamName: {}}

	rng := rand.New(rand.NewPCG(20260901, 5))
	for i := range 1000 {
		source := genRenderableCondition(rng, 3)

		sql, params, named := renderOnce(t, source, collection, proposed)
		sql2, params2, named2 := renderOnce(t, source, collection, proposed)

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
