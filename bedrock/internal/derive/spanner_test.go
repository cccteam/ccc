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
