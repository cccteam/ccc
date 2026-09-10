// Package transition holds the changes impulse add makes to an application: the half of
// each option that files and source edits do reliably, applied before the rest is handed
// to an agent with the failing checks as its obligations. A transition records what it
// did and what it could not do, and the brief carries both.
package transition

import (
	"fmt"
	"strings"
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

	return b.String()
}
