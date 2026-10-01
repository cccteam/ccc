package cli

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/go-playground/errors/v5"
	"github.com/spf13/cobra"

	"github.com/cccteam/ccc/bedrock/internal/github"
	"github.com/cccteam/ccc/bedrock/internal/render"
)

// restoreUse names the command; releaseInput the operations workflow's release input (its
// environment input is named like the label).
const (
	restoreUse   = "restore"
	releaseInput = "release"
)

// releaseTagRE is a release tag, v<major>.<minor>.<patch>.
var releaseTagRE = regexp.MustCompile(`^v\d+\.\d+\.\d+$`)

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
			ctx := cmd.Context()
			env, tag := args[0], args[1]
			rc, err := d.repository(dirFlag, placementFlag)
			if err != nil {
				return err
			}
			p := rc.placement
			if !slices.Contains(p.Environments, env) {
				return errors.Newf("%q is not one of the environments (%s)", env, strings.Join(p.Environments, ", "))
			}
			if env == p.Production() {
				return errors.Newf("%s is production, which is never restored by a run; a hotfix is based on the release production runs", env)
			}
			if !releaseTagRE.MatchString(tag) {
				return errors.Newf("%q is not a release tag (v<major>.<minor>.<patch>, such as v1.4.0)", tag)
			}
			if _, ok := p.Project(env); !ok {
				return errors.Newf("%s is not wired for operations: placement.json records no project for it (projects.%s, the id and the number, which bedrock org register prints); record it, render, and merge the rendered workflow first", env, env)
			}
			client, err := d.github(ctx)
			if err != nil {
				return err
			}
			login, err := client.User(ctx)
			if err != nil {
				return errors.Wrap(err, "reading who the token belongs to")
			}
			if _, err := client.TagCommit(ctx, rc.owner, rc.repo, tag); err != nil {
				if github.NotFound(err) {
					return errors.Newf("no release %s in %s/%s: an environment is restored to a release that exists", tag, rc.owner, rc.repo)
				}

				return errors.Wrapf(err, "resolving %s", tag)
			}
			inputs := map[string]string{environmentLabel: env, releaseInput: tag}
			if err := client.DispatchWorkflow(ctx, rc.owner, rc.repo, render.OperationsWorkflow, p.DefaultBranch, inputs); err != nil {
				return errors.Wrapf(err, "starting the operations workflow of %s/%s on %s", rc.owner, rc.repo, p.DefaultBranch)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Asked, as %s, for %s to be restored to %s: the operations workflow of %s/%s runs it (https://github.com/%s/%s/actions/workflows/%s). The run replaces %s's database (%s), deploys %s, and its record names you.\n",
				login, env, tag, rc.owner, rc.repo, rc.owner, rc.repo, render.OperationsWorkflow, env, p.RestoreKind(env), tag)

			return nil
		},
	}
	cmd.Flags().StringVar(&dirFlag, "dir", "", "the stack directory holding the placement (default: the application's stack, found from the working directory)")
	cmd.Flags().StringVar(&placementFlag, "placement", "", "placement file (default: placement.json in the stack directory)")

	return cmd
}
