package check

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/cccteam/ccc/impulse/app"
	"github.com/cccteam/ccc/impulse/ci"
)

// lineOf finds the line, counting from 1, where the pull request workflow of an
// application with the flat browser workspace first reads as want, so a test case names
// a line by its content and not by a number the template moves.
func lineOf(t *testing.T, want string) string {
	t.Helper()

	rendered, err := ci.Render(&app.App{WebApps: []app.WebApp{{Dir: "web"}}})
	if err != nil {
		t.Fatal(err)
	}
	for i, line := range strings.Split(string(rendered), "\n") {
		if line == want {
			return strconv.Itoa(i + 1)
		}
	}
	t.Fatalf("no line %q in the rendered workflow", want)

	return ""
}

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
			want: Result{Name: ciWorkflow{}.Name(), Status: Pass, Summary: ".github/workflows/ci.yml and .github/workflows/ci-cache.yml match what impulse renders from the code (15 job(s): title, go-build, go-test, go-test-skipauth, go-lint, go-lint-skipauth, go-vuln, go-semgrep, go-check, go, angular-web, web, image, secrets, migrations)"},
		},
		{
			name:    "a missing file fails: no checks run",
			prepare: func(t *testing.T, _ *app.App) { t.Helper() },
			want: Result{Name: ciWorkflow{}.Name(), Status: Fail, Summary: ".github/workflows/ci.yml is missing: the pull requests run no checks", Details: []string{
				"run impulse render (go tool impulse render) to write it from the code",
			}},
		},
		{
			name: "a missing cache-filling workflow fails: the checks start cold",
			prepare: func(t *testing.T, a *app.App) {
				t.Helper()
				if _, err := ci.Write(a); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(a.Abs(ci.CacheFile)); err != nil {
					t.Fatal(err)
				}
			},
			want: Result{Name: ciWorkflow{}.Name(), Status: Fail, Summary: ".github/workflows/ci-cache.yml is missing: the checks start cold on every pull request", Details: []string{
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
				`.github/workflows/ci.yml:` + lineOf(t, "      - run: bun run test") + `: the code renders "      - run: bun run test"; the file has ""`,
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
