package integration

import (
	"encoding/json"
	"net/http"
	"slices"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
)

// TestLogin drives the served stack the way the console does: sign in, read the sector
// list and a sector's permission digest, and probe a sector the login holds nothing in.
//
// Demonstrates: tenancy.concealed, tenancy.tenant-record, auth.password.
func TestLogin(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	s := newServed(ctx, t)

	tests := []struct {
		name        string
		user        string
		password    string
		wantLogin   int
		wantDomains []string
		// wantBastion is the status of a sector-scoped request in Bastion: concealed
		// (not found) for a login with no foothold there.
		wantBastion int
		// wantSectors is the status of the global sector list.
		wantSectors int
		// wantWings is the status of the Anvil wings list; zero means OK.
		wantWings int
	}{
		{name: "the governor signs in and sees every sector", user: "governor", password: personaPassword, wantLogin: http.StatusOK, wantDomains: []string{anvil, bastion, cinder}, wantBastion: http.StatusOK, wantSectors: http.StatusOK},
		{name: "the marshal sees only Anvil; Bastion is concealed", user: "marshal", password: personaPassword, wantLogin: http.StatusOK, wantDomains: []string{anvil}, wantBastion: http.StatusNotFound, wantSectors: http.StatusOK},
		{name: "the archivist reads every sector but cannot list the wings", user: "archivist", password: personaPassword, wantLogin: http.StatusOK, wantDomains: []string{anvil, bastion, cinder}, wantBastion: http.StatusForbidden, wantSectors: http.StatusOK, wantWings: http.StatusForbidden},
		{name: "a wrong password is refused", user: "governor", password: "nope", wantLogin: http.StatusUnauthorized},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			b := newBrowser(t, s, consoleAPI)

			// The first request primes the XSRF cookie the login route requires.
			if status, body := b.do(ctx, http.MethodGet, consoleAPI+"/user/session", nil); status != http.StatusOK {
				t.Fatalf("GET /api/user/session before login: status %d: %s", status, body)
			}

			status, body := b.login(ctx, tt.user, tt.password)
			if status != tt.wantLogin {
				t.Fatalf("login status = %d, want %d: %s", status, tt.wantLogin, body)
			}
			if tt.wantLogin != http.StatusOK {
				if status, body := b.do(ctx, http.MethodGet, consoleAPI+"/user-domains", nil); status != http.StatusUnauthorized {
					t.Errorf("GET /api/user-domains without a session: status %d, want 401: %s", status, body)
				}

				return
			}

			status, body = b.do(ctx, http.MethodGet, consoleAPI+"/user-domains", nil)
			if status != http.StatusOK {
				t.Fatalf("GET /api/user-domains: status %d: %s", status, body)
			}
			var domains []string
			if err := json.Unmarshal(body, &domains); err != nil {
				t.Fatalf("user-domains is not a list: %v: %s", err, body)
			}
			if !slices.Equal(domains, tt.wantDomains) {
				t.Errorf("user-domains = %v, want %v", domains, tt.wantDomains)
			}

			status, body = b.do(ctx, http.MethodGet, consoleAPI+"/permission-digest?domain="+anvil, nil)
			if status != http.StatusOK {
				t.Fatalf("GET anvil digest: status %d: %s", status, body)
			}
			var digest accesstypes.PermissionDigest
			if err := json.Unmarshal(body, &digest); err != nil {
				t.Fatalf("digest is not a PermissionDigest: %v: %s", err, body)
			}

			// The sector record is global: only a global grant lists it.
			if status, body := b.do(ctx, http.MethodGet, consoleAPI+"/sectors", nil); status != tt.wantSectors {
				t.Errorf("GET /api/sectors as %s: status %d, want %d: %s", tt.user, status, tt.wantSectors, body)
			}

			// The sector-scoped list answers in a sector the login holds a grant in; a
			// login with a foothold but no grant on the wings is refused, not concealed.
			wantWings := tt.wantWings
			if wantWings == 0 {
				wantWings = http.StatusOK
			}
			if status, body := b.do(ctx, http.MethodGet, consoleAPI+"/sectors/"+anvil+"/wings", nil); status != wantWings {
				t.Errorf("GET anvil wings as %s: status %d, want %d: %s", tt.user, status, wantWings, body)
			}

			// A sector the login holds nothing in is concealed: a sector-scoped request
			// answers not found, the same as a sector that does not exist.
			if status, body := b.do(ctx, http.MethodGet, consoleAPI+"/sectors/"+bastion+"/wings", nil); status != tt.wantBastion {
				t.Errorf("GET bastion wings as %s: status %d, want %d: %s", tt.user, status, tt.wantBastion, body)
			}
			if status, body := b.do(ctx, http.MethodGet, consoleAPI+"/sectors/nowhere/wings", nil); status != http.StatusNotFound {
				t.Errorf("GET wings of an unknown sector: status %d, want 404: %s", status, body)
			}
		})
	}
}
