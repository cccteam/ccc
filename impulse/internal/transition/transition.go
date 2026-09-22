// Package transition holds the changes impulse add makes to an application: the half of
// each option that files and source edits do reliably, applied before the rest is handed
// to an agent with the failing checks as its obligations. A transition records what it
// did and what it could not do, and the brief carries both.
package transition

import (
	"context"
	"fmt"
	"strings"

	"github.com/cccteam/ccc/impulse/internal/app"
	"github.com/cccteam/ccc/impulse/internal/check"
)

// Change is what a transition did and what it left to the agent.
type Change struct {
	// Command is the impulse command line the change was made under.
	Command string
	// Did lists the edits made, one per line.
	Did []string
	// Skipped lists the deterministic edits that could not be made, with the reason, so
	// the agent knows to make them.
	Skipped []string
	// Warnings are the schema warnings the regeneration printed (its "Warning: " lines),
	// so they reach the command's output and the brief instead of ending in a swallowed
	// clean run.
	Warnings []string
}

func (c *Change) didf(format string, args ...any) {
	c.Did = append(c.Did, fmt.Sprintf(format, args...))
}

func (c *Change) skipf(format string, args ...any) {
	c.Skipped = append(c.Skipped, fmt.Sprintf(format, args...))
}

// Text renders the change for the brief's "what changed" section.
func (c *Change) Text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "`%s` made these changes and staged them:\n\n", c.Command)
	for _, d := range c.Did {
		fmt.Fprintf(&b, "- %s\n", d)
	}
	if len(c.Skipped) > 0 {
		b.WriteString("\nIt could not make these; they are yours:\n\n")
		for _, s := range c.Skipped {
			fmt.Fprintf(&b, "- %s\n", s)
		}
	}
	if len(c.Warnings) > 0 {
		b.WriteString("\ngo generate printed these schema warnings; each is fixed in the schema or accepted by pinning its typed value in the program's warnings test:\n\n")
		for _, w := range c.Warnings {
			fmt.Fprintf(&b, "- %s\n", w)
		}
	}

	return b.String()
}

// warningPrefix is how a generate program prints one schema warning.
const warningPrefix = "Warning: "

// generate runs go generate ./... at the application root, the one regeneration every
// transition ends in. A clean run records did and every warning line the programs
// printed; a failed run records the failure and the output tail as the agent's, with
// the given lead sentence.
func generate(ctx context.Context, a *app.App, exec check.Execer, ch *Change, failed, did string) {
	out, err := exec.Run(ctx, a.Root, nil, "go", "generate", "./...")
	if err != nil {
		ch.skipf("%s\n%s", failed, strings.TrimSpace(string(out)))

		return
	}
	ch.didf("%s", did)
	for line := range strings.Lines(string(out)) {
		if w, ok := strings.CutPrefix(strings.TrimRight(line, "\r\n"), warningPrefix); ok {
			ch.Warnings = append(ch.Warnings, w)
		}
	}
}
