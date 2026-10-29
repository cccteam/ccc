// release.go is release: the release trigger run again on the default branch's head, in the
// first environment, where the trigger lives, started from GitHub. A push the platform failed
// to build (a build ended before its first step ran) leaves a merged release pull request
// untagged, with nothing to run the trigger again short of the next push; this is the door.

package cli

import (
	"fmt"

	"github.com/go-playground/errors/v5"
	"github.com/spf13/cobra"
)

// releaseUse names the command.
const releaseUse = "release"

// newRelease is release.
func newRelease(d deps) *cobra.Command {
	var dirFlag, placementFlag string
	cmd := &cobra.Command{
		Use:   releaseUse,
		Short: "Cut the release the default branch is owed: the release trigger run again on the branch's head, started from GitHub",
		Long: `release starts the operations workflow of the application's repository with the release action:
the workflow's job, in the GitHub Environment named after the first environment, exchanges its
GitHub token for that environment's operations identity and runs the application's release
trigger on the default branch's head, as a push to the branch runs it. The trigger lives in the
first environment alone, so the command names no environment, and no release, since the release
does not exist yet. The build runs as the release identity: the merge-back guard, then
release-please, which tags and publishes the release a merged release pull request is owed and
opens or refreshes the next release pull request from the commits since the last release; the tag
then runs its builds in every environment as a tag always has, the approval gates included. With
nothing pending it cuts nothing: the build runs and ends, and nothing changes. The command itself
changes nothing: it checks that the placement records the first environment's project (projects,
which bedrock org register prints), then dispatches the workflow as the person signed in to gh
(or GITHUB_TOKEN) and prints where to watch it. Nobody runs a trigger or submits a build by hand:
every build starts from a trigger, and the release trigger is run again through this door.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			rc, err := d.repository(dirFlag, placementFlag)
			if err != nil {
				return err
			}
			p := rc.placement
			if len(p.Environments) == 0 {
				return errors.New("the placement names no environment: the release trigger lives in the first one")
			}
			env := p.Environments[0]
			login, err := dispatchOperation(cmd.Context(), d, rc, env, "", map[string]string{actionInput: actionRelease})
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Asked, as %s, for the release trigger to run on %s: the operations workflow of %s/%s runs it in %s, where the trigger lives (%s). release-please cuts the release %s is owed, if a merged release pull request awaits its tag, and opens or refreshes the next release pull request; with nothing pending it cuts nothing. A tag made this way runs its builds as a push's tag would.\n",
				login, p.DefaultBranch, rc.owner, rc.repo, env, workflowURL(rc), p.DefaultBranch)

			return nil
		},
	}
	cmd.Flags().StringVar(&dirFlag, "dir", "", "the stack directory holding the placement (default: the application's stack, found from the working directory)")
	cmd.Flags().StringVar(&placementFlag, "placement", "", "placement file (default: placement.json in the stack directory)")

	return cmd
}
