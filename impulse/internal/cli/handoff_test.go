package cli

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/cccteam/ccc/impulse/internal/check"
	"github.com/cccteam/ccc/impulse/internal/handoff"
)

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
			report: handoffReport{results: results, agent: handoff.Agent{Command: "claude"}},
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
			report: handoffReport{results: results[:2], agent: handoff.Agent{}, styled: true},
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
