package derive

import "testing"

// TestSpannerDatabaseCapacity: Google's rule, ten databases for every hundred processing
// units below a node and a hundred from a node on.
func TestSpannerDatabaseCapacity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		units int
		want  int
	}{
		{name: "the smallest instance", units: 100, want: 10},
		{name: "two hundred", units: 200, want: 20},
		{name: "below a node", units: 900, want: 90},
		{name: "a node", units: 1000, want: 100},
		{name: "beyond a node", units: 3000, want: 100},
		{name: "nothing known", units: 0, want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := SpannerDatabaseCapacity(tt.units); got != tt.want {
				t.Errorf("SpannerDatabaseCapacity(%d) = %d, want %d", tt.units, got, tt.want)
			}
		})
	}
}

// TestSpannerSentences: the rule and the way out through the size change at a node, where
// no raise adds a database.
func TestSpannerSentences(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		units     int
		wantRule  string
		wantRaise string
	}{
		{name: "below a node", units: 300, wantRule: "ten for every hundred below a node", wantRaise: "raise processing_units in 2-spn/terraform.tfvars (every hundred units below a node hold ten more databases; a node, 1000 units, holds a hundred), or"},
		{name: "a node", units: 1000, wantRule: "a hundred from a node on", wantRaise: "no raise of processing_units in 2-spn/terraform.tfvars adds a database from a node on, so"},
		{name: "beyond a node", units: 2000, wantRule: "a hundred from a node on", wantRaise: "no raise of processing_units in 2-spn/terraform.tfvars adds a database from a node on, so"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := SpannerCapacityRule(tt.units); got != tt.wantRule {
				t.Errorf("SpannerCapacityRule(%d) = %q, want %q", tt.units, got, tt.wantRule)
			}
			if got := SpannerRaise("processing_units in 2-spn/terraform.tfvars", tt.units); got != tt.wantRaise {
				t.Errorf("SpannerRaise(%d) = %q, want %q", tt.units, got, tt.wantRaise)
			}
		})
	}
}
