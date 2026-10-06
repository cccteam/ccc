package accesstypes

// policyKind is the kind of place a policy row is held in.
type policyKind uint8

// The kinds. One domain is the zero kind, so the zero PolicyScope is the zero
// Domain's partition, which holds nothing (fail closed), never the global
// partition and never every domain.
const (
	policyKindDomain policyKind = iota
	policyKindGlobal
	policyKindEvery
)

// PolicyScope is where policy is held: a role membership, a custom role and
// its grants live in the global partition, in one tenant domain, or in every
// tenant domain. It is the type the policy store, the user manager and the
// login's role synchronization take.
//
// A permission check takes a Scope, not a PolicyScope: a request is in one
// partition, and "every domain" is not a place a request can be in. The two
// types are kept apart so that a value of this type cannot reach a check: a
// PolicyScope is not a Scope, and nothing converts one into a Scope.
// Scope.PolicyScope converts the other way, since every partition a request
// can be in is also a place policy can be held: the global scope is held
// globally, a tenant scope in that one domain.
//
// Every-domain-ness is structural, the unexported kind, never a distinguished
// Domain value: DomainPolicyScope("anything") is one tenant domain by
// construction. A membership held in every domain reaches every tenant scope,
// including one the store holds no other row in, so a tenant created at run
// time needs nothing written for its every-domain members. The axis is the
// one Scope carries, the default axis, for the same reason.
//
// PolicyScope is comparable and usable as a map key. Like Scope it has no
// parsed or serialized form: stores and wire formats express the kind
// structurally.
type PolicyScope struct {
	axis   string
	domain Domain
	kind   policyKind
}

// GlobalPolicyScope returns the place policy is held for the global
// partition.
func GlobalPolicyScope() PolicyScope {
	return PolicyScope{kind: policyKindGlobal}
}

// DomainPolicyScope returns the place policy is held for one tenant domain.
// Any domain value is a legal tenant name; no value routes to the global
// partition or to every domain.
func DomainPolicyScope(domain Domain) PolicyScope {
	return PolicyScope{domain: domain}
}

// EveryDomainPolicyScope returns the place policy is held for every tenant
// domain: a membership or a custom role held there reaches every tenant
// scope, those that exist today and those created later.
func EveryDomainPolicyScope() PolicyScope {
	return PolicyScope{kind: policyKindEvery}
}

// PolicyScope returns where policy for this partition is held: the global
// scope is held globally, a tenant scope in that one domain. There is no
// conversion the other way.
func (s Scope) PolicyScope() PolicyScope {
	if s.global {
		return PolicyScope{axis: s.axis, kind: policyKindGlobal}
	}

	return PolicyScope{axis: s.axis, domain: s.domain}
}

// IsGlobal reports whether policy is held in the global partition.
func (p PolicyScope) IsGlobal() bool {
	return p.kind == policyKindGlobal
}

// IsEveryDomain reports whether policy is held in every tenant domain.
func (p PolicyScope) IsEveryDomain() bool {
	return p.kind == policyKindEvery
}

// Domain returns the tenant domain and true when policy is held in one
// domain, or the zero Domain and false for the global partition and for
// every domain.
func (p PolicyScope) Domain() (Domain, bool) {
	if p.kind != policyKindDomain {
		return "", false
	}

	return p.domain, true
}

// Axis returns the name of the axis the place belongs to: the default axis,
// the empty string, for every value this package constructs.
func (p PolicyScope) Axis() string {
	return p.axis
}

// Covers reports whether policy held here applies in scope: the global
// partition covers the global scope, one domain covers that domain's scope,
// and every domain covers every tenant scope.
func (p PolicyScope) Covers(scope Scope) bool {
	if p.axis != scope.axis {
		return false
	}
	switch p.kind {
	case policyKindGlobal:
		return scope.global
	case policyKindEvery:
		return !scope.global
	default:
		return !scope.global && scope.domain == p.domain
	}
}

// String renders the place for display only: "global" for the global
// partition, "every domain" for every domain, otherwise the tenant domain.
// The output is ambiguous by design (a tenant literally named "global" or
// "every domain" renders identically) and must never be parsed; use the
// predicates for logic.
func (p PolicyScope) String() string {
	switch p.kind {
	case policyKindGlobal:
		return "global"
	case policyKindEvery:
		return "every domain"
	default:
		return string(p.domain)
	}
}
