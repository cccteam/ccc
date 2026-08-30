package accesstypes

import "time"

// Environment is the per-request decision context: the environment attributes
// a permission check may fold conditions against. An environment attribute is
// a fact of the request context that is neither row data nor subject data;
// the v1 vocabulary is a single attribute, now — the request's UTC instant.
//
// Environment is opaque (the Scope precedent): unexported fields behind
// constructors, so later attributes are additive and break nobody. A value is
// built by chaining With* methods, each returning a copy — there is no open
// Set(key, value) bag; every attribute is a real method with a real type,
// added here when that attribute is designed. Callers that layer values chain
// onto what they received: the app's seam adds its values first, the
// framework chains its own after.
//
// Presence is tracked per attribute: accessors return (value, ok), so a set
// zero value is distinguishable from an attribute never supplied. The zero
// Environment is valid and carries nothing.
//
// Fail loud on absence: when a check folds a condition referencing an
// attribute the Environment does not carry, the check must fail with an
// error (500-class) — never a silent allow or deny. A missing input is a
// programming bug, the same posture as a malformed condition.
type Environment struct {
	now    time.Time
	hasNow bool
}

// NewEnvironment returns an Environment carrying no attributes. Attributes
// are added by chaining With* methods.
func NewEnvironment() Environment {
	return Environment{}
}

// EnvironmentAt returns an Environment pinned to the given instant —
// NewEnvironment().WithNow(now). It exists so condition suites pin time
// directly instead of mocking a clock near the engine.
func EnvironmentAt(now time.Time) Environment {
	return NewEnvironment().WithNow(now)
}

// WithNow returns a copy of the Environment carrying now as its time
// attribute, normalized to UTC. Conditions express absolute UTC instants and
// windows only; tenant-local windows are expressed against tenant data
// reached by join, never through the environment.
//
// The instant must be sampled once per request: the value the check folds
// with is the identical value later bound into SQL as a parameter, so the
// two consumers can never disagree about a window boundary.
func (e Environment) WithNow(now time.Time) Environment {
	e.now = now.UTC()
	e.hasNow = true

	return e
}

// Now returns the request's UTC instant and true, or the zero time and false
// when the Environment does not carry one.
func (e Environment) Now() (time.Time, bool) {
	if !e.hasNow {
		return time.Time{}, false
	}

	return e.now, true
}
