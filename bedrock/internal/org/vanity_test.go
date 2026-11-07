package org_test

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/cccteam/ccc/bedrock/internal/org"
)

// TestVanityDomainsShape: the map reads, the list the field was is refused with what it
// is now, and the helpers tell the kept domains, their hostnames and their records apart.
func TestVanityDomainsShape(t *testing.T) {
	t.Parallel()

	var p org.Placement
	if err := json.Unmarshal([]byte(`{"vanityDomains": ["harborlights.example"]}`), &p); err == nil || !strings.Contains(err.Error(), `vanityDomains is a list, and it is a map keyed by the domain now: {"example.com": {}} for a domain delegated to the network project, {"example.com": {"dns": "customer"}} for one kept at its DNS provider`) {
		t.Errorf("the list form: error = %v", err)
	}
	if err := json.Unmarshal([]byte(`{"prefix": "imp", "vanityDomains": {"harborlights.example": {}, "beacon.example": {"dns": "customer"}}, "hostnames": {"harbor": {"prd": {"beacon.example": "b", "www.beacon.example": "b", "harbor.apps.imp.example": "b"}, "stg": {"stg.beacon.example": "b"}}}}`), &p); err != nil {
		t.Fatal(err)
	}
	if got := p.VanityDomainNames(); !slices.Equal(got, []string{"beacon.example", "harborlights.example"}) {
		t.Errorf("VanityDomainNames() = %v", got)
	}
	if got := p.KeptVanityDomains(); !slices.Equal(got, []string{"beacon.example"}) {
		t.Errorf("KeptVanityDomains() = %v", got)
	}
	if got := p.DelegatedVanityDomains(); !slices.Equal(got, []string{"harborlights.example"}) {
		t.Errorf("DelegatedVanityDomains() = %v", got)
	}
	if !p.VanityKept("beacon.example") || p.VanityKept("harborlights.example") || p.VanityKept("other.example") {
		t.Error("VanityKept() tells the kept domain wrong")
	}
	if got := p.VanityHostnames("beacon.example"); !slices.Equal(got, []string{"beacon.example", "stg.beacon.example", "www.beacon.example"}) {
		t.Errorf("VanityHostnames() = %v", got)
	}
	if got := p.VanityAuthorizationName("beacon.example"); got != "imp-net-gbl-dnsauth-beacon-example" {
		t.Errorf("VanityAuthorizationName() = %q", got)
	}
	if got := p.KeptVanityRecords("beacon.example"); !slices.Equal(got, []string{
		"@ A <the load balancer's address, the address output>",
		"stg A <the load balancer's address, the address output>",
		"www A <the load balancer's address, the address output>",
		"_acme-challenge CNAME <the authorization's target, in the vanity_domains output>",
	}) {
		t.Errorf("KeptVanityRecords() = %q", got)
	}
}
