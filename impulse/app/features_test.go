package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

// featureDeclarations is a resources package declaring two flags, one in a group with
// a constant of another type.
const featureDeclarations = `package resources

import "github.com/cccteam/ccc/resource"

// Debriefs lets a crew write and read mission debriefs.
const Debriefs resource.Feature = "debriefs"

const (
	// CargoManifest shows a ship's cargo bays,
	// bay by bay.
	CargoManifest resource.Feature = "cargo_manifest"
	// NotAFlag is a constant of another type.
	NotAFlag string = "not_a_flag"
)
`

// featureGatedResources is a resources package gating a struct, a field by its doc and
// a field by its line comment.
const featureGatedResources = `package resources

type (
	// Debrief is gated whole.
	//
	// @resource
	// @feature(Debriefs)
	Debrief struct {
		ID string ` + "`spanner:\"Id\"`" + `
	}

	// Ship carries gated fields.
	//
	// @resource
	Ship struct {
		ID   string ` + "`spanner:\"Id\"`" + `
		// @feature(CargoManifest)
		CargoBays *int64 ` + "`spanner:\"CargoBays\"`" + `
		Holds     *int64 ` + "`spanner:\"Holds\"`" + ` // @feature( CargoManifest )
	}
)
`

// writeTree writes files under root, creating directories as needed.
func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()

	for rel, content := range files {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestFeatureDeclarationsAndGates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		files     map[string]string
		wantFlags []FeatureFlag
		wantGates []FeatureGate
	}{
		{
			name: "declarations with their descriptions, in file order; a constant of another type is not a flag",
			files: map[string]string{
				"pkg/resources/features.go": featureDeclarations,
			},
			wantFlags: []FeatureFlag{
				{File: "pkg/resources/features.go", Line: 6, Constant: "Debriefs", Name: "debriefs", Description: "Debriefs lets a crew write and read mission debriefs.", Package: "example.com/acme/beacon/pkg/resources"},
				{File: "pkg/resources/features.go", Line: 11, Constant: "CargoManifest", Name: "cargo_manifest", Description: "CargoManifest shows a ship's cargo bays, bay by bay.", Package: "example.com/acme/beacon/pkg/resources"},
			},
		},
		{
			name: "gates on a struct, a field's doc, and a field's line comment",
			files: map[string]string{
				"pkg/resources/ships.go": featureGatedResources,
			},
			wantGates: []FeatureGate{
				{File: "pkg/resources/ships.go", Line: 7, Constant: "Debriefs", Target: "Debrief"},
				{File: "pkg/resources/ships.go", Line: 17, Constant: "CargoManifest", Target: "Ship.CargoBays"},
				{File: "pkg/resources/ships.go", Line: 19, Constant: "CargoManifest", Target: "Ship.Holds"},
			},
		},
		{
			name: "generated files and tests are not read",
			files: map[string]string{
				"pkg/resources/zz_gen_features.go": featureDeclarations,
				"pkg/resources/ships_test.go":      featureGatedResources,
			},
		},
		{
			name: "a flag typed through an aliased import",
			files: map[string]string{
				"pkg/resources/flags.go": "package resources\n\nimport res \"github.com/cccteam/ccc/resource\"\n\n// Hyperdrive jumps.\nconst Hyperdrive res.Feature = \"hyperdrive\"\n",
			},
			wantFlags: []FeatureFlag{
				{File: "pkg/resources/flags.go", Line: 6, Constant: "Hyperdrive", Name: "hyperdrive", Description: "Hyperdrive jumps.", Package: "example.com/acme/beacon/pkg/resources"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			writeTree(t, root, map[string]string{"go.mod": "module example.com/acme/beacon\n\ngo 1.26\n"})
			writeTree(t, root, tt.files)
			a, err := Discover(root)
			if err != nil {
				t.Fatalf("Discover() error = %v", err)
			}
			if diff := cmp.Diff(tt.wantFlags, a.Features, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("Features mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.wantGates, a.FeatureGates, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("FeatureGates mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestFeatureUses(t *testing.T) {
	t.Parallel()

	base := map[string]string{
		"go.mod":                    "module example.com/acme/beacon\n\ngo 1.26\n",
		"pkg/resources/features.go": featureDeclarations,
		"web/angular.json":          "{}\n",
	}
	tests := []struct {
		name  string
		files map[string]string
		want  []FeatureUse
	}{
		{
			name: "no use",
		},
		{
			name: "a qualified read in another package, and one in its test",
			files: map[string]string{
				"app/debriefs.go":      "package app\n\nimport \"example.com/acme/beacon/pkg/resources\"\n\nfunc on(a *App) bool {\n\treturn a.FeatureSet().Enabled(resources.Debriefs)\n}\n\ntype App struct{}\n\nfunc (a *App) FeatureSet() *set { return nil }\n\ntype set struct{}\n\nfunc (s *set) Enabled(f any) bool { return s != nil && f != nil }\n",
				"app/debriefs_test.go": "package app\n\nimport (\n\t\"testing\"\n\n\t\"example.com/acme/beacon/pkg/resources\"\n)\n\nfunc TestOn(t *testing.T) {\n\tt.Log(resources.Debriefs)\n}\n",
			},
			want: []FeatureUse{
				{File: "app/debriefs.go", Line: 6, Constant: "Debriefs"},
				{File: "app/debriefs_test.go", Line: 10, Constant: "Debriefs", Test: true},
			},
		},
		{
			name: "a bare read in the declaring package, the declaration itself not counted",
			files: map[string]string{
				"pkg/resources/defaults.go": "package resources\n\n// DefaultFlag is the flag the console opens on.\nvar DefaultFlag = CargoManifest\n",
			},
			want: []FeatureUse{{File: "pkg/resources/defaults.go", Line: 4, Constant: "CargoManifest"}},
		},
		{
			name: "another package's identifier of the same name is not a read",
			files: map[string]string{
				"pkg/rpc/debriefs.go": "package rpc\n\n// Debriefs is a method, not the flag.\ntype Debriefs struct{}\n",
			},
		},
		{
			name: "browser reads of the generated constant, a spec marked, generated files and other words skipped",
			files: map[string]string{
				"web/console/src/app/nav.ts":              "import { Feature } from './zz_gen_api';\nexport const item = { feature: Feature.Debriefs };\nexport const other = FeatureX.Debriefs;\n",
				"web/console/src/app/nav.html":            "<a *cccFeature=\"Feature.CargoManifest\">Cargo</a>\n",
				"web/console/src/app/nav.spec.ts":         "it('shows', () => expect(Feature.Debriefs).toBe('debriefs'));\n",
				"web/console/src/app/core/zz_gen_api.ts":  "export const Feature = { Debriefs: 'debriefs' as Feature };\n",
				"web/node_modules/left/index.ts":          "Feature.Debriefs\n",
				"docs/notes.ts":                           "Feature.Debriefs outside every browser application\n",
				"web/console/src/app/core/zz_gen_more.ts": "Feature.CargoManifest\n",
			},
			want: []FeatureUse{
				{File: "web/console/src/app/nav.html", Line: 1, Constant: "CargoManifest"},
				{File: "web/console/src/app/nav.spec.ts", Line: 1, Constant: "Debriefs", Test: true},
				{File: "web/console/src/app/nav.ts", Line: 2, Constant: "Debriefs"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			writeTree(t, root, base)
			writeTree(t, root, tt.files)
			a, err := Discover(root)
			if err != nil {
				t.Fatalf("Discover() error = %v", err)
			}
			got, err := a.FeatureUses()
			if err != nil {
				t.Fatalf("FeatureUses() error = %v", err)
			}
			if diff := cmp.Diff(tt.want, got, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("FeatureUses() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestFeatureConstant(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		want string
	}{
		{name: "debriefs", want: "Debriefs"},
		{name: "cargo_manifest", want: "CargoManifest"},
		{name: "api_v2", want: "ApiV2"},
		{name: "a", want: "A"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := FeatureConstant(tt.name); got != tt.want {
				t.Errorf("FeatureConstant(%q) = %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}
