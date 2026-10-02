package live

import (
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// TestFake_signals pins the fake's signals: a signal runs every subscription to its
// kind synchronously and is remembered, a subscription to another kind hears nothing,
// a subscription made after earlier signals hears the later ones, and a stopped
// subscription hears nothing more while the others go on.
func TestFake_signals(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// run drives the fake and returns the signals each subscription counted.
		run             func(t *testing.T, f *Fake) []int
		wantCounts      []int
		wantSignals     []Kind
		wantSubscribers map[Kind]int
	}{
		{
			name: "a signal with no subscription is remembered",
			run: func(t *testing.T, f *Fake) []int {
				t.Helper()
				if err := f.Signal(t.Context(), KindFeatures); err != nil {
					t.Fatalf("Signal() error = %v", err)
				}

				return nil
			},
			wantSignals:     []Kind{KindFeatures},
			wantSubscribers: map[Kind]int{KindFeatures: 0},
		},
		{
			name: "two subscriptions to one kind each hear every signal of it; a subscription to another kind hears only its own",
			run: func(t *testing.T, f *Fake) []int {
				t.Helper()
				counts := make([]int, 3)
				for i, kind := range []Kind{KindFeatures, KindFeatures, KindTenants} {
					if _, err := f.Subscribe(kind, func() { counts[i]++ }); err != nil {
						t.Fatalf("Subscribe() error = %v", err)
					}
				}
				for _, kind := range []Kind{KindFeatures, KindFeatures, KindTenants} {
					if err := f.Signal(t.Context(), kind); err != nil {
						t.Fatalf("Signal() error = %v", err)
					}
				}

				return counts
			},
			wantCounts:      []int{2, 2, 1},
			wantSignals:     []Kind{KindFeatures, KindFeatures, KindTenants},
			wantSubscribers: map[Kind]int{KindFeatures: 2, KindTenants: 1, KindPolicy: 0},
		},
		{
			name: "a subscription made after a signal hears the signals after it",
			run: func(t *testing.T, f *Fake) []int {
				t.Helper()
				counts := make([]int, 2)
				if _, err := f.Subscribe(KindPolicy, func() { counts[0]++ }); err != nil {
					t.Fatalf("Subscribe() error = %v", err)
				}
				if err := f.Signal(t.Context(), KindPolicy); err != nil {
					t.Fatalf("Signal() error = %v", err)
				}
				if _, err := f.Subscribe(KindPolicy, func() { counts[1]++ }); err != nil {
					t.Fatalf("Subscribe() error = %v", err)
				}
				if err := f.Signal(t.Context(), KindPolicy); err != nil {
					t.Fatalf("Signal() error = %v", err)
				}

				return counts
			},
			wantCounts:      []int{2, 1},
			wantSignals:     []Kind{KindPolicy, KindPolicy},
			wantSubscribers: map[Kind]int{KindPolicy: 2},
		},
		{
			name: "a stopped subscription hears nothing more; the other goes on",
			run: func(t *testing.T, f *Fake) []int {
				t.Helper()
				counts := make([]int, 2)
				stop, err := f.Subscribe(KindFeatures, func() { counts[0]++ })
				if err != nil {
					t.Fatalf("Subscribe() error = %v", err)
				}
				if _, err := f.Subscribe(KindFeatures, func() { counts[1]++ }); err != nil {
					t.Fatalf("Subscribe() error = %v", err)
				}
				if err := f.Signal(t.Context(), KindFeatures); err != nil {
					t.Fatalf("Signal() error = %v", err)
				}
				stop()
				if err := f.Signal(t.Context(), KindFeatures); err != nil {
					t.Fatalf("Signal() error = %v", err)
				}

				return counts
			},
			wantCounts:      []int{1, 2},
			wantSignals:     []Kind{KindFeatures, KindFeatures},
			wantSubscribers: map[Kind]int{KindFeatures: 1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := NewFake()
			got := tt.run(t, f)
			if diff := cmp.Diff(tt.wantCounts, got); diff != "" {
				t.Errorf("signal counts mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.wantSignals, f.Signals()); diff != "" {
				t.Errorf("Signals() mismatch (-want +got):\n%s", diff)
			}
			for kind, want := range tt.wantSubscribers {
				if got := f.Subscribers(kind); got != want {
					t.Errorf("Subscribers(%s) = %d, want %d", kind, got, want)
				}
			}
		})
	}
}

func TestFake_signals_Err(t *testing.T) {
	t.Parallel()

	f := NewFake()
	f.Err = errors.New("down")
	if err := f.Signal(t.Context(), KindFeatures); err == nil {
		t.Error("Signal() error = nil, want Err")
	}
	if _, err := f.Subscribe(KindFeatures, func() {}); err == nil {
		t.Error("Subscribe() error = nil, want Err")
	}
}
