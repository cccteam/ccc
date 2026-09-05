package integration

import (
	"net/http"
	"testing"
)

// TestPortalOutlet drives the portal the way its browser application does: sign in
// under the portal prefix, read the portal's tenant list and digest, list the portal's
// member resource, and confirm a console-only resource is not on the portal at all.
func TestPortalOutlet(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	s := newServed(ctx, t)
	b := newBrowser(t, s, portalAPI)

	if status, body := b.do(ctx, http.MethodGet, portalAPI+"/user/session", nil); status != http.StatusOK {
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
		{name: "the portal serves the session's tenants", target: portalAPI + "/user-domains", want: http.StatusOK},
		{name: "the portal serves the tenant digest", target: portalAPI + "/permission-digest?domain=" + north, want: http.StatusOK},
		{name: "announcements are a portal member", target: portalAPI + "/tenants/" + north + "/announcements", want: http.StatusOK},
		{name: "the tenant record is console-only", target: portalAPI + "/tenants", want: http.StatusNotFound},
		{name: "readings are machines-only", target: portalAPI + "/tenants/" + north + "/readings", want: http.StatusNotFound},
		{name: "a portal session does not open the console", target: consoleAPI + "/tenants/" + north + "/announcements", want: http.StatusOK},
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

// TestMachinesOutlet drives the machines outlet: the bearer key binds the request to
// the service identity, which then goes through the same tenancy and permission checks
// as a browser; without the key the surface is closed.
func TestMachinesOutlet(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	s := newServed(ctx, t)

	tests := []struct {
		name   string
		key    string
		target string
		want   int
	}{
		{name: "the key opens a tenant the service account holds a grant in", key: machinesAPIKey, target: "/machines/tenants/" + north + "/readings", want: http.StatusOK},
		{name: "a tenant without a foothold is concealed", key: machinesAPIKey, target: "/machines/tenants/" + south + "/readings", want: http.StatusNotFound},
		{name: "announcements are not on the machines outlet", key: machinesAPIKey, target: "/machines/tenants/" + north + "/announcements", want: http.StatusNotFound},
		{name: "a wrong key is refused", key: "not-the-key", target: "/machines/tenants/" + north + "/readings", want: http.StatusUnauthorized},
		{name: "no key is refused", key: "", target: "/machines/tenants/" + north + "/readings", want: http.StatusUnauthorized},
		{name: "readings are not on the console outlet", key: machinesAPIKey, target: consoleAPI + "/tenants/" + north + "/readings", want: http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			status, body := machine(ctx, t, s, tt.key, http.MethodGet, tt.target)
			if status != tt.want {
				t.Errorf("GET %s: status %d, want %d: %s", tt.target, status, tt.want, body)
			}
		})
	}
}
