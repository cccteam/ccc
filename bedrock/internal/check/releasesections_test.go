package check

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/cccteam/ccc/impulse/ci"
)

// seededConfig is the configuration render seeds, read from the harbor golden.
func seededConfig(t *testing.T) string {
	t.Helper()

	src, err := os.ReadFile(filepath.Join(golden, "root", releasePleaseConfig))
	if err != nil {
		t.Fatalf("reading the seeded configuration: %v", err)
	}

	return string(src)
}

func TestScanReleaseSections(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		config  func(t *testing.T) string
		noFile  bool
		want    []ReleaseSectionFinding
		wantErr bool
	}{
		{
			name:   "the seeded configuration passes",
			config: seededConfig,
		},
		{
			name: "a type removed is refused by name",
			config: func(t *testing.T) string {
				t.Helper()

				return strings.Replace(seededConfig(t), "    { \"type\": \"upgrade\", \"section\": \"Upgrades\" },\n", "", 1)
			},
			want: []ReleaseSectionFinding{{Path: "release-please-config.json", Missing: []string{"upgrade"}}},
		},
		{
			name: "a type hidden passes",
			config: func(t *testing.T) string {
				t.Helper()

				return strings.Replace(seededConfig(t), "{ \"type\": \"upgrade\", \"section\": \"Upgrades\" }", "{ \"type\": \"upgrade\", \"section\": \"Upgrades\", \"hidden\": true }", 1)
			},
		},
		{
			name: "several types removed are named in the title check's order",
			config: func(*testing.T) string {
				return `{"release-type": "go", "changelog-sections": [{"type": "feat", "section": "Features"}, {"type": "fix", "section": "Bug Fixes"}], "packages": {".": {}}}`
			},
			want: []ReleaseSectionFinding{{Path: "release-please-config.json", Missing: []string{"feature", "perf", "revert", "docs", "deps", "upgrade", "infra", "config", "style", "chore", "refactor", "cleanup", "test", "build", "ci"}}},
		},
		{
			name: "no changelog-sections is release-please's default list, which lacks the types outside it",
			config: func(*testing.T) string {
				return `{"release-type": "go", "packages": {".": {}}}`
			},
			want: []ReleaseSectionFinding{{Path: "release-please-config.json", Missing: []string{"deps", "upgrade", "infra", "config", "cleanup"}}},
		},
		{
			name: "a package's own list is checked by its name; one without inherits the top level",
			config: func(*testing.T) string {
				return `{"release-type": "go", "changelog-sections": [` + allSections() + `], "packages": {"site": {"changelog-sections": [{"type": "feat", "section": "Features"}]}, "api": {}}}`
			},
			want: []ReleaseSectionFinding{{Path: "release-please-config.json", Package: "site", Missing: []string{"feature", "fix", "perf", "revert", "docs", "deps", "upgrade", "infra", "config", "style", "chore", "refactor", "cleanup", "test", "build", "ci"}}},
		},
		{
			name:   "no configuration checks nothing",
			noFile: true,
		},
		{
			name:    "a configuration that is not JSON is an error",
			config:  func(*testing.T) string { return `{"release-type": "go",` },
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			if !tt.noFile {
				if err := os.WriteFile(filepath.Join(dir, releasePleaseConfig), []byte(tt.config(t)), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			got, err := scanReleaseSections(dir)
			if (err != nil) != tt.wantErr {
				t.Fatalf("scanReleaseSections() error = %v, wantErr %t", err, tt.wantErr)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("findings mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// allSections is a changelog-sections body with one entry per accepted type.
func allSections() string {
	entries := make([]string, len(ci.TitleTypes))
	for i, typ := range ci.TitleTypes {
		entries[i] = `{"type": "` + typ + `", "section": "` + typ + `"}`
	}

	return strings.Join(entries, ", ")
}

func TestReleaseSectionProblem(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		finding ReleaseSectionFinding
		want    string
	}{
		{
			name:    "one type at the top level",
			finding: ReleaseSectionFinding{Missing: []string{"upgrade"}},
			want:    "changelog-sections has no entry for `upgrade`: release-please drops a merge",
		},
		{
			name:    "three types in a package",
			finding: ReleaseSectionFinding{Package: "site", Missing: []string{"upgrade", "infra", "config"}},
			want:    "package site's changelog-sections has no entry for `upgrade`, `infra` and `config`: release-please drops a merge",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.finding.Problem(); !strings.HasPrefix(got, tt.want) {
				t.Errorf("Problem() = %q, want a prefix %q", got, tt.want)
			}
		})
	}
}
