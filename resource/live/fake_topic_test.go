package live

import (
	"context"
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// TestFake_topic pins the fake's application topic: a broadcast runs every watch on
// its topic synchronously and is remembered, a stopped or canceled watch hears
// nothing more, a watch on another topic hears nothing, and Err fails both calls.
func TestFake_topic(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// run drives the fake and returns the signals each watch counted.
		run            func(t *testing.T, f *Fake) []int
		wantSignals    []int
		wantBroadcasts []string
		wantWatching   int
	}{
		{
			name: "a broadcast with no watch is remembered",
			run: func(t *testing.T, f *Fake) []int {
				t.Helper()
				if err := f.Broadcast(t.Context(), "features"); err != nil {
					t.Fatalf("Broadcast() error = %v", err)
				}

				return nil
			},
			wantBroadcasts: []string{"features"},
		},
		{
			name: "two watches on the topic each hear every broadcast; a watch on another topic hears none",
			run: func(t *testing.T, f *Fake) []int {
				t.Helper()
				counts := make([]int, 3)
				for i, topic := range []string{"features", "features", "other"} {
					if _, err := f.Watch(t.Context(), topic, func() { counts[i]++ }); err != nil {
						t.Fatalf("Watch() error = %v", err)
					}
				}
				for range 2 {
					if err := f.Broadcast(t.Context(), "features"); err != nil {
						t.Fatalf("Broadcast() error = %v", err)
					}
				}

				return counts
			},
			wantSignals:    []int{2, 2, 0},
			wantBroadcasts: []string{"features", "features"},
			wantWatching:   2,
		},
		{
			name: "a stopped watch hears nothing more",
			run: func(t *testing.T, f *Fake) []int {
				t.Helper()
				counts := make([]int, 1)
				stop, err := f.Watch(t.Context(), "features", func() { counts[0]++ })
				if err != nil {
					t.Fatalf("Watch() error = %v", err)
				}
				if err := f.Broadcast(t.Context(), "features"); err != nil {
					t.Fatalf("Broadcast() error = %v", err)
				}
				stop()
				if err := f.Broadcast(t.Context(), "features"); err != nil {
					t.Fatalf("Broadcast() error = %v", err)
				}

				return counts
			},
			wantSignals:    []int{1},
			wantBroadcasts: []string{"features", "features"},
		},
		{
			name: "a watch whose context ended hears nothing more",
			run: func(t *testing.T, f *Fake) []int {
				t.Helper()
				counts := make([]int, 1)
				ctx, cancel := context.WithCancel(t.Context())
				if _, err := f.Watch(ctx, "features", func() { counts[0]++ }); err != nil {
					t.Fatalf("Watch() error = %v", err)
				}
				cancel()
				// AfterFunc runs the stop on its own goroutine; wait for the watch to go.
				for f.Watching("features") > 0 {
					continue
				}
				if err := f.Broadcast(t.Context(), "features"); err != nil {
					t.Fatalf("Broadcast() error = %v", err)
				}

				return counts
			},
			wantSignals:    []int{0},
			wantBroadcasts: []string{"features"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := NewFake()
			got := tt.run(t, f)
			if diff := cmp.Diff(tt.wantSignals, got); diff != "" {
				t.Errorf("signals mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.wantBroadcasts, f.Broadcasts()); diff != "" {
				t.Errorf("Broadcasts() mismatch (-want +got):\n%s", diff)
			}
			if got := f.Watching("features"); got != tt.wantWatching {
				t.Errorf("Watching(features) = %d, want %d", got, tt.wantWatching)
			}
		})
	}
}

func TestFake_topic_Err(t *testing.T) {
	t.Parallel()

	f := NewFake()
	f.Err = errors.New("down")
	if err := f.Broadcast(t.Context(), "features"); err == nil {
		t.Error("Broadcast() error = nil, want Err")
	}
	if _, err := f.Watch(t.Context(), "features", func() {}); err == nil {
		t.Error("Watch() error = nil, want Err")
	}
}
