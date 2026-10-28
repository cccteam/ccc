//go:build skipAuth

package integration

import (
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
)

// TestSelfService_signInFirstThroughOpenSignUp proves the order the creation shape
// settles: a stranger signs in first, through the staff auth's google registration (any
// Google account, open sign-up: a name the auth has never seen becomes a user), holds
// nothing, and then names their tenant. Under the session library's skipAuth build tag
// Google is simulated: the login route sends the browser straight to the callback, and
// the callback signs in whoever APP_GOOGLE_USERNAME names. The test sets that variable,
// so it does not run in parallel.
func TestSelfService_signInFirstThroughOpenSignUp(t *testing.T) {
	ctx := t.Context()
	s := newServed(ctx, t)
	b := newBrowser(t, s)

	// The methods route offers the registration beside the password form.
	status, body := b.do(ctx, http.MethodGet, "/api/user/methods", nil)
	if status != http.StatusOK || !strings.Contains(string(body), `"google"`) {
		t.Fatalf("GET /api/user/methods: status %d, want 200 naming the google registration: %s", status, body)
	}

	// Signing in: the login route sends the browser to the simulated directory, which
	// is the callback.
	t.Setenv("APP_GOOGLE_USERNAME", "wanderer")
	status, location := b.redirect(ctx, "/api/user/google/login?returnUrl="+url.QueryEscape("/dashboard"))
	if status != http.StatusFound {
		t.Fatalf("GET /api/user/google/login: status %d, want %d to the directory", status, http.StatusFound)
	}
	// The account's Workspace is not one the deployment administers, so the callback
	// sends the browser to the login page's code step (pending=mfa), and the sign-in code
	// opens the session, going on to /dashboard.
	status, location = b.redirect(ctx, location)
	if status != http.StatusFound || location != "/login?pending=mfa&returnUrl="+url.QueryEscape("/dashboard") {
		t.Fatalf("the callback answered %d to %q, want %d to the login page's code step, returning to /dashboard", status, location, http.StatusFound)
	}
	b.secondFactor(ctx, "wanderer", false)
	status, body = b.do(ctx, http.MethodGet, "/api/user/session", nil)
	if status != http.StatusOK || !strings.Contains(string(body), `"wanderer"`) {
		t.Fatalf("GET /api/user/session after the sign-in: status %d: %s", status, body)
	}

	// Signed in and holding nothing, the stranger names their tenant and stands in it.
	if domains := userDomains(ctx, t, b); len(domains) != 0 {
		t.Fatalf("user-domains before = %v, want none", domains)
	}
	if status, body := b.do(ctx, http.MethodPost, "/api/create-tenant", []byte(`{"id":"wander","name":"Wander"}`)); status != http.StatusOK {
		t.Fatalf("POST /api/create-tenant: status %d: %s", status, body)
	}
	if domains := userDomains(ctx, t, b); !slices.Equal(domains, []string{"wander"}) {
		t.Errorf("user-domains after = %v, want [wander]", domains)
	}
	if status, body := b.do(ctx, http.MethodGet, "/api/tenants/wander/announcements", nil); status != http.StatusOK {
		t.Errorf("GET /api/tenants/wander/announcements: status %d, want 200: %s", status, body)
	}
}
