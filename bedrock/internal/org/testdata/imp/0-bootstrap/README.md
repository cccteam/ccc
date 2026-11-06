# 0-bootstrap

The chicken-and-egg layer. It adopts the resources the creation made, creates the org layer identity, grants both identities what they need
on the organization's folder, and makes the Cloud Build triggers that plan and apply every
layer of this repository. Nothing is granted at the organization node: the folder is the whole of this
organization's reach. Its first apply is the root organization's
creating identity's, in the creation, before any layer identity can run
("Created by the root organization" below); from then on the
apply trigger applies it as the boot identity, like every layer ("The Cloud
Build triggers" below).

## What it creates

- Adopts, through `import` blocks: the organization's folder at the org root
  and the `terraform` folder under it, the
  boot project `imp-boot-gbl-core-<suffix>` inside it, and the boot layer
  identity `imp-boot-gbl-tofu`. From the first apply on they are ordinary
  managed resources.
- The boot project's API set (billing, Cloud Build, resource manager,
  Essential Contacts, IAM, IAM credentials, logging, org policy, Privileged
  Access Manager, service usage, storage), its labels, and the deletion of its
  default compute service account.
- Role bindings on the organization's folder for the boot layer identity
  (`var.boot_layer_roles`) and for the org layer identity
  (`var.org_layer_roles`), the boot layer identity's roles on the boot project
  alone (`var.boot_project_roles`), the org layer identity's on the boot
  project alone (`var.org_project_roles`: Custom Roles Administrator, for the
  custom role `1-org` creates there), and `roles/privilegedaccessmanager.admin` on the
  boot project alone for the org
  layer identity: `1-org` declares there each environment's layer state
  entitlement, the environment layer identity's slot in the state bucket for a
  recovery of `2-env` by hand (`2-env/README.md`, "Applying").
- The org layer identity `imp-org-gbl-tofu`, in the boot project.
- The two boot layers' plan identities `imp-boot-gbl-plan` and
  `imp-org-gbl-plan`, in the boot project, with the reads on the
  organization's folder of `var.boot_plan_roles`, the boot project as their quota project, the
  planner's right to become them (`cloud-build.tf`), for the boot one the
  reads of the connection, the repository link and the triggers on the boot
  project (`roles/cloudbuild.connectionViewer`, `roles/cloudbuild.builds.viewer`,
  `cloud-build.tf`) and, for the org one, the
  read of the infrastructure GitHub App's key (`github.tf`) and of the
  entitlements `1-org` declares on the boot project: a pull request plans each
  boot layer as its own read-only identity. A binding on the organization's
  folder is this layer's to make; nothing of the organization's is bound at
  the organization node.
- The custom role `projectUpdater` in the boot project
  (`resourcemanager.projects.update`), granted to the boot identity on it;
  `1-org` makes the same role in each project it creates, for its identity.
- `roles/billing.user` on the billing account for both identities, only when
  `manage_billing_iam` is true ("The spend budget" below).
- The Cloud Build side (`cloud-build.tf`): the GitHub connection
  `imp-boot-uc1-github` in the boot project, from the Cloud Build GitHub App's
  installation and the token secret's alias `placement.json` records
  (`cloudBuildInstallationId`, `cloudBuildTokenSecretVersion`; "Created by
  the root organization" below); the link of this repository
  (imp-example/imp-impulse-infrastructure) under it; the read of the token secret for
  the boot project's Cloud Build service agent and for each environment
  project's, by the numbers `placement.json` records (`projectNumbers`), since
  the environments' connections (`2-env`) name the same alias; the two
  runner identities `imp-org-gbl-planner` and `imp-org-gbl-applier`
  with their grants; and the two triggers, `imp-boot-uc1-layers-plan` on a pull
  request into `master` and `imp-boot-uc1-layers-apply` on a push
  to it, whose steps are the trigger's own ("The Cloud Build triggers").
- The grants on the three GitHub Apps' key secrets, which live in the root
  organization `imr`'s root project (`root` in
  `placement.json`; `github.tf`): the org identity's policy administration of
  the deployer and release secrets, the org plan identity's read of the
  infrastructure key, and both boot plan identities' read of the three
  secrets' policies. This layer makes no secret: every reader here names a
  secret's alias `current`, which that organization pins ("The GitHub Apps'
  keys").
- The state bucket's grants for the four identities (`state-bucket.tf`): the
  list, each apply identity's own prefix (`0-bootstrap/`, `1-org/`), the org
  identity's read of `0-bootstrap/`, each plan identity's read of the prefixes
  its layer's plan reads, and the custom role `stateBucketPolicyAdmin` in
  the boot project (reads and sets a bucket's IAM policy, nothing else) on
  the bucket for the two apply identities, so each layer can
  grant for the identities it creates; for both runners, the read of the organization's
  folder, its projects and their IAM policies for the check's leftover report; and for the
  applier, the markers under `applied/` that record each layer's last successful apply
  (`cloud-build.tf`).

Not created here, on purpose: the state bucket. It is seeded and left outside
OpenTofu because this layer's own state lives in it.

## Created by the root organization

Nothing here was seeded or applied by hand. The root organization
`imr` (`root` in `placement.json`) created this
organization from a pull request in its repository (`bedrock org create
imp` there, then the merge), as its creating identity
`imr-create-gbl-tofu`, which holds Folder Creator at the
organization node and Billing Account Administrator on the billing account,
granted by that organization's bootstrap (its `README.md`, "Bootstrapped by one
command"). The creation did under a new folder what a hand seed and the first
applies would, and left no person with a grant to trim:

1. **The seed, by API.** The organization's folder at the organization root,
   named `imp`, which made the creating identity its administrator, as
   Google makes a folder's creator, with Project Creator and Tag Administrator
   for the identity itself, for the first applies; the `terraform` folder
   under it; the boot project
   `imp-boot-gbl-core-a1b2` in it, linked to the billing account with its APIs
   enabled; the boot layer identity `imp-boot-gbl-tofu` with the roles
   of `var.boot_layer_roles` on the organization's folder and of
   `var.boot_project_roles` on the boot project; Folder Admin on the
   folder for the break-glass account (`seed@imp.example`), its one
   standing grant; the state bucket `imp-boot-gbl-state-a1b2`; and, in the root
   organization's root project, the boot identity's `secretPolicyAdmin` on
   the three GitHub Apps' key secrets and the Cloud Build token secret ("The
   GitHub Apps' keys" below).
2. **The first apply of this layer**, against the bucket (`tofu init` at the
   prefix `0-bootstrap`; no local state, nothing to migrate), as the creation
   identity: the folders, the project and the boot identity adopted through
   the `import` blocks (`terraform.tfvars` carries their ids, seeded by the
   creation), and the rest made: the org layer identity, the plan identities,
   the custom roles, the key grants, the connection from the Cloud Build
   authorization the root organization recorded (`cloudBuildInstallationId`
   and `cloudBuildTokenSecretVersion`, which name the Bot Account's token
   in that organization's root project by its alias `current`, so a rotation
   there reaches every connection: one authorization serves every
   organization in the Google Cloud organization), the link of this
   repository under it, the two triggers, and the bucket's grants. Then the
   creating identity granted the boot identity and the org identity Billing
   Account User on the billing account, which this organization's own applies
   need the day `1-org` links a project it creates.
3. **The first apply of `1-org`**, the same way, with the infrastructure
   GitHub App's token for its GitHub provider: the folders, the six projects
   with their liens, identities, custom roles and org policies. Its
   `project_ids` and `project_numbers` outputs went into `placement.json`
   (`projects`, `projectNumbers`) and the layers were rendered again, so every
   identity is named by its project before anything was pushed.
4. **This repository**, made private in the `imp-example`
   organization with the rendered foundation as its first push, which the
   apply trigger of step 2 ran on: every layer, as its own identity, in layer
   order, since none had a marker yet ("The Cloud Build triggers" below).
   Each layer's `.terraform.lock.hcl` came with the push, from the first
   applies' inits.
5. **The withdrawal.** The creating identity took its own grants off
   everything it made: Owner on each project, Folder Admin and Folder Editor
   on each folder, and its three roles on the organization's folder. It holds
   nothing here now; a marker in the root organization's state bucket says the
   creation is done, and a pull request there recorded the folder
   (`hostingOrganizations`), from which that organization keeps the boot
   identity's grants on the shared secrets rendered.

One thing stays by hand, by a billing administrator, after the creation: the
spend budget ("The spend budget" below). The consent screen of each environment
project and the role groups follow as the root `README.md` says ("Created by
the root organization" there).

`bedrock org check`, run with Google credentials that read the projects' and
folders' IAM policies, lists each grant of a person's it finds (a person's
Owner on a project, a person's Folder Admin or Folder Editor on a folder
under the organization's, a role of the break-glass account's beyond its
Folder Admin) and each grant on the organization's folder to a principal
outside the organization: neither the break-glass account nor an identity in
one of this organization's projects. After a creation there is none; one
means the creating identity's withdrawal did not finish, or somebody granted
by hand.

### The spend budget, by a billing administrator

The two layer identities, `imp-boot-gbl-tofu` and `imp-org-gbl-tofu`,
hold `roles/billing.user` on the billing account `012345-6789AB-CDEF01` since the
creation, which granted it as the root organization's creating identity, a
Billing Account Administrator. `1-org` needs it the day it links a project it
creates; the creation linked the six. `manage_billing_iam = false` in
`terraform.tfvars` keeps this layer from asserting the same two members itself,
which the identity applying it may not do; set it to `true` once that identity
is a billing administrator, and the two `google_billing_account_iam_member`
resources assert them.

In the Billing console, as a billing administrator, set the spend budget:
Billing, the billing account `012345-6789AB-CDEF01`, Budgets & alerts, Create
budget. Scope it to every project of the billing account, give it a monthly
amount, and keep the alert thresholds (50%, 90% and 100% of the actual spend)
and the email to the billing account's administrators and users. A budget
sends alerts and stops nothing. Nothing in the layers renders it: it belongs to
the billing account, where no layer identity holds a role that manages budgets.

## Inputs

| Name | Description | Type | Default | Required |
|---|---|---|---|:---:|
| `billing_account_id` | Billing account ID to link projects to. | `string` | n/a | yes |
| `boot_layer_roles` | Roles granted to the boot layer identity on the organization's folder. The seed grants the same list; change both together. | `list(string)` | eight roles, see `variables.tf` | no |
| `boot_plan_roles` | Reads on the organization's folder of the two boot layers' plan identities. | `list(string)` | eight roles, see `variables.tf` | no |
| `boot_project_id` | Project ID of the seeded boot project, suffix included. Adopted, not created. | `string` | n/a | yes |
| `boot_project_roles` | Roles granted to the boot layer identity on the boot project alone, the ones Google never binds on a folder. The seed grants the same list; change both together. | `list(string)` | one role, see `variables.tf` | no |
| `org_project_roles` | Roles granted to the org layer identity on the boot project alone, the ones Google never binds on a folder: Custom Roles Administrator, for the custom role `1-org` creates there. | `list(string)` | one role, see `variables.tf` | no |
| `github_default_branch` | The infrastructure repository's default branch: the plan trigger runs on a pull request into it, the apply trigger on a push to it. | `string` | `"master"` | no |
| `github_organization` | The GitHub organization of the infrastructure and application repositories. | `string` | `"imp-example"` | no |
| `infrastructure_repository` | This repository's name, the one the connection links and the triggers watch. | `string` | `"imp-impulse-infrastructure"` | no |
| `manage_billing_iam` | Whether this layer manages the two `roles/billing.user` grants. Needs Billing Account Administrator to apply. | `bool` | `true` | no |
| `org_layer_roles` | Roles granted to the org layer identity on the organization's folder. | `list(string)` | eleven roles, see `variables.tf` | no |
| `prefix` | Short org-wide prefix used in resource names. | `string` | `"imp"` | no |
| `state_bucket` | The seeded state bucket, granted on here for the four identities and the applier. | `string` | the placement's `stateBucket` | no |
| `organization_folder_id` | Numeric ID of the seeded organization folder, named after the prefix, without `folders/`. Adopted, not created; 1-org creates the environment folders under it. |
| `terraform_folder_id` | Numeric ID of the seeded `terraform` folder, under the organization folder, without `folders/`. Adopted, not created. | `string` | n/a | yes |

`terraform.tfvars` sets the required ones and `manage_billing_iam = false`;
the rest are rendered from `placement.json`, the Cloud Build GitHub
authorization and the environment projects' numbers among them.

## Outputs

| Name | Description |
|---|---|
| `apply_trigger_name`, `plan_trigger_name` | The two triggers; `null` until the authorization is recorded. |
| `applier_member`, `planner_member` | The two runner identities as IAM members, which `1-org` lets impersonate the identities it creates. |
| `boot_plan_service_accounts` | The two boot layers' plan identities' emails (boot, org). |
| `boot_project_id` | The boot project, quota and billing project for every downstream layer. |
| `boot_project_number` | The boot project's number, recorded in `placement.json` as `projectNumbers.boot`. |
| `boot_service_account_email` | The boot layer identity. |
| `connection_name` | The boot project's Cloud Build GitHub connection; `null` until the authorization is recorded. |
| `org_id` | Numeric organization ID. |
| `org_service_account_email` | The org layer identity. |
| `state_bucket_policy_admin_role` | The custom role in the boot project that sets a bucket's IAM policy. |
| `terraform_folder_id` | The folder holding the automation plane. |

## The Cloud Build triggers

Every layer of this repository is planned on a pull request and applied on
its merge by two Cloud Build triggers in the boot project, rendered by
bedrock like the layers (`cloud-build.tf`). The plan trigger
(`imp-boot-uc1-layers-plan`) runs on every pull request into
`master`: it plans each layer whose `.tf`, `.tfvars` or lock
file the pull request changes, every layer when `.opentofu-version` changes,
side by side, each as that layer's plan identity, and posts each plan on the
pull request as the deployer GitHub App, one comment per layer kept current
across pushes. The apply trigger (`imp-boot-uc1-layers-apply`) runs on every push
to `master`: it applies each layer changed since that layer's
last successful apply (a marker under `applied/` in the state bucket holds
the commit; a layer with no marker is applied), as that layer's apply
identity, in layer order (0-bootstrap, 1-org, 2-shr and 2-spn, then 2-env for tst, stg and prd, then 2-net), one at a time, stopping at
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

To apply one layer again with no change to it, which a registration's second
pass of `2-env` needs, start the apply trigger by hand on the default branch
with the layer, and for `2-env` the environment (without one, the three in
order); in a terminal, as a person holding `roles/cloudbuild.builds.editor`
on the boot project:

```bash
gcloud builds triggers run imp-boot-uc1-layers-apply --region=us-central1 --project=<boot project> --branch=master --substitutions=_LAYER=2-env,_ENVIRONMENT=tst
```

Cloud Build runs the builds of a trigger as they come; two applies of one
layer at once would contend for the layer's state lock, and the second
fails on it, so start a run by hand when none is running.

Who runs what, and why the first time works in order:

- The plan trigger's builds run as the planner (`imp-org-gbl-planner`),
  the apply trigger's as the applier (`imp-org-gbl-applier`). Neither
  holds a layer's roles: each impersonates the layer identity of its kind for
  that layer's tofu (`roles/iam.serviceAccountTokenCreator` on each identity:
  this layer grants it on its four identities, `1-org` on the twelve it
  creates), so a plan runs with a plan identity's reads and an
  apply with the apply identity's writes. What each holds itself: the build
  logs, the read of its own build (its substitutions, the pull request, the
  commit), the connection's read token (the changed files are read through
  it), the two apps' keys, the read of the folder, the projects and their IAM
  policies for the check's leftover report, and, for the applier, the markers
  under `applied/` in the state bucket.
- IAM refuses a binding for a service account that does not exist, so each
  layer grants for the identities it creates. This layer grants for the boot
  identity, the org identity and the two boot layers' plan identities
  (`imp-boot-gbl-plan`, `imp-org-gbl-plan`), all of which exist
  when its grants apply; `1-org` grants for the twelve identities of the six
  projects. `2-env` grants for the application identities it creates, with
  the bucket's policy authority `1-org` gives each environment layer identity.
- The first apply of this layer and the first apply of `1-org` are the
  root organization's creating identity's, in the creation ("Created by the
  root organization" above, and `1-org/README.md`), as the organization
  folder's administrator and with the roles it granted itself there. The
  bucket exists from the seed, so this layer's grants on it apply in that
  first apply; `1-org`'s grants apply in its first apply as the same identity,
  and from then on as the org identity, which holds the bucket's policy
  authority from here.
- From the first push on, a push to `master` applies every
  layer through the apply trigger, and a pull request plans them: every
  identity exists and `placement.json` names every project before the push.
  A run that touches a layer whose identity's project the placement does not
  record (`REPLACEME`) says so and stops.

Each identity's role set is completed by refusal: a layer applied as its
identity that is refused a permission gets the role that carries it, in
`var.boot_layer_roles` (or `var.boot_project_roles`, for a role Google binds on
a project and never on a folder), `var.org_layer_roles` (or
`var.org_project_roles`, the same for the org layer identity on the boot
project) or `1-org`'s `layer_roles`; a
plan of a boot layer, in `var.boot_plan_roles`.

## The infrastructure GitHub App

`1-org` configures the applications' repositories with the GitHub provider,
which needs a token with an organization owner's rights. In a run of the
triggers that token is an installation token of the infrastructure GitHub App,
minted in the run from its private key. A third app beside the release and
deployer apps, because widening the deployer app would give every application
pipeline those rights. The app is the root organization's: its bootstrap made
it from bedrock's declaration (the root `README.md`, "Bootstrapped by one
command") with the repository permissions Actions (read), Administration (read
and write), Contents (read and write), Metadata (read) and Pull requests
(read and write), and the
organization permissions Administration, Variables and Self-hosted runners
(read and write), installed on the organization for all repositories, and this
organization records the same App ID (`githubInfrastructureAppId` in
`placement.json`, filled in by the creation). The provider reads a repository's
environments and their branch policies on the Actions permission and writes
them on Administration; with Contents the creation pushes a new organization's
rendered repository, and with Pull requests it opens the hand-off pull request
in the root organization's repository; the organization permissions are for the
`CI_LARGE_RUNNER` variable, the runner and its runner group, which the root
organization's apply sets and makes when its placement names a larger runner
(`ciLargeRunner`, `ciLargeRunnerSize`). The app bypasses the branch rules it
writes, here as in the root organization's repository (`1-org/github.tf`).

The key itself lives in the root organization `imr`'s root
project (`root` in `placement.json`), added, proven and pinned there by its key
operators; a run of the triggers here reads the alias `current` of
`imr-root-gbl-github-infrastructure-key`, which that organization pins, and nothing is
added in this repository. Until the alias names a version, a run of the
triggers that plans or applies `1-org` says so and stops; the Google side of
the layer is unaffected. A person applying `1-org` by hand keeps using their
own token (`1-org/README.md`).

## The GitHub Apps' keys

The three GitHub Apps' private keys live in the root organization
`imr`'s root project (`imr-root-gbl-core-c3d4`; `root` in
`placement.json`), one secret per app for every bedrock organization in the
Google Cloud organization: `imr-root-gbl-github-infrastructure-key`,
`imr-root-gbl-github-deployer-key` and `imr-root-gbl-github-release-key`. Every reader here names
a secret's alias `current`, which that organization's placement pins and its
apply moves; nothing is added or pinned in this repository, `bedrock secret
add` and `pin` refuse here and name that organization, and a rotation there
switches every reader at once (that organization's `README.md`, "The GitHub
Apps' keys"). No key reaches GitHub.

What this organization records is the apps' App IDs, which are not secrets:
`githubInfrastructureAppId` for the infrastructure app, `githubDeployerAppId`
for the deployer app, which the plan trigger's run posts each layer's plan on
the pull request as and every application's pipeline talks back on its pull
requests as (a deployment carrying the environment's URL, a comment, the
guard's refusals), and `githubReleaseAppId` for the release app, which each
application's release trigger cuts the application's releases as
(release-please, in Cloud Build as the application's release identity). The
apps themselves, their permissions and their installations are the root
organization's bootstrap's, made once from bedrock's declaration of each (the
root `README.md`, "Bootstrapped by one command"); this organization records
the same App IDs, filled in by the creation.

How the reads are granted (`github.tf`): the root organization gives this
organization's boot identity `secretPolicyAdmin` on the three secrets, a
custom role of its project that reads and sets a secret's IAM policy and
nothing else. This layer grants from it: the org identity's policy
administration of the deployer and release secrets, which `1-org` hands to
each environment layer identity so `2-env` can grant the applications'
identities their read; the org plan identity's read of the infrastructure
key; the runners' reads of the infrastructure and deployer keys
(`cloud-build.tf`); and both boot plan identities' read of the three secrets'
policies, which a plan of this layer or of `1-org` refreshes the grants
through. That is the only reach this organization has into that one.

Both grants came with the creation ("Created by the root organization"
above): `bedrock org create` listed this organization in the root
organization's `placement.json` (`hostingOrganizations`: the prefix and the
boot project, whose suffix the command chose) before the merge, and the
creating identity, which holds the same role on the four shared secrets,
granted the boot identity as the seed and then applied this layer with its
own policy administration of them. The hand-off recorded the folder, from
which that organization's `0-bootstrap` keeps the boot identity's grant
rendered. Nothing was granted by hand.

Until the App IDs are recorded, the plans are in the runs' logs alone and
the pipelines talk back through nothing; until the root organization has
pinned a key, the pipelines that read it say so and wait. Nothing else is
affected.

## Recovery, by hand

When the triggers cannot run (the connection broken, the repository
unreachable, a change that must be applied before it can merge), a person
applies from their own computer. For `2-env`, a member of the environment's
team group asks for two entitlements for the same short time: the layer
administrator, the environment layer identity's roles on the environment
project, and the layer state entitlement, the identity's slot in the state
bucket and the boot project as its quota project. The person then runs the
layer as themselves, with the identity's roles, on the same code as the
triggers, and the audit log names the person (`2-env/README.md`). This layer
and `1-org` have no entitlement: their recovery is the break-glass account's,
which grants itself, for the run, the roles the layer's identity holds on the
organization's folder (`variables.tf`) with the Folder Admin the creation
gave it there, applies from the layer's directory against the bucket as the
creation's first applies ran (`tofu init`, then `tofu apply`, with its own
sign-in and, for `1-org`, their own GitHub token), and takes the grants off
after. An apply by hand leaves the layer's marker as it was, so the next
push applies the layer again: the plan is empty when the hand apply was
complete.

## Destroying the organization

`bedrock org destroy` deletes every project of the organization, and with them
everything the layers and the applications made: the layer identities, the
state bucket with every layer's and every application's state and records, the
connection and the triggers, the registry and its images, the Spanner
instances and databases, the load balancer and the zone,
every environment's services, jobs, secrets, records and files. The folders,
the custom roles and the tags, the org policies, the entitlements and the audit
log sink go with them, and on GitHub the rulesets, the operations environments,
the runner group and the organization variable. The repositories stay, with
their code, pull requests and releases. Google keeps a deleted project for 30
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

Then it destroys `1-org` and `0-bootstrap`, in that order, each in two steps.
An apply with `destroy=true`, the variable both layers declare for this command
alone, records the projects' deletion policy as DELETE, records that no API is
disabled on the destroy (the project module disables each API it enabled before
deleting the project, and an API a resource outside the layer's state still uses,
Compute Engine's for `2-env`'s pull-request backend, refuses; the project's
deletion takes everything regardless), and lifts the folders' deletion
protection, all three read by the provider from the state and not from the
configuration. Then the destroy, tried again after a pause while it fails: a
folder refuses for a while after its last project's deletion, and the next
attempt gets past it. `1-org`'s GitHub repositories are taken out of its state
first, and so are its tag keys, values and bindings, which go with the
projects that hold them (a tag value refuses to go while a binding still holds
it, and the bindings go with the projects' deletion, not before).
`0-bootstrap`'s state lives in the bucket the boot project holds, so its
destroy runs on a copy of the layer with a local copy of the state, pulled
before the project goes. A layer whose state holds nothing is skipped, so the
command also finishes an organization whose first applies stopped halfway. The
inner layers and the applications' stacks are never destroyed one by one:
deleting a project takes everything in it, whatever its resources' own
protections say.

When `2-net` registers domains (`registrations` in `2-net/terraform.tfvars`),
the network project is kept, with its folder and the organization's: a
registration cannot move to another project, and that project is a person's to
deal with (`1-org/README.md`, "Removing a project"). The command refuses a
checkout rendered by an earlier bedrock, which declares no `destroy` variable:
`bedrock org render` first.

It ends with a line in `JOURNAL.md`. Starting over is a new record in the root
organization's repository (`bedrock org create imp` there, which
chooses a new boot project suffix, and its pull request), after this
organization's entry is taken out of that placement; the GitHub side (the
organization, its Bot Account `imp-bot`, the apps and their
installations) is as it was, and this repository stays with its history: the
new creation pushes its foundation on it, as its placement names another
state bucket.
