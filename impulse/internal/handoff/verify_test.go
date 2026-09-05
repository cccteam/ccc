package handoff

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/cccteam/ccc/impulse/internal/app"
	"github.com/cccteam/ccc/impulse/internal/check"
)

// fakeGit answers git commands from a table keyed by the command line; a `git show`
// answers from files, so the index is a directory of the test's making.
type fakeGit struct {
	t       *testing.T
	answers map[string]string
	// index holds the files `git show :<path>` returns.
	index map[string]string
}

func (f fakeGit) Run(_ context.Context, _ string, _ []string, name string, args ...string) ([]byte, error) {
	line := name + " " + strings.Join(args, " ")
	if rel, ok := strings.CutPrefix(line, "git show :"); ok {
		content, ok := f.index[rel]
		if !ok {
			return nil, errors.New("fatal: path does not exist in the index")
		}

		return []byte(content), nil
	}
	out, ok := f.answers[line]
	if !ok {
		f.t.Fatalf("unexpected command %q", line)
	}

	return []byte(out), nil
}

// copyFixture copies a fixture application into a temporary directory so a test can edit
// the tree; the index stays what the test says it is.
func copyFixture(t *testing.T, name string) *app.App {
	t.Helper()

	dst := t.TempDir()
	if err := os.CopyFS(dst, os.DirFS(filepath.Join("..", "app", "testdata", name))); err != nil {
		t.Fatalf("copy fixture: %v", err)
	}
	a, err := app.Discover(dst)
	if err != nil {
		t.Fatalf("app.Discover() error = %v", err)
	}

	return a
}

func TestVerify(t *testing.T) {
	t.Parallel()

	program, err := os.ReadFile(filepath.Join("..", "app", "testdata", "singlesite", singlesiteProgram))
	if err != nil {
		t.Fatal(err)
	}
	eslint, err := os.ReadFile(filepath.Join("..", "app", "testdata", "singlesite", "web", "console", "eslint.config.js"))
	if err != nil {
		t.Fatal(err)
	}
	index := map[string]string{singlesiteProgram: string(program), "web/console/eslint.config.js": string(eslint)}
	same := map[string]string{"git rev-parse HEAD": "abc123def4567890\n", "git diff --cached --name-only": ""}

	tests := []struct {
		name    string
		edit    func(t *testing.T, a *app.App)
		answers map[string]string
		base    *Baseline
		want    check.Result
	}{
		{
			name: "nothing moved, no baseline",
			want: check.Result{Name: "guardrails", Status: check.Pass, Summary: "1 generator program(s) and 1 lint configuration(s) unchanged against the index"},
		},
		{
			name:    "nothing moved, with the baseline",
			answers: same,
			base:    &Baseline{Head: "abc123def4567890"},
			want:    check.Result{Name: "guardrails", Status: check.Pass, Summary: "1 generator program(s) and 1 lint configuration(s) unchanged against the index"},
		},
		{
			name: "the lint configuration edited and a Go lint configuration added",
			edit: func(t *testing.T, a *app.App) {
				t.Helper()
				if err := os.WriteFile(a.Abs("web/console/eslint.config.js"), []byte("export default [];\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(a.Abs(GolangciConfig), []byte("version: '2'\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
			want: check.Result{Name: "guardrails", Status: check.Fail, Summary: "2 guardrail(s) moved during the handoff", Details: []string{
				GolangciConfig + ": a lint configuration was added",
				"web/console/eslint.config.js: the lint configuration was edited",
			}},
		},
		{
			name: "an option removed from the program",
			edit: func(t *testing.T, a *app.App) {
				t.Helper()
				edited := strings.Replace(string(program), "\t\tgeneration.WithRPC(\"pkg/rpc\"),\n", "", 1)
				if err := os.WriteFile(a.Abs(singlesiteProgram), []byte(edited), 0o600); err != nil {
					t.Fatal(err)
				}
			},
			want: check.Result{Name: "guardrails", Status: check.Fail, Summary: "1 guardrail(s) moved during the handoff", Details: []string{
				singlesiteProgram + `: removed WithRPC("pkg/rpc")`,
			}},
		},
		{
			name:    "the agent committed and staged",
			answers: map[string]string{"git rev-parse HEAD": "fedcba9876543210\n", "git diff --cached --name-only": "pkg/resources/beacons.go\n"},
			base:    &Baseline{Head: "abc123def4567890"},
			want: check.Result{Name: "guardrails", Status: check.Fail, Summary: "2 guardrail(s) moved during the handoff", Details: []string{
				"HEAD moved from abc123def456 to fedcba987654: the agent committed",
				"the staged paths changed: the agent staged its work, so the index no longer holds the tree at the handoff",
			}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			a := copyFixture(t, "singlesite")
			if tt.edit != nil {
				tt.edit(t, a)
			}
			repo := Repo{Root: a.Root, Exec: fakeGit{t: t, answers: tt.answers, index: index}}
			got, err := Verify(t.Context(), a, repo, tt.base)
			if err != nil {
				t.Fatalf("Verify() error = %v", err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("Verify() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestRepoDirty(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status string
		want   []string
	}{
		{name: "clean", status: ""},
		{name: "modified, staged, and untracked, the brief excepted", status: " M pkg/a.go\nA  pkg/b.go\n?? notes.txt\n?? " + File + "\n", want: []string{"pkg/a.go", "pkg/b.go", "notes.txt"}},
		{name: "only the brief", status: "?? " + File + "\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repo := Repo{Root: "/w/beacon", Exec: fakeGit{t: t, answers: map[string]string{"git status --porcelain --untracked-files=all": tt.status}}}
			got, err := repo.Dirty(t.Context())
			if err != nil {
				t.Fatalf("Dirty() error = %v", err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("Dirty() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
