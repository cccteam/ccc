package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/cccteam/ccc/impulse/internal/check"
	"github.com/cccteam/ccc/impulse/internal/handoff"
)

// handoffExec answers git from a table keyed by the command line, and anything else with
// nothing, since the checks are injected.
type handoffExec struct {
	answers map[string]string
}

func (f handoffExec) Run(_ context.Context, _ string, _ []string, name string, args ...string) ([]byte, error) {
	return []byte(f.answers[name+" "+strings.Join(args, " ")]), nil
}

// TestHandoff runs the command on a tree of the test's making: a dirty tree is refused,
// with --staged the staged change is accepted and the brief lists its paths with the
// change and meaning given, a change beside the staged ones is refused, --change needs
// --staged, and a clean check hands off nothing.
func TestHandoff(t *testing.T) {
	t.Parallel()

	failing := []check.Result{{Name: "paging", Status: check.Fail, Summary: "1 offset", Details: []string{"pkg/x.go:3: Offset"}}}
	passing := []check.Result{{Name: "pins", Status: check.Pass, Summary: "ok"}}
	markerChange := "`impulse upgrade (recipe marker, step 2)` made these changes and staged them:\n\n- moved the marker to its new form\n"
	tests := []struct {
		name  string
		flags handoffFlags
		// status answers git status --porcelain; staged answers git diff --cached.
		status  string
		staged  string
		results []check.Result
		// wantOut are fragments of the output in order; wantBrief whether the brief
		// exists after, wantInBrief a fragment of it; wantErr the error.
		wantOut     []string
		wantBrief   bool
		wantInBrief string
		wantErr     string
	}{
		{
			name:    "a dirty tree is refused without --staged",
			status:  "M  go.mod\n M pkg/a.go\n?? " + handoff.File + "\n",
			results: failing,
			wantErr: "the working tree is not clean (go.mod, pkg/a.go): commit or stash first, so the agent's work is exactly its own",
		},
		{
			name:        "a clean tree: the brief says the tool changed nothing",
			results:     failing,
			wantOut:     []string{"FAIL  paging  1 offset", "Wrote the brief to .impulse-handoff.md: 1 obligation(s) under 1 failing check(s)."},
			wantBrief:   true,
			wantInBrief: "## What changed\n\nNothing was changed by the tool. `impulse check` found the obligations below in the tree as it is.\n\n## The failing checks",
		},
		{
			name:        "the staged change is accepted, and the brief lists its paths with the change and its meaning",
			flags:       handoffFlags{staged: true, changes: []string{markerChange}, meanings: []string{"The marker names the form the step expects."}},
			status:      "M  go.mod\nA  pkg/marker.txt\n?? " + handoff.File + "\n",
			staged:      "go.mod\npkg/marker.txt\n",
			results:     failing,
			wantOut:     []string{"FAIL  paging  1 offset", "Wrote the brief to .impulse-handoff.md: 1 obligation(s) under 1 failing check(s).", "impulse handoff --verify"},
			wantBrief:   true,
			wantInBrief: "## What changed\n\n" + markerChange + "\nThe change under review is staged in the index, 2 path(s):\n\n- go.mod\n- pkg/marker.txt\n\n## What it means\n\nThe marker names the form the step expects.\n\n## The failing checks",
		},
		{
			name:        "the staged change alone, with nothing said about it: the brief lists its paths",
			flags:       handoffFlags{staged: true},
			status:      "M  go.mod\n",
			staged:      "go.mod\n",
			results:     failing,
			wantBrief:   true,
			wantInBrief: "## What changed\n\nThe change under review is staged in the index, 1 path(s):\n\n- go.mod\n\n## The failing checks",
		},
		{
			name:    "a change beside the staged ones is refused",
			flags:   handoffFlags{staged: true},
			status:  "M  go.mod\nMM pkg/b.go\n M pkg/a.go\n?? notes.txt\n",
			results: failing,
			wantErr: "the working tree has changes beside the staged ones (pkg/b.go, pkg/a.go, notes.txt): stage or stash them, so the index holds exactly the change under review and the agent's work is exactly its own",
		},
		{
			name:    "--change without --staged is refused",
			flags:   handoffFlags{changes: []string{"x"}},
			wantErr: "--change and --meaning describe the change staged in the index: pass --staged with them",
		},
		{
			name:    "a clean check with --staged hands off nothing",
			flags:   handoffFlags{staged: true},
			status:  "M  go.mod\n",
			staged:  "go.mod\n",
			results: passing,
			wantOut: []string{"PASS  pins  ok", "Nothing to hand off: the check is clean."},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/acme/beacon\n\ngo 1.26\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			exec := handoffExec{answers: map[string]string{
				"git status --porcelain --untracked-files=all": tt.status,
				"git diff --cached --name-only":                tt.staged,
			}}
			var out, errOut strings.Builder
			h := &handoffer{
				exec:   exec,
				checks: func(context.Context, *check.Env) []check.Result { return tt.results },
				out:    &out, err: &errOut,
			}
			f := tt.flags
			f.appDir, f.agentCommand = root, handoff.DefaultCommand
			err := h.run(t.Context(), &f)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("run() error = %v, want %q; output:\n%s", err, tt.wantErr, out.String())
				}
			} else if err != nil {
				t.Fatalf("run() error = %v; output:\n%s", err, out.String())
			}
			containsInOrder(t, out.String(), tt.wantOut)
			brief, err := os.ReadFile(filepath.Join(root, handoff.File))
			if (err == nil) != tt.wantBrief {
				t.Errorf("brief present = %v, want %v", err == nil, tt.wantBrief)
			}
			if tt.wantInBrief != "" && !strings.Contains(string(brief), tt.wantInBrief) {
				t.Errorf("brief lacks %q:\n%s", tt.wantInBrief, brief)
			}
		})
	}
}

func TestHandoffReport(t *testing.T) {
	t.Parallel()

	results := []check.Result{
		{Name: "options", Status: check.Pass, Summary: "flat layout"},
		{Name: "tenancy-wired", Status: check.Fail, Summary: "2 problems", Details: []string{"a", "b"}},
		{Name: "outlet-wired", Status: check.Fail, Summary: "go generate failed"},
		{Name: "pins", Status: check.Warn, Summary: "1 pin", Details: []string{"x"}},
	}
	tests := []struct {
		name   string
		report handoffReport
		want   string
	}{
		{
			name:   "plain",
			report: handoffReport{results: results, agent: &handoff.Agent{Command: "claude"}},
			want: `
Wrote the brief to .impulse-handoff.md: 3 obligation(s) under 2 failing check(s).

Next steps
  1. claude -p --permission-mode default --allowedTools Read,Edit,Write,MultiEdit,Glob,Grep,Bash(go:*),Bash(gofmt:*),Bash(impulse:*),Bash(golangci-lint-v2:*),Bash(bun:*),Bash(bunx:*),Bash(npm:*),Bash(npx:*) < .impulse-handoff.md
     Runs the agent on the brief, or read the brief and do the work yourself.
  2. impulse handoff --verify
     Re-runs the check and compares the generator programs and lint configuration against the index.
`,
		},
		{
			name:   "styled",
			report: handoffReport{results: results[:2], agent: &handoff.Agent{}, styled: true},
			want: "\nWrote the brief to .impulse-handoff.md: 2 obligation(s) under 1 failing check(s).\n" +
				"\n\x1b[1mNext steps\x1b[0m\n" +
				"  \x1b[1m1.\x1b[0m claude -p --permission-mode default --allowedTools Read,Edit,Write,MultiEdit,Glob,Grep,Bash(go:*),Bash(gofmt:*),Bash(impulse:*),Bash(golangci-lint-v2:*),Bash(bun:*),Bash(bunx:*),Bash(npm:*),Bash(npx:*) < .impulse-handoff.md\n" +
				"     Runs the agent on the brief, or read the brief and do the work yourself.\n" +
				"  \x1b[1m2.\x1b[0m impulse handoff --verify\n" +
				"     Re-runs the check and compares the generator programs and lint configuration against the index.\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var sb strings.Builder
			tt.report.write(&sb)
			if diff := cmp.Diff(tt.want, sb.String()); diff != "" {
				t.Errorf("write() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
