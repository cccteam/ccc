package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// backupsUse names the command.
const backupsUse = "backups"

// newBackups is backups <env>.
func newBackups(d deps) *cobra.Command {
	var dirFlag, placementFlag string
	cmd := &cobra.Command{
		Use:   backupsUse + " <env>",
		Short: "List what an environment's database can be restored to, in a workflow run's summary",
		Long: `backups lists, in the summary of an operations workflow run, what an environment can be restored
to: every release that was live there, when, with its cut and the pre-release backup that holds the
database as it was before that release's migrations (kept fourteen days); and every generation of
the database a restore made, from which backup, when, the generation it left as the forensic copy
and that copy's forensic backup (kept thirty days). A generation's own history reaches back the
placement's spannerRetention while the generation exists; the backups are the fixed points. From
the listing, bedrock restore takes --before <release>, --backup <name> or --at <moment> (with
--of <database> for a moment in an earlier generation).

The command checks that the placement records the environment's project, dispatches the workflow's
list action as the person signed in to gh (or GITHUB_TOKEN), and prints where to read the summary;
nothing on this machine reads the cloud, and nothing changes.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			env := args[0]
			rc, err := d.operationTarget(dirFlag, placementFlag, env)
			if err != nil {
				return err
			}
			login, err := dispatchOperation(cmd.Context(), d, rc, env, "", map[string]string{actionInput: actionList})
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Asked, as %s, for the releases, backups and database generations of %s: the operations workflow of %s/%s reads them from the deployment records and writes them into its run's summary (%s). bedrock restore takes --before, --backup or --at from there.\n",
				login, env, rc.owner, rc.repo, workflowURL(rc))

			return nil
		},
	}
	cmd.Flags().StringVar(&dirFlag, "dir", "", "the stack directory holding the placement (default: the application's stack, found from the working directory)")
	cmd.Flags().StringVar(&placementFlag, "placement", "", "placement file (default: placement.json in the stack directory)")

	return cmd
}
