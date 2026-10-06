package check

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cccteam/ccc/bedrock/internal/derive"
)

// TestScanMaintenance is the check's warnings about the maintenance windows: production
// without a setting, a dated slot that has passed, and the release file missing,
// malformed or with no router to hold it.
func TestScanMaintenance(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 5, 15, 0, 0, 0, time.UTC)
	chicago := func(weekly bool, dates ...derive.DatedSlot) derive.MaintenanceWindow {
		w := derive.MaintenanceWindow{TimeZone: "America/Chicago", Dates: dates}
		if weekly {
			w.Weekly = []derive.WeeklySlot{{Day: "Sunday", From: "02:00", To: "04:00"}}
		}

		return w
	}
	tests := []struct {
		name string
		// maintenance is the placement's block; routerDir the model's router package;
		// releaseFile the file's content, written under appDir when set.
		maintenance map[string]derive.MaintenanceWindow
		routerDir   string
		releaseFile string
		want        []string
	}{
		{
			name:      "production without a setting and no release file earn two warnings",
			routerDir: "pkg/router",
			want: []string{
				"prd has no maintenance setting (placement.json \"maintenance\": {\"prd\": ...}): a breaking release to prd is refused at the start of its run until one is written; \"anytime\" is a setting, and so are the client's windows",
				"no release file at pkg/router/zz_gen_release.json: no outlet declares an oldest answered release, so no release is breaking and the maintenance window never holds a run; the resource generator writes it beside the generated router (go generate ./...)",
			},
		},
		{
			name:        "production set and the release file in place earn none",
			maintenance: map[string]derive.MaintenanceWindow{"prd": {Anytime: true}},
			routerDir:   "pkg/router",
			releaseFile: `{"outlets": {"default": {"oldestAnswered": "1.5.0"}}}`,
		},
		{
			name:        "a dated slot that has passed is named",
			maintenance: map[string]derive.MaintenanceWindow{"prd": chicago(true, derive.DatedSlot{On: "2026-09-15", From: "22:00", To: "23:30"}, derive.DatedSlot{On: "2026-11-15", From: "22:00", To: "23:30"})},
			routerDir:   "pkg/router",
			releaseFile: `{"outlets": {"default": {"oldestAnswered": ""}}}`,
			want:        []string{"maintenance.prd: the dated slot on 2026-09-15 (22:00 to 23:30) has passed; remove it"},
		},
		{
			name:        "a release file that does not read is a warning here and a refusal in the run",
			maintenance: map[string]derive.MaintenanceWindow{"prd": {Anytime: true}},
			routerDir:   "pkg/router",
			releaseFile: `{"outlets": {"default": {"oldestAnswered": "soon"}}}`,
			want:        []string{`the release file does not read, and the release check will refuse the run: `},
		},
		{
			name:        "no router directory means no release file to read",
			maintenance: map[string]derive.MaintenanceWindow{"prd": {Anytime: true}},
			want:        []string{"the site generator declares no routes directory (GenerateRoutes), so there is no release file to read and no outlet declares an oldest answered release: no release is breaking, and the maintenance window never holds a run"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			appDir := t.TempDir()
			if tt.releaseFile != "" {
				dir := filepath.Join(appDir, filepath.FromSlash(tt.routerDir))
				if err := os.MkdirAll(dir, 0o750); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, derive.ReleaseFileName), []byte(tt.releaseFile), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			m := &derive.Model{RouterDir: tt.routerDir, Placement: &derive.Placement{Environments: []string{"tst", "stg", "prd"}, Maintenance: tt.maintenance}}
			findings := scanMaintenance(m, appDir, now)
			if len(findings) != len(tt.want) {
				t.Fatalf("scanMaintenance() = %+v, want %d finding(s): %v", findings, len(tt.want), tt.want)
			}
			for i, want := range tt.want {
				if !strings.Contains(findings[i].Problem, want) {
					t.Errorf("finding %d = %q, want it to contain %q", i, findings[i].Problem, want)
				}
			}
		})
	}
}
