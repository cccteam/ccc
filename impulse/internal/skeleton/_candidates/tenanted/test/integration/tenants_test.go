package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"testing"
)

// TestTenants_createdThroughTheAPIAreUsableAtOnce proves the tenant roster follows the
// Tenant record's write paths without a restart: a tenant the administrator creates
// through the consolidated handler is served under its segment at once (the generated
// guard asks the roster, which the commit updated), named in the administrator's tenant
// list (a domain role held in every domain is a foothold there too), concealed from the
// member who holds nothing there, and gone from the segment the moment it is deleted.
func TestTenants_createdThroughTheAPIAreUsableAtOnce(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	s := newServed(ctx, t)
	admin := signedIn(ctx, t, s, adminUser)
	member := signedIn(ctx, t, s, memberUser)

	const (
		east         = "east"
		eastSegment  = "/api/tenants/" + east + "/announcements"
		chartEast    = `[{"op":"add","path":"/tenants/` + east + `","value":{"name":"East"}}]`
		dissolveEast = `[{"op":"remove","path":"/tenants/` + east + `"}]`
	)

	// Before: the segment answers not-found for a tenant that does not exist.
	if status, body := admin.do(ctx, http.MethodGet, eastSegment, nil); status != http.StatusNotFound {
		t.Fatalf("GET %s before the tenant exists: status %d, want 404: %s", eastSegment, status, body)
	}
	if status, body := admin.do(ctx, http.MethodPatch, "/api/resources", []byte(chartEast)); status != http.StatusOK {
		t.Fatalf("creating the tenant: status %d: %s", status, body)
	}

	// The reads happen while the tenant exists, so they are rows of one test rather than
	// subtests, which would run after the delete below.
	reads := []struct {
		name       string
		browser    *browser
		wantStatus int
	}{
		{name: "the administrator reads the new tenant's segment at once", browser: admin, wantStatus: http.StatusOK},
		{name: "the member holds nothing there and is answered not-found", browser: member, wantStatus: http.StatusNotFound},
	}
	for _, read := range reads {
		if status, body := read.browser.do(ctx, http.MethodGet, eastSegment, nil); status != read.wantStatus {
			t.Errorf("%s: GET %s: status %d, want %d: %s", read.name, eastSegment, status, read.wantStatus, body)
		}
	}

	// The administrator's tenant list names the new tenant: the roster lists it, and the
	// domain role held in every domain is a foothold in it.
	status, body := admin.do(ctx, http.MethodGet, "/api/user-domains", nil)
	if status != http.StatusOK {
		t.Fatalf("GET /api/user-domains: status %d: %s", status, body)
	}
	var domains []string
	if err := json.Unmarshal(body, &domains); err != nil {
		t.Fatalf("user-domains is not a list: %v: %s", err, body)
	}
	if !slices.Contains(domains, east) {
		t.Errorf("user-domains = %v, want %s among them", domains, east)
	}

	// Deleted, the segment answers not-found again at once.
	if status, body := admin.do(ctx, http.MethodPatch, "/api/resources", []byte(dissolveEast)); status != http.StatusOK {
		t.Fatalf("deleting the tenant: status %d: %s", status, body)
	}
	if status, body := admin.do(ctx, http.MethodGet, eastSegment, nil); status != http.StatusNotFound {
		t.Errorf("GET %s after the tenant is deleted: status %d, want 404: %s", eastSegment, status, body)
	}
}

// signedIn returns a browser signed in as the development login: the first request
// primes the XSRF cookie the login route requires.
func signedIn(ctx context.Context, t *testing.T, s *served, user string) *browser {
	t.Helper()

	b := newBrowser(t, s)
	if status, body := b.do(ctx, http.MethodGet, "/api/user/session", nil); status != http.StatusOK {
		t.Fatalf("GET /api/user/session before login as %s: status %d: %s", user, status, body)
	}
	if status, body := b.login(ctx, user, adminPassword); status != http.StatusOK {
		t.Fatalf("login as %s: status %d: %s", user, status, body)
	}

	return b
}
