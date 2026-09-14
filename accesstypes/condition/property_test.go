package condition_test

// Property tests over the condition compiler (design plan §11): for randomly
// generated expressions across the whole vocabulary, the canonical String()
// form reparses to the identical rendering — String is a faithful, stable
// encoding and Parse is its inverse. The expressions come from the shared
// conditiontest generator, so every production the grammar has reaches this
// property; the generator is seeded, so a failure reproduces.

import (
	"fmt"
	"math/rand/v2"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/accesstypes/condition"
	"github.com/cccteam/ccc/accesstypes/condition/conditiontest"
)

// roundTripVocabulary spans every comparison type, both subject forms, a
// join-path attribute, and a write context, so the property sees every leaf
// the generator can draw.
var roundTripVocabulary = conditiontest.Vocabulary{
	Attributes: []conditiontest.Attribute{
		{Name: "owner", Type: accesstypes.AttributeTypeString},
		{Name: "state", Type: accesstypes.AttributeTypeString},
		{Name: "priority", Type: accesstypes.AttributeTypeNumber},
		{Name: "estimated_cost", Type: accesstypes.AttributeTypeNumber},
		{Name: "x", Type: accesstypes.AttributeTypeBool},
		{Name: "A1", Type: accesstypes.AttributeTypeTimestamp},
		{Name: "startsOn", Type: accesstypes.AttributeTypeDate},
		{Name: "shipClass", Type: accesstypes.AttributeTypeString, JoinPath: true},
	},
	SubjectSets:   []string{"crews", "wings"},
	SubjectValues: []string{"approvalLimit", "homeSector"},
	PostImage:     true,
}

// TestParse_stringRoundTripProperty: Parse(expr.String()).String() ==
// expr.String() for arbitrary generated expressions.
func TestParse_stringRoundTripProperty(t *testing.T) {
	t.Parallel()

	gen := conditiontest.New(rand.New(rand.NewPCG(20260901, 11)), &roundTripVocabulary)
	for i := range 2000 {
		expr := gen.Expr(4)
		source := expr.String()

		reparsed, err := condition.Parse(source)
		if err != nil {
			t.Fatalf("case %d: Parse(%q) error = %v", i, source, err)
		}
		if got := reparsed.String(); got != source {
			t.Fatalf("case %d: round trip diverged:\n source: %s\nreparse: %s", i, source, got)
		}
	}
}

// TestParse_rejectsOversizeDepth pins that the generator-reachable shapes stay
// inside the language limits used above (a guard on the property itself: if
// the generator ever outgrows the limits, the round-trip failure should name
// the limit, not confuse the property).
func TestParse_rejectsOversizeDepth(t *testing.T) {
	t.Parallel()

	source := ""
	for range 40 {
		source += "NOT ("
	}
	source += "owner = subject"
	for range 40 {
		source += ")"
	}
	if _, err := condition.Parse(source); err == nil {
		t.Fatal("Parse() accepted a 40-deep tree, want the depth limit to reject it")
	}

	if _, err := condition.Parse(fmt.Sprintf("owner = '%s'", string(make([]byte, 5000)))); err == nil {
		t.Fatal("Parse() accepted a 5KB source, want the size limit to reject it")
	}
}

// implicationVocabulary keeps the generated leaves dense on two string
// attributes and one number, so same-reference literal sets that overlap come
// up often enough for the transitivity property to fire.
var implicationVocabulary = conditiontest.Vocabulary{
	Attributes: []conditiontest.Attribute{
		{Name: "state", Type: accesstypes.AttributeTypeString},
		{Name: "owner", Type: accesstypes.AttributeTypeString},
		{Name: "hazard", Type: accesstypes.AttributeTypeNumber},
	},
	SubjectSets:   []string{"crews"},
	SubjectValues: []string{"homeSector"},
}

// leafRef is the reference a generated leaf tests: its left side's canonical
// text (an attribute, new.attribute, now, or a temporal function of now).
func leafRef(t *testing.T, e condition.Expr) string {
	t.Helper()

	switch e := e.(type) {
	case condition.Comparison:
		return e.Left.String()
	case condition.In:
		return e.Left.String()
	case condition.NullTest:
		return e.Left.String()
	default:
		t.Fatalf("generated leaf %s is a %T, not a leaf", e, e)

		return ""
	}
}

// literalSetHolds evaluates a positive or negated literal-set leaf on a value,
// given as a literal's canonical text, the way a database would on a non-NULL
// cell: membership in the list, or its negation. ok is false for a leaf that
// is not a literal set.
func literalSetHolds(e condition.Expr, value string) (holds, ok bool) {
	var negated bool
	var literals []condition.Literal
	switch e := e.(type) {
	case condition.Comparison:
		lit, isLiteral := e.Right.(condition.Literal)
		if !isLiteral || (e.Op != condition.Eq && e.Op != condition.NotEq) {
			return false, false
		}
		negated, literals = e.Op == condition.NotEq, []condition.Literal{lit}
	case condition.In:
		if e.SubjectSet != "" {
			return false, false
		}
		negated, literals = e.Negated, e.Literals
	default:
		return false, false
	}

	member := false
	for _, lit := range literals {
		if lit.String() == value {
			member = true
		}
	}

	return member != negated, true
}

// TestImplies_properties: over generated leaves, Implies is reflexive, holds
// only between leaves on one reference, is transitive, and never claims an
// implication a value refutes; and any disjunct list covers itself.
func TestImplies_properties(t *testing.T) {
	t.Parallel()

	gen := conditiontest.New(rand.New(rand.NewPCG(20260914, 61)), &implicationVocabulary)
	leaves := make([]condition.Expr, 0, 400)
	for range 400 {
		leaves = append(leaves, gen.Expr(0))
	}

	for i, leaf := range leaves {
		if !condition.Implies(leaf, leaf) {
			t.Fatalf("leaf %d: Implies(%s, itself) = false", i, leaf)
		}
	}
	for i := range 300 {
		disjuncts := condition.Disjuncts(gen.Expr(3))
		if uncovered := condition.Uncovered(disjuncts, disjuncts); len(uncovered) != 0 {
			t.Fatalf("case %d: Uncovered(x, x) = %v for %v", i, uncovered, disjuncts)
		}
		if !condition.Covers(disjuncts, disjuncts) {
			t.Fatalf("case %d: Covers(x, x) = false for %v", i, disjuncts)
		}
	}

	// The counts prove the rules fired rather than the properties holding
	// vacuously.
	implied, transitive := 0, 0
	for _, p := range leaves {
		for _, q := range leaves {
			if leafRef(t, p) != leafRef(t, q) {
				if condition.Implies(p, q) {
					t.Fatalf("Implies(%s, %s) = true across references", p, q)
				}

				continue
			}
			if !condition.Implies(p, q) {
				continue
			}
			if p.String() != q.String() {
				implied++
			}
			checkImplicationSound(t, p, q)
			transitive += checkImplicationTransitive(t, leaves, p, q)
		}
	}
	if implied < 50 || transitive < 20 {
		t.Fatalf("the property fired on %d implications and %d transitive chains, too few to have exercised the rules", implied, transitive)
	}
}

// checkImplicationSound fails when a value the two literal-set leaves name
// satisfies the premise and not the conclusion.
func checkImplicationSound(t *testing.T, p, q condition.Expr) {
	t.Helper()

	for _, value := range literalTexts(p, q) {
		pHolds, pOK := literalSetHolds(p, value)
		qHolds, qOK := literalSetHolds(q, value)
		if pOK && qOK && pHolds && !qHolds {
			t.Fatalf("Implies(%s, %s) = true, but the value %s satisfies the premise and not the conclusion", p, q, value)
		}
	}
}

// checkImplicationTransitive fails when q implies a same-reference leaf r
// that p does not, and counts the chains whose three leaves differ.
func checkImplicationTransitive(t *testing.T, leaves []condition.Expr, p, q condition.Expr) int {
	t.Helper()

	chains := 0
	for _, r := range leaves {
		if leafRef(t, q) != leafRef(t, r) || !condition.Implies(q, r) {
			continue
		}
		if !condition.Implies(p, r) {
			t.Fatalf("Implies(%s, %s) and Implies(%s, %s), but Implies(%s, %s) = false", p, q, q, r, p, r)
		}
		if p.String() != q.String() && q.String() != r.String() {
			chains++
		}
	}

	return chains
}

// literalTexts is every literal either leaf names, in canonical text, plus one
// no leaf names: the values on which the two can differ.
func literalTexts(leaves ...condition.Expr) []string {
	texts := []string{"'a value no pool holds'"}
	for _, leaf := range leaves {
		switch e := leaf.(type) {
		case condition.Comparison:
			if lit, ok := e.Right.(condition.Literal); ok {
				texts = append(texts, lit.String())
			}
		case condition.In:
			for _, lit := range e.Literals {
				texts = append(texts, lit.String())
			}
		}
	}

	return texts
}
