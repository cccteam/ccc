package filestore

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"google.golang.org/api/googleapi"
)

// TestBucketCheck: the readiness check probes a refused permission again every few
// seconds for a bounded time, so a grant made moments before the process started takes
// effect without the store starting refused; still refused at the bound, the store starts
// in its refused state with no error; any other error, and a context ending during the
// wait, are errors.
func TestBucketCheck(t *testing.T) {
	t.Parallel()

	refused := &googleapi.Error{Code: http.StatusForbidden, Message: "the service account does not have storage.objects.list access"}
	tests := []struct {
		name string
		// answers are the probes' errors in order; the last one repeats.
		answers     []error
		cancelAfter int
		wantErr     string
		wantRefused bool
		wantSleeps  int
	}{
		{name: "granted at once: no wait", answers: []error{nil}},
		{name: "granted on the third probe: two waits, the store usable", answers: []error{refused, refused, nil}, wantSleeps: 2},
		{name: "refused past the bound: the refused state, no error", answers: []error{refused}, wantRefused: true, wantSleeps: int(startupWait / startupRetry)},
		{name: "a missing bucket is an error at once", answers: []error{&googleapi.Error{Code: http.StatusNotFound, Message: "bucket not found"}}, wantErr: "bucket not found"},
		{name: "the context ending during the wait is an error", answers: []error{refused}, cancelAfter: 1, wantErr: "context canceled", wantSleeps: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			clock := time.Date(2026, 10, 5, 6, 0, 0, 0, time.UTC)
			probes, sleeps := 0, 0
			b := &Bucket{name: "b"}
			b.probe = func(context.Context) error {
				i := min(probes, len(tt.answers)-1)
				probes++

				return tt.answers[i]
			}
			b.sleep = func(ctx context.Context, d time.Duration) error {
				sleeps++
				clock = clock.Add(d)
				if tt.cancelAfter > 0 && sleeps >= tt.cancelAfter {
					cancel()
				}
				if ctx.Err() != nil {
					return ctx.Err()
				}

				return nil
			}
			b.now = func() time.Time { return clock }

			err := b.Check(ctx)
			if tt.wantErr == "" && err != nil {
				t.Fatalf("Check() error = %v", err)
			}
			if tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("Check() error = %v, want one containing %q", err, tt.wantErr)
			}
			if got := b.refused.Load(); got != tt.wantRefused {
				t.Errorf("refused = %v, want %v", got, tt.wantRefused)
			}
			if sleeps != tt.wantSleeps {
				t.Errorf("waited %d times, want %d", sleeps, tt.wantSleeps)
			}
			if tt.wantErr == "" && !tt.wantRefused && !errors.Is(tt.answers[len(tt.answers)-1], nil) {
				t.Errorf("the last probe %v should have been nil", tt.answers[len(tt.answers)-1])
			}
		})
	}
}
