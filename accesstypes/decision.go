package accesstypes

import (
	"fmt"
	"slices"
)

// decisionOutcome is the closed set of answers a permission check gives. The
// zero value is denied, so a zero Decision fails closed.
type decisionOutcome int

const (
	decisionDenied decisionOutcome = iota
	decisionGranted
	decisionConditional
)

// Decision is the per-resource answer from a permission check:
//
//   - Denied: no grant matches; nothing to evaluate.
//   - Granted: at least one matching grant is unconditional; conditions on
//     other grants are moot.
//   - Conditional: only conditional grants match. The decision carries the
//     conditions grouped by field scope (ConditionGroups), which the data
//     layer must find true.
//
// Decision is opaque: values come from the Denied, Granted, and Conditional
// constructors, so a Conditional decision always carries its groups and the
// other outcomes never do. The zero Decision is Denied — fail closed.
type Decision struct {
	outcome decisionOutcome
	groups  []ConditionGroup
}

// Denied returns the Decision for a check no grant matches.
func Denied() Decision {
	return Decision{}
}

// Granted returns the Decision for a check matched by at least one
// unconditional grant.
func Granted() Decision {
	return Decision{outcome: decisionGranted}
}

// Conditional returns the Decision for a check matched only by conditional
// grants, carrying the conditions grouped by field scope. At least one group
// is required: a Conditional decision with nothing to evaluate is a
// programming bug, so an empty call panics.
func Conditional(groups ...ConditionGroup) Decision {
	if len(groups) == 0 {
		panic("accesstypes.Conditional() requires at least one ConditionGroup")
	}

	return Decision{outcome: decisionConditional, groups: groups}
}

// IsDenied reports whether the decision is Denied.
func (d Decision) IsDenied() bool {
	return d.outcome == decisionDenied
}

// IsGranted reports whether the decision is Granted.
func (d Decision) IsGranted() bool {
	return d.outcome == decisionGranted
}

// IsConditional reports whether the decision is Conditional.
func (d Decision) IsConditional() bool {
	return d.outcome == decisionConditional
}

// ConditionGroups returns the condition groups a Conditional decision
// carries, or nil for Denied and Granted.
func (d Decision) ConditionGroups() []ConditionGroup {
	return d.groups
}

// String renders the decision outcome for display and error messages only;
// it is never parsed.
func (d Decision) String() string {
	switch d.outcome {
	case decisionDenied:
		return "denied"
	case decisionGranted:
		return "granted"
	case decisionConditional:
		return "conditional"
	default:
		return fmt.Sprintf("invalid decision outcome %d", int(d.outcome))
	}
}

// ConditionGroup pairs one condition payload with the field scope it covers.
// The group key is a field's complete set of covering grants: fields whose
// covering sets are identical share one group, because their any-of
// combinations would render as the same expression anyway; a unique
// combination is its own group. The OR across a group's covering grants
// happens inside its Condition payload; the AND across groups (every touched
// field must pass) is computed by the caller across group results — kept
// separate so a denial can name the failing group's resources.
type ConditionGroup struct {
	// Resources is the field scope: the checked resources sharing this
	// group's covering-grant set.
	Resources []Resource

	// Condition is the group's single payload — the any-of combination of the
	// covering grants' conditions.
	Condition Condition
}

// Decisions is the answer to one permission check: the Decision for each
// checked resource, all from a single policy snapshot (one call, one
// snapshot — a second fetch could let a revocation land between check and
// filter).
type Decisions map[Resource]Decision

// DeniedResources returns the checked resources whose decision is Denied,
// sorted lexically for deterministic error messages. An empty result means
// no resource was denied.
func (d Decisions) DeniedResources() []Resource {
	var denied []Resource
	for resource, decision := range d {
		if decision.IsDenied() {
			denied = append(denied, resource)
		}
	}
	slices.Sort(denied)

	return denied
}

// ConditionalResources returns the checked resources whose decision is
// Conditional, sorted lexically for deterministic error messages. An empty
// result means no resource's answer depends on a condition.
func (d Decisions) ConditionalResources() []Resource {
	var conditional []Resource
	for resource, decision := range d {
		if decision.IsConditional() {
			conditional = append(conditional, resource)
		}
	}
	slices.Sort(conditional)

	return conditional
}
