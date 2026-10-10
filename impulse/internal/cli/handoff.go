package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/go-playground/errors/v5"
	"github.com/spf13/cobra"

	"github.com/cccteam/ccc/impulse/app"
	"github.com/cccteam/ccc/impulse/internal/check"
	"github.com/cccteam/ccc/impulse/internal/handoff"
	"github.com/cccteam/ccc/impulse/internal/ledger"
)

func newHandoff() *cobra.Command {
	var f handoffFlags

	cmd := &cobra.Command{
		Use:   "handoff",
		Short: "Hand the failing checks to an agent, then verify its work against the guardrails",
		Long: `handoff runs impulse check on a clean working tree and, when checks fail, writes a
brief for an agent: the failing checks verbatim as the obligations, the option set in
force, and the rules. With --agent it launches Claude Code on the brief and, when the
agent returns, re-runs the check and compares the guardrails (the generator programs'
option set and the lint configuration) against the index, so a weakened check is a
failure and not a pass. Without --agent it prints the command to run the agent by hand,
and --verify does the same verification afterwards.

With --staged the change under review is what is staged in the index, and the working
tree may hold it: the brief lists the staged paths under "What changed", with what each
--change says was changed and what each --meaning says it means, and the check, the
agent and the verification run as without the flag. A change beside the staged one,
unstaged or untracked, is refused, so the index holds exactly the change under review
and the agent's work is exactly its own. This is how impulse upgrade hands a failing step
to the release that added it: the walk stages the step and runs go tool impulse handoff
--staged through the pinned impulse, each recipe's change and meaning passed through, so
the brief, the agent and the verification are that release's.

The brief is written to ` + handoff.File + ` at the application root. It is transient:
the verification removes it when everything is clean, and it is never committed.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			h := &handoffer{
				exec: check.OSExec{},
				checks: func(ctx context.Context, env *check.Env) []check.Result {
					return check.Run(ctx, env, check.All())
				},
				out: cmd.OutOrStdout(),
				err: cmd.ErrOrStderr(),
			}

			return h.run(cmd.Context(), &f)
		},
	}
	f.bind(cmd)

	return cmd
}

// handoffFlags are impulse handoff's flags: the transitions' (the application root, the
// agent, the regen skip) and its own.
type handoffFlags struct {
	transitionFlags
	verify    bool
	reference string
	// staged says the change under review is the one staged in the index; changes and
	// meanings describe it for the brief, as a transition's change and meaning do.
	staged   bool
	changes  []string
	meanings []string
}

func (f *handoffFlags) bind(cmd *cobra.Command) {
	f.transitionFlags.bind(cmd)
	cmd.Flags().BoolVar(&f.verify, "verify", false, "verify a handoff done by hand: re-run the check and compare the guardrails against the index")
	cmd.Flags().StringVar(&f.reference, "reference", "", "path of a finished application with the same options wired, for the agent to read")
	cmd.Flags().BoolVar(&f.staged, "staged", false, "the change under review is what is staged in the index: the tree may hold it, and the brief lists its paths")
	cmd.Flags().StringArrayVar(&f.changes, "change", nil, "with --staged, what was changed, for the brief (repeatable; impulse upgrade passes each recipe's)")
	cmd.Flags().StringArrayVar(&f.meanings, "meaning", nil, "with --staged, what the change means, for the brief (repeatable)")
}

// handoffer is impulse handoff with its parts injected, so the command is tested without
// git or the checks.
type handoffer struct {
	exec   check.Execer
	checks func(ctx context.Context, env *check.Env) []check.Result
	out    io.Writer
	err    io.Writer
}

// run is the command: the tree is held to the handoff's premise, the check runs, and a
// failing one is handed off.
func (h *handoffer) run(ctx context.Context, f *handoffFlags) error {
	if (len(f.changes) > 0 || len(f.meanings) > 0) && !f.staged {
		return errors.New("--change and --meaning describe the change staged in the index: pass --staged with them")
	}
	a, err := app.Discover(f.appDir)
	if err != nil {
		return err
	}
	repo := handoff.For(a, h.exec)
	if err := repo.Check(ctx); err != nil {
		return err
	}
	env := &check.Env{App: a, Exec: h.exec, SkipGenerate: f.skipGenerate, Out: h.err, Ledger: ledger.Current{}}
	if f.verify {
		return verifyHandoff(ctx, env, repo, nil, h.out)
	}
	if err := refuseDirty(ctx, repo, f.staged); err != nil {
		return err
	}

	results := h.checks(ctx, env)
	check.Report(h.out, results)
	if !check.Failed(results) {
		fmt.Fprintf(h.out, "\nNothing to hand off: the check is clean.\n")

		return removeBrief(a)
	}
	guard, err := handoff.Take(a, handoff.FromTree(a))
	if err != nil {
		return err
	}
	brief := &handoff.Brief{App: a, Results: results, Reference: f.reference, Guard: guard}
	if f.staged {
		brief.Change = strings.Join(f.changes, "\n")
		brief.Meaning = strings.Join(f.meanings, "\n\n")
		if brief.Staged, err = repo.Staged(ctx); err != nil {
			return err
		}
	}
	ag := &handoff.Agent{Command: f.agentCommand, ExtraArgs: f.agentArgs}

	return completeHandoff(ctx, h.out, f.appDir, env, repo, brief, ag, f.agent)
}

// refuseDirty holds the tree to the handoff's premise, that the index is the tree at the
// handoff: clean, or with the staged change under review, holding nothing beside it.
func refuseDirty(ctx context.Context, repo handoff.Repo, staged bool) error {
	if staged {
		unstaged, err := repo.Unstaged(ctx)
		if err != nil {
			return err
		}
		if len(unstaged) > 0 {
			return errors.Newf("the working tree has changes beside the staged ones (%s): stage or stash them, so the index holds exactly the change under review and the agent's work is exactly its own", strings.Join(unstaged, ", "))
		}

		return nil
	}
	dirty, err := repo.Dirty(ctx)
	if err != nil {
		return err
	}
	if len(dirty) > 0 {
		return errors.Newf("the working tree is not clean (%s): commit or stash first, so the agent's work is exactly its own", strings.Join(dirty, ", "))
	}

	return nil
}

// completeHandoff writes the brief and either prints the command to run the agent or
// launches it and verifies its work. It is the tail every transition shares.
func completeHandoff(ctx context.Context, out io.Writer, appDir string, env *check.Env, repo handoff.Repo, brief *handoff.Brief, ag *handoff.Agent, launch bool) error {
	a := env.App
	if err := os.WriteFile(a.Abs(handoff.File), []byte(brief.String()), 0o600); err != nil {
		return errors.Wrap(err, "os.WriteFile()")
	}
	report := handoffReport{results: brief.Results, left: brief.Left, agent: ag, reference: brief.Reference, styled: isTerminal(out)}
	if !launch {
		report.write(out)

		return nil
	}

	base, err := handoff.Record(ctx, repo)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "\nHanding %s to %s.\n\n", report.obligations(), ag.Args()[0])
	if err := ag.Run(ctx, a.Root, brief.String(), out); err != nil {
		return err
	}
	fmt.Fprintf(out, "\nThe agent returned. Verifying.\n\n")
	// The agent changed the tree, so the application is read again.
	env.App, err = app.Discover(appDir)
	if err != nil {
		return err
	}

	return verifyHandoff(ctx, env, repo, base, out)
}

// verifyHandoff re-runs the check and the guardrail comparison and reports both. A clean
// result removes the brief.
func verifyHandoff(ctx context.Context, env *check.Env, repo handoff.Repo, base *handoff.Baseline, out io.Writer) error {
	results := check.Run(ctx, env, check.All())
	guard, err := handoff.Verify(ctx, env.App, repo, base)
	if err != nil {
		return err
	}
	results = append(results, guard)
	check.Report(out, results)
	if check.Failed(results) {
		fmt.Fprintf(out, "\nThe handoff is not clean. %s stays in place; fix the failures or hand off again.\n", handoff.File)

		return exitError{code: failedExit}
	}
	fmt.Fprintf(out, "\nClean. Review the diff and open the pull request.\n")

	return removeBrief(env.App)
}

func removeBrief(a *app.App) error {
	if err := os.Remove(a.Abs(handoff.File)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return errors.Wrap(err, "os.Remove()")
	}

	return nil
}

// handoffReport tells the user what was written and how to run the agent by hand.
type handoffReport struct {
	results []check.Result
	// left counts the obligations the change listed itself, outside the checks.
	left  int
	agent *handoff.Agent
	// reference is the finished application the brief points at, or empty.
	reference string
	styled    bool
}

// obligations counts the failing checks' detail lines and the checks they fall under,
// and the obligations the change listed itself.
func (r *handoffReport) obligations() string {
	checks, lines := 0, 0
	for _, res := range r.results {
		if res.Status != check.Fail {
			continue
		}
		checks++
		lines += max(len(res.Details), 1)
	}
	if r.left > 0 {
		return fmt.Sprintf("%d obligation(s) under %d failing check(s), and %d listed by the change", lines, checks, r.left)
	}

	return fmt.Sprintf("%d obligation(s) under %d failing check(s)", lines, checks)
}

func (r *handoffReport) write(w io.Writer) {
	bold := func(s string) string {
		if !r.styled {
			return s
		}

		return "\x1b[1m" + s + "\x1b[0m"
	}
	fmt.Fprintf(w, "\nWrote the brief to %s: %s.\n", handoff.File, r.obligations())
	fmt.Fprintf(w, "\n%s\n", bold("Next steps"))
	fmt.Fprintf(w, "  %s %s\n", bold("1."), r.agent.CommandLine())
	fmt.Fprintf(w, "     Runs the agent on the brief, or read the brief and do the work yourself.\n")
	fmt.Fprintf(w, "  %s impulse handoff --verify\n", bold("2."))
	fmt.Fprintf(w, "     Re-runs the check and compares the generator programs and lint configuration against the index.\n")
	if r.reference != "" {
		fmt.Fprintf(w, "     The brief points the agent at %s for the finished shape; delete it when done.\n", r.reference)
	}
}
