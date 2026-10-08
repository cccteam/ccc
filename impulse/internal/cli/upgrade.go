package cli

import (
	"context"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/go-playground/errors/v5"
	"github.com/spf13/cobra"
	"golang.org/x/mod/modfile"
	"golang.org/x/mod/semver"

	"github.com/cccteam/ccc/impulse/app"
	"github.com/cccteam/ccc/impulse/ci"
	"github.com/cccteam/ccc/impulse/internal/check"
	"github.com/cccteam/ccc/impulse/internal/handoff"
	"github.com/cccteam/ccc/impulse/internal/ledger"
	transition_ "github.com/cccteam/ccc/impulse/internal/transition"
)

// The commands an upgrade step runs in the application.
const (
	goCommand        = "go"
	gitCommand       = "git"
	upgradeCommitTyp = "upgrade"
)

// toolPinNote is the body of the commit that moves the impulse tool pin.
const toolPinNote = "the impulse tool pin moves to the running impulse and the owned files are rendered again from the code"

func newUpgrade() *cobra.Command {
	var (
		f      transitionFlags
		dryRun bool
	)
	cmd := &cobra.Command{
		Use:   "upgrade",
		Short: "Move the application to the running impulse and through the ledger's steps after the one its pins stand at, one commit each",
		Long: `upgrade moves an application forward to the running impulse and through the steps the
ledger records after the one its pins stand at, and commits each. Where the application
stands is read from the framework pins in its go.mod (the resource, access, session and
accesstypes versions it builds against), never from a file of its own: the latest step whose
pins they reach is the position, and every step after it is pending. The impulse tool pin
is not a step's: when it is behind the running impulse it moves first (go get -tool), the
owned files are rendered again (impulse render), go generate runs, impulse check runs, and
the result is committed as "` + upgradeCommitTyp + `: impulse <version>"; an impulse built
from a checkout leaves the pin alone, and one older than the pin refuses, since the pinned
one is the impulse to run. Then each pending step is one commit: its recipes run (each
detects the old form in the application and edits only where it finds it, so running twice
is safe), the pins it moves move to its set (go get; a pin already at or beyond the step's
stays), the owned files are rendered again, go generate runs, impulse check runs, and
everything is committed under "` + upgradeCommitTyp + `: <the pins moved>". A failing check
stops the walk with the handoff brief written (` + handoff.File + `) and the step's changes
staged: fix or hand off, commit, and run upgrade again; it resumes from whatever go.mod
says. No step is skipped, since a recipe is written against the shape the step before it
left behind.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			u := &upgrader{
				exec:    check.OSExec{},
				steps:   ledger.Steps,
				running: check.RunningBuild(),
				verify: func(ctx context.Context, env *check.Env) []check.Result {
					return check.Run(ctx, env, check.All())
				},
				owned: func(a *app.App) ([]string, error) {
					outcome, err := ci.Write(a)

					return outcome.WrittenFiles(), err
				},
				out: cmd.OutOrStdout(),
				err: cmd.ErrOrStderr(),
			}

			return u.run(cmd.Context(), &f, dryRun)
		},
	}
	f.bind(cmd)
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print what the walk would do, and change nothing")

	return cmd
}

// upgrader walks an application through the ledger. Its parts are injected so the walk is
// tested without go, git or the checks.
type upgrader struct {
	exec    check.Execer
	steps   []ledger.Step
	running check.Build
	// verify runs the checks on the application after a step; owned rewrites the owned
	// files from the code and reports whether anything changed.
	verify func(ctx context.Context, env *check.Env) []check.Result
	owned  func(a *app.App) ([]string, error)
	out    io.Writer
	err    io.Writer
}

// plan is what a walk does: the tool pin's move, when the running impulse is a release
// the pin is behind, and the pending steps.
type plan struct {
	// position is the index of the step the application stands at, -1 before the first.
	position int
	pending  []ledger.Step
	// toolPin is go.mod's impulse pin, "" when it has none; release is the running
	// impulse's version when it was built from a module version, else "".
	toolPin string
	release string
}

// movesTool reports whether the walk moves the tool pin: the running impulse is a release
// and the pin is not it.
func (p *plan) movesTool() bool {
	return p.release != "" && p.toolPin != p.release
}

// end is the impulse the application pins when the walk is done.
func (p *plan) end() string {
	if p.movesTool() {
		return p.release
	}

	return p.toolPin
}

// run reads the position and the tool pin, prints the plan, and walks it.
func (u *upgrader) run(ctx context.Context, f *transitionFlags, dryRun bool) error {
	a, err := app.Discover(f.appDir)
	if err != nil {
		return err
	}
	p, err := u.plan(a)
	if err != nil {
		return err
	}
	at := "before the ledger's first step"
	if p.position >= 0 {
		at = fmt.Sprintf("at step %d of %d", p.position+1, len(u.steps))
	}
	pinned := "no impulse"
	if p.toolPin != "" {
		pinned = "impulse " + p.toolPin
	}
	if len(p.pending) == 0 && !p.movesTool() {
		fmt.Fprintf(u.out, "%s stands %s, the ledger's last, and pins %s, the running impulse: nothing to do.\n", appLabel(a), at, pinned)

		return nil
	}
	fmt.Fprintf(u.out, "%s stands %s and pins %s; the walk:\n", appLabel(a), at, pinned)
	if p.movesTool() {
		fmt.Fprintf(u.out, "  impulse %s: %s\n", p.release, toolPinNote)
	}
	for i := range p.pending {
		fmt.Fprintf(u.out, "  %s\n", planLine(u.steps, p.position+1+i))
	}
	if p.release == "" {
		fmt.Fprintf(u.out, "This impulse was built from a checkout (%s), so the tool pin stays where it is.\n", u.running.Version)
	}
	fmt.Fprintln(u.out)
	if dryRun {
		return nil
	}
	repo := handoff.For(a, u.exec)
	if err := repo.Check(ctx); err != nil {
		return err
	}
	dirty, err := repo.Dirty(ctx)
	if err != nil {
		return err
	}
	if len(dirty) > 0 {
		return errors.Newf("the working tree is not clean (%d path(s)): commit or stash first, so each step is one commit", len(dirty))
	}
	if p.movesTool() {
		if err := u.moveTool(ctx, f, repo, p.release); err != nil {
			return err
		}
	}
	for i := range p.pending {
		if err := u.step(ctx, f, repo, &p.pending[i], p.position+2+i); err != nil {
			return err
		}
	}
	fmt.Fprintf(u.out, "%s stands at step %d of %d and pins impulse %s. Review the commits and open the pull request.\n", appLabel(a), len(u.steps), len(u.steps), p.end())

	return nil
}

// plan reads where the application stands and what the walk does; a tool pin newer than
// the running impulse is refused, since the pinned impulse is the one to run.
func (u *upgrader) plan(a *app.App) (*plan, error) {
	pins := ledger.AppPins(a.GoMod)
	p := &plan{position: ledger.Position(u.steps, pins), pending: ledger.Pending(u.steps, pins), toolPin: goModImpulsePin(a.GoMod)}
	if u.running.FromModule && semver.IsValid(u.running.Version) {
		p.release = u.running.Version
	}
	if p.release != "" && p.toolPin != "" && semver.Compare(p.toolPin, p.release) > 0 {
		return nil, errors.Newf("go.mod pins impulse at %s, newer than the running %s: run the pinned one (go tool impulse upgrade)", p.toolPin, p.release)
	}

	return p, nil
}

// goModImpulsePin is the version go.mod requires impulse at, "" when it does not.
func goModImpulsePin(mod *modfile.File) string {
	if mod == nil {
		return ""
	}
	for _, r := range mod.Require {
		if r.Mod.Path == check.ImpulseModule {
			return r.Mod.Version
		}
	}

	return ""
}

// appLabel names the application after its module path's last segment.
func appLabel(a *app.App) string {
	if a.GoMod == nil || a.GoMod.Module == nil {
		return "the application"
	}

	return path.Base(a.GoMod.Module.Mod.Path)
}

// planLine says what a step does: its number, the pins it moves from the step before,
// its recipes, and its note.
func planLine(steps []ledger.Step, i int) string {
	var prev map[string]string
	if i > 0 {
		prev = steps[i-1].Pins
	}
	line := fmt.Sprintf("step %d, %s", i+1, strings.Join(steps[i].Moves(prev), ", "))
	if names := recipeNames(&steps[i]); names != "" {
		line += " (" + names + ")"
	}

	return line + ": " + steps[i].Note
}

// recipeNames lists a step's recipes as "recipe paging" or "recipes paging, filters", ""
// when it has none.
func recipeNames(s *ledger.Step) string {
	names := make([]string, 0, len(s.Recipes))
	for _, recipe := range s.Recipes {
		names = append(names, recipe.Name())
	}
	switch len(names) {
	case 0:
		return ""
	case 1:
		return "recipe " + names[0]
	default:
		return "recipes " + strings.Join(names, ", ")
	}
}

// moveTool moves the impulse tool pin to the running impulse and finishes the step: the
// owned files, the regeneration, the check, and the commit.
func (u *upgrader) moveTool(ctx context.Context, f *transitionFlags, repo handoff.Repo, release string) error {
	fmt.Fprintf(u.out, "=== impulse %s: %s ===\n", release, toolPinNote)
	a, err := app.Discover(f.appDir)
	if err != nil {
		return err
	}
	fmt.Fprintf(u.out, "Pin the impulse tool to %s.\n", release)
	if out, err := u.exec.Run(ctx, a.Root, nil, goCommand, "get", "-tool", check.ImpulseModule+"@"+release); err != nil {
		return errors.Wrapf(err, "go get -tool %s@%s: %s", check.ImpulseModule, release, lastLine(out))
	}
	if err := u.tidy(ctx, a); err != nil {
		return err
	}
	commit := &commit{label: "the impulse " + release + " tool pin step", subject: upgradeCommitTyp + ": impulse " + release, body: toolPinNote}

	return u.finish(ctx, f, repo, commit)
}

// step walks one step: the recipes, the pin bump, and the finish; a failing check stops
// with the brief written.
func (u *upgrader) step(ctx context.Context, f *transitionFlags, repo handoff.Repo, s *ledger.Step, number int) error {
	fmt.Fprintf(u.out, "=== step %d: %s ===\n", number, s.Note)
	changes, meanings, err := u.recipes(ctx, f.appDir, s, number)
	if err != nil {
		return err
	}
	a, err := app.Discover(f.appDir)
	if err != nil {
		return err
	}
	moves := s.Moves(ledger.AppPins(a.GoMod))
	if err := u.bumpPins(ctx, a, s); err != nil {
		return err
	}
	moved := strings.Join(moves, ", ")
	if moved == "" {
		moved = fmt.Sprintf("step %d", number)
	}
	commit := &commit{label: fmt.Sprintf("step %d (%s)", number, moved), subject: upgradeCommitTyp + ": " + moved, body: s.Note, changes: changes, meanings: meanings}
	if names := recipeNames(s); names != "" {
		commit.subject += " (" + names + ")"
		commit.body += "\n\nRecipes applied by impulse upgrade: " + strings.TrimPrefix(strings.TrimPrefix(names, "recipes "), "recipe ") + "."
	}

	return u.finish(ctx, f, repo, commit)
}

// commit is what a step commits and how the step is named when its check fails.
type commit struct {
	label   string
	subject string
	body    string
	// changes and meanings are what the step's recipes did and what it means, for the
	// brief when the check fails.
	changes  []string
	meanings []string
}

// finish ends a step after its edits: the application is read again, the owned files are
// rendered, go generate runs, the checks run, everything is staged, and a clean check is
// committed; a failing one writes the brief and stops.
func (u *upgrader) finish(ctx context.Context, f *transitionFlags, repo handoff.Repo, c *commit) error {
	a, err := app.Discover(f.appDir)
	if err != nil {
		return err
	}
	written, err := u.owned(a)
	if err != nil {
		return err
	}
	if len(written) > 0 {
		fmt.Fprintf(u.out, "Rewrote %s from the code.\n", ci.List(written))
	}
	if !f.skipGenerate {
		if err := u.regenerate(ctx, a); err != nil {
			return err
		}
	}
	env := &check.Env{App: a, Exec: u.exec, SkipGenerate: f.skipGenerate, Fix: true, Out: u.err}
	results := u.verify(ctx, env)
	if err := repo.StageAll(ctx); err != nil {
		return err
	}
	check.Report(u.out, results)
	if check.Failed(results) {
		guard, err := handoff.Take(a, handoff.FromTree(a))
		if err != nil {
			return err
		}
		brief := &handoff.Brief{App: a, Change: strings.Join(c.changes, "\n"), Meaning: strings.Join(c.meanings, "\n\n"), Results: results, Guard: guard}
		ag := &handoff.Agent{Command: f.agentCommand, ExtraArgs: f.agentArgs}
		if err := completeHandoff(ctx, u.out, f.appDir, env, repo, brief, ag, f.agent); err != nil {
			return err
		}

		return errors.Newf("%s left the check failing; its changes are staged and the brief is at %s. When the check is clean, commit and run impulse upgrade again: it resumes from go.mod", c.label, handoff.File)
	}
	if out, err := u.exec.Run(ctx, a.Root, nil, gitCommand, "commit", "-q", "-m", c.subject, "-m", c.body); err != nil {
		return errors.Wrapf(err, "git commit: %s", lastLine(out))
	}
	fmt.Fprintf(u.out, "Committed: %s.\n\n", c.subject)

	return nil
}

// recipes runs the step's recipes in order, each on the tree the one before it left, and
// answers what they changed and what the changes mean, for the brief.
func (u *upgrader) recipes(ctx context.Context, appDir string, s *ledger.Step, number int) (changes, meanings []string, err error) {
	for _, recipe := range s.Recipes {
		a, err := app.Discover(appDir)
		if err != nil {
			return nil, nil, err
		}
		found, err := recipe.Detect(ctx, a)
		if err != nil {
			return nil, nil, errors.Wrapf(err, "recipe %s", recipe.Name())
		}
		if len(found) == 0 {
			fmt.Fprintf(u.out, "Recipe %s: nothing in the old form.\n", recipe.Name())

			continue
		}
		fmt.Fprintf(u.out, "Recipe %s finds %d place(s) in the old form:\n", recipe.Name(), len(found))
		for _, line := range found {
			fmt.Fprintf(u.out, "  - %s\n", line)
		}
		change, err := recipe.Apply(ctx, a, u.exec)
		if err != nil {
			return nil, nil, errors.Wrapf(err, "recipe %s", recipe.Name())
		}
		change.Command = fmt.Sprintf("impulse upgrade (recipe %s, step %d)", recipe.Name(), number)
		writeChange(u.out, change)
		changes = append(changes, change.Text())
		meanings = append(meanings, recipe.Meaning())
	}

	return changes, meanings, nil
}

// bumpPins moves the framework pins the step moves to its set (a pin already at or beyond
// it stays), then tidies the module.
func (u *upgrader) bumpPins(ctx context.Context, a *app.App, s *ledger.Step) error {
	pins := ledger.AppPins(a.GoMod)
	for _, name := range s.PinNames() {
		if have, ok := pins[name]; ok && semver.Compare(have, s.Pins[name]) >= 0 {
			fmt.Fprintf(u.out, "%s is at %s already.\n", name, have)

			continue
		}
		fmt.Fprintf(u.out, "Pin %s to %s.\n", name, s.Pins[name])
		if out, err := u.exec.Run(ctx, a.Root, nil, goCommand, "get", name+"@"+s.Pins[name]); err != nil {
			return errors.Wrapf(err, "go get %s@%s: %s", name, s.Pins[name], lastLine(out))
		}
	}

	return u.tidy(ctx, a)
}

// tidy runs go mod tidy after the pins moved.
func (u *upgrader) tidy(ctx context.Context, a *app.App) error {
	if out, err := u.exec.Run(ctx, a.Root, nil, goCommand, "mod", "tidy"); err != nil {
		return errors.Wrapf(err, "go mod tidy: %s", lastLine(out))
	}

	return nil
}

// regenerate runs go generate and prints the schema warnings it raised.
func (u *upgrader) regenerate(ctx context.Context, a *app.App) error {
	fmt.Fprintf(u.out, "Running go generate ./...\n")
	out, err := u.exec.Run(ctx, a.Root, nil, goCommand, "generate", "./...")
	if err != nil {
		return errors.Wrapf(err, "go generate ./...: %s", lastLine(out))
	}
	for line := range strings.Lines(string(out)) {
		if strings.HasPrefix(line, transition_.WarningPrefix) {
			fmt.Fprintf(u.out, "  %s", line)
		}
	}

	return nil
}

// lastLine is the last non-empty line of a command's output, for an error.
func lastLine(out []byte) string {
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")

	return strings.TrimSpace(lines[len(lines)-1])
}
