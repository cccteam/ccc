package firestore

import (
	"context"
	"slices"
	"sync"
	"time"

	cloudfirestore "cloud.google.com/go/firestore"
	"github.com/cccteam/ccc/resource/live"
	"github.com/cccteam/logger"
	"github.com/go-playground/errors/v5"
)

// The reopen backoff: how long the subscriber waits before reopening a listener
// Firestore ended, first and at most.
const (
	ReopenFirst = time.Second
	ReopenMost  = 30 * time.Second
)

// The signals document's fields under each kind.
const (
	signalAtField = "at"
	signalByField = "by"
)

// signalsState is the signaler's and the subscriber's state: the per-kind write in
// flight and whether another is owed, and the one listener with its consumers.
type signalsState struct {
	mu sync.Mutex
	// writes is the coalescing state per kind.
	writes map[live.Kind]*signalWrite
	// onWrite, when set, observes every write of the document before it is sent; the
	// package's tests count and hold writes through it.
	onWrite func(kind live.Kind)

	// consumers are the subscriptions per kind, by id, run in id order.
	consumers map[live.Kind]map[int]func()
	nextID    int
	// seen is each kind's time as of the last snapshot handled: a kind fires when its
	// time advances past it.
	seen map[live.Kind]time.Time
	// listening says the listener was started; endListener ends the current listener
	// (the test's way of ending it as Firestore would) and the loop reopens it.
	listening   bool
	endListener context.CancelFunc
	reopenFirst time.Duration
	reopenMost  time.Duration
}

// signalWrite is one kind's coalescing state.
type signalWrite struct {
	inFlight bool
	pending  bool
}

// newSignalsState returns the empty state with the default backoff.
func newSignalsState() signalsState {
	return signalsState{
		writes:      make(map[live.Kind]*signalWrite),
		consumers:   make(map[live.Kind]map[int]func()),
		seen:        make(map[live.Kind]time.Time),
		reopenFirst: ReopenFirst,
		reopenMost:  ReopenMost,
	}
}

// signalsDoc returns the application's signals document.
func (s *Service) signalsDoc() *cloudfirestore.DocumentRef {
	return s.client.Collection(ApplicationCollection).Doc(SignalsDocument)
}

// Signal writes the kind's field of the signals document, {at: the server timestamp,
// by: this instance}, merged so the other kinds' fields stay: one write, which every
// instance's listener observes as the kind's time advancing. While a write of the kind
// is in flight a later Signal of the kind is absorbed and answers nil at once; when the
// write in flight ends, the service makes one following write of its own for every
// Signal absorbed meanwhile (a signal carries nothing but the fact of a change, so one
// write after the last call covers every call before it), and logs that write's
// failure. The error of the caller's own write is returned for the caller to log; the
// caller's contract stays log and never fail the request.
func (s *Service) Signal(ctx context.Context, kind live.Kind) error {
	if !s.beginWrite(kind) {
		return nil
	}
	err := s.writeSignal(ctx, kind)
	s.endWrite(kind)

	return err
}

// beginWrite takes the kind's write slot, or notes that a following write is owed when
// the slot is taken, and reports whether the caller writes.
func (s *Service) beginWrite(kind live.Kind) bool {
	s.signals.mu.Lock()
	defer s.signals.mu.Unlock()
	write := s.signals.writes[kind]
	if write == nil {
		write = &signalWrite{}
		s.signals.writes[kind] = write
	}
	if write.inFlight {
		write.pending = true

		return false
	}
	write.inFlight = true

	return true
}

// endWrite releases the kind's write slot, or keeps it and makes the following write
// the absorbed signals are owed, on the service's own goroutine and context.
func (s *Service) endWrite(kind live.Kind) {
	s.signals.mu.Lock()
	write := s.signals.writes[kind]
	if !write.pending {
		write.inFlight = false
		s.signals.mu.Unlock()

		return
	}
	write.pending = false
	s.signals.mu.Unlock()

	go s.followingWrite(kind)
}

// followingWrite is the one write the signals absorbed during a write in flight are
// owed: bounded like a request's publish, its failure logged, and the slot handled as
// any write's is, so signals absorbed meanwhile get one more.
func (s *Service) followingWrite(kind live.Kind) {
	ctx, cancel := context.WithTimeout(s.lifetime, live.PublishTimeout)
	defer cancel()
	if err := s.writeSignal(ctx, kind); err != nil && s.lifetime.Err() == nil {
		logger.FromCtx(s.lifetime).Errorf("live: the following signal of kind %q failed; the subscribers reread at their backstop: %v", kind, err)
	}
	s.endWrite(kind)
}

// writeSignal is the one write: the kind's field set with merge.
func (s *Service) writeSignal(ctx context.Context, kind live.Kind) error {
	s.signals.mu.Lock()
	onWrite := s.signals.onWrite
	s.signals.mu.Unlock()
	if onWrite != nil {
		onWrite(kind)
	}
	field := string(kind)
	data := map[string]any{
		field: map[string]any{
			signalAtField: cloudfirestore.ServerTimestamp,
			signalByField: s.instance,
		},
	}
	if _, err := s.signalsDoc().Set(ctx, data, cloudfirestore.Merge(cloudfirestore.FieldPath{field})); err != nil {
		return errors.Wrap(err, "firestore.DocumentRef.Set()")
	}

	return nil
}

// Subscribe runs onSignal on every signal of the kind from now on, on the listener's
// goroutine. The first Subscribe opens this instance's one listener on the signals
// document and returns without waiting for it: nothing here waits on the network, so
// a backend that is slow to answer never holds a caller, the signals mutex or a
// consumer's shutdown. The listener's first snapshot is handled like every later one,
// against times not seen yet, so a subscription made before it arrives is woken once
// for each kind the document already holds (the state at the start, which the
// subscription cannot tell from a signal written just after it was made; a wake is a
// nudge to reread, never a fact) and no signal after Subscribe returns is missed; a
// subscription made after the first snapshot hears the signals after it alone. A
// subscription made later joins the running listener. stop ends this subscription
// alone; the listener lives until Close.
func (s *Service) Subscribe(kind live.Kind, onSignal func()) (func(), error) {
	if err := s.startListening(); err != nil {
		return nil, err
	}
	s.signals.mu.Lock()
	defer s.signals.mu.Unlock()
	id := s.signals.nextID
	s.signals.nextID++
	if s.signals.consumers[kind] == nil {
		s.signals.consumers[kind] = make(map[int]func())
	}
	s.signals.consumers[kind][id] = onSignal

	return func() {
		s.signals.mu.Lock()
		defer s.signals.mu.Unlock()
		delete(s.signals.consumers[kind], id)
	}, nil
}

// startListening opens the listener once and hands it to the loop, which reads its
// snapshots; it blocks on nothing, so the first snapshot seeds each kind's time and
// wakes the subscriptions already made, on the loop's goroutine.
func (s *Service) startListening() error {
	s.signals.mu.Lock()
	defer s.signals.mu.Unlock()
	if s.lifetime.Err() != nil {
		return errors.New("live/firestore: the service is closed")
	}
	if s.signals.listening {
		return nil
	}
	snapshots, end := s.openListener()
	s.signals.listening = true
	s.signals.endListener = end
	go s.listen(snapshots, end)

	return nil
}

// openListener opens a listener on the signals document under its own context, which
// ends it, under the service's lifetime, which ends every listener.
func (s *Service) openListener() (*cloudfirestore.DocumentSnapshotIterator, context.CancelFunc) {
	ctx, end := context.WithCancel(s.lifetime)

	return s.signalsDoc().Snapshots(ctx), end
}

// listen is the listener's loop: every snapshot fires the kinds whose time advanced
// past the last seen, the first snapshot of all against no time seen (so it wakes the
// subscriptions made before it for the kinds the document holds); a listener that
// ends while the service lives is logged and reopened after the backoff, and the
// reopened listener's first snapshot is handled like any other, so every kind whose
// time advanced while the listener was down fires once.
func (s *Service) listen(snapshots *cloudfirestore.DocumentSnapshotIterator, end context.CancelFunc) {
	delay := s.signals.reopenFirst
	for {
		delivered, err := s.deliver(snapshots)
		snapshots.Stop()
		end()
		if s.lifetime.Err() != nil {
			return
		}
		if delivered {
			delay = s.signals.reopenFirst
		}
		logger.FromCtx(s.lifetime).Errorf("live: the signals listener ended; reopening in %s: %v", delay, err)
		select {
		case <-s.lifetime.Done():
			return
		case <-time.After(delay):
		}
		delay = min(delay*2, s.signals.reopenMost)
		snapshots, end = s.openListener()
		s.signals.mu.Lock()
		s.signals.endListener = end
		s.signals.mu.Unlock()
	}
}

// deliver fires the kinds each snapshot advances until the listener ends, reporting
// whether it delivered any snapshot and why it ended.
func (s *Service) deliver(snapshots *cloudfirestore.DocumentSnapshotIterator) (bool, error) {
	delivered := false
	for {
		snapshot, err := snapshots.Next()
		if err != nil {
			return delivered, errors.Wrap(err, "firestore.DocumentSnapshotIterator.Next()")
		}
		delivered = true
		s.fire(snapshot)
	}
}

// fire runs the subscriptions of every kind whose time the snapshot advanced past the
// last seen, in subscription order, and notes the new times.
func (s *Service) fire(snapshot *cloudfirestore.DocumentSnapshot) {
	times := signalTimes(snapshot)
	s.signals.mu.Lock()
	kinds := make([]live.Kind, 0, len(times))
	for kind := range times {
		kinds = append(kinds, kind)
	}
	slices.Sort(kinds)
	var wake []func()
	for _, kind := range kinds {
		at := times[kind]
		if !at.After(s.signals.seen[kind]) {
			continue
		}
		s.signals.seen[kind] = at
		ids := make([]int, 0, len(s.signals.consumers[kind]))
		for id := range s.signals.consumers[kind] {
			ids = append(ids, id)
		}
		slices.Sort(ids)
		for _, id := range ids {
			wake = append(wake, s.signals.consumers[kind][id])
		}
	}
	s.signals.mu.Unlock()

	for _, onSignal := range wake {
		onSignal()
	}
}

// signalTimes reads each kind's time out of a snapshot of the signals document: every
// field holding a map with a timestamp at "at". A document that does not exist, or a
// field of another shape, holds no kind.
func signalTimes(snapshot *cloudfirestore.DocumentSnapshot) map[live.Kind]time.Time {
	times := make(map[live.Kind]time.Time)
	if snapshot == nil || !snapshot.Exists() {
		return times
	}
	for field, value := range snapshot.Data() {
		entry, ok := value.(map[string]any)
		if !ok {
			continue
		}
		at, ok := entry[signalAtField].(time.Time)
		if !ok {
			continue
		}
		times[live.Kind(field)] = at
	}

	return times
}

// dropListener ends the current listener as Firestore would end it on its own: the
// loop logs it and reopens after the backoff. The package's tests drive the reopen
// through it.
func (s *Service) dropListener() {
	s.signals.mu.Lock()
	end := s.signals.endListener
	s.signals.mu.Unlock()
	if end != nil {
		end()
	}
}

// signalsIdle reports whether no signal write is in flight for any kind.
func (s *Service) signalsIdle() bool {
	s.signals.mu.Lock()
	defer s.signals.mu.Unlock()
	for _, write := range s.signals.writes {
		if write.inFlight {
			return false
		}
	}

	return true
}

// observeWrites sets the function every signal write is reported to before it is sent.
func (s *Service) observeWrites(onWrite func(kind live.Kind)) {
	s.signals.mu.Lock()
	defer s.signals.mu.Unlock()
	s.signals.onWrite = onWrite
}
