package integration

import (
	"net/http"
	"testing"
)

// TestPortalSite drives the portal site: its own server over the same database and
// policy store. The portal's login is a portal-only identity; the tenant record is
// served by the console site alone, so the portal has no route for it.
func TestPortalSite(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	s := newServed(ctx, t)
	b := newBrowser(t, s.portal)

	if status, body := b.do(ctx, http.MethodGet, "/api/user/session", nil); status != http.StatusOK {
		t.Fatalf("GET session before login: status %d: %s", status, body)
	}
	if status, body := b.login(ctx, clientUser, adminPassword); status != http.StatusOK {
		t.Fatalf("portal login: status %d: %s", status, body)
	}

	tests := []struct {
		name   string
		target string
		want   int
	}{
		{name: "the portal serves the session's tenants", target: "/api/user-domains", want: http.StatusOK},
		{name: "the portal serves the tenant digest", target: "/api/permission-digest?domain=" + north, want: http.StatusOK},
		{name: "announcements are served by the portal", target: "/api/tenants/" + north + "/announcements", want: http.StatusOK},
		{name: "a tenant without a foothold is concealed", target: "/api/tenants/" + south + "/announcements", want: http.StatusNotFound},
		{name: "the tenant record is the console site's alone", target: "/api/tenants", want: http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, body := b.do(ctx, http.MethodGet, tt.target, nil)
			if status != tt.want {
				t.Errorf("GET %s: status %d, want %d: %s", tt.target, status, tt.want, body)
			}
		})
	}
}
