package condition

import (
	"strings"
	"testing"
)

// TestParse_canonical pins the grammar through the canonical rendering:
// parsing the source and rendering the tree yields the canonical spelling,
// and re-parsing a rendering yields the same rendering (round-trip).
func TestParse_canonical(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
		want   string
	}{
		{
			name:   "subject-set membership",
			source: "crew IN subject.crews",
			want:   "crew IN subject.crews",
		},
		{
			name:   "post-image threshold against a subject value",
			source: "new.estimatedCost <= subject.approvalLimit",
			want:   "new.estimatedCost <= subject.approvalLimit",
		},
		{
			name:   "conjunction with an environment window",
			source: "crew IN subject.crews AND now < '2027-03-01T00:00:00Z'",
			want:   "crew IN subject.crews AND now < '2027-03-01T00:00:00Z'",
		},
		{
			name:   "capture guard",
			source: "assignee IS NULL AND new.assignee = subject",
			want:   "assignee IS NULL AND new.assignee = subject",
		},
		{
			name:   "old-vs-new comparison: may lower, never raise",
			source: "new.priority <= priority",
			want:   "new.priority <= priority",
		},
		{
			name:   "old-vs-new alongside a state guard",
			source: "state IN ('draft', 'scheduled') AND new.priority <= priority",
			want:   "state IN ('draft', 'scheduled') AND new.priority <= priority",
		},
		{
			name:   "time-of-day window in a named zone",
			source: "timeOfDay(now, 'America/Denver') >= '06:00' AND timeOfDay(now, 'America/Denver') < '13:30'",
			want:   "timeOfDay(now, 'America/Denver') >= '06:00' AND timeOfDay(now, 'America/Denver') < '13:30'",
		},
		{
			name:   "day-of-week membership in the local zone",
			source: "dayOfWeek(now, local) IN ('mon', 'tue', 'wed', 'thu', 'fri')",
			want:   "dayOfWeek(now, local) IN ('mon', 'tue', 'wed', 'thu', 'fri')",
		},
		{
			name:   "wrap-around night window is ordinary OR",
			source: "timeOfDay(now, local) >= '22:00' OR timeOfDay(now, local) < '06:00'",
			want:   "timeOfDay(now, local) >= '22:00' OR timeOfDay(now, local) < '06:00'",
		},
		{
			name:   "day equality",
			source: "dayOfWeek(now, 'UTC') = 'sat'",
			want:   "dayOfWeek(now, 'UTC') = 'sat'",
		},
		{
			// local is reserved only inside the zone argument: elsewhere it is
			// an ordinary binding name.
			name:   "local is an ordinary attribute outside a zone argument",
			source: "local = true",
			want:   "local = true",
		},
		{
			name:   "AND binds tighter than OR",
			source: "a = 1 OR b = 2 AND c = 3",
			want:   "a = 1 OR b = 2 AND c = 3",
		},
		{
			name:   "parentheses hoist OR under AND",
			source: "(a = 1 OR b = 2) AND c = 3",
			want:   "(a = 1 OR b = 2) AND c = 3",
		},
		{
			name:   "NOT on a comparison needs no parentheses",
			source: "NOT crew IN subject.crews",
			want:   "NOT crew IN subject.crews",
		},
		{
			name:   "NOT on a group keeps parentheses",
			source: "NOT (a = 1 AND b = 2)",
			want:   "NOT (a = 1 AND b = 2)",
		},
		{
			name:   "literal list membership",
			source: "state IN ('open', 'approved')",
			want:   "state IN ('open', 'approved')",
		},
		{
			name:   "negated literal list membership",
			source: "state NOT IN ('closed')",
			want:   "state NOT IN ('closed')",
		},
		{
			name:   "negated subject-set membership",
			source: "crew NOT IN subject.crews",
			want:   "crew NOT IN subject.crews",
		},
		{
			name:   "IS NOT NULL",
			source: "assignee IS NOT NULL",
			want:   "assignee IS NOT NULL",
		},
		{
			name:   "keywords are case-insensitive",
			source: "a = 1 and b = 2 or not c = 3",
			want:   "a = 1 AND b = 2 OR NOT c = 3",
		},
		{
			name:   "quote doubling escapes",
			source: "name = 'O''Malley'",
			want:   "name = 'O''Malley'",
		},
		{
			name:   "signed numbers verbatim",
			source: "amount <= -3.50 AND n > +2",
			want:   "amount <= -3.50 AND n > +2",
		},
		{
			name:   "boolean literals canonicalize lower",
			source: "active = TRUE AND flag != FALSE",
			want:   "active = true AND flag != false",
		},
		{
			name:   "boolean literal in a list",
			source: "flag IN (true, false)",
			want:   "flag IN (true, false)",
		},
		{
			name:   "now as an operand",
			source: "contractEnd > now",
			want:   "contractEnd > now",
		},
		{
			name:   "whitespace is free",
			source: "  a\t=\n1  ",
			want:   "a = 1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			expr, err := Parse(tt.source)
			if err != nil {
				t.Fatalf("Parse(%q) error = %v", tt.source, err)
			}
			if got := expr.String(); got != tt.want {
				t.Errorf("Parse(%q).String() = %q, want %q", tt.source, got, tt.want)
			}

			again, err := Parse(expr.String())
			if err != nil {
				t.Fatalf("Parse(rendering %q) error = %v", expr.String(), err)
			}
			if got := again.String(); got != tt.want {
				t.Errorf("round-trip rendering = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestParse_errors pins the rejections: malformed syntax, the forms the
// language deliberately excludes, and the fixed limits.
func TestParse_errors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		source      string
		wantContain string
	}{
		{
			name:        "empty condition",
			source:      "   ",
			wantContain: "empty condition",
		},
		{
			name:        "the not-equal spelling is !=",
			source:      "a <> 1",
			wantContain: "!=",
		},
		{
			name:        "subject cannot stand on the left",
			source:      "subject = 'alice'",
			wantContain: "subject cannot stand on the left",
		},
		{
			name:        "new does not apply to reserved words",
			source:      "new.now = 1",
			wantContain: "cannot be used",
		},
		{
			name:        "now has no set membership",
			source:      "now IN ('a')",
			wantContain: "only relational comparison",
		},
		{
			name:        "now has no null test",
			source:      "now IS NULL",
			wantContain: "only relational comparison",
		},
		{
			name:        "attribute-to-attribute needs a new.-qualified left side",
			source:      "a = b",
			wantContain: "only against a new.-qualified left side (old-vs-new)",
		},
		{
			name:        "new. never qualifies the right side",
			source:      "new.a = new.b",
			wantContain: "may only qualify a comparison's left side",
		},
		{
			name:        "a right-side attribute takes no dotted path",
			source:      "new.a = b.c",
			wantContain: "unexpected \".\"",
		},
		{
			name:        "keyword on the right is not an attribute",
			source:      "new.a = AND",
			wantContain: "keyword",
		},
		{
			name:        "unterminated string",
			source:      "name = 'open",
			wantContain: "unterminated string",
		},
		{
			name:        "empty IN list",
			source:      "state IN ()",
			wantContain: "expected a literal",
		},
		{
			name:        "trailing tokens",
			source:      "a = 1 b = 2",
			wantContain: "after the end of the condition",
		},
		{
			name:        "keyword where an attribute is expected",
			source:      "null = 1",
			wantContain: "keyword",
		},
		{
			name:        "reserved word as a subject-set name",
			source:      "a IN subject.now",
			wantContain: "cannot be used",
		},
		{
			name:        "bare subject after IN",
			source:      "a IN subject",
			wantContain: "requires a set name",
		},
		{
			name:        "NOT without IN after an attribute",
			source:      "a NOT NULL",
			wantContain: "expected IN after NOT",
		},
		{
			name:        "decimal needs digits after the point",
			source:      "a = 1.",
			wantContain: "digits after the point",
		},
		{
			name:        "unknown function",
			source:      "localHour(now, 'UTC') = '06:00'",
			wantContain: "unknown function \"localHour\"",
		},
		{
			name:        "a temporal function anchors on now",
			source:      "timeOfDay(created, 'UTC') = '06:00'",
			wantContain: "reads the environment fact now",
		},
		{
			name:        "a temporal zone is quoted or local",
			source:      "timeOfDay(now, zone) = '06:00'",
			wantContain: "expected a quoted zone name or local",
		},
		{
			name:        "a temporal zone cannot be empty",
			source:      "timeOfDay(now, '') = '06:00'",
			wantContain: "zone cannot be empty",
		},
		{
			name:        "timeOfDay has no set membership",
			source:      "timeOfDay(now, local) IN ('06:00')",
			wantContain: "timeOfDay supports only relational comparison",
		},
		{
			name:        "temporal functions are never NULL",
			source:      "dayOfWeek(now, local) IS NULL",
			wantContain: "never NULL",
		},
		{
			name:        "dayOfWeek takes a literal day list, not a subject set",
			source:      "dayOfWeek(now, local) IN subject.workdays",
			wantContain: "literal day list",
		},
		{
			name:        "function names match case-sensitively",
			source:      "TIMEOFDAY(now, 'UTC') = '06:00'",
			wantContain: "unknown function",
		},
		{
			name:        "source over the 4 KB limit",
			source:      "a = '" + strings.Repeat("x", maxSourceBytes) + "'",
			wantContain: "over the 4096-byte limit",
		},
		{
			name:        "nesting over the depth limit",
			source:      strings.Repeat("(", 40) + "a = 1" + strings.Repeat(")", 40),
			wantContain: "nesting deeper",
		},
		{
			name:        "NOT chain over the depth limit",
			source:      strings.Repeat("NOT ", 40) + "a = 1",
			wantContain: "nesting deeper",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := Parse(tt.source)
			if err == nil {
				t.Fatalf("Parse(%q) expected an error containing %q, got nil", tt.source, tt.wantContain)
			}
			if !strings.Contains(err.Error(), tt.wantContain) {
				t.Errorf("Parse(%q) error = %q, want containing %q", tt.source, err, tt.wantContain)
			}
		})
	}
}
