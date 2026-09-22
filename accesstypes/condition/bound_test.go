package condition

import (
	"testing"
)

// TestWithoutPostImage pins the fail-open upper bound the capability envelope
// renders: post-image atoms assume TRUE (FALSE under negation), the evaluable
// residue survives, and the logic simplifies.
func TestWithoutPostImage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
		want   string
	}{
		{
			name:   "a pure post-image condition is potentially-true",
			source: "new.priority <= 3",
			want:   "TRUE",
		},
		{
			name:   "an AND keeps its evaluable conjuncts",
			source: "state IN ('draft', 'scheduled') AND new.priority <= priority",
			want:   "state IN ('draft', 'scheduled')",
		},
		{
			name:   "an OR with a post-image branch widens to TRUE",
			source: "state = 'draft' OR new.priority <= priority",
			want:   "TRUE",
		},
		{
			// An unknown atom is unknown in both polarities: its negation is
			// just as potentially-true as it is.
			name:   "a negated post-image atom is still potentially-true",
			source: "NOT new.priority <= priority",
			want:   "TRUE",
		},
		{
			name:   "negation over a group bounds inside-out",
			source: "NOT (state = 'draft' AND new.priority = 1)",
			want:   "TRUE",
		},
		{
			name:   "a condition without post-image terms is untouched",
			source: "state = 'draft' AND owner = subject",
			want:   "state = 'draft' AND owner = subject",
		},
		{
			name:   "surviving negation stays",
			source: "NOT state = 'closed' AND new.priority <= priority",
			want:   "NOT state = 'closed'",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			expr, err := Parse(tt.source)
			if err != nil {
				t.Fatalf("Parse(%q) error = %v", tt.source, err)
			}
			if got := WithoutPostImage(expr).String(); got != tt.want {
				t.Errorf("WithoutPostImage(%q) = %q, want %q", tt.source, got, tt.want)
			}
		})
	}
}

// TestFailOpen pins the generalized bound under a caller-supplied predicate —
// here "only atoms whose left side is the uniform state attribute are
// evaluable", the create-under-parent affordance's posture.
func TestFailOpen(t *testing.T) {
	t.Parallel()

	stateOnly := func(atom Expr) bool {
		switch n := atom.(type) {
		case Comparison:
			return !n.Left.PostImage && n.Left.Name == "state"
		case In:
			return !n.Left.PostImage && n.Left.Name == "state"
		case NullTest:
			return !n.Left.PostImage && n.Left.Name == "state"
		default:
			return true
		}
	}

	tests := []struct {
		name   string
		source string
		want   string
	}{
		{
			name:   "non-state conjuncts assume TRUE, the state residue survives",
			source: "state = 'draft' AND owner = subject",
			want:   "state = 'draft'",
		},
		{
			name:   "a pure non-state condition has no residue",
			source: "new.priority <= 3 AND owner = subject",
			want:   "TRUE",
		},
		{
			name:   "an OR with an unknown branch widens to TRUE",
			source: "state = 'draft' OR owner = subject",
			want:   "TRUE",
		},
		{
			name:   "a negated unknown atom is still potentially-true",
			source: "state = 'draft' AND NOT owner = subject",
			want:   "state = 'draft'",
		},
		{
			name:   "a state-only condition is untouched",
			source: "state IN ('draft', 'scheduled')",
			want:   "state IN ('draft', 'scheduled')",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			expr, err := Parse(tt.source)
			if err != nil {
				t.Fatalf("Parse(%q) error = %v", tt.source, err)
			}
			if got := FailOpen(expr, stateOnly).String(); got != tt.want {
				t.Errorf("FailOpen(%q) = %q, want %q", tt.source, got, tt.want)
			}
		})
	}
}
