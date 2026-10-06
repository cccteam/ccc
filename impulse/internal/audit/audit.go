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

	"github.com/cccteam/ccc/impulse/app"
	"github.com/cccteam/ccc/impulse/internal/check"
)

// The prefixes a generate program prints its lines with.
const (
	WarningPrefix = "Warning: "
	AuditPrefix   = "Audit: "
)

// noAuditFlag is how Go's flag package refuses the -audit flag on a program that does not
// declare it.
const noAuditFlag = "flag provided but not defined: -audit"

// Program is what one generate program raised under -audit, or how its run failed.
type Program struct {
	// File is the program's declaring file, root-relative.
	File string
	// Dir is the directory go run runs it from, root-relative.
	Dir string
	// Lines are the Warning: and Audit: lines the program printed, prefixes included, in
	// the order printed.
	Lines []string
	// Failed reports a run that exited with an error; NoAudit a program that does not take
	// -audit; Tail the failed run's output tail.
	Failed  bool
	NoAudit bool
	Tail    []string
}

// Command is the command line the program was run with, root-relative.
func (p *Program) Command() string {
	return "go run ./" + p.Dir + " -audit"
}

// Collect runs every generator program of the application with -audit from the module
// root, by the directory go run runs it from, and returns what each raised.
func Collect(ctx context.Context, a *app.App, exec check.Execer) []Program {
	programs := make([]Program, 0, len(a.Generators))
	for _, g := range a.Generators {
		p := Program{File: g.File, Dir: a.ProgramDir(g)}
		output, err := exec.Run(ctx, a.Root, nil, "go", "run", "./"+p.Dir, "-audit")
		text := string(output)
		switch {
		case err != nil && strings.Contains(text, noAuditFlag):
			p.Failed, p.NoAudit = true, true
		case err != nil:
			p.Failed = true
			p.Tail = tail(text, 20)
		default:
			p.Lines = findings(text)
		}
		programs = append(programs, p)
	}

	return programs
}

// Run runs every generator program of the application with -audit and writes one section
// per program: the lines it raised, or "no findings". It reports whether any program
// failed; a failure's output tail is written under its heading, and a program that does
// not take -audit is told to adopt the runner shape.
func Run(ctx context.Context, a *app.App, exec check.Execer, out io.Writer) (failed bool) {
	if len(a.Generators) == 0 {
		fmt.Fprintln(out, "no generator program found (no file calls generation.NewResourceGenerator)")

		return true
	}
	for _, p := range Collect(ctx, a, exec) {
		fmt.Fprintf(out, "%s (%s)\n", p.File, p.Command())
		switch {
		case p.NoAudit:
			failed = true
			fmt.Fprintf(out, "  FAIL  the program does not take -audit; adopt the runner shape (README, impulse audit)\n")
		case p.Failed:
			failed = true
			fmt.Fprintf(out, "  FAIL  %s failed\n", p.Command())
			for _, line := range p.Tail {
				fmt.Fprintf(out, "        %s\n", line)
			}
		default:
			if len(p.Lines) == 0 {
				fmt.Fprintln(out, "  no findings")
			}
			for _, line := range p.Lines {
				fmt.Fprintf(out, "  %s\n", line)
			}
		}
	}
	WriteFeatures(out, a)
	WriteEngines(out, a)

	return failed
}

// The headings and lines of the feature flags section.
const (
	featuresHeading  = "feature flags"
	noFeaturesLine   = "  none declared"
	featureUsesError = "  the uses could not be read: %v"
)

// WriteFeatures writes the feature flags section: every declared flag with its
// description, then where it is used (the resources, fields and methods its @feature
// gates, and the Go and browser code that reads it, tests and specs marked), or that it
// gates nothing and is read nowhere, which the feature-flags check fails on.
func WriteFeatures(out io.Writer, a *app.App) {
	fmt.Fprintln(out, featuresHeading)
	if len(a.Features) == 0 {
		fmt.Fprintln(out, noFeaturesLine)

		return
	}
	uses, err := a.FeatureUses()
	if err != nil {
		fmt.Fprintf(out, featureUsesError+"\n", err)
	}
	for i := range a.Features {
		flag := &a.Features[i]
		fmt.Fprintf(out, "  %s (%s, %s:%d): %s\n", flag.Constant, flag.Name, flag.File, flag.Line, descriptionOf(flag))
		gates := a.GatesOf(flag.Constant)
		var reads []string
		for _, u := range uses {
			if u.Constant != flag.Constant {
				continue
			}
			line := fmt.Sprintf("%s:%d", u.File, u.Line)
			if u.Test {
				line += " (test)"
			}
			reads = append(reads, line)
		}
		if len(gates) == 0 && len(reads) == 0 {
			fmt.Fprintln(out, "    gates nothing and is read nowhere (the feature-flags check fails on it)")

			continue
		}
		for _, g := range gates {
			fmt.Fprintf(out, "    gates %s (%s:%d)\n", g.Target, g.File, g.Line)
		}
		if len(reads) > 0 {
			fmt.Fprintf(out, "    read in %s\n", strings.Join(reads, ", "))
		}
	}
}

// The heading and lines of the permission engines section.
const (
	enginesHeading = "permission engines"
	noEnginesLine  = "  none constructed"
)

// WriteEngines writes the permission engines section: every engine the application
// constructs (access.New outside tests) by package, file and line, and whether it is
// handed a change signal (access.WithChangeSignal), which the change-signal check fails
// without.
func WriteEngines(out io.Writer, a *app.App) {
	fmt.Fprintln(out, enginesHeading)
	if len(a.Engines) == 0 {
		fmt.Fprintln(out, noEnginesLine)

		return
	}
	for _, e := range a.Engines {
		state := "handed a change signal (access.WithChangeSignal)"
		switch {
		case e.ChangeSignal:
		case e.OptionsForwarded:
			state = "forwards its options, so a change signal cannot be read here (the change-signal check fails on it)"
		default:
			state = "no change signal (the change-signal check fails on it)"
		}
		fmt.Fprintf(out, "  %s (%s:%d): %s\n", e.Package, e.File, e.Line, state)
	}
}

// descriptionOf is a flag's description, or says it has none.
func descriptionOf(flag *app.FeatureFlag) string {
	if flag.Description == "" {
		return "(no description)"
	}

	return flag.Description
}

// findings returns the warning and audit lines of a program's output, prefixes included,
// in the order printed.
func findings(text string) []string {
	var lines []string
	for line := range strings.Lines(text) {
		line = strings.TrimRight(line, "\r\n")
		if strings.HasPrefix(line, WarningPrefix) || strings.HasPrefix(line, AuditPrefix) {
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
