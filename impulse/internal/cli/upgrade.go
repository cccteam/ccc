package cli

import (
	"context"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/go-playground/errors/v5"
	"github.com/spf13/cobra"

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

func newUpgrade() *cobra.Command {
	var (
		f      transitionFlags
		dryRun bool
		target string
	)

	cmd := &cobra.Command{
		Use:   "upgrade",
		Short: "Walk the application through the impulse releases after the one its pins stand at, one commit per release",
		Long: `upgrade moves an application forward through the impulse releases the ledger records,
release by release, and commits each. Where the application stands is read from the
framework pins in its go.mod (the resource, access, session and accesstypes versions it
builds against), never from a file of its own: the latest release whose pins they reach is
the position, and every release after it up to the running impulse's release is pending.

Each release is one step: its recipes run (each detects the old form in the application and
edits only where it finds it, so running twice is safe), the pins move to the release's set
and the impulse tool pin to the release (go get), the owned files are rendered again
(impulse render), go generate runs, impulse check runs, and everything is committed under
"` + upgradeCommitTyp + `: upgrade to impulse <version>". A failing check stops the walk with
the handoff brief written (` + handoff.File + `) and the step's changes staged: fix or hand
off, commit, and run upgrade again; it resumes from whatever go.mod says. No release is
skipped, since a recipe is written against the shape the release before it left behind.

The ledger starts at the first published impulse beta; before it, there is nothing to walk.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			u := &upgrader{
				exec:     check.OSExec{},
				releases: ledger.Releases,
				running:  check.RunningBuild(),
				verify: func(ctx context.Context, env *check.Env) []check.Result {
					return check.Run(ctx, env, check.All())
				},
				owned: func(a *app.App) (bool, error) {
					outcome, err := ci.Write(a)

					return outcome.Written, err
				},
				out: cmd.OutOrStdout(),
				err: cmd.ErrOrStderr(),
			}

			return u.run(cmd.Context(), &f, dryRun, target)
		},
	}
	f.bind(cmd)
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print the releases and recipes the walk would apply, and change nothing")
	cmd.Flags().StringVar(&target, "to", "", "stop at this impulse release instead of the running impulse's")

	return cmd
}

// upgrader walks an application through the ledger. Its parts are injected so the walk is
// tested without go, git or the checks.
type upgrader struct {
	exec     check.Execer
	releases []ledger.Release
	running  check.Build
	// verify runs the checks on the application after a step; owned rewrites the owned
	// files from the code and reports whether anything changed.
	verify func(ctx context.Context, env *check.Env) []check.Result
	owned  func(a *app.App) (bool, error)
	out    io.Writer
	err    io.Writer
}

// run reads the position, lists the pending releases, and walks them.
func (u *upgrader) run(ctx context.Context, f *transitionFlags, dryRun bool, target string) error {
	a, err := app.Discover(f.appDir)
	if err != nil {
		return err
	}
	if len(u.releases) == 0 {
		fmt.Fprintf(u.out, "The ledger records no impulse release yet (its first entry is the first published impulse beta): there is nothing to walk.\n")

		return nil
	}
	if target == "" && u.running.FromModule {
		target = u.running.Version
	}
	pins := ledger.AppPins(a.GoMod)
	pending, err := ledger.Pending(u.releases, pins, target)
	if err != nil {
		return err
	}
	position := ledger.Position(u.releases, pins)
	at := "before every recorded release"
	if position >= 0 {
		at = "at impulse " + u.releases[position].Version
	}
	if len(pending) == 0 {
		fmt.Fprintf(u.out, "%s stands %s, the latest the walk reaches: nothing to replay.\n", appLabel(a), at)

		return nil
	}
	fmt.Fprintf(u.out, "%s stands %s; %d release(s) to walk:\n", appLabel(a), at, len(pending))
	for i := range pending {
		fmt.Fprintf(u.out, "  %s\n", planLine(&pending[i]))
	}
	if !u.running.FromModule && target == "" {
		fmt.Fprintf(u.out, "This impulse was built from a checkout, so the walk ends at the ledger's last release rather than at the running impulse's.\n")
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
		return errors.Newf("the working tree is not clean (%d path(s)): commit or stash first, so each release is one commit", len(dirty))
	}
	for i := range pending {
		if err := u.step(ctx, f, repo, &pending[i]); err != nil {
			return err
		}
	}
	fmt.Fprintf(u.out, "%s stands at impulse %s. Review the commits and open the pull request.\n", appLabel(a), pending[len(pending)-1].Version)

	return nil
}

// appLabel names the application after its module path's last segment.
func appLabel(a *app.App) string {
	if a.GoMod == nil || a.GoMod.Module == nil {
		return "the application"
	}

	return path.Base(a.GoMod.Module.Mod.Path)
}

// planLine says what a release's step does: its version, its note, and its recipes.
func planLine(r *ledger.Release) string {
	names := make([]string, 0, len(r.Recipes))
	for _, recipe := range r.Recipes {
		names = append(names, recipe.Name())
	}
	if len(names) == 0 {
		return fmt.Sprintf("impulse %s: %s (the pins move; no recipe)", r.Version, r.Note)
	}

	return fmt.Sprintf("impulse %s: %s (recipes: %s)", r.Version, r.Note, strings.Join(names, ", "))
}

// step walks one release: the recipes, the pin bump, the owned files, the regeneration,
// the check, and the commit; a failing check stops with the brief written.
func (u *upgrader) step(ctx context.Context, f *transitionFlags, repo handoff.Repo, r *ledger.Release) error {
	fmt.Fprintf(u.out, "=== impulse %s: %s ===\n", r.Version, r.Note)
	changes, meanings, err := u.recipes(ctx, f.appDir, r)
	if err != nil {
		return err
	}
	a, err := app.Discover(f.appDir)
	if err != nil {
		return err
	}
	if err := u.bumpPins(ctx, a, r); err != nil {
		return err
	}
	// The pins moved, so the application is read again for the owned files and the checks.
	if a, err = app.Discover(f.appDir); err != nil {
		return err
	}
	written, err := u.owned(a)
	if err != nil {
		return err
	}
	if written {
		fmt.Fprintf(u.out, "Rewrote %s from the code.\n", ci.File)
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
		brief := &handoff.Brief{App: a, Change: strings.Join(changes, "\n"), Meaning: strings.Join(meanings, "\n\n"), Results: results, Guard: guard}
		ag := &handoff.Agent{Command: f.agentCommand, ExtraArgs: f.agentArgs}
		if err := completeHandoff(ctx, u.out, f.appDir, env, repo, brief, ag, f.agent); err != nil {
			return err
		}

		return errors.Newf("impulse %s left the check failing; its changes are staged and the brief is at %s. When the check is clean, commit and run impulse upgrade again: it resumes from go.mod", r.Version, handoff.File)
	}
	if err := u.commit(ctx, a, r); err != nil {
		return err
	}
	fmt.Fprintf(u.out, "Committed: %s: upgrade to impulse %s.\n\n", upgradeCommitTyp, r.Version)

	return nil
}

// recipes runs the release's recipes in order, each on the tree the one before it left,
// and answers what they changed and what the changes mean, for the brief.
func (u *upgrader) recipes(ctx context.Context, appDir string, r *ledger.Release) (changes, meanings []string, err error) {
	for _, recipe := range r.Recipes {
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
		change.Command = "impulse upgrade (recipe " + recipe.Name() + ", impulse " + r.Version + ")"
		writeChange(u.out, change)
		changes = append(changes, change.Text())
		meanings = append(meanings, recipe.Meaning())
	}

	return changes, meanings, nil
}

// bumpPins moves the framework pins to the release's set and the impulse tool pin to the
// release, then tidies the module.
func (u *upgrader) bumpPins(ctx context.Context, a *app.App, r *ledger.Release) error {
	for _, name := range r.PinNames() {
		fmt.Fprintf(u.out, "Pin %s to %s.\n", name, r.Pins[name])
		if out, err := u.exec.Run(ctx, a.Root, nil, goCommand, "get", name+"@"+r.Pins[name]); err != nil {
			return errors.Wrapf(err, "go get %s@%s: %s", name, r.Pins[name], lastLine(out))
		}
	}
	fmt.Fprintf(u.out, "Pin the impulse tool to %s.\n", r.Version)
	if out, err := u.exec.Run(ctx, a.Root, nil, goCommand, "get", "-tool", check.ImpulseModule+"@"+r.Version); err != nil {
		return errors.Wrapf(err, "go get -tool %s@%s: %s", check.ImpulseModule, r.Version, lastLine(out))
	}
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

// commit commits the staged step as the release's upgrade.
func (u *upgrader) commit(ctx context.Context, a *app.App, r *ledger.Release) error {
	subject := fmt.Sprintf("%s: upgrade to impulse %s", upgradeCommitTyp, r.Version)
	body := r.Note
	if len(r.Recipes) > 0 {
		names := make([]string, 0, len(r.Recipes))
		for _, recipe := range r.Recipes {
			names = append(names, recipe.Name())
		}
		body += "\n\nRecipes applied by impulse upgrade: " + strings.Join(names, ", ") + "."
	}
	if out, err := u.exec.Run(ctx, a.Root, nil, gitCommand, "commit", "-q", "-m", subject, "-m", body); err != nil {
		return errors.Wrapf(err, "git commit: %s", lastLine(out))
	}

	return nil
}

// lastLine is the last non-empty line of a command's output, for an error.
func lastLine(out []byte) string {
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")

	return strings.TrimSpace(lines[len(lines)-1])
}
