package condition

import (
	"slices"
	"strings"
	"testing"
	"time"
)

// TestFold_temporal pins the temporal functions (§05, decided 2026-09-03):
// the decision instant renders into a zone's wall clock at fold time — the
// engine is the only evaluator, SQL never sees a timezone — and the usual
// TRUE/FALSE absorption composes the window with row-referencing residue.
func TestFold_temporal(t *testing.T) {
	t.Parallel()

	denver := func(t *testing.T) *time.Location {
		t.Helper()
		loc, err := time.LoadLocation("America/Denver")
		if err != nil {
			t.Fatalf("LoadLocation(America/Denver) error = %v", err)
		}

		return loc
	}

	// 2026-09-02 15:30 UTC is 09:30 Wednesday in Denver (MDT, UTC-6).
	wedMidMorning := time.Date(2026, 9, 2, 15, 30, 0, 0, time.UTC)
	// 2026-09-06 01:00 UTC is 19:00 Saturday in Denver (still the 5th there).
	satEvening := time.Date(2026, 9, 6, 1, 0, 0, 0, time.UTC)

	tests := []struct {
		name        string
		source      string
		facts       Facts
		want        string
		wantErrPart string
	}{
		{
			name:   "inside the named-zone window",
			source: "timeOfDay(now, 'America/Denver') >= '06:00' AND timeOfDay(now, 'America/Denver') < '13:30'",
			facts:  NewFacts().WithNow(wedMidMorning),
			want:   "TRUE",
		},
		{
			// The same instant read in UTC is 15:30 — outside the window. The
			// zone is load-bearing, not decorative.
			name:   "the same instant misses the window in UTC",
			source: "timeOfDay(now, 'UTC') >= '06:00' AND timeOfDay(now, 'UTC') < '13:30'",
			facts:  NewFacts().WithNow(wedMidMorning),
			want:   "FALSE",
		},
		{
			name:   "local resolves from the zone fact",
			source: "timeOfDay(now, local) < '13:30'",
			facts:  NewFacts().WithNow(wedMidMorning).WithZone(time.UTC),
			want:   "FALSE",
		},
		{
			name:   "day membership in the zone's calendar, not UTC's",
			source: "dayOfWeek(now, 'America/Denver') IN ('mon', 'tue', 'wed', 'thu', 'fri')",
			facts:  NewFacts().WithNow(satEvening),
			want:   "FALSE",
		},
		{
			// The same instant is already Sunday in UTC; Denver still reads
			// Saturday.
			name:   "day equality across the date line of the zone",
			source: "dayOfWeek(now, 'America/Denver') = 'sat'",
			facts:  NewFacts().WithNow(satEvening),
			want:   "TRUE",
		},
		{
			name:   "negated day membership",
			source: "dayOfWeek(now, 'UTC') NOT IN ('sat', 'sun')",
			facts:  NewFacts().WithNow(wedMidMorning),
			want:   "TRUE",
		},
		{
			name:   "wrap-around night window holds after dusk",
			source: "timeOfDay(now, 'America/Denver') >= '22:00' OR timeOfDay(now, 'America/Denver') < '06:00'",
			facts:  NewFacts().WithNow(time.Date(2026, 9, 3, 5, 0, 0, 0, time.UTC)), // 23:00 Denver
			want:   "TRUE",
		},
		{
			name:   "the window composes with row-referencing residue",
			source: "state = 'draft' AND timeOfDay(now, 'America/Denver') < '13:30'",
			facts:  NewFacts().WithNow(wedMidMorning),
			want:   "state = 'draft'",
		},
		{
			name:   "a closed window folds the whole conjunction closed",
			source: "state = 'draft' AND timeOfDay(now, 'America/Denver') >= '13:30'",
			facts:  NewFacts().WithNow(wedMidMorning),
			want:   "FALSE",
		},
		{
			name:        "local without a zone fact fails loud",
			source:      "timeOfDay(now, local) < '13:30'",
			facts:       NewFacts().WithNow(wedMidMorning),
			wantErrPart: "references local, which the environment does not carry",
		},
		{
			name:        "a temporal term without the instant fails loud",
			source:      "dayOfWeek(now, 'UTC') = 'sat'",
			facts:       NewFacts(),
			wantErrPart: "references now, which the environment does not carry",
		},
		{
			name:        "an unknown zone fails loud",
			source:      "timeOfDay(now, 'Mars/Olympus_Mons') < '13:30'",
			facts:       NewFacts().WithNow(wedMidMorning),
			wantErrPart: "not a timezone name",
		},
		{
			name:        "a malformed time literal fails loud",
			source:      "timeOfDay(now, 'UTC') < '25:99'",
			facts:       NewFacts().WithNow(wedMidMorning),
			wantErrPart: "not a 24-hour HH:MM time of day",
		},
		{
			name:        "a non-day literal fails loud",
			source:      "dayOfWeek(now, 'UTC') = 'saturday'",
			facts:       NewFacts().WithNow(wedMidMorning),
			wantErrPart: "not a day name",
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
			if tt.wantErrPart != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErrPart) {
					t.Fatalf("Fold(%q) error = %v, want containing %q", tt.source, err, tt.wantErrPart)
				}

				return
			}
			if err != nil {
				t.Fatalf("Fold(%q) error = %v", tt.source, err)
			}
			if got := folded.String(); got != tt.want {
				t.Errorf("Fold(%q) = %q, want %q", tt.source, got, tt.want)
			}
		})
	}

	// DST is unambiguous by construction: the conversion goes instant → wall
	// clock, so the fall-back window simply holds on both passes of the
	// repeated hour. 2026-11-01 07:30 UTC and 08:30 UTC both read 01:30 in
	// Denver — MDT before the clocks fall back, MST after.
	t.Run("both passes of the fall-back hour read the same wall clock", func(t *testing.T) {
		t.Parallel()

		expr, err := Parse("timeOfDay(now, 'America/Denver') >= '01:00' AND timeOfDay(now, 'America/Denver') < '02:00'")
		if err != nil {
			t.Fatalf("Parse() error = %v", err)
		}
		for _, instant := range []time.Time{
			time.Date(2026, 11, 1, 7, 30, 0, 0, time.UTC),
			time.Date(2026, 11, 1, 8, 30, 0, 0, time.UTC),
		} {
			if local := instant.In(denver(t)); local.Hour() != 1 || local.Minute() != 30 {
				t.Fatalf("test premise: %v reads %02d:%02d in Denver, want 01:30", instant, local.Hour(), local.Minute())
			}
			folded, err := Fold(expr, NewFacts().WithNow(instant))
			if err != nil {
				t.Fatalf("Fold() error = %v", err)
			}
			if got := folded.String(); got != "TRUE" {
				t.Errorf("Fold() at %v = %q, want TRUE", instant, got)
			}
		}
	})
}

// TestClassify_temporal pins the classification a temporal term carries: an
// environment fact — row-free, binding nothing, needing the instant.
func TestClassify_temporal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		source       string
		wantRowFree  bool
		wantBindings []string
		wantUsesNow  bool
	}{
		{
			name:        "temporal terms are environment facts",
			source:      "dayOfWeek(now, local) IN ('mon', 'tue') AND timeOfDay(now, 'UTC') < '18:00'",
			wantRowFree: true,
			wantUsesNow: true,
		},
		{
			name:         "a row term beside a temporal term keeps the row classification",
			source:       "state = 'draft' AND timeOfDay(now, local) < '18:00'",
			wantRowFree:  false,
			wantBindings: []string{"state"},
			wantUsesNow:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			expr, err := Parse(tt.source)
			if err != nil {
				t.Fatalf("Parse(%q) error = %v", tt.source, err)
			}

			if got := RowFree(expr); got != tt.wantRowFree {
				t.Errorf("RowFree() = %v, want %v", got, tt.wantRowFree)
			}
			if got := Bindings(expr); !slices.Equal(got, tt.wantBindings) {
				t.Errorf("Bindings() = %v, want %v", got, tt.wantBindings)
			}
			if got := UsesNow(expr); got != tt.wantUsesNow {
				t.Errorf("UsesNow() = %v, want %v", got, tt.wantUsesNow)
			}
		})
	}
}
