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
Three files are seeded once and then yours: `terraform.tfvars` here, the
placement values per environment; `.gitignore` here, keeping the
per-environment backend caches and saved plans out of the repository; and the
`Dockerfile` at the root, the image build in its first shape ("Customizing the
pipeline").

## Applying

The first apply of an environment is by hand, before any release exists there;
every later apply is the pipeline's. A release's tag build plans the stack for
its environment as the apply identity after the image build and before the
migrations, runs the tests (no authoritative IAM resource other than the file
stores' bucket policies; every secret version
a revision template pins exists and is enabled), applies on a pass, and writes
the plan's summary to the build log and the deployment record, under the
release's own approval. A pull-request build plans the stack for every
environment as that environment's plan identity, a reader, and posts the
summaries on the pull request: the plan the reviewer approves. So a change to
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

By hand, the same shape as `2-env`: no workspaces, one state prefix per
environment (`3-app/harbor/<env>`, the stack's slot in the organization's
state bucket), supplied at init, with a backend cache per environment:

```bash
cd infrastructure
export TF_DATA_DIR=.terraform.tst
tofu init -backend-config="prefix=3-app/harbor/tst"
tofu plan -var environment=tst
tofu apply -var environment=tst
```

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
  `roles/logging.logWriter`, `roles/cloudtrace.agent`,
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
  database under the condition naming it (`firestore.tf`). It holds no
  accessor on a runtime secret, which a migration never reads.
- **The database** `imp-<env>-gbl-harbor-db` on the environment's instance
  (`2-env` output `spanner_instance`: tst's own, the spn instance for stg and
  prd), GoogleSQL, no schema (the migrations own it). prd: deletion and drop
  protection on, a weekly full backup (Sundays 02:00 UTC) and a daily
  incremental one (02:00 UTC), each kept 90 days.
- **The default file store** `imp-<env>-gbl-harbor-files-<project number>`
  (`dataConfig.FileStore` names it to the processes that construct the
  data level, as a `gs://` URL), in the primary region, uniform access, no public
  access, unversioned; prd's survives a destroy. The site and the job
  process hold `roles/storage.objectUser` on it, and nobody else: the stack
  sets the bucket's whole permission list (`google_storage_bucket_iam_policy.files`),
  so Cloud Storage's default grants to the project's basic roles are gone
  from it and no Owner, Editor or Viewer of the project reads an uploaded
  file through them. A grant added on the bucket by hand, for a day's
  debugging, is removed by the next release's apply; a grant added on the
  project is not. The migrate command gets
  neither the URL nor a grant.
- **The task queue** `imp-<env>-uc1-harbor-tasks`
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
  (`dataConfig.FirestoreDatabase` names it to the processes that construct
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
  Credentials API; and the web API key `imp-<env>-gbl-harbor-firebase`
  the browser presents to sign in with one, restricted to the Identity Toolkit
  and Secure Token APIs, handed to the processes that construct the data
  level as `APP_FIREBASE_API_KEY` (`dataConfig.FirebaseAPIKey`). Firebase
  Authentication on the environment project is `2-env`'s.
- **Secret containers**, no versions, one per secret the code declares in
  `pkg/config/data.go`, named `imp-<env>-gbl-harbor-<name>`:

  | Variable | Field | Container |
  |---|---|---|
  | `APP_COOKIE_KEY` | `dataConfig.CookieKey` | `...-cookie-key` |
  | `APP_STAFF_OIDC_CLIENT_SECRET` | `dataConfig.StaffClientSecret` | `...-staff-oidc-client-secret` |

  The site's identity holds accessor, and so does the job process's, on
  the secrets at the levels it constructs: it runs the application's own
  code, and what that code reads the derivation cannot know. The migrate command
  constructs the same configuration level, but its work is known: the session
  library reads these values only when someone signs in (the cookie key falls
  back to an ephemeral one), which a migration never does, so the deploy
  identity that runs it holds no accessor, and the command runs without them.
- **Cloud Run**: the service `imp-<env>-<region>-harbor-app` in both regions (`uc1|uw3`)
  (ingress internal and load balancer, 0 to 2 instances, CPU only during
  requests, `allUsers` invoker so the load balancer can forward). The migration (`cmd/deployment/migrate`) is no Cloud
  Run resource: the pipeline takes the migrate command out of the release's
  image and runs it on the build worker as the deploy identity, with the
  variables `locals.tf` derives for it (`migrate_env`, which `cloud-build.tf`
  passes to the pipeline as `_MIGRATE_ENV`).
  Beside the service, the template job `imp-<env>-uc1-harbor-jobs` for the job process
  (`cmd/jobs`; its timeout, retries and resources are `var.jobs_timeout`,
  `var.jobs_retries` and `var.jobs_resources`), never run and never deployed
  to: each build copies it into a job of its own, named after it with the
  build's version (`…-jobs-v0-1-15`), on the build's image, and bakes that
  job's name into the image as the site's `APP_JOBS_JOB`, so a revision starts the
  job of its own build and a traffic rollback starts the earlier one; the site
  holds `roles/run.invoker` on the template, a grant the pipeline copies onto
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
  this stack's.
- **Load balancer backend**: a serverless NEG per region and one global
  backend service `imp-<env>-gbl-harbor-backend` over both, external managed,
  outlier detection on (5 consecutive errors in a 1-second interval eject a
  backend for 30 seconds, at most 50% ejected, enforced at 100), request
  logging at full sample rate. No Cloud Armor. The URL map in the net project
  routes this environment's hostnames to it across projects (below).
- **Cloud Build triggers** on the repository link `2-env` registered, running
  `cloudbuild.yaml` as the deploy identity: `imp-<env>-uc1-harbor-version` on a
  tag `^v\d+\.\d+\.\d+$` in every environment, with Cloud Build approval
  required in stg and prd (the placement's `approvals`); `imp-tst-uc1-harbor-pr` in tst only, on a pull
  request against `master`, run only on a `/gcbrun` comment. Their builds
  run on Cloud Build's default machine, the placement naming no
  `buildMachine` ("The pipeline's contract" has what the machine bounds).
- `imp-tst-<primary region code>-harbor-sweep`, tst only: the sweep, run
  hourly by the Cloud Scheduler job of the same name.
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
| `GOOGLE_CLOUD_SPANNER_PROJECT`, `_INSTANCE_ID`, `_DATABASE_NAME` | data | the database | yes | yes | yes |
| `APP_FILE_STORE` | data | the default file store, as a `gs://` URL | yes | | yes |
| `APP_TASKS_QUEUE` | data | the task queue | yes | | yes |
| `GOOGLE_CLOUD_FIRESTORE_PROJECT` | data | the environment project (the Firestore database's) | yes | yes | yes |
| `APP_FIRESTORE_DATABASE` | data | the Firestore database | yes | yes | yes |
| `APP_FIREBASE_API_KEY` | data | the Firebase web API key | yes | yes | yes |
| `APP_STAFF_OIDC_HOSTED_DOMAIN` | data | `var.staff_oidc_hosted_domain` | yes | yes | yes |
| `APP_STAFF_OIDC_GROUP_PREFIX` | data | `var.staff_oidc_group_prefix` | yes | yes | yes |
| `APP_STAFF_OIDC_CLIENT_ID` | data | `var.staff_oidc_client_id[env]` | yes | | |
| `APP_STAFF_OIDC_REDIRECT_URL` | data | `https://<first hostname>/api/user/callback` | yes | | |
| `APP_STAFF_OIDC_GROUP_LOOKUP` | data | `var.staff_oidc_group_lookup` | yes | | |
| `APP_JOBS_JOB` | site | the job of the build, baked into the image (Dockerfile, `ARG JOBS_JOB`) | yes | | |
| `APP_COOKIE_KEY`, `APP_STAFF_OIDC_CLIENT_SECRET` | data | secret, at the pinned version | yes | | yes |

The migrate command and the job process carry the hosted domain and group prefix because the session
library refuses to construct without them. The migrate command carries the Firestore
database because the data level opens its live service when it is constructed, and
the release's role migration signals the running instances through it; the web
API key rides beside it, a public value. Not set: `APP_VERSION` (the
pipeline bakes it into the image, so a deploy never edits the template's
variables), `APP_JOBS_JOB` (baked into the image the same way, the job of
that build), `APP_DEFAULT_SESSION_TIMEOUT` (code default), `PORT` (Cloud Run
sets it), `APP_CONSOLE_DIST and APP_PORTAL_DIST` (where the image put the bundle).

### Secret versions

`var.secret_versions` pins, per environment and per variable, the version the
environment runs. A secret with no pin has its container but is not mounted:
the process starts without the variable, which is "not yet". After an
operator adds a value (`bedrock secret add <env> <VARIABLE>`, or `gcloud
secrets versions add` as a Secret Version Adder), the pin is bumped in
`terraform.tfvars` (`bedrock secret pin`) in the same pull request as the
rotation and rolled out by the next apply and deploy. `latest` is allowed
only where the map says so, for a secret whose placement marks it as tracking;
a pinned number is the default posture.

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

### Build secrets

A secret the image build needs (a component license for the browser build)
is not a runtime secret: no process reads it, so no runtime identity may.
`var.build_secrets` declares them per environment, `NAME = version`. The stack
creates the container `imp-<env>-gbl-harbor-<kebab name>` (an operator adds
the value with `bedrock secret add <env> NAME`, and `bedrock secret pin <env>
NAME <version>` moves the pin here) and grants the deploy identity, and only
it, accessor on it. The triggers carry the pins as `_BUILD_SECRETS`
(`NAME=<version resource>`, comma-separated); the pipeline's BuildImage step
reads each as the deploy identity and passes it to the build as a BuildKit
secret, never a build argument, which would land in the image's history. The
Dockerfile mounts it in the one step that needs it:

    RUN --mount=type=secret,id=NAME,required=true \
        NAME="$(cat /run/secrets/NAME)" bun run build

A pull-request build reads tst's build secrets at tst's pins. A
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
  (`<env>=<bucket>`), which a pull-request build against a hotfix line reads
  as those identities to say where the hotfix will be refused; `_APPLY_IDENTITY`, the identity the tag build applies this
  environment's stack as, and the pull-request build a pull request's stack
  as; `_RESTORE` and `_REQUESTER`, empty on a tag's own build and set on a
  restore run (what replaces the environment's database, and who asked), the
  requester alone on a rerun (`bedrock rerun`: the release's tag build again,
  production included);
  `_MIGRATE_ACTION`, `_MIGRATE_TABLE` and `_MIGRATE_VERSION`, empty on a tag's
  own build and set by the operations workflow's migration job (`version`,
  `rerun` or `force` on the environment's migrations, with `_REQUESTER` naming
  who asked); `_MIGRATIONS_DIR`, the
  schema migrations directory, which decides whether `/gcbrun shared-db` is
  allowed; `_REPO_FULL_NAME`, the repository as GitHub names it, for the sweep;
  `_HOSTNAME`, the environment's canonical hostname (a pull-request stack's
  own, which the pipeline talks back with); `_DEPLOYER_APP_ID` and
  `_DEPLOYER_KEY_SECRET`, the deployer GitHub App the pipeline talks back on a
  pull request as and the pinned secret version of its key, empty until 2-env
  holds them; `_SEED`, true where the migrate command applies the development seed
  (`schema/devseed`, as data migrations tracked apart from the schema, so a
  seeded database takes nothing twice): always on the pull-request trigger, a
  pull request's database being new; on a release build only in the
  placement's `seed` environments, none by default and never production, so a
  database holding data is seeded only where the placement says so. Every
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
  `<app>/<env>/<release>/<build>.json`, and can never overwrite one; a
  release that runs again in an environment adds a record, and the newest
  under the release's prefix is its current one. A record carries the plan
  of this stack the build applied (`stack`: counts and changes).

## Hostnames and the net layer

`var.hostnames` gives each environment its hostnames, in `2-net`'s
convention of one label under the apps domain (its wildcard certificate
covers exactly that): `harbor-tst.impulseframework.dev`,
`harbor-stg.impulseframework.dev`, `harbor.impulseframework.dev`. The first is
canonical and forms the staff sign-in's redirect URL. `2-net` routes a
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
containers, no triggers and no backend of its own. The pull-request build
applies it as the tst apply identity before it deploys, and destroys it on
`/gcbrun down` or when the pull request closes.

Shared mode. `/gcbrun shared-db` applies the stack with `shared_database`
true: no database of its own, the app identity granted database user on
tst's database (an additive membership naming the pull request's own
account), and the pipeline runs no migration. It refuses shared mode when
the pull request changes anything under `schema/migrations` against its
base, because a migration on the shared database would change tst before
any release. A later plain `/gcbrun` switches back: the pull request's own
database is created, the membership on tst's is removed, and the migrations
run. `/gcbrun reload-db` recreates the pull request's own database and cannot
be combined with `shared-db`. The build also recreates it without being asked
when a migration the last build applied is no longer in the tree by name and
content (renumbered past an index master took, or changed before it
reached master): the database is disposable, so it is replaced, the
migrations apply afresh, and the pull request is told why. A build that failed
between its migrations and its record leaves the older record behind, so
`/gcbrun reload-db` stays the hand fix for that.

The guard. A pull request may have edited this stack any way at all, so the
pull-request build plans first and applies only when every resource the plan
creates, changes or destroys carries the pull request's name (`harbor-pr<N>` in its
name, account, service, database or parent), except an IAM membership whose
member is one of the pull request's own accounts. Anything else stops the run
and is posted on the pull request. Before that, `bedrock check` refuses an
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
the deploy identity: it lists the pull-request services by their
`pull_request` label, asks GitHub whether each pull request is closed, and
destroys the stacks of the closed ones as the apply identity.

Talk-back. When 2-env holds the deployer GitHub App (its App ID and the pinned
version of its key), the pull-request build mints an installation token from
the key and talks back on the pull request as the app: a GitHub deployment
named `harbor-pr<N>` carrying the environment's URL, which the pull request's
sidebar shows, a comment with the release and the database mode, the guard's
refusals, and on `/gcbrun down` the deployment marked inactive.

## Customizing the pipeline

The deploy sequence in `cloudbuild.yaml` is bedrock's and is rewritten on every
render, so a lost step is caught rather than copied. Its first step gets the
bedrock the placement pins (`bedrockVersion`), one of two ways. The pin is a
release: the step downloads its binary from the GitHub Release and verifies it
against the placement's checksum (`bedrockSha256`). A commit pin would be built
with `go install` and verified by Go's checksum database instead.
Every step after it is one `bedrock deploy` command run in the image whose tool
it drives: gcloud's for most, OpenTofu's for the pull request's stack, docker's
for the image build. The steps share the checkout as their workspace: each
reads the facts (`environment.sh`) and the build (`build.json`), does one thing
and appends what it learned, and none installs anything. They run in order,
but for two lanes side by side in a pull-request build: the pull request's
stack (its plan, guard and apply) and the image (the release check and the
build), joined again before the application deploys. `bedrock deploy
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
  environment, `_NAME = value`: the triggers carry them, the pipeline exports
  them to the hooks and passes them to the image build as build arguments
  (`ARG _NAME` in the Dockerfile). A name the pipeline's contract already
  carries is refused by the triggers' plan.
- **The Dockerfile.** Seeded from the code's shape (the site and the migrate
  command, the job process, the browser workspace and its bundles, the schema
  directory) and then yours: extra stages, build arguments, private assets.
  `bedrock check` refuses a Dockerfile that builds no binary the pipeline
  runs or deploys (`/migrate`, taken out of the image for the migration
  steps; `/jobs`, the job process's), or that drops the lines carrying
  the build's job to the site (`ARG JOBS_JOB`, `ENV APP_JOBS_JOB="${JOBS_JOB}"`).

Anything beyond that is a new hook point or a new `bedrock deploy` command,
never an edit to the rendered file.

## Hand steps

Per environment, after the first apply:

1. In the Google Cloud console, in the environment project, APIs & Services >
   Credentials > Create credentials > OAuth client ID, type Web application,
   authorized redirect URI = output `staff_oidc_redirect_url`. Put the client
   ID in `terraform.tfvars` (`staff_oidc_client_id`) and add the client secret
   as version 1 of `imp-<env>-gbl-harbor-staff-oidc-client-secret`.
   The role-groups read needs no step of its own: the sign-in reads a person's
   groups through the Cloud Identity Groups API with their own sign-in token,
   and 1-org enables that API in the environment project. If the role groups
   are nested, set `staff_oidc_group_lookup = "nested"` in `terraform.tfvars`.
2. Generate a cookie key (`openssl rand -base64 32`) and add it as version 1
   of `imp-<env>-gbl-harbor-cookie-key`.
3. Pin the versions in `secret_versions` and apply.

## Inputs

| Name | Description | Type | Default | Required |
|---|---|---|---|:---:|
| `build_secrets` | Build-time secrets per environment, NAME = pinned version; each reaches the image build as a BuildKit secret. | `map(map(string))` | `{}` | no |
| `environment` | `tst`, `stg`, or `prd`; passed as `-var` on every run. | `string` | n/a | yes |
| `hostnames` | Hostnames per environment; the first is canonical. | `map(list(string))` | the three above | no |
| `jobs_resources` | CPU and memory of one run of the job process. | `object({ cpu, memory })` | `1`, `512Mi` | no |
| `jobs_retries` | Retries of a failed run of the job process. | `number` | `0` | no |
| `jobs_timeout` | How long one run of the job process may take. | `string` | `"1800s"` | no |
| `placeholder_image` | Image the services and job are created with. | `string` | `us-docker.pkg.dev/cloudrun/container/hello` | no |
| `secret_versions` | Pinned secret version per environment per variable. | `map(map(string))` | all empty | no |
| `substitutions` | Extra trigger substitutions per environment, for the hooks and the image build. | `map(map(string))` | `{}` | no |
| `tasks_max_attempts` | Attempts per task of the queue. | `number` | `5` | no |
| `tasks_max_concurrent` | Tasks of the queue in flight at once. | `number` | `10` | no |
| `staff_oidc_client_id` | OAuth client ID, per environment. | `map(string)` | all empty | no |
| `staff_oidc_group_lookup` | How far the groups read reaches: direct or nested. | `string` | `"direct"` | no |
| `staff_oidc_group_prefix` | Prefix of the role groups. | `string` | `"staff-"` | no |
| `staff_oidc_hosted_domain` | Workspace domain logins are restricted to. | `string` | `"impulseframework.com"` | no |
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
| `tasks_queue` | The task queue's resource name. |
| `staff_oidc_redirect_url` | The redirect URI to register on the OAuth client. |
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
