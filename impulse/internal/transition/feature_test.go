package transition

import (
	"os"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/cccteam/ccc/impulse/app"
)

// The files a feature flag's tests start from: a declared flag, a resource it gates,
// a Go read, a browser read, and the seed carrying its row.
const (
	featureSeedUp = featureSeedLead + "\nINSERT INTO FeatureFlags (Name, Description, Enabled, UpdatedAt, UpdatedBy)\n  VALUES ('debriefs', '', FALSE, PENDING_COMMIT_TIMESTAMP(), 'Process devseed');\n"
	featureSeedDn = featureSeedDownLead + "\nDELETE FROM FeatureFlags WHERE Name = 'debriefs';\n"
)

var featureFiles = map[string]string{
	"pkg/resources/features.go":                        "package resources\n\nimport \"github.com/cccteam/ccc/resource\"\n\n// Debriefs lets crews debrief.\nconst Debriefs resource.Feature = \"debriefs\"\n",
	"pkg/resources/debriefs.go":                        "package resources\n\n// Debrief is gated.\n//\n// @resource\n// @feature(Debriefs)\ntype Debrief struct {\n\tID string `spanner:\"Id\"`\n}\n",
	"app/debriefs.go":                                  "package app\n\nimport \"example.com/acme/beacon/pkg/resources\"\n\nvar on = resources.Debriefs\n",
	"web/console/src/app/nav.ts":                       "export const item = { feature: Feature.Debriefs };\n",
	"schema/devseed/000001_dev_tenants.up.sql":         "INSERT INTO Tenants (Id, Name) VALUES ('north', 'North');\n",
	"schema/devseed/000002_dev_feature_flags.up.sql":   featureSeedUp,
	"schema/devseed/000002_dev_feature_flags.down.sql": featureSeedDn,
}

func TestAddFeatureValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		feature AddFeature
		extra   map[string]string
		wantErr string
	}{
		{name: "a new flag", feature: AddFeature{Name: "cargo_manifest"}},
		{name: "a name with a capital", feature: AddFeature{Name: "Cargo"}, wantErr: `feature name "Cargo"`},
		{name: "a name with a dash", feature: AddFeature{Name: "cargo-manifest"}, wantErr: `feature name "cargo-manifest"`},
		{name: "a site on a flat application", feature: AddFeature{Name: "cargo", Site: "console"}, wantErr: "the application is flat"},
		{name: "a flag already declared", feature: AddFeature{Name: "debriefs"}, extra: featureFiles, wantErr: "pkg/resources/features.go:6: Debriefs already declares the flag \"debriefs\""},
		{name: "a name taking a declared constant", feature: AddFeature{Name: "debriefs_"}, extra: featureFiles, wantErr: `"debriefs_" would take the same constant`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			a := beacon(t, tt.extra)
			err := tt.feature.Validate(a)
			switch {
			case tt.wantErr == "" && err != nil:
				t.Errorf("Validate() error = %v", err)
			case tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)):
				t.Errorf("Validate() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestAddFeatureApply(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		feature   AddFeature
		extra     map[string]string
		wantDid   []string
		wantFiles map[string]string
	}{
		{
			name:    "the first flag of an application without a seed",
			feature: AddFeature{Name: "cargo_manifest"},
			wantDid: []string{
				`pkg/resources/features.go: declared CargoManifest ("cargo_manifest") with a doc stub; the comment is the description the flags dialog shows, so replace it`,
				`schema/devseed/000001_dev_feature_flags.up.sql and .down.sql: the development seed's feature flags file, with the "cargo_manifest" row off; set Enabled to TRUE there to start development and the test environments with the feature on`,
				`ran go generate ./..., which listed CargoManifest in Features() and added "cargo_manifest" to the browser's Feature union`,
			},
			wantFiles: map[string]string{
				"pkg/resources/features.go": "package resources\n\nimport \"github.com/cccteam/ccc/resource\"\n\n" + featuresLeadText() + "\n" +
					"// CargoManifest is a feature flag. Replace this sentence with what the feature turns on:\n// the comment is the description the feature flags dialog shows beside its switch.\nconst CargoManifest resource.Feature = \"cargo_manifest\"\n",
				"schema/devseed/000001_dev_feature_flags.up.sql":   featureSeedLead + "\nINSERT INTO FeatureFlags (Name, Description, Enabled, UpdatedAt, UpdatedBy)\n  VALUES ('cargo_manifest', '', FALSE, PENDING_COMMIT_TIMESTAMP(), 'Process devseed');\n",
				"schema/devseed/000001_dev_feature_flags.down.sql": featureSeedDownLead + "\nDELETE FROM FeatureFlags WHERE Name = 'cargo_manifest';\n",
			},
		},
		{
			name:    "a second flag joins the file and the seed",
			feature: AddFeature{Name: "cargo_manifest"},
			extra:   featureFiles,
			wantDid: []string{
				`pkg/resources/features.go: declared CargoManifest ("cargo_manifest") with a doc stub; the comment is the description the flags dialog shows, so replace it`,
				`schema/devseed/000002_dev_feature_flags.up.sql and .down.sql: the "cargo_manifest" row, off; set Enabled to TRUE there to start development and the test environments with the feature on`,
				`ran go generate ./..., which listed CargoManifest in Features() and added "cargo_manifest" to the browser's Feature union`,
			},
			wantFiles: map[string]string{
				"pkg/resources/features.go":                        featureFiles["pkg/resources/features.go"] + "\n// CargoManifest is a feature flag. Replace this sentence with what the feature turns on:\n// the comment is the description the feature flags dialog shows beside its switch.\nconst CargoManifest resource.Feature = \"cargo_manifest\"\n",
				"schema/devseed/000002_dev_feature_flags.up.sql":   featureSeedUp + "\nINSERT INTO FeatureFlags (Name, Description, Enabled, UpdatedAt, UpdatedBy)\n  VALUES ('cargo_manifest', '', FALSE, PENDING_COMMIT_TIMESTAMP(), 'Process devseed');\n",
				"schema/devseed/000002_dev_feature_flags.down.sql": featureSeedDn + "\nDELETE FROM FeatureFlags WHERE Name = 'cargo_manifest';\n",
			},
		},
		{
			name:    "a seed without a feature flags file starts one at the next number",
			feature: AddFeature{Name: "cargo_manifest"},
			extra:   map[string]string{"schema/devseed/000001_dev_tenants.up.sql": "INSERT INTO Tenants (Id, Name) VALUES ('north', 'North');\n"},
			wantDid: []string{
				`pkg/resources/features.go: declared CargoManifest ("cargo_manifest") with a doc stub; the comment is the description the flags dialog shows, so replace it`,
				`schema/devseed/000002_dev_feature_flags.up.sql and .down.sql: the development seed's feature flags file, with the "cargo_manifest" row off; set Enabled to TRUE there to start development and the test environments with the feature on`,
				`ran go generate ./..., which listed CargoManifest in Features() and added "cargo_manifest" to the browser's Feature union`,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			a := beacon(t, tt.extra)
			exec := &fakeExec{}
			change, err := tt.feature.Apply(t.Context(), a, exec)
			if err != nil {
				t.Fatalf("Apply() error = %v", err)
			}
			if diff := cmp.Diff(tt.wantDid, change.Did); diff != "" {
				t.Errorf("Did mismatch (-want +got):\n%s", diff)
			}
			if len(change.Skipped) != 0 {
				t.Errorf("Skipped = %v, want none", change.Skipped)
			}
			if diff := cmp.Diff([]string{"go generate ./..."}, exec.calls); diff != "" {
				t.Errorf("commands mismatch (-want +got):\n%s", diff)
			}
			for rel, want := range tt.wantFiles {
				if diff := cmp.Diff(want, read(t, a, rel)); diff != "" {
					t.Errorf("%s mismatch (-want +got):\n%s", rel, diff)
				}
			}
			// The tree reads back with the flag declared.
			again := mustDiscover(t, a.Root)
			if len(again.FeatureByName(tt.feature.Name)) != 1 {
				t.Errorf("after Apply, %d declaration(s) of %q, want 1", len(again.FeatureByName(tt.feature.Name)), tt.feature.Name)
			}
		})
	}
}

// featuresLeadText is the lead of a new features file, read from the editor's own
// output so the test does not restate it.
func featuresLeadText() string {
	out, err := app.AddFeatureConstant("features.go", nil, "resources", "X", "x")
	if err != nil {
		panic(err)
	}
	_, lead, _ := strings.Cut(string(out), "import \"github.com/cccteam/ccc/resource\"\n\n")
	lead, _, _ = strings.Cut(lead, "\n// X is a feature flag.")

	return lead
}

func TestRemoveFeatureValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		feature RemoveFeature
		extra   map[string]string
		wantErr string
	}{
		{name: "a declared flag", feature: RemoveFeature{Name: "debriefs"}, extra: featureFiles},
		{name: "a flag nothing declares", feature: RemoveFeature{Name: "hyperdrive"}, extra: featureFiles, wantErr: `no resource.Feature constant declares the flag "hyperdrive" (the flags are debriefs)`},
		{name: "no flags at all", feature: RemoveFeature{Name: "hyperdrive"}, wantErr: "(the flags are none)"},
		{name: "a malformed name", feature: RemoveFeature{Name: "Debriefs"}, wantErr: `feature name "Debriefs"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			a := beacon(t, tt.extra)
			err := tt.feature.Validate(a)
			switch {
			case tt.wantErr == "" && err != nil:
				t.Errorf("Validate() error = %v", err)
			case tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)):
				t.Errorf("Validate() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestRemoveFeatureApply(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		extra     map[string]string
		wantDid   []string
		wantUses  []string
		wantGone  []string
		wantFiles map[string]string
	}{
		{
			name:  "the only flag: its file goes, its gate goes, its row goes, its uses are listed",
			extra: featureFiles,
			wantDid: []string{
				`deleted pkg/resources/features.go, which declared Debriefs ("debriefs") and nothing else`,
				"pkg/resources/debriefs.go: removed @feature(Debriefs) from Debrief, which the next regeneration serves unconditionally",
				`schema/devseed/000002_dev_feature_flags.up.sql and .down.sql: removed the "debriefs" row; the table's row goes at the next deploy, when MigrateFeatures deletes what the release no longer declares, and its FeatureFlagChanges rows stay as the audit`,
				`ran go generate ./..., which dropped "debriefs" from Features() and from the browser's Feature union`,
			},
			wantUses: []string{
				"app/debriefs.go:5: resources.Debriefs",
				"web/console/src/app/nav.ts:1: Feature.Debriefs",
			},
			wantGone: []string{"pkg/resources/features.go"},
			wantFiles: map[string]string{
				"pkg/resources/debriefs.go":                        "package resources\n\n// Debrief is gated.\n//\n// @resource\ntype Debrief struct {\n\tID string `spanner:\"Id\"`\n}\n",
				"schema/devseed/000002_dev_feature_flags.up.sql":   strings.TrimSuffix(featureSeedLead, "\n") + "\n",
				"schema/devseed/000002_dev_feature_flags.down.sql": strings.TrimSuffix(featureSeedDownLead, "\n") + "\n",
			},
		},
		{
			name: "one of two flags: the other stays, with the import",
			extra: map[string]string{
				"pkg/resources/features.go": "package resources\n\nimport \"github.com/cccteam/ccc/resource\"\n\n// Debriefs lets crews debrief.\nconst Debriefs resource.Feature = \"debriefs\"\n\n// Hyperdrive jumps.\nconst Hyperdrive resource.Feature = \"hyperdrive\"\n",
			},
			wantDid: []string{
				`pkg/resources/features.go: removed Debriefs ("debriefs")`,
				`ran go generate ./..., which dropped "debriefs" from Features() and from the browser's Feature union`,
			},
			wantFiles: map[string]string{
				"pkg/resources/features.go": "package resources\n\nimport \"github.com/cccteam/ccc/resource\"\n\n// Hyperdrive jumps.\nconst Hyperdrive resource.Feature = \"hyperdrive\"\n",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			a := beacon(t, tt.extra)
			exec := &fakeExec{}
			change, err := RemoveFeature{Name: "debriefs"}.Apply(t.Context(), a, exec)
			if err != nil {
				t.Fatalf("Apply() error = %v", err)
			}
			if diff := cmp.Diff(tt.wantDid, change.Did); diff != "" {
				t.Errorf("Did mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.wantUses, change.Uses, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("Uses mismatch (-want +got):\n%s", diff)
			}
			if change.UsesNote != usesNote {
				t.Errorf("UsesNote = %q", change.UsesNote)
			}
			for _, rel := range tt.wantGone {
				if _, err := os.Stat(a.Abs(rel)); !os.IsNotExist(err) {
					t.Errorf("%s still exists (err %v)", rel, err)
				}
			}
			for rel, want := range tt.wantFiles {
				if diff := cmp.Diff(want, read(t, a, rel)); diff != "" {
					t.Errorf("%s mismatch (-want +got):\n%s", rel, diff)
				}
			}
			if again := mustDiscover(t, a.Root); len(again.FeatureByName("debriefs")) != 0 {
				t.Errorf("after Apply, %q is still declared", "debriefs")
			}
		})
	}
}

func TestFeatureChangeText(t *testing.T) {
	t.Parallel()

	change := Change{Command: "impulse remove feature debriefs", Did: []string{"a"}, Uses: []string{"app/x.go:3: resources.Debriefs"}, UsesNote: "Note."}
	want := "`impulse remove feature debriefs` made these changes and staged them:\n\n- a\n\nNote.\n\n- app/x.go:3: resources.Debriefs\n"
	if diff := cmp.Diff(want, change.Text()); diff != "" {
		t.Errorf("Text() mismatch (-want +got):\n%s", diff)
	}
}

func TestFeatureMeanings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		meaning string
		wants   []string
	}{
		{name: "add", meaning: AddFeature{Name: "cargo_manifest"}.Meaning(), wants: []string{"@feature(CargoManifest)", "Feature.CargoManifest", "SetFeature", "FeatureAdministrator", "openFeatureFlagsDialog", "impulse remove feature cargo_manifest"}},
		{name: "remove", meaning: RemoveFeature{Name: "debriefs"}.Meaning(), wants: []string{"inline the on branch", "MigrateFeatures", "FeatureFlagChanges"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			for _, want := range tt.wants {
				if !strings.Contains(tt.meaning, want) {
					t.Errorf("Meaning() lacks %q", want)
				}
			}
		})
	}
}
