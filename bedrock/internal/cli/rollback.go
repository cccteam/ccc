package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/go-playground/errors/v5"
	"github.com/spf13/cobra"

	"github.com/cccteam/ccc/bedrock/internal/prompt"
)

// rollbackUse names the command.
const rollbackUse = "rollback"

// newRollback is rollback <env> --reason <why> [--to <release>] [--at <moment>].
func newRollback(d deps) *cobra.Command {
	var dirFlag, placementFlag, to, at, reason string
	cmd := &cobra.Command{
		Use:   rollbackUse + " <env> --reason <why> [--to <release>] [--at <moment>]",
		Short: "Return an environment to an earlier release on that release's data, started from GitHub",
		Long: `rollback returns an environment whose release builds keep a release backup (the placement's
releaseBackups list; production unless it says otherwise) to an earlier release on the data of that
release's last moment. It is for production, where a release that went wrong is taken back; an
environment on production's instance returns to production's release with bedrock restore instead.

Every release build in such an environment starts a backup of the database as of the moment before
its migrations ran (the cut), kept fourteen days, so the release before it can be returned to on its
own data. The command checks the environment and the inputs, prints the statement (what returns to
what, on which data, asked for by whom and why), asks for the environment's name typed, and then
dispatches the repository's operations workflow as the person signed in to gh (or GITHUB_TOKEN). The
workflow's job reads the environment's deployment records for the live release, which the
environment leaves, and, unless named here, the release to return to (the release live before the
live one) and the backup to restore (the pre-release backup of the first release after it), prints
the statement with them, runs the environment's rollback trigger (never the release trigger) and
waits for its approval in Cloud Build for thirty minutes at most, canceling the build after that so
a rollback nobody approved is not left waiting. Where the organization's placement names production
reviewers, production's GitHub Environment waits for one of them first.

The build does the work as the deploy identity: the application goes into maintenance whatever the
window; the live database is kept, protected, as the forensic copy and a backup of it is taken as of
that moment, kept thirty days; the chosen backup is restored into the database's next generation
(<database>-2, then -3); the release's migrations run on it (nothing applies when the backup is at
the release's schema); the release deploys and takes the traffic; the record names the requester,
the reason, the release left, both backups and both databases. Writes made after the backup's
moment are in the forensic copy alone. The command itself changes nothing.

--to names the release to return to (one that was live in the environment); --at names an RFC 3339
moment, and a backup made as of it is restored instead of a release's pre-release backup (Spanner
keeps the past up to the environment's version retention, spannerRetention, seven days unless the
placement says otherwise). One of the two or neither.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			env := args[0]
			rc, err := d.operationTarget(dirFlag, placementFlag, env)
			if err != nil {
				return err
			}
			p := rc.placement
			if !p.KeepsReleaseBackups(env) {
				return errors.Newf("no release backup is kept in %s: the placement's releaseBackups list (%s) does not name it, so there is no backup of an earlier release to return to; bedrock restore serves it", env, strings.Join(p.ReleaseBackupEnvironments(), ", "))
			}
			reason = strings.TrimSpace(reason)
			switch {
			case reason == "":
				return errors.New("a rollback says why it was asked for: --reason, in a sentence")
			case strings.Contains(reason, "|"):
				return errors.New("the reason carries a | character, which the build's substitutions cannot; say it another way")
			case to != "" && at != "":
				return errors.New("--to names the release to return to and --at the moment whose data to restore: a rollback takes one of them, or neither for the release before the live one on its last data")
			}
			backup := ""
			if at != "" {
				moment, err := time.Parse(time.RFC3339, at)
				if err != nil {
					return errors.Newf("--at %q is not an RFC 3339 moment (2026-10-05T04:30:00Z)", at)
				}
				backup = "@" + moment.UTC().Format(time.RFC3339)
			}
			out := cmd.OutOrStdout()
			returns, data := "the release live before the live one", "its last data (the pre-release backup of the first release after it)"
			if to != "" {
				returns = to
			}
			if backup != "" {
				data = "the data as of " + at + " (a backup made as of then)"
			}
			fmt.Fprintf(out, "=== ROLLBACK of %s: %s returns to %s on %s, asked for by you: %s ===\n", env, rc.repo, returns, data, reason)
			fmt.Fprintln(out, "The application goes into maintenance. The live database is kept as the forensic copy and a backup of it is taken. The backup is restored into the database's next generation; the release's migrations run on it; the release deploys and takes the traffic; the record names all of it. Writes made after the backup's moment are in the forensic copy alone.")
			typed, err := prompt.New(cmd.InOrStdin(), out).Line("Type the environment's name (" + env + ") to ask for it, anything else to stop:")
			if err != nil {
				return errors.Wrap(err, "a rollback is asked for by typing the environment's name")
			}
			if typed != env {
				return errors.Newf("%q is not %s: the rollback was not asked for, and nothing changed", typed, env)
			}
			login, err := dispatchOperation(cmd.Context(), d, rc, env, to, map[string]string{actionInput: actionRollback, reasonInput: reason, backupInput: backup})
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
	cmd.Flags().StringVar(&at, "at", "", "an RFC 3339 moment whose data to restore, from a backup made as of it (default: the release's last data)")

	return cmd
}
