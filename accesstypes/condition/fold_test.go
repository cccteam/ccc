package condition

import (
	"strings"
	"testing"
	"time"
)

// TestFold pins the fact-folding semantics: fact-only terms evaluate (UTC
// instant comparison), TRUE and FALSE absorb through the logic, and anything
// touching row data or a subject attribute passes through untouched for the
// database.
func TestFold(t *testing.T) {
	t.Parallel()

	instant := time.Date(2027, 3, 1, 0, 0, 0, 0, time.UTC)
	before := instant.Add(-time.Hour)
	after := instant.Add(time.Hour)

	tests := []struct {
		name   string
		source string
		facts  Facts
		want   string
	}{
		{
			name:   "window still open folds true",
			source: "now < '2027-03-01T00:00:00Z'",
			facts:  NewFacts().WithNow(before),
			want:   "TRUE",
		},
		{
			name:   "window passed folds false",
			source: "now < '2027-03-01T00:00:00Z'",
			facts:  NewFacts().WithNow(after),
			want:   "FALSE",
		},
		{
			name:   "boundary instant is exclusive for less-than",
			source: "now < '2027-03-01T00:00:00Z'",
			facts:  NewFacts().WithNow(instant),
			want:   "FALSE",
		},
		{
			name:   "boundary instant is inclusive for less-or-equal",
			source: "now <= '2027-03-01T00:00:00Z'",
			facts:  NewFacts().WithNow(instant),
			want:   "TRUE",
		},
		{
			name:   "offset timestamps compare as instants",
			source: "now = '2027-03-01T02:00:00+02:00'",
			facts:  NewFacts().WithNow(instant),
			want:   "TRUE",
		},
		{
			name:   "true conjunct drops out",
			source: "crew IN subject.crews AND now < '2027-03-01T00:00:00Z'",
			facts:  NewFacts().WithNow(before),
			want:   "crew IN subject.crews",
		},
		{
			name:   "false conjunct absorbs the conjunction",
			source: "crew IN subject.crews AND now < '2027-03-01T00:00:00Z'",
			facts:  NewFacts().WithNow(after),
			want:   "FALSE",
		},
		{
			name:   "true disjunct absorbs the disjunction",
			source: "crew IN subject.crews OR now < '2027-03-01T00:00:00Z'",
			facts:  NewFacts().WithNow(before),
			want:   "TRUE",
		},
		{
			name:   "false disjunct drops out",
			source: "crew IN subject.crews OR now < '2027-03-01T00:00:00Z'",
			facts:  NewFacts().WithNow(after),
			want:   "crew IN subject.crews",
		},
		{
			name:   "NOT inverts a folded term",
			source: "NOT (now < '2027-03-01T00:00:00Z')",
			facts:  NewFacts().WithNow(after),
			want:   "TRUE",
		},
		{
			name:   "now against now folds true",
			source: "now <= now",
			facts:  NewFacts().WithNow(instant),
			want:   "TRUE",
		},
		{
			name:   "data expression passes through untouched",
			source: "assignee = subject AND state IN ('open', 'approved')",
			facts:  NewFacts().WithNow(instant).WithSubject("dana"),
			want:   "assignee = subject AND state IN ('open', 'approved')",
		},
		{
			name:   "now against a subject value is data for the database",
			source: "now < subject.shiftEnd",
			facts:  NewFacts().WithNow(instant),
			want:   "now < subject.shiftEnd",
		},
		{
			name:   "nested groups simplify through the logic",
			source: "(now >= '2027-03-01T00:00:00Z' OR crew IN subject.crews) AND state = 'open'",
			facts:  NewFacts().WithNow(after),
			want:   "state = 'open'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			expr, err := Parse(tt.source)
			if err != nil {
				t.Fatalf("Parse(%q) error = %v", tt.source, err)
			}

			folded, err := Fold(expr, tt.facts)
			if err != nil {
				t.Fatalf("Fold(%q) error = %v", tt.source, err)
			}
			if got := folded.String(); got != tt.want {
				t.Errorf("Fold(%q) = %q, want %q", tt.source, got, tt.want)
			}
		})
	}
}

// TestFold_errors pins the fail-loud posture: a referenced fact the Facts do
// not carry, and a fact compared against the wrong shape, are errors — never
// a silent allow or deny.
func TestFold_errors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		source      string
		facts       Facts
		wantContain string
	}{
		{
			name:        "missing now fails loud",
			source:      "now < '2027-03-01T00:00:00Z'",
			facts:       NewFacts(),
			wantContain: "does not carry",
		},
		{
			name:        "malformed timestamp literal fails loud",
			source:      "now < '2027-03-99'",
			facts:       NewFacts().WithNow(time.Now()),
			wantContain: "RFC 3339",
		},
		{
			name:        "now against a number fails loud",
			source:      "now < 5",
			facts:       NewFacts().WithNow(time.Now()),
			wantContain: "cannot be compared",
		},
		{
			name:        "now against subject fails loud",
			source:      "now = subject",
			facts:       NewFacts().WithNow(time.Now()).WithSubject("dana"),
			wantContain: "cannot be compared",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			expr, err := Parse(tt.source)
			if err != nil {
				t.Fatalf("Parse(%q) error = %v", tt.source, err)
			}

			if _, err := Fold(expr, tt.facts); err == nil {
				t.Fatalf("Fold(%q) expected an error containing %q, got nil", tt.source, tt.wantContain)
			} else if !strings.Contains(err.Error(), tt.wantContain) {
				t.Errorf("Fold(%q) error = %q, want containing %q", tt.source, err, tt.wantContain)
			}
		})
	}
}

// TestFacts_accessors pins the presence tracking: a set zero value is
// distinguishable from a fact never supplied, and WithNow normalizes to UTC.
func TestFacts_accessors(t *testing.T) {
	t.Parallel()

	if _, ok := NewFacts().Subject(); ok {
		t.Error("NewFacts().Subject() ok = true, want false")
	}
	if _, ok := NewFacts().Now(); ok {
		t.Error("NewFacts().Now() ok = true, want false")
	}

	local := time.Date(2027, 3, 1, 2, 0, 0, 0, time.FixedZone("X", 2*3600))
	f := NewFacts().WithSubject("dana").WithNow(local)

	if subject, ok := f.Subject(); !ok || subject != "dana" {
		t.Errorf("Subject() = %q, %v; want %q, true", subject, ok, "dana")
	}
	now, ok := f.Now()
	if !ok {
		t.Fatal("Now() ok = false, want true")
	}
	if now.Location() != time.UTC {
		t.Errorf("Now() location = %v, want UTC", now.Location())
	}
	if want := time.Date(2027, 3, 1, 0, 0, 0, 0, time.UTC); !now.Equal(want) {
		t.Errorf("Now() = %v, want %v", now, want)
	}
}
