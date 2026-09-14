package condition

import (
	"slices"
	"testing"
)

func mustParse(t *testing.T, source string) Expr {
	t.Helper()

	e, err := Parse(source)
	if err != nil {
		t.Fatalf("Parse(%q) error = %v", source, err)
	}

	return e
}

func TestDisjuncts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
		want   []string
	}{
		{name: "a comparison is its own disjunct", source: "state = 'open'", want: []string{"state = 'open'"}},
		{name: "an OR splits", source: "state = 'open' OR hazard < 3", want: []string{"state = 'open'", "hazard < 3"}},
		{name: "nested ORs flatten", source: "(state = 'open' OR hazard < 3) OR owner = subject", want: []string{"state = 'open'", "hazard < 3", "owner = subject"}},
		{name: "an AND stays whole", source: "state = 'open' AND hazard < 3", want: []string{"state = 'open' AND hazard < 3"}},
		{name: "an AND inside an OR is one disjunct", source: "state = 'open' OR (hazard < 3 AND owner = subject)", want: []string{"state = 'open'", "hazard < 3 AND owner = subject"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var got []string
			for _, d := range Disjuncts(mustParse(t, tt.source)) {
				got = append(got, d.String())
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("Disjuncts() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestImplies(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		premise    string
		conclusion string
		want       bool
	}{
		// Rule 1: identity.
		{name: "identical text", premise: "state = 'open'", conclusion: "state = 'open'", want: true},
		{name: "spelling differences are canonical", premise: "state='open'", conclusion: "state = 'open'", want: true},
		{name: "a range implies only itself", premise: "hazard < 3", conclusion: "hazard < 3", want: true},
		{name: "a range does not imply a wider range", premise: "hazard < 3", conclusion: "hazard < 4", want: false},
		// Rule 2: positive literal-set inclusion.
		{name: "an equality implies an IN list holding its value", premise: "state = 'completed'", conclusion: "state IN ('completed', 'failed', 'stood_down')", want: true},
		{name: "an equality does not imply an IN list without its value", premise: "state = 'open'", conclusion: "state IN ('completed', 'failed')", want: false},
		{name: "an IN list implies a superset", premise: "state IN ('completed', 'failed')", conclusion: "state IN ('completed', 'failed', 'stood_down')", want: true},
		{name: "an IN list does not imply a subset", premise: "state IN ('completed', 'failed', 'stood_down')", conclusion: "state IN ('completed', 'failed')", want: false},
		{name: "an IN list does not imply an equality outside it", premise: "state IN ('completed', 'failed')", conclusion: "state = 'completed'", want: false},
		{name: "a one-element IN list and the equality imply each other", premise: "state IN ('completed')", conclusion: "state = 'completed'", want: true},
		{name: "the equality implies the one-element IN list", premise: "state = 'completed'", conclusion: "state IN ('completed')", want: true},
		{name: "order inside the IN list does not matter", premise: "state IN ('failed', 'completed')", conclusion: "state IN ('completed', 'failed')", want: true},
		{name: "numbers compare as literals too", premise: "hazard = 3", conclusion: "hazard IN (1, 2, 3)", want: true},
		// Rule 3: the negated dual.
		{name: "NOT IN a wider list implies NOT IN a narrower one", premise: "state NOT IN ('completed', 'failed')", conclusion: "state NOT IN ('completed')", want: true},
		{name: "NOT IN a narrower list does not imply NOT IN a wider one", premise: "state NOT IN ('completed')", conclusion: "state NOT IN ('completed', 'failed')", want: false},
		{name: "NOT IN implies the inequality on one of its values", premise: "state NOT IN ('completed', 'failed')", conclusion: "state != 'failed'", want: true},
		{name: "an inequality implies NOT IN a list of just its value", premise: "state != 'failed'", conclusion: "state NOT IN ('failed')", want: true},
		{name: "an inequality does not imply NOT IN a longer list", premise: "state != 'failed'", conclusion: "state NOT IN ('completed', 'failed')", want: false},
		{name: "polarities do not mix: an equality does not imply NOT IN", premise: "state = 'open'", conclusion: "state NOT IN ('completed')", want: false},
		{name: "polarities do not mix: NOT IN does not imply an equality", premise: "state NOT IN ('completed')", conclusion: "state = 'open'", want: false},
		// Rule 4: a conjunction implies through any conjunct.
		{name: "a conjunction implies what one conjunct implies", premise: "state = 'completed' AND fee > 0", conclusion: "state IN ('completed', 'failed')", want: true},
		{name: "a conjunction implies its own conjunct", premise: "state = 'completed' AND fee > 0", conclusion: "fee > 0", want: true},
		{name: "a nested conjunction is searched through", premise: "owner = subject AND (state = 'completed' AND fee > 0)", conclusion: "state IN ('completed', 'failed')", want: true},
		{name: "a conjunction implies nothing its conjuncts do not", premise: "state = 'open' AND fee > 0", conclusion: "state IN ('completed', 'failed')", want: false},
		{name: "an OR inside an AND is one conjunct tested by identity", premise: "fee > 0 AND (state = 'completed' OR state = 'failed')", conclusion: "state = 'completed' OR state = 'failed'", want: true},
		{name: "an OR inside an AND is not read as a literal set", premise: "fee > 0 AND (state = 'completed' OR state = 'failed')", conclusion: "state IN ('completed', 'failed')", want: false},
		{name: "a disjunction does not imply one of its disjuncts", premise: "state = 'completed' OR fee > 0", conclusion: "state IN ('completed', 'failed')", want: false},
		{name: "the conclusion being a conjunction is identity only", premise: "state = 'completed' AND fee > 0 AND hazard < 3", conclusion: "state = 'completed' AND fee > 0", want: false},
		// No guessing.
		{name: "an integer and a decimal are different literals", premise: "hazard = 1", conclusion: "hazard IN (1.0, 2)", want: false},
		{name: "string case is significant", premise: "state = 'A'", conclusion: "state IN ('a', 'b')", want: false},
		{name: "a string and a number are different literals", premise: "hazard = '1'", conclusion: "hazard IN (1, 2)", want: false},
		{name: "the post-image is another reference", premise: "new.state = 'completed'", conclusion: "state IN ('completed', 'failed')", want: false},
		{name: "the pre-image is another reference", premise: "state = 'completed'", conclusion: "new.state IN ('completed', 'failed')", want: false},
		{name: "another attribute never follows", premise: "state = 'completed'", conclusion: "kind IN ('completed', 'failed')", want: false},
		{name: "a subject set is identity only", premise: "owner IN subject.crews", conclusion: "owner IN subject.crews", want: true},
		{name: "a subject set is not a literal set", premise: "owner = 'u1'", conclusion: "owner IN subject.crews", want: false},
		{name: "a subject value is identity only", premise: "owner = subject.homeSector", conclusion: "owner IN ('a', 'b')", want: false},
		{name: "a subject comparison is identity only", premise: "owner = subject", conclusion: "owner IN ('u1')", want: false},
		{name: "a null test is identity only", premise: "state IS NULL", conclusion: "state IS NULL", want: true},
		{name: "a null test does not follow from a literal set", premise: "state = 'open'", conclusion: "state IS NOT NULL", want: false},
		{name: "NOT of an IN is not read as NOT IN", premise: "NOT state IN ('completed')", conclusion: "state NOT IN ('completed', 'failed')", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := Implies(mustParse(t, tt.premise), mustParse(t, tt.conclusion)); got != tt.want {
				t.Errorf("Implies(%q, %q) = %t, want %t", tt.premise, tt.conclusion, got, tt.want)
			}
		})
	}
}

// The archivist's Missions grants (Lodestar): the row fields on every closed
// state, the money fields on completed only.
const (
	closedStates = "state IN ('completed', 'failed', 'stood_down')"
	completed    = "state = 'completed'"
)

func TestUncovered(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		field         string
		row           []string
		wantUncovered []string
	}{
		{name: "the same condition covers itself", field: "state = 'open'", row: []string{"state = 'open'"}},
		{name: "spelling differences are canonical", field: "state='open'", row: []string{"state = 'open'"}},
		{name: "a wider field set covers a narrower predicate", field: "state = 'open' OR hazard < 3", row: []string{"hazard < 3"}},
		{name: "a predicate disjunct the field lacks is uncovered", field: "state = 'open'", row: []string{"state = 'open'", "hazard < 3"}, wantUncovered: []string{"hazard < 3"}},
		{name: "an empty predicate is covered by anything", field: "state = 'open'", row: nil},
		{name: "the archivist's deadline: the completed grant implies the closed states", field: closedStates, row: []string{closedStates, completed}},
		{name: "the archivist's fee: the closed states do not imply completed", field: completed, row: []string{closedStates, completed}, wantUncovered: []string{closedStates}},
		{name: "uncovered disjuncts come back in row order", field: "state = 'open'", row: []string{"hazard < 3", "state = 'open'", "owner = subject"}, wantUncovered: []string{"hazard < 3", "owner = subject"}},
		{name: "the field's equalities merge into one set", field: "state = 'failed' OR state = 'stood_down'", row: []string{"state IN ('failed', 'stood_down')"}},
		{name: "the field's equality and IN list merge", field: "state = 'failed' OR state IN ('stood_down', 'completed')", row: []string{"state IN ('failed', 'completed')"}},
		{name: "the merged set is still a set: a value outside it is uncovered", field: "state = 'failed' OR state = 'stood_down'", row: []string{"state IN ('failed', 'open')"}, wantUncovered: []string{"state IN ('failed', 'open')"}},
		{name: "sets on different references do not merge", field: "state = 'failed' OR kind = 'stood_down'", row: []string{"state IN ('failed', 'stood_down')"}, wantUncovered: []string{"state IN ('failed', 'stood_down')"}},
		{name: "negated field disjuncts are tested alone, not intersected", field: "state NOT IN ('a', 'b') OR state NOT IN ('b', 'c')", row: []string{"state NOT IN ('b')"}, wantUncovered: []string{"state NOT IN ('b')"}},
		{name: "a negated field disjunct covers the wider NOT IN", field: "state NOT IN ('a', 'b') OR hazard < 3", row: []string{"state NOT IN ('a', 'b', 'c')"}},
		{name: "a conjunction in the predicate is covered through one conjunct", field: closedStates, row: []string{closedStates, "state = 'completed' AND fee > 0"}},
		{name: "a conjunction in the predicate is covered through the merged set", field: "state = 'failed' OR state = 'stood_down'", row: []string{"fee > 0 AND state IN ('failed', 'stood_down')"}},
		{name: "a conjunction in the field covers only itself", field: "state = 'completed' AND fee > 0", row: []string{closedStates}, wantUncovered: []string{closedStates}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			row := make([]Expr, 0, len(tt.row))
			for _, source := range tt.row {
				row = append(row, mustParse(t, source))
			}
			field := Disjuncts(mustParse(t, tt.field))

			var got []string
			for _, d := range Uncovered(field, row) {
				got = append(got, d.String())
			}
			if !slices.Equal(got, tt.wantUncovered) {
				t.Errorf("Uncovered() = %v, want %v", got, tt.wantUncovered)
			}
			if gotCovers, want := Covers(field, row), len(tt.wantUncovered) == 0; gotCovers != want {
				t.Errorf("Covers() = %t, want %t", gotCovers, want)
			}
		})
	}
}
