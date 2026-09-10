package handoff

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/cccteam/ccc/impulse/internal/app"
)

// fixture discovers one of the app package's fixture applications.
func fixture(t *testing.T, name string) *app.App {
	t.Helper()

	a, err := app.Discover(filepath.Join("..", "app", "testdata", name))
	if err != nil {
		t.Fatalf("app.Discover(%s) error = %v", name, err)
	}

	return a
}

const singlesiteProgram = "cmd/generate/resourcegenerator/main.go"

var singlesiteLines = []string{
	`resources "pkg/resources"`,
	`migrations ["file://schema/migrations"]`,
	`packages ["example.com/lighthouse/pkg/resources", "example.com/lighthouse/pkg/rpc"]`,
	`GenerateHandlers("app")`,
	`GenerateRoutes("pkg/router", "api")`,
	`WithRouterOutlet("portal", "portal", ServesSessions())`,
	`GenerateHandlerTests("test/authz")`,
	`WithRPC("pkg/rpc")`,
	`WithConsolidatedHandlers("resources", true, "Beacon")`,
	`WithPluralOverrides({"Lens": "Lenses"})`,
	`CaserInitialismOverrides({"GPS": true})`,
	`WithSpannerEmulatorVersion("1.5.56")`,
	`GenerateTypescript("web/console/src/app/core/service", GenerateMetadata(), GeneratePermissions(), GenerateEnums())`,
	`GenerateTypescript("web/portal/src/app/core/service", ForOutlet("portal"), GenerateEnums())`,
}

func TestTake(t *testing.T) {
	t.Parallel()

	a := fixture(t, "singlesite")
	tree, err := Take(a, FromTree(a))
	if err != nil {
		t.Fatalf("Take(tree) error = %v", err)
	}
	eslint := tree.Configs["web/console/eslint.config.js"]
	if eslint == "" {
		t.Fatalf("Take(tree) read no eslint config: %v", tree.Configs)
	}

	tests := []struct {
		name string
		read Reader
		want Snapshot
	}{
		{
			name: "the working tree",
			read: FromTree(a),
			want: Snapshot{
				Programs: map[string][]string{singlesiteProgram: singlesiteLines},
				Configs:  map[string]string{"web/console/eslint.config.js": eslint},
			},
		},
		{
			name: "a source without the program or the config",
			read: func(string) ([]byte, error) { return nil, os.ErrNotExist },
			want: Snapshot{Programs: map[string][]string{}, Configs: map[string]string{}},
		},
		{
			name: "a source holding another program",
			read: func(rel string) ([]byte, error) {
				if rel != singlesiteProgram {
					return nil, os.ErrNotExist
				}

				return []byte(`package main

import "github.com/cccteam/ccc/resource/generation"

func run() {
	generation.NewResourceGenerator(nil, "pkg/resources", []string{"file://schema/migrations"}, []string{},
		generation.GenerateHandlers("app"),
	)
}
`), nil
			},
			want: Snapshot{
				Programs: map[string][]string{singlesiteProgram: {
					`resources "pkg/resources"`, `migrations ["file://schema/migrations"]`, `packages []`, `GenerateHandlers("app")`,
				}},
				Configs: map[string]string{},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := Take(a, tt.read)
			if err != nil {
				t.Fatalf("Take() error = %v", err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("Take() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestCompare(t *testing.T) {
	t.Parallel()

	base := Snapshot{
		Programs: map[string][]string{"cmd/generate/main.go": {`resources "pkg/resources"`, `GenerateHandlers("app")`, `GenerateRoutes("pkg/router", "api")`}},
		Configs:  map[string]string{GolangciConfig: "aaa", "web/eslint.config.js": "bbb"},
	}
	tests := []struct {
		name  string
		after Snapshot
		want  []string
	}{
		{
			name:  "unchanged",
			after: base,
		},
		{
			name: "an option added, one removed, one rewritten",
			after: Snapshot{
				Programs: map[string][]string{"cmd/generate/main.go": {`resources "pkg/resources"`, `GenerateHandlers("app")`, `GenerateRoutes("pkg/router", "v2")`, `WithRPC("pkg/rpc")`}},
				Configs:  base.Configs,
			},
			want: []string{
				`cmd/generate/main.go: added GenerateRoutes("pkg/router", "v2")`,
				`cmd/generate/main.go: added WithRPC("pkg/rpc")`,
				`cmd/generate/main.go: removed GenerateRoutes("pkg/router", "api")`,
			},
		},
		{
			name: "a program removed and another added",
			after: Snapshot{
				Programs: map[string][]string{"apps/portal/cmd/generate/main.go": {`resources "apps/portal/pkg/resources"`}},
				Configs:  base.Configs,
			},
			want: []string{
				"apps/portal/cmd/generate/main.go: a generator program was added",
				"cmd/generate/main.go: the generator program was removed",
			},
		},
		{
			name: "lint configuration edited, removed, and added",
			after: Snapshot{
				Programs: base.Programs,
				Configs:  map[string]string{GolangciConfig: "changed", "web/.eslintignore": "ccc"},
			},
			want: []string{
				GolangciConfig + ": the lint configuration was edited",
				"web/.eslintignore: a lint configuration was added",
				"web/eslint.config.js: the lint configuration was removed",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if diff := cmp.Diff(tt.want, Compare(base, tt.after)); diff != "" {
				t.Errorf("Compare() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
