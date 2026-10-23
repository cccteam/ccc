package org

import (
	"strings"
	"testing"
)

func TestRetire(t *testing.T) {
	t.Parallel()

	p := testPlacement(t)
	p.Registration = map[string]string{"harbor": RegistrationGrants}
	p.OutOfService = map[string][]string{"harbor": {"tst", "stg", "prd"}}
	p.Hostnames = map[string]map[string]map[string]string{"harbor": {"prd": {"harbor.impulseframework.dev": "projects/p/global/backendServices/b"}}}
	if err := p.Retire("harbor"); err != nil {
		t.Fatalf("Retire() = %v", err)
	}
	if got := strings.Join(p.Codes(), ","); got != "beacon" {
		t.Errorf("applications = %s, want beacon alone", got)
	}
	if p.Registration != nil || p.OutOfService != nil || p.Hostnames != nil {
		t.Errorf("harbor's records stay: registration %v, outOfService %v, hostnames %v", p.Registration, p.OutOfService, p.Hostnames)
	}
	if err := p.Validate(); err != nil {
		t.Errorf("the placement without harbor is invalid: %v", err)
	}
	// The other application's records stay as they were.
	q := testPlacement(t)
	q.OutOfService = map[string][]string{"harbor": {"tst"}, "beacon": {"prd"}}
	if err := q.Retire("harbor"); err != nil {
		t.Fatalf("Retire() = %v", err)
	}
	if len(q.OutOfService) != 1 || len(q.OutOfService["beacon"]) != 1 {
		t.Errorf("beacon's out-of-service record changed: %v", q.OutOfService)
	}
	err := q.Retire("quill")
	if err == nil || !strings.Contains(err.Error(), "quill is not registered (applications in placement.json): nothing to retire") {
		t.Errorf("Retire() of an unregistered application = %v, want the refusal", err)
	}
}
