# 0-bootstrap

The chicken-and-egg layer. It adopts the resources the seed step created by
hand, creates the dns layer identity, grants it the zones' administration in the
boot project and the boot identity what it needs on the organization's folder, and makes the Cloud Build triggers that plan and apply every
layer of this repository. Nothing is granted at the organization node: the
folder is the whole of this organization's reach. Its first apply is the
break-glass account's, from a person's computer, before any layer identity
can run; from then on the
apply trigger applies it as the boot identity, like every layer ("The Cloud
Build triggers" below).

## What it creates

- Adopts, through `import` blocks: the organization's folder at the org root
  and the `terraform` folder under it, the
  boot project `imz-boot-gbl-core-<suffix>` inside it, and the boot layer
  identity `imz-boot-gbl-tofu`. From the first apply on they are ordinary
  managed resources.
- The boot project's API set (Cloud Build, resource manager, Cloud DNS, IAM,
  IAM credentials, logging, Secret Manager, service usage, storage), its
  labels, and the deletion of its default compute service account.
- Role bindings on the organization's folder for the boot layer identity
  (`var.boot_layer_roles`), and `roles/dns.admin` on the boot project alone for
  the dns layer identity, whose zones live there (`1-dns`).
- The dns layer identity `imz-dns-gbl-tofu`, in the boot project.
- The two boot layers' plan identities `imz-boot-gbl-plan` and
  `imz-dns-gbl-plan`, in the boot project, with the reads on the
  organization's folder of `var.boot_plan_roles`, the boot project as their quota project, the
  planner's right to become them (`cloud-build.tf`), for the boot one the
  reads of the connection, the repository link and the triggers on the boot
  project (`roles/cloudbuild.connectionViewer`, `roles/cloudbuild.builds.viewer`,
  `cloud-build.tf`) and, for the dns one, the read of the infrastructure
  GitHub App's key (`github.tf`) and of the zones (`roles/dns.reader` on the
  boot project): a pull request plans each boot layer as its own read-only
  identity. A binding on the organization's folder is this layer's to make;
  nothing of the organization's is bound at the organization node.
- The custom role `projectUpdater` in the boot project
  (`resourcemanager.projects.update`), granted to the boot identity on it.
- `roles/billing.user` on the billing account for the boot identity, only when
  `manage_billing_iam` is true. See step 5.
- The Cloud Build side (`cloud-build.tf`): the GitHub connection
  `imz-boot-uc1-github` in the boot project, from the Cloud Build GitHub App's
  installation and the token secret's version `placement.json` records
  (`cloudBuildInstallationId`, `cloudBuildTokenSecretVersion`; "The Cloud
  Build GitHub authorization" below); the link of this repository
  (imp-example/imz-zones-infrastructure) under it; the read of the token secret for
  the boot project's Cloud Build service agent; the two runner identities
  `imz-org-gbl-planner` and `imz-org-gbl-applier` with their
  grants; and the two triggers, `imz-boot-uc1-layers-plan` on a pull request into
  `master` and `imz-boot-uc1-layers-apply` on a push to it, whose
  steps are the trigger's own ("The Cloud Build triggers").
- The two GitHub Apps' key containers in the boot project (`github.tf`):
  `imz-boot-gbl-github-infrastructure-key` for the infrastructure app, whose token
  `1-dns`'s GitHub provider needs, and `imz-boot-gbl-github-deployer-key` for the
  deployer app, which the plan trigger posts the plans as; the dns identity
  administers both.
- The state bucket's grants for the four identities (`state-bucket.tf`): the
  list, each apply identity's own prefix (`0-bootstrap/`, `1-dns/`), each plan
  identity's read of the prefixes its layer's plan reads, and the custom
  role `stateBucketPolicyAdmin` in the boot project (reads and sets a
  bucket's IAM policy, nothing else) on the bucket for the boot identity,
  which applies this layer through the triggers; and for the applier, the
  markers under `applied/` that record each layer's last successful apply
  (`cloud-build.tf`).

Not created here, on purpose: the state bucket. It is seeded and left outside
OpenTofu because this layer's own state lives in it.

## First-time setup

Steps 1, 2, 5 and 6 happen once, by hand. Everything step 1 creates is adopted
by an `import` block in step 3, so nothing stays outside OpenTofu except the
bucket, the spend budget (step 5) and, while `manage_billing_iam` is false,
the billing grant.

The person who runs them is signed in as the organization's break-glass
account (`seed@imp.example`, `breakGlassAccount` in `placement.json`): a
Google account created in the Google Workspace Directory with no person
behind it, which seeds the organization and applies the two foundation
layers the first time, and is signed into after that only when the pipeline
cannot act (step 6, "Recovery, by hand" and "Destroying the organization"
below, and a GitHub App's key rotation). Two grants are made for it in Google
Cloud IAM before the seed, by an administrator of the Google Cloud
organization and of the billing account; the table in step 1 names them. It
creates the organization's folder, which makes it the folder's administrator,
and everything else happens under that folder: the seed grants it there what
the first applies need, and Google leaves a creator with Owner on each
project and Folder Admin and Folder Editor on each folder it creates. All of
those are temporary. The layers run as their identities; once the apply trigger
applies the layers, step 6 removes them, and the account keeps one grant,
Folder Admin on the organization's folder: its key to everything below, for
the day the pipeline cannot act. `bedrock org check` lists every grant still
to remove.

### 1. Seed with gcloud

In a terminal, signed in as the break-glass account (`seed@imp.example`).
The seed, the first apply of this layer and the first apply of `1-dns`
run as that account, before any layer identity exists. Above the
organization's folder it holds two roles, granted in Google Cloud IAM:

| Role | Granted on | Needed for |
|---|---|---|
| Folder Creator (`roles/resourcemanager.folderCreator`) | the organization | the organization's folder, at the organization root; the seed's terraform folder is made under it as its administrator |
| Billing Account User (`roles/billing.user`) | the billing account | linking the boot project to the billing account |

Everything else happens under the folder the seed creates, as its
administrator, which Google makes a folder's creator. Sign in, then check
the two roles before anything is created: `bedrock org preflight`, run at
this repository's root, asks Google which of the permissions behind each
role these credentials hold, on the organization and the billing account
`placement.json` names. It prints one line per role, `holds` or `missing`
with the permissions tested, and exits 1 naming each missing role and who
grants it. Seed only once it passes:

```bash
gcloud auth login
gcloud auth application-default login
bedrock org preflight

export ORG_ID=123456789012
export BILLING_ACCOUNT_ID=012345-6789AB-CDEF01
export PREFIX=imz
export REGION=us-central1
export BREAK_GLASS=seed@imp.example
```

Create the organization's folder at the org root, named after the prefix.
Google gives its creator Folder Admin and Folder Editor on it, and from here
on everything the seed and the layers make sits under it: the folder is the
whole of this organization's reach in the Google Cloud organization, and
another bedrock organization beside it keeps its own, since sibling folders
must differ in name. Then the `terraform` folder under it, and the roles the
seed and the first applies need on the folder, granted to the account itself:
Project Creator, for the boot project. Its Folder Admin covers the rest: the folders it
makes, the IAM on them and on the projects it creates, which it is Owner of
as their creator. Step 6 removes these again.

```bash
gcloud resource-manager folders create \
  --display-name="${PREFIX}" \
  --organization="${ORG_ID}"

export ORGANIZATION_FOLDER_ID=$(gcloud resource-manager folders list \
  --organization="${ORG_ID}" --filter="displayName=${PREFIX}" \
  --format='value(name)' | sed 's|folders/||')

gcloud resource-manager folders create \
  --display-name=terraform \
  --folder="${ORGANIZATION_FOLDER_ID}"

export TERRAFORM_FOLDER_ID=$(gcloud resource-manager folders list \
  --folder="${ORGANIZATION_FOLDER_ID}" --filter="displayName=terraform" \
  --format='value(name)' | sed 's|folders/||')

for role in \
  roles/resourcemanager.projectCreator
do
  gcloud resource-manager folders add-iam-policy-binding "${ORGANIZATION_FOLDER_ID}" \
    --member="user:${BREAK_GLASS}" --role="${role}" --condition=None
done
```

Create the boot project. The four-hex suffix is what the project factory would
have generated, and the ID can never be changed afterwards; the display name
carries no suffix, as the project module names the projects. Cloud Build and Secret Manager are enabled here because step 2's
browser authorization uses both before this layer's first apply:

```bash
export BOOT_PROJECT_ID="${PREFIX}-boot-gbl-core-$(od -An -tx1 -N2 /dev/urandom | tr -d ' \n')"

gcloud projects create "${BOOT_PROJECT_ID}" \
  --name="${PREFIX}-boot-gbl-core" \
  --folder="${TERRAFORM_FOLDER_ID}" \
  --labels=terraform=true,terraform_source_path=0-bootstrap,source_repo=imz-zones-infrastructure,organization=imz,environment=boot,bedrock-lab=true

gcloud billing projects link "${BOOT_PROJECT_ID}" --billing-account="${BILLING_ACCOUNT_ID}"

gcloud services enable \
  cloudbilling.googleapis.com cloudbuild.googleapis.com cloudresourcemanager.googleapis.com \
  iam.googleapis.com iamcredentials.googleapis.com secretmanager.googleapis.com \
  serviceusage.googleapis.com storage.googleapis.com \
  --project="${BOOT_PROJECT_ID}"
```

Stop here for a minute or two. The owner binding a project creator receives is
not immediately visible to the IAM service, and the next command fails with
`PERMISSION_DENIED` on a permission you genuinely hold. This works as a poll:

```bash
until gcloud iam service-accounts list \
  --project="${BOOT_PROJECT_ID}" >/dev/null 2>&1; do sleep 10; done
```

Create the boot layer identity and grant it its roles on the organization's
folder. This list is the same one in `var.boot_layer_roles`; change both
together. Nothing is granted at the organization node:

```bash
export SA_EMAIL="${PREFIX}-boot-gbl-tofu@${BOOT_PROJECT_ID}.iam.gserviceaccount.com"

gcloud iam service-accounts create "${PREFIX}-boot-gbl-tofu" \
  --project="${BOOT_PROJECT_ID}" \
  --display-name="OpenTofu SA - ${PREFIX}-boot-gbl" \
  --description="Layer identity for ${PREFIX}-boot-gbl. Applies the 0-bootstrap layer from the infrastructure repository's apply trigger."

for role in \
  roles/cloudbuild.builds.editor \
  roles/cloudbuild.connectionAdmin \
  roles/iam.roleAdmin \
  roles/iam.serviceAccountAdmin \
  roles/resourcemanager.folderAdmin \
  roles/resourcemanager.projectCreator \
  roles/resourcemanager.projectDeleter \
  roles/resourcemanager.projectIamAdmin \
  roles/serviceusage.serviceUsageAdmin
do
  gcloud resource-manager folders add-iam-policy-binding "${ORGANIZATION_FOLDER_ID}" \
    --member="serviceAccount:${SA_EMAIL}" --role="${role}" --condition=None
done
```

Create the state bucket. It reuses the boot project's suffix because bucket
names are globally unique too. Versioning keeps every prior state file:

```bash
export STATE_BUCKET="${PREFIX}-boot-gbl-state-${BOOT_PROJECT_ID##*-}"

gcloud storage buckets create "gs://${STATE_BUCKET}" \
  --project="${BOOT_PROJECT_ID}" \
  --location="${REGION}" \
  --uniform-bucket-level-access \
  --public-access-prevention

gcloud storage buckets update "gs://${STATE_BUCKET}" \
  --versioning \
  --update-labels=terraform=true,terraform_source_path=0-bootstrap,source_repo=imz-zones-infrastructure,organization=imz,environment=boot,bedrock-lab=true
```

Keep `BOOT_PROJECT_ID`, `ORGANIZATION_FOLDER_ID`, `TERRAFORM_FOLDER_ID`, and `STATE_BUCKET`. Steps 2 to
4 need them, and so does `placement.json`.

### 2. The Cloud Build GitHub authorization

The boot project's Cloud Build connection, the layers', authorizes with a GitHub
token that only a browser can produce, once per organization, here, before
this layer's first apply. The person at the browser is signed in as the
break-glass account, Owner on the boot project from step 1; the GitHub
account signed in is the organization's machine account,
`imp-machine` (`githubMachineAccount` in `placement.json`): a GitHub
user that acts for no person, belongs to the `imp-example`
organization and is an owner of it, since installing a GitHub App on an
organization takes an owner, and the connection's token is that account's,
so a person leaving the organization breaks nothing.

1. In the Google Cloud console, in the boot project `BOOT_PROJECT_ID`, as the
   break-glass account: Cloud Build, Repositories (2nd gen), Create host
   connection, GitHub, in the region `us-central1`, named
   `imz-boot-uc1-github`, the name this layer gives it. Sign in to GitHub as
   the machine account `imp-machine`, install or select the Google
   Cloud Build GitHub App on the `imp-example` organization with
   access to all repositories, and finish. The console stores the token as a
   regional secret in the boot project.
2. Read the installation ID from the app's settings page on GitHub
   (`https://github.com/organizations/imp-example/settings/installations/<id>`)
   and the token secret's version name from Secret Manager in the boot
   project (`projects/<boot project>/locations/us-central1/secrets/<name>/versions/<n>`).
   Record both in `placement.json`, as `cloudBuildInstallationId` and
   `cloudBuildTokenSecretVersion`; `bedrock org render` writes them into this
   layer's connection and triggers. This layer's first apply then adopts the console's connection
   (step 3).

An alternative that skips the console is a classic personal access token of
`imp-machine` with the `repo`, `read:user` and `read:org` scopes
(Cloud Build accepts no fine-grained token), added by hand as a version of a
secret in the boot project, with `cloudBuildTokenSecretVersion` naming it; the
installation is still made on GitHub, on the app's page. A classic token
expires on the schedule its creator set, while the console's authorization
does not; and GitHub revokes either kind after a year unused. The token is
exercised when a repository link is made, at an application's registration,
so an organization that registers nothing for a year may find it revoked at
the next registration. The remedy, either way, is the browser step again,
signed in as the machine account `imp-machine`, or a new classic
token pinned as a new version.

### 3. Apply with local state

In a terminal, in this directory. Fill in `terraform.tfvars` first:
`boot_project_id`, `organization_folder_id` and `terraform_folder_id` from step 1. Then record the
seed's values in `placement.json` at the repository root (`stateBucket`,
`projects.boot` and `projectNumbers.boot`, the number from
`gcloud projects describe "${BOOT_PROJECT_ID}" --format='value(projectNumber)'`),
and the bedrock the triggers run: `bedrock upgrade`, in this repository,
which records the release (or a commit, `bedrock upgrade <commit>`) as
`bedrockVersion` and `bedrockSha256` and runs `bedrock org render`: the
backend blocks, this layer's bucket grants and the triggers all name the
seed's values.

The first apply runs on local state, so the `backend "gcs"` block in
`initialize.tf` has to be commented out for it. A configuration that declares
a backend refuses to plan until that backend is initialized, and the bucket
name is not in the block yet. The connection the console made in step 2 is
adopted rather than made again:

```bash
cd 0-bootstrap
# comment out the backend "gcs" { ... } block in initialize.tf
tofu init
tofu import 'google_cloudbuildv2_connection.github[0]' "projects/${BOOT_PROJECT_ID}/locations/${REGION}/connections/imz-boot-uc1-github"
tofu plan
```

The plan should show the folder, project, boot service account and the
connection as imports rather than creates, and the rest as creates: the dns
identity and its grants, the custom roles, the key containers, the runner
identities, the repository link and the two triggers, and the bucket's
grants. If any of the imports shows as a create, the ID in `boot_project_id`
or `terraform_folder_id`, or the connection's name, is wrong. Stop and fix it
before applying; creating a second boot project is a mess to unwind.

```bash
tofu apply
```

This writes `terraform.tfstate` in the directory (ignored by git). `init` also
writes `.terraform.lock.hcl`, which is committed: it cannot be generated
without an `init`, so this first one produces it.

### 4. Migrate the state into the bucket

Restore the `backend "gcs"` block, which names the bucket since the render in
step 3. Then:

```bash
tofu init -migrate-state
```

Answer yes to copying the local state into the bucket, confirm with
`tofu plan` (no changes), and delete the local `terraform.tfstate` and its
backup. From here on every run inits against the bucket directly.

Record `boot_project_id` in `1-dns/terraform.tfvars` (a seeded file, which a
render never rewrites: a repository rendered again on a new seed still
carries the old value), and add a line to `JOURNAL.md`.

### 5. Billing grants and the budget, by a billing administrator

The break-glass account holds Billing Account User on the billing account. That role
links projects to the account but cannot change the account's IAM policy, so
the `google_billing_account_iam_member` resource in this layer cannot be
applied as the break-glass account. `manage_billing_iam = false` in `terraform.tfvars`
skips it. Instead, in a terminal, a Billing Account Administrator makes the
grant by hand:

```bash
export BILLING_ACCOUNT_ID=012345-6789AB-CDEF01
export BOOT_PROJECT_ID=<from step 1>

gcloud billing accounts add-iam-policy-binding "${BILLING_ACCOUNT_ID}" \
  --member="serviceAccount:imz-boot-gbl-tofu@${BOOT_PROJECT_ID}.iam.gserviceaccount.com" \
  --role=roles/billing.user
```

Nothing depends on the grant until the apply trigger runs this layer as the
boot identity, which keeps the boot project linked to the account; the
break-glass account's own Billing Account User role covers the first
apply. Set `manage_billing_iam = true` once the identity applying this layer
can manage billing IAM; the resource then asserts the same member. The dns
identity links nothing and needs no grant.

In the Billing console, as the same billing administrator, set the spend
budget: Billing, the billing account `012345-6789AB-CDEF01`, Budgets & alerts,
Create budget. Scope it to every project of the billing account, give it a
monthly amount, and keep the alert thresholds (50%, 90% and 100% of the actual
spend) and the email to the billing account's administrators and users. A
budget sends alerts and stops nothing. Nothing in the layers renders it: it
belongs to the billing account, where no layer identity holds a role that
manages budgets.

### 6. Trimming the break-glass account

In a terminal, once the apply trigger has applied the layers (every push to
`master` from here on): as the break-glass account for the
grants on the folders and projects, and as the administrators who made them
for the two above the folder. What comes off, and why it was there:

- Owner on the boot project: a creator's grant, which Google gives
  whoever creates a project.
- Folder Admin and Folder Editor on the `terraform` folder, and Folder Editor on the
  organization's folder: a creator's grant, which Google gives whoever creates
  a folder.
- Project Creator on the organization's folder: the seed's grant for the first
  applies (step 1).
- Folder Creator at the organization, by its administrator, and Billing
  Account User on the billing account, by a billing administrator: the two
  roles step 1's table names, which the seed and the first applies needed and
  the layers' identities now hold for themselves (step 5).

What stays: Folder Admin on the organization's folder, the account's one
standing grant and its key to everything below. With it, on the day the
pipeline cannot act, the account grants itself for the run what the recovery
needs and takes it back after ("Recovery, by hand"). Nothing of the
organization's is left at the organization node.

```bash
gcloud projects remove-iam-policy-binding <project id> --member="user:${BREAK_GLASS}" --role=roles/owner
gcloud resource-manager folders remove-iam-policy-binding <folder id> --member="user:${BREAK_GLASS}" --role=roles/resourcemanager.folderAdmin
gcloud resource-manager folders remove-iam-policy-binding <folder id> --member="user:${BREAK_GLASS}" --role=roles/resourcemanager.folderEditor
gcloud resource-manager folders remove-iam-policy-binding "${ORGANIZATION_FOLDER_ID}" --member="user:${BREAK_GLASS}" --role=<each role the seed granted in step 1>
gcloud organizations remove-iam-policy-binding "${ORG_ID}" --member="user:${BREAK_GLASS}" --role=roles/resourcemanager.folderCreator
gcloud billing accounts remove-iam-policy-binding "${BILLING_ACCOUNT_ID}" --member="user:${BREAK_GLASS}" --role=roles/billing.user
```

`bedrock org check` lists each grant of this step still held on a project or
a folder, with its place, when it runs with Google credentials that read
their IAM policies. The two above the folder it cannot see from inside the
folder, so their removal is the administrators' to confirm.

## Inputs

| Name | Description | Type | Default | Required |
|---|---|---|---|:---:|
| `billing_account_id` | Billing account ID to link projects to. | `string` | n/a | yes |
| `boot_layer_roles` | Roles granted to the boot layer identity on the organization's folder. The seed grants the same list; change both together. | `list(string)` | nine roles, see `variables.tf` | no |
| `boot_plan_roles` | Reads on the organization's folder of the two boot layers' plan identities. | `list(string)` | eight roles, see `variables.tf` | no |
| `boot_project_id` | Project ID of the seeded boot project, suffix included. Adopted, not created. | `string` | n/a | yes |
| `github_default_branch` | The infrastructure repository's default branch: the plan trigger runs on a pull request into it, the apply trigger on a push to it. | `string` | `"master"` | no |
| `github_organization` | The GitHub organization of the infrastructure and application repositories. | `string` | `"imp-example"` | no |
| `infrastructure_repository` | This repository's name, the one the connection links and the triggers watch. | `string` | `"imz-zones-infrastructure"` | no |
| `manage_billing_iam` | Whether this layer manages the `roles/billing.user` grant. Needs Billing Account Administrator to apply. | `bool` | `true` | no |
| `organization_domain` | Domain of the GCP organization. | `string` | n/a | yes |
| `prefix` | Short org-wide prefix used in resource names. | `string` | `"imp"` | no |
| `state_bucket` | The seeded state bucket, granted on here for the four identities and the applier. | `string` | the placement's `stateBucket` | no |
| `organization_folder_id` | Numeric ID of the seeded organization folder, named after the prefix, without `folders/`. Adopted, not created. |
| `terraform_folder_id` | Numeric ID of the seeded `terraform` folder, under the organization folder, without `folders/`. Adopted, not created. | `string` | n/a | yes |

`terraform.tfvars` sets the required ones and `manage_billing_iam = false`;
the rest are rendered from `placement.json`, the Cloud Build GitHub
authorization among them.

## Outputs

| Name | Description |
|---|---|
| `apply_trigger_name`, `plan_trigger_name` | The two triggers; `null` until the authorization is recorded. |
| `applier_member`, `planner_member` | The two runner identities as IAM members, which impersonate the two layers' identities. |
| `boot_plan_service_accounts` | The two boot layers' plan identities' emails (boot, dns). |
| `boot_project_id` | The boot project, quota and billing project for every downstream layer. |
| `boot_project_number` | The boot project's number, recorded in `placement.json` as `projectNumbers.boot`. |
| `boot_service_account_email` | The boot layer identity. |
| `connection_name` | The boot project's Cloud Build GitHub connection; `null` until the authorization is recorded. |
| `github_deployer_key_secret` | The container of the deployer GitHub App's private key. |
| `github_infrastructure_key_secret` | The container of the infrastructure GitHub App's private key. |
| `org_id` | Numeric organization ID. |
| `org_service_account_email` | The dns layer identity. |
| `state_bucket_policy_admin_role` | The custom role in the boot project that sets a bucket's IAM policy. |
| `terraform_folder_id` | The folder holding the automation plane. |

## The Cloud Build triggers

Every layer of this repository is planned on a pull request and applied on
its merge by two Cloud Build triggers in the boot project, rendered by
bedrock like the layers (`cloud-build.tf`). The plan trigger
(`imz-boot-uc1-layers-plan`) runs on every pull request into
`master`: it plans each layer whose `.tf`, `.tfvars` or lock
file the pull request changes, every layer when `.opentofu-version` changes,
side by side, each as that layer's plan identity, and posts each plan on the
pull request as the deployer GitHub App, one comment per layer kept current
across pushes. The apply trigger (`imz-boot-uc1-layers-apply`) runs on every push
to `master`: it applies each layer changed since that layer's
last successful apply (a marker under `applied/` in the state bucket holds
the commit; a layer with no marker is applied), as that layer's apply
identity, in layer order (0-bootstrap, then 1-dns), one at a time, stopping at
the first failure. A plan that fails only because it reads outputs the plan
of an earlier layer in the same pull request creates passes, "planned after"
that layer applies, since the merge applies that layer first (the root README,
"How the layers are applied").

The steps are the trigger's, so a pull request cannot change them; a pull
request changes only what the steps read. Each run is three steps for a
release pin and four for a commit pin: the first reads the bedrock pin off
`placement.json` in the checkout (`bedrockVersion` and, for a release,
`bedrockSha256`; `bedrock upgrade` moves it) and downloads the release's
binary, verified against the checksum, or for a commit checks with git that a
branch of `cccteam/ccc` reaches it (a commit no branch reaches stops the
run); for a commit pin a second step builds it with `go install`, where Go's
checksum database verifies it (a release pin renders no such step, so a
production organization's runs never pull the Go image); the next runs
`bedrock org check`, which refuses the run when a committed layer differs
from what the placement renders, holds a `.tf` file no render produces, or
holds an override file; the last runs `bedrock org plan` or `bedrock org
apply`. The steps follow the pin's kind at render time: `bedrock upgrade`
renders this layer with the pin it moves, and when the pin changes kind, a
release to a commit or a commit to a release, it applies this layer itself,
as the person running it, after `yes` typed back, since no trigger applies
this layer and the release shape's fetch step refuses a commit pin (no step
follows to build it). A pull request that reaches the plan trigger with a
commit pin while the triggers are in the release shape fails at the fetch
step, whose message says to apply this layer from that branch by hand. The
bedrock the run uses is the one the checkout pins, never one the trigger
names.

The trigger's steps lag one apply behind the repository. A change to them
is rendered by a new bedrock, planned and applied by a run on the old steps,
and the next run is the first on the new ones. Since the pin is read at run
time, a pull request that moves it already runs the new bedrock under the
old steps, so the one rule bedrock keeps is that a release works under the
previous release's steps. Both triggers pass `_BEDROCK_STEPS`, the shape of
their steps and the kind of pin they were rendered for, and `bedrock org
plan` and `apply` say in the run's log when they run under another shape.

To apply one layer again with no change to it, start the apply trigger by hand
on the default branch with the layer; in a terminal, as a person holding `roles/cloudbuild.builds.editor`
on the boot project:

```bash
gcloud builds triggers run imz-boot-uc1-layers-apply --region=us-central1 --project=<boot project> --branch=master --substitutions=_LAYER=1-dns
```

Cloud Build runs the builds of a trigger as they come; two applies of one
layer at once would contend for the layer's state lock, and the second
fails on it, so start a run by hand when none is running.

Who runs what, and why the first time works in order:

- The plan trigger's builds run as the planner (`imz-org-gbl-planner`),
  the apply trigger's as the applier (`imz-org-gbl-applier`). Neither
  holds a layer's roles: each impersonates the layer identity of its kind for
  that layer's tofu (`roles/iam.serviceAccountTokenCreator` on each identity:
  this layer grants it on its four identities, every identity a zones
  organization has), so a plan runs with a plan identity's reads and an apply
  with the apply identity's writes. What each holds itself: the build logs,
  the read of its own build (its substitutions, the pull request, the
  commit), the connection's read token (the changed files are read through
  it), the two apps' keys, and, for the applier, the markers under `applied/`
  in the state bucket.
- IAM refuses a binding for a service account that does not exist, so each
  layer grants for the identities it creates. This layer grants for the boot
  identity, the dns identity and the two boot layers' plan identities
  (`imz-boot-gbl-plan`, `imz-dns-gbl-plan`), all of which exist
  when its grants apply; `1-dns` creates no identity.
- The first apply of this layer and the first apply of `1-dns` are the
  break-glass account's (steps 3 and 4 above, and `1-dns/README.md`), as the
  organization folder's administrator and with the roles step 1 grants. The bucket exists from step 1, so this layer's
  grants on it apply in step 3; `1-dns` grants nothing on it.
- From the first apply of this layer on, a push to `master`
  can apply this layer and `1-dns` through the apply trigger, and a pull
  request can plan them (their four identities and the runners' grants on
  them exist). Nothing follows: a zones organization has no other layer.

Each identity's role set is completed by refusal: a layer applied as its
identity that is refused a permission gets the role that carries it, in
`var.boot_layer_roles` or, for the dns identity, `bootstrap.tf`'s grant on
the boot project; a plan of a boot layer, in `var.boot_plan_roles` or the dns
plan identity's grant there.

## The infrastructure GitHub App

`1-dns` rules this repository's default branch with the GitHub provider,
which needs a token with an organization owner's rights. In a run of the
triggers that token is an installation token of the infrastructure GitHub
App, minted in the run from its private key. An app beside the deployer app,
because widening the deployer app would give the plan comments those rights.
One GitHub organization holds one such app, shared by every bedrock
organization in it: where a hosting organization has it already, this
organization records the same App ID and installs nothing new. Once, by a
person:

1. On GitHub, as an owner of the `imp-example` organization: the
   organization's Settings, Developer settings, GitHub Apps, New GitHub App.
   Name it `imp-example-infrastructure`, give it any homepage URL,
   turn Webhook off. Repository permissions: Administration (read and write),
   Metadata (read); the provider adopts this repository and writes its
   ruleset on Administration. Where can this app be installed: only this
   account. Create it, and note the App ID on its settings page. A permission
   added later is accepted on the installation: the organization's Settings,
   Third-party Access, GitHub Apps, the app.
2. On the app's settings page: Private keys, Generate a private key. The
   browser downloads a `.pem` file.
3. On the same page: Install App, on the organization, for all repositories.
4. In a terminal, in this repository, as the break-glass account, once
   this layer has made the container: record the App ID in `placement.json`
   as `githubInfrastructureAppId`, then add the key to the container and pin
   the version `add` prints (1 for the first):

   ```bash
   bedrock secret add github-infrastructure-key --from-file <the .pem file>
   bedrock secret pin github-infrastructure-key <version>
   ```

   `add` puts the key into `imz-boot-gbl-github-infrastructure-key` in the boot project.
   `pin` checks with Secret Manager that the version is enabled, records it as
   `githubInfrastructureKeyVersion` in `placement.json` and runs `bedrock org
   render`. Commit what they changed and delete the `.pem` file. Pinned, never
   `latest`: a new key is a new version and a new pin, and nobody edits the
   version in `placement.json` by hand.

Until both values are recorded, a run of the triggers that plans or applies
`1-dns` says so and stops; the Google side of the layer is unaffected. A
person applying `1-dns` by hand keeps using their own token
(`1-dns/README.md`).

## The deployer GitHub App's key

The plan trigger's run posts each layer's plan on the pull request as the
organization's deployer GitHub App. The app itself, its permissions and its
installation are made once on GitHub (the root `README.md`, "The deployer
GitHub App"); a GitHub organization that holds a hosting organization has it
already, and this organization records the same App ID. What this layer needs
from it, once, after its first apply has made the container:

1. In `placement.json`: the app's App ID, from its settings page, as
   `githubDeployerAppId`.
2. In a terminal, in this repository, as the break-glass account:

   ```bash
   bedrock secret add github-deployer-key --from-file <the .pem file>
   bedrock secret pin github-deployer-key <version>
   ```

   `add` puts the key into `imz-boot-gbl-github-deployer-key` in the boot project and
   prints the version; `pin` checks with Secret Manager that the version is
   enabled, records it as `githubDeployerKeyVersion` in `placement.json` and
   runs `bedrock org render`. Commit what they
   changed and delete the `.pem` file. Pinned, never `latest`, and never
   edited by hand.

Until both values are recorded, the plans are in the runs' logs alone;
nothing else is affected.

## Recovery, by hand

When the triggers cannot run (the connection broken, the repository
unreachable, a change that must be applied before it can merge), a person
applies from their own computer, signed in as the break-glass account: with
the Folder Admin it keeps on the organization's folder it grants itself, for
the run, the roles the layer's identity holds there (`variables.tf`), applies
the way the first applies ran (step 3 above, without the local state;
`1-dns/README.md`, "Applying"), and takes the grants off after. An apply by
hand leaves the layer's marker as it was, so the next push applies the layer
again: the plan is empty when the hand apply was complete.

## Destroying the organization

`bedrock org destroy` deletes the boot project, and with it everything the
layers made: the layer identities, the state bucket with both layers' states,
the connection and the triggers, the GitHub Apps' keys, and the zones with
their record sets. The folders and the custom roles go with it, and on GitHub
this repository's ruleset. The repository stays, with its code and pull
requests. Every domain delegated to this organization then points at servers
that answer nothing, so a zones organization is destroyed once no domain is
delegated to it (`1-dns/README.md`). Google keeps a deleted project for 30
days, in which the console can restore it; its id never returns after.

It runs by hand, at a person's computer, signed in as the break-glass account
(`gcloud auth application-default login`), holding Owner on the organization's
folder for the run, which the Folder Admin it keeps there lets it grant itself
and which goes with the folder; with `GITHUB_TOKEN`
set to the infrastructure GitHub App's installation token or an organization
owner's token (gh's token serves when gh is signed in as one). It says what
goes, project by project, and asks for the prefix typed back before anything
runs; `--yes` stands in for it where there is no terminal, as in a disposable
organization's scripts.

Then it destroys `1-dns` and `0-bootstrap`, in that order. `1-dns` first: this
repository and the zones with their record sets are taken out of its state
(the repository stays on GitHub; the zones go with the boot project, which
their `prevent_destroy` would otherwise refuse), then the destroy, which
deletes the ruleset. `0-bootstrap` in two steps: an apply with `destroy=true`,
the variable it declares for this command alone, records the boot project's
deletion policy as DELETE, records that no API is disabled on the destroy, and
lifts the folders' deletion protection, all three read by the provider from
the state and not from the configuration; then the destroy, tried again after
a pause while it fails, since a folder refuses for a while after its last
project's deletion. Its state lives in the bucket the boot project holds, so
the destroy runs on a copy of the layer with a local copy of the state,
pulled before the project goes. A layer whose state holds nothing is skipped,
so the command also finishes an organization whose first applies stopped
halfway. The command refuses a checkout rendered by an earlier bedrock, which
declares no `destroy` variable: `bedrock org render` first.

It ends with a line in `JOURNAL.md`. Starting over is a new seed (step 1, with
a new boot project), `bedrock org render` on its values, and the hand steps
again; the GitHub side (the organization, its machine account
`imp-machine`, the apps and their installations) is as it was.
