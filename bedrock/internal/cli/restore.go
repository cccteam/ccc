package cli

import (
	"fmt"

	"github.com/go-playground/errors/v5"
	"github.com/spf13/cobra"
)

// restoreUse names the command.
const restoreUse = "restore"

// newRestore is restore <env> <release>.
func newRestore(d deps) *cobra.Command {
	var dirFlag, placementFlag string
	cmd := &cobra.Command{
		Use:   restoreUse + " <env> <release>",
		Short: "Restore an environment to a release, started from GitHub",
		Long: `restore starts the operations workflow of the application's repository for the environment and the
release: the workflow's job, in the GitHub Environment named after the environment, exchanges its
GitHub token for the environment's operations identity and runs the environment's version trigger
for the release with the restore instruction. Everything that changes the environment happens
inside that run, in the pipeline's order, as the deploy identity: the environment's database is
replaced (an empty database for the first environment and for a seeded one, which the migrations
then fill, and the seed where the placement's seed list names the environment; production's most
recent backup for the environment on production's instance), the release's jobs are created, the migrations run, the revision deploys, traffic moves
and the record carries the reason and who asked. The command itself changes nothing: it checks that
the environment is not production, that the release exists, and that the placement records the
environment's project (projects, which bedrock org register prints), then dispatches the workflow
as the person signed in to gh (or GITHUB_TOKEN) and prints where to watch it. A release waits for
its approval in Cloud Build as any release does there.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			env, tag := args[0], args[1]
			rc, err := d.operationTarget(dirFlag, placementFlag, env)
			if err != nil {
				return err
			}
			if env == rc.placement.Production() {
				return errors.Newf("%s is production, which is never restored by a run; a hotfix is based on the release production runs", env)
			}
			login, err := dispatchOperation(cmd.Context(), d, rc, env, tag, map[string]string{actionInput: actionRestore})
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Asked, as %s, for %s to be restored to %s: the operations workflow of %s/%s runs it (%s). The run replaces %s's database (%s), deploys %s, and its record names you.\n",
				login, env, tag, rc.owner, rc.repo, workflowURL(rc), env, rc.placement.RestoreKind(env), tag)

			return nil
		},
	}
	cmd.Flags().StringVar(&dirFlag, "dir", "", "the stack directory holding the placement (default: the application's stack, found from the working directory)")
	cmd.Flags().StringVar(&placementFlag, "placement", "", "placement file (default: placement.json in the stack directory)")

	return cmd
}
