package accesstypes

// Scope identifies the permission partition an operation applies to: the
// global partition or a single tenant domain. Global-ness is structural — the
// unexported flag, never a distinguished Domain value — so no domain string
// can ever reference the global partition: DomainScope("anything") is a tenant
// scope by construction. The zero Scope is the zero Domain's tenant scope,
// which holds no grants (fail closed), never global.
//
// A tenant domain belongs to an axis: the dimension its tenant list partitions
// the application along. An application with one tenant list declares no axis
// and its scopes belong to the default axis, whose identity is the empty
// string — permanently, so rows stored under it stay correct if the
// application later declares a second axis. Every constructor in this package
// produces a default-axis scope; there is no constructor for a named axis
// yet. The field exists now so that comparisons, map keys, and the permission
// stores' row identity already carry the axis, and adding one later changes
// nothing about them.
//
// Scope is comparable and usable as a map key. It deliberately has no parsed
// or serialized form: configuration and wire formats express global
// structurally (a separate field or key), never as a magic string.
type Scope struct {
	axis   string
	domain Domain
	global bool
}

// GlobalScope returns the Scope for the global partition, where permissions
// apply across the entire application rather than within a tenant domain.
func GlobalScope() Scope {
	return Scope{global: true}
}

// DomainScope returns the tenant Scope for domain. Any domain value is a
// legal tenant name; no value routes to the global partition.
func DomainScope(domain Domain) Scope {
	return Scope{domain: domain}
}

// IsGlobal reports whether the scope is the global partition.
func (s Scope) IsGlobal() bool {
	return s.global
}

// Domain returns the tenant domain and true for a tenant scope, or the zero
// Domain and false for the global scope.
func (s Scope) Domain() (Domain, bool) {
	if s.global {
		return "", false
	}

	return s.domain, true
}

// Axis returns the name of the axis the scope belongs to. The default axis is
// the empty string, and every Scope this package constructs belongs to it; the
// global scope carries the empty axis as well.
func (s Scope) Axis() string {
	return s.axis
}

// String renders the scope for display only: "global" for the global scope,
// otherwise the tenant domain. The output is ambiguous by design (a tenant
// literally named "global" renders identically) and must never be parsed;
// use IsGlobal for logic.
func (s Scope) String() string {
	if s.global {
		return "global"
	}

	return string(s.domain)
}
