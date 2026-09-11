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
