package check

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/cccteam/ccc/impulse/app"
)

func TestOutletShape(t *testing.T) {
	t.Parallel()

	const (
		staffAuth   = `generation.Auth("example.com/harbor/pkg/auth/staff", generation.Password)`
		membersAuth = `generation.Auth("example.com/harbor/pkg/auth/members", generation.OIDCAzure)`
	)

	tests := []struct {
		name        string
		program     string
		wantStatus  Status
		wantSummary string
		wantDetails []string
	}{
		{
			name: "one application at the root",
			program: program("pkg/resources",
				`generation.GenerateHandlers("app"),`,
				`generation.GenerateRouter(),`,
				`generation.GenerateRoutes("pkg/router", "api", `+staffAuth+`, generation.WebApp("/")),`,
				`generation.WithRouterOutlet("machines", "machines", generation.APIKey()),`,
			),
			wantStatus:  Pass,
			wantSummary: "1 browser application(s) serve their API under their mount path",
		},
		{
			name: "two applications, each API under its mount path",
			program: program("pkg/resources",
				`generation.GenerateHandlers("app"),`,
				`generation.GenerateRouter(),`,
				`generation.GenerateRoutes("pkg/router", "console/api", `+staffAuth+`, generation.WebApp("/console")),`,
				`generation.WithRouterOutlet("portal", "portal/api", `+membersAuth+`, generation.WebApp("/portal")),`,
			),
			wantStatus:  Pass,
			wantSummary: "2 browser application(s) serve their API under their mount path",
		},
		{
			name: "the console under a path keeps the root's prefix",
			program: program("pkg/resources",
				`generation.GenerateHandlers("app"),`,
				`generation.GenerateRouter(),`,
				`generation.GenerateRoutes("pkg/router", "api", `+staffAuth+`, generation.WebApp("/console")),`,
				`generation.WithRouterOutlet("portal", "portal/api", `+membersAuth+`, generation.WebApp("/portal")),`,
			),
			wantStatus:  Warn,
			wantSummary: "1 browser outlet(s) serve their API outside their application's mount path",
			wantDetails: []string{
				"cmd/generate/main.go:15: outlet default serves its API under /api and its browser application at /console; the one shape puts an outlet's API under its application's mount path, /console/api",
			},
		},
		{
			name: "a root application under another segment and a portal beside its prefix",
			program: program("pkg/resources",
				`generation.GenerateHandlers("app"),`,
				`generation.GenerateRouter(),`,
				`generation.GenerateRoutes("pkg/router", "v1", `+staffAuth+`, generation.WebApp("/")),`,
				`generation.WithRouterOutlet("portal", "portal", `+membersAuth+`, generation.WebApp("/portal")),`,
			),
			wantStatus:  Warn,
			wantSummary: "2 browser outlet(s) serve their API outside their application's mount path",
			wantDetails: []string{
				"cmd/generate/main.go:15: outlet default serves its API under /v1 and its browser application at /; the one shape puts an outlet's API under its application's mount path, /api",
				"cmd/generate/main.go:16: outlet portal serves its API under /portal and its browser application at /portal; the one shape puts an outlet's API under its application's mount path, /portal/api",
			},
		},
		{
			name: "a hand-written router declares no browser application",
			program: program("pkg/resources",
				`generation.GenerateHandlers("app"),`,
				`generation.GenerateRoutes("pkg/router", "api"),`,
				`generation.WithRouterOutlet("portal", "portal/api", generation.ServesSessions()),`,
			),
			wantStatus:  Skip,
			wantSummary: "no outlet serves a browser application",
		},
		{
			name:        "no site generator",
			program:     program("pkg/sharedresources", `generation.GenerateTypescript("web/src"),`),
			wantStatus:  Skip,
			wantSummary: "no site generator",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			write := func(rel, content string) {
				t.Helper()
				if err := os.MkdirAll(filepath.Dir(filepath.Join(root, rel)), 0o750); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, rel), []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			write("go.mod", "module example.com/harbor\n\ngo 1.26.6\n")
			write("cmd/generate/main.go", tt.program)

			a, err := app.Discover(root)
			if err != nil {
				t.Fatalf("app.Discover() error = %v", err)
			}
			got := outletShape{}.Run(context.Background(), &Env{App: a})
			want := Result{Name: outletShape{}.Name(), Status: tt.wantStatus, Summary: tt.wantSummary, Details: tt.wantDetails}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("Run() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
