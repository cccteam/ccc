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
wants, and the rendered cloudbuild.yaml runs them with the bedrock its first step downloads: the
release the placement pins (bedrockVersion), verified against its checksum (bedrockSha256). Today:
resolve, validate-release, check-release, migrate, jobs, service, shift-traffic and record; the
image build stays a docker step.`,
	}
	cmd.AddCommand(newDeployResolve(d), newDeployValidateRelease(d), newDeployCheckRelease(d), newDeployMigrate(d), newDeployJobs(d), newDeployService(d), newDeployShiftTraffic(d), newDeployRecord(d))

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

// newDeployMigrate is deploy migrate.
func newDeployMigrate(d deps) *cobra.Command {
	var workspace string
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Run the migrate job with this build's image",
		Long: `migrate updates the migrate job to this build's image and the pipeline's labels (its variables,
identity, resources and retry policy are the application layer's) and runs it to completion through
the Cloud Run API, with the seed (schema/devseed as data migrations after the schema) where _SEED is
true: every pull request, its database being new, and a release build only in the environments the
placement's seed list names. A seeded database takes nothing twice. A build that runs no migrations
(shared-db) skips the job; a failed execution stops the build and names itself.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return deploy.Migrate(cmd.Context(), d.deploy, deploy.Workspace(workspace), cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", "/workspace", "the directory the build's steps share")

	return cmd
}

// newDeployJobs is deploy jobs.
func newDeployJobs(d deps) *cobra.Command {
	var workspace string
	cmd := &cobra.Command{
		Use:   "jobs",
		Short: "Update the job process's Cloud Run job to this build's image, without running it",
		Long: `jobs updates the job process's Cloud Run job (cmd/jobs, named by the stack's _JOBS_JOB) to this
build's image and the pipeline's labels through the Cloud Run API and does not run it: the
application runs its job process (the site through the Cloud Run API, or a schedule), and the
pipeline only keeps the job on the image every other process of the build runs. Its variables,
identity, timeout, retries and resources are the application layer's. The step runs after the
migrations, so a run the application starts from then on sees the migrated schema.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return deploy.Jobs(cmd.Context(), d.deploy, deploy.Workspace(workspace), cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", "/workspace", "the directory the build's steps share")

	return cmd
}

// newDeployService is deploy service.
func newDeployService(d deps) *cobra.Command {
	var workspace string
	cmd := &cobra.Command{
		Use:   "service",
		Short: "Put a new revision of the service in every region, receiving no traffic yet",
		Long: `service updates the service in every region to this build's image and the pipeline's labels
through the Cloud Run API; the service itself is the infrastructure's (its variables, secret
mounts, identity and scaling are the application layer's). Before each update the service is
checked for the state a failed earlier deploy leaves behind, traffic pointed at a revision that is
not ready, and repaired by moving traffic back to the last ready revision. The traffic that serves
now is pinned by revision name so the new revision takes none; a revision tag, when there is one,
names the new revision under its own URL. It leaves revisions.txt (region, service, revision per
line) for shift-traffic and record.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return deploy.Deploy(cmd.Context(), d.deploy, deploy.Workspace(workspace), cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", "/workspace", "the directory the build's steps share")

	return cmd
}

// newDeployShiftTraffic is deploy shift-traffic.
func newDeployShiftTraffic(d deps) *cobra.Command {
	var workspace string
	cmd := &cobra.Command{
		Use:   "shift-traffic",
		Short: "Move every region to 100 percent on its new revision",
		Long: `shift-traffic moves each service's traffic to the revision this build deployed (revisions.txt),
keeping the tags other revisions carry. A pull-request revision served under its tag alone leaves
the traffic where it is.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return deploy.ShiftTraffic(cmd.Context(), d.deploy, deploy.Workspace(workspace), cmd.OutOrStdout())
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
