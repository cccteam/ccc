package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"testing"

	"github.com/cccteam/session"
)

// TestSelfService_aSignedInStrangerCreatesTheirTenant proves the self-service half of the
// Tenant record's creation shape (@tenant(creation: both)): a person who signs in holding
// no tenant creates one through the generated create-your-tenant route and stands in it
// at once, seated as its administrator in the same call; whoever already holds a tenant
// is refused, the administrator included, since an administrator creates tenants through
// the record's own create path (tenants_test.go) and never here; and the body is held to
// the record, so a refused body writes nothing.
func TestSelfService_aSignedInStrangerCreatesTheirTenant(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	s := newServed(ctx, t)
	newcomer := signedIn(ctx, t, s, createLogin(ctx, t, s, "newcomer"))
	another := signedIn(ctx, t, s, createLogin(ctx, t, s, "another"))
	admin := signedIn(ctx, t, s, adminUser)
	member := signedIn(ctx, t, s, memberUser)

	const (
		west        = "west"
		westSegment = "/api/tenants/" + west + "/announcements"
	)

	// Before: the newcomer holds nothing, and the tenant does not exist.
	if domains := userDomains(ctx, t, newcomer); len(domains) != 0 {
		t.Fatalf("user-domains before = %v, want none", domains)
	}
	if status, body := admin.do(ctx, http.MethodGet, westSegment, nil); status != http.StatusNotFound {
		t.Fatalf("GET %s before the tenant exists: status %d, want 404: %s", westSegment, status, body)
	}

	// The newcomer names their tenant and is answered its key.
	status, body := newcomer.do(ctx, http.MethodPost, "/api/create-tenant", []byte(`{"id":"`+west+`","name":"West"}`))
	if status != http.StatusOK {
		t.Fatalf("POST /api/create-tenant: status %d: %s", status, body)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &created); err != nil || created.ID != west {
		t.Fatalf("POST /api/create-tenant answered %s (err %v), want the key %q", body, err, west)
	}

	// At once: the newcomer stands in the tenant (the seat is written and the engine
	// answers it before the route answers), its segment serves them, and the
	// administrator, holding the domain role in every tenant, sees it too.
	if domains := userDomains(ctx, t, newcomer); !slices.Equal(domains, []string{west}) {
		t.Errorf("user-domains after = %v, want [%s]", domains, west)
	}
	reads := []struct {
		name       string
		browser    *browser
		wantStatus int
	}{
		{name: "the newcomer reads their tenant's segment", browser: newcomer, wantStatus: http.StatusOK},
		{name: "the administrator reads it", browser: admin, wantStatus: http.StatusOK},
		{name: "the member holds nothing there and is answered not-found", browser: member, wantStatus: http.StatusNotFound},
	}
	for _, read := range reads {
		if status, body := read.browser.do(ctx, http.MethodGet, westSegment, nil); status != read.wantStatus {
			t.Errorf("%s: GET %s: status %d, want %d: %s", read.name, westSegment, status, read.wantStatus, body)
		}
	}

	// Refused: one tenant per person, and only a person holding none creates one.
	refusals := []struct {
		name       string
		browser    *browser
		body       string
		wantStatus int
	}{
		{name: "the newcomer, holding a tenant now, is refused a second", browser: newcomer, body: `{"id":"west-again","name":"West again"}`, wantStatus: http.StatusForbidden},
		{name: "the member of a tenant is refused", browser: member, body: `{"id":"east","name":"East"}`, wantStatus: http.StatusForbidden},
		{name: "the administrator is refused: an administrator creates tenants through the record, never here", browser: admin, body: `{"id":"east","name":"East"}`, wantStatus: http.StatusForbidden},
		{name: "a body without the name is refused", browser: another, body: `{"id":"east"}`, wantStatus: http.StatusBadRequest},
		{name: "a body without the key is refused", browser: another, body: `{"name":"East"}`, wantStatus: http.StatusBadRequest},
		{name: "a key a tenant holds already is refused", browser: another, body: `{"id":"` + north + `","name":"North"}`, wantStatus: http.StatusConflict},
	}
	for _, refusal := range refusals {
		if status, body := refusal.browser.do(ctx, http.MethodPost, "/api/create-tenant", []byte(refusal.body)); status != refusal.wantStatus {
			t.Errorf("%s: POST /api/create-tenant %s: status %d, want %d: %s", refusal.name, refusal.body, status, refusal.wantStatus, body)
		}
	}
	// None of the refused bodies wrote a row, and the other stranger still holds nothing.
	if status, body := admin.do(ctx, http.MethodGet, "/api/tenants/east/announcements", nil); status != http.StatusNotFound {
		t.Errorf("GET /api/tenants/east/announcements after the refusals: status %d, want 404: %s", status, body)
	}
	if domains := userDomains(ctx, t, another); len(domains) != 0 {
		t.Errorf("user-domains of the other stranger = %v, want none", domains)
	}
}

// createLogin creates a password login holding no role anywhere: a stranger to every
// tenant, with the development password.
func createLogin(ctx context.Context, t *testing.T, s *served, user string) string {
	t.Helper()

	password := adminPassword
	if _, err := s.auth.Session().API().CreateSessionUser(ctx, &session.CreateUserRequest{Username: user, Password: &password}); err != nil {
		t.Fatalf("CreateSessionUser(%s) error = %v", user, err)
	}

	return user
}

// userDomains reads the browser's tenant list from the generated user-domains route.
func userDomains(ctx context.Context, t *testing.T, b *browser) []string {
	t.Helper()

	status, body := b.do(ctx, http.MethodGet, "/api/user-domains", nil)
	if status != http.StatusOK {
		t.Fatalf("GET /api/user-domains: status %d: %s", status, body)
	}
	var domains []string
	if err := json.Unmarshal(body, &domains); err != nil {
		t.Fatalf("user-domains is not a list: %v: %s", err, body)
	}

	return domains
}
