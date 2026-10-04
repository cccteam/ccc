# 2-env

What every application in an environment shares: the environment's Spanner
instance (tst only), the Cloud Build connection to GitHub, the
deployment-record bucket, the team group's release approval and
entitlements, and, per application, the apply and deploy identities with
their grants and the repository link. One directory, applied once per
environment, as that environment's layer identity (`imp-<env>-gbl-tofu`
from `1-org`).

## Applying

Applied by the layers workflow (`.github/workflows/layers.yml`), once per environment in
promotion order: a pull request that changes this directory plans it for
tst, stg and prd as each environment's plan identity
(`imp-<env>-gbl-plan`) and posts the three plans; the merge applies it
for tst, then stg, then prd as `imp-<env>-gbl-tofu`, and stops at the
first failure. tst first because it holds the GitHub token secret and makes
the grants the other two connections need. Run workflow on the Actions tab,
with `2-env`, applies the three again with no change: each environment
grants the next environment's deploy identities read on its records bucket
(the release gate reads the previous environment's records) and reads those
identities from the next environment's state, which the first pass did not
have yet, so a registration applies this layer twice; `bedrock org register
<app>` prints that sequence. The first `init` writes `.terraform.lock.hcl`;
commit it. `terraform.tfvars` is committed and holds the boot project, the
bucket, the connection's two GitHub values and the deployer app's;
`environment` is never in it; the
applications are in `applications.auto.tfvars`, rendered from
`placement.json`.

No workspaces. The state of each environment lives at its own prefix,
`2-env/<env>`, in the state bucket, and a backend block cannot read a
variable, so the prefix is supplied at init, which is how the workflow runs
it too. By hand, for recovery, a member of the environment's team group asks
for two entitlements for the same short time ("The team group" below): the
layer administrator, which grants the environment layer identity's roles on
the environment project, and the layer state entitlement, which grants the
identity's slot in the state bucket and the boot project as its quota
project. The person then runs the layer as themselves, signed in with
`gcloud auth application-default login`, on the same code the workflow runs:
the Google provider and the state backend act with the person's own
credentials, and the audit log names the person, not the identity.
`TF_DATA_DIR` keeps one backend cache per environment in the same checkout,
so an init for one environment can never be paired with a plan for another:

```bash
cd 2-env
export TF_DATA_DIR=.terraform.tst
tofu init -backend-config="prefix=2-env/tst"
tofu plan -var environment=tst
tofu apply -var environment=tst
```

Then the same with `stg` and `prd`, each under its own two entitlements.

Order among the project layers: `2-shr` and `2-spn` before this layer (it
reads their outputs: the instance for stg and prd, the repository names for a
warning). After this layer, the shared layers are applied with the identities
it created, because the grants sit in projects where this layer's identity
holds nothing: each application's deploy identity is a pusher in `2-shr`
(writer on its repository), and its stg and prd apply identities are database
admins in `2-spn` (on the shared instance); both are rendered into those
layers' `applications.auto.tfvars` from `placement.json`, never copied by
hand. Then the application stacks, then `2-net` with their backends in its
rendered `hosts`. The `2-net` read here is optional: it publishes the
two load balancer principals, which are derived from `1-org` until it exists,
and a `shared_vpc_id` that is null.

## What it creates

- **tst only**: the Spanner instance `imp-tst-gbl-spanner`, multi-region
  `nam10`, 100 processing units (`var.spanner_processing_units`,
  validated at or below 200), Standard edition, no autoscaler, default backup
  schedule `NONE`. stg and prd share the instance `2-spn` creates.
- The Cloud Build (2nd generation) GitHub connection
  `imp-<env>-uc1-github` for the environment project, from the Cloud Build
  GitHub App installation on the imp-example organization and the OAuth
  token secret version (below). Plus the Cloud Build service agent's identity,
  forced into existence.
- Per application in `var.applications` (default `["harbor", "beacon"]`), a repository
  link `imp-<env>-uc1-<app>-repo` under the connection, pointing at
  `https://github.com/imp-example/<app>.git`. The application stack's
  triggers reference it. It lives here because creating one takes
  `cloudbuild.repositories.create`, a connection administrator's permission,
  and the design brief puts the repository registration with the environment
  layer.
- The deployment-record bucket `imp-<env>-gbl-records-<hex4>`, US
  multi-region, versioned, uniform access, public access prevented. Its
  permission list is set whole by this layer (`records.tf`), one binding per
  role: `roles/storage.objectCreator` for the environment's deploy
  identities, and `roles/storage.objectViewer` for those deploy identities,
  the environment's application plan identities (the hotfix preview reads
  the live record), the next environment's deploy identities (the record
  gate) and the environment's team group (a person reads a record through
  the group). Nobody else: Cloud Storage's default grants to the project's
  basic roles are gone from it, so an Owner, Editor or Viewer of the project
  or the organization reads no record through them. A grant added on the
  bucket by hand, for a day's debugging, is removed by this layer's next
  apply; a grant added on the project is not, so an Owner of the project can
  still give itself a storage role there. An organization whose state still
  holds the four member resources an earlier bedrock granted the bucket with
  applies this layer once to move to the policy: `records.tf` takes them out
  of the state without destroying them (`removed` blocks with
  `destroy = false`, carried for one bedrock release), since destroying one
  would take its member out of the permission list the policy has just set,
  until the next apply set it again.
- The team group's grants (`team-group.tf`): `roles/cloudbuild.builds.approver`
  on the environment project for the environment's team group in
  `stg` and `prd`, and the Privileged Access Manager
  entitlements its members ask for: the secret operator, the layer
  administrator and, for an environment on its own instance, the Spanner
  admin and viewer ("The team group" below).
- Firebase Authentication on the environment project (`identity-platform.tf`):
  Identity Platform initialized once, with no sign-in provider and no sign-up
  a browser could make on its own. An application that serves live pages
  signs its browsers in with a custom token its server mints, and the
  browser presents the application's web API key (its stack's) to exchange
  it; the configuration is the project's, which is why it is here and not in
  a stack (two applications in one project, or a pull-request stack beside
  the environment's, cannot each own it). Initializing it makes Firebase
  create an API key, which the layers workflow restricts after each apply
  ("Identity Platform" below).
- The cross-project load balancer grants: `roles/compute.loadBalancerServiceUser`
  on the environment project for the net layer identity and the net
  project's Compute Engine service agent, so `2-net`'s URL map can reference
  backend services created here. Addressed from `1-org`'s outputs.
- Per application, two service accounts in the environment project and their
  grants. Application codes are validated at 6 characters or fewer so every
  ID stays within 30.

### Application apply identity `imp-<env>-gbl-<app>-tofu`

Applies the application's stack (its repository's `infrastructure/`, state slot `3-app/<app>`) for this environment. On the environment project:
`roles/serviceusage.serviceUsageConsumer` (it uses the environment project
as its quota project), `roles/run.admin`, `roles/compute.loadBalancerAdmin`
(the application's serverless network endpoint groups and backend services,
which the stack makes in this project and a tag build's plan reads),
`roles/iam.serviceAccountAdmin`, `roles/iam.serviceAccountUser`,
`roles/cloudscheduler.admin`,
`roles/cloudtasks.queueAdmin`, `roles/datastore.owner` (its Firestore
database, with the indexes and time-to-live policies on it),
`roles/firebaserules.admin` (the database's security rules),
`roles/serviceusage.apiKeysAdmin` (the web API key its browsers present),
`roles/cloudbuild.builds.editor`,
`roles/logging.admin`, `roles/monitoring.admin`,
`roles/resourcemanager.projectIamAdmin`, and the custom organization role
`secretContainerAdmin` from `1-org` (secrets as containers, never payloads). Cloud Storage
is two bounded grants on the environment project: `roles/storage.admin` under
a condition admitting every bucket whose name starts with
`imp-<env>-gbl-<app>-`, which is the application's file stores
(`imp-<env>-gbl-<app>-files-<project number>`, `files-<name>` for a
named store) and its pull-request stacks' (`imp-<env>-gbl-<app>-pr<N>-files-<project number>`)
with their objects, and nothing of another application's buckets or of the
records bucket; and the organization's `storageBucketCreator` role without
condition, since creating a bucket (`storage.buckets.create`) and listing the
project's buckets (`storage.buckets.list`) are checked on the project, where
no bucket's name can admit them. Without the condition, any application's
apply identity, and so its pipeline and its pull-request builds, would read
and delete every other application's files and every record. `roles/compute.networkUser` on the environment
project only when `2-net` publishes a shared VPC (it publishes null). In tst,
on the tst instance: the organization's `spannerDatabaseCreator` role
(creating a database and listing what the instance holds are checked on the
instance) and, under a condition naming the application's own database and
its pull-request databases, `roles/spanner.databaseAdmin` (the policy, the
schema, the drop) and `roles/spanner.backupAdmin` (the backup schedules a
production stack makes on its database, which `databaseAdmin` does not read,
and the backups taken from it); nothing of another application's database.
In stg and prd the same grants on the shared instance are `2-spn`'s, from its
`database_admins`, with `roles/resourcemanager.projectIamAdmin` on the `spn`
project under a condition admitting the grants of
`roles/monitoring.metricWriter` alone, which the stack gives its runtime
identities and the deploy identity for the Spanner client's metrics. On the state
bucket (the boot project's): `roles/storage.legacyBucketReader` unconditionally
(a list is a request on the bucket and cannot be conditioned by object name),
`roles/storage.objectUser` on `3-app/<app>/<env>/` (the pull-request stacks
sit below it), and `roles/storage.objectViewer` on the upstream states its
stack reads: `1-org/`, every `2-env/<env>/` (the plan identities a
pull-request build impersonates, the previous environment's records bucket),
`2-shr/`. The deploy identity of the same application may impersonate it
(`roles/iam.serviceAccountTokenCreator` on the account): a release's tag build
applies the environment's stack as it, and in tst the pull-request build
applies the pull request's stack as it.

### Application plan identity `imp-<env>-gbl-<app>-plan`

Plans the application's stack for this environment on a pull request, from
tst's Cloud Build, and writes nothing: the plan a reviewer approves is the
plan of every environment, each made as that environment's plan identity. On
the environment project: `applicationPlanReader` (`1-org`'s custom role: the
read of every resource type the stack declares, found in the lab from
the plans' refusals, and nothing of what those resources hold; `roles/viewer`
would read the rows of a database in this project, and reads no record and
no uploaded file either way, since the records bucket and the file stores
carry no default grant to the project's basic roles),
`roles/iam.securityReviewer` (reading the IAM policies the stack's grants are
refreshed from, a bucket's and a queue's among them) and
`roles/serviceusage.serviceUsageConsumer`. On the state bucket:
`roles/storage.legacyBucketReader` unconditionally, and
`roles/storage.objectViewer` on `3-app/<app>/<env>/` and on the upstream
states the apply identity reads; no write, so the plan runs without the state
lock. On its own environment's records bucket: `roles/storage.objectViewer`,
a binding of the bucket's policy (`records.tf`),
since a pull-request build against a hotfix line reads the environment's live
record as this identity to say where the line's next release will be refused.
On the Spanner instance the application's database lives on, the
organization's `spannerPlanReader` role (the database, its IAM policy and its
backup schedules, nothing of the data): on the tst instance from this layer,
on the shared instance from `2-spn`'s `database_planners`, which also holds
`roles/iam.securityReviewer` on the `spn` project for the stack's metric
writer grants there. tst's deploy identity of the same application may impersonate it
(`roles/iam.serviceAccountTokenCreator`, read from tst's `2-env` state; tst's
own in tst), since tst's Cloud Build runs the pull-request builds; nothing
else may.

### Application deploy identity `imp-<env>-gbl-<app>-deploy`

Runs every build and deploy; never writes infrastructure. On the environment
project: `roles/serviceusage.serviceUsageConsumer`, `roles/run.developer`,
`roles/logging.logWriter`, `roles/monitoring.viewer` (reads the metrics a
maintenance step waits on: the old revision's active instances before the
database is replaced or migrated), `runJobPolicyAdmin` (`1-org`'s custom role:
reads and sets the IAM policy of Cloud Run jobs, so `bedrock deploy jobs` can
copy the template job's grant onto the job it makes for each build of an
application's job process) and `cloudBuildBuildReader` (`1-org`'s custom role:
reads the build it runs in, the pipeline's first step, and nothing else of
Cloud Build; `roles/cloudbuild.builds.builder`, the cloud's bundle for a
build's service account, would carry every object of every bucket in the
project with it); in tst also `cloudBuildTriggerRunner`, since Cloud Scheduler
runs the application's sweep trigger as this identity. Bounded grants:
`roles/storage.objectCreator` and `roles/storage.objectViewer` on its own
environment's records bucket, as bindings of the bucket's policy (`records.tf`;
a record is written once and read back: the
stale-database check of a pull-request build reads the pull request's newest
record, and the environment's live version is in its newest live record;
neither role overwrites or deletes a record, none of the deploy identity's
project roles reaches the bucket, and the apply identity it may act as holds
storage admin only under a condition naming the application's own buckets,
so it writes a record once);
`roles/secretmanager.secretAccessor` on the build-time secrets named in
`var.build_time_secrets` (empty by default) and on the deployer GitHub App's key container (`github-apps.tf`), and on no runtime secret; nothing on
a Spanner instance (pull-request databases are the pull-request stack's, which
runs as the apply identity). Three grants are made elsewhere from this identity's member:
`roles/artifactregistry.writer` on the application's own repository by
`2-shr` (`pushers`); `roles/iam.serviceAccountUser` on the application's
runtime identities by the application stack, where those identities are
created, so a deploy identity may act as its own application's processes and
no other's; and, by the same stack, `roles/spanner.databaseAdmin` on the
application's own database (a member of that database's policy; with
`roles/datastore.user` on its Firestore database where the migrate command
constructs that level), for the migrations the pipeline runs on the build
worker as this identity. The `repositories_registered` check warns while `2-shr` does not
list the application at all.

### Application operations identity `imp-<env>-gbl-<app>-ops`

A restore of the environment to a release (`bedrock restore <env> <release>`),
a release run again (`bedrock rerun <env> <release>`) and an operation on the
environment's migrations (`bedrock migration version|rerun|force`) are started
from GitHub: the application's operations workflow runs the environment's
version trigger with the instruction, and the pipeline does the work as the
deploy identity, as for any release. The workflow's job holds no key. It
exchanges GitHub's short-lived token for this identity through the
environment's workload identity pool `imp-<env>-github`
(`operations.tf`), whose provider trusts tokens of the organization's
repositories alone, from the operations workflow file, run in the GitHub
Environment named after this environment (`1-org` declares one per
environment, deploying from the default branch alone), and whose binding on
the identity narrows that to the application's own repository. The identity
holds `cloudBuildTriggerRunner` (`1-org`'s custom role: starts a trigger's
build and reads how it went) on the environment project, and
`roles/iam.serviceAccountUser` on the application's deploy identity, which
starting a trigger whose builds run as it requires, and nothing else here.
Below production the application's own stack adds one bounded read (its
`logging.tf`): `roles/logging.viewAccessor` on the view over the bucket
holding the application's build logs, so the workflow's migration job prints
the lines the migrate command wrote into the build's log and reads no other
log. In `prd` the identity serves the
rerun alone and holds those two grants and nothing more: the workflow and the
pipeline refuse the restore instruction there, no migration operation reaches
it, and it reads no log view. The role carries `cloudbuild.builds.create`,
which also submits a build without a trigger, and Cloud Build evaluates no
resource condition that would narrow it to the version trigger, so the
identity's bound is who may become it (the application's own operations
workflow, in the environment's GitHub Environment, from the default branch);
a release a rerun starts waits for its approval in Cloud Build as any
release does, and no person holds the role anywhere. The application's
placement records the environment project's id and number (`projects`,
production's for the rerun: `bedrock org register` writes them into the
application's first placement from `projects` and `projectNumbers` in
`placement.json` here, and `bedrock org render` prints them), and bedrock
renders the workflow with the provider's name (`github_identity_provider`)
and the identity's email from them.

### The team group

Each person's access to this environment comes from one group, the
environment's team group, which `placement.json` names (`teamGroups`,
rendered into `var.team_groups`) and the Workspace Admin console holds. The
group approves the environment's releases where a release waits for an
approval, and its members ask for a time-limited grant of everything else
that changes the environment. No person holds a standing role that changes
an environment, nothing in this layer names a person, and the three
environments' groups may be the same people or not: production's is named
on its own.

Release approval is the one standing grant: `roles/cloudbuild.builds.approver`
on the environment project in `stg` and `prd`, the
environments whose version triggers wait for a release's approval (every
environment but the first, the rule the application stacks declare their
triggers from); approving is the gate itself, and the audit log records who
approved. In `tst` nothing waits for an approval and the group
holds nothing standing.

Everything else is an entitlement in Privileged Access Manager
(`team-group.tf`, which first sets the service up on the project: its service
agent and the service agent role, as the console's "Set up PAM" would): a right
a member of the group asks for, in the console or
with `gcloud pam grants create`, for up to the entitlement's longest grant,
with a justification. In `stg` and `prd` the request waits
for one approval by another member of the same group, who writes a
justification of their own (a requester cannot approve their own request);
in `tst` it is granted at once. The grant is an IAM binding on
the project that the service makes for the time asked and removes after it,
and every request, approval and grant is in the audit log. The longest
grants come from `placement.json` (`entitlementDurations`, by the keys
below); unset, the secret operator an hour, the Spanner admin two hours, the Spanner viewer four hours and the layer administrator four hours.

| Entitlement | Key | What it grants, on the environment project unless the row says otherwise |
|---|---|---|
| `imp-<env>-secret-operator` | `secretOperator` | `1-org`'s `secretOperator` role: `bedrock secret add` creates a container ahead of the release that first reads it (named and labeled as the application stack names it, which adopts it at its next apply) and adds the value, and `bedrock secret pin` moves the pin; the role also finds the project by its labels and bills Secret Manager to it, and never reads a payload. |
| `imp-<env>-spanner-admin` | `spannerAdmin` | `roles/spanner.databaseAdmin` and `roles/spanner.backupAdmin`: the environment's databases, their schema, rows and backups, for a migration that stopped and the rows it validated. |
| `imp-<env>-spanner-viewer` | `spannerViewer` | `roles/spanner.databaseReader` and `1-org`'s `spannerPlanReader`: the rows, read only, and the instance with the names of its databases. |
| `imp-<env>-layer-administrator` | `layerAdministrator` | The roles this environment's layer identity (`imp-<env>-gbl-tofu`) holds on the environment project: `1-org`'s `app` role set and `roles/storage.admin` under the condition naming the records bucket, read from `1-org`'s `environment_layer_grants` output, so the two cannot drift. A recovery of this layer by hand, as the person ("Applying" above), with the next entitlement. |
| `imp-<env>-layer-state` | `layerAdministrator` | Declared by `1-org` on the boot project, not here (`1-org/entitlements.tf`): the identity's slot in the state bucket (its own prefix `2-env/<env>/`, the upstream states it reads, the bucket's list and its policy, each by a condition on the name) and `roles/serviceusage.serviceUsageConsumer`, for the boot project as the quota project. An entitlement grants on one project, and the bucket is the boot project's. |

The Spanner entitlements are declared where the environment's instance is.
For an environment on its own instance (`tst` by default) every
database in the project is the environment's, so this layer declares them on
the environment project with no condition. For an environment on the shared
instance (`stg` and `prd` by default) the databases live
in the `spn` project, where this layer's identity holds nothing, so `2-spn`
declares them on that project (`entitlements.tf` there), each database role
bounded by a condition to the environment's own databases and backups and
the organization's `spannerPlanReader` role unconditioned, for the instance
and the names of its databases; the ids are the same.

The layer administrator grants the identity's roles rather than the right
to act as the identity (`roles/iam.serviceAccountTokenCreator` on it),
because that right cannot be bounded to the one identity: IAM evaluates no
`resource.name` for a service account, so a condition naming the identity
never admits the token call, and without a condition the role admits every
service account in the project, the deploy and application identities among
them.

The layer administrator covers this layer alone. The apply identities of
`0-bootstrap` and `1-org` (in the boot project) and of `2-shr`, `2-spn` and
`2-net` (in the shared projects) sit outside the environment projects, and
the groups are per environment, so no entitlement covers them: their
recovery is the bootstrap administrator's, with the organization-level roles
first-time setup uses, since those layers change rarely and a broken apply
there is a setup-grade event (`0-bootstrap/README.md`, "Recovery, by hand").

Creating the entitlements needs `roles/privilegedaccessmanager.admin` on the
project, in `1-org`'s `app` role set, with the Privileged Access Manager API
in its `app` API set; the set is completed by refusal.

### Identity Platform

Identity Platform is Google's sign-in service behind Firebase Authentication,
and `identity-platform.tf` initializes it once per environment project
("What it creates" above). Initializing it makes Firebase create an API key in
the project, named "Browser key (auto created by Firebase)", with no restriction
of any kind. An API key is a public value (a browser presents it, and anyone
can copy it from a page), so what keeps one harmless is its API restriction:
the list of APIs that accept it. A key with none is accepted by every API in
the project that takes an API key. Nothing presents this one: each
application's browsers present the application's own web API key, which its
stack makes and restricts to the two sign-in APIs,
`identitytoolkit.googleapis.com` and `securetoken.googleapis.com`. Left alone, it would
be a standing credential with no owner.

No layer can declare it. OpenTofu adopts a key that already exists only by
its id, which Firebase assigns, and the Google provider has no data source
that finds a key by its name. So the layers workflow restricts it: after each
apply of this layer, as the layer identity, a step lists the keys of that
name in the environment project and restricts each to the same two sign-in
APIs, which leaves it no more capable than the applications' keys. The layer
identity holds `roles/serviceusage.apiKeysAdmin` for this, in `1-org`'s `app`
role set. A restriction rather than a deletion, because Firebase may make a
deleted key again when the Firebase console is used on the project; a key
Firebase makes after a run is restricted by the next one. Until then
`bedrock org check` names every key in an environment project that carries
no API restriction, this one or any other, and Run workflow with `2-env`
restricts it at once.

After an apply by hand ("Applying" above), the same restriction by hand, for
the environment's project:

```bash
for key in $(gcloud services api-keys list --project <env project> --filter 'displayName="Browser key (auto created by Firebase)"' --format 'value(name)'); do
  gcloud services api-keys update "$key" --api-target=service=identitytoolkit.googleapis.com --api-target=service=securetoken.googleapis.com
done
```

## The GitHub authorization, before the first application

The connection authorizes with a GitHub OAuth token that only a browser can
produce. Once per organization, after `1-org` has made the tst project and
before the first application is registered: `bedrock org register` refuses
the first application while either value below is unset, since the
applications' triggers exist once this layer holds the connection and the
repository's link, and nothing is built by hand before them.

The browser step signs in to GitHub as the organization's machine account,
`imp-machine` (`githubMachineAccount` in `placement.json`): a GitHub user that
acts for no person. It must belong to the `imp-example` organization and be an
owner of it, since installing a GitHub App on an organization takes an owner, and
the connection's token is that account's, so a person leaving the organization
breaks nothing.

1. In the Google Cloud console, in the **tst** project, open Cloud Build >
   Repositories (2nd gen) > Create host connection > GitHub. Sign in to GitHub
   as the machine account `imp-machine`, install or select the Google Cloud
   Build GitHub App on the `imp-example` organization with access to all
   repositories, and finish. The console stores the token as a secret in the
   tst project.
2. Read the installation ID from the app's settings page on GitHub
   (`https://github.com/organizations/imp-example/settings/installations/<id>`)
   and the token secret's version name from Secret Manager in the tst project
   (`projects/<tst project>/secrets/<name>/versions/<n>`). Put both in
   `terraform.tfvars` as `github_app_installation_id` and
   `github_oauth_token_secret_version`.
3. Apply this layer through the workflow (the pull request that sets the two
   values). Besides tst's own connection, the tst apply grants
   `roles/secretmanager.secretAccessor` on that secret to the Cloud Build
   service agent of each of the three environment projects, so stg and prd
   reuse the same version rather than repeating the browser step.

If the console's connection is left in place, delete it after the apply, or
import it: this layer's connection is the one that lives on. An alternative
that skips the console is a fine-grained personal access token of
`imp-machine` (contents, metadata, pull requests) added by hand as a version
of a container created here, with the same variable pointing at it.

## The deployer GitHub App's key, after each environment's first apply

The pipeline talks back on a pull request as the organization's deployer
GitHub App (`github-apps.tf`): a deployment carrying the environment's URL, a
comment, the guard's refusals. The app itself, its permissions and its
installation are made once on GitHub (the root `README.md`, "The two GitHub
Apps"). What this layer needs from it:

1. In `terraform.tfvars`: the app's App ID, from its settings page, as
   `github_deployer_app_id`, one value for the organization.
2. In a terminal, in this repository, per environment once this layer has
   applied there and made the container
   `imp-<env>-gbl-github-deployer-key`, as a member of the environment's
   team group under its secret operator entitlement ("The team group" above):

   ```bash
   bedrock secret add github-deployer-key <env> --from-file <the .pem file>
   bedrock secret pin github-deployer-key <env> <version>
   ```

   `add` puts the key into the environment's container and prints the
   version; `pin` checks with Secret Manager that the version is enabled and
   writes its resource name into `terraform.tfvars`, under the environment in
   `github_deployer_key_secret_versions`. Pinned, never `latest`, and never
   edited by hand.
3. On GitHub, the pull request that carries the two values: its merge
   applies this layer through the workflow, and each application's stack in
   the environment passes the key to its pipeline at its next apply.

Until an environment has both values, its pipeline talks back through
nothing; the rest of the layer and the deployments are unaffected.

## Prerequisites this layer does not create

- On the state bucket, for each environment layer identity, from `1-org`
  (`workflow.tf` there): the list, object read on `1-org/`, `2-shr/`,
  `2-spn/`, `2-net/` and every `2-env/`, object user on `2-env/<env>/`, and
  the bucket's policy authority (0-bootstrap's custom role) so this layer
  can grant the application identities their slots (below). The federation
  binding that lets the workflow become the identity is `1-org`'s too.
- `roles/resourcemanager.tagUser` on the public-invoker tag for each
  application's apply identity, from `1-org` (`public-invokers.auto.tfvars`),
  committed after this layer has created the identities.
- Nothing grants the application apply identity anything on the boot
  project, so the application stacks use their environment project as quota
  project; this layer grants `serviceUsageConsumer` there for that.
- `roles/privilegedaccessmanager.admin` on the environment project for the
  environment layer identity and the Privileged Access Manager API on the
  project, both from `1-org` (its `app` role set and API set), for the team
  group's entitlements; and the team groups themselves, made in the Workspace
  Admin console and named in `placement.json` (`teamGroups`), since IAM
  refuses a grant to a group that does not exist.

## Inputs

| Name | Description | Type | Default | Required |
|---|---|---|---|:---:|
| `applications` | Application codes registered in this environment, 6 characters or fewer. | `list(string)` | `["harbor", "beacon"]` | no |
| `boot_project_id` | Boot project; quota and billing project for API calls. | `string` | n/a | yes |
| `build_time_secrets` | Secret IDs the deploy identity may read during a build, by application. | `map(list(string))` | `{}` | no |
| `environment` | `tst`, `stg`, or `prd`; passed as `-var` on every run. | `string` | n/a | yes |
| `github_app_installation_id` | Cloud Build GitHub App installation on the organization. | `number` | n/a | yes |
| `github_oauth_token_secret_version` | `projects/<tst project>/secrets/<name>/versions/<n>`. | `string` | n/a | yes |
| `github_deployer_app_id` | App ID of the deployer GitHub App the pipeline talks back as, from the app's settings page ("The deployer GitHub App's key" above). | `number` | `null` | no |
| `github_deployer_key_secret_versions` | Per environment, the pinned Secret Manager version of the deployer app's private key, in the container this layer creates: added with `bedrock secret add github-deployer-key <env>` and written here by `bedrock secret pin github-deployer-key <env> <version>`. | `map(string)` | `{}` | no |
| `github_organization` | GitHub organization of the application repositories. | `string` | `"imp-example"` | no |
| `team_groups` | The environments' team groups by code, a group's address each, from `placement.json` (`teamGroups`). | `map(string)` | rendered | no |
| `spanner_config` | tst instance configuration. | `string` | `"nam10"` | no |
| `spanner_processing_units` | tst instance size; 100 or 200. | `number` | `100` | no |
| `state_bucket` | State bucket, for the upstream layers' outputs. | `string` | n/a | yes |

## Outputs

Read by the application's stack through `data "terraform_remote_state"`, prefix
`2-env/<env>`.

| Name | Description |
|---|---|
| `applications` | Per application: `apply_identity_email`, `apply_identity_member`, `deploy_identity_email`, `deploy_identity_member`, `deploy_identity_id` (full resource name, for triggers), `repository_id`, `repository_name`. |
| `connection_id`, `connection_name` | The Cloud Build GitHub connection. |
| `environment` | Environment code. |
| `prefix` | Naming prefix, from `1-org`. |
| `project_id`, `project_number` | The environment project. |
| `records_bucket` | The deployment-record bucket. |
| `region`, `region_code`, `secondary_region`, `secondary_region_code` | The two regions and their codes. |
| `spanner_instance` | `{ project, name }` of the instance applications use. |

## Upstream outputs assumed

Read with `try()`, so a missing one reads as absent. Names to reconcile with
the layers that publish them:

| Layer | Output | Used for |
|---|---|---|
| `1-org` | `prefix`, `project_ids`, `project_numbers`, `layer_service_accounts`, `environment_layer_grants` (the layer administrator's roles), `gcp_region`, `gcp_secondary_region`, `region_code`, `secondary_region_code`, `secret_container_admin_role`, `secret_operator_role`, `run_job_policy_admin_role`, `spanner_plan_reader_role` | everything |
| `2-shr` | `repository_names` | map of application code to repository ID; the `repositories_registered` warning |
| `2-spn` | `project_id`, `instance_name` | the shared instance for stg and prd (project falls back to `1-org`'s) |
| `2-net` | `shared_vpc_id` | null today; gates `compute.networkUser` |
| `2-net` | `load_balancer_service_user`, `compute_service_agent` | the two `loadBalancerServiceUser` grants (derived from `1-org` until `2-net` exists) |

And the reverse direction: this layer's `applications` output names the
identities the shared layers bind (`2-shr`'s `pushers` and `pullers`,
`2-spn`'s `database_admins`); their values are rendered from `placement.json`
into each layer's `applications.auto.tfvars`, and the identities must exist
before those layers apply.
