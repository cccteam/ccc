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

// The commands an upgrade step runs in the application, and the name the pinned impulse
// runs under (go tool impulse).
const (
	goCommand        = "go"
	gitCommand       = "git"
	impulseTool      = "impulse"
	upgradeCommitTyp = "upgrade"
)

// subjectLimit is the longest commit subject a step writes, in characters: one that still
// reads in a one-line log. Past it the pins' versions leave the subject for the body.
const subjectLimit = 100

// toolPinNote is the body of the commit of the tool-only move, the walk's last: the
// running impulse is a release that added no step.
const toolPinNote = "the impulse tool pin moves to the running impulse and the owned files are rendered again from the code"

// handoffStagedSince is the first impulse release whose handoff takes the walk's staged
// tree (impulse handoff --staged): the release the pull request that added the flag took.
// At a step checked by a pinned impulse from it on, that impulse writes the brief and
// runs the agent; at one checked by an earlier release, the walk reads its report back
// and writes the brief itself.
const handoffStagedSince = "v0.3.2"

func newUpgrade() *cobra.Command {
	var (
		f      transitionFlags
		dryRun bool
	)
	cmd := &cobra.Command{
		Use:   "upgrade",
		Short: "Move the application through the ledger's steps after the one its pins stand at, then to the running impulse, one commit each",
		Long: `upgrade moves an application through the steps the ledger records after the one its pins
stand at, then to the running impulse, and commits each. Where the application stands is
read from the framework pins in its go.mod (the resource, access, session and accesstypes
versions it builds against), never from a file of its own: the latest step whose pins they
reach is the position, and every step after it is pending. Each pending step is one commit:
its recipes run (each detects the old form in the application and edits only where it finds
it, so running twice is safe), the pins it moves move to its set (go get; a pin already at
or beyond the step's stays), the impulse tool pin moves to the release that added the step
(go get -tool; the tool directive puts that impulse's requirements into the build list, so
the pin moves with its step and never ahead of one), the owned files are rendered again,
go generate runs, impulse check runs (the pinned impulse renders and checks, through go
tool impulse, when it is not the running one), and everything is committed under
"` + upgradeCommitTyp + `: <the pins moved>". Last, when the running impulse is a release the
pin is still behind (one that added no step), the pin moves to it, the owned files are
rendered again, go generate and impulse check run, and the result is committed as
"` + upgradeCommitTyp + `: impulse <version>"; an impulse built from a checkout makes no such
move, and one older than the pin refuses, since the pinned one is the impulse to run. Run
the impulse to upgrade to without pinning it first (go run github.com/cccteam/ccc/impulse@<version>
upgrade): go get -tool would drag the framework pins ahead of the steps. A failing check
stops the walk with the step's changes staged and the handoff brief written (` + handoff.File + `).
At a step checked by the pinned impulse, that impulse writes the brief from the staged
tree and, with --agent, runs the agent and verifies its work, through go tool impulse
handoff --staged, from impulse ` + handoffStagedSince + ` on; a pinned release before it leaves the
walk to write the brief from that release's report, and the agent to the user. Fix or
hand off, commit, and run upgrade again; it resumes from whatever go.mod says. No step is
skipped, since a recipe is written against the shape the step before it left behind.`,
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

// execer runs the walk's commands: held until they end (Run), or streamed as they run
// (Stream), for the pinned impulse's handoff, whose agent talks as it works.
type execer interface {
	check.Execer
	check.Streamer
}

// upgrader walks an application through the ledger. Its parts are injected so the walk is
// tested without go, git or the checks.
type upgrader struct {
	exec    execer
	steps   []ledger.Step
	running check.Build
	// verify runs the checks on the application after a step; owned rewrites the owned
	// files from the code and reports whether anything changed.
	verify func(ctx context.Context, env *check.Env) []check.Result
	owned  func(a *app.App) ([]string, error)
	out    io.Writer
	err    io.Writer
}

// release is the running impulse's version when it was built from a module version, else
// "": a build from a checkout is no release.
func (u *upgrader) release() string {
	if u.running.FromModule && semver.IsValid(u.running.Version) {
		return u.running.Version
	}

	return ""
}

// plan is what a walk does: the pending steps, each moving the tool pin to the release
// that added it, and last the tool-only move, when the running impulse is a release the
// steps leave the pin behind.
type plan struct {
	// position is the index of the step the application stands at, -1 before the first.
	position int
	pending  []ledger.Step
	// toolPin is go.mod's impulse pin, "" when it has none; release is the running
	// impulse's version when it was built from a module version, else "".
	toolPin string
	release string
}

// afterSteps is the impulse the application pins once the pending steps are walked: the
// last release a pending step moves the pin to, or go.mod's pin when none does.
func (p *plan) afterSteps() string {
	pin := p.toolPin
	for i := range p.pending {
		if p.pending[i].MovesTool(pin) {
			pin = p.pending[i].Impulse
		}
	}

	return pin
}

// movesTool reports whether the walk ends with the tool-only move: the running impulse is
// a release and the steps leave the pin behind it.
func (p *plan) movesTool() bool {
	if p.release == "" {
		return false
	}
	pin := p.afterSteps()

	return pin == "" || semver.Compare(pin, p.release) < 0
}

// end is the impulse the application pins when the walk is done.
func (p *plan) end() string {
	if p.movesTool() {
		return p.release
	}

	return p.afterSteps()
}

// run reads the position and the tool pin, prints the plan, and walks it: the pending
// steps in order, then the tool-only move.
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
	for i := range p.pending {
		fmt.Fprintf(u.out, "  %s\n", planLine(u.steps, p.position+1+i))
	}
	if p.movesTool() {
		fmt.Fprintf(u.out, "  impulse %s: %s\n", p.release, toolPinNote)
	}
	if p.release == "" {
		fmt.Fprintf(u.out, "This impulse was built from a checkout (%s), so the tool-only move does not apply: the pin ends where the last step leaves it.\n", u.running.Version)
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
	for i := range p.pending {
		if err := u.step(ctx, f, repo, &p.pending[i], p.position+2+i); err != nil {
			return err
		}
	}
	if p.movesTool() {
		if err := u.moveTool(ctx, f, repo, p.release); err != nil {
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
	p := &plan{position: ledger.Position(u.steps, pins), pending: ledger.Pending(u.steps, pins), toolPin: goModImpulsePin(a.GoMod), release: u.release()}
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

// toolPinLabel names the tool pin's move as a step's moves name a framework pin's
// ("ccc/impulse v0.2.0").
func toolPinLabel(version string) string {
	return ledger.Short(check.ImpulseModule) + " " + version
}

// planLine says what a step does: its number, the pins it moves from the step before,
// its recipes, the tool pin it moves, and its note.
func planLine(steps []ledger.Step, i int) string {
	var prevPins map[string]string
	prevImpulse := ""
	if i > 0 {
		prevPins, prevImpulse = steps[i-1].Pins, steps[i-1].Impulse
	}
	s := &steps[i]
	line := fmt.Sprintf("step %d", i+1)
	if moves := s.Moves(prevPins); len(moves) > 0 {
		line += ", " + strings.Join(moves, ", ")
	}
	if names := recipeNames(s); names != "" {
		line += " (" + names + ")"
	}
	if s.MovesTool(prevImpulse) {
		line += ", pins " + toolPinLabel(s.Impulse)
	}

	return line + ": " + s.Note
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

// moveTool is the tool-only move, after the steps: the impulse tool pin moves to the
// running impulse, a release that added no step, and the move finishes as a step does:
// the owned files, the regeneration, the check, and the commit.
func (u *upgrader) moveTool(ctx context.Context, f *transitionFlags, repo handoff.Repo, release string) error {
	fmt.Fprintf(u.out, "=== impulse %s: %s ===\n", release, toolPinNote)
	a, err := app.Discover(f.appDir)
	if err != nil {
		return err
	}
	if err := u.pinTool(ctx, a, release); err != nil {
		return err
	}
	if err := u.tidy(ctx, a); err != nil {
		return err
	}
	commit := &commit{label: "the impulse " + release + " tool pin step", subject: upgradeCommitTyp + ": impulse " + release, body: toolPinNote}

	return u.finish(ctx, f, repo, commit)
}

// step walks one step: the recipes, the pin bump (the framework pins, then the tool pin),
// and the finish; a failing check stops with the brief written.
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
	if s.MovesTool(goModImpulsePin(a.GoMod)) {
		moves = append(moves, toolPinLabel(s.Impulse))
	}
	if err := u.bumpPins(ctx, a, s); err != nil {
		return err
	}
	moved := strings.Join(moves, ", ")
	if moved == "" {
		moved = fmt.Sprintf("step %d", number)
	}
	suffix, body := "", s.Note
	if names := recipeNames(s); names != "" {
		suffix = " (" + names + ")"
		body += "\n\nRecipes applied by impulse upgrade: " + strings.TrimPrefix(strings.TrimPrefix(names, "recipes "), "recipe ") + "."
	}
	subject := upgradeCommitTyp + ": " + moved + suffix
	if len(subject) > subjectLimit && len(moves) > 0 {
		// Pseudo-versions run long: the modules alone name the commit, the versions go
		// in the body.
		subject = upgradeCommitTyp + ": " + strings.Join(modulesOf(moves), ", ") + suffix
		body += "\n\nPins moved: " + moved + "."
	}
	commit := &commit{label: fmt.Sprintf("step %d (%s)", number, moved), subject: subject, body: body, changes: changes, meanings: meanings, release: s.Impulse}

	return u.finish(ctx, f, repo, commit)
}

// modulesOf names the modules of the moves ("ccc/resource v0.12.0" names ccc/resource).
func modulesOf(moves []string) []string {
	modules := make([]string, 0, len(moves))
	for _, move := range moves {
		modules = append(modules, strings.Fields(move)[0])
	}

	return modules
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
	// release is the impulse release that added the step, "" for the tool-only move.
	// When it is set and go.mod's pin is not the running release, the pinned impulse
	// renders the owned files and checks the step, through go tool impulse.
	release string
}

// finish ends a step after its edits: the application is read again, the owned files are
// rendered, go generate runs, the checks run (by the pinned impulse when the step is not
// the running release's), everything is staged, and a clean check is committed; a failing
// one is handed off and stops the walk.
func (u *upgrader) finish(ctx context.Context, f *transitionFlags, repo handoff.Repo, c *commit) error {
	a, err := app.Discover(f.appDir)
	if err != nil {
		return err
	}
	pinned := ""
	if pin := goModImpulsePin(a.GoMod); c.release != "" && pin != u.release() {
		pinned = pin
	}
	var (
		results []check.Result
		failed  bool
	)
	if pinned != "" {
		results, failed, err = u.checkPinned(ctx, f, a, pinned)
	} else {
		results, err = u.check(ctx, f, a)
		failed = check.Failed(results)
	}
	if err != nil {
		return err
	}
	if err := repo.StageAll(ctx); err != nil {
		return err
	}
	if failed {
		return u.handoff(ctx, f, repo, a, c, results, pinned)
	}
	if out, err := u.exec.Run(ctx, a.Root, nil, gitCommand, "commit", "-q", "-m", c.subject, "-m", c.body); err != nil {
		return errors.Wrapf(err, "git commit: %s", lastLine(out))
	}
	fmt.Fprintf(u.out, "Committed: %s.\n\n", c.subject)

	return nil
}

// check renders the owned files, regenerates, and runs the checks in process, by the
// running impulse, and reports them.
func (u *upgrader) check(ctx context.Context, f *transitionFlags, a *app.App) ([]check.Result, error) {
	written, err := u.owned(a)
	if err != nil {
		return nil, err
	}
	if len(written) > 0 {
		fmt.Fprintf(u.out, "Rewrote %s from the code.\n", ci.List(written))
	}
	if !f.skipGenerate {
		if err := u.regenerate(ctx, a); err != nil {
			return nil, err
		}
	}
	results := u.verify(ctx, u.env(f, a))
	check.Report(u.out, results)

	return results, nil
}

// checkPinned renders the owned files and runs the check through the impulse go.mod pins
// (go tool impulse), so a step is rendered and checked by the release that added it, and
// reports whether the check failed. The regeneration is the application's own generator
// program, built against its pins, and runs as always. A pinned release that writes its
// own brief (handoffStaged) says how its check ended by its exit status, failedExit for
// a failing check, and its report is printed and not read, since the brief comes from
// that release's own check; an earlier release's report is read back into results for
// the brief the walk writes. A check the pinned impulse could not run at all is an error,
// not a failure.
func (u *upgrader) checkPinned(ctx context.Context, f *transitionFlags, a *app.App, pinned string) ([]check.Result, bool, error) {
	fmt.Fprintf(u.out, "The pinned impulse %s renders the owned files and checks the step.\n", pinned)
	out, err := u.exec.Run(ctx, a.Root, nil, goCommand, "tool", impulseTool, "render")
	if err != nil {
		return nil, false, errors.Wrapf(err, "go tool impulse render: %s", lastLine(out))
	}
	fmt.Fprint(u.out, string(out))
	if !f.skipGenerate {
		if err := u.regenerate(ctx, a); err != nil {
			return nil, false, err
		}
	}
	args := []string{"tool", impulseTool, "check", "--fix"}
	if f.skipGenerate {
		args = append(args, "--skip-generate")
	}
	out, err = u.exec.Run(ctx, a.Root, nil, goCommand, args...)
	var (
		results []check.Result
		failed  bool
	)
	if handoffStaged(pinned) {
		failed = err != nil && exitCode(err) == failedExit
	} else {
		results = check.ParseReport(out)
		failed = check.Failed(results)
	}
	if err != nil && !failed {
		return nil, false, errors.Wrapf(err, "go tool impulse check: %s", lastLine(out))
	}
	fmt.Fprint(u.out, string(out))

	return results, failed, nil
}

// handoffStaged reports whether the pinned impulse writes the brief for the walk's staged
// tree: a release at or after handoffStagedSince. A pseudo-version names a commit between
// two releases and sorts before the later one, so it reads as a release without the flag.
func handoffStaged(pinned string) bool {
	return semver.IsValid(pinned) && semver.Compare(pinned, handoffStagedSince) >= 0
}

// exitCode is the status a command that ran exited with, read through the error its
// runner returned, or -1 for a command that did not run.
func exitCode(err error) int {
	var exited interface{ ExitCode() int }
	if errors.As(err, &exited) {
		return exited.ExitCode()
	}

	return -1
}

// env is the check environment of a step: the checks apply their mechanical remedies.
func (u *upgrader) env(f *transitionFlags, a *app.App) *check.Env {
	return &check.Env{App: a, Exec: u.exec, SkipGenerate: f.skipGenerate, Fix: true, Out: u.err, Ledger: ledger.Current{}}
}

// handoff stops the step with its changes staged and the brief written for the failing
// check. At a step checked in process the brief is written from the results, and the
// agent is launched when asked (--agent). At a step checked by a pinned impulse that
// writes its own brief (handoffStaged), that impulse does it all from the staged tree,
// through go tool impulse handoff --staged: the brief, the agent and the verification
// after it are the release that added the step's. An earlier pinned release's brief is
// written here from its report, and the agent is left to the user, since the
// verification after it would be the running impulse's.
func (u *upgrader) handoff(ctx context.Context, f *transitionFlags, repo handoff.Repo, a *app.App, c *commit, results []check.Result, pinned string) error {
	var err error
	switch {
	case pinned == "":
		err = u.handoffInProcess(ctx, f, repo, a, c, results, f.agent)
	case handoffStaged(pinned):
		err = u.handoffPinned(ctx, f, a, c, pinned)
	default:
		if f.agent {
			fmt.Fprintf(u.out, "The agent is not launched at a step checked by the pinned impulse %s, a release before %s; run it on the brief by hand.\n", pinned, handoffStagedSince)
		}
		err = u.handoffInProcess(ctx, f, repo, a, c, results, false)
	}
	if err != nil {
		return err
	}

	return errors.Newf("%s left the check failing; its changes are staged and the brief is at %s. When the check is clean, commit and run impulse upgrade again: it resumes from go.mod", c.label, handoff.File)
}

// handoffInProcess writes the brief from the results, with the staged paths as the
// change under review, and launches and verifies the agent when launch says so.
func (u *upgrader) handoffInProcess(ctx context.Context, f *transitionFlags, repo handoff.Repo, a *app.App, c *commit, results []check.Result, launch bool) error {
	guard, err := handoff.Take(a, handoff.FromTree(a))
	if err != nil {
		return err
	}
	staged, err := repo.Staged(ctx)
	if err != nil {
		return err
	}
	brief := &handoff.Brief{App: a, Change: strings.Join(c.changes, "\n"), Meaning: strings.Join(c.meanings, "\n\n"), Results: results, Guard: guard, Staged: staged}
	ag := &handoff.Agent{Command: f.agentCommand, ExtraArgs: f.agentArgs}

	return completeHandoff(ctx, u.out, f.appDir, u.env(f, a), repo, brief, ag, launch)
}

// handoffPinned hands the staged step to the pinned impulse's handoff (go tool impulse
// handoff --staged), which runs its check again for the brief, writes it with each
// recipe's change and meaning passed through, and launches and verifies the agent when
// asked. Its output is streamed, since the agent talks as it works.
func (u *upgrader) handoffPinned(ctx context.Context, f *transitionFlags, a *app.App, c *commit, pinned string) error {
	does := "writes the brief"
	if f.agent {
		does = "writes the brief, runs the agent and verifies its work"
	}
	fmt.Fprintf(u.out, "The pinned impulse %s %s, through go tool impulse handoff --staged.\n", pinned, does)
	args := []string{"tool", impulseTool, "handoff", "--staged"}
	for _, change := range c.changes {
		args = append(args, "--change", change)
	}
	for _, meaning := range c.meanings {
		args = append(args, "--meaning", meaning)
	}
	if f.skipGenerate {
		args = append(args, "--skip-generate")
	}
	if f.agent {
		args = append(args, "--agent", "--agent-command", f.agentCommand)
		for _, arg := range f.agentArgs {
			args = append(args, "--agent-arg", arg)
		}
	}
	if out, err := u.exec.Stream(ctx, a.Root, u.out, goCommand, args...); err != nil {
		return errors.Wrapf(err, "go tool impulse handoff: %s", lastLine(out))
	}

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
// it stays), then the tool pin to the release that added the step, when it names one and
// the pin is behind it, then tidies the module. The tool pin moves after the framework
// pins and never ahead of a step: the tool directive puts impulse's requirements into the
// build list, and moved early it would carry the framework pins past the steps between.
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
	if s.Impulse != "" {
		if pin := goModImpulsePin(a.GoMod); !s.MovesTool(pin) {
			fmt.Fprintf(u.out, "%s is at %s already.\n", check.ImpulseModule, pin)
		} else if err := u.pinTool(ctx, a, s.Impulse); err != nil {
			return err
		}
	}

	return u.tidy(ctx, a)
}

// pinTool moves the impulse tool pin to the version.
func (u *upgrader) pinTool(ctx context.Context, a *app.App, version string) error {
	fmt.Fprintf(u.out, "Pin the impulse tool to %s.\n", version)
	if out, err := u.exec.Run(ctx, a.Root, nil, goCommand, "get", "-tool", check.ImpulseModule+"@"+version); err != nil {
		return errors.Wrapf(err, "go get -tool %s@%s: %s", check.ImpulseModule, version, lastLine(out))
	}

	return nil
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
