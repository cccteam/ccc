// Package audit runs an application's generate programs with -audit and prints what they
// raised: the schema warnings every generation prints and the audit pass's findings,
// advisory findings about shapes the framework handles under a stated limitation, which
// a normal generation never prints. It is a command of its own, not a check: the check is
// the gate on every change, the audit is read by decision (before a release, after a
// schema change to a table that stores files, when a stated limitation is in question),
// and nothing here fails on a finding.
package audit

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/cccteam/ccc/impulse/internal/app"
	"github.com/cccteam/ccc/impulse/internal/check"
)

// The prefixes a generate program prints its lines with.
const (
	warningPrefix = "Warning: "
	auditPrefix   = "Audit: "
)

// noAuditFlag is how Go's flag package refuses the -audit flag on a program that does not
// declare it.
const noAuditFlag = "flag provided but not defined: -audit"

// Run runs every generator program of the application with -audit from the module root,
// by the directory go run runs it from, and writes one section per program: the lines it
// raised, or "no findings". It reports whether any program failed; a failure's output
// tail is written under its heading, and a program that does not take -audit is told to
// adopt the runner shape.
func Run(ctx context.Context, a *app.App, exec check.Execer, out io.Writer) (failed bool) {
	if len(a.Generators) == 0 {
		fmt.Fprintln(out, "no generator program found (no file calls generation.NewResourceGenerator)")

		return true
	}
	for _, g := range a.Generators {
		dir := a.ProgramDir(g)
		fmt.Fprintf(out, "%s (go run ./%s -audit)\n", g.File, dir)
		output, err := exec.Run(ctx, a.Root, nil, "go", "run", "./"+dir, "-audit")
		text := string(output)
		switch {
		case err != nil && strings.Contains(text, noAuditFlag):
			failed = true
			fmt.Fprintf(out, "  FAIL  the program does not take -audit; adopt the runner shape (README, impulse audit)\n")
		case err != nil:
			failed = true
			fmt.Fprintf(out, "  FAIL  go run ./%s -audit failed\n", dir)
			for _, line := range tail(text, 20) {
				fmt.Fprintf(out, "        %s\n", line)
			}
		default:
			lines := findings(text)
			if len(lines) == 0 {
				fmt.Fprintln(out, "  no findings")
			}
			for _, line := range lines {
				fmt.Fprintf(out, "  %s\n", line)
			}
		}
	}

	return failed
}

// findings returns the warning and audit lines of a program's output, prefixes included,
// in the order printed.
func findings(text string) []string {
	var lines []string
	for line := range strings.Lines(text) {
		line = strings.TrimRight(line, "\r\n")
		if strings.HasPrefix(line, warningPrefix) || strings.HasPrefix(line, auditPrefix) {
			lines = append(lines, line)
		}
	}

	return lines
}

// tail returns the last lines of a failed run's output.
func tail(text string, keep int) []string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil
	}
	if len(lines) > keep {
		lines = append([]string{fmt.Sprintf("... %d earlier lines omitted", len(lines)-keep)}, lines[len(lines)-keep:]...)
	}

	return lines
}
