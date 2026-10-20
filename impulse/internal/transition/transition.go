// Package transition holds the changes impulse add makes to an application: the half of
// each option that files and source edits do reliably, applied before the rest is handed
// to an agent with the failing checks as its obligations. A transition records what it
// did and what it could not do, and the brief carries both.
package transition

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/go-playground/errors/v5"
	"golang.org/x/mod/modfile"

	"github.com/cccteam/ccc/impulse/app"
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
	// Left lists the obligations the change leaves that the check cannot see, one line
	// per item (file:line: what, or the item alone): the hand-written code that still
	// names a removed flag (a browser use fails the browser's own build, not the check),
	// the work of a scaffolded job. LeftNote is the sentence that says what to do about
	// them. A change with any is handed to the agent even when the check is clean.
	Left     []string
	LeftNote string
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
	if len(c.Left) > 0 {
		fmt.Fprintf(&b, "\n%s\n\n", c.LeftNote)
		for _, u := range c.Left {
			fmt.Fprintf(&b, "- %s\n", u)
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

// listed joins names for a message, or says there are none.
func listed(names []string) string {
	if len(names) == 0 {
		return "none"
	}

	return strings.Join(names, ", ")
}

// WarningPrefix is how a generate program prints one schema warning.
const WarningPrefix = "Warning: "

// plainErr is the error expression an inserted statement returns where the file imports
// no errors package to wrap it with.
const plainErr = "err"

// generate makes the module whole and runs go generate ./... at the application root,
// the one regeneration every transition ends in. go mod tidy comes first, so go.mod
// requires every module the change's new imports reach: a directory method's file
// imports the session library's google or azure package, which signs in through an OIDC
// library a module with password methods alone never required, and the generate programs
// refuse to build without the requirement ("updates to go.mod needed"). What tidy changed
// is recorded as did lines. A clean run records did and every warning line the programs
// printed. A failed tidy or generate refuses the transition with the given lead sentence
// and the command's output, instead of noting it for the agent: the regeneration is the
// transition's own step, and an application left without it is half-bound, its router,
// user surfaces and browser client still those of the application before the change.
func generate(ctx context.Context, a *app.App, exec check.Execer, ch *Change, failed, did string) error {
	if err := tidy(ctx, a, exec, ch); err != nil {
		return err
	}
	out, err := exec.Run(ctx, a.Root, nil, "go", "generate", "./...")
	if err != nil {
		return errors.Newf("%s\n%s", failed, strings.TrimSpace(string(out)))
	}
	ch.didf("%s", did)
	for line := range strings.Lines(string(out)) {
		if w, ok := strings.CutPrefix(strings.TrimRight(line, "\r\n"), WarningPrefix); ok {
			ch.Warnings = append(ch.Warnings, w)
		}
	}

	return nil
}

// tidy runs go mod tidy at the application root and records what it changed in go.mod,
// a did line per requirement added, moved to another version, made direct or dropped,
// and nothing when go.mod stands. The application's parsed go.mod follows the file. A
// failed tidy refuses the transition, as the regeneration after it would: the module
// cannot be made whole.
func tidy(ctx context.Context, a *app.App, exec check.Execer, ch *Change) error {
	before, err := parseGoMod(a)
	if err != nil {
		return err
	}
	out, err := exec.Run(ctx, a.Root, nil, "go", "mod", "tidy")
	if err != nil {
		return errors.Newf("go mod tidy failed, so go.mod does not require what the change imports; fix the cause and run it, then go generate ./...:\n%s", strings.TrimSpace(string(out)))
	}
	after, err := parseGoMod(a)
	if err != nil {
		return err
	}
	a.GoMod = after
	for _, line := range requirementChanges(before, after) {
		ch.didf("%s", line)
	}

	return nil
}

// parseGoMod reads the application's go.mod as it is on disk.
func parseGoMod(a *app.App) (*modfile.File, error) {
	path := a.Abs("go.mod")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.Wrap(err, "os.ReadFile()")
	}
	f, err := modfile.Parse(path, data, nil)
	if err != nil {
		return nil, errors.Wrap(err, "modfile.Parse()")
	}

	return f, nil
}

// requirementChanges says how the requirements moved from one go.mod to the next, a did
// line per module: the modules required anew, at another version, or directly where they
// were indirect, in the order the file lists them, then the modules no longer required.
func requirementChanges(before, after *modfile.File) []string {
	was := make(map[string]*modfile.Require, len(before.Require))
	for _, r := range before.Require {
		was[r.Mod.Path] = r
	}
	is := make(map[string]bool, len(after.Require))
	var lines []string
	for _, r := range after.Require {
		is[r.Mod.Path] = true
		prev, ok := was[r.Mod.Path]
		switch {
		case !ok:
			lines = append(lines, fmt.Sprintf("go.mod: requires %s %s%s (go mod tidy)", r.Mod.Path, r.Mod.Version, requirementNote(r.Mod.Path)))
		case prev.Mod.Version != r.Mod.Version:
			lines = append(lines, fmt.Sprintf("go.mod: requires %s %s in place of %s%s (go mod tidy)", r.Mod.Path, r.Mod.Version, prev.Mod.Version, requirementNote(r.Mod.Path)))
		case prev.Indirect && !r.Indirect:
			lines = append(lines, fmt.Sprintf("go.mod: requires %s %s directly%s (go mod tidy)", r.Mod.Path, r.Mod.Version, requirementNote(r.Mod.Path)))
		}
	}
	for _, r := range before.Require {
		if !is[r.Mod.Path] {
			lines = append(lines, fmt.Sprintf("go.mod: no longer requires %s (go mod tidy)", r.Mod.Path))
		}
	}

	return lines
}

// oidcModule is the OIDC library the session library's google and azure packages sign in
// through, which a module with password methods alone never requires.
const oidcModule = "github.com/coreos/go-oidc/v3"

// requirementNote says what a module the options are known to pull into go.mod is, for
// tidy's did line, leading comma included; a module outside the list is named plainly.
func requirementNote(path string) string {
	switch path {
	case oidcModule:
		return ", the OIDC library the session library's directory methods sign in through"
	case cobraImportPath:
		return ", the command tree's library"
	default:
		return ""
	}
}
