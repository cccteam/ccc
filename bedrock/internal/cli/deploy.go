package cli

import (
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/cccteam/ccc/bedrock/internal/deploy"
	"github.com/cccteam/ccc/bedrock/internal/derive"
	"github.com/cccteam/ccc/bedrock/internal/hook"
	"github.com/cccteam/ccc/bedrock/internal/render"
)

// deployUse names the command group.
const deployUse = "deploy"

// newDeploy is the command group over the deploy sequence's steps.
func newDeploy(d deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   deployUse,
		Short: "The deploy sequence, one command per step",
		Long: `deploy holds the steps of the deploy sequence as commands, one per pipeline step, each over the
same inputs: the facts resolve writes to the workspace (environment.sh), the build as Cloud Build
describes it (build.json) and what the earlier steps appended. A step reads those files, does one
thing, and appends what it learned for the steps after it; no step installs anything. The rendered
cloudbuild.yaml runs them with the bedrock its first step gets: for a release pin, the release the
placement pins (bedrockVersion) downloaded and verified against its checksum (bedrockSha256); for
a commit pin, that commit built with go install and verified by Go's checksum database. The steps,
in order: resolve, validate-release, guard-migrations, plan-environments, pr-stack plan, pr-stack
guard, pr-stack apply, check-release, build-image, maintenance on (a restore run), stack plan, stack
apply, jobs (an application with a job process), migrate --preflight, window, maintenance on
--window (a breaking release), migrate, service, shift-traffic, maintenance off, sweep-jobs (a job
process again), record and talk-back, with hook <stage> where the application commits a hook
script. The hourly sweep runs sweep.`,
	}
	envStack := &cobra.Command{
		Use:   "stack",
		Short: "The environment's stack in a tag build: plan, apply",
		Long: `stack holds the two steps of a tag build that apply the environment's application stack, after the
image build and before the migrations, as the apply identity: plan runs tofu init on the
environment's state prefix (3-app/<app>/<env>), saves the plan with its JSON, prints and appends its
summary and runs the tests; apply applies exactly that plan. The plan is this build's own: the one a
reviewer approved on the pull request was bound to the state of that moment, and a release bundles
several pull requests. A pull-request build skips both and plans every environment earlier
(plan-environments).`,
	}
	envStack.AddCommand(newDeployEnvStackPlan(d), newDeployEnvStackApply(d))
	stack := &cobra.Command{
		Use:   "pr-stack",
		Short: "A pull request's own environment: plan, guard, apply",
		Long: `pr-stack holds the three steps that stand a pull request's environment up, or take it down on
/gcbrun down: the application's stack applied into the pull request's own state prefix
(3-app/<app>/<env>/pr<N>) as the apply identity. plan saves the plan, guard lets only the pull
request's own resources through, apply applies exactly that plan. A tag build skips all three.`,
	}
	stack.AddCommand(newDeployStackPlan(d), newDeployStackGuard(d), newDeployStackApply(d))
	cmd.AddCommand(newDeployResolve(d), newDeployValidateRelease(d), newDeployGuardMigrations(d), newDeployPlanEnvironments(d), stack, newDeployHook(d),
		newDeployCheckRelease(d), newDeployBuildImage(d), envStack, newDeployBackup(d), newDeployMigrate(d), newDeployJobs(d), newDeployService(d),
		newDeployShiftTraffic(d), newDeploySweepJobs(d), newDeployRecord(d), newDeployTalkBack(d), newDeploySweep(d), newDeployMaintenance(d), newDeployWindow(d))

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
migrations run and traffic shifts. It refuses a build that is neither a tag's nor a pull
request's, one that names no services, a pull-request build with no connection or
no /gcbrun comment, an unknown option, and shared-db with reload-db. A tag build may carry a
restore instruction (_RESTORE: empty, or production-backup for the environment on production's
instance; _REQUESTER names who asked): the environment's database is replaced before the release
deploys, which the facts carry on (RESTORE, RESTORE_REQUESTER); a pull-request build carries none,
and production is never restored by a run. A requester alone is a rerun (bedrock rerun: the
release's tag build again, production included), which the record names. Whether the build seeds
(SEED) is read from the placement in the checkout (infrastructure/placement.json): every pull
request seeds, and a tag build seeds where the placement's seed list names the environment, so a
release that changes the list seeds with its own; the trigger's _SEED, from its stack's last
apply, stays among the substitutions as what the trigger said, and the log says when the two
differ. In an environment on the seed list a tag build decides a restore itself when the tree no
longer carries a seed file as the environment's live release applied it (the release's record lists the seed files with their
hashes): the release is the requester and the reason is a fact of its own (RESTORE_REASON), on the
record; a restore asked for takes precedence, and a seed file added beside the applied ones
recreates nothing. The substitutions the application declares for its hooks and its image build
and its build secrets' pins are read from the checkout the same way (substitutions and
build_secrets in infrastructure/terraform.tfvars; the build secrets' containers alone from the
trigger), so a release that changes them builds with its own; a declared name the contract
carries, or one starting with _BUILD_ARG_, is refused. The build arguments the placement in the
checkout declares (buildArguments) are values of the stack's, so a tag build exports the trigger's
_BUILD_ARG_<NAME> for each name the checkout declares, saying when the trigger does not carry one
yet (it comes with this build's stack apply, and reaches the image from the next release on), and
a pull-request build exports none, its image taking the pull request's own stack's values (deploy
pr-stack apply). It writes environment.sh (the facts, BUILD_SECRETS among them, then the
contract's substitutions as the trigger passed them and the declared ones as the checkout declares
them), build-args.txt (the declared substitutions as the image build's arguments, NAME=value lines)
and build.json (the build as Cloud Build describes it) to the workspace.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			known, err := render.SubstitutionNames()
			if err != nil {
				return err
			}
			req := &deploy.ResolveRequest{BuildID: os.Getenv("BUILD_ID"), Project: os.Getenv("PROJECT_ID"), Location: os.Getenv("LOCATION"), Known: known, Source: workspace}
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
	var workspace, routerDir string
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
record of it. A hotfix passes one more check: this environment's newest live record lists the
migration and seed files its database holds, each with its hash, and the hotfix is refused when
the database holds a file it does not carry, or one whose content differs, naming the file; the
environment is restored to the hotfix first (a restore run replaces the database and skips the
check); in prd the hotfix must also be on the line production runs. Last the maintenance window:
the release is breaking when the oldest release its session outlets still answer, read from the
release file the resource generator writes beside the generated router (--router-dir names the
router package; zz_gen_release.json), is newer than the release this environment runs live (its
newest live deployment record), or when an outlet answers its own release alone ("this"); a
breaking release deploys behind the maintenance page inside the environment's maintenance window
(placement.json, "maintenance"), and under "releases": "all" every release waits for the window.
The step leaves the decision (WINDOW_NEEDED, WINDOW_BREAKING, WINDOW_REASON) and refuses here what
can never proceed: prd without a setting when the release is breaking, a window whose next opening
is further away than the build can wait (its timeout less three hours for the steps after the
window), a window with no opening ahead. A restore run and an environment in maintenance from an
earlier run pass the gate. Without a release file no outlet declares an oldest answered release,
which it says, and no release is breaking. A pull-request build previews the window instead: what
its release turns away in each environment, read as that environment's plan identity, and which
environments hold it. A refusal starts with "Build REJECTED" and says why. A tag build's log first
names the bedrock running it, and says when that is a commit pin, which it does not refuse. It
reads environment.sh and build.json from the workspace, and the migration files, the placement
(infrastructure/placement.json) and the release file from the checkout.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return deploy.ValidateRelease(cmd.Context(), d.deploy, deploy.Workspace(workspace), d.running().version, routerDir, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", "/workspace", "the directory the build's steps share")
	cmd.Flags().StringVar(&routerDir, "router-dir", "", "the generated router's package directory in the checkout, root-relative, where the resource generator writes the release file (zz_gen_release.json); empty when the application generates no router")

	return cmd
}

// newDeployWindow is deploy window.
func newDeployWindow(d deps) *cobra.Command {
	var workspace string
	cmd := &cobra.Command{
		Use:   "window",
		Short: "Wait for the environment's maintenance window to open",
		Long: `window is the gate of a window release: a run validate-release found to need the environment's
maintenance window (WINDOW_NEEDED), a breaking release or any release where the setting says all.
It runs after the image is built, the stack applied and the jobs made, and the pre-flight has run,
so the window holds only maintenance, the migrations and the rollout. It reads the environment's
setting from the checkout's placement, prints when the run will proceed and waits, reading the
clock again at most every ten minutes; when the window opens it appends when (WINDOW_OPENED), how
long it waited (WINDOW_WAITED) and which opening let it in (WINDOW_SLOT) for the record. An
opening further away than the build can wait stops the run here, before anything changes, naming
it. A run that waits for no window says so and ends; a run in maintenance already (a restore run,
or a rerun after a window release that failed, whose service carries the maintenance variable)
passes at once, since the interruption has happened and waiting would hold the client on the
maintenance page until the next window.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return deploy.WaitForWindow(cmd.Context(), d.deploy, deploy.Workspace(workspace), cmd.OutOrStdout())
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

// migrateProgram is where the image build leaves the migrate command for the migration
// steps: the home directory every step of a build shares, beside the hooks program.
const migrateProgram = "/builder/home/migrate"

// newDeployMigrate is deploy migrate.
func newDeployMigrate(d deps) *cobra.Command {
	var (
		workspace, programPath, versionVariable string
		preflight                               bool
	)
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Run the release's migrate command on this worker",
		Long: `migrate runs the migrations on the build worker: the release's own migrate command, which build-image
took out of the environment's image (/migrate), run in the checkout as the deploy identity with the
variables the stack derived for it (MIGRATE_ENV, which the stack steps read from the applied stack's
substitutions output: the levels the command constructs, and no secret) and the variable the image
sets to the release (--version-variable) set to the build's version, with the seed (schema/devseed as
data migrations after the schema) where resolve's SEED fact is true: every pull request, its
database being new, and a release build only in the environments the seed list of the placement in
the checkout names, so a release that changes the list migrates with its own. A seeded database
takes nothing twice. The command reaches Spanner and Firestore through their APIs as the deploy
identity, which the stack grants database admin on the application's own database; its lines go
straight into the build log, and the deployment record lists the migrations applied. A build that
runs no migrations (shared-db) runs nothing; a command that exits with an error stops the build, its
message above. With --preflight, in a run that waits for the maintenance window (WINDOW_NEEDED) and
replaces no database, the command is run once with -version instead, before the wait: it loads its
configuration against the environment's database and prints what the migrations tables say, so a
configuration that does not load or a database that cannot be reached stops the run with nothing
changed and the window not entered; nothing is applied. Any other run says so and does nothing.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return deploy.Migrate(cmd.Context(), d.deploy, deploy.Workspace(workspace), programPath, versionVariable, preflight, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", "/workspace", "the directory the build's steps share")
	cmd.Flags().StringVar(&programPath, "program-path", migrateProgram, "where build-image left the migrate command")
	cmd.Flags().StringVar(&versionVariable, "version-variable", "", "the variable the image sets to the release, set to the build's version for the command; none when empty")
	cmd.Flags().BoolVar(&preflight, "preflight", false, "run the command once with -version before the wait for the maintenance window")

	return cmd
}

// newDeployJobs is deploy jobs.
func newDeployJobs(d deps) *cobra.Command {
	var workspace string
	cmd := &cobra.Command{
		Use:   "jobs",
		Short: "Make this build's job of the job process from the stack's template job, without running it",
		Long: `jobs makes this build's job of the job process (cmd/jobs) after the stack's apply, for an
application with one: a copy of the template job the stack owns (named by the stack's _JOBS_JOB;
never run, never deployed to), named <template>-<version key> (v0.1.15 gives v0-1-15) and put on
this build's image with the pipeline's labels through the Cloud Run API, with the template's IAM
policy (the stack grants the site's identity run.invoker on the template; the copy is what lets the
site start this job): the service carries the template's name (APP_JOBS_TEMPLATE, set by the stack)
and the image its version, and the framework names the job of its own build from the two, so the
revision this build deploys starts a job of its own code, and a traffic rollback to an earlier
revision starts that revision's job. Only the running service starts the job process: the pipeline never runs it, a
hook never starts it, and a schedule calls an endpoint on the service, which starts it. The job's
variables, identity, timeout, retries and resources are the template's, the application layer's. A
build of a version this environment deployed before updates the job it made then. The step runs
before the migrations: making a job touches no data, so a failure here stops the run with the
database untouched. An application without a job process has no step for it; a torn-down
pull-request environment has nothing to make.`,
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

// newDeployBackup is deploy backup.
func newDeployBackup(d deps) *cobra.Command {
	var workspace string
	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Start the release backup: the database as of the cut, before the migrations",
		Long: `backup starts, as the apply identity, a Spanner backup of the environment's database as of this
moment, the cut, in a tag build whose environment the placement's releaseBackups list names (production
alone unless it says otherwise) and that applies its migrations; the backup is named after the database
and the release, is kept fourteen days, and is what bedrock rollback restores into the database's next
generation when the release goes wrong. The step waits for nothing: Spanner takes the backup while the
release goes on. Its facts (CUT, RELEASE_BACKUP, RELEASE_BACKUP_TIME, RELEASE_BACKUP_EXPIRES) reach
the deployment record. A pull-request build, a run that deploys nothing or applies no migration, a
restore run and a rollback run take no backup and say so. A backup that cannot start fails the step,
before any migration ran.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return deploy.StartReleaseBackup(cmd.Context(), d.deploy, deploy.Workspace(workspace), time.Now(), cmd.OutOrStdout())
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
revision, serving under its tag), with the plan of the stack the build applied and, in a restore
run, what replaced the database, who asked and what the stack replaced. The next environment's
gate reads it, since a release reaches an environment after it is live in the previous one. It
reads the workspace: environment.sh
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

// workspaceFlag is the flag every step takes for the directory the build's steps share.
func workspaceFlag(cmd *cobra.Command, workspace *string) {
	cmd.Flags().StringVar(workspace, "workspace", "/workspace", "the directory the build's steps share (the checkout)")
}

// newDeployGuardMigrations is deploy guard-migrations.
func newDeployGuardMigrations(d deps) *cobra.Command {
	var workspace string
	cmd := &cobra.Command{
		Use:   "guard-migrations",
		Short: "Refuse migrations the migrate command could not apply in order",
		Long: `guard-migrations is the deploy gate for the migration rule bedrock check applies before the
merge. The schema migrations directory and the seed directory beside it (schema/devseed) must each
be one sequence: six-digit indexes, one up file each, at most one down, contiguous. In a
pull-request build, every schema migration the branch started from must still be in the tree
unchanged, and the sequence is read together with the default branch's, so an index the default
branch took since the branch was cut is refused now rather than after the merge. Seed files are
development data: a pull request may edit or remove one as long as the directory stays one
sequence (a removal renumbers the files after it), and a changed seed applies from the start by
recreating the pull request's database on its next build. A refusal lists every problem, names
the fix (bedrock migration renumber, which go generate runs) and is posted on the pull request.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return deploy.GuardMigrations(cmd.Context(), d.deploy, deploy.Workspace(workspace), cmd.OutOrStdout())
		},
	}
	workspaceFlag(cmd, &workspace)

	return cmd
}

// newDeployStackPlan is deploy pr-stack plan.
func newDeployStackPlan(d deps) *cobra.Command {
	var workspace string
	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Plan the pull request's stack and save the plan",
		Long: `plan runs tofu init and tofu plan in the checkout's infrastructure directory as the apply identity,
against the pull request's own state prefix: the destroy on /gcbrun down; without a database of
its own on /gcbrun shared-db; with the pull request's database replaced when resolve decided it is
recreated and it exists. It leaves the plan (pr.plan) and its JSON (pr-plan.json) in the workspace.
It runs in the OpenTofu image, whose tofu it drives.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return deploy.PlanStack(cmd.Context(), d.deploy, deploy.Workspace(workspace), cmd.OutOrStdout())
		},
	}
	workspaceFlag(cmd, &workspace)

	return cmd
}

// newDeployStackGuard is deploy pr-stack guard.
func newDeployStackGuard(d deps) *cobra.Command {
	var workspace string
	cmd := &cobra.Command{
		Use:   "guard",
		Short: "Refuse a plan that touches what is not the pull request's",
		Long: `guard reads the saved plan's JSON and lets only the pull request's own resources through: every
resource it creates, changes or destroys carries the pull request's name (<app>-pr<N>) in what
names it, or is an IAM membership of one of the pull request's accounts. Anything else stops the
build and is listed on the pull request. Shared mode is refused too when the pull request changes
the schema migrations against the default branch.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return deploy.GuardPlan(cmd.Context(), d.deploy, deploy.Workspace(workspace), cmd.OutOrStdout())
		},
	}
	workspaceFlag(cmd, &workspace)

	return cmd
}

// newDeployStackApply is deploy pr-stack apply.
func newDeployStackApply(d deps) *cobra.Command {
	var workspace string
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Apply the saved plan of the pull request's stack",
		Long: `apply applies exactly the plan the guard passed. After a destroy nothing deploys (SKIP_DEPLOY is
appended to environment.sh); else the stack's substitutions output names the pull request's
services, job process's job, hostname, the migrate command's variables and the build arguments
the placement declares (_BUILD_ARG_<NAME>, the pull request's own values, which the image build
passes), which are appended for the steps after. A database recreated without being asked (resolve found the migrations the
last build applied no longer in the tree) is said on the pull request. It runs in the OpenTofu
image.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return deploy.ApplyStack(cmd.Context(), d.deploy, deploy.Workspace(workspace), cmd.OutOrStdout())
		},
	}
	workspaceFlag(cmd, &workspace)

	return cmd
}

// newDeployPlanEnvironments is deploy plan-environments.
func newDeployPlanEnvironments(d deps) *cobra.Command {
	var workspace string
	cmd := &cobra.Command{
		Use:   "plan-environments",
		Short: "Plan and test the stack for every environment on a pull request, as each one's plan identity",
		Long: `plan-environments runs, in a pull-request build, tofu init and tofu plan in the checkout's
infrastructure directory once per environment of the promotion order (_ENVIRONMENTS), against
that environment's state prefix (3-app/<app>/<env>) as its plan identity (_PLAN_IDENTITIES: a
reader, so the plan runs without the state lock and a pull-request build in tst can change no
environment). Each plan runs the tests a tag build runs before its apply: no authoritative IAM
resource in the stack, and every secret version a planned revision template pins exists and is
enabled. One comment on the pull request carries every environment's summary; a failing plan or
test stops the build and is said on the pull request. A tag build plans its own environment
later (stack plan). It runs in the OpenTofu image.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return deploy.PlanEnvironments(cmd.Context(), d.deploy, deploy.Workspace(workspace), cmd.OutOrStdout())
		},
	}
	workspaceFlag(cmd, &workspace)

	return cmd
}

// newDeployEnvStackPlan is deploy stack plan.
func newDeployEnvStackPlan(d deps) *cobra.Command {
	var workspace string
	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Plan the environment's stack in a tag build, save the plan and run the tests",
		Long: `plan runs, in a tag build, tofu init and tofu plan in the checkout's infrastructure directory
against the environment's state prefix (3-app/<app>/<env>) as the apply identity (_APPLY_IDENTITY),
leaves the plan (stack.plan) and its JSON (stack-plan.json) in the workspace, prints the summary
and each change, appends the summary (STACK_PLAN) for the record, and runs the tests: no
authoritative IAM resource in the stack, and every secret version a planned revision template pins
exists and is enabled, read as the apply identity. A failing plan or test stops the build with the
stack unapplied. In a restore run (RESTORE=empty) the plan replaces the Spanner database and,
in tst, the file stores' buckets, each when the stack has it (tofu state list), so the migrations apply
afresh (and the seed, where the placement's seed list names the environment); what it replaces is
appended (RESTORE_REPLACED) for the record. The Firestore database is not replaced, since Firestore
keeps a deleted database's id unavailable for minutes: the apply step deletes its documents. In a
restore from production's backup (RESTORE=production-backup) the step first drops the environment's
database and restores it under its own name from the most recent backup of production's database on
the instance they share, as the apply identity; the plan then recreates the memberships the drop took
with it, and the backup is appended (RESTORE_BACKUP, RESTORE_BACKUP_TIME) for the record. While the
application is in maintenance (after deploy maintenance on) the plan carries the maintenance
variable's live value (-var maintenance=1), so that declared and live agree and the apply leaves
the service alone. It runs in the OpenTofu image.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return deploy.PlanEnvironmentStack(cmd.Context(), d.deploy, deploy.Workspace(workspace), cmd.OutOrStdout())
		},
	}
	workspaceFlag(cmd, &workspace)

	return cmd
}

// newDeployEnvStackApply is deploy stack apply.
func newDeployEnvStackApply(d deps) *cobra.Command {
	var workspace string
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Apply the saved plan of the environment's stack in a tag build",
		Long: `apply applies, in a tag build, exactly the plan stack plan saved, as the apply identity, and says
what it did; a plan with no change applies nothing. It then reads back, from the stack's
substitutions output as the stack now stands, the migrate command's settings and databases
(MIGRATE_ENV, MIGRATE_DATABASES) and the environment's hostname (CANONICAL_HOSTNAME, which deploy
service names the next revision's URL by), so a release that changes them deploys with its own,
where the trigger carries the last apply's. In a restore run it then deletes every document
of the environment's Firestore database (the stack's firestore_database output), as the apply
identity, since they refer to rows the restore replaced; what it cleared is appended
(RESTORE_CLEARED) for the record. It runs in the OpenTofu image.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return deploy.ApplyEnvironmentStack(cmd.Context(), d.deploy, deploy.Workspace(workspace), cmd.OutOrStdout())
		},
	}
	workspaceFlag(cmd, &workspace)

	return cmd
}

// hooksProgram is where the image build leaves the hooks program for the hook steps: the
// home directory every step of a build shares.
const hooksProgram = "/builder/home/hooks"

// newDeployHook is deploy hook.
func newDeployHook(d deps) *cobra.Command {
	var (
		workspace, programPath string
		program                bool
	)
	stages := make([]string, 0, len(hook.Stages))
	for _, s := range hook.Stages {
		stages = append(stages, string(s))
	}
	cmd := &cobra.Command{
		Use:       "hook <stage>",
		Short:     "Run the application's hook for a stage",
		ValidArgs: stages,
		Long: `hook runs the application's hook for the stage in the checkout as the build's deploy identity, with
every fact of environment.sh and every substitution it exports in its environment: the script
infrastructure/hooks/<stage>.sh, or with --program the application's hooks program
(cmd/deployment/hooks, built on impulse's deployhook package), which build-image took out of the
image. The stages, in the pipeline's order: after-down (only on a teardown), before-build,
before-migrate, after-migrate, before-traffic and after-traffic; the program takes the four after
the image build, a script any. A hook before the build may add build arguments by appending
NAME=value lines to the file BUILD_ARGS_FILE names. No script, nothing runs; a failing hook stops
the build at its stage. The pipeline has a step for each stage the application implements.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := ""
			if program {
				path = programPath
			}

			return deploy.Hook(cmd.Context(), d.deploy, deploy.Workspace(workspace), hook.Stage(args[0]), path, cmd.OutOrStdout())
		},
	}
	workspaceFlag(cmd, &workspace)
	cmd.Flags().BoolVar(&program, "program", false, "run the hooks program rather than the stage's script")
	cmd.Flags().StringVar(&programPath, "program-path", hooksProgram, "where build-image left the hooks program")

	return cmd
}

// newDeployBuildImage is deploy build-image.
func newDeployBuildImage(d deps) *cobra.Command {
	var (
		workspace, secretDir, migratePath, hooksPath string
		hooks                                        bool
	)
	cmd := &cobra.Command{
		Use:   "build-image",
		Short: "Build the image from the checkout's Dockerfile and push it",
		Long: `build-image runs docker buildx build over the checkout's Dockerfile and pushes the image under its
two tags (<release>-<env> and <commit>-<env>), unless check-release found this commit's build to
reuse. The build arguments are VERSION and COMMIT, the build arguments the placement declares
(every _BUILD_ARG_<NAME> environment.sh carries, passed as NAME=value and named in the log
without their values), the declared substitutions and what a hook before the build added
(build-args.txt, NAME=value lines). Each declared build secret
(BUILD_SECRETS, which resolve read from the checkout's pins) is read as the deploy identity by its
pinned version into --secret-dir (memory
backed, gone with the step) and passed as a BuildKit secret the Dockerfile mounts; it is never a
build argument, which the image would keep. The build runs in a BuildKit container (buildx's
docker-container driver, created for the build: the one driver that exports a cache) and pushes a
plain image. It reads a layer cache from the registry and writes its own there (cache-<commit>): this commit's, the commit the environment runs live, and in a
pull-request build the pull request's last build; layers are content-addressed, so the cache changes
nothing in what the build produces. A build argument is part of a layer's key, and a dependency
stage that sees one of the placement's (through the stage its FROM names or one it copies from) is
built with it and caches under a digest of its values (cache-<commit>-go-<digest>), so each
environment reads and writes its own. The digest the push answered is appended to
environment.sh (IMAGE_DIGEST). The migrate command (/migrate in the image) is copied out of the
image, built or reused, into the home directory the steps share, for the migration steps after it,
which run it on the worker; with --hooks, the application's hooks program (/hooks) is copied out the
same way for the hook steps. It runs in the docker builder image, whose docker it drives.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			path := ""
			if hooks {
				path = hooksPath
			}

			return deploy.BuildImage(cmd.Context(), d.deploy, deploy.Workspace(workspace), secretDir, migratePath, path, cmd.OutOrStdout())
		},
	}
	workspaceFlag(cmd, &workspace)
	cmd.Flags().StringVar(&secretDir, "secret-dir", "/dev/shm", "where the build secrets are written for docker, memory-backed")
	cmd.Flags().StringVar(&migratePath, "migrate-path", migrateProgram, "where the migrate command is left for the migration steps")
	cmd.Flags().BoolVar(&hooks, "hooks", false, "take the hooks program out of the image for the hook steps")
	cmd.Flags().StringVar(&hooksPath, "hooks-path", hooksProgram, "where the hooks program is left")

	return cmd
}

// newDeployTalkBack is deploy talk-back.
func newDeployTalkBack(d deps) *cobra.Command {
	var workspace string
	cmd := &cobra.Command{
		Use:   "talk-back",
		Short: "Say on the pull request what the build did",
		Long: `talk-back speaks on the pull request as the deployer GitHub App (2-env's App ID and pinned key
version, which the stack passes as _DEPLOYER_APP_ID and _DEPLOYER_KEY_SECRET): a GitHub deployment
named <app>-pr<N> carrying the environment's URL, which the pull request's sidebar shows, and a
comment with the release and the database mode. A teardown marks every deployment of the
environment inactive instead. The app's installation token is minted when there is something to
say, from its private key read by the pinned version, and is kept nowhere. A tag build, or an
environment without a deployer app yet, says nothing.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return deploy.TalkBack(cmd.Context(), d.deploy, deploy.Workspace(workspace), cmd.OutOrStdout())
		},
	}
	workspaceFlag(cmd, &workspace)

	return cmd
}

// newDeploySweepJobs is deploy sweep-jobs.
func newDeploySweepJobs(d deps) *cobra.Command {
	var workspace string
	cmd := &cobra.Command{
		Use:   "sweep-jobs",
		Short: "Delete the builds' jobs of the job process that nothing runs any more",
		Long: `sweep-jobs deletes the builds' jobs nothing runs any more, for an application with a job process: a
job of the job process (a copy of the template job, named after it) whose version key no revision of
the service in any region carries, since a revision that exists can take a traffic rollback and then
starts the job of its own build. A job with an execution still running stays, and so does one made
in the last three hours, since its build may still be running; the template stays always. Nothing
retires a revision: that is Cloud Run's own ceiling of revisions per service. The step runs after
traffic moved; an application without a job process has no step for it. A pull request's jobs go
with its environment (/gcbrun down, the hourly sweep).`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return deploy.SweepJobs(cmd.Context(), d.deploy, deploy.Workspace(workspace), cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", "/workspace", "the directory the build's steps share")

	return cmd
}

// newDeploySweep is deploy sweep.
func newDeploySweep(d deps) *cobra.Command {
	var workspace string
	cmd := &cobra.Command{
		Use:   "sweep",
		Short: "Destroy the environments of closed pull requests",
		Long: `sweep is the hourly sweep's one step (cloudbuild-sweep.yaml). It reads its build through the Cloud
Build API (BUILD_ID, PROJECT_ID and LOCATION, which the step passes), lists the pull requests
whose services stand (the pull_request label on the application's services, in every region),
asks GitHub which of them are closed with the repository's token minted from the Cloud Build
connection, and destroys each closed one's stack as the apply identity from its own state prefix,
the way /gcbrun down does. Closing or merging a pull request starts no build, so this is how an
environment nobody took down goes away. It runs in the OpenTofu image.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			req := &deploy.SweepRequest{BuildID: os.Getenv("BUILD_ID"), Project: os.Getenv("PROJECT_ID"), Location: os.Getenv("LOCATION")}

			return deploy.Sweep(cmd.Context(), d.deploy, deploy.Workspace(workspace), req, cmd.OutOrStdout())
		},
	}
	workspaceFlag(cmd, &workspace)

	return cmd
}

// newDeployMaintenance is deploy maintenance on|off.
func newDeployMaintenance(d deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "maintenance",
		Short: "Put the application into maintenance before its database is replaced, and take it out after",
		Long: `maintenance holds the steps around a run that replaces or interrupts the application's database:
on, at two places, after the image build and before the environment's stack is applied (a restore
run, whose database the plan replaces) and after the wait for the maintenance window (a breaking
release, with --window); and off, after traffic moved to the release's revision. Any other run keeps
the application serving, and the steps do nothing.`,
	}
	var (
		onWorkspace, offWorkspace string
		window                    bool
	)
	on := &cobra.Command{
		Use:   "on",
		Short: "Start the release's image as a maintenance revision and move all traffic to it",
		Long: `on puts the application into maintenance when the run needs it: before the stack, a restore run
(RESTORE is set); with --window, after the wait for the maintenance window, a breaking release
(WINDOW_BREAKING is set, from validate-release) that is not in maintenance already, after a second
look at the window, which must still be open, or the environment in maintenance from an earlier
run, else the step stops the run with nothing changed and names the next opening. The release's own
image starts as a revision with ` + derive.MaintenanceVariable + `=1 in every region, under the tag next and
with no traffic; the revision is probed through the load balancer's next hostname and must answer
503 with the marker header X-Maintenance: 1, else the step stops the run with nothing moved and
names the application's missing switch (impulse check maintenance-switch); then all traffic moves
to it, the application's task queue (_TASKS_QUEUE) is paused and, on a restore, purged, the running
executions of the serving build's job are canceled, and the old revision's requests in flight are
let finish: its active instances are read from Cloud Monitoring until none is, or until the
service's request timeout has passed since traffic moved. The facts it appends (MAINTENANCE,
MAINTENANCE_REVISIONS, MAINTENANCE_QUEUE, MAINTENANCE_PURGED, MAINTENANCE_CANCELED,
MAINTENANCE_WAITED) reach the record. An ordinary release inside a window (releases all) takes no
maintenance revision and deploys the rolling way; a pull-request build never goes into maintenance.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return deploy.MaintenanceOn(cmd.Context(), d.deploy, deploy.Workspace(onWorkspace), window, cmd.OutOrStdout())
		},
	}
	on.Flags().StringVar(&onWorkspace, "workspace", "/workspace", "the directory the build's steps share")
	on.Flags().BoolVar(&window, "window", false, "the position after the wait for the maintenance window: a breaking release goes into maintenance here")
	off := &cobra.Command{
		Use:   "off",
		Short: "Resume the task queue once the release's revision serves",
		Long: `off takes the application out of maintenance after traffic moved to the release's revision: the
task queue paused by maintenance on is resumed, against the new release. The maintenance revisions
stay, with no traffic, as any old revision does. A run that was not in maintenance has nothing to
end, except a queue an earlier run's maintenance left paused (a restore run that failed after
maintenance on), which it resumes: the release this run deployed serves now.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return deploy.MaintenanceOff(cmd.Context(), d.deploy, deploy.Workspace(offWorkspace), cmd.OutOrStdout())
		},
	}
	off.Flags().StringVar(&offWorkspace, "workspace", "/workspace", "the directory the build's steps share")
	cmd.AddCommand(on, off)

	return cmd
}
