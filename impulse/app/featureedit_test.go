package app

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestAddFeatureConstant(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "a new file, with the lead and the import",
			want: "package resources\n\nimport \"github.com/cccteam/ccc/resource\"\n\n" + featuresFileLead + "\n" +
				"// Debriefs is a feature flag. Replace this sentence with what the feature turns on:\n// the comment is the description the feature flags dialog shows beside its switch.\nconst Debriefs resource.Feature = \"debriefs\"\n",
		},
		{
			name: "appended to a file that declares one already",
			src:  "package resources\n\nimport \"github.com/cccteam/ccc/resource\"\n\n// Hyperdrive jumps.\nconst Hyperdrive resource.Feature = \"hyperdrive\"\n",
			want: "package resources\n\nimport \"github.com/cccteam/ccc/resource\"\n\n// Hyperdrive jumps.\nconst Hyperdrive resource.Feature = \"hyperdrive\"\n\n" +
				"// Debriefs is a feature flag. Replace this sentence with what the feature turns on:\n// the comment is the description the feature flags dialog shows beside its switch.\nconst Debriefs resource.Feature = \"debriefs\"\n",
		},
		{
			name: "appended to a file without the import, which gains it",
			src:  "package resources\n\n// Kind names a kind.\ntype Kind string\n",
			want: "package resources\n\nimport \"github.com/cccteam/ccc/resource\"\n\n// Kind names a kind.\ntype Kind string\n\n" +
				"// Debriefs is a feature flag. Replace this sentence with what the feature turns on:\n// the comment is the description the feature flags dialog shows beside its switch.\nconst Debriefs resource.Feature = \"debriefs\"\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := AddFeatureConstant("pkg/resources/features.go", []byte(tt.src), "resources", "Debriefs", "debriefs")
			if err != nil {
				t.Fatalf("AddFeatureConstant() error = %v", err)
			}
			if diff := cmp.Diff(tt.want, string(got)); diff != "" {
				t.Errorf("AddFeatureConstant() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestRemoveFeatureConstant(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		src       string
		constant  string
		want      string
		wantEmpty bool
		wantErr   string
	}{
		{
			name:      "the only declaration leaves an empty file",
			src:       "package resources\n\nimport \"github.com/cccteam/ccc/resource\"\n\n// The flags.\n\n// Debriefs lets crews debrief.\nconst Debriefs resource.Feature = \"debriefs\"\n",
			constant:  "Debriefs",
			wantEmpty: true,
		},
		{
			name:     "one of two standalone declarations, with its doc comment",
			src:      "package resources\n\nimport \"github.com/cccteam/ccc/resource\"\n\n// Debriefs lets crews debrief.\nconst Debriefs resource.Feature = \"debriefs\"\n\n// Hyperdrive jumps.\nconst Hyperdrive resource.Feature = \"hyperdrive\"\n",
			constant: "Debriefs",
			want:     "package resources\n\nimport \"github.com/cccteam/ccc/resource\"\n\n// Hyperdrive jumps.\nconst Hyperdrive resource.Feature = \"hyperdrive\"\n",
		},
		{
			name:     "a spec of a group, with its doc and line comment",
			src:      "package resources\n\nimport \"github.com/cccteam/ccc/resource\"\n\nconst (\n\t// Debriefs lets crews debrief.\n\tDebriefs resource.Feature = \"debriefs\" // on\n\t// Hyperdrive jumps.\n\tHyperdrive resource.Feature = \"hyperdrive\"\n)\n",
			constant: "Debriefs",
			want:     "package resources\n\nimport \"github.com/cccteam/ccc/resource\"\n\nconst (\n\t// Hyperdrive jumps.\n\tHyperdrive resource.Feature = \"hyperdrive\"\n)\n",
		},
		{
			name:     "the last flag beside another declaration drops the unused import",
			src:      "package resources\n\nimport (\n\t\"github.com/cccteam/ccc/resource\"\n)\n\n// Debriefs lets crews debrief.\nconst Debriefs resource.Feature = \"debriefs\"\n\n// Kind names a kind.\ntype Kind string\n",
			constant: "Debriefs",
			want:     "package resources\n\n// Kind names a kind.\ntype Kind string\n",
		},
		{
			name:     "the import stays while another flag reads it",
			src:      "package resources\n\nimport (\n\t\"fmt\"\n\n\t\"github.com/cccteam/ccc/resource\"\n)\n\n// Debriefs lets crews debrief.\nconst Debriefs resource.Feature = \"debriefs\"\n\n// Hyperdrive jumps.\nconst Hyperdrive resource.Feature = \"hyperdrive\"\n\nfunc say() { fmt.Println(Hyperdrive) }\n",
			constant: "Debriefs",
			want:     "package resources\n\nimport (\n\t\"fmt\"\n\n\t\"github.com/cccteam/ccc/resource\"\n)\n\n// Hyperdrive jumps.\nconst Hyperdrive resource.Feature = \"hyperdrive\"\n\nfunc say() { fmt.Println(Hyperdrive) }\n",
		},
		{
			name:     "a constant the file does not declare",
			src:      "package resources\n\nimport \"github.com/cccteam/ccc/resource\"\n\nconst Hyperdrive resource.Feature = \"hyperdrive\"\n",
			constant: "Debriefs",
			wantErr:  "declares no constant Debriefs",
		},
		{
			name:     "a constant declared beside others in one spec",
			src:      "package resources\n\nimport \"github.com/cccteam/ccc/resource\"\n\nconst Debriefs, Hyperdrive resource.Feature = \"debriefs\", \"hyperdrive\"\n",
			constant: "Debriefs",
			wantErr:  "declared beside 1 other name(s)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, empty, err := RemoveFeatureConstant("pkg/resources/features.go", []byte(tt.src), tt.constant)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("RemoveFeatureConstant() error = %v, want %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("RemoveFeatureConstant() error = %v", err)
			}
			if empty != tt.wantEmpty {
				t.Errorf("RemoveFeatureConstant() empty = %v, want %v", empty, tt.wantEmpty)
			}
			if tt.wantEmpty {
				return
			}
			if diff := cmp.Diff(tt.want, string(got)); diff != "" {
				t.Errorf("RemoveFeatureConstant() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestRemoveFeatureAnnotations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		src         string
		want        string
		wantRemoved int
	}{
		{
			name:        "a line of its own goes whole; a shared line keeps its other text",
			src:         "package resources\n\ntype (\n\t// Debrief is gated.\n\t//\n\t// @resource\n\t// @feature(Debriefs)\n\tDebrief struct {\n\t\tID string\n\t\t// @feature( Debriefs ) @outlet(portal)\n\t\tBody string\n\t\tNotes string // @feature(Debriefs)\n\t}\n)\n",
			want:        "package resources\n\ntype (\n\t// Debrief is gated.\n\t//\n\t// @resource\n\tDebrief struct {\n\t\tID string\n\t\t// @outlet(portal)\n\t\tBody  string\n\t\tNotes string //\n\t}\n)\n",
			wantRemoved: 3,
		},
		{
			name: "another flag's annotation stays",
			src:  "package resources\n\n// Ship is gated.\n// @feature(CargoManifest)\ntype Ship struct{}\n",
			want: "package resources\n\n// Ship is gated.\n// @feature(CargoManifest)\ntype Ship struct{}\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, removed, err := RemoveFeatureAnnotations("pkg/resources/ships.go", []byte(tt.src), "Debriefs")
			if err != nil {
				t.Fatalf("RemoveFeatureAnnotations() error = %v", err)
			}
			if removed != tt.wantRemoved {
				t.Errorf("removed = %d, want %d", removed, tt.wantRemoved)
			}
			if diff := cmp.Diff(tt.want, string(got)); diff != "" {
				t.Errorf("RemoveFeatureAnnotations() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
