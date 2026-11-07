// vanity.go is the vanity domains of a hosting organization: a client's own domain an
// application serves on, declared by the domain, delegated to the network project or kept
// at the client's DNS provider and pointed here by the records bedrock prints.

package org

import (
	"bytes"
	"encoding/json"
	"maps"
	"slices"
	"strings"

	"github.com/go-playground/errors/v5"
)

// DNSCustomer is the dns value of a vanity domain kept at the customer's DNS provider.
const DNSCustomer = "customer"

// VanityDomain is what the placement says about one vanity domain beyond its name.
type VanityDomain struct {
	// DNS says where the domain's zone lives: empty, delegated to the network project,
	// whose zone serves it (2-net makes the zone; the registrar or the parent's provider
	// is pointed at it, once); "customer", kept at the customer's DNS provider, which
	// stays authoritative for the domain, its mail and every other record untouched, and
	// carries the records bedrock prints (an A record per hostname the applications
	// declare on the domain, at the load balancer's address, and the certificate's
	// authorization CNAME): no zone here, named records there, never a wildcard.
	DNS string `json:"dns,omitempty"`
}

// VanityDomains are the vanity domains by domain. The type exists for the list the field
// was before: a placement written then is refused with what the field is now, not with a
// decoding error.
type VanityDomains map[string]VanityDomain

// UnmarshalJSON reads the map, and refuses the list the field was.
func (v *VanityDomains) UnmarshalJSON(data []byte) error {
	if bytes.HasPrefix(bytes.TrimSpace(data), []byte("[")) {
		return errors.New(`vanityDomains is a list, and it is a map keyed by the domain now: {"example.com": {}} for a domain delegated to the network project, {"example.com": {"dns": "customer"}} for one kept at its DNS provider`)
	}
	m := map[string]VanityDomain{}
	if err := json.Unmarshal(data, &m); err != nil {
		return errors.Wrap(err, "json.Unmarshal(): vanityDomains")
	}
	*v = m

	return nil
}

// VanityDomainNames are the vanity domains, sorted.
func (p *Placement) VanityDomainNames() []string {
	return slices.Sorted(maps.Keys(p.VanityDomains))
}

// VanityKept reports whether the vanity domain is kept at the customer's DNS provider.
func (p *Placement) VanityKept(domain string) bool {
	return p.VanityDomains[domain].DNS == DNSCustomer
}

// KeptVanityDomains are the vanity domains kept at the customer's DNS provider, sorted.
func (p *Placement) KeptVanityDomains() []string {
	return p.vanityDomainsWhere(true)
}

// DelegatedVanityDomains are the vanity domains delegated to the network project, which
// have a zone there, sorted.
func (p *Placement) DelegatedVanityDomains() []string {
	return p.vanityDomainsWhere(false)
}

func (p *Placement) vanityDomainsWhere(kept bool) []string {
	var out []string
	for _, d := range p.VanityDomainNames() {
		if p.VanityKept(d) == kept {
			out = append(out, d)
		}
	}

	return out
}

// VanityHostnames are the hostnames the organization records on the vanity domain, the
// domain itself and labels of it, sorted: what a kept domain's DNS provider carries an A
// record for.
func (p *Placement) VanityHostnames(domain string) []string {
	var hosts []string
	for _, byEnv := range p.Hostnames {
		for _, byHost := range byEnv {
			for host := range byHost {
				if d, _, ok := p.VanityDomainOf(host); ok && d == domain {
					hosts = append(hosts, host)
				}
			}
		}
	}
	slices.Sort(hosts)

	return slices.Compact(hosts)
}

// VanityAuthorizationName is the name 2-net gives the vanity domain's DNS authorization
// in Certificate Manager, from which the check reads the authorization record of a kept
// domain, which no zone here holds.
func (p *Placement) VanityAuthorizationName(domain string) string {
	return p.Prefix + "-" + netProject + "-gbl-dnsauth-" + strings.ReplaceAll(domain, ".", "-")
}

// KeptVanityRecords are the records a kept vanity domain's DNS provider carries, as the
// rendered README lists them: the host relative to the domain, the type and where the
// value comes from; bedrock domain check prints them with the values.
func (p *Placement) KeptVanityRecords(domain string) []string {
	var lines []string
	for _, host := range p.VanityHostnames(domain) {
		relative := "@"
		if label := strings.TrimSuffix(host, "."+domain); label != host {
			relative = label
		}
		lines = append(lines, relative+" A <the load balancer's address, the address output>")
	}

	return append(lines, "_acme-challenge CNAME <the authorization's target, in the vanity_domains output>")
}
