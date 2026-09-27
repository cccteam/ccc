package cli

import (
	"time"

	"github.com/spf13/cobra"

	"github.com/cccteam/ccc/bedrock/internal/deploy"
)

// deployUse names the command group.
const deployUse = "deploy"

// newDeploy is the command group over the deploy sequence's steps.
func newDeploy(d deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   deployUse,
		Short: "The deploy sequence, one command per step",
		Long: `deploy holds the steps of the deploy sequence as commands, each over the same inputs: the
facts the pipeline's resolve step exports to the workspace (environment.sh), the build as Cloud
Build describes it (build.json) and what the earlier steps left there. A pipeline lists the
steps it wants, and the rendered cloudbuild.yaml runs them from the bedrock image the placement
pins (bedrockImage). Today: record. The rest of the sequence follows, one step at a time.`,
	}
	cmd.AddCommand(newDeployRecord(d))

	return cmd
}

// newDeployRecord is deploy record.
func newDeployRecord(d deps) *cobra.Command {
	var workspace string
	cmd := &cobra.Command{
		Use:   "record",
		Short: "Write the deployment record once traffic has moved",
		Long: `record writes what this build deployed to the records bucket, as <app>/<env>/<release>/<build
id>.json: the version, the commit, the image and its digest, the regions and the revision each
runs, the time, and whether traffic shifted to it (live) or not (preview: a pull request's
revision, serving under its tag). The next environment's gate reads it, since a release reaches
an environment after it is live in the previous one. It reads the workspace: environment.sh
(the facts, with the image digest the build appended), build.json (the substitutions: the
application, the environment, the records bucket, the commit) and revisions.txt (the revisions
the deploy step created). A torn-down pull-request environment (SKIP_DEPLOY) records nothing.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			req, err := deploy.NewRecordRequest(deploy.Workspace(workspace), time.Now())
			if err != nil {
				return err
			}

			return deploy.WriteRecord(cmd.Context(), d.storage, req, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", "/workspace", "the directory the build's steps share")

	return cmd
}
