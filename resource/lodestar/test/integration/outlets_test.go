package integration

import (
	"encoding/json"
	"net/http"
	"testing"
)

// portalPage is the browser page a portal login returns to.
const portalPage = "/portal/tracker"

// TestPortalOutlet drives the portal the way its browser application does: sign in
// through the directory under the portal prefix as a client whose groups name the
// client-portal role, read the portal's sector list and digest, confirm a console-only
// resource is not on the portal at all, and confirm the client's session is a stranger to
// the console.
//
// Demonstrates: outlet.session, outlet.isolation, auth.two-populations, outlet.api-key, machine-identity.
func TestPortalOutlet(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	s := newServed(ctx, t)
	b := newBrowser(t, s, portalAPI)

	if status, body := b.do(ctx, http.MethodGet, portalAPI+"/user/session", nil); status != http.StatusOK {
		t.Fatalf("GET session before login: status %d: %s", status, body)
	}
	if status, location := b.loginPortal(ctx, clientUser, "client-portal"); status != http.StatusFound || location != portalPage {
		t.Fatalf("portal login: status %d to %q, want %d to %q", status, location, http.StatusFound, portalPage)
	}
	status, body := b.do(ctx, http.MethodGet, portalAPI+"/user/session", nil)
	if status != http.StatusOK {
		t.Fatalf("GET session after login: status %d: %s", status, body)
	}
	var sess struct {
		Authenticated bool   `json:"authenticated"`
		Username      string `json:"username"`
	}
	if err := json.Unmarshal(body, &sess); err != nil {
		t.Fatal(err)
	}
	if !sess.Authenticated || sess.Username != clientUser {
		t.Fatalf("session after login = %+v, want authenticated as %s", sess, clientUser)
	}

	tests := []struct {
		name   string
		target string
		want   int
	}{
		{name: "the portal serves the session's sectors", target: portalAPI + "/user-domains", want: http.StatusOK},
		{name: "the portal serves the sector digest", target: portalAPI + "/permission-digest?domain=" + anvil, want: http.StatusOK},
		{name: "the sector record is console-only", target: portalAPI + "/sectors", want: http.StatusNotFound},
		{name: "wings are console-only", target: portalAPI + "/sectors/" + anvil + "/wings", want: http.StatusNotFound},
		{name: "a client's session is a stranger to the console", target: consoleAPI + "/sectors/" + anvil + "/wings", want: http.StatusUnauthorized},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			status, body := b.do(ctx, http.MethodGet, tt.target, nil)
			if status != tt.want {
				t.Errorf("GET %s: status %d, want %d: %s", tt.target, status, tt.want, body)
			}
		})
	}
}

// TestPortalLoginNeedsARole proves the directory's authority: a login whose groups name no
// role the members store knows is refused and returned to the portal's login page with
// the reason.
//
// Demonstrates: auth.directory-roles.
func TestPortalLoginNeedsARole(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	s := newServed(ctx, t)
	b := newBrowser(t, s, portalAPI)

	status, location := b.loginPortal(ctx, "stranger", "not-a-role")
	if status != http.StatusFound || location == portalPage {
		t.Fatalf("portal login with no known role: status %d to %q, want a redirect back to the login page", status, location)
	}
	if status, body := b.do(ctx, http.MethodGet, portalAPI+"/user-domains", nil); status != http.StatusUnauthorized {
		t.Errorf("GET user-domains after a refused login: status %d, want 401: %s", status, body)
	}
}

// TestConsoleSessionOnPortal signs a crew login in on the console and confirms its
// session opens nothing on the portal: the two outlets bind to two auths.
//
// Demonstrates: auth.two-populations.
func TestConsoleSessionOnPortal(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	s := newServed(ctx, t)
	b := newBrowser(t, s, consoleAPI)

	if status, body := b.do(ctx, http.MethodGet, consoleAPI+"/user/session", nil); status != http.StatusOK {
		t.Fatalf("GET session before login: status %d: %s", status, body)
	}
	if status, body := b.login(ctx, "governor", personaPassword); status != http.StatusOK {
		t.Fatalf("console login: status %d: %s", status, body)
	}

	tests := []struct {
		name   string
		target string
		want   int
	}{
		{name: "the console serves the crew login", target: consoleAPI + "/sectors/" + anvil + "/wings", want: http.StatusOK},
		{name: "the crew session is a stranger to the portal", target: portalAPI + "/user-domains", want: http.StatusUnauthorized},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			status, body := b.do(ctx, http.MethodGet, tt.target, nil)
			if status != tt.want {
				t.Errorf("GET %s: status %d, want %d: %s", tt.target, status, tt.want, body)
			}
		})
	}
}

// TestDroidsOutlet drives the droids outlet's isolation: nothing that is not a droids
// member is served under its prefix. The key's own refusals need a member route to reach
// the middleware and are proven in droids_test once the telemetry resource exists.
func TestDroidsOutlet(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	s := newServed(ctx, t)

	tests := []struct {
		name   string
		key    string
		target string
		want   int
	}{
		{name: "wings are not on the droids outlet", key: droidsAPIKey, target: "/droids/sectors/" + anvil + "/wings", want: http.StatusNotFound},
		{name: "the sector record is not on the droids outlet", key: droidsAPIKey, target: "/droids/sectors", want: http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			status, body := droid(ctx, t, s, tt.key, http.MethodGet, tt.target, nil)
			if status != tt.want {
				t.Errorf("GET %s: status %d, want %d: %s", tt.target, status, tt.want, body)
			}
		})
	}
}
