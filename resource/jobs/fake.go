package jobs

import (
	"context"
	"strconv"
	"sync"
)

// Fake is a Starter for tests: it records every start with its arguments and answers an
// execution name of its own, or the error it was given.
type Fake struct {
	mu sync.Mutex
	// Err is answered by every start when set.
	Err     error
	started [][]string
}

// NewFake is a fake that has started nothing yet.
func NewFake() *Fake {
	return &Fake{}
}

// Start records the start and answers executions/1, executions/2, ... in order, or Err.
func (f *Fake) Start(_ context.Context, args ...string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.Err != nil {
		return "", f.Err
	}
	f.started = append(f.started, append([]string(nil), args...))

	return "projects/p/locations/l/jobs/j/executions/j-" + strconv.Itoa(len(f.started)), nil
}

// Started is every start's arguments, in order.
func (f *Fake) Started() [][]string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([][]string(nil), f.started...)
}
