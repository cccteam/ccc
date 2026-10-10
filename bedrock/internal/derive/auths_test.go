package derive

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cccteam/ccc/impulse/app"
)

// TestCallbackRoute finds the OIDC callback in the generated router whichever receiver
// registers it: the router generated before the body-limit step registers the session
// routes on the group itself (r.Get), and the router generated since registers them on
// the bounded group (bounded.Get, or bounded.Post for a POST callback). A router that
// registers no callback, as a password auth's does, is refused.
func TestCallbackRoute(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		source  string
		want    Route
		wantErr string
	}{
		{
			name: "the session routes on the group itself, before the body-limit step",
			source: `package router

func routes(r chi.Router, h *session.Handlers) {
	r.Get("/api/user/login", h.Login())
	r.Get("/api/user/callback", h.CallbackOIDC())
	r.Get("/api/user/session", h.Authenticated())
	r.Delete("/api/user/session", h.Logout())
}
`,
			want: Route{Method: http.MethodGet, Path: "/api/user/callback", File: "pkg/router/zz_gen_router.go"},
		},
		{
			name: "the session routes on the bounded group, since the body-limit step",
			source: `package router

func routes(r chi.Router, h *session.Handlers) {
	// The session routes are bounded at BodyLimit, as the generated routes are.
	bounded := r.With(resource.BodyLimit(BodyLimit))
	bounded.Get("/api/user/login", h.Login())
	bounded.Get("/api/user/callback", h.CallbackOIDC())
	bounded.Get("/api/user/session", h.Authenticated())
	bounded.Delete("/api/user/session", h.Logout())
}
`,
			want: Route{Method: http.MethodGet, Path: "/api/user/callback", File: "pkg/router/zz_gen_router.go"},
		},
		{
			name: "a POST callback on the bounded group",
			source: `package router

func routes(r chi.Router, h *session.Handlers) {
	bounded := r.With(resource.BodyLimit(BodyLimit))
	bounded.Get("/api/user/login", h.Login())
	bounded.Post("/api/user/callback", h.CallbackOIDC())
	bounded.Get("/api/user/session", h.Authenticated())
}
`,
			want: Route{Method: http.MethodPost, Path: "/api/user/callback", File: "pkg/router/zz_gen_router.go"},
		},
		{
			name: "no callback registered, a password auth's router",
			source: `package router

func routes(r chi.Router, h *session.Handlers) {
	bounded := r.With(resource.BodyLimit(BodyLimit))
	bounded.Post("/api/user/login", h.Login())
	bounded.Get("/api/user/session", h.Authenticated())
	bounded.Delete("/api/user/session", h.Logout())
}
`,
			wantErr: "no file under pkg/router registers the OIDC callback (h.CallbackOIDC)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			routerDir := filepath.Join(dir, "pkg", "router")
			if err := os.MkdirAll(routerDir, 0o750); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(routerDir, "zz_gen_router.go"), []byte(tt.source), 0o600); err != nil {
				t.Fatal(err)
			}
			got, err := callbackRoute(&app.App{Root: dir}, "pkg/router")
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("callbackRoute() error = %v, wantErr %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("callbackRoute() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("callbackRoute() = %+v, want %+v", got, tt.want)
			}
		})
	}
}
