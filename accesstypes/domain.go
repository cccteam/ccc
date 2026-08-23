package accesstypes

import "strings"

// GlobalDomain is the domain used when a permission is applied at the Global level
// instead of to a specific domain.
//
// The value carries the reserved marker character ':' so it can never collide
// with a caller-authored tenant name: the access policy stores reject any
// other ':'-bearing domain at their write boundary, and a tenant literally
// named "global" is ordinary data, distinct from this marker.
const GlobalDomain = Domain("access:global")

// reservedMarker is the character reserved for access-defined sentinel values
// (GlobalDomain, GlobalResource). It may never appear in caller-authored domain
// or resource names.
const reservedMarker = ':'

// Domain represents a domain in the authorization system
type Domain string

// HasReservedMarker reports whether the domain value carries the reserved marker
// character ':'. Only access-defined sentinels (GlobalDomain) legitimately carry
// it, and a sentinel is code, never data: ingestion boundaries (route guards,
// consolidated operation paths) reject every marker-bearing value supplied as
// data — including a literal "access:global" — so a misconfigured tenant list can
// never route a check into the global partition.
func (d Domain) HasReservedMarker() bool {
	return strings.ContainsRune(string(d), reservedMarker)
}
