package check

import (
	"context"
	"errors"
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
// workspace: the files as the code renders them (with origin's default branch read, or
// not), a hand edit, no file at all, and a default branch the cache-filling workflow does
// not fill.
func TestCIWorkflow(t *testing.T) {
	t.Parallel()

	const jobs = "15 job(s): title, go-build, go-test, go-test-skipauth, go-lint, go-lint-skipauth, go-vuln, go-semgrep, go-check, go, angular-web, web, image, secrets, migrations"
	const symref = "git ls-remote --symref origin HEAD"
	written := func(t *testing.T, a *app.App) {
		t.Helper()
		if _, err := ci.Write(a); err != nil {
			t.Fatal(err)
		}
	}
	tests := []struct {
		name string
		// main is the application's main.go, for a //impulse:ci line.
		main string
		// prepare leaves the application's workflow in the state under test.
		prepare func(t *testing.T, a *app.App)
		// answers is what git says, by command line; nil runs without an Execer.
		answers map[string]fakeAnswer
		want    Result
	}{
		{
			name:    "the files as the code renders them pass; without an Execer origin is not asked",
			prepare: written,
			want:    Result{Name: ciWorkflow{}.Name(), Status: Pass, Summary: ".github/workflows/ci.yml and .github/workflows/ci-cache.yml match what impulse renders from the code (" + jobs + "; .github/workflows/ci-cache.yml fills the caches on main and master); origin's default branch could not be read"},
		},
		{
			name:    "origin's default branch is one the workflow fills",
			prepare: written,
			answers: map[string]fakeAnswer{symref: {out: "ref: refs/heads/main\tHEAD\n0123456789abcdef0123456789abcdef01234567\tHEAD\n"}},
			want:    Result{Name: ciWorkflow{}.Name(), Status: Pass, Summary: ".github/workflows/ci.yml and .github/workflows/ci-cache.yml match what impulse renders from the code (" + jobs + "; .github/workflows/ci-cache.yml fills the caches on main and master), and origin's default branch is main"},
		},
		{
			name:    "no remote, or no network",
			prepare: written,
			answers: map[string]fakeAnswer{symref: {out: "fatal: 'origin' does not appear to be a git repository\n", err: errors.New("exit status 128")}},
			want:    Result{Name: ciWorkflow{}.Name(), Status: Pass, Summary: ".github/workflows/ci.yml and .github/workflows/ci-cache.yml match what impulse renders from the code (" + jobs + "; .github/workflows/ci-cache.yml fills the caches on main and master); origin's default branch could not be read"},
		},
		{
			name:    "a default branch the workflow does not fill fails",
			prepare: written,
			answers: map[string]fakeAnswer{symref: {out: "ref: refs/heads/trunk\tHEAD\n"}},
			want: Result{Name: ciWorkflow{}.Name(), Status: Fail, Summary: ".github/workflows/ci-cache.yml fills the caches on main and master, and origin's default branch is trunk: the pull requests start cold on every run", Details: []string{
				"declare default-branch=trunk on the //impulse:ci line (a comment line in any non-test Go file), then run impulse render (go tool impulse render)",
			}},
		},
		{
			name:    "the declared default branch is origin's",
			main:    "//impulse:ci default-branch=trunk\n\npackage main\n",
			prepare: written,
			answers: map[string]fakeAnswer{symref: {out: "ref: refs/heads/trunk\tHEAD\n"}},
			want:    Result{Name: ciWorkflow{}.Name(), Status: Pass, Summary: ".github/workflows/ci.yml and .github/workflows/ci-cache.yml match what impulse renders from the code (" + jobs + "; .github/workflows/ci-cache.yml fills the caches on trunk), and origin's default branch is trunk"},
		},
		{
			name:    "the declared default branch is not origin's",
			main:    "//impulse:ci default-branch=trunk\n\npackage main\n",
			prepare: written,
			answers: map[string]fakeAnswer{symref: {out: "ref: refs/heads/main\tHEAD\n"}},
			want: Result{Name: ciWorkflow{}.Name(), Status: Fail, Summary: ".github/workflows/ci-cache.yml fills the caches on trunk, the //impulse:ci line's default-branch (main.go:1), and origin's default branch is main: the pull requests start cold on every run", Details: []string{
				"set default-branch=main on the line, or drop the setting if that is main or master, then run impulse render (go tool impulse render)",
			}},
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
			files := map[string]string{
				"go.mod":           "module example.com/beacon\n\ngo 1.26.6\n",
				"web/angular.json": "{\n  \"version\": 1,\n  \"projects\": {}\n}\n",
			}
			if tt.main != "" {
				files["main.go"] = tt.main
			}
			for rel, content := range files {
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
			env := &Env{App: a}
			if tt.answers != nil {
				env.Exec = &fakeExec{t: t, answers: tt.answers}
			}
			got := ciWorkflow{}.Run(context.Background(), env)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("Run() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
