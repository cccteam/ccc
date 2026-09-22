package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/go-playground/errors/v5"
	"github.com/spf13/cobra"

	"github.com/cccteam/ccc/impulse/internal/advise"
	"github.com/cccteam/ccc/impulse/internal/app"
	"github.com/cccteam/ccc/impulse/internal/check"
	"github.com/cccteam/ccc/impulse/internal/handoff"
)

func newAdvise() *cobra.Command {
	var f transitionFlags

	cmd := &cobra.Command{
		Use:   "advise",
		Short: "Write a brief on the schema warnings, audit findings, and role warnings for an agent's judgment",
		Long: `advise runs every generator program of the application with -audit and writes a brief for
an agent to ` + advise.File + ` at the application root: the warnings and findings each
program raised, where the accepted schema and role warnings are pinned, impulse's own
reading of each kind, and the question each kind leaves to judgment. The agent answers
in prose, names the edit it recommends, and edits nothing; the tool reads no test file
itself.

With --agent it launches Claude Code on the brief with the read-only tools, prints the
answer, and removes the brief; without it the brief stays and the command to run the
agent is printed. With --skip-generate no program is run and the brief names each
program's warnings test instead, whose pinned values the agent reads. Analysis by an agent
is by request: no other command does this.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			a, err := app.Discover(f.appDir)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			env := &check.Env{App: a, Exec: check.OSExec{}, SkipGenerate: f.skipGenerate, Out: cmd.ErrOrStderr()}
			brief, err := advise.Collect(ctx, env, f.skipGenerate)
			if err != nil {
				return err
			}
			if err := os.WriteFile(a.Abs(advise.File), []byte(brief.String()), 0o600); err != nil {
				return errors.Wrap(err, "os.WriteFile()")
			}
			ag := &handoff.Agent{Command: f.agentCommand, ExtraArgs: f.agentArgs, Tools: handoff.ReadOnlyTools, BriefFile: advise.File}
			gathered := fmt.Sprintf("%d warning(s) and %d finding(s) across %d program(s)", brief.Warnings(), brief.Findings(), len(brief.Programs))
			if brief.SkipGenerate {
				gathered = fmt.Sprintf("the pinned warnings of %d program(s), generation skipped", len(brief.Programs))
			}
			if !f.agent {
				writeAdviceReport(out, gathered, ag, isTerminal(out))

				return nil
			}

			fmt.Fprintf(out, "Asking %s about %s.\n\n", ag.Args()[0], gathered)
			if err := ag.Run(ctx, a.Root, brief.String(), out); err != nil {
				return err
			}
			if err := os.Remove(a.Abs(advise.File)); err != nil && !errors.Is(err, os.ErrNotExist) {
				return errors.Wrap(err, "os.Remove()")
			}

			return nil
		},
	}

	f.bind(cmd)

	return cmd
}

// writeAdviceReport tells the user what was written and how to run the agent by hand.
func writeAdviceReport(w io.Writer, gathered string, ag *handoff.Agent, styled bool) {
	bold := func(s string) string {
		if !styled {
			return s
		}

		return "\x1b[1m" + s + "\x1b[0m"
	}
	fmt.Fprintf(w, "Wrote the brief to %s: %s.\n", advise.File, gathered)
	fmt.Fprintf(w, "\n%s\n", bold("Next step"))
	fmt.Fprintf(w, "  %s\n", ag.CommandLine())
	fmt.Fprintf(w, "  Asks the agent for its reading, or read the brief and hand it to your own. It edits nothing; delete the brief when done.\n")
}
