package firestore

import "github.com/cccteam/ccc/resource/live"

// DropListener ends the service's current signals listener as Firestore would end it on
// its own, so a test can watch the subscriber reopen it.
func DropListener(s *Service) {
	s.dropListener()
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
