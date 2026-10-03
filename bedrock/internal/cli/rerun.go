// rerun.go is rerun <env> <release>: a release's tag build run again in an environment,
// production included, started from GitHub.

package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// rerunUse names the command.
const rerunUse = "rerun"

// newRerun is rerun <env> <release>.
func newRerun(d deps) *cobra.Command {
	var dirFlag, placementFlag string
	cmd := &cobra.Command{
		Use:   rerunUse + " <env> <release>",
		Short: "Run a release again in an environment, production included, started from GitHub",
		Long: `rerun starts the operations workflow of the application's repository for the environment and the
release with the run action: the workflow's job, in the GitHub Environment named after the
environment, exchanges its GitHub token for the environment's operations identity and runs the
environment's version trigger for the release with no instruction, as the release's tag ran it. The
build runs from the start as the deploy identity: the image is built, the stack applied, the
migrations run (a migrate job that stopped at a statement continues from it once the cause is
fixed), the revision deploys, traffic moves, and the record names who asked. Production is reached
like any environment, since nothing of a rerun is a restore: the trigger's approval gate applies
as to any release, and the GitHub Environment's lock as to a restore. The command itself changes
nothing: it checks that the release exists and that the placement records the environment's
project (projects, which bedrock org register prints), then dispatches the workflow as the person
signed in to gh (or GITHUB_TOKEN) and prints where to watch it. Nobody runs a trigger or submits a
build by hand: every build starts from a trigger, and a release is run again through this door.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			env, tag := args[0], args[1]
			rc, err := d.operationTarget(dirFlag, placementFlag, env)
			if err != nil {
				return err
			}
			login, err := dispatchOperation(cmd.Context(), d, rc, env, tag, map[string]string{actionInput: actionRun})
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Asked, as %s, for %s to run %s again: the operations workflow of %s/%s runs it (%s). The release's tag build runs again from the start, waits for its approval in Cloud Build where %s requires one, and its record names you.\n",
				login, env, tag, rc.owner, rc.repo, workflowURL(rc), env)

			return nil
		},
	}
	cmd.Flags().StringVar(&dirFlag, "dir", "", "the stack directory holding the placement (default: the application's stack, found from the working directory)")
	cmd.Flags().StringVar(&placementFlag, "placement", "", "placement file (default: placement.json in the stack directory)")

	return cmd
}
