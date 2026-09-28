package hook

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func TestScripts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		files []string
		dirs  []string
		want  []Stage
	}{
		{name: "no hooks directory"},
		{name: "the stages with a script, in the pipeline's order", files: []string{"after-traffic.sh", "after-migrate.sh", "notes.txt", "later.sh"}, want: []Stage{AfterMigrate, AfterTraffic}},
		{name: "a directory named like a script is not one", dirs: []string{"before-build.sh"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			dir := filepath.Join(root, filepath.FromSlash(Dir))
			for _, d := range tt.dirs {
				if err := os.MkdirAll(filepath.Join(dir, d), 0o750); err != nil {
					t.Fatal(err)
				}
			}
			for _, f := range tt.files {
				if err := os.MkdirAll(dir, 0o750); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, f), []byte("echo\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			got, err := Scripts(root)
			if err != nil {
				t.Fatalf("Scripts() error = %v", err)
			}
			if diff := cmp.Diff(tt.want, got, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("Scripts() (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		stage      Stage
		wantValid  bool
		wantScript string
	}{
		{stage: BeforeBuild, wantValid: true, wantScript: "infrastructure/hooks/before-build.sh"},
		{stage: AfterDown, wantValid: true, wantScript: "infrastructure/hooks/after-down.sh"},
		{stage: "during-lunch", wantScript: "infrastructure/hooks/during-lunch.sh"},
	}
	for _, tt := range tests {
		t.Run(string(tt.stage), func(t *testing.T) {
			t.Parallel()

			if got := tt.stage.Valid(); got != tt.wantValid {
				t.Errorf("Valid() = %t, want %t", got, tt.wantValid)
			}
			if got := tt.stage.Script(); got != tt.wantScript {
				t.Errorf("Script() = %q, want %q", got, tt.wantScript)
			}
		})
	}
}
