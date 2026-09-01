package condition

// Property tests over the condition compiler (design plan §11): for randomly
// generated expressions across the whole vocabulary, the canonical String()
// form reparses to the identical tree — String is a faithful, stable encoding
// and Parse is its inverse. The generator is seeded, so a failure reproduces.

import (
	"fmt"
	"math/rand/v2"
	"testing"
)

// genExpr builds a random expression of bounded depth over the vocabulary.
func genExpr(rng *rand.Rand, depth int) Expr {
	if depth <= 0 {
		return genLeaf(rng)
	}
	switch rng.IntN(6) {
	case 0:
		return And{Operands: []Expr{genExpr(rng, depth-1), genExpr(rng, depth-1)}}
	case 1:
		return Or{Operands: []Expr{genExpr(rng, depth-1), genExpr(rng, depth-1)}}
	case 2:
		return Not{Operand: genExpr(rng, depth-1)}
	default:
		return genLeaf(rng)
	}
}

func genLeaf(rng *rand.Rand) Expr {
	ops := []CompareOp{Eq, NotEq, Less, LessEq, Greater, GreaterEq}
	switch rng.IntN(5) {
	case 0:
		return Comparison{Left: genRef(rng), Op: ops[rng.IntN(len(ops))], Right: genOperand(rng)}
	case 1:
		// now is relational-only against its operand forms.
		return Comparison{Left: Ref{Name: "now"}, Op: ops[rng.IntN(len(ops))], Right: StringLiteral{Value: "2026-01-02T15:04:05Z"}}
	case 2:
		return In{Left: genRef(rng), Negated: rng.IntN(2) == 0, Literals: genLiterals(rng)}
	case 3:
		return In{Left: genRef(rng), Negated: rng.IntN(2) == 0, SubjectSet: genName(rng)}
	default:
		return NullTest{Left: genRef(rng), Negated: rng.IntN(2) == 0}
	}
}

func genRef(rng *rand.Rand) Ref {
	return Ref{Name: genName(rng), PostImage: rng.IntN(4) == 0}
}

func genName(rng *rand.Rand) string {
	names := []string{"owner", "state", "priority", "crewId", "estimated_cost", "x", "A1"}

	return names[rng.IntN(len(names))]
}

func genOperand(rng *rand.Rand) Operand {
	switch rng.IntN(6) {
	case 0:
		return StringLiteral{Value: genString(rng)}
	case 1:
		return NumberLiteral{Text: genNumberText(rng)}
	case 2:
		return BoolLiteral{Value: rng.IntN(2) == 0}
	case 3:
		return Subject{}
	case 4:
		return Now{}
	default:
		return SubjectValue{Name: genName(rng)}
	}
}

func genLiterals(rng *rand.Rand) []Literal {
	n := 1 + rng.IntN(3)
	literals := make([]Literal, 0, n)
	for range n {
		switch rng.IntN(3) {
		case 0:
			literals = append(literals, StringLiteral{Value: genString(rng)})
		case 1:
			literals = append(literals, NumberLiteral{Text: genNumberText(rng)})
		default:
			literals = append(literals, BoolLiteral{Value: rng.IntN(2) == 0})
		}
	}

	return literals
}

func genString(rng *rand.Rand) string {
	// Includes the quote-doubling edge, spaces, and keyword-shaped content.
	values := []string{"open", "it's", "''", "", "AND", "subject", "2026-01-02T15:04:05Z", "a b c"}

	return values[rng.IntN(len(values))]
}

func genNumberText(rng *rand.Rand) string {
	values := []string{"0", "42", "10.5", "-3", "-0.25", "1000000"}

	return values[rng.IntN(len(values))]
}

// TestParse_stringRoundTripProperty: Parse(expr.String()).String() ==
// expr.String() for arbitrary generated expressions.
func TestParse_stringRoundTripProperty(t *testing.T) {
	t.Parallel()

	rng := rand.New(rand.NewPCG(20260901, 11))
	for i := range 2000 {
		expr := genExpr(rng, 4)
		source := expr.String()

		reparsed, err := Parse(source)
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
	if _, err := Parse(source); err == nil {
		t.Fatal("Parse() accepted a 40-deep tree, want the depth limit to reject it")
	}

	if _, err := Parse(fmt.Sprintf("owner = '%s'", string(make([]byte, 5000)))); err == nil {
		t.Fatal("Parse() accepted a 5KB source, want the size limit to reject it")
	}
}
