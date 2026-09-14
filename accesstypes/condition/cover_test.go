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

func TestCovers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		field string
		row   []string
		want  bool
	}{
		{name: "the same condition covers itself", field: "state = 'open'", row: []string{"state = 'open'"}, want: true},
		{name: "spelling differences are canonical", field: "state='open'", row: []string{"state = 'open'"}, want: true},
		{name: "a wider field set covers a narrower predicate", field: "state = 'open' OR hazard < 3", row: []string{"hazard < 3"}, want: true},
		{name: "a predicate disjunct the field lacks is uncovered", field: "state = 'open'", row: []string{"state = 'open'", "hazard < 3"}, want: false},
		{name: "implication is not spelled and does not cover", field: "state = 'completed'", row: []string{"state IN ('completed', 'failed')"}, want: false},
		{name: "an empty predicate is covered by anything", field: "state = 'open'", row: nil, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			row := make([]Expr, 0, len(tt.row))
			for _, source := range tt.row {
				row = append(row, mustParse(t, source))
			}
			if got := Covers(Disjuncts(mustParse(t, tt.field)), row); got != tt.want {
				t.Errorf("Covers() = %t, want %t", got, tt.want)
			}
		})
	}
}
