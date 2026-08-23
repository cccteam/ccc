package accesstypes

import (
	"fmt"
	"strings"
)

// GlobalDomain is the domain used when a permission is applied at the Global level
// instead of to a specific domain.
//
// The value carries the reserved marker character ':' so it can never collide
// with a caller-authored tenant name: the access policy stores reject any
// other ':'-bearing domain at their write boundary, and a tenant literally
// named "global" is ordinary data, distinct from this marker.
const GlobalDomain = Domain("access:global")

const domainPrefix = "domain:"

// Domain represents a domain in the authorization system
type Domain string

// UnmarshalDomain unmarshals a domain string into a Domain type.
func UnmarshalDomain(domain string) Domain {
	d := Domain(strings.TrimPrefix(domain, domainPrefix))
	if !d.isValid() {
		panic(fmt.Sprintf("invalid domain %q", domain))
	}

	return d
}

// Marshal marshals a Domain type into a string.
func (d Domain) Marshal() string {
	if !d.isValid() {
		panic(fmt.Sprintf("invalid domain %q, type can not contain prefix", string(d)))
	}

	return domainPrefix + string(d)
}

func (d Domain) isValid() bool {
	return !strings.HasPrefix(string(d), domainPrefix)
}
