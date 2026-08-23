package accesstypes

// GlobalDomain is the domain used when a permission is applied at the Global level
// instead of to a specific domain.
//
// The value carries the reserved marker character ':' so it can never collide
// with a caller-authored tenant name: the access policy stores reject any
// other ':'-bearing domain at their write boundary, and a tenant literally
// named "global" is ordinary data, distinct from this marker.
const GlobalDomain = Domain("access:global")

// Domain represents a domain in the authorization system
type Domain string
