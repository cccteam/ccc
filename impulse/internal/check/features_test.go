package check

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/cccteam/ccc/impulse/app"
)

// featureProgram is a flat application's generator program.
const featureProgram = `package main

import (
	"context"

	"github.com/cccteam/ccc/resource/generation"
)

func run(ctx context.Context) error {
	generator, err := generation.NewResourceGenerator(
		ctx,
		"pkg/resources",
		[]string{"file://schema/migrations"},
		generation.GenerateHandlers("app"),
		generation.GenerateRoutes("pkg/router", "api"),
	)
	if err != nil {
		return err
	}
	defer generator.Close()

	return generator.Generate()
}
`

// featureMigration is the library's statements as the candidates copy them.
const featureMigration = `-- The feature flags.

CREATE TABLE FeatureFlags (
  Name STRING(64) NOT NULL,
  Description STRING(MAX) NOT NULL,
  Enabled BOOL NOT NULL,
  UpdatedAt TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
  UpdatedBy STRING(MAX) NOT NULL,
) PRIMARY KEY (Name);

CREATE TABLE FeatureFlagChanges (
  Name STRING(64) NOT NULL,
  ChangedAt TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
  Enabled BOOL NOT NULL,
  ChangedBy STRING(MAX) NOT NULL,
) PRIMARY KEY (Name, ChangedAt);
`

// The flag declarations and the gate the cases build on.
const (
	featureDeclared = "package resources\n\nimport \"github.com/cccteam/ccc/resource\"\n\n// Debriefs lets crews debrief.\nconst Debriefs resource.Feature = \"debriefs\"\n"
	featureGated    = "package resources\n\n// Debrief is gated.\n//\n// @resource\n// @feature(Debriefs)\ntype Debrief struct {\n\tID string `spanner:\"Id\"`\n}\n"
	featureRead     = "package app\n\nimport \"example.com/harbor/pkg/resources\"\n\nvar on = resources.Debriefs\n"
	goListResource  = "go list -m -f {{.Dir}} github.com/cccteam/ccc/resource"
	goDownload      = "go mod download github.com/cccteam/ccc/resource"
)

// downloadedLater answers go list with no directory until go mod download ran, as a
// module cache that does not hold the resource module yet does.
func downloadedLater(moduleDir string) map[string]fakeAnswer {
	answers := map[string]fakeAnswer{}
	answers[goListResource] = fakeAnswer{out: "\n", run: func() { answers[goListResource] = fakeAnswer{out: moduleDir + "\n"} }}
	answers[goDownload] = fakeAnswer{}

	return answers
}

func TestFeatureFlags(t *testing.T) {
	t.Parallel()

	moduleDir, err := filepath.Abs(filepath.Join("testdata", "resourcemodule"))
	if err != nil {
		t.Fatal(err)
	}
	oldModuleDir, err := filepath.Abs(filepath.Join("testdata", "oldresourcemodule"))
	if err != nil {
		t.Fatal(err)
	}
	generated := map[string]string{
		"cmd/generate/resourcegenerator/main.go":       featureProgram,
		"pkg/resources/doc.go":                         "package resources\n",
		"pkg/resources/zz_gen_features.go":             "package resources\n",
		"schema/migrations/000005_FeatureFlags.up.sql": featureMigration,
		"web/angular.json":                             "{}\n",
	}
	with := func(files map[string]string, extra map[string]string) map[string]string {
		all := map[string]string{}
		for k, v := range files {
			all[k] = v
		}
		for k, v := range extra {
			all[k] = v
		}

		return all
	}
	without := func(files map[string]string, rel string) map[string]string {
		all := with(files, nil)
		delete(all, rel)

		return all
	}
	differing := strings.Replace(featureMigration, "UpdatedBy STRING(MAX) NOT NULL,", "UpdatedBy STRING(64) NOT NULL,", 1)
	matchSource := "resourcemodule/feature.go"

	tests := []struct {
		name        string
		files       map[string]string
		answers     map[string]fakeAnswer
		wantStatus  Status
		wantSummary string
		wantDetails []string
		wantCalls   []string
	}{
		{
			name:       "no site generator",
			files:      map[string]string{"pkg/resources/doc.go": "package resources\n"},
			wantStatus: Skip, wantSummary: "no site generator",
		},
		{
			name:       "the generator emits no feature flags and none is declared",
			files:      without(generated, "pkg/resources/zz_gen_features.go"),
			wantStatus: Skip, wantSummary: "the generator emits no feature flags (no zz_gen_features.go in a resources package)",
		},
		{
			name:       "a flag declared where the generator emits none",
			files:      with(without(generated, "pkg/resources/zz_gen_features.go"), map[string]string{"pkg/resources/features.go": featureDeclared}),
			wantStatus: Fail, wantSummary: "1 feature flag problem(s)",
			wantDetails: []string{
				`pkg/resources/features.go:6: Debriefs ("debriefs") is declared, but the generator emits no feature flags (no zz_gen_features.go in pkg/resources); move the resource pin to a version that carries them and regenerate`,
			},
		},
		{
			name:       "the tables match and no flag is declared",
			files:      generated,
			answers:    map[string]fakeAnswer{goListResource: {out: moduleDir + "\n"}},
			wantStatus: Pass, wantSummary: "FeatureFlags and FeatureFlagChanges match the resource module's statements; no flag declared",
			wantCalls: []string{goListResource},
		},
		{
			name:       "a module the cache does not hold yet is downloaded first",
			files:      generated,
			answers:    downloadedLater(moduleDir),
			wantStatus: Pass, wantSummary: "FeatureFlags and FeatureFlagChanges match the resource module's statements; no flag declared",
			wantCalls: []string{goListResource, goDownload, goListResource},
		},
		{
			name:       "the migration is missing",
			files:      without(generated, "schema/migrations/000005_FeatureFlags.up.sql"),
			answers:    map[string]fakeAnswer{goListResource: {out: moduleDir + "\n"}},
			wantStatus: Fail, wantSummary: "2 feature flag problem(s)",
			wantDetails: []string{
				"no migration creates the FeatureFlagChanges table, which the generated feature flag routes and the deploy's MigrateFeatures read; copy its statement from resource.FeatureFlagsDDL(resource.SpannerDBType) (" + matchSource + ") into the next migration under schema/migrations",
				"no migration creates the FeatureFlags table, which the generated feature flag routes and the deploy's MigrateFeatures read; copy its statement from resource.FeatureFlagsDDL(resource.SpannerDBType) (" + matchSource + ") into the next migration under schema/migrations",
			},
			wantCalls: []string{goListResource},
		},
		{
			name:       "a table differs from the library's statement",
			files:      with(generated, map[string]string{"schema/migrations/000005_FeatureFlags.up.sql": differing}),
			answers:    map[string]fakeAnswer{goListResource: {out: moduleDir + "\n"}},
			wantStatus: Fail, wantSummary: "1 feature flag problem(s)",
			wantDetails: []string{
				"schema/migrations/000005_FeatureFlags.up.sql: CREATE TABLE FeatureFlags differs from resource.FeatureFlagsDDL(resource.SpannerDBType) in " + matchSource + "; the library reads the table by those columns, so bring it to the statement in a new migration",
			},
			wantCalls: []string{goListResource},
		},
		{
			name:       "the resource module cannot be found",
			files:      generated,
			answers:    map[string]fakeAnswer{goListResource: {out: "go: module github.com/cccteam/ccc/resource: not a known dependency\n", err: errExit}},
			wantStatus: Fail, wantSummary: "1 feature flag problem(s)",
			wantDetails: []string{
				"the resource module's source could not be found (go list -m -f {{.Dir}} github.com/cccteam/ccc/resource): go: module github.com/cccteam/ccc/resource: not a known dependency",
			},
			wantCalls: []string{goListResource},
		},
		{
			name:       "the resource module predates feature flags",
			files:      generated,
			answers:    map[string]fakeAnswer{goListResource: {out: oldModuleDir + "\n"}},
			wantStatus: Fail, wantSummary: "1 feature flag problem(s)",
			wantDetails: []string{
				"the resource module at " + oldModuleDir + " declares no FeatureFlagsDDL: its version predates feature flags, so the schema cannot be compared with it",
			},
			wantCalls: []string{goListResource},
		},
		{
			name:       "a flag that gates nothing and is read nowhere",
			files:      with(generated, map[string]string{"pkg/resources/features.go": featureDeclared}),
			answers:    map[string]fakeAnswer{goListResource: {out: moduleDir + "\n"}},
			wantStatus: Fail, wantSummary: "1 feature flag problem(s)",
			wantDetails: []string{
				`pkg/resources/features.go:6: Debriefs ("debriefs") gates nothing and is read nowhere: put @feature(Debriefs) on a resource, a field or a method, read it (a.FeatureSet().Enabled(resources.Debriefs) in Go, Feature.Debriefs in the browser), or remove it (impulse remove feature debriefs)`,
			},
			wantCalls: []string{goListResource},
		},
		{
			name:       "a flag read only in a test is read nowhere",
			files:      with(generated, map[string]string{"pkg/resources/features.go": featureDeclared, "app/on_test.go": strings.Replace(featureRead, "package app", "package app_test", 1)}),
			answers:    map[string]fakeAnswer{goListResource: {out: moduleDir + "\n"}},
			wantStatus: Fail, wantSummary: "1 feature flag problem(s)",
			wantDetails: []string{
				`pkg/resources/features.go:6: Debriefs ("debriefs") gates nothing and is read nowhere: put @feature(Debriefs) on a resource, a field or a method, read it (a.FeatureSet().Enabled(resources.Debriefs) in Go, Feature.Debriefs in the browser), or remove it (impulse remove feature debriefs)`,
			},
			wantCalls: []string{goListResource},
		},
		{
			name:        "a gated flag",
			files:       with(generated, map[string]string{"pkg/resources/features.go": featureDeclared, "pkg/resources/debriefs.go": featureGated}),
			answers:     map[string]fakeAnswer{goListResource: {out: moduleDir + "\n"}},
			wantStatus:  Pass,
			wantSummary: "FeatureFlags and FeatureFlagChanges match the resource module's statements; 1 flag(s) declared, each gating or read",
			wantDetails: []string{"Debriefs (debriefs): 1 gate(s), 0 read(s)"},
			wantCalls:   []string{goListResource},
		},
		{
			name:        "a flag read in Go",
			files:       with(generated, map[string]string{"pkg/resources/features.go": featureDeclared, "app/on.go": featureRead}),
			answers:     map[string]fakeAnswer{goListResource: {out: moduleDir + "\n"}},
			wantStatus:  Pass,
			wantSummary: "FeatureFlags and FeatureFlagChanges match the resource module's statements; 1 flag(s) declared, each gating or read",
			wantDetails: []string{"Debriefs (debriefs): 0 gate(s), 1 read(s)"},
			wantCalls:   []string{goListResource},
		},
		{
			name:        "a flag read in the browser",
			files:       with(generated, map[string]string{"pkg/resources/features.go": featureDeclared, "web/console/src/app/nav.ts": "export const item = { feature: Feature.Debriefs };\n"}),
			answers:     map[string]fakeAnswer{goListResource: {out: moduleDir + "\n"}},
			wantStatus:  Pass,
			wantSummary: "FeatureFlags and FeatureFlagChanges match the resource module's statements; 1 flag(s) declared, each gating or read",
			wantDetails: []string{"Debriefs (debriefs): 0 gate(s), 1 read(s)"},
			wantCalls:   []string{goListResource},
		},
		{
			name: "a name declared twice, and an annotation naming no constant",
			files: with(generated, map[string]string{
				"pkg/resources/features.go": featureDeclared,
				"pkg/resources/again.go":    "package resources\n\nimport \"github.com/cccteam/ccc/resource\"\n\n// DebriefsAgain repeats the name.\nconst DebriefsAgain resource.Feature = \"debriefs\"\n",
				"pkg/resources/debriefs.go": strings.Replace(featureGated, "@feature(Debriefs)", "@feature(Hyperdrive)", 1),
				"app/on.go":                 featureRead,
			}),
			answers:    map[string]fakeAnswer{goListResource: {out: moduleDir + "\n"}},
			wantStatus: Fail, wantSummary: "3 feature flag problem(s)",
			wantDetails: []string{
				`pkg/resources/again.go:6: DebriefsAgain ("debriefs") gates nothing and is read nowhere: put @feature(DebriefsAgain) on a resource, a field or a method, read it (a.FeatureSet().Enabled(resources.DebriefsAgain) in Go, Feature.DebriefsAgain in the browser), or remove it (impulse remove feature debriefs)`,
				`pkg/resources/features.go:6: Debriefs declares the flag "debriefs", which DebriefsAgain already declares at pkg/resources/again.go:6; MigrateFeatures refuses two declarations of one name`,
				"pkg/resources/debriefs.go:6: @feature(Hyperdrive) on Debrief names no resource.Feature constant the application declares; the generator refuses it",
			},
			wantCalls: []string{goListResource},
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
			exec := &fakeExec{t: t, answers: tt.answers}
			got := featureFlags{}.Run(context.Background(), &Env{App: a, Exec: exec})
			want := Result{Name: "feature-flags", Status: tt.wantStatus, Summary: tt.wantSummary, Details: tt.wantDetails}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("Run() mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.wantCalls, exec.calls); diff != "" {
				t.Errorf("commands mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestFeatureFlagsMeaning(t *testing.T) {
	t.Parallel()

	meaning := Meaning(featureFlagsName)
	for _, want := range []string{"resource.Feature", "@feature(Debriefs)", "impulse remove feature", "resource.FeatureFlagsDDL(resource.SpannerDBType)"} {
		if !strings.Contains(meaning, want) {
			t.Errorf("Meaning() lacks %q", want)
		}
	}
}
