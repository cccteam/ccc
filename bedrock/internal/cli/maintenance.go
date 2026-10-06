package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// maintenanceUse names the command.
const maintenanceUse = "maintenance"

// newMaintenance is maintenance off <env>.
func newMaintenance(d deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   maintenanceUse,
		Short: "Take an application out of a maintenance an earlier run left on, started from GitHub",
		Long: `maintenance off takes an application out of the maintenance a failed run left it in: a run that put
the maintenance page up and then stopped (a restore refused at its plan, a canceled build) leaves
every region's traffic on the maintenance revision and the task queue paused. The step moves the
traffic back to the revision the maintenance revision displaced (named by the label the maintenance
step put on it) and resumes the queue, and does nothing else: no stack, no migration, no deploy, no
record. The other way out is to run the release again (bedrock rerun), which deploys and ends the
maintenance on the way, and is the answer when the failed run should be finished rather than undone.`,
	}
	var dirFlag, placementFlag string
	off := &cobra.Command{
		Use:   "off <env>",
		Short: "Move traffic back to the revision that served before maintenance and resume the queue",
		Long: `off dispatches the operations workflow's maintenance action as the person signed in to gh (or
GITHUB_TOKEN). The workflow's job reads the environment's deployment records for the live release and
runs its build with the maintenance instruction: the pipeline's last step takes the application out
of maintenance and every other step is skipped; the build waits for its approval in Cloud Build where
the environment requires one. The command checks that the placement records the environment's project
and prints where to watch the run; it changes nothing itself.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			env := args[0]
			rc, err := d.operationTarget(dirFlag, placementFlag, env)
			if err != nil {
				return err
			}
			login, err := dispatchOperation(cmd.Context(), d, rc, env, "", map[string]string{actionInput: actionMaintenance})
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Asked, as %s, for %s to be taken out of maintenance: the operations workflow of %s/%s runs the live release's build with the maintenance instruction (%s), which moves the traffic back to the revision the maintenance revision displaced and resumes the queue, and deploys nothing.\n",
				login, env, rc.owner, rc.repo, workflowURL(rc))

			return nil
		},
	}
	off.Flags().StringVar(&dirFlag, "dir", "", "the stack directory holding the placement (default: the application's stack, found from the working directory)")
	off.Flags().StringVar(&placementFlag, "placement", "", "placement file (default: placement.json in the stack directory)")
	cmd.AddCommand(off)

	return cmd
}
