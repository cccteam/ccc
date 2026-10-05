package cli

import (
	"fmt"

	"github.com/go-playground/errors/v5"
	"github.com/spf13/cobra"

	"github.com/cccteam/ccc/bedrock/internal/derive"
)

// restoreUse names the command.
const restoreUse = "restore"

// newRestore is restore <env> [<release>].
func newRestore(d deps) *cobra.Command {
	var dirFlag, placementFlag string
	cmd := &cobra.Command{
		Use:   restoreUse + " <env> [<release>]",
		Short: "Restore an environment to a release, started from GitHub",
		Long: `restore starts the operations workflow of the application's repository for the environment and the
release. Its first case is staging's return to production: staging runs a release against
production's data before production does, so between releases it sits at production's release, and
the failure expected there is a migration meeting production's data. A restore of staging is a
rollback to production's release on production's backup, after which the failed release returns
through a hotfix. So for an environment restored from production's backup (one on production's
instance, off the seed list) the release may be left out, and production's live release is what the
environment returns to: the workflow's job reads it from production's deployment records and says
which; a release named is run as named, and the job says whether it is production's. The first
environment and a seeded one restore to an empty database, which has no production state to return
to, so they name their release.

The workflow's job, in the GitHub Environment named after the environment, exchanges its
GitHub token for the environment's operations identity and runs the environment's version trigger
for the release with the restore instruction. Everything that changes the environment happens
inside that run, in the pipeline's order, as the deploy identity: the environment's database is
replaced (an empty database for the first environment and for a seeded one, which the migrations
then fill, and the seed where the placement's seed list names the environment; production's most
recent backup for the environment on production's instance), the migrations run on the build worker
against the replaced database, the revision deploys, traffic moves and the record carries the reason
and who asked. The command itself changes nothing: it checks that
the environment is not production, that the release exists, and that the placement records the
environment's project (projects, which bedrock org register prints), then dispatches the workflow
as the person signed in to gh (or GITHUB_TOKEN) and prints where to watch it. A release waits for
its approval in Cloud Build as any release does there.`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			env, tag := args[0], ""
			if len(args) == 2 {
				tag = args[1]
			}
			rc, err := d.operationTarget(dirFlag, placementFlag, env)
			if err != nil {
				return err
			}
			if env == rc.placement.Production() {
				return errors.Newf("%s is production, which is never restored by a run; a hotfix is based on the release production runs, and bedrock rollback returns it to an earlier release", env)
			}
			kind := rc.placement.RestoreKind(env)
			if tag == "" && kind != derive.RestoreBackup {
				return errors.Newf("%s restores to an empty database (%s), which has no production state to return to: name the release to run", env, kind)
			}
			login, err := dispatchOperation(cmd.Context(), d, rc, env, tag, map[string]string{actionInput: actionRestore})
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			switch {
			case tag == "":
				fmt.Fprintf(out, "Asked, as %s, for %s to be restored to production's live release: the operations workflow of %s/%s runs it (%s). Its job reads the release from production's deployment records and says which; the run replaces %s's database (%s), deploys that release, and its record names you.\n",
					login, env, rc.owner, rc.repo, workflowURL(rc), env, kind)
			case kind == derive.RestoreBackup:
				fmt.Fprintf(out, "Asked, as %s, for %s to be restored to %s: the operations workflow of %s/%s runs it (%s). Its job says whether %s is production's live release, the one a restore from production's backup returns to when none is named; the run replaces %s's database (%s), deploys %s, and its record names you.\n",
					login, env, tag, rc.owner, rc.repo, workflowURL(rc), tag, env, kind, tag)
			default:
				fmt.Fprintf(out, "Asked, as %s, for %s to be restored to %s: the operations workflow of %s/%s runs it (%s). The run replaces %s's database (%s), deploys %s, and its record names you.\n",
					login, env, tag, rc.owner, rc.repo, workflowURL(rc), env, kind, tag)
			}

			return nil
		},
	}
	cmd.Flags().StringVar(&dirFlag, "dir", "", "the stack directory holding the placement (default: the application's stack, found from the working directory)")
	cmd.Flags().StringVar(&placementFlag, "placement", "", "placement file (default: placement.json in the stack directory)")

	return cmd
}
