package handoff

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/go-playground/errors/v5"
)

// Agent launches an agent on the brief. Claude Code is the first agent: it runs
// non-interactively, reads the brief on standard input, and is allowed exactly the tools
// the rules need. The brief itself is agent-neutral; another agent is another Agent.
type Agent struct {
	// Command is the agent's executable. Empty means "claude".
	Command string
	// ExtraArgs are appended to the command line: a model, a turn limit, a budget.
	ExtraArgs []string
	// Tools are the tools the agent may use without asking, in Claude Code's own syntax.
	// Empty means the editing set the handoff needs (EditingTools); a caller that wants
	// an answer and no edit passes ReadOnlyTools.
	Tools []string
	// BriefFile is the root-relative brief the agent reads. Empty means the handoff's.
	BriefFile string
}

// DefaultCommand is the Claude Code executable.
const DefaultCommand = "claude"

// The tool sets, in Claude Code's own syntax. Non-interactive runs deny everything else,
// so git and the package managers the application does not use are out by omission.
var (
	// EditingTools are the tools the handoff's rules need: reading, editing, and the
	// build commands.
	EditingTools = []string{
		"Read", "Edit", "Write", "MultiEdit", "Glob", "Grep",
		"Bash(go:*)", "Bash(gofmt:*)", "Bash(impulse:*)", "Bash(golangci-lint-v2:*)",
		"Bash(bun:*)", "Bash(bunx:*)", "Bash(npm:*)", "Bash(npx:*)",
	}
	// ReadOnlyTools read the tree and nothing else: for an agent asked for an answer and
	// no edit.
	ReadOnlyTools = []string{"Read", "Glob", "Grep"}
)

// Args returns the command line, executable first.
func (ag *Agent) Args() []string {
	command := ag.Command
	if command == "" {
		command = DefaultCommand
	}
	tools := ag.Tools
	if len(tools) == 0 {
		tools = EditingTools
	}

	args := make([]string, 0, 6+len(ag.ExtraArgs))
	args = append(args, command, "-p", "--permission-mode", "default", "--allowedTools", strings.Join(tools, ","))

	return append(args, ag.ExtraArgs...)
}

// Brief is the root-relative file the agent reads: the handoff's unless the caller named
// another.
func (ag *Agent) Brief() string {
	if ag.BriefFile != "" {
		return ag.BriefFile
	}

	return File
}

// CommandLine renders the command a person runs to hand the brief to the agent by hand.
func (ag *Agent) CommandLine() string {
	return strings.Join(ag.Args(), " ") + " < " + ag.Brief()
}

// Run launches the agent in dir with the brief on standard input, streaming its output
// to out until it exits. The directory holding this executable leads the agent's PATH,
// so the impulse the agent runs the check with is the one that handed off.
func (ag *Agent) Run(ctx context.Context, dir, brief string, out io.Writer) error {
	args := ag.Args()
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = dir
	cmd.Env = environWithSelf()
	cmd.Stdin = strings.NewReader(brief)
	cmd.Stdout = out
	cmd.Stderr = out
	if err := cmd.Run(); err != nil {
		then := ""
		if ag.Brief() == File {
			then = " and then `impulse handoff --verify`"
		}

		return errors.Newf("%s exited with an error (%v); the brief stays at %s, so once the agent is fixed run `%s`%s", args[0], err, ag.Brief(), ag.CommandLine(), then)
	}

	return nil
}

// environWithSelf is the environment with this executable's directory first on PATH.
func environWithSelf() []string {
	env := os.Environ()
	self, err := os.Executable()
	if err != nil {
		return env
	}
	dir := filepath.Dir(self)
	for i, kv := range env {
		if p, ok := strings.CutPrefix(kv, "PATH="); ok {
			env[i] = "PATH=" + dir + string(os.PathListSeparator) + p

			return env
		}
	}

	return append(env, "PATH="+dir)
}
