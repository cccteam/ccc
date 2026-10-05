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
root, which the release workflow reads: `release-please-config.json`, starting
the releases at 0.1.0 (`initial-version`) with a feature on the minor below
1.0, and `.release-please-manifest.json`, at 0.0.0 until release-please moves
it with each release. `bedrock check` refuses the root without either.

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
summaries on the pull request: the plan the reviewer approves. An environment
whose stack has not had its first apply yet is skipped with "no stack yet: its
first apply is by hand", in the log and in the summary, until it has. So a change to
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

By hand, the same shape as `2-env`: no workspaces, one state prefix per
environment (`3-app/beacon/<env>`, the stack's slot in the organization's
state bucket), supplied at init, with a backend cache per environment:

```bash
cd infrastructure
export TF_DATA_DIR=.terraform.tst
tofu init -backend-config="prefix=3-app/beacon/tst"
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
  `imp-<env>-gbl-beacon-app` for the site (`main.go`) with
  `roles/logging.logWriter`, `roles/cloudtrace.agent`,
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
  before its migrations run, kept fourteen days, which `bedrock rollback`
  restores into the database's next generation (`imp-<env>-gbl-beacon-db-2`,
  then `-3`): the stack points at the generation the deployment records name
  (`var.database_generation`) and keeps the earlier ones, drop-protected, as
  forensic copies. The rollback trigger `imp-<env>-<region>-beacon-rollback`,
  disabled for events and run by the operations workflow alone, runs it. The
  database keeps its past for the placement's `spannerRetention` (seven days
  unless it says otherwise).
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
- **Cloud Build triggers** on the repository link `2-env` registered, running
  `cloudbuild.yaml` as the deploy identity: `imp-<env>-uc1-beacon-version` on a
  tag `^v\d+\.\d+\.\d+$` in every environment, with Cloud Build approval
  required in stg and prd (the placement's `approvals`); `imp-tst-uc1-beacon-pr` in tst only, on a pull
  request against `master`, run only on a `/gcbrun` comment. Their builds
  run on `E2_HIGHCPU_8` (8 vCPUs), the placement's
  `buildMachine` ("The pipeline's contract" has what the machine bounds).
- `imp-tst-<primary region code>-beacon-sweep`, tst only: the sweep, run
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
  build reading the pull request's own from its stack's `substitutions`
  output once that stack is applied. Every
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
  `<app>/<env>/<release>/<build>.json`, and can never overwrite one; a
  release that runs again in an environment adds a record, and the newest
  under the release's prefix is its current one. A record carries the plan
  of this stack the build applied (`stack`: counts and changes).

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
creates, changes or destroys carries the pull request's name (`beacon-pr<N>` in its
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
named `beacon-pr<N>` carrying the environment's URL, which the pull request's
sidebar shows, a comment with the release and the database mode, the guard's
refusals, and on `/gcbrun down` the deployment marked inactive.

## Customizing the pipeline

The deploy sequence in `cloudbuild.yaml` is bedrock's and is rewritten on every
render, so a lost step is caught rather than copied. Its first step gets the
bedrock the placement pins (`bedrockVersion`), one of two ways. The pin is a
commit: the step builds that commit of bedrock with `go install` in a Go image
pinned by digest, and Go's checksum database verifies it, so the placement
carries no checksum. A release pin would be downloaded from its GitHub Release
and verified against the placement's checksum (`bedrockSha256`) instead.
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

## Hand steps

Per environment, after the first apply:

1. Generate a cookie key (`openssl rand -base64 32`) and add it as version 1
   of `imp-<env>-gbl-beacon-cookie-key`.
2. Pin the versions in `secret_versions` and apply.

## Inputs

| Name | Description | Type | Default | Required |
|---|---|---|---|:---:|
| `build_secrets` | Build-time secrets per environment, NAME = pinned version; each reaches the image build as a BuildKit secret. | `map(map(string))` | `{}` | no |
| `cloud_armor` | Cloud Armor per environment: `"preview"`, `"enforce"` or `"off"` (the policy kept, detached); an environment left out has no policy. Off before removed. | `map(string)` | `{}` | no |
| `environment` | `tst`, `stg`, or `prd`; passed as `-var` on every run. | `string` | n/a | yes |
| `hostnames` | Hostnames per environment; the first is canonical. | `map(list(string))` | the three above | no |
| `placeholder_image` | Image the services and job are created with. | `string` | `us-docker.pkg.dev/cloudrun/container/hello` | no |
| `secret_versions` | Pinned secret version per environment per variable. | `map(map(string))` | all empty | no |
| `substitutions` | Extra trigger substitutions per environment, for the hooks and the image build. | `map(map(string))` | `{}` | no |
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
