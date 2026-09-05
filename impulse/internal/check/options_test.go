package check

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/cccteam/ccc/impulse/internal/app"
)

// program renders one generator program: the resource package, its module-local
// packages, and the option calls, one per line.
func program(resourceDir string, options ...string) string {
	return `package main

import (
	"context"

	"github.com/cccteam/ccc/resource/generation"
)

func main() {
	_, _ = generation.NewResourceGenerator(context.Background(), "` + resourceDir + `", []string{"file://schema/migrations"}, []string{"example.com/harbor/` + resourceDir + `"},
		` + strings.Join(options, "\n\t\t") + `
	)
}
`
}

// TestOptionsCoherence builds small applications in a temporary directory and pins the
// findings of the options check. The fixture applications cover the passing shapes; these
// cases cover the disagreements.
func TestOptionsCoherence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// files maps root-relative paths to content; a value of "" creates a package
		// directory holding a doc.go.
		files       map[string]string
		wantStatus  Status
		wantSummary string
		wantDetails []string
	}{
		{
			name: "flat and tenanted with outlets",
			files: map[string]string{
				"cmd/generate/main.go": program("pkg/resources",
					`generation.GenerateHandlers("app"),`,
					`generation.GenerateRoutes("pkg/router", "api"),`,
					`generation.WithRouterOutlet("portal", "portal/api", generation.ServesSessions()),`,
					`generation.WithRouterOutlet("machines", "machines"),`,
					`generation.WithDomainRoute("tenants"),`,
					`generation.WithConcealedDomains(),`,
					`generation.GenerateTypescript("web/portal/src", generation.ForOutlet("portal")),`,
				),
				"pkg/resources": "", "app": "", "pkg/router": "",
			},
			wantStatus:  Pass,
			wantSummary: `flat layout, 1 site(s); tenanted on "tenants" (concealed); outlets portal (sessions), machines`,
			wantDetails: []string{
				"harbor (cmd/generate/main.go): resources pkg/resources, handlers app, routes pkg/router under /api, typescript web/portal/src (outlet portal)",
			},
		},
		{
			name: "one program contradicting itself",
			files: map[string]string{
				"cmd/generate/main.go": program("pkg/resources",
					`generation.GenerateHandlers("app"),`,
					`generation.GenerateHandlers("app"),`,
					`generation.WithRouterOutlet("portal", "portal/api"),`,
					`generation.WithRouterOutlet("portal", "portal2"),`,
					`generation.WithConcealedDomains(),`,
					`generation.GenerateTypescript("web/portal/src", generation.ForOutlet("portal")),`,
					`generation.GenerateTypescript("web/portal/src", generation.ForOutlet("ghost")),`,
				),
				"pkg/resources": "", "app": "",
			},
			wantStatus:  Fail,
			wantSummary: "8 option set problem(s)",
			wantDetails: []string{
				"cmd/generate/main.go:12: GenerateHandlers passed 2 times; the last call silently wins",
				"cmd/generate/main.go:17: GenerateTypescript target web/portal/src repeats the target at cmd/generate/main.go:16",
				"cmd/generate/main.go: GenerateHandlers without GenerateRoutes; nothing serves the handlers",
				"cmd/generate/main.go:13: WithRouterOutlet requires GenerateRoutes",
				"cmd/generate/main.go: WithConcealedDomains without WithDomainRoute; there are no domains to conceal",
				`cmd/generate/main.go:14: outlet "portal" is declared again (first at cmd/generate/main.go:13)`,
				`cmd/generate/main.go:16: ForOutlet("portal") names an outlet without ServesSessions; a browser app cannot bootstrap there`,
				`cmd/generate/main.go:17: ForOutlet("ghost") names an outlet the program does not declare`,
			},
		},
		{
			name: "undeclared outlet and missing directories",
			files: map[string]string{
				"cmd/generate/main.go": program("pkg/resources",
					`generation.GenerateHandlers("app"),`,
					`generation.GenerateRoutes("pkg/router", "api"),`,
					`generation.WithRPC("pkg/rpc"),`,
					`generation.GenerateTypescript("web/portal/src", generation.ForOutlet("portal")),`,
				),
				"app": "",
			},
			wantStatus:  Fail,
			wantSummary: "5 option set problem(s)",
			wantDetails: []string{
				"cmd/generate/main.go: resource package names pkg/resources, which is not a directory in the tree",
				"cmd/generate/main.go: GenerateRoutes names pkg/router, which is not a directory in the tree",
				"cmd/generate/main.go: WithRPC names pkg/rpc, which is not a directory in the tree",
				"cmd/generate/main.go: local package example.com/harbor/pkg/resources has no directory pkg/resources in the module",
				`cmd/generate/main.go:14: ForOutlet("portal") names an outlet the program does not declare`,
			},
		},
		{
			name: "two sites in the flat layout",
			files: map[string]string{
				"cmd/generate/console/main.go": program("pkg/resources",
					`generation.GenerateHandlers("app"),`,
					`generation.GenerateRoutes("pkg/router", "api"),`,
				),
				"cmd/generate/portal/main.go": program("portal/pkg/resources",
					`generation.GenerateHandlers("portal/app"),`,
					`generation.GenerateRoutes("portal/pkg/router", "api"),`,
				),
				"pkg/resources": "", "app": "", "pkg/router": "",
				"portal/pkg/resources": "", "portal/app": "", "portal/pkg/router": "",
			},
			wantStatus:  Fail,
			wantSummary: "1 option set problem(s)",
			wantDetails: []string{
				"2 sites share the flat layout (cmd/generate/console/main.go, cmd/generate/portal/main.go); a second site belongs under apps/<site>/",
			},
		},
		{
			name: "multi-site with a straggler and a tenancy disagreement",
			files: map[string]string{
				"cmd/generate/console/main.go": program("apps/console/pkg/resources",
					`generation.GenerateHandlers("apps/console/app"),`,
					`generation.GenerateRoutes("apps/console/pkg/router", "api"),`,
					`generation.WithDomainRoute("tenants"),`,
				),
				"cmd/generate/portal/main.go": program("apps/portal/pkg/resources",
					`generation.GenerateHandlers("apps/portal/app"),`,
					`generation.GenerateRoutes("apps/portal/pkg/router", "api"),`,
				),
				"cmd/generate/kiosk/main.go": program("apps/kiosk/pkg/resources",
					`generation.GenerateHandlers("kiosk/app"),`,
					`generation.GenerateRoutes("apps/kiosk/pkg/router", "api"),`,
					`generation.WithDomainRoute("tenants"),`,
				),
				"apps/console/pkg/resources": "", "apps/console/app": "", "apps/console/pkg/router": "",
				"apps/portal/pkg/resources": "", "apps/portal/app": "", "apps/portal/pkg/router": "",
				"apps/kiosk/pkg/resources": "", "kiosk/app": "", "apps/kiosk/pkg/router": "",
			},
			wantStatus:  Fail,
			wantSummary: "2 option set problem(s)",
			wantDetails: []string{
				"cmd/generate/kiosk/main.go: site packages are not under one apps/<site>/ directory (resources apps/kiosk/pkg/resources, handlers kiosk/app, routes apps/kiosk/pkg/router)",
				`sites disagree on tenancy: console is tenanted on "tenants" but portal is not tenanted`,
			},
		},
		{
			name: "multi-site agreeing",
			files: map[string]string{
				"cmd/generate/console/main.go": program("apps/console/pkg/resources",
					`generation.GenerateHandlers("apps/console/app"),`,
					`generation.GenerateRoutes("apps/console/pkg/router", "api"),`,
					`generation.WithDomainRoute("tenants"),`,
				),
				"cmd/generate/portal/main.go": program("apps/portal/pkg/resources",
					`generation.GenerateHandlers("apps/portal/app"),`,
					`generation.GenerateRoutes("apps/portal/pkg/router", "api"),`,
					`generation.WithDomainRoute("tenants"),`,
				),
				"cmd/generate/shared/main.go": program("pkg/sharedresources",
					`generation.GenerateTypescript("apps/console/web/src"),`,
				),
				"apps/console/pkg/resources": "", "apps/console/app": "", "apps/console/pkg/router": "",
				"apps/portal/pkg/resources": "", "apps/portal/app": "", "apps/portal/pkg/router": "",
				"pkg/sharedresources": "",
			},
			wantStatus:  Pass,
			wantSummary: `multi-site layout, 2 site(s) (console, portal) + 1 shared; tenanted on "tenants"; no outlets`,
			wantDetails: []string{
				"console (cmd/generate/console/main.go): resources apps/console/pkg/resources, handlers apps/console/app, routes apps/console/pkg/router under /api",
				"portal (cmd/generate/portal/main.go): resources apps/portal/pkg/resources, handlers apps/portal/app, routes apps/portal/pkg/router under /api",
				"shared (cmd/generate/shared/main.go): resources pkg/sharedresources, typescript apps/console/web/src",
			},
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
			for rel, content := range tt.files {
				if content == "" {
					write(filepath.Join(rel, "doc.go"), "package "+filepath.Base(rel)+"\n")

					continue
				}
				write(rel, content)
			}

			a, err := app.Discover(root)
			if err != nil {
				t.Fatalf("app.Discover() error = %v", err)
			}
			got := options{}.Run(context.Background(), &Env{App: a})
			want := Result{Name: options{}.Name(), Status: tt.wantStatus, Summary: tt.wantSummary, Details: tt.wantDetails}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("Run() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
