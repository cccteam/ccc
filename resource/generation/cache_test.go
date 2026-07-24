package generation

import (
	"strings"
	"testing"
)

func matchedTypescriptRunConfig() typescriptRunConfig {
	return typescriptRunConfig{
		TargetDir:           "gui/src/app/core/service",
		GenMetadata:         true,
		GenPermission:       true,
		GenEnums:            true,
		TypescriptOverrides: defaultTypescriptOverrides(),
		VirtualDir:          "pkg/virtualresources",
		ComputedDir:         "pkg/computedresources",
		RPCDir:              "pkg/rpc",
		PluralOverrides:     map[string]string{"Person": "People"},
	}
}

func Test_typescriptRunConfig_diff(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		mutate      func(*typescriptRunConfig)
		wantSetting string
	}{
		{name: "identical", mutate: func(*typescriptRunConfig) {}},
		{name: "target dir", mutate: func(c *typescriptRunConfig) { c.TargetDir = "elsewhere" }, wantSetting: "TypeScript target directory"},
		{name: "metadata", mutate: func(c *typescriptRunConfig) { c.GenMetadata = false }, wantSetting: "GenerateMetadata"},
		{name: "permissions", mutate: func(c *typescriptRunConfig) { c.GenPermission = false }, wantSetting: "GeneratePermissions"},
		{name: "enums", mutate: func(c *typescriptRunConfig) { c.GenEnums = false }, wantSetting: "GenerateEnums"},
		{name: "virtual dir", mutate: func(c *typescriptRunConfig) { c.VirtualDir = "" }, wantSetting: "WithVirtualResources"},
		{name: "computed dir", mutate: func(c *typescriptRunConfig) { c.ComputedDir = "" }, wantSetting: "WithComputedResources"},
		{name: "rpc dir", mutate: func(c *typescriptRunConfig) { c.RPCDir = "" }, wantSetting: "WithRPC"},
		{
			name:        "typescript overrides",
			mutate:      func(c *typescriptRunConfig) { c.TypescriptOverrides = map[string]string{"x": "y"} },
			wantSetting: "WithTypescriptOverrides",
		},
		{
			name:        "plural overrides",
			mutate:      func(c *typescriptRunConfig) { c.PluralOverrides = nil },
			wantSetting: "WithPluralOverrides",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			base := matchedTypescriptRunConfig()
			other := matchedTypescriptRunConfig()
			tt.mutate(&other)

			diffs := base.diff(&other)
			if tt.wantSetting == "" {
				if len(diffs) != 0 {
					t.Fatalf("diff() = %v, want empty", diffs)
				}

				return
			}
			if len(diffs) != 1 {
				t.Fatalf("diff() = %v, want exactly one difference", diffs)
			}
			if !strings.HasPrefix(diffs[0], tt.wantSetting+":") {
				t.Errorf("diff()[0] = %q, want prefix %q", diffs[0], tt.wantSetting+":")
			}
		})
	}
}

// Test_typescriptMarker_configFor pins the per-destination handoff lookup: the marker
// holds one configuration per GenerateTypescript target directory, matched on the
// cleaned path, and directories the Resource Generator did not emit to return nil (the
// deprecated generator then keeps its legacy path for them).
func Test_typescriptMarker_configFor(t *testing.T) {
	t.Parallel()

	marker := typescriptMarker{Configs: []typescriptRunConfig{
		{TargetDir: "apps/admin/gui/src"},
		{TargetDir: "apps/portal/gui/src"},
	}}

	tests := []struct {
		name          string
		marker        typescriptMarker
		dir           string
		wantTargetDir string // "" means no configuration (nil)
	}{
		{name: "exact match", marker: marker, dir: "apps/portal/gui/src", wantTargetDir: "apps/portal/gui/src"},
		{name: "cleaned-path match", marker: marker, dir: "apps/admin/gui/src/", wantTargetDir: "apps/admin/gui/src"},
		{name: "unemitted destination", marker: marker, dir: "apps/partner/gui/src"},
		{name: "empty marker", marker: typescriptMarker{}, dir: "apps/admin/gui/src"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := tt.marker.configFor(tt.dir)
			if tt.wantTargetDir == "" {
				if got != nil {
					t.Errorf("configFor(%q) = %v, want nil", tt.dir, got)
				}

				return
			}
			if got == nil || got.TargetDir != tt.wantTargetDir {
				t.Errorf("configFor(%q) = %v, want the %q configuration", tt.dir, got, tt.wantTargetDir)
			}
		})
	}
}

// Test_deprecatedTypescriptRunConfig pins that the deprecated generator's options
// resolve into the same comparable configuration shape the Resource Generator records,
// including the default TypeScript override map when none is supplied.
func Test_deprecatedTypescriptRunConfig(t *testing.T) {
	t.Parallel()

	got, err := deprecatedTypescriptRunConfig("gui/src/app/core/service", []option{
		GenerateMetadata(),
		GeneratePermissions(),
		GenerateEnums(),
		WithVirtualResources("pkg/virtualresources"),
		WithComputedResources("pkg/computedresources"),
		WithRPC("pkg/rpc"),
		WithPluralOverrides(map[string]string{"Person": "People"}),
	})
	if err != nil {
		t.Fatalf("deprecatedTypescriptRunConfig() error = %v", err)
	}

	want := matchedTypescriptRunConfig()
	if diffs := want.diff(&got); len(diffs) != 0 {
		t.Errorf("deprecatedTypescriptRunConfig() differs from expected configuration:\n  %s", strings.Join(diffs, "\n  "))
	}
}
