// callback is used to register callback functions
package callback

import (
	"sync"

	"github.com/cccteam/ccc/accesstypes"
)

var registry *Registry

type Registry struct {
	mu sync.RWMutex

	callbacks map[accesstypes.Resource][]any
}

func NewRegistry() *Registry {
	if registry == nil {
		registry = &Registry{
			callbacks: make(map[accesstypes.Resource][]any),
		}
	}

	return registry
}

func (r *Registry) Callbacks(res accesstypes.Resource) []any {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.callbacks[res]
}

func (r *Registry) LoadCallbacks(res accesstypes.Resource, c ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.callbacks[res] = append(r.callbacks[res], c...)
}
