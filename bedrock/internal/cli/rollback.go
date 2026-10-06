package cli

import (
	"fmt"
	"strings"

	"github.com/go-playground/errors/v5"
	"github.com/spf13/cobra"

	"github.com/cccteam/ccc/bedrock/internal/prompt"
)

// rollbackUse names the command.
const rollbackUse = "rollback"

// newRollback is rollback <env> --reason <why> [--to <release>].
func newRollback(d deps) *cobra.Command {
	var dirFlag, placementFlag, to, reason string
	cmd := &cobra.Command{
		Use:   rollbackUse + " <env> --reason <why> [--to <release>]",
		Short: "Return an environment to an earlier release with nothing of the database, started from GitHub",
		Long: `rollback returns an environment to an earlier release and touches nothing of the database: the
earlier release's build runs again (its image reused, its stack applied, its revision deployed, the
traffic moved), with no migration run, no backup taken and nothing restored. It is the first answer
to a release that went wrong, in any environment: a schema change migrates forward in a way the
running code still works with, so the earlier release runs on the database as the release left it.
When the database is wrong too, bedrock restore returns it, in a run of its own, after this one.

The command checks the environment and the inputs, prints the statement (what returns to what,
asked for by whom and why), asks for the environment's name typed, and then dispatches the
repository's operations workflow as the person signed in to gh (or GITHUB_TOKEN). The workflow's
job reads the environment's deployment records for the live release, which the environment leaves,
and, unless named here, the release to return to (the release live before the live one), prints
the statement, runs the environment's rollback trigger (never the release trigger) and waits for
its approval in Cloud Build for thirty minutes at most, canceling the build after that so a
rollback nobody approved is not left waiting. Where the organization's placement names production
reviewers, production's GitHub Environment waits for one of them first.

The build does the work as the deploy identity, the way a release deploys: behind the maintenance
page where the return is breaking for the environment's clients, never waiting for a window; the
record names the requester, the reason, the release left and the migrations the database holds.
The command itself changes nothing.

--to names the release to return to (one that was live in the environment).`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			env := args[0]
			rc, err := d.operationTarget(dirFlag, placementFlag, env)
			if err != nil {
				return err
			}
			reason = strings.TrimSpace(reason)
			switch {
			case reason == "":
				return errors.New("a rollback says why it was asked for: --reason, in a sentence")
			case strings.Contains(reason, "|"):
				return errors.New("the reason carries a | character, which the build's substitutions cannot; say it another way")
			}
			out := cmd.OutOrStdout()
			returns := "the release live before the live one"
			if to != "" {
				returns = to
			}
			fmt.Fprintf(out, "=== ROLLBACK of %s: %s returns to %s, asked for by you: %s ===\n", env, rc.repo, returns, reason)
			fmt.Fprintln(out, "Nothing of the database: it stays as the live release left it, and the earlier release runs on it. No migration runs, no backup is taken, nothing is restored; the earlier release's build runs again and deploys as a release does. The database is returned by bedrock restore, in a run of its own.")
			typed, err := prompt.New(cmd.InOrStdin(), out).Line("Type the environment's name (" + env + ") to ask for it, anything else to stop:")
			if err != nil {
				return errors.Wrap(err, "a rollback is asked for by typing the environment's name")
			}
			if typed != env {
				return errors.Newf("%q is not %s: the rollback was not asked for, and nothing changed", typed, env)
			}
			login, err := dispatchOperation(cmd.Context(), d, rc, env, to, map[string]string{actionInput: actionRollback, reasonInput: reason})
			if err != nil {
				return err
			}
			fmt.Fprintf(out, "Asked, as %s, for %s to return to %s: the operations workflow of %s/%s runs it (%s). Its job reads the environment's records for what is live and what it returns to, prints the statement, runs the rollback trigger and waits for the approval in Cloud Build, thirty minutes at most; the record names you and the reason.\n",
				login, env, returns, rc.owner, rc.repo, workflowURL(rc))

			return nil
		},
	}
	cmd.Flags().StringVar(&dirFlag, "dir", "", "the stack directory holding the placement (default: the application's stack, found from the working directory)")
	cmd.Flags().StringVar(&placementFlag, "placement", "", "placement file (default: placement.json in the stack directory)")
	cmd.Flags().StringVar(&reason, "reason", "", "why the rollback is asked for, in a sentence (required)")
	cmd.Flags().StringVar(&to, "to", "", "the release to return to (default: the release live before the live one)")

	return cmd
}
