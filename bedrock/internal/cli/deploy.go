package cli

import (
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/cccteam/ccc/bedrock/internal/deploy"
	"github.com/cccteam/ccc/bedrock/internal/render"
)

// deployUse names the command group.
const deployUse = "deploy"

// newDeploy is the command group over the deploy sequence's steps.
func newDeploy(d deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   deployUse,
		Short: "The deploy sequence, one command per step",
		Long: `deploy holds the steps of the deploy sequence as commands, each over the same inputs: the
facts the resolve step exports to the workspace (environment.sh), the build as Cloud Build
describes it (build.json) and what the earlier steps left there. A pipeline lists the steps it
wants, and the rendered cloudbuild.yaml runs them from the bedrock image the placement pins
(bedrockImage). Today: resolve, validate-release, check-release and record. The rest of the
sequence follows, one step at a time.`,
	}
	cmd.AddCommand(newDeployResolve(d), newDeployValidateRelease(d), newDeployCheckRelease(d), newDeployRecord(d))

	return cmd
}

// newDeployResolve is deploy resolve.
func newDeployResolve(d deps) *cobra.Command {
	var workspace string
	cmd := &cobra.Command{
		Use:   "resolve",
		Short: "Work out what this build deploys, where, and how",
		Long: `resolve is the first step of the sequence. It reads the build through the Cloud Build API
(BUILD_ID, PROJECT_ID and LOCATION, which the pipeline passes to the step), mints the repository's
GitHub token from the Cloud Build connection the trigger reads it through, and works out the facts
the later steps share: the trigger's kind (a tag's build, or a pull request's, which deploys only
to tst), the pull request's instruction (the words after its latest /gcbrun comment: shared-db,
reload-db, down), the image and its tags (<release>-<env> and <commit>-<env>), and whether the
migrate job runs and traffic shifts. It refuses a build that is neither a tag's nor a pull
request's, one that names no services or migrate job, a pull-request build with no connection or
no /gcbrun comment, an unknown option, and shared-db with reload-db. It writes environment.sh (the
facts, then every substitution of the build), build-args.sh (the declared substitutions as build
arguments) and build.json (the build as Cloud Build describes it) to the workspace.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			known, err := render.SubstitutionNames()
			if err != nil {
				return err
			}
			req := &deploy.ResolveRequest{BuildID: os.Getenv("BUILD_ID"), Project: os.Getenv("PROJECT_ID"), Location: os.Getenv("LOCATION"), Known: known}
			facts, err := deploy.Resolve(cmd.Context(), d.deploy, req, cmd.OutOrStdout())
			if err != nil {
				return err
			}

			return facts.Write(deploy.Workspace(workspace))
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", "/workspace", "the directory the build's steps share")

	return cmd
}

// newDeployValidateRelease is deploy validate-release.
func newDeployValidateRelease(d deps) *cobra.Command {
	var workspace string
	cmd := &cobra.Command{
		Use:   "validate-release",
		Short: "Refuse a tag that may not deploy here",
		Long: `validate-release is what stands between a tag and a build, for a tag build (a pull-request
build has no release to validate). Two checks through the GitHub API, with the token resolve
minted: the tag belongs to a GitHub Release cut by an accepted release actor (_RELEASE_ACTORS: the
release app as <slug>[bot]), which is how release-please, and nothing else, makes a release; and
the tagged commit is on the default branch, or it is a hotfix, the tip of hotfix/<major>.<minor>.x
whose base on the default branch carries a release tag of the same line. Then the record gate:
this environment follows the previous one in the promotion order (_PREVIOUS_ENV, empty in the
first), and a release runs here only after the previous environment holds a live deployment
record of it. A refusal starts with "Build REJECTED" and says why. It reads environment.sh and
build.json from the workspace.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return deploy.ValidateRelease(cmd.Context(), d.deploy, deploy.Workspace(workspace), cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", "/workspace", "the directory the build's steps share")

	return cmd
}

// newDeployCheckRelease is deploy check-release.
func newDeployCheckRelease(d deps) *cobra.Command {
	var workspace string
	cmd := &cobra.Command{
		Use:   "check-release",
		Short: "Decide whether the image build runs",
		Long: `check-release reads the registry before the image build. One registry serves every environment
and the image tags carry the environment (<release>-<env>, <commit>-<env>), so two digests tell
the story: neither tag exists, the build runs; the commit is built and the release tag is not,
the release name is added to that build and nothing is rebuilt (a tag moved, or a pull-request
release whose commit was fast-forwarded to the default branch); both exist and agree, the build
is reused; the release tag names another build, the run is refused, since a release names one
build per environment. It appends IMAGE_DIGEST and REUSE_IMAGE to environment.sh for the steps
after it.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return deploy.CheckRelease(cmd.Context(), d.deploy, deploy.Workspace(workspace), cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", "/workspace", "the directory the build's steps share")

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

			return deploy.WriteRecord(cmd.Context(), d.deploy.Storage, req, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", "/workspace", "the directory the build's steps share")

	return cmd
}
