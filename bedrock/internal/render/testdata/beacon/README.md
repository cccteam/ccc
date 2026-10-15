# beacon's infrastructure

The application stack of [beacon](https://github.com/impulseframework/beacon):
everything the application needs in one environment that is not the
environment itself. It lives here, in the application repository's
`infrastructure` directory, and is applied once per environment, as the
application apply identity `imp-<env>-gbl-beacon-tofu` that `2-env` created.
Its state lives at `3-app/beacon/<env>` in the organization's state bucket,
the layer's slot there.

Every resource here is derived from something beacon declares, and each one
says in a comment which declaration (a struct field under `pkg/config`, a
route, a command) it comes from. bedrock renders these files from the code
(`bedrock render`) and owns them: `bedrock check` compares them with the code
and fails on drift. It renders the pipeline the same way, `cloudbuild.yaml` and
`cloudbuild-sweep.yaml` at the repository root where Cloud Build reads them, and
its generate-time step, `cmd/generate/bedrock.go`, beside the
directive that runs the application's generators.
Other files are seeded once and then yours: `terraform.tfvars` here, the
placement values per environment; `.gitignore` here, keeping the
per-environment backend caches and saved plans out of the repository; the
`Dockerfile` at the root, the image build in its first shape ("Customizing the
pipeline"), with its `.dockerignore`; and release-please's two files at the
root, which the release trigger reads (`cloud-build.tf`): `release-please-config.json`, starting
the releases at 0.1.0 (`initial-version`) with a feature on the minor below
1.0 and a changelog section for every title type the CI's `title` check
accepts (`upgrade`, `infra` and `config` shown, so a merge of only those
releases a patch; `cleanup` hidden), and `.release-please-manifest.json`, at
0.0.0 until release-please moves it with each release. `bedrock check` refuses
the root without either, and a configuration whose sections lack an accepted
type.

## Applying

The first apply of an environment, before any release exists there, is the
operations workflow's `apply` action (Actions, operations, with the
environment): it runs in the GitHub Environment named after the environment,
as the environment's operations identity, and starts the stack trigger `2-env`
holds for this application there, which applies the stack from the default
branch as the apply identity (`bedrock deploy stack first`: the plan with the
environment and the generated secrets' pins derived from their containers,
nothing while a container has no version, which the first release adds).
Nobody holds a cloud role for it: repository write and the Environment's
approval are what the action takes, and the audit log names who asked. Every
later apply is the pipeline's, and a release run again (the `run` action)
applies the stack again. After a teardown (`bedrock teardown`), which
destroyed the triggers a release's build would run in, the same `apply`
action brings the stack back, and the `run` action then runs the release
the environment ran. A release's tag build plans the stack for
its environment as the apply identity after the image build and before the
migrations, runs the tests (no authoritative IAM resource other than the file
stores' bucket policies; every secret version
a revision template pins exists and is enabled; and, in every environment after
tst, a version pinned for every secret the code declares, checked
before the plan so that a refused release changes nothing; "Secret versions"
has the rule), applies on a pass, and writes
the plan's summary to the build log and the deployment record, under the
release's own approval. The stack's check trigger (`cloud-build.tf`; "What it
creates" has the triggers) plans the stack for every environment on every push
to a pull request, as that environment's plan identity, a reader, and posts the
summaries on the pull request: the plan the reviewer approves, and the check
the merge waits for. A secret an environment after tst has no pin
for is not a refusal there: the environment's summary says which, and the pin
to make, ahead of the release it would refuse. An environment
whose stack has not had its first apply yet is skipped with "no stack yet: its
first apply is the operations workflow's apply action", in the log and in the
summary, until it has. So a change to
this directory or to `terraform.tfvars` (a secret pin, a value for one
environment) is a commit with a releasable type, and promotes as a release.
An infrastructure change must be safe on the running service: add first,
remove later; a change that is not is declared breaking in release-please's
own way (`!` after the commit type, or a `BREAKING CHANGE:` footer) for the
notes, and deploys behind the maintenance page inside the environment's
maintenance window when the release declares its outlet answers its own
release alone (`OldestAnswered(generation.ThisRelease)` in the generator
program, which the pipeline reads from the release file beside the router).

A stack rendered by an older bedrock has a template migrate job and a
migrate identity, from when the pipeline ran the migrations as a Cloud Run
job. The first apply after a render with this bedrock removes them (the job,
the account, its project roles and the deploy identity's user grant on it)
and grants the deploy identity, which now runs the migrate command on the
build worker, `roles/spanner.databaseAdmin` on the database in their place: the plan
shows the job and the account destroyed and the grants created, and nothing
on the service. The log bucket and its sink keep their names; the sink's
filter now names this application's builds.

The state: no workspaces, one state prefix per environment
(`3-app/beacon/<env>`, the stack's slot in the organization's state bucket),
supplied at init, with a backend cache per environment (`TF_DATA_DIR`), which
every apply, the first included, points tofu at.

`2-env` for the same environment must be applied first (identities,
connection, repository link, records bucket, instance), `2-shr` before that
(the registry, with the deploy identity in its `pushers`), for stg and prd
`2-spn` with the apply identity in its `database_admins`, and `2-net`
afterwards with this stack's `net_hosts` output in its `hosts`. The stack uses
the environment project as its quota project (`user_project_override`), read
from `2-env`'s state.

## What it creates

- **Runtime identities**, one per deployed process:
  `imp-<env>-gbl-beacon-app` for the site (`main.go`) with
  `roles/logging.logWriter`, `roles/cloudtrace.agent`, `roles/telemetry.tracesWriter`,
  `roles/monitoring.metricWriter` on the project, `roles/spanner.databaseUser`
  on the database, and accessor on the secrets.
  The deploy identity from `2-env` gets `roles/iam.serviceAccountUser` on
  it. The migration (`cmd/deployment/migrate`) has no identity of its
  own: the pipeline takes the migrate command out of the release's image and
  runs it on the build worker as the deploy identity, and this stack grants
  that identity `roles/spanner.databaseAdmin` on the database only, for DDL
  (a member of the database's own policy, so no other database is reached;
  `spanner.tf`), `roles/monitoring.metricWriter` on the project for the Spanner
  client's metrics (`service-accounts.tf`). It holds no
  accessor on a runtime secret, which a migration never reads. The Spanner
  client writes its client-side metrics (operation and attempt latency,
  counts) to the project that owns the instance and logs a denial at every
  export without `roles/monitoring.metricWriter` there: on tst's own instance
  that is the environment project, covered by the roles above; on the shared
  instance (stg and prd) it is the spn project, where the site
  and the deploy identity hold the role too (`spanner.tf`), granted by the
  apply identity, which `2-spn` lets grant that role there and no other.
- **The database** `imp-<env>-gbl-beacon-db` on the environment's instance
  (`2-env` output `spanner_instance`: tst's own, the spn instance for stg and
  prd), GoogleSQL, no schema (the migrations own it). prd: deletion and drop
  protection on, a weekly full backup (Sundays 02:00 UTC) and a daily
  incremental one (02:00 UTC), each kept 90 days. In prd, every
  release build also starts a backup of the database as of the cut, the moment
  before its migrations run, kept fourteen days, which `bedrock restore --before`
  returns the database to, into its next generation (`imp-<env>-gbl-beacon-db-2`,
  then `-3`), as `--at` and `--backup` do from a moment or any backup: the stack
  points at the generation the deployment records name (`var.database_generation`)
  and keeps the earlier ones, drop-protected, as forensic copies, and a moment
  in an earlier generation's history is taken as a backup of that generation.
  `bedrock rollback` returns the code alone, through the rollback trigger
  `imp-<env>-<region>-beacon-rollback`, disabled for events and run by the
  operations workflow alone, and touches no database. The database keeps its
  past for the placement's `spannerRetention` (seven days unless it says
  otherwise).
- **Secret containers**, no versions, one per secret the code declares in
  `pkg/config/data.go`, named `imp-<env>-gbl-beacon-<name>`:

  | Variable | Field | Container |
  |---|---|---|
  | `APP_COOKIE_KEY` | `dataConfig.CookieKey` | `...-cookie-key` |

  The site's identity holds accessor. The migrate command
  constructs the same configuration level, but its work is known: the session
  library reads these values only when someone signs in (the cookie key falls
  back to an ephemeral one), which a migration never does, so the deploy
  identity that runs it holds no accessor, and the command runs without them.
- **Cloud Run**: the service `imp-<env>-<region>-beacon-app` in both regions (`uc1|uw3`)
  (ingress internal and load balancer, CPU only during requests, `allUsers`
  invoker so the load balancer can forward), created with a
  placeholder image. It scales from zero to Cloud Run's default maximum instances per region, the placement capping no environment (`maxInstances`).
  The migration (`cmd/deployment/migrate`) is no Cloud
  Run resource: the pipeline takes the migrate command out of the release's
  image and runs it on the build worker as the deploy identity, with the
  variables `locals.tf` derives for it (`migrate_env`, which `cloud-build.tf`
  passes to the pipeline as `_MIGRATE_ENV`).
  From the first deploy on, the image
  and the labels and annotations a deploy stamps are the pipeline's
  (`ignore_changes`); identity, scaling, variables, and secret mounts stay
  this stack's.
- **Load balancer backend**: a serverless NEG per region and one global
  backend service `imp-<env>-gbl-beacon-backend` over both, external managed,
  outlier detection on (5 consecutive errors in a 1-second interval eject a
  backend for 30 seconds, at most 50% ejected, enforced at 100), request
  logging at full sample rate, and Cloud Armor's policy attached where
  `terraform.tfvars` turns it on (Cloud Armor, below). The URL map in the net
  project routes this environment's hostnames to it across projects (below).
- **Cloud Build triggers** on the repository link `2-env` registered. Two run
  `cloudbuild.yaml` as the deploy identity: `imp-<env>-uc1-beacon-version` on a
  tag `^v\d+\.\d+\.\d+$` in every environment, with Cloud Build approval
  required in stg and prd (the placement's `approvals`); `imp-tst-uc1-beacon-pr` in tst only, on a pull
  request against `master` or a hotfix line, run only on a `/gcbrun`
  comment: the preview, which builds the pull request's image and deploys it
  into the pull request's own environment, and applies no Terraform. Their builds
  run on `E2_HIGHCPU_8` (8 vCPUs), the placement's
  `buildMachine` ("The pipeline's contract" has what the machine bounds).
  Two hold their steps in the trigger itself (`cloud-build.tf`) and run as the
  apply identity, tst only, so what applies Terraform for a pull request
  never runs from a file the pull request edits:
  `imp-tst-uc1-beacon-check`, on every push to a pull request
  against `master` or a hotfix line, runs `bedrock check`, plans and
  tests the stack for every environment as that environment's plan identity
  (the summaries go on the pull request, with the hotfix and window previews)
  and applies the pull request's own stack again when it has a preview
  standing, so the preview's infrastructure follows the pushes; its result is
  the required check the merge waits for (`1-org` names it, reported by
  Google's Cloud Build GitHub App). `imp-tst-uc1-beacon-pr-stack`, on
  the same `/gcbrun` comment as the preview, plans the pull request's own
  stack, lets only the pull request's resources through, applies it (or
  destroys it, on `/gcbrun down`) and writes what it applied to the records
  bucket for the preview build ("The pipeline's contract" has the object).
  One holds its steps in the trigger and runs as the release identity
  (`2-env` makes it in tst alone):
  `imp-tst-uc1-beacon-release`, on every push to
  `master` or a hotfix line, runs release-please (pinned, through
  `bedrock deploy release`) as the organization's release GitHub App: it
  makes the tag and the GitHub Release of a release pull request the push
  merged, then opens or updates the release pull request from the commits
  since the last release. The step mints the app's token from its key's
  pinned version in Secret Manager (`2-env`'s output; the key never reaches
  GitHub), and the trigger exists once that version is pinned
  (`bedrock secret pin github-release-key`, in the organization's
  repository). The tag then fires the version trigger in every environment.
- `imp-tst-<primary region code>-beacon-sweep`, tst only: the sweep, run
  hourly by the Cloud Scheduler job of the same name as the apply identity.
  The trigger-held steps are this file's text, so they change with the stack:
  a release renders them, and they take effect in an environment with that
  release's stack apply there, one apply behind the repository. The bedrock
  they run is never in the text: their first step reads the pin
  (`bedrockVersion`, `bedrockSha256`) from `infrastructure/placement.json` in
  the checkout when it runs, so a pull request that moves the pin runs the
  bedrock it pins under the steps the last apply set. The rule that follows
  is that a bedrock release works under the previous release's steps, which
  is why the steps are few (fetch bedrock, `bedrock check`, one command) and a
  command says in its log when the steps it runs under are an older bedrock's.
  A stack first rendered with these triggers is applied by hand in tst once
  before the pull request that brings it can pass the check, since the check
  trigger does not exist until then.
  The environments chain by deployment records: a release runs in an
  environment only after the previous one in the order (tst, stg, prd) holds
  a live record of it, which the pipeline checks before it builds. A release
  that turns away the release an environment runs (the oldest release its
  outlets still answer, read from the release file beside the generated
  router, is newer than the live one) is a breaking release: it deploys behind
  the maintenance page inside the environment's maintenance window
  (`placement.json`, `maintenance`: `"anytime"`, or the client's weekly and
  dated slots in a time zone, every environment but prd anytime
  unless written, prd refused until written), waiting inside the
  run with its image built and its stack applied; under `"releases": "all"` every
  release waits for the window and an ordinary one then deploys the rolling
  way.

### Configuration the processes receive

By level (`pkg/config`): a process gets the levels it constructs and nothing
above them.

| Variable | Level | Value | Service | Migrate command |
|---|---|---|---|---|
| `APP_SERVICE_NAME` | core | `beacon` / `beacon-migrate` | yes | yes |
| `GOOGLE_CLOUD_LOGGING_PROJECT` | core | the environment project | yes | yes |
| `GOOGLE_CLOUD_SPANNER_PROJECT`, `_INSTANCE_ID`, `_DATABASE_NAME` | data | the database | yes | yes |
| `APP_COOKIE_KEY` | data | secret, at the pinned version | yes | |

Not set: `APP_VERSION` (the
pipeline bakes it into the image, so a deploy never edits the template's
variables), `APP_DEFAULT_SESSION_TIMEOUT` (code default), `PORT` (Cloud Run
sets it), `APP_CONSOLE_DIST` (where the image put the bundle).

### Secret versions

`var.secret_versions` pins, per environment and per variable, the version the
environment runs. A secret with no pin has its container but is not mounted:
the process starts without the variable, which is "not yet". That is
tst's allowance alone, where a value goes in ahead of the release
that first reads it. In every environment after tst, a release
whose code declares a secret the environment has no pin for is refused at that
environment's plan, before anything is applied: the refusal names the
environment, every unpinned variable and the pin to make (`bedrock secret pin
<env> <VARIABLE> <version>`, after `bedrock secret add <env> <VARIABLE>` has
put the value in place), and the pull request's plan says the same in that
environment's summary, so the pin lands before the release reaches it. A value
from outside (an OAuth client secret, a third party's key) goes in through the
Secret Manager console first, as a member of the environment's team group
holding the secret operator entitlement for the time asked (`2-env`'s
`team-group.tf`), else `gcloud secrets versions add`, else `bedrock secret add
<env> <VARIABLE>`; the pin is then bumped in `terraform.tfvars` (`bedrock
secret pin`) in the same pull request as the rotation and rolled out by the
next apply and deploy. `latest` is allowed only where the map says so, for a
secret whose placement marks it as tracking; a pinned number is the default
posture. A secret the code declares `secret:"generated"` needs none of this
(next section).

A new secret's value goes in ahead of the release that first reads it: the
operator creates the container and adds the value (`bedrock secret add`
creates a container the project lacks, named as this stack names it and
labeled as it labels it), and `secret-manager.tf` adopts it: every declared
container the project holds and this state does not is imported at plan time,
so the apply reconciles it instead of failing to create it. A pull-request
stack reads tst's containers and adopts none. Creating a
container and adding a version is the `secretOperator` role 1-org defines,
which nobody holds standing: a member of the environment's team group asks
for the secret operator entitlement (2-env's `team-group.tf`) and holds the
role for the time asked.

### Generated secrets

A secret declared `secret:"generated"` in the code (`APP_COOKIE_KEY`) has a
value nobody types: 32 bytes of cryptographically secure random data, base64,
made by the tooling. Its entry in `secret_versions` is the word `generated` in
every environment (`bedrock render` seeds it; `bedrock check` refuses a
number there, and the word on a secret someone puts in place), and its
version is derived at each build the way the image digest is: the release's
plan step, as the apply identity, reads the container's newest enabled
version, adds version 1 when the container has none (creating the container
ahead of the apply, which adopts it, with the adding grant on it), and passes
the number to the plan (`var.generated_secret_versions`), so the revision
template pins `versions/N` exactly as a committed pin does, and the
deployment record names N. A pull request's plans read each environment's
newest version the same way and add none; a plan run by hand without the
variable mounts no generated secret (the first apply by hand runs that way),
and the next release build does.

`bedrock secret rotate <env> <VARIABLE>` rotates one: the operations workflow,
as the environment's operations identity, adds a new random version and runs
the live release again, so a revision pinned to the new version takes traffic
in the same action. `secret-manager.tf` grants the apply and operations
identities Secret Version Adder on the generated containers alone (adding,
never reading). A rotated cookie key signs every user of that environment
out, which is the point. `bedrock secret add` and `bedrock secret pin` refuse
a generated variable.

### Build secrets

A secret the image build needs (a component license for the browser build)
is not a runtime secret: no process reads it, so no runtime identity may.
`var.build_secrets` declares them per environment, `NAME = version`. The stack
creates the container `imp-<env>-gbl-beacon-<kebab name>` (an operator adds
the value with `bedrock secret add <env> NAME`, and `bedrock secret pin <env>
NAME <version>` moves the pin here) and grants the deploy identity, and only
it, accessor on it. The triggers carry the pins as `_BUILD_SECRETS`
(`NAME=<version resource>`, comma-separated), as this stack's last apply set
them; a build reads the pins from `terraform.tfvars` in the commit it builds
and takes only the containers from the trigger, so a release that moves a pin
builds with the new version. A name the trigger does not carry yet is one the
release declares: its container and its grant come with that release's apply,
after the image build, so the image build reads it from the next release on.
The pipeline's BuildImage step reads each as the deploy identity and passes it
to the build as a BuildKit secret, never a build argument, which would land
in the image's history. The Dockerfile mounts it in the one step that needs
it:

    RUN --mount=type=secret,id=NAME,required=true \
        NAME="$(cat /run/secrets/NAME)" bun run build

A pull-request build reads tst's build secrets at the pins its own
`terraform.tfvars` states for tst. A
secret the deploy identity must read that is not the application's own (a
hook fetching a shared configuration) is granted in 2-env
(`build_time_secrets`) instead.

## The pipeline's contract

What `cloudbuild.yaml` in the beacon repository can rely on, from the trigger
substitutions and this stack's outputs:

- `_ENV`, `_APP`, `_PROJECT`; `_SERVICES` as `<region>=<service>` per region,
  comma-separated; `_MIGRATE_ENV`, the variables the migrate command runs
  with on the build worker, as a JSON object (`locals.tf`, `migrate_env`;
  the pipeline reads it back from the `substitutions` output after it
  applies the stack, so a release that changes them migrates with its
  own); `_MIGRATE_DATABASES`, the databases the migrate command reaches,
  by resource name, as a JSON list (`locals.tf`, `migrate_databases`; read
  back the same way, and `deploy migrate` reads each as the deploy identity
  until its grants are in effect, since this very build's apply may have
  created them); `_REGISTRY` as
  `<hostname>/<shr project>/<repository>`; `_RECORDS_BUCKET`;
  `_REPO_CONNECTION_NAME` and `_REPO_NAME` (empty until 2-env holds the
  connection and the repository's link, when the triggers exist; the
  pipeline refuses a build whose connection is empty); `_RELEASE_ACTORS`, the logins whose GitHub Releases the tag
  check accepts (the release app as `<slug>[bot]`); `_PREVIOUS_ENV` and
  `_PREVIOUS_RECORDS_BUCKET`, the environment before this one and its records
  bucket, empty in the first environment; `_ENVIRONMENTS`, the promotion
  order, `_PLAN_IDENTITIES`, each environment's plan identity
  (`<env>=<email>`), which a pull-request build plans the stack for every
  environment as, and `_RECORDS_BUCKETS`, each environment's records bucket
  (`<env>=<bucket>`), which a pull-request build against a hotfix line reads
  as those identities to say where the hotfix will be refused; `_APPLY_IDENTITY`, the identity the tag build applies this
  environment's stack as (impersonated by the deploy identity), and the
  identity the stack's own triggers run as in tst (the check trigger, the
  pull-request stack trigger, the sweep), which drive tofu as themselves;
  `_RESTORE` and `_REQUESTER`, empty on a tag's own build and set on a
  restore run (what replaces the environment's database, and who asked), with
  `_RESTORE_DATABASE` and `_RESTORE_DATABASE_BACKUP` on a restore from
  production's backup (production's live database by its deployment record,
  the generation a restore put it in, and the backup it was restored from,
  which stands in while the generation has no backup of its own), `_RESTORE`
  a backup's name or `@<moment>` on a restore to a backup (any environment,
  production included: the backup is restored into the database's next
  generation and the live database kept as the forensic copy), `_ROLLBACK`,
  `_REASON` and the requester on a rollback (the release left; the earlier
  release's build again with nothing of the database), `_MAINTENANCE=off` on
  the run that only takes the application out of maintenance, the
  requester alone on a rerun (`bedrock rerun`: the release's tag build again,
  production included);
  `_MIGRATE_ACTION`, `_MIGRATE_TABLE` and `_MIGRATE_VERSION`, empty on a tag's
  own build and set by the operations workflow's migration job (`version`,
  `rerun` or `force` on the environment's migrations, with `_REQUESTER` naming
  who asked); `_MIGRATIONS_DIR`, the
  schema migrations directory, which decides whether `/gcbrun shared-db` is
  allowed; `_REPO_FULL_NAME`, the repository as GitHub names it, for the sweep;
  `_HOSTNAME`, the environment's canonical hostname (a pull-request stack's
  own, which the pipeline talks back with; a release build reads it back from
  the `substitutions` output after it applies the stack, so a release that
  changes the hostnames names its next revision's URL by its own);
  `_DEPLOYER_APP_ID` and
  `_DEPLOYER_KEY_SECRET`, the deployer GitHub App the pipeline talks back on a
  pull request as and the pinned secret version of its key, empty until 2-env
  holds them; `_SEED`, what this stack says about the development seed
  (`schema/devseed`, as data migrations tracked apart from the schema, so a
  seeded database takes nothing twice): true on the pull-request trigger, a
  pull request's database being new, and on the version trigger only in the
  placement's `seed` environments, none by default and never production, so a
  database holding data is seeded only where the placement says so. The
  pipeline keeps `_SEED` as what the trigger said and decides from the
  placement in the checkout it builds, so a release that changes the `seed`
  list seeds with its own list in that release; `_BUILD_ARG_<NAME>`, one per
  build argument `placement.json` declares (`buildArguments`), each the value
  of this stack's it names (none: the placement declares no build argument),
  which the image build passes as `--build-arg NAME=value`, a pull-request
  build reading the pull request's own from its stack job's outputs
  (`deploy pr-outputs`, below). Every
  build starts from a trigger: the triggers exist once 2-env holds the
  environment's GitHub connection and the repository's link, and the pipeline
  refuses a build whose connection or repository name is empty, so nothing is
  submitted by hand.
- The services are deployed through the Cloud Run API by `bedrock deploy
  service`, which changes the image and the labels and leaves the template's
  variables, secrets and identity alone: the revision template is this stack's.
  `deploy migrate` runs the release's migrate command, which `deploy
  build-image` took out of the image, on the build worker as the deploy
  identity, with `_MIGRATE_ENV`'s variables and the release in
  `APP_VERSION`, once the identity reads each of `_MIGRATE_DATABASES`
  (a grant this build's apply created takes IAM seconds to minutes to put in
  effect; the step waits up to three minutes); its lines are the build log's.
- `logging.tf` routes the log entries of this application's builds (a sink
  on the version trigger's id and, in tst, the pull-request trigger's;
  the entries stay in the project's `_Default` bucket too) into a log bucket
  of their own and grants the operations identity `roles/logging.viewAccessor`
  on that bucket's view alone, so the operations workflow's migration job
  prints the migration steps' lines in its summary and the identity reads
  neither the application's own logs nor another application's builds. In
  every environment but production.
- One image per release and environment in the one repository,
  `beacon:<release>-<env>` (its commit's tag beside it), carrying the site and
  the migrate command, with `APP_VERSION` baked in at build.
- `options.logging: CLOUD_LOGGING_ONLY`, required when a build runs as a
  user-specified service account, and `options.machineType: E2_HIGHCPU_8`,
  the machine the placement's `buildMachine` names. The machine is a build-level
  option: the whole run is on it, a window release's wait included, and no step
  has a machine of its own. A larger machine shortens the image step, whose
  compile and bundle build run fresh in every environment (the registry's layer
  cache holds the downloads alone), and costs more per build minute. How many
  builds the environment project runs at once in a region is its CPU quota for
  Cloud Build's default pool (`concurrent_public_pool_build_cpus`, per project
  and region) divided by the machine's vCPUs, shared by every application in
  the project: a project whose quota is 128 CPUs runs sixteen builds of 8 vCPUs
  at once, or four of 32. Cloud Build sets that quota per project and never
  raises it; only a private pool's CPU quota is adjustable. The machines a
  placement may name, with their vCPUs: `E2_MEDIUM` (1 vCPU), `E2_STANDARD_2` (2 vCPUs), `E2_HIGHCPU_8` (8 vCPUs), and `E2_HIGHCPU_32` (32 vCPUs).
- The deploy identity writes one object per run into `_RECORDS_BUCKET`, at
  `beacon/<env>/<stack id>/releases/<release>/<build>.json` (a pull request's
  at `beacon/tst/pr<N>-<commit>/<build>.json`), and can never
  overwrite one; a release that runs again in an environment adds a record,
  and the newest under the release's prefix is its current one. A record
  carries the plan of this stack the build applied (`stack`: counts and
  changes) and the id of the stack it lies under (`stackId`; "The stack's
  id"). A restore writes the database's generation beside the records,
  `beacon/<env>/<stack id>/database/<n>.json`, and every later build reads
  the highest.
- The pull request's stack job (`deploy pr-stack`, in the pr-stack trigger, as
  the apply identity) writes one object per pull request and commit into
  `_RECORDS_BUCKET`, at `beacon/stacks/tst/pr<N>/<commit>.json`, written again
  on a new `/gcbrun` comment on the same commit: its `status` (`applied`,
  `destroyed`, or `failed` with a `reason`), the `comment` it acted on (the
  comment's id; 0 for the check trigger's refresh on a push), its `build`, and
  for an applied stack what the preview build deploys with: the services by
  region, the hostname, the job process's job, the migrate command's settings
  and databases, the build arguments by name, and the database mode (shared,
  or recreated and why). The apply identity alone writes under
  `beacon/stacks/` (2-env's records policy); the deploy identity reads. The
  preview build reads the object where it first needs the stack (`deploy
  pr-outputs`: before the image build when the placement declares build
  arguments, after it otherwise), matched to its own `/gcbrun` comment, and
  waits for the job when it is not there yet: up to ten minutes, fifteen
  seconds between looks, then the build stops naming the trigger. A failed
  job stops the build at once with the job's reason; on `/gcbrun down` the
  build waits for the job's destroy instead, runs the after-down hook and
  marks the deployment inactive.

## Hostnames and the net layer

`var.hostnames` gives each environment its hostnames, in `2-net`'s
convention of one label under the apps domain (its wildcard certificate
covers exactly that): `beacon-tst.impulseframework.dev`,
`beacon-stg.impulseframework.dev`, `beacon.impulseframework.dev`. The first is
canonical. `2-net` routes a
hostname from an entry in its `hosts` variable, hostname to backend service
URI, in its `terraform.tfvars`; this stack's `net_hosts` output is exactly
those entries for the environment:

```
hosts = {
  "beacon-tst.impulseframework.dev" = "projects/<the tst project>/global/backendServices/imp-tst-gbl-beacon-backend"
}
```

DNS (the wildcard A record) and the certificate already cover the hostname;
adding the entry and applying `2-net` is the whole registration.

## Cloud Armor

None unless `terraform.tfvars` names the environment (`cloud_armor`, by
environment: `"preview"` evaluates the policy and logs what each rule would have
done, `"enforce"` applies it, `"off"` keeps the policy and detaches it from the
backend services; an environment left out has no policy, and a pull-request
stack never has one, since the environment layer serves previews from one
backend service and a policy attaches to a backend service). Turn an
environment off before removing its entry: an apply that deletes the policy
while the backend services still name it fails, because OpenTofu destroys the
policy before it updates the services and Compute refuses to delete a policy
in use; off first and removed next, each apply goes through. Turn an
environment to preview first and read its load balancer's request logs for a
while: `jsonPayload.previewSecurityPolicy` names the rule a request would have
met, and a legitimate request that meets one wants a field exclusion or a
bypass in `placement.json` before the environment is enforced
(`jsonPayload.enforcedSecurityPolicy` then names the rule that decided). The
policy is `imp-<env>-gbl-beacon-armor` (`cloud-armor.tf`), on the backend
service and the next revision's. Cloud Armor Standard bills each policy, each
rule and the requests the policy evaluates, at the rates on its pricing page.

Rules run in priority order and the first match decides. The rule sets are
bedrock's defaults, the reference deployment's five at sensitivity 1;
`placement.json` replaces the list (`cloudArmor.ruleSets`, each set at a
sensitivity from 1, the rules least likely to misfire, to 4, every rule). A set that reads no body (scanner
detection) runs first, on every path. Then the bypasses: each a route whose body
is a file or a third party's rather than the application's JSON, allowed so
that no rule below reads it: the generated router's upload and stored-file
routes (`pkg/router/zz_gen_release.json`, `fileRoutes`) and the placement's
(`cloudArmor.bypasses`, a webhook under the Root hook whose sender signs its
body, a media stream). Then the other sets, each scoped to the outlets' routes
(/api), where the input a rule can judge arrives. Every other
request is allowed. Each rule's description names its source.

| Priority | Action | Rule | Source |
|---|---|---|---|
| 1000 | deny | scanner detection (scannerdetection-v33-stable, sensitivity 1) on every path | bedrock's default rule sets |
| 3000 | deny | SQL injection (sqli-v33-stable, sensitivity 1) on /api | bedrock's default rule sets |
| 3010 | deny | SQL injection in JSON bodies (json-sqli-canary, sensitivity 1) on /api | bedrock's default rule sets |
| 3020 | deny | cross-site scripting (xss-v33-stable, sensitivity 1) on /api | bedrock's default rule sets |
| 3030 | deny | protocol attacks (protocolattack-v33-stable, sensitivity 1) on /api | bedrock's default rule sets |
| 2147483647 | allow | every other request | the default rule |

## Pull-request environments

The same stack, applied in tst with `pull_request` set to the pull request's
number, is that pull request's environment: its own state
(`3-app/beacon/tst/pr<N>`), its own database on tst's instance and its
own runtime identities, short names throughout (`beacon-pr<N>` for the service in
each region, `beacon-pr<N>-app`, `beacon-pr<N>-db`), and the
hostname `beacon-pr<N>.impulseframework.dev`, which the wildcard backend 2-env creates once
in tst serves by picking the Cloud Run service named by the hostname's
first label (2-net's `*.impulseframework.dev` host rule points at it). It reads
tst's secret containers at tst's pinned versions and creates no
containers, no triggers and no backend of its own. The stack's pr-stack
trigger applies it as the tst apply identity on the same `/gcbrun` comment
that starts the preview build, from steps the pull request cannot edit, and
destroys it on `/gcbrun down`; the sweep destroys it when the pull request
closes. The preview build reads what the job applied from the records bucket
(the services, the hostname, the job, the migrate command's settings, the
build arguments, the database mode; "The pipeline's contract" has the object)
and deploys into it. A push to the pull request applies the stack again from
the check trigger while a preview stands, so the preview's infrastructure
follows the pushes; its image does not, until the next `/gcbrun`.

Shared mode. `/gcbrun shared-db` applies the stack with `shared_database`
true: no database of its own, the app identity granted database user on
tst's database (an additive membership naming the pull request's own
account), and the pipeline runs no migration. It refuses shared mode when
the pull request changes anything under `schema/migrations` against its
base, because a migration on the shared database would change tst before
any release. A later plain `/gcbrun` switches back: the pull request's own
database is created, the membership on tst's is removed, and the migrations
run. `/gcbrun reload-db` recreates the pull request's own database and cannot
be combined with `shared-db`. The stack job also recreates it without being asked
when a migration the last build applied is no longer in the tree by name and
content (renumbered past an index master took, or changed before it
reached master): the database is disposable, so it is replaced, the
migrations apply afresh, and the pull request is told why. A build that failed
between its migrations and its record leaves the older record behind, so
`/gcbrun reload-db` stays the hand fix for that.

The guard. A pull request may have edited this stack any way at all, so the
stack job plans first and applies only when every resource the plan
creates, changes or destroys carries the pull request's name (`beacon-pr<N>` in its
name, account, service, database or parent), except an IAM membership whose
member is one of the pull request's own accounts. Anything else stops the job
and is posted on the pull request, and the preview build stops with the same
reason. The guard holds because the job's steps are the trigger's own text:
a pull request edits `cloudbuild.yaml`, never them. Before that, `bedrock check` refuses an
authoritative IAM resource (`*_iam_binding`, `*_iam_policy`) anywhere in the
stack: one apply would remove another's members. The file stores' bucket
policies (`storage.tf`) are the one exception, admitted by their addresses: a
pull-request stack makes buckets of its own, so their policies remove nobody
else's members, and the policy is what keeps the project's basic roles off
the bucket.

The migration guard. The schema migrations under `schema/migrations` and the seed
migrations beside them (`devseed`) are applied once each in the order of their
indexes, so every build first checks that each directory is one sequence
(six-digit indexes, one up file per index, at most one down, contiguous from
the lowest present), and a pull-request build also checks that every schema
migration the branch started with is still there unchanged, and reads the
sequence together with the default branch's, so an index taken there since the
branch was cut is refused now, not found after the merge. The refusal is posted
on the pull request and names the fix: `bedrock migration renumber` moves the
pull request's own migrations, up and down together, to follow
master's highest index with no gap, keeping their order, and
`go generate ./...` runs it first through the rendered
`cmd/generate/bedrock.go`, so the generators read the migrations as
the pipeline will; a committed schema migration is never renumbered. Seed files
are development data and may be edited or removed, the directory staying one
sequence (a removal renumbers the files after it, in their own sequence). A
changed seed applies from the start by recreating the database, since every
build's record lists the seed files it applied and their content: a pull
request's database is recreated on its next build, and an environment on the
placement's `seed` list is restored by the next release whose tree no longer
carries a seed file as the environment's live release applied it (edited,
renumbered or removed). That release runs as a restore run the release itself
asks for: the application serves its maintenance page while the database (and
the file stores' buckets) is replaced and the Firestore documents are deleted, the
migrations and the seed apply from the start, and the record carries the
reason. A seed file added beside the applied ones is applied as a new data
migration and recreates nothing; an environment off the list never applied the
seed and is not touched; production is never on the list. `bedrock check` applies the sequence rule
locally; the schema-protection workflow in the application's CI is the merge
gate for the unchanged rule.

The sweep. Closing or merging a pull request starts no build, so every hour
Cloud Scheduler runs the sweep trigger (`cloudbuild-sweep.yaml`) in tst as
the apply identity: it lists the pull-request services by their
`pull_request` label, asks GitHub whether each pull request is closed, and
destroys the stacks of the closed ones.

Talk-back. When 2-env holds the deployer GitHub App (its App ID and the pinned
version of its key), the pull-request build mints an installation token from
the key and talks back on the pull request as the app: a GitHub deployment
named `beacon-pr<N>` carrying the environment's URL, which the pull request's
sidebar shows, a comment with the release and the database mode, and on
`/gcbrun down` the deployment marked inactive. The stack's triggers talk back
as the same app: the plans' summaries and the previews from the check
trigger, the guard's refusals and a database recreated from the stack job.

## Customizing the pipeline

The deploy sequence in `cloudbuild.yaml` is bedrock's and is rewritten on every
render, so a lost step is caught rather than copied. Its first step gets the
bedrock the placement pins (`bedrockVersion`), one of two ways. The pin is a
commit: the step builds that commit of bedrock with `go install` in a Go image
pinned by digest, and Go's checksum database verifies it, so the placement
carries no checksum. A release pin would be downloaded from its GitHub Release
and verified against the placement's checksum (`bedrockSha256`) instead.
Every step after it is one `bedrock deploy` command run in the image whose tool
it drives: gcloud's for most, OpenTofu's for the environment's stack in a tag
build, docker's for the image build. The steps share the checkout as their
workspace: each reads the facts (`environment.sh`) and the build
(`build.json`), does one thing and appends what it learned, and none installs
anything. They run in order. A pull-request build applies no Terraform from
this file: the pull request's stack is the stack's own triggers' job
(`cloud-build.tf`), and the build reads what that job applied (`deploy
pr-outputs`) where it first needs it. `bedrock deploy
<command> --help` says what each does in full. The version appears in that one
place; `bedrock upgrade` moves the pin and re-renders. The machine the run is
on is the placement's too (`buildMachine`; "The pipeline's contract" has what it
bounds). What an application adds is declared in files of its own:

- **Hooks.** A script the application commits at
  `infrastructure/hooks/<stage>.sh` runs at that stage (`bedrock deploy hook
  <stage>`), as the deploy identity, in the checkout, with the pipeline's facts
  (the environment, project, image, release, pull-request number, database
  mode) and every substitution of the build in its environment; a hook before
  the build may add build arguments by appending `NAME=value` lines to the file
  `BUILD_ARGS_FILE` names (`/workspace/build-args.txt`). The four stages after
  the image build may instead be functions of a hooks program: a Go program at
  `cmd/deployment/hooks` built on impulse's `deployhook` package
  (`deployhook.Main(deployhook.Hooks{BeforeTraffic: checkNextRevision})`), which the
  Dockerfile builds into the image as `/hooks` and the image build takes out
  for the hook steps; a stage has a script or a function, not both. The
  pipeline has a step for each stage the application implements, so a new hook
  is a render away: `bedrock check` reports the pipeline as differing until then.
  The stages, in order: `before-build` (files written here are the
  Dockerfile's to copy: a fetched config, a frontend version file),
  `before-migrate` (the image is built; `IMAGE_DIGEST` names it),
  `after-migrate` (the schema is migrated and the service not yet deployed:
  a check against a dependency the release needs, a notice to another system;
  a hook has no database and never starts the job process, whose work on the
  data the running service starts), `before-traffic`
  (the new revision is deployed in every region and the old one still
  serves: `NEXT_URL` is the new revision's public URL through the load
  balancer, `https://beacon-<env>-next.<domain>/`, empty in a pull-request
  build, and `REVISION_URLS` the tagged run.app URL per region; a failure
  here stops the build with the old revision serving), `after-traffic` (a
  smoke test, a cache warm; a failure here stops the build before the
  deployment record, so the record gate never sees a release the hook
  refused) and `after-down` (after a pull-request environment is torn down,
  by `/gcbrun down`).
- **Build secrets.** A secret the image build needs is declared in
  `terraform.tfvars` (`build_secrets`, NAME = pinned version per environment)
  and reaches the build as a BuildKit secret the Dockerfile mounts ("Build
  secrets" above); never a build argument.
- **Declared substitutions.** `substitutions` in `terraform.tfvars`, per
  environment, `_NAME = value`: the pipeline reads them from the commit it
  builds, exports them to the hooks and passes them to the image build as
  build arguments (`ARG _NAME` in the Dockerfile), so a release that declares,
  changes or drops one builds with its own. The triggers carry them too, as
  this stack's last apply set them, and the build's log names each the commit
  changes. A name the pipeline's contract already carries is refused by the
  build before anything is built, and by the triggers' plan, and so is a name
  starting with `_BUILD_ARG_`, which the next item's substitutions use.
- **Values the stack makes.** `buildArguments` in `placement.json` maps a
  build argument's name to a value this stack makes in each environment (the
  values are listed below), such as the Firebase web API key, which exists
  only once the stack is applied, so no person writes it as a declared
  substitution. The triggers carry each as `_BUILD_ARG_<NAME>` from the
  stack's own resources (`cloud-build.tf`), and the image build passes it as
  `--build-arg NAME=value`; the Dockerfile's stage that builds with it declares `ARG NAME`
  and reads it as an environment variable, and `bedrock check` refuses a
  declared argument no stage declares. A release build passes what its
  trigger carries, as this stack's last apply set it, so an argument a release
  declares first reaches the image from the next release on, and the build's
  log says so; a pull-request build waits for the pull request's own stack and
  passes its values. A build argument is part of the layer cache key, so a
  layer that sees one is built per environment, and a dependency stage that
  sees one writes its cache under a digest of the values; a build secret never
  carries such a value, since a secret is not part of the cache key and
  another environment's layer would be served. This application declares none. The
  values a build argument may take:
  - `firebaseApiKey`: the key string of the Firebase web API key the stack makes for the application, which the browser presents to sign in (made when the code declares APP_FIREBASE_API_KEY).
  - `firestoreDatabase`: the id of the application's Firestore database (made when the code declares APP_FIRESTORE_DATABASE).
  - `projectId`: the environment project's id.
  - `environment`: the environment's name.
  - `hostname`: the service's canonical hostname in the environment.
- **The Dockerfile.** Seeded from the code's shape (the site and the migrate
  command, the browser workspace and its bundles, the schema
  directory) and then yours: extra stages, build arguments, private assets.
  `bedrock check` refuses a Dockerfile that builds no binary the pipeline
  runs or deploys (`/migrate`, taken out of the image for the migration
  steps).

Anything beyond that is a new hook point or a new `bedrock deploy` command,
never an edit to the rendered file.

## The stack's id

The first apply gives the stack an id, `random_id.stack` in `initialize.tf`:
four hex characters, kept in the state for the stack's life. Three names
carry it as a suffix, the Cloud Tasks queue
`imp-<env>-uc1-beacon-tasks-<id>`, the Firebase web API key
`imp-<env>-gbl-beacon-firebase-<id>` and the migrate log bucket
`imp-<env>-gbl-beacon-migrate-logs-<id>`: Google keeps each of those
names for a while after a delete (a queue's for days, a log bucket's for
seven, an API key's for thirty), so a stack destroyed and made again takes
names of its own instead of waiting for the old ones to clear. The id is also
where the stack's deployment records lie. The apply writes it to the records
bucket as `beacon/stacks/<env>/stack.json` (`{"id": "<hex>"}`), the pipeline
reads it there before it reads or writes a record, and the records sit under
`beacon/<env>/<id>/` ("The pipeline's contract"), so a new stack starts a
history of its own and the old stack's records stay where they were, read by
nothing. A release build whose stack says no id (the stack was applied by a
bedrock without one) is refused at the record; apply the stack once. The id
is the output `stack_id`. In prd the stack protects the database
(deletion and drop protection), the Cloud Run services, the file stores (no
force-destroy) and the Firestore database from deletion, and a destroy
refuses them as they stand; `bedrock destroy` lifts them with an apply before
its destroy ("Taking an environment down"), and the stack made in its place
has an id of its own.

## Taking an environment down

Two commands take an environment's stack down by hand, by a person at their
own computer with their own sign-in, driving tofu in this directory against
the environment's prefix the way the first apply does; neither runs from a
trigger. Both refuse while `2-net` routes a host to the environment's
backend service, since Google refuses the backend's deletion while the load
balancer uses it: the hosts come out of `2-net` first, through
`bedrock org teardown` or the application's removal from the organization's
`placement.json` in a pull request. Both ask for the environment's name typed
back (`--yes` stands in where there is no terminal), and both leave a record
beside the stack's id (`beacon/stacks/<env>/teardown.json`: what went, when,
by whom, and the release that was live).

`bedrock teardown <env>` destroys what a pipeline makes again and keeps what
holds data or a name. Goes: the Cloud Run services and the job with their
invoker grants and tags, the endpoint groups and backend services, the Cloud
Build triggers and the sweep's schedule, the scheduled routes' jobs and
identity, the Cloud Armor policy, the request-log exclusion. Stays: the
database and every generation of it, the file stores with their policies,
the secret containers with their accessors, the identities with their
grants, the Firestore database with its indexes, fields and rules, the
Firebase key, the migrate log bucket and sink, the Cloud Tasks queue with its
grants, and `random_id.stack`, so the records and the names stay the
environment's own. In prd the command applies the stack with
`teardown=true` first, recording that the services' deletion protection is
lifted; then it plans the destroy with every kept resource excluded, reads
the plan back and holds it against what it means to destroy (anything beyond
or short of it stops the run with nothing applied), and applies it. Bringing
the environment back is the apply by hand above ("Applying"), from the
checkout of the release it ran, which makes the services and the triggers
again with the placeholder image, then `bedrock rerun <env> <release>`
through them.

`bedrock destroy <env>` destroys the stack whole, the database, the file
stores and their objects, the Firestore database, the secret containers and
their versions, the identities, the queue, the logs and the key included, in
prd after an apply with `destroy=true` recording every protection
lifted. The deployment records stay in the records bucket under
`beacon/<env>/<id>/`, read by nothing; the Spanner backups the pipeline and
the schedules took stay until their retention ends; Google keeps the three
names above for a while, and a stack applied again takes an id of its own.

With `--all`, either command takes every environment in promotion order
(tst, stg, prd), then destroys every pull-request stack under
`3-app/beacon/tst/` whole, the way the sweep destroys a closed
pull request's.

## Hand steps

Per environment, after the first apply:

None: every secret the code declares is generated (the first release build in
each environment adds its first version and pins it; the first apply by hand
mounts none yet), and the sign-in needs no registration.

## Inputs

| Name | Description | Type | Default | Required |
|---|---|---|---|:---:|
| `build_secrets` | Build-time secrets per environment, NAME = pinned version; each reaches the image build as a BuildKit secret. | `map(map(string))` | `{}` | no |
| `cloud_armor` | Cloud Armor per environment: `"preview"`, `"enforce"` or `"off"` (the policy kept, detached); an environment left out has no policy. Off before removed. | `map(string)` | `{}` | no |
| `destroy` | Set by `bedrock destroy` alone: every protection lifted at the apply before its destroy. | `bool` | `false` | no |
| `environment` | `tst`, `stg`, or `prd`; passed as `-var` on every run. | `string` | n/a | yes |
| `generated_secret_versions` | The generated secrets' versions, derived and passed by the pipeline's plan step; never written here. | `map(string)` | `{}` | no |
| `hostnames` | Hostnames per environment; the first is canonical. | `map(list(string))` | the three above | no |
| `placeholder_image` | Image the services and job are created with. | `string` | `us-docker.pkg.dev/cloudrun/container/hello` | no |
| `secret_versions` | Pinned secret version per environment per variable; the word `generated` for a generated secret. | `map(map(string))` | all empty | no |
| `substitutions` | Extra trigger substitutions per environment, for the hooks and the image build. | `map(map(string))` | `{}` | no |
| `teardown` | Set by `bedrock teardown` alone: the services' deletion protection lifted at the apply before its destroy. | `bool` | `false` | no |
| `state_bucket` | State bucket, for the upstream layers' outputs. | `string` | n/a | yes |

## Outputs

| Name | Description |
|---|---|
| `backend_service_id`, `backend_service_self_link` | The backend service, as a `projects/.../global/backendServices/...` URI and as a full self link. |
| `database` | `{ project, instance, name }`. |
| `hostnames` | For `2-net`'s host rules, certificate, and DNS. |
| `identities` | `{ app }` runtime identity emails. |
| `net_hosts` | The `hosts` entries for `2-net`: each hostname mapped to the backend service URI. |
| `registry` | `<hostname>/<project>/<repository>`; null until `2-shr` registers beacon. |
| `secrets` | Per variable: `secret_id` and the pinned `version` (null when unpinned). |
| `services` | Per region code: `name`, `region`, `uri`. |
| `stack_id` | The stack's id (four hex characters): the suffix of the names Google keeps after a delete, and the prefix of the stack's deployment records. |
| `substitutions` | What the triggers pass to `cloudbuild.yaml`. |
| `triggers` | `{ version, pr }` trigger IDs (`pr` null outside tst). |

## Upstream outputs assumed

| Layer | Output | Used for |
|---|---|---|
| `1-org` | (read for completeness; nothing used directly yet) | |
| `2-env` | `applications[beacon]`, `spanner_instance`, `records_bucket`, `project_id`, `prefix`, `region`, `region_code`, `secondary_region`, `secondary_region_code` | everything; the organization's infrastructure repository defines them |
| `2-shr` | `image_paths` | `_REGISTRY`: `<registry hostname>/<shr project>/<repository>` for beacon |

`2-shr` grants `roles/artifactregistry.reader` on every repository to each
environment's Cloud Run service agent itself (`pull_environments`), so a
revision here can pull its image with nothing further.

## Open questions

- Outlier detection with serverless NEGs on an external managed backend
  service validates against the provider schema; whether the API accepts this
  exact parameter set is confirmed at the first apply.
- The application apply identity's role set is read from the denials of the
  first apply and the first builds. The first tag build's plan of an
  environment's stack was refused the serverless network endpoint groups
  (`compute.regionNetworkEndpointGroups.get`), which a pull request's stack
  never makes; `roles/compute.loadBalancerAdmin` on the environment project
  covers them and the backend services. Creating a second-generation trigger,
  and building as a user-specified service account from a second-generation
  repository, may still want `cloudbuild.repositories.get` or similar.
