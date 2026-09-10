package integration

import (
	"encoding/json"
	"net/http"
	"slices"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
)

// TestLogin drives the served stack the way the console does: sign in, read the
// session, read the tenant list and a tenant's permission digest, and probe a tenant
// the login holds nothing in.
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
		// wantSouth is the status of a tenant-scoped request in south: concealed (not
		// found) for a login with no foothold there.
		wantSouth int
	}{
		{name: "the administrator signs in and sees both tenants", user: adminUser, password: adminPassword, wantLogin: http.StatusOK, wantDomains: []string{north, south}, wantSouth: http.StatusOK},
		{name: "the member sees only north; south is concealed", user: memberUser, password: adminPassword, wantLogin: http.StatusOK, wantDomains: []string{north}, wantSouth: http.StatusNotFound},
		{name: "a wrong password is refused", user: adminUser, password: "nope", wantLogin: http.StatusUnauthorized},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			b := newBrowser(t, s)

			// The first request primes the XSRF cookie the login route requires.
			if status, body := b.do(ctx, http.MethodGet, "/api/user/session", nil); status != http.StatusOK {
				t.Fatalf("GET /api/user/session before login: status %d: %s", status, body)
			}

			status, body := b.login(ctx, tt.user, tt.password)
			if status != tt.wantLogin {
				t.Fatalf("login status = %d, want %d: %s", status, tt.wantLogin, body)
			}
			if tt.wantLogin != http.StatusOK {
				if status, body := b.do(ctx, http.MethodGet, "/api/user-domains", nil); status != http.StatusUnauthorized {
					t.Errorf("GET /api/user-domains without a session: status %d, want 401: %s", status, body)
				}

				return
			}

			status, body = b.do(ctx, http.MethodGet, "/api/user-domains", nil)
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

			status, body = b.do(ctx, http.MethodGet, "/api/permission-digest?domain="+north, nil)
			if status != http.StatusOK {
				t.Fatalf("GET north digest: status %d: %s", status, body)
			}
			var digest accesstypes.PermissionDigest
			if err := json.Unmarshal(body, &digest); err != nil {
				t.Fatalf("digest is not a PermissionDigest: %v: %s", err, body)
			}

			// The tenant record is global: every signed-in login with a global grant
			// lists it, and the digest reports the Tenants resource.
			status, body = b.do(ctx, http.MethodGet, "/api/tenants", nil)
			if want := http.StatusOK; tt.user == adminUser && status != want {
				t.Errorf("GET /api/tenants as %s: status %d, want %d: %s", tt.user, status, want, body)
			}
			if want := http.StatusForbidden; tt.user == memberUser && status != want {
				t.Errorf("GET /api/tenants as %s: status %d, want %d: %s", tt.user, status, want, body)
			}

			// The tenant-scoped list answers in a tenant the login holds a grant in.
			status, body = b.do(ctx, http.MethodGet, "/api/tenants/"+north+"/announcements", nil)
			if status != http.StatusOK {
				t.Errorf("GET north announcements as %s: status %d, want 200: %s", tt.user, status, body)
			}

			// A tenant the login holds nothing in is concealed: a tenant-scoped request
			// answers not found, the same as a tenant that does not exist.
			status, body = b.do(ctx, http.MethodGet, "/api/tenants/"+south+"/announcements", nil)
			if status != tt.wantSouth {
				t.Errorf("GET south announcements as %s: status %d, want %d: %s", tt.user, status, tt.wantSouth, body)
			}
			status, body = b.do(ctx, http.MethodGet, "/api/tenants/nowhere/announcements", nil)
			if status != http.StatusNotFound {
				t.Errorf("GET announcements of an unknown tenant: status %d, want 404: %s", status, body)
			}
		})
	}
}
