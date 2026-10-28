package integration

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// portalPage is the browser page a portal login returns to.
const portalPage = "/portal/tracker"

// portalLogin is the portal's login page: where a refused login returns, with the reason as
// a code in the query and nothing else.
const portalLogin = "/portal/login"

// TestPortalOutlet drives the portal the way its browser application does: sign in
// through the directory under the portal prefix as a client whose groups name the
// client-portal role, read the portal's sector list and digest, confirm a console-only
// resource is not on the portal at all, and confirm the client's session is a stranger to
// the console.
//
// Demonstrates: GenerateRouter, outlet.session, outlet.isolation, auth.two-populations, outlet.tokens, machine-identity.
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

// TestPortalLoginNeedsARole proves the directory's authority and the shape of a refusal: a
// login whose groups name no role the members store knows is refused and returned to the
// portal's login page, and the Location is the login page with a query of exactly
// code=no_roles: the reason is a code the page maps to its own sentence, never text, so
// nothing a page could render arrives in the URL.
//
// Demonstrates: auth.directory-roles, auth.login-refusal-code.
func TestPortalLoginNeedsARole(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	s := newServed(ctx, t)
	b := newBrowser(t, s, portalAPI)

	status, location := b.loginPortal(ctx, "stranger", "not-a-role")
	if want := portalLogin + "?code=no_roles"; status != http.StatusFound || location != want {
		t.Fatalf("portal login with no known role: status %d to %q, want %d to %q", status, location, http.StatusFound, want)
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

	b.signIn(ctx, "governor")

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

// TestRootRedirect drives the served root with two browser applications on one router
// tree: neither is mounted at /, so the root alone answers a temporary redirect to the
// console, the default outlet's application at /console/, while an unmatched path at the
// root is not found and an unknown path under the console's API prefix is not found
// rather than the console's entry document. These are a browser's first requests, before
// any session.
//
// Demonstrates: WebApp.several.
func TestRootRedirect(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	s := newServed(ctx, t)
	b := newBrowser(t, s, consoleAPI)

	tests := []struct {
		name         string
		target       string
		wantStatus   int
		wantLocation string
	}{
		{name: "the root alone redirects to the console's application", target: "/", wantStatus: http.StatusTemporaryRedirect, wantLocation: "/console/"},
		{name: "an unmatched path at the root is not found", target: "/nowhere", wantStatus: http.StatusNotFound},
		{name: "an unknown path under the console's API prefix is not found", target: consoleAPI + "/nowhere", wantStatus: http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			status, location := b.redirect(ctx, tt.target)
			if status != tt.wantStatus || location != tt.wantLocation {
				t.Errorf("GET %s: status %d to %q, want %d to %q", tt.target, status, location, tt.wantStatus, tt.wantLocation)
			}
		})
	}
}

// deviceCodeGrant is the device code grant type (RFC 8628).
const deviceCodeGrant = "urn:ietf:params:oauth:grant-type:device_code"

// TestDroidsTokens drives the droids outlet's token check and the ways a token reaches
// it. The droid service account's configured key, seeded as its application token,
// opens the droids' routes; a request with no token, or with one the crew auth never
// issued, is refused with the challenge naming the outlet. A crew member issues an
// application token of their own on the console, which acts as them on the droids
// outlet, is listed as used, and dies with its revocation. A command-line client obtains
// a device grant with a crew member's approval, refreshes it, and loses the rotated
// access token.
//
// Demonstrates: outlet.tokens.
func TestDroidsTokens(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	s := newServed(ctx, t)
	reports := "/droids/sectors/" + anvil + "/droid-reports"

	t.Run("the seeded key opens the droids' routes and anything else is challenged", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name          string
			token         string
			want          int
			wantChallenge string
		}{
			{name: "the droid service account's configured key", token: droidsAPIKey, want: http.StatusOK},
			{name: "no token", token: "", want: http.StatusUnauthorized, wantChallenge: `Bearer realm="droids"`},
			{name: "a token the crew auth never issued", token: "not-a-token", want: http.StatusUnauthorized, wantChallenge: `Bearer realm="droids", error="invalid_token"`},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				resp := droidResponse(ctx, t, s, tt.token, http.MethodGet, reports, nil)
				defer resp.Body.Close()
				body, _ := io.ReadAll(resp.Body)
				if resp.StatusCode != tt.want {
					t.Errorf("GET %s: status %d, want %d: %s", reports, resp.StatusCode, tt.want, body)
				}
				if got := resp.Header.Get("WWW-Authenticate"); got != tt.wantChallenge {
					t.Errorf("GET %s: WWW-Authenticate %q, want %q", reports, got, tt.wantChallenge)
				}
			})
		}
	})

	t.Run("an application token a crew member issues acts as them until they revoke it", func(t *testing.T) {
		t.Parallel()

		b := newBrowser(t, s, consoleAPI)
		b.signIn(ctx, "governor")
		status, body := b.do(ctx, http.MethodPost, consoleAPI+"/user/tokens", []byte(`{"name":"survey script","outlet":"droids"}`))
		if status != http.StatusOK {
			t.Fatalf("POST user/tokens: status %d: %s", status, body)
		}
		var issued struct {
			ID     string `json:"id"`
			Token  string `json:"token"`
			Outlet string `json:"outlet"`
		}
		if err := json.Unmarshal(body, &issued); err != nil {
			t.Fatalf("decoding the issued token: %v: %s", err, body)
		}
		if issued.Token == "" || issued.Outlet != "droids" {
			t.Fatalf("issued token = %+v, want a secret for the droids outlet", issued)
		}

		// The token passes the check and acts as the governor: the features digest
		// answers any token, and the droids' reports, which only the droid service
		// account's role lists, are refused to the governor for want of the grant, as
		// they would be on the console, rather than challenged as a stranger's request is.
		if status, body := droid(ctx, t, s, issued.Token, http.MethodGet, "/droids/features", nil); status != http.StatusOK {
			t.Errorf("GET /droids/features with the governor's token: status %d, want %d: %s", status, http.StatusOK, body)
		}
		if status, body := droid(ctx, t, s, issued.Token, http.MethodGet, reports, nil); status != http.StatusForbidden {
			t.Errorf("GET %s with the governor's token: status %d, want %d (the governor holds no droid grant): %s", reports, status, http.StatusForbidden, body)
		}

		status, body = b.do(ctx, http.MethodGet, consoleAPI+"/user/tokens", nil)
		if status != http.StatusOK {
			t.Fatalf("GET user/tokens: status %d: %s", status, body)
		}
		var listed []struct {
			ID         string  `json:"id"`
			Kind       string  `json:"kind"`
			Name       string  `json:"name"`
			Outlet     string  `json:"outlet"`
			LastUsedAt *string `json:"lastUsedAt"`
		}
		if err := json.Unmarshal(body, &listed); err != nil {
			t.Fatalf("decoding the token list: %v: %s", err, body)
		}
		found := false
		for _, tok := range listed {
			if tok.ID != issued.ID {
				continue
			}
			found = true
			if tok.Kind != "application" || tok.Name != "survey script" || tok.Outlet != "droids" || tok.LastUsedAt == nil {
				t.Errorf("listed token = %+v, want an application token named survey script for droids, used", tok)
			}
		}
		if !found {
			t.Fatalf("the issued token %s is not in the governor's list: %s", issued.ID, body)
		}

		if status, body := b.do(ctx, http.MethodPost, consoleAPI+"/user/tokens/revoke", []byte(`{"id":"`+issued.ID+`"}`)); status != http.StatusOK {
			t.Fatalf("POST user/tokens/revoke: status %d: %s", status, body)
		}
		if status, body := droid(ctx, t, s, issued.Token, http.MethodGet, "/droids/features", nil); status != http.StatusUnauthorized {
			t.Errorf("GET /droids/features with the revoked token: status %d, want %d: %s", status, http.StatusUnauthorized, body)
		}
	})

	t.Run("a command-line client obtains a device grant with a crew member's approval", func(t *testing.T) {
		t.Parallel()

		status, body := droid(ctx, t, s, "", http.MethodPost, "/droids/oauth/device/authorize", []byte(`{"client_id":"corral","outlet":"droids"}`))
		if status != http.StatusOK {
			t.Fatalf("POST oauth/device/authorize: status %d: %s", status, body)
		}
		// The device flow's answers are the RFC's, snake_case: read as maps.
		var started map[string]any
		if err := json.Unmarshal(body, &started); err != nil {
			t.Fatalf("decoding the device authorization: %v: %s", err, body)
		}
		deviceCode, _ := started["device_code"].(string)
		userCode, _ := started["user_code"].(string)
		if deviceCode == "" || userCode == "" {
			t.Fatalf("device authorization = %s, want a device code and a user code", body)
		}
		// The client is sent to the console's device page (crew.Settings.DevicePage), with
		// the code the person types already in the query.
		if got, _ := started["verification_uri"].(string); got != "/console/device" {
			t.Errorf("verification_uri = %q, want the console's device page /console/device", got)
		}
		if got, _ := started["verification_uri_complete"].(string); got != "/console/device?userCode="+userCode {
			t.Errorf("verification_uri_complete = %q, want /console/device?userCode=%s", got, userCode)
		}

		// The marshal reads what waits under the code and approves it on the console.
		b := newBrowser(t, s, consoleAPI)
		b.signIn(ctx, "marshal")
		status, body = b.do(ctx, http.MethodGet, consoleAPI+"/user/device?userCode="+url.QueryEscape(userCode), nil)
		if status != http.StatusOK {
			t.Fatalf("GET user/device: status %d: %s", status, body)
		}
		var pending struct {
			ClientID string `json:"clientId"`
			Outlet   string `json:"outlet"`
		}
		if err := json.Unmarshal(body, &pending); err != nil {
			t.Fatalf("decoding the pending authorization: %v: %s", err, body)
		}
		if pending.ClientID != "corral" || pending.Outlet != "droids" {
			t.Errorf("pending authorization = %+v, want the corral client for droids", pending)
		}
		if status, body := b.do(ctx, http.MethodPost, consoleAPI+"/user/device/approve", []byte(`{"userCode":"`+userCode+`"}`)); status != http.StatusOK {
			t.Fatalf("POST user/device/approve: status %d: %s", status, body)
		}

		// The client's poll redeems the code for a pair that acts as the marshal.
		pair := redeem(ctx, t, s, `{"grant_type":"`+deviceCodeGrant+`","device_code":"`+deviceCode+`"}`)
		if status, body := droid(ctx, t, s, pair.access, http.MethodGet, "/droids/features", nil); status != http.StatusOK {
			t.Errorf("GET /droids/features with the access token: status %d, want %d: %s", status, http.StatusOK, body)
		}

		// A refresh exchange rotates the pair: the old access token is dead with it.
		next := redeem(ctx, t, s, `{"grant_type":"refresh_token","refresh_token":"`+pair.refresh+`"}`)
		if status, body := droid(ctx, t, s, pair.access, http.MethodGet, "/droids/features", nil); status != http.StatusUnauthorized {
			t.Errorf("GET /droids/features with the rotated access token: status %d, want %d: %s", status, http.StatusUnauthorized, body)
		}
		if status, body := droid(ctx, t, s, next.access, http.MethodGet, "/droids/features", nil); status != http.StatusOK {
			t.Errorf("GET /droids/features with the next access token: status %d, want %d: %s", status, http.StatusOK, body)
		}
	})
}

// tokenPair is what the token endpoint answers a grant with: the access token and the
// refresh token (the RFC's access_token and refresh_token).
type tokenPair struct {
	access  string
	refresh string
}

// redeem posts a grant to the droids outlet's token endpoint and returns the pair.
func redeem(ctx context.Context, t *testing.T, s *served, grant string) tokenPair {
	t.Helper()

	status, body := droid(ctx, t, s, "", http.MethodPost, "/droids/oauth/token", []byte(grant))
	if status != http.StatusOK {
		t.Fatalf("POST oauth/token: status %d: %s", status, body)
	}
	var answer map[string]any
	if err := json.Unmarshal(body, &answer); err != nil {
		t.Fatalf("decoding the token pair: %v: %s", err, body)
	}
	pair := tokenPair{}
	pair.access, _ = answer["access_token"].(string)
	pair.refresh, _ = answer["refresh_token"].(string)
	kind, _ := answer["token_type"].(string)
	if pair.access == "" || pair.refresh == "" || !strings.EqualFold(kind, "Bearer") {
		t.Fatalf("token pair = %s, want a bearer pair", body)
	}

	return pair
}
