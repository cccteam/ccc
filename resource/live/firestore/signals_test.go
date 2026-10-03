package firestore_test

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cccteam/ccc/resource/live"
	livefirestore "github.com/cccteam/ccc/resource/live/firestore"
)

// The waits the signals tests allow: a signal reaches a listener within settle, and a
// kind that must stay quiet is watched for quiet. settle is long because it is only ever
// waited out on a failure: on a loaded runner, where the Firestore emulator shares the
// machine with the Spanner emulator and the generation suite, it has taken over ten
// seconds, and once over forty-five, to establish a listener on a database created a
// moment before, and a passing wait ends the instant the signal arrives.
const (
	settle = 2 * time.Minute
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

// signal writes a signal of the kind through the service, failing the test on an error.
// One answer is retried: the emulator answers a write as already canceled when it
// rides the connection whose listener streams were cancelled a moment before (the drop
// in the reopen test), although the document is written and the listeners go on to see
// it; a real Firestore never cancels a call whose context is alive, so the retry is the
// test's allowance for the emulator and nothing the service does.
func signal(t *testing.T, svc *livefirestore.Service, kind live.Kind) {
	t.Helper()

	var err error
	for attempt := range 5 {
		if attempt > 0 {
			time.Sleep(200 * time.Millisecond)
		}
		err = svc.Signal(t.Context(), kind)
		if err == nil {
			return
		}
		if !strings.Contains(err.Error(), "already cancel") || t.Context().Err() != nil {
			break
		}
		t.Logf("Signal(%s) attempt %d answered the emulator's cancellation; retrying", kind, attempt+1)
	}
	t.Fatalf("Signal(%s) error = %v", kind, err)
}

// snapshotLog records the times every snapshot an instance's listener received carried,
// so a count that disagrees with a test's expectation is explained by what Firestore
// delivered rather than guessed at.
type snapshotLog struct {
	mu   sync.Mutex
	seen []string
}

// logSnapshots attaches a snapshotLog to the service.
func logSnapshots(svc *livefirestore.Service) *snapshotLog {
	l := &snapshotLog{}
	livefirestore.ObserveSnapshots(svc, func(times map[live.Kind]time.Time) {
		kinds := make([]string, 0, len(times))
		for kind, at := range times {
			kinds = append(kinds, string(kind)+"@"+at.UTC().Format("15:04:05.000000"))
		}
		sort.Strings(kinds)
		l.mu.Lock()
		l.seen = append(l.seen, "{"+strings.Join(kinds, " ")+"}")
		l.mu.Unlock()
	})

	return l
}

func (l *snapshotLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()

	return strings.Join(l.seen, " ")
}

// expect is one counter's expectation: no wake at all, or at least n when the count is
// a floor. A count is a floor where a write may race a listener's own delivery: under
// load the emulator has delivered one write as more than one advancing snapshot, and a
// wake is a nudge to reread, so a kind woken more often than its writes is not wrong,
// while a kind woken less often, or a kind that must stay quiet waking at all, is.
type expect struct {
	n     int
	floor bool
}

// none expects no wake at all: the kind must stay quiet.
func none() expect {
	return expect{}
}

// atLeast expects n wakes or more.
func atLeast(n int) expect {
	return expect{n: n, floor: true}
}

// logSnapshotsOnFailure prints what each instance's listener received when the test
// fails, so a wait that ran out is read off the snapshots that did or did not arrive.
func logSnapshotsOnFailure(t *testing.T, first, second *snapshotLog) {
	t.Helper()
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("the first instance's snapshots: %s\nthe second instance's snapshots: %s", first, second)
		}
	})
}

// checkCounts compares the counts with the expectations and explains a mismatch with
// what each instance's listener received.
func checkCounts(t *testing.T, what string, want []expect, got []int, logs ...*snapshotLog) {
	t.Helper()

	ok := len(want) == len(got)
	for i := range want {
		if !ok {
			break
		}
		if want[i].floor && got[i] < want[i].n || !want[i].floor && got[i] != want[i].n {
			ok = false
		}
	}
	if ok {
		return
	}
	wants := make([]string, 0, len(want))
	for _, w := range want {
		if w.floor {
			wants = append(wants, fmt.Sprintf(">=%d", w.n))
		} else {
			wants = append(wants, fmt.Sprintf("%d", w.n))
		}
	}
	delivered := make([]string, 0, len(logs))
	for i, l := range logs {
		delivered = append(delivered, fmt.Sprintf("instance %d received %s", i+1, l))
	}
	t.Errorf("%s: counts %v, want [%s]; %s", what, got, strings.Join(wants, " "), strings.Join(delivered, "; "))
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
		want []expect
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
			want: []expect{atLeast(1), none(), atLeast(1)},
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
			want: []expect{none(), atLeast(1), none()},
		},
		{
			name: "a subscription made before the first snapshot is woken once for a kind the document holds; one made after hears only the signals after it",
			run: func(t *testing.T, first, second *livefirestore.Service) []int {
				t.Helper()
				// Written before either instance listens: the state at the start.
				signal(t, first, live.KindPolicy)
				// The first instance's listener opens here; its first snapshot holds the
				// policy kind, so the subscription made before it wakes once.
				firstPolicy := subscribe(t, first, live.KindPolicy)
				firstPolicy.waitFor(t, "the first instance's policy subscription, at the start", 1)
				signal(t, first, live.KindPolicy)
				firstPolicy.waitFor(t, "the first instance's policy subscription", 2)
				// The second instance starts listening now, with two signals in the past:
				// one wake, not two.
				secondPolicy := subscribe(t, second, live.KindPolicy)
				secondPolicy.waitFor(t, "the second instance's policy subscription, at the start", 1)
				// A subscription made after the first instance's listener began hears
				// nothing of the past.
				late := subscribe(t, first, live.KindPolicy)
				if got := settled(secondPolicy, late); got[0] != 1 || got[1] != 0 {
					t.Fatalf("after the start: second instance %d (want one wake), late subscription %d (want none): the past is one nudge at most", got[0], got[1])
				}
				signal(t, second, live.KindPolicy)
				firstPolicy.waitFor(t, "the first instance's policy subscription", 3)
				secondPolicy.waitFor(t, "the second instance's policy subscription", 2)
				late.waitFor(t, "the late subscription", 1)

				return settled(firstPolicy, secondPolicy, late)
			},
			want: []expect{atLeast(3), atLeast(2), atLeast(1)},
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
				// The first signal's deliveries are complete after the quiet period, so
				// what the stopped subscription hears from here on is the second signal's.
				time.Sleep(quiet)
				before := stopped.count()
				stop()
				signal(t, first, live.KindFeatures)
				kept.waitFor(t, "the kept subscription", 2)
				other.waitFor(t, "the other instance's subscription", 2)
				counts := settled(stopped, kept, other)
				counts[0] -= before

				return counts
			},
			want: []expect{none(), atLeast(2), atLeast(2)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			database := databaseFor(t)
			first := newServiceIn(t, database)
			second := newServiceIn(t, database)
			firstLog, secondLog := logSnapshots(first), logSnapshots(second)
			logSnapshotsOnFailure(t, firstLog, secondLog)
			checkCounts(t, "signal counts", tt.want, tt.run(t, first, second), firstLog, secondLog)
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
	firstLog, secondLog := logSnapshots(first), logSnapshots(second)
	logSnapshotsOnFailure(t, firstLog, secondLog)
	firstFeatures := subscribe(t, first, live.KindFeatures)
	firstTenants := subscribe(t, first, live.KindTenants)
	firstPolicy := subscribe(t, first, live.KindPolicy)
	secondFeatures := subscribe(t, second, live.KindFeatures)
	secondTenants := subscribe(t, second, live.KindTenants)
	// Subscribe waits on nothing, so a signal heard on both instances is what proves
	// both listeners live before they are dropped; otherwise a drop could land before a
	// listener's first snapshot and the reopen would count one advance more.
	signal(t, first, live.KindFeatures)
	firstFeatures.waitFor(t, "the first instance's features subscription", 1)
	secondFeatures.waitFor(t, "the second instance's features subscription", 1)

	livefirestore.DropListener(first)
	livefirestore.DropListener(second)
	// Two kinds advance while both listeners are down, the features kind twice.
	signal(t, first, live.KindFeatures)
	signal(t, first, live.KindFeatures)
	signal(t, first, live.KindTenants)

	firstFeatures.waitFor(t, "the first instance's features subscription", 2)
	firstTenants.waitFor(t, "the first instance's tenants subscription", 1)
	secondFeatures.waitFor(t, "the second instance's features subscription", 2)
	secondTenants.waitFor(t, "the second instance's tenants subscription", 1)
	// The kinds that advanced while the listeners were down wake once each on the
	// reopen, the writes before the drop having been heard; the kind that did not
	// advance stays quiet.
	checkCounts(t, "signal counts after the reopen", []expect{atLeast(2), atLeast(1), none(), atLeast(2), atLeast(1)},
		settled(firstFeatures, firstTenants, firstPolicy, secondFeatures, secondTenants), firstLog, secondLog)

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
