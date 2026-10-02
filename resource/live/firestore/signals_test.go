package firestore_test

import (
	"sync"
	"testing"
	"time"

	"github.com/cccteam/ccc/resource/live"
	livefirestore "github.com/cccteam/ccc/resource/live/firestore"
	"github.com/google/go-cmp/cmp"
)

// The waits the signals tests allow: a signal reaches a listener within settle, and a
// kind that must stay quiet is watched for quiet.
const (
	settle = 10 * time.Second
	quiet  = 500 * time.Millisecond
)

// counter counts the signals one subscription heard.
type counter struct {
	mu sync.Mutex
	n  int
	// heard is signaled on every count, so a waiter wakes without polling.
	heard chan struct{}
}

func newCounter() *counter {
	return &counter{heard: make(chan struct{}, 64)}
}

func (c *counter) signal() {
	c.mu.Lock()
	c.n++
	c.mu.Unlock()
	c.heard <- struct{}{}
}

func (c *counter) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.n
}

// waitFor waits until the counter reaches want, failing the test past settle.
func (c *counter) waitFor(t *testing.T, who string, want int) {
	t.Helper()

	deadline := time.After(settle)
	for c.count() < want {
		select {
		case <-c.heard:
		case <-deadline:
			t.Fatalf("%s heard %d signals within %s, want %d", who, c.count(), settle, want)
		}
	}
}

// subscribe subscribes a counter to the kind on the service, stopped when the test ends.
func subscribe(t *testing.T, svc *livefirestore.Service, kind live.Kind) *counter {
	t.Helper()

	c := newCounter()
	stop, err := svc.Subscribe(kind, c.signal)
	if err != nil {
		t.Fatalf("Subscribe(%s) error = %v", kind, err)
	}
	t.Cleanup(stop)

	return c
}

// signal signals the kind through the service.
func signal(t *testing.T, svc *livefirestore.Service, kind live.Kind) {
	t.Helper()

	if err := svc.Signal(t.Context(), kind); err != nil {
		t.Fatalf("Signal(%s) error = %v", kind, err)
	}
}

// settled waits the quiet period and returns the counts, in order, so a test can pin
// that nothing more arrived.
func settled(counters ...*counter) []int {
	time.Sleep(quiet)
	counts := make([]int, 0, len(counters))
	for _, c := range counters {
		counts = append(counts, c.count())
	}

	return counts
}

// TestService_signals pins the signals over the emulator with two instances on one
// document: a signal of a kind wakes that kind's subscriptions on both instances and
// no other kind's, the state at the start is not a signal, a subscription made after
// the listener began hears the signals after it, and a stopped subscription hears
// nothing more while the others go on.
func TestService_signals(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// run drives two instances on one document and returns the counts to pin, after
		// the quiet period.
		run  func(t *testing.T, first, second *livefirestore.Service) []int
		want []int
	}{
		{
			name: "a signal of a kind wakes its subscriptions on both instances and never another kind's",
			run: func(t *testing.T, first, second *livefirestore.Service) []int {
				t.Helper()
				firstFeatures := subscribe(t, first, live.KindFeatures)
				firstTenants := subscribe(t, first, live.KindTenants)
				secondFeatures := subscribe(t, second, live.KindFeatures)
				signal(t, first, live.KindFeatures)
				firstFeatures.waitFor(t, "the first instance's features subscription", 1)
				secondFeatures.waitFor(t, "the second instance's features subscription", 1)

				return settled(firstFeatures, firstTenants, secondFeatures)
			},
			want: []int{1, 0, 1},
		},
		{
			name: "the other kind wakes its own subscription alone",
			run: func(t *testing.T, first, second *livefirestore.Service) []int {
				t.Helper()
				firstFeatures := subscribe(t, first, live.KindFeatures)
				firstTenants := subscribe(t, first, live.KindTenants)
				secondFeatures := subscribe(t, second, live.KindFeatures)
				signal(t, second, live.KindTenants)
				firstTenants.waitFor(t, "the first instance's tenants subscription", 1)

				return settled(firstFeatures, firstTenants, secondFeatures)
			},
			want: []int{0, 1, 0},
		},
		{
			name: "the state at the start is not a signal; a subscription made after it hears the signals after it",
			run: func(t *testing.T, first, second *livefirestore.Service) []int {
				t.Helper()
				// Written before either instance listens: the state at the start.
				signal(t, first, live.KindPolicy)
				firstPolicy := subscribe(t, first, live.KindPolicy)
				signal(t, first, live.KindPolicy)
				firstPolicy.waitFor(t, "the first instance's policy subscription", 1)
				// The second instance starts listening now, with one signal in the past.
				secondPolicy := subscribe(t, second, live.KindPolicy)
				late := subscribe(t, first, live.KindPolicy)
				if got := settled(secondPolicy, late); got[0] != 0 || got[1] != 0 {
					t.Fatalf("subscriptions made after a signal heard %v, want nothing: the past is not a signal", got)
				}
				signal(t, second, live.KindPolicy)
				firstPolicy.waitFor(t, "the first instance's policy subscription", 2)
				secondPolicy.waitFor(t, "the second instance's policy subscription", 1)
				late.waitFor(t, "the late subscription", 1)

				return settled(firstPolicy, secondPolicy, late)
			},
			want: []int{2, 1, 1},
		},
		{
			name: "a stopped subscription hears nothing more; the others go on",
			run: func(t *testing.T, first, second *livefirestore.Service) []int {
				t.Helper()
				stopped := newCounter()
				stop, err := first.Subscribe(live.KindFeatures, stopped.signal)
				if err != nil {
					t.Fatalf("Subscribe() error = %v", err)
				}
				kept := subscribe(t, first, live.KindFeatures)
				other := subscribe(t, second, live.KindFeatures)
				signal(t, first, live.KindFeatures)
				stopped.waitFor(t, "the subscription to be stopped", 1)
				kept.waitFor(t, "the kept subscription", 1)
				other.waitFor(t, "the other instance's subscription", 1)
				stop()
				signal(t, first, live.KindFeatures)
				kept.waitFor(t, "the kept subscription", 2)
				other.waitFor(t, "the other instance's subscription", 2)

				return settled(stopped, kept, other)
			},
			want: []int{1, 2, 2},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			database := databaseFor(t)
			first := newServiceIn(t, database)
			second := newServiceIn(t, database)
			if diff := cmp.Diff(tt.want, tt.run(t, first, second)); diff != "" {
				t.Errorf("signal counts mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestService_signals_reopen pins the one place that owns reconnection: a listener that
// ends is reopened after the backoff, and on the reopen every kind whose time advanced
// while it was down fires once, on both instances, while a kind that did not stays
// quiet.
func TestService_signals_reopen(t *testing.T) {
	t.Parallel()

	database := databaseFor(t)
	// The first wait is long enough to signal twice while the listener is down, and
	// short enough to settle well inside the deadline.
	backoff := livefirestore.WithReopenBackoff(time.Second, time.Second)
	first := newServiceIn(t, database, backoff)
	second := newServiceIn(t, database, backoff)
	firstFeatures := subscribe(t, first, live.KindFeatures)
	firstTenants := subscribe(t, first, live.KindTenants)
	firstPolicy := subscribe(t, first, live.KindPolicy)
	secondFeatures := subscribe(t, second, live.KindFeatures)
	secondTenants := subscribe(t, second, live.KindTenants)

	livefirestore.DropListener(first)
	livefirestore.DropListener(second)
	// Two kinds advance while both listeners are down, the features kind twice.
	signal(t, first, live.KindFeatures)
	signal(t, first, live.KindFeatures)
	signal(t, first, live.KindTenants)

	firstFeatures.waitFor(t, "the first instance's features subscription", 1)
	firstTenants.waitFor(t, "the first instance's tenants subscription", 1)
	secondFeatures.waitFor(t, "the second instance's features subscription", 1)
	secondTenants.waitFor(t, "the second instance's tenants subscription", 1)
	if diff := cmp.Diff([]int{1, 1, 0, 1, 1}, settled(firstFeatures, firstTenants, firstPolicy, secondFeatures, secondTenants)); diff != "" {
		t.Errorf("signal counts after the reopen mismatch (-want +got):\n%s", diff)
	}

	// The reopened listener is a working one: a later signal reaches it at once.
	signal(t, second, live.KindPolicy)
	firstPolicy.waitFor(t, "the first instance's policy subscription", 1)
}

// TestService_Signal_coalesces pins the publisher's coalescing: a burst of signals of
// one kind while a write of it is in flight is one following write, and the subscribers
// still wake, since the following write lands after the last call.
func TestService_Signal_coalesces(t *testing.T) {
	t.Parallel()

	const burst = 10
	database := databaseFor(t)
	writer := newServiceIn(t, database)
	reader := newServiceIn(t, database)
	features := subscribe(t, reader, live.KindFeatures)

	var mu sync.Mutex
	writes := make([]live.Kind, 0)
	hold := make(chan struct{})
	livefirestore.ObserveWrites(writer, func(kind live.Kind) {
		mu.Lock()
		writes = append(writes, kind)
		first := len(writes) == 1
		mu.Unlock()
		if first {
			<-hold
		}
	})

	// The first signal's write is held in flight; the burst behind it is absorbed and
	// answers at once.
	done := make(chan error, 1)
	go func() {
		done <- writer.Signal(t.Context(), live.KindFeatures)
	}()
	for {
		mu.Lock()
		started := len(writes) == 1
		mu.Unlock()
		if started {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	for range burst {
		signal(t, writer, live.KindFeatures)
	}
	close(hold)
	if err := <-done; err != nil {
		t.Fatalf("Signal() error = %v", err)
	}

	deadline := time.Now().Add(settle)
	for !livefirestore.SignalsIdle(writer) {
		if time.Now().After(deadline) {
			t.Fatal("a signal write is still in flight after the burst")
		}
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	got := len(writes)
	mu.Unlock()
	if got != 2 {
		t.Errorf("writes = %d, want 2: the write in flight and one following write for the %d signals absorbed", got, burst)
	}
	// The reader wakes at least once for the burst, and at most once per write.
	features.waitFor(t, "the reader's features subscription", 1)
	if n := settled(features)[0]; n > 2 {
		t.Errorf("the reader heard %d signals, want at most one per write (2)", n)
	}
}

// TestService_Signal_failure pins the error path: a closed client's write fails and the
// error is returned for the caller to log, and a fresh client signals again.
func TestService_Signal_failure(t *testing.T) {
	t.Parallel()

	database := databaseFor(t)
	closed, err := livefirestore.New(t.Context(), livefirestore.Config{
		ProjectID:    testProject,
		DatabaseID:   database,
		EmulatorHost: firestoreEmulator(t),
	})
	if err != nil {
		t.Fatalf("firestore.New() error = %v", err)
	}
	if err := closed.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := closed.Signal(t.Context(), live.KindFeatures); err == nil {
		t.Fatal("Signal() on a closed client error = nil, want the write's failure")
	}
	if _, err := closed.Subscribe(live.KindFeatures, func() {}); err == nil {
		t.Fatal("Subscribe() on a closed client error = nil, want a refusal")
	}

	fresh := newServiceIn(t, database)
	features := subscribe(t, fresh, live.KindFeatures)
	signal(t, fresh, live.KindFeatures)
	features.waitFor(t, "the fresh client's features subscription", 1)
}
