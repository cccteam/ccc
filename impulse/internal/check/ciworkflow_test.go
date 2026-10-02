package check

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/cccteam/ccc/impulse/app"
	"github.com/cccteam/ccc/impulse/ci"
)

// TestCIWorkflow runs the owned-file comparison over an application with one browser
// workspace: the file as the code renders it, a hand edit, and no file at all.
func TestCIWorkflow(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// prepare leaves the application's workflow in the state under test.
		prepare func(t *testing.T, a *app.App)
		want    Result
	}{
		{
			name: "the file as the code renders it passes",
			prepare: func(t *testing.T, a *app.App) {
				t.Helper()
				if _, err := ci.Write(a); err != nil {
					t.Fatal(err)
				}
			},
			want: Result{Name: ciWorkflow{}.Name(), Status: Pass, Summary: ".github/workflows/ci.yml matches what impulse renders from the code (6 job(s): title, go, angular-web, image, secrets, migrations)"},
		},
		{
			name:    "a missing file fails: no checks run",
			prepare: func(t *testing.T, _ *app.App) { t.Helper() },
			want: Result{Name: ciWorkflow{}.Name(), Status: Fail, Summary: ".github/workflows/ci.yml is missing: the pull requests run no checks", Details: []string{
				"run impulse render (go tool impulse render) to write it from the code",
			}},
		},
		{
			name: "a hand edit fails at its first line",
			prepare: func(t *testing.T, a *app.App) {
				t.Helper()
				if _, err := ci.Write(a); err != nil {
					t.Fatal(err)
				}
				data, err := os.ReadFile(a.Abs(ci.File))
				if err != nil {
					t.Fatal(err)
				}
				edited := strings.Replace(string(data), "      - run: bun run test\n", "", 1)
				if err := os.WriteFile(a.Abs(ci.File), []byte(edited), 0o600); err != nil {
					t.Fatal(err)
				}
			},
			want: Result{Name: ciWorkflow{}.Name(), Status: Fail, Summary: ".github/workflows/ci.yml differs from what impulse renders from the code", Details: []string{
				`.github/workflows/ci.yml:133: the code renders "      - run: bun run test"; the file has ""`,
				"run impulse render (go tool impulse render) to rewrite it; the file is impulse's: change the code or impulse, not the file",
			}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			for rel, content := range map[string]string{
				"go.mod":           "module example.com/beacon\n\ngo 1.26.6\n",
				"web/angular.json": "{\n  \"version\": 1,\n  \"projects\": {}\n}\n",
			} {
				if err := os.MkdirAll(filepath.Dir(filepath.Join(root, rel)), 0o750); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, rel), []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			a, err := app.Discover(root)
			if err != nil {
				t.Fatalf("app.Discover() error = %v", err)
			}
			tt.prepare(t, a)
			got := ciWorkflow{}.Run(context.Background(), &Env{App: a})
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("Run() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
