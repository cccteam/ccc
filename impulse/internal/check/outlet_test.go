package check

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/cccteam/ccc/impulse/internal/app"
)

const (
	outletAngularJSON = `{
  "projects": {
    "console": {"root": "console", "sourceRoot": "console/src",
      "architect": {"serve": {"options": {"proxyConfig": "console/proxy.conf.js"}}}},
    "portal": {"root": "portal", "sourceRoot": "portal/src",
      "architect": {"serve": {"options": {"proxyConfig": "portal/proxy.conf.js"}}}}
  }
}
`
	consoleProxy = "module.exports = { '/api/': { target: 'http://127.0.0.1:8080' } };\n"
	portalProxy  = "module.exports = { '/portal/api/': { target: 'http://127.0.0.1:8080' } };\n"
	fullRouter   = `package router

func New(h any) {
	generatedRoutes(nil, h)
	generatedPortalRoutes(nil, h)
	generatedMachinesRoutes(nil, h)
}
`
	outletMembers = `package resources

type (
	// Announcement is on the console and the portal.
	//
	// @resource
	// @outlet(default, portal)
	Announcement struct{}
)
`
)

func TestOutletWired(t *testing.T) {
	t.Parallel()

	declared := program("pkg/resources",
		`generation.GenerateHandlers("app"),`,
		`generation.GenerateRoutes("pkg/router", "api"),`,
		`generation.WithRouterOutlet("portal", "portal/api", generation.ServesSessions()),`,
		`generation.WithRouterOutlet("machines", "machines"),`,
		`generation.GenerateTypescript("web/console/src/app/core/service"),`,
		`generation.GenerateTypescript("web/portal/src/app/core/service", generation.ForOutlet("portal")),`,
	)

	tests := []struct {
		name        string
		files       map[string]string
		wantStatus  Status
		wantSummary string
		wantDetails []string
	}{
		{
			name: "every outlet wired",
			files: map[string]string{
				"cmd/generate/main.go":           declared,
				"pkg/router/router.go":           fullRouter,
				"pkg/resources/announcements.go": outletMembers,
				"web/angular.json":               outletAngularJSON,
				"web/console/proxy.conf.js":      consoleProxy,
				"web/portal/proxy.conf.js":       portalProxy,
			},
			wantStatus:  Pass,
			wantSummary: "3 outlet(s) mounted: default (/api), portal (/portal/api, sessions), machines (/machines)",
			wantDetails: []string{"outlet machines has no @outlet(machines) members yet"},
		},
		{
			name: "unmounted outlet, clientless session outlet, and a proxy that misses the prefix",
			files: map[string]string{
				"cmd/generate/main.go": program("pkg/resources",
					`generation.GenerateHandlers("app"),`,
					`generation.GenerateRoutes("pkg/router", "api"),`,
					`generation.WithRouterOutlet("portal", "portal/api", generation.ServesSessions()),`,
					`generation.WithRouterOutlet("kiosk", "kiosk", generation.ServesSessions()),`,
					`generation.WithRouterOutlet("machines", "machines"),`,
					`generation.GenerateTypescript("web/console/src/app/core/service"),`,
					`generation.GenerateTypescript("web/portal/src/app/core/service", generation.ForOutlet("portal")),`,
				),
				"pkg/router/router.go": `package router

func New(h any) {
	generatedRoutes(nil, h)
	generatedPortalRoutes(nil, h)
	generatedKioskRoutes(nil, h)
}
`,
				"pkg/resources/announcements.go": outletMembers,
				"web/angular.json":               outletAngularJSON,
				"web/console/proxy.conf.js":      consoleProxy,
				"web/portal/proxy.conf.js":       consoleProxy,
			},
			wantStatus:  Fail,
			wantSummary: "3 outlet wiring problem(s)",
			wantDetails: []string{
				"web/portal/proxy.conf.js does not forward /portal/api, the portal outlet's prefix, so ng serve for portal cannot reach it",
				"cmd/generate/main.go:14: outlet kiosk serves sessions, but no GenerateTypescript target names it (ForOutlet); no browser app can bootstrap there",
				"cmd/generate/main.go: no file in pkg/router calls generatedMachinesRoutes; the machines outlet's routes are not mounted",
				"outlet kiosk has no @outlet(kiosk) members yet",
			},
		},
		{
			name: "no browser workspace and no proxy is nothing to report",
			files: map[string]string{
				"cmd/generate/main.go":           declared,
				"pkg/router/router.go":           fullRouter,
				"pkg/resources/announcements.go": outletMembers,
			},
			wantStatus:  Pass,
			wantSummary: "3 outlet(s) mounted: default (/api), portal (/portal/api, sessions), machines (/machines)",
			wantDetails: []string{"outlet machines has no @outlet(machines) members yet"},
		},
		{
			name: "no site generator",
			files: map[string]string{
				"cmd/generate/main.go": program("pkg/sharedresources", `generation.GenerateTypescript("web/src"),`),
			},
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
			for rel, content := range tt.files {
				write(rel, content)
			}

			a, err := app.Discover(root)
			if err != nil {
				t.Fatalf("app.Discover() error = %v", err)
			}
			got := outletWired{}.Run(context.Background(), &Env{App: a})
			want := Result{Name: outletWired{}.Name(), Status: tt.wantStatus, Summary: tt.wantSummary, Details: tt.wantDetails}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("Run() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
