package accesstypes

import (
	"fmt"
	"strings"
)

// GlobalDomain is the domain used when a permission is applied at the Global level
// instead of to a specific domain.
const GlobalDomain = Domain("global")

const domainPrefix = "domain:"

type Domain string

func UnmarshalDomain(domain string) Domain {
	d := Domain(strings.TrimPrefix(domain, domainPrefix))
	if !d.isValid() {
		panic(fmt.Sprintf("invalid domain %q", domain))
	}

	return d
}

func (d Domain) Marshal() string {
	if !d.isValid() {
		panic(fmt.Sprintf("invalid domain %q, type can not contain prefix", string(d)))
	}

	return domainPrefix + string(d)
}

func (d Domain) isValid() bool {
	return !strings.HasPrefix(string(d), domainPrefix)
}
