package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/go-playground/errors/v5"
	"github.com/spf13/cobra"

	"github.com/cccteam/ccc/impulse/internal/app"
	"github.com/cccteam/ccc/impulse/internal/check"
	"github.com/cccteam/ccc/impulse/internal/handoff"
)

func newHandoff() *cobra.Command {
	var (
		appDir       string
		agent        bool
		agentCommand string
		agentArgs    []string
		verify       bool
		skipGenerate bool
		reference    string
	)

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

The brief is written to ` + handoff.File + ` at the application root. It is transient:
the verification removes it when everything is clean, and it is never committed.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			a, err := app.Discover(appDir)
			if err != nil {
				return err
			}
			repo := handoff.For(a, check.OSExec{})
			if err := repo.Check(ctx); err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			env := &check.Env{App: a, Exec: check.OSExec{}, SkipGenerate: skipGenerate, Out: cmd.ErrOrStderr()}
			if verify {
				return verifyHandoff(ctx, env, repo, nil, out)
			}

			dirty, err := repo.Dirty(ctx)
			if err != nil {
				return err
			}
			if len(dirty) > 0 {
				return errors.Newf("the working tree is not clean (%s): commit or stash first, so the agent's work is exactly its own", strings.Join(dirty, ", "))
			}

			results := check.Run(ctx, env, check.All())
			check.Report(out, results)
			if !check.Failed(results) {
				fmt.Fprintf(out, "\nNothing to hand off: the check is clean.\n")

				return removeBrief(a)
			}
			guard, err := handoff.Take(a, handoff.FromTree(a))
			if err != nil {
				return err
			}
			brief := &handoff.Brief{App: a, Results: results, Reference: reference, Guard: guard}
			if err := os.WriteFile(a.Abs(handoff.File), []byte(brief.String()), 0o600); err != nil {
				return errors.Wrap(err, "os.WriteFile()")
			}
			ag := handoff.Agent{Command: agentCommand, ExtraArgs: agentArgs}
			report := handoffReport{results: results, agent: ag, styled: isTerminal(out)}
			if !agent {
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
		},
	}

	cmd.Flags().StringVar(&appDir, "app", ".", "application root (the directory holding go.mod)")
	cmd.Flags().BoolVar(&agent, "agent", false, "launch the agent on the brief and verify when it returns")
	cmd.Flags().StringVar(&agentCommand, "agent-command", handoff.DefaultCommand, "the agent executable")
	cmd.Flags().StringArrayVar(&agentArgs, "agent-arg", nil, "an argument appended to the agent's command line (repeatable), such as --model or --max-budget-usd")
	cmd.Flags().BoolVar(&verify, "verify", false, "verify a handoff done by hand: re-run the check and compare the guardrails against the index")
	cmd.Flags().BoolVar(&skipGenerate, "skip-generate", false, "skip the regen check (no emulator, no working-tree rewrite)")
	cmd.Flags().StringVar(&reference, "reference", "", "path of a finished application with the same options wired, for the agent to read")

	return cmd
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

		return exitError{code: 1}
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
	agent   handoff.Agent
	styled  bool
}

// obligations counts the failing checks' detail lines and the checks they fall under.
func (r *handoffReport) obligations() string {
	checks, lines := 0, 0
	for _, res := range r.results {
		if res.Status != check.Fail {
			continue
		}
		checks++
		lines += max(len(res.Details), 1)
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
}
