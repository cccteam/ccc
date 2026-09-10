package app

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func TestDiscover(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		dir               string
		wantErr           string
		wantGenerators    []string
		wantWebApps       []WebApp
		wantImages        []EmulatorRef
		wantHarnesses     []EmulatorRef
		wantEnvTemplate   string
		wantEnvTagNames   []string
		wantSiteFiles     []string
		wantSharedFiles   []string
		wantProblemCount  int
		wantHandlersDir   string
		wantTSTargets     []TSTarget
		wantEmulatorInGen string
	}{
		{
			name:            "single site",
			dir:             "testdata/singlesite",
			wantGenerators:  []string{"cmd/generate/resourcegenerator/main.go"},
			wantWebApps:     []WebApp{{Dir: "web/console"}, {Dir: "web/portal"}},
			wantImages:      []EmulatorRef{{File: "Procfile", Line: 1, Version: "1.5.56"}},
			wantHarnesses:   []EmulatorRef{{File: "test/integration/main_test.go", Line: 12, Version: "1.5.56"}},
			wantEnvTemplate: ".envrc.template",
			wantEnvTagNames: []string{
				"LIGHTHOUSE_PROJECT_ID", "LIGHTHOUSE_SPANNER_INSTANCE_ID", "LIGHTHOUSE_SPANNER_DATABASE",
				"LIGHTHOUSE_PORT", "LIGHTHOUSE_COOKIE_KEY", "LIGHTHOUSE_SESSION_TIMEOUT", "LIGHTHOUSE_BEACON_API_KEY",
			},
			wantSiteFiles:   []string{"cmd/generate/resourcegenerator/main.go"},
			wantHandlersDir: "app",
			wantTSTargets: []TSTarget{
				{Dir: "web/console/src/app/core/service", Pos: "cmd/generate/resourcegenerator/main.go:35"},
				{Dir: "web/portal/src/app/core/service", Outlet: "portal", Pos: "cmd/generate/resourcegenerator/main.go:40"},
			},
			wantEmulatorInGen: "1.5.56",
		},
		{
			name: "multi site",
			dir:  "testdata/multisite",
			wantGenerators: []string{
				"cmd/generate/resourcegenerator_pilots/main.go",
				"cmd/generate/resourcegenerator_shared/main.go",
				"cmd/generate/resourcegenerator_tugs/main.go",
			},
			wantWebApps:     []WebApp{{Dir: "apps/pilots/gui"}, {Dir: "apps/tugs/gui"}},
			wantImages:      []EmulatorRef{{File: "process-compose.yaml", Line: 3, Version: "1.5.43"}},
			wantSiteFiles:   []string{"cmd/generate/resourcegenerator_pilots/main.go", "cmd/generate/resourcegenerator_tugs/main.go"},
			wantSharedFiles: []string{"cmd/generate/resourcegenerator_shared/main.go"},
			wantHandlersDir: "apps/pilots/app",
			wantTSTargets: []TSTarget{
				{Dir: "apps/pilots/gui/src/app/core/service", Pos: "cmd/generate/resourcegenerator_pilots/main.go:19"},
			},
			wantEmulatorInGen: "1.5.44",
		},
		{
			name:             "bad program",
			dir:              "testdata/badprogram",
			wantGenerators:   []string{"cmd/generate/main.go"},
			wantSharedFiles:  []string{"cmd/generate/main.go"}, // the handlers dir is not literal, so no site is read
			wantProblemCount: 6,
		},
		{
			name:    "no go.mod",
			dir:     "testdata",
			wantErr: "no go.mod at",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			a, err := Discover(tt.dir)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Discover() error = %v, want containing %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("Discover() error = %v", err)
			}

			wantRoot, _ := filepath.Abs(tt.dir)
			if a.Root != wantRoot {
				t.Errorf("Root = %q, want %q", a.Root, wantRoot)
			}
			if diff := cmp.Diff(tt.wantGenerators, files(a.Generators)); diff != "" {
				t.Errorf("Generators mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.wantWebApps, a.WebApps, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("WebApps mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.wantImages, a.EmulatorImages, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("EmulatorImages mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.wantHarnesses, a.EmulatorHarnesses, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("EmulatorHarnesses mismatch (-want +got):\n%s", diff)
			}
			if a.EnvTemplate != tt.wantEnvTemplate {
				t.Errorf("EnvTemplate = %q, want %q", a.EnvTemplate, tt.wantEnvTemplate)
			}
			var names []string
			for _, tag := range a.EnvTags {
				names = append(names, tag.Name)
			}
			if diff := cmp.Diff(tt.wantEnvTagNames, names, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("EnvTags mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.wantSiteFiles, files(a.SiteGenerators()), cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("SiteGenerators mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.wantSharedFiles, files(a.SharedGenerators()), cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("SharedGenerators mismatch (-want +got):\n%s", diff)
			}

			first := a.Generators[0]
			if got := len(first.Problems); got != tt.wantProblemCount {
				t.Errorf("Problems = %d, want %d:\n%s", got, tt.wantProblemCount, problems(first))
			}
			if tt.wantProblemCount > 0 {
				return
			}
			if got := first.HandlersDir(); got != tt.wantHandlersDir {
				t.Errorf("HandlersDir() = %q, want %q", got, tt.wantHandlersDir)
			}
			if diff := cmp.Diff(tt.wantTSTargets, first.TypescriptTargets(), cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("TypescriptTargets mismatch (-want +got):\n%s", diff)
			}
			if got := first.EmulatorVersion(); got != tt.wantEmulatorInGen {
				t.Errorf("EmulatorVersion() = %q, want %q", got, tt.wantEmulatorInGen)
			}
		})
	}
}

func TestSingleSiteGeneratorDetails(t *testing.T) {
	t.Parallel()

	a, err := Discover("testdata/singlesite")
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	g := a.Generators[0]

	tests := []struct {
		name string
		got  any
		want any
	}{
		{name: "resource package dir", got: g.ResourcePackageDir, want: "pkg/resources"},
		{name: "migration sources", got: g.MigrationSources, want: []string{"file://schema/migrations"}},
		{name: "local packages", got: g.LocalPackages, want: []string{"example.com/lighthouse/pkg/resources", "example.com/lighthouse/pkg/rpc"}},
		{name: "routes dir", got: g.RoutesDir(), want: "pkg/router"},
		{name: "handler tests dir", got: g.HandlerTestsDir(), want: "test/authz"},
		{name: "rpc dir", got: g.RPCDir(), want: "pkg/rpc"},
		{name: "option count", got: len(g.Options), want: 11},
		{name: "plural overrides", got: mustOption(t, g, "WithPluralOverrides").Args[0].Map, want: map[string]string{"Lens": "Lenses"}},
		{name: "initialism overrides", got: mustOption(t, g, "CaserInitialismOverrides").Args[0].Map, want: map[string]string{"GPS": "true"}},
		{name: "consolidated bool", got: mustOption(t, g, "WithConsolidatedHandlers").Args[1].Bool, want: true},
		{name: "consolidated variadic", got: mustOption(t, g, "WithConsolidatedHandlers").Args[2].Str, want: "Beacon"},
		{name: "outlet option nested", got: mustOption(t, g, "WithRouterOutlet").Args[2].Call.Name, want: "ServesSessions"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if diff := cmp.Diff(tt.want, tt.got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestWebAppFor(t *testing.T) {
	t.Parallel()

	a := &App{WebApps: []WebApp{{Dir: "web/console"}, {Dir: "web/console/nested"}, {Dir: "gui"}}}

	tests := []struct {
		name    string
		rel     string
		want    string
		wantOK  bool
		message string
	}{
		{name: "inside", rel: "gui/src/app", want: "gui", wantOK: true},
		{name: "deepest wins", rel: "web/console/nested/src", want: "web/console/nested", wantOK: true},
		{name: "the dir itself", rel: "web/console", want: "web/console", wantOK: true},
		{name: "sibling prefix is not inside", rel: "web/consoles/src", wantOK: false},
		{name: "outside", rel: "pkg/router", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := a.WebAppFor(tt.rel)
			if ok != tt.wantOK || got.Dir != tt.want {
				t.Errorf("WebAppFor(%q) = (%q, %v), want (%q, %v)", tt.rel, got.Dir, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func files(gs []*Generator) []string {
	var out []string
	for _, g := range gs {
		out = append(out, g.File)
	}

	return out
}

func problems(g *Generator) string {
	var b strings.Builder
	for _, p := range g.Problems {
		b.WriteString(p.String() + "\n")
	}

	return b.String()
}

func mustOption(t *testing.T, g *Generator, name string) Call {
	t.Helper()
	c, ok := g.Option(name)
	if !ok {
		t.Fatalf("option %s not found", name)
	}

	return c
}
