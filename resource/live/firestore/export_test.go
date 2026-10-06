package firestore

import (
	"context"
	"time"

	"github.com/go-playground/errors/v5"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/cccteam/ccc/resource/live"
)

// DropListener ends the service's current signals listener as Firestore would end it on
// its own, so a test can watch the subscriber reopen it.
func DropListener(s *Service) {
	s.dropListener()
}

// StallFirstSnapshots makes the next n listeners' first snapshots go unseen, as a stream
// that never speaks would leave them, so a test can watch the first snapshot bound
// reopen the listener.
func StallFirstSnapshots(s *Service, n int) {
	s.stallFirstSnapshots(n)
}

// IgnoreListenerSnapshots drops every snapshot the service's listener delivers, so a test
// can watch the reconcile reads wake the subscriptions on their own.
func IgnoreListenerSnapshots(s *Service) {
	s.ignoreListenerSnapshots()
}

// LateReopens is how many listeners the service reopened for a first snapshot that
// never came.
func LateReopens(s *Service) int {
	return s.lateReopenCount()
}

// SignalsIdle reports whether no signal write is in flight for any kind.
func SignalsIdle(s *Service) bool {
	return s.signalsIdle()
}

// ObserveWrites sets the function every signal write is reported to before it is sent,
// so a test can count writes and hold one in flight.
func ObserveWrites(s *Service, onWrite func(kind live.Kind)) {
	s.observeWrites(onWrite)
}

// ObserveSnapshots sets the function every snapshot's times are reported to before the
// kinds it advances fire, so a test can explain the wakes it counted.
func ObserveSnapshots(s *Service, onSnapshot func(times map[live.Kind]time.Time)) {
	s.observeSnapshots(onSnapshot)
}

// ReadSignals reads the signals document back with a plain get, outside any listener,
// and returns the times it holds: a test that waited a listener out tells a write that
// never landed (the document lacks the kind) from an update the listener was not
// delivered (the document holds it). A document that does not exist holds no kind.
func ReadSignals(ctx context.Context, s *Service) (map[live.Kind]time.Time, error) {
	snapshot, err := s.signalsDoc().Get(ctx)
	if err != nil && status.Code(err) != codes.NotFound {
		return nil, errors.Wrap(err, "firestore.DocumentRef.Get()")
	}

	return signalTimes(snapshot), nil
}
