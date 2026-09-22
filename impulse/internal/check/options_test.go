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
// packages, and the option calls, one per line. The program runs itself under go
// generate, as the options check requires.
func program(resourceDir string, options ...string) string {
	// The directive is spliced in so this file's own source does not start a line with
	// it: go generate reads every Go file for that prefix, raw strings included.
	return "package main\n\n" + "//go:generate go run .\n\n" + `import (
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

// declaredProgram renders a generator program declared in a package of its own,
// cmd/generate, as an application lays it out when tests run the declaration in-process:
// the package holds NewGenerator; the runner, a main package beside it, imports it.
func declaredProgram(resourceDir string, options ...string) string {
	return `package generate

import (
	"context"

	"github.com/cccteam/ccc/resource/generation"
)

func NewGenerator(ctx context.Context) (generation.Generator, error) {
	return generation.NewResourceGenerator(ctx, "` + resourceDir + `", []string{"file://schema/migrations"}, []string{"example.com/harbor/` + resourceDir + `"},
		` + strings.Join(options, "\n\t\t") + `
	)
}
`
}

// generateDirective renders the cmd/generate/generate.go that runs the named program
// directory. The directive is spliced in so this file's own source does not start a
// line with it.
func generateDirective(program string) string {
	return "package generate\n\n" + "//go:generate go run ./" + program + "\n"
}

// runner renders a main package that runs the declared program through the package it
// imports; imports lists the packages it imports.
func runner(imports ...string) string {
	quoted := make([]string, 0, len(imports))
	for _, imp := range imports {
		quoted = append(quoted, "\t\""+imp+"\"")
	}

	return "package main\n\nimport (\n\t\"context\"\n\t\"log\"\n\n" + strings.Join(quoted, "\n") + "\n)\n\nfunc main() {\n\tif err := run(context.Background()); err != nil {\n\t\tlog.Fatal(err)\n\t}\n}\n\nfunc run(ctx context.Context) error {\n\t_ = ctx\n\n\treturn nil\n}\n"
}

// TestOptionsCoherence builds small applications in a temporary directory and pins the
// findings of the options check. The fixture applications cover the passing shapes; these
// cases cover the disagreements, and the layout where the program is declared in a
// package the runner imports.
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
				"cmd/generate/main.go:14: GenerateHandlers passed 2 times; the last call silently wins",
				"cmd/generate/main.go:19: GenerateTypescript target web/portal/src repeats the target at cmd/generate/main.go:18",
				"cmd/generate/main.go: GenerateHandlers without GenerateRoutes; nothing serves the handlers",
				"cmd/generate/main.go:15: WithRouterOutlet requires GenerateRoutes",
				"cmd/generate/main.go: WithConcealedDomains without WithDomainRoute; there are no domains to conceal",
				`cmd/generate/main.go:16: outlet "portal" is declared again (first at cmd/generate/main.go:15)`,
				`cmd/generate/main.go:18: ForOutlet("portal") names an outlet without ServesSessions; a browser app cannot bootstrap there`,
				`cmd/generate/main.go:19: ForOutlet("ghost") names an outlet the program does not declare`,
			},
		},
		{
			name: "undeclared outlet and missing directories",
			files: map[string]string{
				"cmd/generate/main.go": program("pkg/resources",
					`generation.GenerateHandlers("app"),`,
					`generation.GenerateRoutes("pkg/router", "api"),`,
					`generation.WithRPC("pkg/rpc"),`,
					`generation.WithTypes("pkg/telemetry"),`,
					`generation.WithTypes("pkg/cms"),`,
					`generation.GenerateTypescript("web/portal/src", generation.ForOutlet("portal")),`,
				),
				"app":     "",
				"pkg/cms": "",
			},
			wantStatus:  Fail,
			wantSummary: "6 option set problem(s)",
			wantDetails: []string{
				"cmd/generate/main.go: resource package names pkg/resources, which is not a directory in the tree",
				"cmd/generate/main.go: GenerateRoutes names pkg/router, which is not a directory in the tree",
				"cmd/generate/main.go: WithRPC names pkg/rpc, which is not a directory in the tree",
				"cmd/generate/main.go: WithTypes names pkg/telemetry, which is not a directory in the tree",
				"cmd/generate/main.go: local package example.com/harbor/pkg/resources has no directory pkg/resources in the module",
				`cmd/generate/main.go:18: ForOutlet("portal") names an outlet the program does not declare`,
			},
		},
		{
			name: "the generated router with every outlet declared",
			files: map[string]string{
				"cmd/generate/main.go": program("pkg/resources",
					`generation.GenerateHandlers("app"),`,
					`generation.GenerateRouter(),`,
					`generation.GenerateRoutes("pkg/router", "api", generation.Auth("example.com/harbor/pkg/auth/staff", generation.Password), generation.WebApp("/")),`,
					`generation.WithRouterOutlet("portal", "portal/api", generation.Auth("example.com/harbor/pkg/auth/members", generation.OIDCAzure), generation.WebApp("/portal")),`,
					`generation.WithRouterOutlet("machines", "machines", generation.APIKey()),`,
					`generation.GenerateTypescript("web/portal/src", generation.ForOutlet("portal")),`,
				),
				"pkg/resources": "", "app": "", "pkg/router": "", "pkg/auth/staff": "", "pkg/auth/members": "",
			},
			wantStatus:  Pass,
			wantSummary: `flat layout, 1 site(s); not tenanted; outlets portal (sessions), machines; generated router`,
			wantDetails: []string{
				"harbor (cmd/generate/main.go): resources pkg/resources, handlers app, routes pkg/router under /api (generated router), typescript web/portal/src (outlet portal)",
			},
		},
		{
			name: "router options without the switch",
			files: map[string]string{
				"cmd/generate/main.go": program("pkg/resources",
					`generation.GenerateHandlers("app"),`,
					`generation.GenerateRoutes("pkg/router", "api", generation.WebApp("/")),`,
					`generation.WithRouterOutlet("portal", "portal/api", generation.Auth("example.com/harbor/pkg/auth/members", generation.OIDCAzure)),`,
					`generation.WithRouterOutlet("machines", "machines", generation.APIKey()),`,
				),
				"pkg/resources": "", "app": "", "pkg/router": "",
			},
			wantStatus:  Fail,
			wantSummary: "3 option set problem(s)",
			wantDetails: []string{
				"cmd/generate/main.go:14: outlet default declares WebApp, which describes the generated router; declare GenerateRouter, or drop the option and compose the outlet in the hand-written router",
				"cmd/generate/main.go:15: outlet portal declares Auth, which describes the generated router; declare GenerateRouter, or drop the option and compose the outlet in the hand-written router",
				"cmd/generate/main.go:16: outlet machines declares APIKey, which describes the generated router; declare GenerateRouter, or drop the option and compose the outlet in the hand-written router",
			},
		},
		{
			name: "the switch with outlets that do not say how they authenticate",
			files: map[string]string{
				"cmd/generate/main.go": program("pkg/resources",
					`generation.GenerateHandlers("app"),`,
					`generation.GenerateRouter(),`,
					`generation.GenerateRoutes("pkg/router", "api"),`,
					`generation.WithRouterOutlet("portal", "portal/api", generation.Auth("example.com/harbor/pkg/auth/members", generation.OIDCAzure), generation.APIKey()),`,
					`generation.WithRouterOutlet("machines", "machines", generation.APIKey(), generation.WebApp("/machines")),`,
					`generation.WithRouterOutlet("kiosk", "kiosk", generation.Auth("example.com/harbor/pkg/auth/kiosk", generation.Password)),`,
				),
				"pkg/resources": "", "app": "", "pkg/router": "", "pkg/auth/members": "",
			},
			wantStatus:  Fail,
			wantSummary: "4 option set problem(s)",
			wantDetails: []string{
				"cmd/generate/main.go:15: outlet default declares neither Auth nor APIKey; under GenerateRouter every outlet says how it authenticates",
				"cmd/generate/main.go:16: outlet portal declares both Auth and APIKey; an outlet is a browser surface behind one auth or a machine surface behind an API key",
				`cmd/generate/main.go:17: outlet machines declares APIKey and WebApp("/machines"); a machine outlet serves no browser application`,
				`cmd/generate/main.go:18: outlet kiosk binds to Auth("example.com/harbor/pkg/auth/kiosk"), which has no directory pkg/auth/kiosk in the module`,
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
			name: "sites layout with a straggler and a tenancy disagreement",
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
			name: "sites layout agreeing",
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
			wantSummary: `sites layout, 2 site(s) (console, portal) + 1 shared; tenanted on "tenants"; no outlets`,
			wantDetails: []string{
				"console (cmd/generate/console/main.go): resources apps/console/pkg/resources, handlers apps/console/app, routes apps/console/pkg/router under /api",
				"portal (cmd/generate/portal/main.go): resources apps/portal/pkg/resources, handlers apps/portal/app, routes apps/portal/pkg/router under /api",
				"shared (cmd/generate/shared/main.go): resources pkg/sharedresources, typescript apps/console/web/src",
			},
		},
		{
			// The declaration lives in cmd/generate so tests can import it; go generate
			// runs the runner beside it, which imports the declaring package.
			name: "a program declared in a package the runner imports is run",
			files: map[string]string{
				"cmd/generate/generate.go":               generateDirective("resourcegenerator"),
				"cmd/generate/generator.go":              declaredProgram("pkg/resources", `generation.GenerateHandlers("app"),`, `generation.GenerateRoutes("pkg/router", "api"),`),
				"cmd/generate/resourcegenerator/main.go": runner("example.com/harbor/cmd/generate"),
				"pkg/resources":                          "", "app": "", "pkg/router": "",
			},
			wantStatus:  Pass,
			wantSummary: "flat layout, 1 site(s); not tenanted; no outlets",
			wantDetails: []string{
				"harbor (cmd/generate/generator.go): resources pkg/resources, handlers app, routes pkg/router under /api",
			},
		},
		{
			// A runner that does not import the declaration runs something else: the
			// declared program is not run.
			name: "a runner that does not import the declaration leaves the program unrun",
			files: map[string]string{
				"cmd/generate/generate.go":               generateDirective("resourcegenerator"),
				"cmd/generate/generator.go":              declaredProgram("pkg/resources", `generation.GenerateHandlers("app"),`, `generation.GenerateRoutes("pkg/router", "api"),`),
				"cmd/generate/resourcegenerator/main.go": runner("example.com/harbor/pkg/resources"),
				"pkg/resources":                          "", "app": "", "pkg/router": "",
			},
			wantStatus:  Fail,
			wantSummary: "1 option set problem(s)",
			wantDetails: []string{
				"cmd/generate/generator.go: no //go:generate directive runs this program, so go generate ./... never regenerates it",
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
