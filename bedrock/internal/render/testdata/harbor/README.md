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

Same shape as `2-env`: no workspaces, one state prefix per environment
(`3-app/harbor/<env>`, the stack's slot in the organization's state bucket),
supplied at init, with a backend cache per environment:

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

- **Runtime identities**, one per process:
  `imp-<env>-gbl-harbor-app` for the site (`main.go`) with
  `roles/logging.logWriter`, `roles/cloudtrace.agent`,
  `roles/monitoring.metricWriter` on the project, `roles/spanner.databaseUser`
  on the database, and accessor on the secrets; `imp-<env>-gbl-harbor-migrate`
  for the migration job (`cmd/deployment/migrate`) with
  `roles/logging.logWriter` and `roles/spanner.databaseAdmin` on the database
  only, for DDL. The deploy identity from `2-env` gets
  `roles/iam.serviceAccountUser` on both.
- **The database** `imp-<env>-gbl-harbor-db` on the environment's instance
  (`2-env` output `spanner_instance`: tst's own, the spn instance for stg and
  prd), GoogleSQL, no schema (the migrations own it). prd: deletion and drop
  protection on, a weekly full backup (Sundays 02:00 UTC) and a daily
  incremental one (02:00 UTC), each kept 90 days.
- **Secret containers**, no versions, one per secret the code declares in
  `pkg/config/data.go`, named `imp-<env>-gbl-harbor-<name>`:

  | Variable | Field | Container |
  |---|---|---|
  | `APP_COOKIE_KEY` | `dataConfig.CookieKey` | `...-cookie-key` |
  | `APP_STAFF_OIDC_CLIENT_SECRET` | `dataConfig.StaffClientSecret` | `...-staff-oidc-client-secret` |
  | `APP_STAFF_OIDC_ADMIN_CREDENTIALS` | `dataConfig.StaffAdminCredentials` | `...-staff-oidc-admin-credentials` |

  Only the site's identity holds accessor. The migrate step constructs the
  same configuration level, but the session library reads these values only
  when someone signs in (the cookie key falls back to an ephemeral one), which
  a migration never does. The design brief's rule of thumb, accessor for every
  process that constructs the level, would grant the migrate identity too;
  this is the narrower reading, and a fork for bedrock's derivation to settle.
- **Cloud Run**: the service `imp-<env>-<region>-harbor-app` in both regions (`uc1|uw3`)
  (ingress internal and load balancer, 0 to 2 instances, CPU only during
  requests, `allUsers` invoker so the load balancer can forward) and the job
  `imp-<env>-uc1-harbor-migrate` (one task, no retries, 15-minute timeout),
  both created with a placeholder image. From the first deploy on, the image
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
  request against `master`, run only on a `/gcbrun` comment.
- `imp-tst-<primary region code>-harbor-sweep`, tst only: the sweep, run
  hourly by the Cloud Scheduler job of the same name.
  The environments chain by deployment records: a release runs in an
  environment only after the previous one in the order (tst, stg, prd) holds
  a live record of it, which the pipeline checks before it builds.

### Configuration the processes receive

By level (`pkg/config`): a process gets the levels it constructs and nothing
above them.

| Variable | Level | Value | Service | Job |
|---|---|---|---|---|
| `APP_SERVICE_NAME` | core | `harbor` / `harbor-migrate` | yes | yes |
| `GOOGLE_CLOUD_LOGGING_PROJECT` | core | the environment project | yes | yes |
| `GOOGLE_CLOUD_SPANNER_PROJECT`, `_INSTANCE_ID`, `_DATABASE_NAME` | data | the database | yes | yes |
| `APP_STAFF_OIDC_HOSTED_DOMAIN` | data | `var.staff_oidc_hosted_domain` | yes | yes |
| `APP_STAFF_OIDC_GROUP_PREFIX` | data | `var.staff_oidc_group_prefix` | yes | yes |
| `APP_STAFF_OIDC_CLIENT_ID` | data | `var.staff_oidc_client_id[env]` | yes | |
| `APP_STAFF_OIDC_REDIRECT_URL` | data | `https://<first hostname>/api/user/callback` | yes | |
| `APP_STAFF_OIDC_ADMIN_SUBJECT` | data | `var.staff_oidc_admin_subject[env]` | yes | |
| `APP_COOKIE_KEY`, `APP_STAFF_OIDC_CLIENT_SECRET`, `APP_STAFF_OIDC_ADMIN_CREDENTIALS` | data | secret, at the pinned version | yes | |

The job carries the hosted domain and group prefix because the session
library refuses to construct without them. Not set: `APP_VERSION` (the
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
granted on the environment project to 2-env's `secret_operators`.

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
  comma-separated; `_MIGRATE_JOB` as `<region>=<job>`; `_REGISTRY` as
  `<hostname>/<shr project>/<repository>`; `_RECORDS_BUCKET`;
  `_REPO_CONNECTION_NAME` and `_REPO_NAME` (placeholders until 2-env holds the
  connection); `_RELEASE_ACTORS`, the logins whose GitHub Releases the tag
  check accepts (the release app as `<slug>[bot]`); `_PREVIOUS_ENV` and
  `_PREVIOUS_RECORDS_BUCKET`, the environment before this one and its records
  bucket, empty in the first environment; `_APPLY_IDENTITY`, the identity the
  pull-request build applies a pull request's stack as; `_MIGRATIONS_DIR`, the
  schema migrations directory, which decides whether `/gcbrun shared-db` is
  allowed; `_REPO_FULL_NAME`, the repository as GitHub names it, for the sweep;
  `_HOSTNAME`, the environment's canonical hostname (a pull-request stack's
  own, which the pipeline talks back with); `_DEPLOYER_APP_ID` and
  `_DEPLOYER_KEY_SECRET`, the deployer GitHub App the pipeline talks back on a
  pull request as and the pinned secret version of its key, empty until 2-env
  holds them; `_SEED`, true where the migrate job applies the development seed
  (`schema/devseed`, as data migrations tracked apart from the schema, so a
  seeded database takes nothing twice): always on the pull-request trigger, a
  pull request's database being new; on a release build only in the
  placement's `seed` environments, none by default and never production, so a
  database holding data is seeded only where the placement says so. Output
  `substitutions` is the same map, for a build submitted by hand before the
  triggers exist.
- The services and the job are deployed with `gcloud run services update
  --image` and `gcloud run jobs update --image` then `gcloud run jobs execute
  --wait`, which leave the template's variables and secrets alone: the
  revision template is this stack's, a deploy changes the image and its labels.
- Two images per release in the one repository, `harbor:<tag>` for the site
  and `harbor-migrate:<tag>` for the job, with `APP_VERSION` baked in at build.
- `options.logging: CLOUD_LOGGING_ONLY`, required when a build runs as a
  user-specified service account.
- The deploy identity writes one object per run into `_RECORDS_BUCKET`, at
  `<app>/<env>/<release>/<build>.json`, and can never overwrite one; a
  release that runs again in an environment (a re-run activates a placement
  change with the same image) adds a record, and the newest under the
  release's prefix is its current one.

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
each region, `harbor-pr<N>-migrate`, `harbor-pr<N>-app`, `harbor-pr<N>-db`), and the
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
account), the migrate job present but never run. The pipeline refuses it when
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
stack: one apply would remove another's members.

The migration guard. The schema migrations under `schema/migrations` and the seed
migrations beside them (`devseed`) are applied once each in the order of their
indexes, so every build first checks that each directory is one sequence
(six-digit indexes, one up file per index, at most one down, contiguous from
the lowest present), and a pull-request build also checks that every migration
the branch started with is still there unchanged, and reads the sequence
together with the default branch's, so an index taken there since the branch
was cut is refused now, not found after the merge. The refusal is posted on the
pull request and names the fix: `bedrock migration renumber` moves the pull
request's own migrations, up and down together, to follow master's
highest index with no gap, keeping their order, and `go generate ./...` runs it
first through the rendered `cmd/generate/bedrock.go`, so the
generators read the migrations as the pipeline will; a committed migration is
never renumbered. A committed seed file is never modified either: a new one
follows it, and `/gcbrun reload-db` recreates the pull request's database so the
seed applies from the start. `bedrock check` applies the sequence rule locally;
the schema-protection workflow in the application's CI is the merge gate for the
unchanged rule.

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
render, so a lost step is caught rather than copied. Its steps are becoming
`bedrock deploy` commands, run from the bedrock image the placement pins
(`bedrockImage`, by digest; moved by upgrade): every step but the image build
is a command (`bedrock deploy resolve`, `validate-release`, `check-release`,
`migrate`, `service`, `shift-traffic`, `record`); the image build stays a
docker step, and a pipeline then lists the steps it wants. What an application
adds is declared in files of its own:

- **Hooks.** A script the application commits at
  `infrastructure/hooks/<stage>.sh` runs at that stage, as the deploy identity,
  in the checkout, with the pipeline's facts sourced from
  `/workspace/environment.sh` (the environment, project, image, release,
  pull-request number, database mode) and every substitution of the build
  exported; a hook before the build may add build arguments by appending
  `BUILD_ARGS+=(--build-arg NAME=value)` lines to `/workspace/build-args.sh`.
  The stages: `before-build` (files written here are the
  Dockerfile's to copy: a fetched config, a frontend version file),
  `before-migrate` (the image is built; `IMAGE_DIGEST` names it),
  `after-traffic` (a smoke test, a cache warm; a failure here stops the build
  before the deployment record, so the record gate never sees a release the
  hook refused) and `after-down` (after a pull-request environment is torn
  down, by `/gcbrun down` or the sweep). No file, nothing runs.
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
  command, the browser workspace and its bundles, the schema directory) and
  then yours: extra stages, build arguments, private assets.

Anything beyond that is a new hook point or, once the steps are `bedrock
deploy` commands, a pipeline the application composes from them; never an edit
to the rendered file.

## Hand steps

Per environment, after the first apply:

1. In the Google Cloud console, in the environment project, APIs & Services >
   Credentials > Create credentials > OAuth client ID, type Web application,
   authorized redirect URI = output `staff_oidc_redirect_url`. Put the client
   ID in `terraform.tfvars` (`staff_oidc_client_id`) and add the client secret
   as version 1 of `imp-<env>-gbl-harbor-staff-oidc-client-secret`.
2. Generate a cookie key (`openssl rand -base64 32`) and add it as version 1
   of `imp-<env>-gbl-harbor-cookie-key`.
3. For the directory read, in the Google Workspace admin console (Security >
   Access and data control > API controls > Domain-wide delegation): add the
   runtime identity's OAuth client ID (the `imp-<env>-gbl-harbor-app`
   service account's unique ID, shown in the Cloud console under IAM > Service
   Accounts) with the scope
   `https://www.googleapis.com/auth/admin.directory.group.readonly`, and put the
   administrator it impersonates (an account holding a Groups-read privilege) in
   `staff_oidc_admin_subject`. No key: the service signs for itself through
   the IAM Credentials API, which this stack grants. The container
   `imp-<env>-gbl-harbor-staff-oidc-admin-credentials` exists for a
   runtime outside Google Cloud, where a service-account key with the same
   delegation goes; on Google Cloud it stays empty.
4. Pin the versions in `secret_versions` and apply.

## Inputs

| Name | Description | Type | Default | Required |
|---|---|---|---|:---:|
| `build_secrets` | Build-time secrets per environment, NAME = pinned version; each reaches the image build as a BuildKit secret. | `map(map(string))` | `{}` | no |
| `environment` | `tst`, `stg`, or `prd`; passed as `-var` on every run. | `string` | n/a | yes |
| `hostnames` | Hostnames per environment; the first is canonical. | `map(list(string))` | the three above | no |
| `placeholder_image` | Image the services and job are created with. | `string` | `us-docker.pkg.dev/cloudrun/container/hello` | no |
| `secret_versions` | Pinned secret version per environment per variable. | `map(map(string))` | all empty | no |
| `substitutions` | Extra trigger substitutions per environment, for the hooks and the image build. | `map(map(string))` | `{}` | no |
| `staff_oidc_admin_subject` | Impersonated Workspace administrator, per environment. | `map(string)` | all empty | no |
| `staff_oidc_client_id` | OAuth client ID, per environment. | `map(string)` | all empty | no |
| `staff_oidc_group_prefix` | Prefix of the role groups. | `string` | `"staff-"` | no |
| `staff_oidc_hosted_domain` | Workspace domain logins are restricted to. | `string` | `"impulseframework.com"` | no |
| `state_bucket` | State bucket, for the upstream layers' outputs. | `string` | n/a | yes |

## Outputs

| Name | Description |
|---|---|
| `backend_service_id`, `backend_service_self_link` | The backend service, as a `projects/.../global/backendServices/...` URI and as a full self link. |
| `database` | `{ project, instance, name }`. |
| `hostnames` | For `2-net`'s host rules, certificate, and DNS. |
| `identities` | `{ app, migrate }` runtime identity emails. |
| `migrate_job` | `{ name, region }` of the migration job. |
| `net_hosts` | The `hosts` entries for `2-net`: each hostname mapped to the backend service URI. |
| `registry` | `<hostname>/<project>/<repository>`; null until `2-shr` registers harbor. |
| `secrets` | Per variable: `secret_id` and the pinned `version` (null when unpinned). |
| `services` | Per region code: `name`, `region`, `uri`. |
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
- The application apply identity's role set is a hypothesis (design brief):
  creating a second-generation trigger, and building as a user-specified
  service account from a second-generation repository, may want
  `cloudbuild.repositories.get` or similar. The first apply and first build
  read the denials.
