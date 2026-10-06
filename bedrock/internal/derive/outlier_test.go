package derive

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// intPtr is a pointer to the number, for writing a threshold.
func intPtr(n int) *int {
	return &n
}

// TestOutlierDetection holds the thresholds a placement may write: each field left out,
// or the whole block, takes its default; a field written replaces its default alone; a
// value that would turn the ejection off, or is not a percentage where one is asked for,
// is refused, naming the field.
func TestOutlierDetection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		set         *OutlierDetection
		want        Outlier
		wantDefault bool
		wantErr     string
	}{
		{name: "no block: today's thresholds", set: nil, want: Outlier{ConsecutiveErrors: 5, EnforcingConsecutiveErrors: 100, MaxEjectionPercent: 50, IntervalSeconds: 1, BaseEjectionSeconds: 30}, wantDefault: true},
		{name: "an empty block: today's thresholds", set: &OutlierDetection{}, want: DefaultOutlier(), wantDefault: true},
		{name: "the defaults written out are the defaults", set: &OutlierDetection{ConsecutiveErrors: intPtr(5), BaseEjectionSeconds: intPtr(30)}, want: DefaultOutlier(), wantDefault: true},
		{
			name: "one field written replaces its default alone",
			set:  &OutlierDetection{ConsecutiveErrors: intPtr(3)},
			want: Outlier{ConsecutiveErrors: 3, EnforcingConsecutiveErrors: 100, MaxEjectionPercent: 50, IntervalSeconds: 1, BaseEjectionSeconds: 30},
		},
		{
			name: "every field written",
			set:  &OutlierDetection{ConsecutiveErrors: intPtr(3), EnforcingConsecutiveErrors: intPtr(90), MaxEjectionPercent: intPtr(100), IntervalSeconds: intPtr(2), BaseEjectionSeconds: intPtr(60)},
			want: Outlier{ConsecutiveErrors: 3, EnforcingConsecutiveErrors: 90, MaxEjectionPercent: 100, IntervalSeconds: 2, BaseEjectionSeconds: 60},
		},
		{name: "no errors in a row is refused", set: &OutlierDetection{ConsecutiveErrors: intPtr(0)}, wantErr: "outlierDetection.consecutiveErrors 0 is outside what keeps outlier detection on (errors in a row, at least 1); leave it out for its default"},
		{name: "enforcing none is refused", set: &OutlierDetection{EnforcingConsecutiveErrors: intPtr(0)}, wantErr: "outlierDetection.enforcingConsecutiveErrors 0 is outside what keeps outlier detection on (a percentage from 1 to 100; 0 would eject no region)"},
		{name: "enforcing more than all is refused", set: &OutlierDetection{EnforcingConsecutiveErrors: intPtr(101)}, wantErr: "outlierDetection.enforcingConsecutiveErrors 101 is outside"},
		{name: "ejecting none is refused", set: &OutlierDetection{MaxEjectionPercent: intPtr(0)}, wantErr: "outlierDetection.maxEjectionPercent 0 is outside"},
		{name: "ejecting more than all is refused", set: &OutlierDetection{MaxEjectionPercent: intPtr(150)}, wantErr: "outlierDetection.maxEjectionPercent 150 is outside"},
		{name: "a zero interval is refused", set: &OutlierDetection{IntervalSeconds: intPtr(0)}, wantErr: "outlierDetection.intervalSeconds 0 is outside what keeps outlier detection on (seconds, at least 1)"},
		{name: "a negative ejection time is refused", set: &OutlierDetection{BaseEjectionSeconds: intPtr(-30)}, wantErr: "outlierDetection.baseEjectionSeconds -30 is outside"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.set.Validate()
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Validate() error = %v, wantErr %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
			got := tt.set.Resolved()
			if got != tt.want {
				t.Errorf("Resolved() = %+v, want %+v", got, tt.want)
			}
			if got.IsDefault() != tt.wantDefault {
				t.Errorf("IsDefault() = %v, want %v", got.IsDefault(), tt.wantDefault)
			}
		})
	}
}

// TestOutlierDetectionPlacement reads the block from a placement file, where a field the
// block does not know is refused like any other unknown field, and a threshold out of
// range refuses the placement.
func TestOutlierDetectionPlacement(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		block   string
		want    Outlier
		wantErr string
	}{
		{name: "the block's fields by name", block: `{"consecutiveErrors": 3, "baseEjectionSeconds": 60}`, want: Outlier{ConsecutiveErrors: 3, EnforcingConsecutiveErrors: 100, MaxEjectionPercent: 50, IntervalSeconds: 1, BaseEjectionSeconds: 60}},
		{name: "a field the block does not know", block: `{"consecutiveFailures": 3}`, wantErr: `unknown field "consecutiveFailures"`},
		{name: "a threshold out of range", block: `{"maxEjectionPercent": 0}`, wantErr: "outlierDetection.maxEjectionPercent 0 is outside"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := testPlacement(t)
			data, err := json.Marshal(p)
			if err != nil {
				t.Fatal(err)
			}
			data = []byte(strings.Replace(string(data), "{", `{"outlierDetection": `+tt.block+`, `, 1))
			file := filepath.Join(t.TempDir(), "placement.json")
			if err := os.WriteFile(file, data, 0o600); err != nil {
				t.Fatal(err)
			}
			back, err := ReadPlacement(file)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("ReadPlacement() error = %v, wantErr %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("ReadPlacement() error = %v", err)
			}
			if got := back.Outlier(); got != tt.want {
				t.Errorf("Outlier() = %+v, want %+v", got, tt.want)
			}
		})
	}
}
