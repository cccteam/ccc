package accesstypes

// Domain represents a tenant domain in the authorization system. A Domain is
// always data — any string is a legal tenant name. Whether an operation
// applies to a tenant domain or to the global partition is expressed by
// Scope, never by a distinguished Domain value.
type Domain string
