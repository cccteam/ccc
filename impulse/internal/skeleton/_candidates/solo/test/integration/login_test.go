package integration

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
)

// TestLogin drives the served stack the way the console does: sign in, read the
// session, read the permission digest, sign out.
func TestLogin(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	s := newServed(ctx, t)

	tests := []struct {
		name       string
		user       string
		password   string
		wantLogin  int
		wantDigest int
	}{
		{name: "the development login signs in and reads its digest", user: adminUser, password: adminPassword, wantLogin: http.StatusOK, wantDigest: http.StatusOK},
		{name: "a wrong password is refused and the digest stays closed", user: adminUser, password: "nope", wantLogin: http.StatusUnauthorized, wantDigest: http.StatusUnauthorized},
		{name: "an unknown login is refused", user: "nobody", password: adminPassword, wantLogin: http.StatusUnauthorized, wantDigest: http.StatusUnauthorized},
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

			status, body = b.do(ctx, http.MethodGet, "/api/user/session", nil)
			if status != http.StatusOK {
				t.Fatalf("GET /api/user/session: status %d: %s", status, body)
			}
			var sess struct {
				Authenticated bool   `json:"authenticated"`
				Username      string `json:"username"`
			}
			if err := json.Unmarshal(body, &sess); err != nil {
				t.Fatal(err)
			}
			if sess.Authenticated != (tt.wantLogin == http.StatusOK) {
				t.Errorf("authenticated = %v, want %v", sess.Authenticated, tt.wantLogin == http.StatusOK)
			}

			status, body = b.do(ctx, http.MethodGet, "/api/permission-digest", nil)
			if status != tt.wantDigest {
				t.Fatalf("GET /api/permission-digest: status %d, want %d: %s", status, tt.wantDigest, body)
			}
			if status == http.StatusOK {
				var digest accesstypes.PermissionDigest
				if err := json.Unmarshal(body, &digest); err != nil {
					t.Fatalf("digest is not a PermissionDigest: %v: %s", err, body)
				}
			}
		})
	}
}
