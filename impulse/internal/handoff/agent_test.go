package handoff

import (
	"testing"
)

func TestAgentCommandLine(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		agent Agent
		want  string
	}{
		{
			name: "Claude Code by default",
			want: "claude -p --permission-mode default --allowedTools Read,Edit,Write,MultiEdit,Glob,Grep,Bash(go:*),Bash(gofmt:*),Bash(impulse:*),Bash(golangci-lint-v2:*),Bash(bun:*),Bash(bunx:*),Bash(npm:*),Bash(npx:*) < .impulse-handoff.md",
		},
		{
			name:  "extra arguments",
			agent: Agent{ExtraArgs: []string{"--model", "sonnet", "--max-budget-usd", "5"}},
			want:  "claude -p --permission-mode default --allowedTools Read,Edit,Write,MultiEdit,Glob,Grep,Bash(go:*),Bash(gofmt:*),Bash(impulse:*),Bash(golangci-lint-v2:*),Bash(bun:*),Bash(bunx:*),Bash(npm:*),Bash(npx:*) --model sonnet --max-budget-usd 5 < .impulse-handoff.md",
		},
		{
			name:  "another executable",
			agent: Agent{Command: "/opt/bin/claude-next"},
			want:  "/opt/bin/claude-next -p --permission-mode default --allowedTools Read,Edit,Write,MultiEdit,Glob,Grep,Bash(go:*),Bash(gofmt:*),Bash(impulse:*),Bash(golangci-lint-v2:*),Bash(bun:*),Bash(bunx:*),Bash(npm:*),Bash(npx:*) < .impulse-handoff.md",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.agent.CommandLine(); got != tt.want {
				t.Errorf("CommandLine() = %q, want %q", got, tt.want)
			}
		})
	}
}
