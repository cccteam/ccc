package resource

import (
	"reflect"
	"sync"
)

// fieldRegistry resolves a struct type's visible fields by name once and keeps the
// answer. Sort and filter field names arrive with a request; the decoder validates
// them against the request type's field mapper, and every later read of a row's field
// by that name goes through the registry and the field's index rather than a fresh
// by-name lookup on the row.
type fieldRegistry struct {
	mu    sync.RWMutex
	types map[reflect.Type]map[string]reflect.StructField
}

var fields = &fieldRegistry{types: make(map[reflect.Type]map[string]reflect.StructField)}

// of returns the by-name index of t's visible fields, resolving it on first use.
func (r *fieldRegistry) of(t reflect.Type) map[string]reflect.StructField {
	r.mu.RLock()
	byName, ok := r.types[t]
	r.mu.RUnlock()
	if ok {
		return byName
	}

	byName = indexFields(t)

	r.mu.Lock()
	r.types[t] = byName
	r.mu.Unlock()

	return byName
}

// indexFields maps the names of t's visible fields, promoted fields included, to
// their declarations with the same precedence a selector has: the shallowest
// declaration wins, and a name declared twice at the same depth is unreachable.
func indexFields(t reflect.Type) map[string]reflect.StructField {
	byName := make(map[string]reflect.StructField)
	ambiguous := make(map[string]struct{})
	for _, f := range reflect.VisibleFields(t) {
		existing, seen := byName[f.Name]
		switch {
		case !seen:
			byName[f.Name] = f
		case len(f.Index) < len(existing.Index):
			byName[f.Name] = f
			delete(ambiguous, f.Name)
		case len(f.Index) == len(existing.Index):
			ambiguous[f.Name] = struct{}{}
		}
	}
	for name := range ambiguous {
		delete(byName, name)
	}

	return byName
}

// structField resolves a field of the struct type t by name.
func structField(t reflect.Type, name string) (reflect.StructField, bool) {
	f, ok := fields.of(t)[name]

	return f, ok
}

// fieldValue reads the named field of the struct value v; the zero Value when v's
// type has no such field.
func fieldValue(v reflect.Value, name string) reflect.Value {
	f, ok := structField(v.Type(), name)
	if !ok {
		return reflect.Value{}
	}

	return v.FieldByIndex(f.Index)
}
