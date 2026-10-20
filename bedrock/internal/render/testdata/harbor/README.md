# harbor's infrastructure

The application stack of [harbor](https://github.com/impulseframework/harbor):
everything the application needs in one environment that is not the
environment itself. It lives here, in the application repository's
`infrastructure` directory, and is applied once per environment, as the
application apply identity `imp-<env>-gbl-harbor-tofu` that `2-env` created.
Its state lives at `3-app/harbor/<env>` in the organization's state bucket,
the layer's slot there.

Every resource here is derived from something harbor declares, and each one
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
approval are what the action takes, and the audit log names who asked. GitHub
lists a workflow once a push of at most twenty commits adds or changes its file
on the default branch, or once it runs, so a repository whose first push carried
more lists none of its workflows until a pull request changes the file, and the
`apply` action cannot be started; until then the stack trigger is started as the
action would start it, by the operator in a terminal (`gcloud builds triggers
run imp-<env>-<region>-harbor-stack` on the default branch, in the
environment's project), the command `bedrock org register harbor --next`
prints at the grants step. Every
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
build worker, `roles/spanner.databaseAdmin` on the database and
`roles/datastore.user` on the Firestore database in their place: the plan
shows the job and the account destroyed and the grants created, and nothing
on the service. The log bucket and its sink keep their names; the sink's
filter now names this application's builds.

The state: no workspaces, one state prefix per environment
(`3-app/harbor/<env>`, the stack's slot in the organization's state bucket),
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
  `imp-<env>-gbl-harbor-app` for the site (`main.go`) with
  `roles/logging.logWriter`, `roles/cloudtrace.agent`, `roles/telemetry.tracesWriter`,
  `roles/monitoring.metricWriter` on the project, `roles/spanner.databaseUser`
  on the database, and accessor on the secrets; `imp-<env>-gbl-harbor-jobs` for the job process
  (`cmd/jobs`) with the site's project roles, `roles/spanner.databaseUser`
  on the database and accessor on the secrets at the levels it constructs.
  The deploy identity from `2-env` gets `roles/iam.serviceAccountUser` on
  each. The migration (`cmd/deployment/migrate`) has no identity of its
  own: the pipeline takes the migrate command out of the release's image and
  runs it on the build worker as the deploy identity, and this stack grants
  that identity `roles/spanner.databaseAdmin` on the database only, for DDL
  (a member of the database's own policy, so no other database is reached;
  `spanner.tf`), `roles/monitoring.metricWriter` on the project for the Spanner
  client's metrics (`service-accounts.tf`), and `roles/datastore.user` on the Firestore
  database under the condition naming it (`firestore.tf`). It holds
  accessor on 
  no other runtime secret, which a migration never reads. The Spanner
  client writes its client-side metrics (operation and attempt latency,
  counts) to the project that owns the instance and logs a denial at every
  export without `roles/monitoring.metricWriter` there: on tst's own instance
  that is the environment project, covered by the roles above; on the shared
  instance (stg and prd) it is the spn project, where the site, the job process
  and the deploy identity hold the role too (`spanner.tf`), granted by the
  apply identity, which `2-spn` lets grant that role there and no other.
- **The database** `imp-<env>-gbl-harbor-db` on the environment's instance
  (`2-env` output `spanner_instance`: tst's own, the spn instance for stg and
  prd), GoogleSQL, no schema (the migrations own it). prd: deletion and drop
  protection on, a weekly full backup (Sundays 02:00 UTC) and a daily
  incremental one (02:00 UTC), each kept 90 days. In prd, every
  release build also starts a backup of the database as of the cut, the moment
  before its migrations run, kept fourteen days, which `bedrock restore --before`
  returns the database to, into its next generation (`imp-<env>-gbl-harbor-db-2`,
  then `-3`), as `--at` and `--backup` do from a moment or any backup: the stack
  points at the generation the deployment records name (`var.database_generation`)
  and keeps the earlier ones, drop-protected, as forensic copies, and a moment
  in an earlier generation's history is taken as a backup of that generation.
  `bedrock rollback` returns the code alone, through the rollback trigger
  `imp-<env>-<region>-harbor-rollback`, disabled for events and run by the
  operations workflow alone, and touches no database. The database keeps its
  past for the placement's `spannerRetention` (seven days unless it says
  otherwise).
- **The default file store** `imp-<env>-<location code>-harbor-files-<project number>`
  (`dataConfig.FileStore` names it to the processes that construct the
  data level, as a `gs://` URL), where the database is: regional with a regional
  instance, dual-region over the shared instance's two regions, the location code the
  region's (`uc1`) or the instance configuration's (`nam10`); uniform access, no public
  access, unversioned, soft delete seven days in prd and none elsewhere;
  prd's survives a destroy. A pull-request stack with its own database has
  its own bucket; one sharing tst's database uses tst's, its
  identities granted on it, since the rows it reads name objects there. The service's
  first revision waits for the bucket's grants. The site and the job
  process hold `roles/storage.objectUser` on it, and the project's basic
  roles keep Cloud Storage's default grants (an Owner reaches an uploaded file
  the way an Owner reaches everything, and can change what the application
  stored); nobody else: the stack sets the bucket's whole permission list
  (`google_storage_bucket_iam_policy.files`), so a grant added on the
  bucket by hand, for a day's debugging, is removed by the next release's
  apply; a grant added on the project is not. The migrate command gets neither
  the URL nor a grant.
- **The task queue** `imp-<env>-uc1-harbor-tasks-<id>` (the stack's id, below)
  (`dataConfig.TasksQueue` names it to the processes that construct the
  data level), in the primary region, `var.tasks_max_concurrent` tasks
  in flight and `var.tasks_max_attempts` attempts each. The site and the
  job process hold `roles/cloudtasks.enqueuer` on it and
  `roles/iam.serviceAccountUser` on their own account, so a task carries the
  enqueuer's OIDC token for the call back. A pull-request stack enqueues on
  tst's queue, since a deleted queue's name stays reserved for seven
  days; so the declaration reaches tst's stack first, and a pull request
  that introduces it binds only once that stack is applied (the same order a
  new secret container takes).
- **The Firestore database** `imp-<env>-gbl-harbor-fs`
  (`FirestoreSettings.DatabaseID` names it to the processes that construct
  the data level), Native mode, in the primary region, beside the
  Spanner database; prd keeps point-in-time recovery on and resists deletion.
  The site, the deploy identity, for the migrate command it runs
  on the build worker (its role migration is a policy write the running
  instances hear through this database) and the
  job process hold `roles/datastore.user` under a condition naming this
  database alone, so nothing else in the shared environment project is
  reachable. With it, what live pages need of the
  database, from the two files beside the schema migrations (`schema/firestore`):
  the 3 composite index(es) of `schema/firestore/firestore.indexes.json` and the
  fields its `fieldOverrides` settle,
  `subscriptions.expiry` and `changes.expires`, each with a time-to-live policy;
  the security rules of `schema/firestore/firestore.rules`, released to this database as
  `cloud.firestore/<database id>` from `firestore.rules`, bedrock's owned copy
  beside the `.tf` files; `roles/iam.serviceAccountTokenCreator` for the site
  on its own account, for the custom tokens it mints through the IAM
  Credentials API; and the web API key `imp-<env>-gbl-harbor-firebase-<id>`
  (the stack's id, below)
  the browser presents to sign in with one, restricted to the Identity Toolkit
  and Secure Token APIs, handed to the processes that construct the data
  level as `APP_FIREBASE_API_KEY` (`FirestoreSettings.APIKey`). Firebase
  Authentication on the environment project is `2-env`'s.
- **Secret containers**, no versions, one per secret the code declares in
  `pkg/config/data.go`, named `imp-<env>-gbl-harbor-<name>`:

  | Variable | Field | Container |
  |---|---|---|
  | `APP_COOKIE_KEY` | `dataConfig.CookieKey` | `...-cookie-key` |
  | `APP_STAFF_DIRECTORY_CLIENT_SECRET` | `dataConfig.StaffDirectoryClientSecret` | `...-staff-directory-client-secret` |
  | `APP_MEMBERS_DIRECTORY_CLIENT_SECRET` | `dataConfig.MembersDirectoryClientSecret` | `...-members-directory-client-secret` |

  The site's identity holds accessor, and so does the job process's, on
  the secrets at the levels it constructs: it runs the application's own
  code, and what that code reads the derivation cannot know. The migrate command
  constructs the same configuration level, but its work is known: the session
  library reads these values only when someone signs in (the cookie key falls
  back to an ephemeral one), which a migration never does, so the deploy
  identity that runs it holds no accessor, and the command runs without them.
- **Cloud Run**: the service `imp-<env>-<region>-harbor-app` in both regions (`uc1|uw3`)
  (ingress internal and load balancer, CPU only during requests, `allUsers`
  invoker so the load balancer can forward). It scales from zero to at most 2 instances per region in every environment, the placement's cap (`maxInstances`).
  The migration (`cmd/deployment/migrate`) is no Cloud
  Run resource: the pipeline takes the migrate command out of the release's
  image and runs it on the build worker as the deploy identity, with the
  variables `locals.tf` derives for it (`migrate_env`, which `cloud-build.tf`
  passes to the pipeline as `_MIGRATE_ENV`).
  Beside the service, the template job `imp-<env>-uc1-harbor-jobs` for the job process
  (`cmd/jobs`; its timeout, retries and resources are `var.jobs_timeout`,
  `var.jobs_retries` and `var.jobs_resources`), never run and never deployed
  to: each build copies it into a job of its own, named after it with the
  build's version (`…-jobs-v0-1-15`), on the build's image; the service carries
  the template's name as `APP_JOBS_TEMPLATE`, and the framework
  (resource/jobs) names the job of its own build from it and the version the
  image bakes in, so a revision starts the job of its own build and a traffic
  rollback starts the earlier one; the site
  holds `roles/run.jobsExecutorWithOverrides` on the template (a start passes the
  command's arguments as container overrides, which `run.invoker` does not admit), a grant the pipeline copies onto
  each build's job with the template's settings.
  Only the running service starts the job process: the pipeline never runs
  it, a hook never starts it, and a schedule calls an endpoint on the service,
  which starts it. The job process ends what it is doing on SIGTERM within
  Cloud Run's grace: an ordinary release cancels nothing, and a breaking one
  cancels the old build's running executions. A build's job lives as long as a
  revision carrying its version exists in any region (that revision can take a
  traffic rollback), and the pipeline deletes the rest after the traffic shift;
  a job with an execution running, or made in the last three hours, stays.
  Revisions are retired by Cloud Run alone (its ceiling of 1,000 per service;
  idle ones cost nothing), and bedrock keeps none of its own. Jobs are deleted
  because every build makes one and Cloud Run allows 1,000 jobs per project
  and region, shared by every application and pull-request environment. How
  far back a rollback reaches is the registry's keep count: Cloud Run keeps an
  image only while a serving revision uses it, and an old revision needs the
  shared registry's copy to start again. The service and the job are created
  with a placeholder image.
  From the first deploy on, the image
  and the labels and annotations a deploy stamps are the pipeline's
  (`ignore_changes`); identity, scaling, variables, and secret mounts stay
  this stack's. The service's port is named `h2c`: the site's main package imports the
  framework's server (`github.com/cccteam/ccc/resource/server`), which speaks HTTP/1.1 and unencrypted
  HTTP/2 on one listener, so Cloud Run talks HTTP/2 to the container and the 32 MiB
  bounds of HTTP/1 request and response bodies no longer apply; the maintenance
  revision runs the same image and speaks it too. `bedrock check` refuses a port
  that does not match the code, as any owned file.
- **Cloud Scheduler jobs**, one per scheduled route: a method the code marks
  `@schedule`, which the generated router serves under `/_scheduled` and lists
  in `pkg/router/zz_gen_release.json`. In every environment, never in a
  pull-request stack, in the primary region, named
  `imp-<env>-uc1-harbor-sched-<route>` (`scheduler.tf`):

  | Route | Schedule | Time zone |
  |---|---|---|
  | `POST /_scheduled/send-daily-digest` | `0 7 * * 1-5` | America/New_York |

  Each job calls `https://<first hostname><route>` with an OIDC token (an
  identity token Google signs) of the invoker identity
  `imp-<env>-gbl-harbor-sched`, with the same URL as the token's audience.
  The service is open to the load balancer, so Cloud Run does not check the
  call; the framework in front of the routes does, admitting a token Google
  signed for the route's URL whose email is the one this stack sets on the
  service as `APP_SCHEDULER_INVOKER`, and answering any other call 401. So the
  invoker holds no role. A pull-request stack leaves the variable unset, and
  its scheduled routes refuse every call.
- **The request log's exclusion** `imp-<env>-gbl-harbor-log-excl`
  (`logging.tf`), in every environment and never in a pull-request stack.
  Cloud Run and the load balancer each write an entry for every request the
  service answers, and the code declares per surface which requests get one;
  the exclusion holds the cloud's own entries to the same words, scoped to
  this service's entries alone (Cloud Run's by the regional services' names,
  the load balancer's by the backend's), since the project holds several
  applications. The words, from `pkg/router/zz_gen_release.json`:

  | Surface | Request log |
  |---|---|
  | `/_scheduled/send-daily-digest` | logged always |
  | `/api/` | sampled at 0.1 |
  | `/api/manifests/{id}/file` | never logged |

  A surface logged always is kept whole. On event drops the entries of the
  requests that answered below 400 and keeps every failure; sampled drops the
  same but the declared fraction of them; never drops every entry under the
  prefix; a route is matched to the end of its path and a prefix by prefix,
  as the router matches them. A surface's clause excepts the surfaces
  declared beneath it, whatever their words, since the application decides a
  request's entry by its nearest declaration: an entry under a child is the
  child's own clause's to drop, and kept whole when the child is logged
  always.
- **Load balancer backend**: a serverless NEG per region and one global
  backend service `imp-<env>-gbl-harbor-backend` over both, external managed,
  outlier detection on (5 consecutive errors in a 1-second interval eject a
  backend for 30 seconds, at most 50% ejected, enforced at 100), request
  logging at full sample rate, and Cloud Armor's policy attached where
  `terraform.tfvars` turns it on (Cloud Armor, below). The URL map in the net
  project routes this environment's hostnames to it across projects (below).
- **Cloud Build triggers** on the repository link `2-env` registered. Two run
  `cloudbuild.yaml` as the deploy identity: `imp-<env>-uc1-harbor-version` on a
  tag `^v\d+\.\d+\.\d+$` in every environment, with Cloud Build approval
  required in stg and prd (the placement's `approvals`); `imp-tst-uc1-harbor-pr` in tst only, on a pull
  request against `master` or a patch line, run only on a `/gcbrun`
  comment: the preview, which builds the pull request's image and deploys it
  into the pull request's own environment, and applies no Terraform. Their builds
  run on Cloud Build's default machine, the placement naming no
  `buildMachine` ("The pipeline's contract" has what the machine bounds).
  Two hold their steps in the trigger itself (`cloud-build.tf`) and run as the
  apply identity, tst only, so what applies Terraform for a pull request
  never runs from a file the pull request edits:
  `imp-tst-uc1-harbor-check`, on every push to a pull request
  against `master` or a patch line, runs `bedrock check`, plans and
  tests the stack for every environment as that environment's plan identity
  (the summaries go on the pull request, with the patch release and window previews)
  and applies the pull request's own stack again when it has a preview
  standing, so the preview's infrastructure follows the pushes; its result is
  the required check the merge waits for (`1-org` names it, reported by
  Google's Cloud Build GitHub App). `imp-tst-uc1-harbor-pr-stack`, on
  the same `/gcbrun` comment as the preview, plans the pull request's own
  stack, lets only the pull request's resources through, applies it (or
  destroys it, on `/gcbrun down`) and writes what it applied to the records
  bucket for the preview build ("The pipeline's contract" has the object).
  One holds its steps in the trigger and runs as the release identity
  (`2-env` makes it in tst alone):
  `imp-tst-uc1-harbor-release`, on every push to
  `master` or a patch line, runs release-please (pinned, through
  `bedrock deploy release`) as the organization's release GitHub App: it
  makes the tag and the GitHub Release of a release pull request the push
  merged, then opens or updates the release pull request from the commits
  since the last release. The step mints the app's token from its key's
  pinned version in Secret Manager (`2-env`'s output; the key never reaches
  GitHub), and the trigger exists once that version is pinned
  (`bedrock secret pin github-release-key`, in the organization's
  repository). The tag then fires the version trigger in every environment.
- `imp-tst-<primary region code>-harbor-sweep`, tst only: the sweep, run
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
  A stack first rendered with these triggers is applied in tst once, by the
  `apply` action, before the pull request that brings it can pass the check,
  since the check trigger does not exist until then.
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

| Variable | Level | Value | Service | Migrate command | Job process |
|---|---|---|---|---|---|
| `APP_SERVICE_NAME` | core | `harbor` / `harbor-migrate` / `harbor-jobs` | yes | yes | yes |
| `GOOGLE_CLOUD_LOGGING_PROJECT` | core | the environment project | yes | yes | yes |
| `APP_TRACE_SAMPLING` | core | `all` in a pull-request stack and tst, `edge` beyond | yes | yes | yes |
| `GOOGLE_CLOUD_SPANNER_PROJECT`, `_INSTANCE_ID`, `_DATABASE_NAME` | data | the database | yes | yes | yes |
| `APP_FILE_STORE` | data | the default file store, as a `gs://` URL | yes | | yes |
| `APP_TASKS_QUEUE` | data | the task queue | yes | | yes |
| `GOOGLE_CLOUD_FIRESTORE_PROJECT` | data | the environment project (the Firestore database's) | yes | yes | yes |
| `APP_FIRESTORE_DATABASE` | data | the Firestore database | yes | yes | yes |
| `APP_FIREBASE_API_KEY` | data | the Firebase web API key | yes | yes | yes |
| `APP_MEMBERS_DIRECTORY_TENANT_ID` | data | `var.members_directory_tenant_id` | yes | yes | yes |
| `APP_MEMBERS_DIRECTORY_CLIENT_ID` | data | `var.members_directory_client_id[env]` | yes | | |
| `APP_MEMBERS_DIRECTORY_REDIRECT_URL` | data | `https://<first hostname>/portal/api/user/directory/callback` | yes | | |
| `APP_STAFF_DIRECTORY_HOSTED_DOMAIN` | data | `var.staff_directory_hosted_domain` | yes | yes | yes |
| `APP_STAFF_DIRECTORY_CLIENT_ID` | data | `var.staff_directory_client_id[env]` | yes | | |
| `APP_STAFF_DIRECTORY_REDIRECT_URL` | data | `https://<first hostname>/api/user/directory/callback` | yes | | |
| `APP_STAFF_DIRECTORY_GROUP_PREFIX` | data | `var.staff_directory_group_prefix` | yes | yes | yes |
| `APP_STAFF_DIRECTORY_GROUP_LOOKUP` | data | `var.staff_directory_group_lookup` | yes | | |
| `APP_MEMBERS_ADMINISTRATOR` | data | `var.members_administrator[env]` | yes | yes | yes |
| `APP_JOBS_TEMPLATE` | site | the job process's template job, which the site's own build's job is named from (the job driver's settings, `siteConfig.Template`) | yes | | |
| `APP_SCHEDULER_INVOKER` | none, the framework reads it | the scheduler's invoker identity, unset in a pull-request stack | yes | | |
| `APP_COOKIE_KEY`, `APP_STAFF_DIRECTORY_CLIENT_SECRET`, `APP_MEMBERS_DIRECTORY_CLIENT_SECRET` | data | secret, at the pinned version | yes | | yes |

The migrate command and the job process carry the registrations' organization slots and group prefixes because the session
library validates them when it makes each registration, at construction. The migrate command carries the Firestore
database because the data level opens its live service when it is constructed, and
the release's role migration signals the running instances through it; the web
API key rides beside it, a public value. Not set: `APP_VERSION` (the
pipeline bakes it into the image, so a deploy never edits the template's
variables), `APP_DEFAULT_SESSION_TIMEOUT` (code default), `PORT` (Cloud Run
sets it), `APP_CONSOLE_DIST and APP_PORTAL_DIST` (where the image put the bundle).

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

A secret declared `secret:"generated"` in the code (none here) has a
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
newest version the same way and add none; a plan without the variable
mounts no generated secret (the first apply runs that way while a container
has no version), and the next release build does.

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
creates the container `imp-<env>-gbl-harbor-<kebab name>` (an operator adds
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

What `cloudbuild.yaml` in the harbor repository can rely on, from the trigger
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
  created them); `_JOBS_JOB` as `<region>=<job>` for the job process; `_FILE_STORES`, the file stores' buckets as
  this stack addresses them, comma-separated, which a restore run in tst
  replaces with the database; `_REGISTRY` as
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
  (`<env>=<bucket>`), which a pull-request build against a patch line reads
  as those identities to say where the patch release will be refused; `_APPLY_IDENTITY`, the identity the tag build applies this
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
  of this stack's it names (`_BUILD_ARG_FIREBASE_API_KEY` and `_BUILD_ARG_PROJECT_ID`),
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
  effect; the step waits up to three minutes); its lines are the build log's. `deploy jobs` makes each
  build's job for the job process as a copy of the template job on the
  build's image, with the template's IAM policy; `deploy sweep-jobs` deletes
  the builds' jobs nothing runs any more.
- `logging.tf` routes the log entries of this application's builds (a sink
  on the version trigger's id and, in tst, the pull-request trigger's;
  the entries stay in the project's `_Default` bucket too) into a log bucket
  of their own and grants the operations identity `roles/logging.viewAccessor`
  on that bucket's view alone, so the operations workflow's migration job
  prints the migration steps' lines in its summary and the identity reads
  neither the application's own logs nor another application's builds. In
  every environment but production.
- One image per release and environment in the one repository,
  `harbor:<release>-<env>` (its commit's tag beside it), carrying the site,
  the migrate command and the job process, with `APP_VERSION` baked in at build.
- `options.logging: CLOUD_LOGGING_ONLY`, required when a build runs as a
  user-specified service account; no `options.machineType`,
  the placement naming no `buildMachine`, so the builds run on Cloud Build's
  default machine (`E2_STANDARD_2`, 2 vCPUs). The machine is a build-level
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
  `harbor/<env>/<stack id>/releases/<release>/<build>.json` (a pull request's
  at `harbor/tst/pr<N>-<commit>/<build>.json`), and can never
  overwrite one; a release that runs again in an environment adds a record,
  and the newest under the release's prefix is its current one. A record
  carries the plan of this stack the build applied (`stack`: counts and
  changes) and the id of the stack it lies under (`stackId`; "The stack's
  id"). A restore writes the database's generation beside the records,
  `harbor/<env>/<stack id>/database/<n>.json`, and every later build reads
  the highest.
- The pull request's stack job (`deploy pr-stack`, in the pr-stack trigger, as
  the apply identity) writes one object per pull request and commit into
  `_RECORDS_BUCKET`, at `harbor/stacks/tst/pr<N>/<commit>.json`, written again
  on a new `/gcbrun` comment on the same commit: its `status` (`applied`,
  `destroyed`, or `failed` with a `reason`), the `comment` it acted on (the
  comment's id; 0 for the check trigger's refresh on a push), its `build`, and
  for an applied stack what the preview build deploys with: the services by
  region, the hostname, the job process's job, the migrate command's settings
  and databases, the build arguments by name, and the database mode (shared,
  or recreated and why). The apply identity alone writes under
  `harbor/stacks/` (2-env's records policy); the deploy identity reads. The
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
covers exactly that): `harbor-tst.impulseframework.dev`,
`harbor-stg.impulseframework.dev`, `harbor.impulseframework.dev`. The first is
canonical and forms the directory registrations' redirect URLs. `2-net` routes a
hostname from an entry in its `hosts` variable, hostname to backend service
URI, in its `terraform.tfvars`; this stack's `net_hosts` output is exactly
those entries for the environment:

```
hosts = {
  "harbor-tst.impulseframework.dev" = "projects/<the tst project>/global/backendServices/imp-tst-gbl-harbor-backend"
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
policy is `imp-<env>-gbl-harbor-armor` (`cloud-armor.tf`), on the backend
service and the next revision's. Cloud Armor Standard bills each policy, each
rule and the requests the policy evaluates, at the rates on its pricing page.

Rules run in priority order and the first match decides. The rule sets
are `placement.json`'s (`cloudArmor.ruleSets`, each at a sensitivity from 1, the
rules least likely to misfire, to 4, every rule). A set that reads no body (scanner
detection) runs first, on every path. Then the bypasses: each a route whose body
is a file or a third party's rather than the application's JSON, allowed so
that no rule below reads it: the generated router's upload and stored-file
routes (`pkg/router/zz_gen_release.json`, `fileRoutes`) and the placement's
(`cloudArmor.bypasses`, a webhook under the Root hook whose sender signs its
body, a media stream). Then the other sets, each scoped to the outlets' routes
(/api and /portal/api), where the input a rule can judge arrives. Every other
request is allowed. Each rule's description names its source.

| Priority | Action | Rule | Source |
|---|---|---|---|
| 1000 | deny | scanner detection (scannerdetection-v33-stable, sensitivity 1) on every path | placement.json cloudArmor.ruleSets |
| 2000 | allow | POST /api/attach-manifest | the @upload method AttachManifest, pkg/router/zz_gen_release.json; its body is a file, not JSON |
| 2001 | allow | GET /api/manifests/{id}/file | the @file column Manifest.Key, pkg/router/zz_gen_release.json; its answer is the stored object |
| 2002 | allow | POST /hooks/registry | placement.json cloudArmor.bypasses; the registry's webhook body is signed by the registry, which the Root hook verifies |
| 2003 | allow | /streams/* | placement.json cloudArmor.bypasses; the manifest stream is bytes, not JSON |
| 3000 | deny | SQL injection (sqli-v33-stable, sensitivity 1) on /api and /portal/api | placement.json cloudArmor.ruleSets |
| 3010 | deny | SQL injection in JSON bodies (json-sqli-canary, sensitivity 1) on /api and /portal/api | placement.json cloudArmor.ruleSets |
| 3020 | deny | cross-site scripting (xss-v33-stable, sensitivity 2) on /api and /portal/api | placement.json cloudArmor.ruleSets |
| 3030 | deny | protocol attacks (protocolattack-v33-stable, sensitivity 1) on /api and /portal/api | placement.json cloudArmor.ruleSets |
| 2147483647 | allow | every other request | the default rule |

Fields the rule sets leave alone (`placement.json`, `cloudArmor.fieldExclusions`):

- `xss-v33-stable` does not read the cookie `harbor_session`, under every rule of the set: the session cookie is random bytes, which the cross-site scripting rules misread

## Pull-request environments

The same stack, applied in tst with `pull_request` set to the pull request's
number, is that pull request's environment: its own state
(`3-app/harbor/tst/pr<N>`), its own database on tst's instance and its
own runtime identities, short names throughout (`harbor-pr<N>` for the service in
each region, `harbor-pr<N>-app`, `harbor-pr<N>-db`), and the
hostname `harbor-pr<N>.impulseframework.dev`, which the wildcard backend 2-env creates once
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
creates, changes or destroys carries the pull request's name (`harbor-pr<N>` in its
name, account, service, database or parent), except an IAM membership whose
member is one of the pull request's own accounts. Anything else stops the job
and is posted on the pull request, and the preview build stops with the same
reason. The guard holds because the job's steps are the trigger's own text:
a pull request edits `cloudbuild.yaml`, never them. Before that, `bedrock check` refuses an
authoritative IAM resource (`*_iam_binding`, `*_iam_policy`) anywhere in the
stack: one apply would remove another's members. The file stores' bucket
policies (`storage.tf`) are the one exception, admitted by their addresses: a
pull-request stack makes buckets of its own, so their policies remove nobody
else's members, and the policy is what keeps a grant added by hand from
outliving the next apply.

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
named `harbor-pr<N>` carrying the environment's URL, which the pull request's
sidebar shows, a comment with the release and the database mode, and on
`/gcbrun down` the deployment marked inactive. The stack's triggers talk back
as the same app: the plans' summaries and the previews from the check
trigger, the guard's refusals and a database recreated from the stack job.

## Customizing the pipeline

The deploy sequence in `cloudbuild.yaml` is bedrock's and is rewritten on every
render, so a lost step is caught rather than copied. Its first step gets the
bedrock the placement pins (`bedrockVersion`), one of two ways. The pin is a
release: the step downloads its binary from the GitHub Release and verifies it
against the placement's checksum (`bedrockSha256`). A commit pin would be built
with `go install` and verified by Go's checksum database instead.
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
  balancer, `https://harbor-<env>-next.<domain>/`, empty in a pull-request
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
  another environment's layer would be served. This application declares
  `FIREBASE_API_KEY` (`firebaseApiKey`) and `PROJECT_ID` (`projectId`). The
  values a build argument may take:
  - `firebaseApiKey`: the key string of the Firebase web API key the stack makes for the application, which the browser presents to sign in (made when the code declares APP_FIREBASE_API_KEY).
  - `firestoreDatabase`: the id of the application's Firestore database (made when the code declares APP_FIRESTORE_DATABASE).
  - `projectId`: the environment project's id.
  - `environment`: the environment's name.
  - `hostname`: the service's canonical hostname in the environment.
- **The Dockerfile.** Seeded from the code's shape (the site and the migrate
  command, the job process, the browser workspace and its bundles, the schema
  directory) and then yours: extra stages, build arguments, private assets.
  `bedrock check` refuses a Dockerfile that builds no binary the pipeline
  runs or deploys (`/migrate`, taken out of the image for the migration
  steps; `/jobs`, the job process's).

Anything beyond that is a new hook point or a new `bedrock deploy` command,
never an edit to the rendered file.

## The stack's id

The first apply gives the stack an id, `random_id.stack` in `initialize.tf`:
four hex characters, kept in the state for the stack's life. Three names
carry it as a suffix, the Cloud Tasks queue
`imp-<env>-uc1-harbor-tasks-<id>`, the Firebase web API key
`imp-<env>-gbl-harbor-firebase-<id>` and the migrate log bucket
`imp-<env>-gbl-harbor-migrate-logs-<id>`: Google keeps each of those
names for a while after a delete (a queue's for days, a log bucket's for
seven, an API key's for thirty), so a stack destroyed and made again takes
names of its own instead of waiting for the old ones to clear. The id is also
where the stack's deployment records lie. The apply writes it to the records
bucket as `harbor/stacks/<env>/stack.json` (`{"id": "<hex>"}`), the pipeline
reads it there before it reads or writes a record, and the records sit under
`harbor/<env>/<id>/` ("The pipeline's contract"), so a new stack starts a
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
beside the stack's id (`harbor/stacks/<env>/teardown.json`: what went, when,
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
the environment back is the `apply` action above ("Applying"), which makes
the services and the triggers again with the placeholder image, then
`bedrock rerun <env> <release>` through them (the `run` action).

`bedrock destroy <env>` destroys the stack whole, the database, the file
stores and their objects, the Firestore database, the secret containers and
their versions, the identities, the queue, the logs and the key included, in
prd after an apply with `destroy=true` recording every protection
lifted. The deployment records stay in the records bucket under
`harbor/<env>/<id>/`, read by nothing; the Spanner backups the pipeline and
the schedules took stay until their retention ends; Google keeps the three
names above for a while, and a stack applied again takes an id of its own.

With `--all`, either command takes every environment in promotion order
(tst, stg, prd), then destroys every pull-request stack under
`3-app/harbor/tst/` whole, the way the sweep destroys a closed
pull request's.

## Hand steps

Per environment, after the first apply:

1. In the Microsoft Entra admin center, for the members auth's directory registration: one app
   registration per environment (Identity > Applications > App registrations >
   New registration), accounts in this organizational directory only, platform
   Web with the redirect URI = output `members_directory_redirect_url` and the
   front-channel logout URL = output `members_directory_logout_url`; then Certificates &
   secrets > New client secret. Put the application (client) ID in
   `terraform.tfvars` (`members_directory_client_id`), the directory (tenant) ID in
   `members_directory_tenant_id` (once for the
   organization; the session library validates it at construction, so the first
   release in an environment waits on it), and add the client secret as
   version 1 of `imp-<env>-gbl-harbor-members-directory-client-secret`.
   In tst, each pull request's environment signs in through the same
   registration: add its callback,
   `https://harbor-pr<N>.impulseframework.dev/portal/api/user/directory/callback`, to the
   registration's redirect URIs before signing in there, until the organization
   runs a test identity gateway.
2. In the Google Cloud console, in the environment project, for the staff auth's directory registration:
   first the project's consent screen, if the organization has not configured
   it there yet (APIs & Services > OAuth consent screen, audience **Internal**,
   so only the organization's own users sign in; one per project, shared by
   every application's client in it). Then APIs & Services > Credentials >
   Create credentials > OAuth client ID, type Web application, authorized
   redirect URI = output `staff_directory_redirect_url`. Put the client ID in
   `terraform.tfvars` (`staff_directory_client_id`), and add the client secret as version 1 of
   `imp-<env>-gbl-harbor-staff-directory-client-secret`.
   In tst, each pull request's environment signs in through the same
   client: add its callback,
   `https://harbor-pr<N>.impulseframework.dev/api/user/directory/callback`, to the client's
   authorized redirect URIs before signing in there, until the organization
   runs a test identity gateway.
   The role-groups read needs no step of its own: the sign-in reads a person's
   groups through the Cloud Identity Groups API with their own sign-in token,
   and 1-org enables that API in the environment project. The groups
   themselves are made in the Workspace Admin console, each set so that its
   members can view its member list (the infrastructure repository's README,
   "The role groups"). If the role groups
   are nested, set `staff_directory_group_lookup = "nested"` in `terraform.tfvars`.
3. Name the members auth's first administrator in `terraform.tfvars` (`members_administrator`), per
   environment: the sign-in name the directory presents (the user principal name
   on Entra, the email on Google). The migrate command of the first release in the
   environment seeds the administrator role membership under it while the auth's
   users table holds no rows, and refuses to seed while the name is empty; the
   person then signs in through the directory and holds the role.
4. Generate a cookie key (`openssl rand -base64 32`) and add it as version 1
   of `imp-<env>-gbl-harbor-cookie-key`.
5. Pin the versions in `secret_versions` and apply.

## Inputs

| Name | Description | Type | Default | Required |
|---|---|---|---|:---:|
| `build_secrets` | Build-time secrets per environment, NAME = pinned version; each reaches the image build as a BuildKit secret. | `map(map(string))` | `{}` | no |
| `cloud_armor` | Cloud Armor per environment: `"preview"`, `"enforce"` or `"off"` (the policy kept, detached); an environment left out has no policy. Off before removed. | `map(string)` | `{}` | no |
| `destroy` | Set by `bedrock destroy` alone: every protection lifted at the apply before its destroy. | `bool` | `false` | no |
| `environment` | `tst`, `stg`, or `prd`; passed as `-var` on every run. | `string` | n/a | yes |
| `generated_secret_versions` | The generated secrets' versions, derived and passed by the pipeline's plan step; never written here. | `map(string)` | `{}` | no |
| `hostnames` | Hostnames per environment; the first is canonical. | `map(list(string))` | the three above | no |
| `jobs_resources` | CPU and memory of one run of the job process. | `object({ cpu, memory })` | `1`, `512Mi` | no |
| `jobs_retries` | Retries of a failed run of the job process. | `number` | `0` | no |
| `jobs_timeout` | How long one run of the job process may take. | `string` | `"1800s"` | no |
| `placeholder_image` | Image the services and job are created with. | `string` | `us-docker.pkg.dev/cloudrun/container/hello` | no |
| `secret_versions` | Pinned secret version per environment per variable; the word `generated` for a generated secret. | `map(map(string))` | all empty | no |
| `substitutions` | Extra trigger substitutions per environment, for the hooks and the image build. | `map(map(string))` | `{}` | no |
| `tasks_max_attempts` | Attempts per task of the queue. | `number` | `5` | no |
| `tasks_max_concurrent` | Tasks of the queue in flight at once. | `number` | `10` | no |
| `teardown` | Set by `bedrock teardown` alone: the services' deletion protection lifted at the apply before its destroy. | `bool` | `false` | no |
| `members_directory_client_id` | Client ID of the members auth's directory registration, per environment. | `map(string)` | all empty | no |
| `members_directory_tenant_id` | Tenant id of the Entra tenant the members auth's directory registration admits. | `string` | `""` | no |
| `staff_directory_client_id` | Client ID of the staff auth's directory registration, per environment. | `map(string)` | all empty | no |
| `staff_directory_group_lookup` | How far the groups read of the staff auth's directory registration reaches: direct or nested. | `string` | `"direct"` | no |
| `staff_directory_group_prefix` | Prefix of the role groups of the staff auth's directory registration. | `string` | `"staff-"` | no |
| `staff_directory_hosted_domain` | Workspace domain the logins of the staff auth's directory registration are restricted to. | `string` | `"impulseframework.com"` | no |
| `members_administrator` | Sign-in name of the members auth's first administrator, per environment. | `map(string)` | all empty | no |
| `state_bucket` | State bucket, for the upstream layers' outputs. | `string` | n/a | yes |

## Outputs

| Name | Description |
|---|---|
| `file_stores` | The file stores' bucket URLs, by variable. |
| `backend_service_id`, `backend_service_self_link` | The backend service, as a `projects/.../global/backendServices/...` URI and as a full self link. |
| `database` | `{ project, instance, name }`. |
| `firestore_database` | The Firestore database's id. |
| `hostnames` | For `2-net`'s host rules, certificate, and DNS. |
| `identities` | `{ app, jobs }` runtime identity emails. |
| `jobs_job` | `{ name, region, resource }` of the job process's Cloud Run job. |
| `net_hosts` | The `hosts` entries for `2-net`: each hostname mapped to the backend service URI. |
| `registry` | `<hostname>/<project>/<repository>`; null until `2-shr` registers harbor. |
| `secrets` | Per variable: `secret_id` and the pinned `version` (null when unpinned). |
| `services` | Per region code: `name`, `region`, `uri`. |
| `stack_id` | The stack's id (four hex characters): the suffix of the names Google keeps after a delete, and the prefix of the stack's deployment records. |
| `tasks_queue` | The task queue's resource name. |
| `members_directory_redirect_url` | The redirect URI to register on the members auth's directory registration. |
| `members_directory_logout_url` | The front-channel logout URL to register on the members auth's directory registration. |
| `staff_directory_redirect_url` | The redirect URI to register on the staff auth's directory registration. |
| `substitutions` | What the triggers pass to `cloudbuild.yaml`. |
| `triggers` | `{ version, pr }` trigger IDs (`pr` null outside tst). |

## Upstream outputs assumed

| Layer | Output | Used for |
|---|---|---|
| `1-org` | (read for completeness; nothing used directly yet) | |
| `2-env` | `applications[harbor]`, `spanner_instance`, `records_bucket`, `project_id`, `prefix`, `region`, `region_code`, `secondary_region`, `secondary_region_code` | everything; the organization's infrastructure repository defines them |
| `2-shr` | `image_paths` | `_REGISTRY`: `<registry hostname>/<shr project>/<repository>` for harbor |

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
