# imp-impulse-infrastructure

OpenTofu configurations for the imp.example Google Cloud organization,
rendered by bedrock. The name says whose and what: `imp` is the
organization prefix every resource here carries, and this is the Impulse
infrastructure, one repository per (organization, kind), so a GitHub
organization can hold infrastructure for more than one Google Cloud
organization without the names colliding. The other way round holds too: a
Google Cloud organization can hold more than one bedrock organization, each
under a folder of its prefix's name at the organization root (`0-bootstrap`),
with projects, identities, custom roles and a tag key of its own. The
projects carry the label `organization = imp`, and the two kinds of
name that are unique across the Google Cloud organization, the custom roles'
ids and the public-invoker tag key's short name, carry a four-hex suffix of
this organization's (`0-bootstrap`'s `random_id.organization`), so two
organizations never collide on one and one torn down never waits for the
other's names to clear. Development runs organizations side by side this
way. Modeled on
[cccteam/tf-gcp-setup](https://github.com/cccteam/tf-gcp-setup): numbered
layers, one identity per layer, no static credentials anywhere. The difference
is the control plane. There is no Scalr; state lives in a Cloud Storage bucket
in the boot project, and the layers run from two Cloud Build triggers in the
boot project, whose steps live in the trigger and not in this repository, as
their own service accounts, with no key anywhere: a pull request plans, its
merge applies.

The running log is [JOURNAL.md](JOURNAL.md). Every step that changes the organization
gets a line there first.

## Layers

| Layer | Directory | Applied as | Creates |
|---|---|---|---|
| 0 | [`0-bootstrap/`](0-bootstrap/) | the bootstrap administrator the first time (local state, then migrated); then the apply trigger as `imp-boot-gbl-tofu` | adopts the seeded `terraform` folder, boot project, and boot identity; the org layer identity and its org-level grants; the project-updater and state-bucket-policy custom roles; the Cloud Build connection, the two runner identities and the two triggers that plan and apply the layers; the two GitHub Apps' key containers; the state bucket's grants for its two identities |
| 1 | [`1-org/`](1-org/) | the bootstrap administrator the first time; then the apply trigger as `imp-org-gbl-tofu` | folders `shared`, `tst`, `stg`, `prd`; the org policy baseline; Essential Contacts; the six projects; a layer identity and a plan identity per project and the two boot layers' plan identities, each impersonated by the trigger runner of its kind, with its state bucket grants; the `secretContainerAdmin` custom role; the applications' repositories |
| 2 | [`2-shr/`](2-shr/) | the apply trigger as `imp-shr-gbl-tofu` | one Artifact Registry Docker repository per application, immutable tags, cleanup policies; reader for each environment's Cloud Run, writer for each application's deploy identities |
| 2 | [`2-spn/`](2-spn/) | the apply trigger as `imp-spn-gbl-tofu` | the shared Spanner instance `imp-spn-gbl-spanner` (nam10, 100 processing units, capped at 200); instance-level database admin grants; the Spanner entitlements of the environments on it |
| 2 | [`2-net/`](2-net/) | the apply trigger as `imp-net-gbl-tofu` | the global external Application Load Balancer, its address, SSL policy, managed certificate and map, the apps domain zone, and a URL map routed from `hosts`; no VPC |
| 2 | [`2-env/`](2-env/) | the apply trigger as `imp-<env>-gbl-tofu`, once per environment (`-var environment=`) | what every application in an environment shares: the tst Spanner instance, the Cloud Build GitHub connection and repository links, the deployment-record bucket, the team group's release approval and entitlements, the per-application apply and deploy identities with their grants |
| 3 | the application's own repository, `infrastructure/` (state slot `3-app/<app>/<env>` in this organization's bucket) | `imp-<env>-gbl-<app>-tofu`, once per environment | the application's stack, derived from its code by bedrock: runtime identities, database, secret containers, Cloud Run services and job, load balancer backend, Cloud Build triggers; harbor's is at imp-example/harbor |

The three shared layers read `1-org`'s state and are independent of one
another. `2-env` and the application stacks are one directory each, applied
once per environment: no workspaces, one state prefix per environment
supplied at init, with `TF_DATA_DIR` keeping a backend cache per environment
(see [`2-env/README.md`](2-env/README.md)). The shared layers and the
environment layers feed each other by variables rather than by writes across
projects: `2-env` publishes each application's identities, and `2-shr`'s
`pushers` and `2-spn`'s `database_admins` list them; an application's stack
(in its own repository since 2026-09-26, its state still at `3-app/<app>/<env>`
here) publishes its backend service, and `2-net`'s `hosts` routes to it.

## Naming

```
{prefix}-{environment}-{region}-{purpose}
```

Prefix `imp`. `gbl` is the region segment only for resources that really are
global: projects, folders, service accounts, IAM. Regional resources carry the
region code, `uc1` for us-central1 and `uw3` for us-west3.

| Resource | Name |
|---|---|
| boot project | `imp-boot-gbl-core-a1b2` |
| test environment project | `imp-tst-gbl-core-a1b2` |
| org layer identity | `imp-org-gbl-tofu` |
| tst layer identity / plan identity | `imp-tst-gbl-tofu` / `imp-tst-gbl-plan` |
| state bucket | `imp-boot-gbl-state-a1b2` |

Project IDs and bucket names are globally unique and permanent, so both carry a
random four-hex suffix. The custom roles' ids (`orgProjectUpdater_a1b2`) and the
public-invoker tag key's short name (`public-invoker-a1b2`) carry one of the
organization's own, since both are unique across the Google Cloud organization
and a deleted role's id stays taken for 37 days; the READMEs name the roles by
their bare names. Every project carries the label `organization = imp`, and every project and bucket the label
`bedrock-lab = "true"`.

## How the layers are applied

Routine changes go through pull requests. Two Cloud Build triggers in the
boot project, rendered by bedrock like the layers (`0-bootstrap/cloud-build.tf`;
`0-bootstrap/README.md`, "The Cloud Build triggers"), run them. The plan
trigger plans every layer a pull request changes as that layer's plan
identity and posts the plan on the pull request, one comment per layer, kept
current across pushes; the apply trigger, on the merge, applies every layer
changed since its last successful apply as its apply identity, in layer order
(0-bootstrap, 1-org, 2-shr and 2-spn, then 2-env for tst, stg and prd, then 2-net), one at a time, every one of them initialized before the
first applies (a layer that has never applied gets its empty state written by
its own init, which a later layer may then read), stopping at the first
failure.

A pull request may add an output to one layer and read it in a later one, as
a bedrock upgrade does when `2-env` starts reading a new output of `1-org`.
The later layer's plan cannot see the output yet: it exists once the earlier
layer is applied, so that plan fails with tofu's "Unsupported attribute"
error. The plan trigger plans the layers side by side, then judges each plan once all of
them have finished: a plan that fails only because it reads outputs the plan
of an earlier layer in the same pull request creates passes, and its comment
on the pull request says it is "planned after" that layer applies. The merge
applies the earlier layer first, so the later one reads the new output when
its turn comes. A layer that has never applied (no state object under its
prefix yet, as on a new organization's first pull request) is not planned at
all and passes the same way, planned after its first apply, which the merge
makes. Any other failure fails the pull request.

The steps a run takes are the trigger's own and never this repository's: a
pull request changes what the steps read, never what they do. A run fetches
the bedrock `placement.json` pins (`bedrockVersion`, moved by `bedrock
upgrade`), checks the committed layers against the placement (`bedrock org
check`, which also refuses a `.tf` file no render produces and any override
file) and only then plans or applies. No key exists anywhere: the trigger's
runner identity impersonates each layer's own identity for that layer's
tofu, the planner the plan identities and the applier the apply identities.
`gcloud builds triggers run` on the apply trigger applies one layer again
with no change to it, which a registration's second pass of `2-env` needs
(`0-bootstrap/README.md`, "The Cloud Build triggers", which also says how a
change to the steps themselves lands one apply later). Recovery is by hand (`0-bootstrap/README.md`, "Recovery, by hand"):
for `2-env` as a member of the environment's team group under the
layer administrator and layer state entitlements, which grant the
environment layer identity's roles for a short time, so the person runs the
layer as themselves and the audit log names them; for the other layers as
the bootstrap administrator. Destroying the organization, projects and all,
is `bedrock org destroy`, by hand (`0-bootstrap/README.md`, "Destroying the
organization").

First-time setup, by hand, before any identity can run. Each step opens with
the place it happens.

1. **The team groups.** In the Workspace Admin console: one group per
   environment, named in `placement.json` (`teamGroups`, a group's address
   each; the same group may serve several environments, but production's is
   not the first environment's). Each person's access to an environment comes
   from its group: the group approves the environment's releases where a
   release waits for an approval, and its members ask for its entitlements
   (`2-env/README.md`, "The team group"). Every render names them, and `2-env`
   grants to them, so they exist before anything else.
2. **GitHub.** On GitHub, as an owner of the `imp-example` organization
   (whoever creates it is its first owner): the organization itself, its
   machine account `imp-machine` (`githubMachineAccount`, a GitHub
   user that is an owner of the organization and acts for no person), this
   repository (`1-org` rules its default branch; nothing else of it is the
   layers'), and the release and deployer apps ("The two GitHub Apps",
   below). The release app's App ID and slug are in `placement.json` before
   the first render. Nothing more is done by hand on GitHub: `1-org`
   configures every application's repository and rules this repository's
   default branch.
3. **Seed.** In a terminal, as the bootstrap administrator: `bedrock org
   preflight` first, which checks that the person holds each of these roles:
   - Folder Creator (`roles/resourcemanager.folderCreator`), on the organization
   - Project Creator (`roles/resourcemanager.projectCreator`), on the organization
   - Organization Administrator (`roles/resourcemanager.organizationAdmin`), on the organization
   - Organization Policy Administrator (`roles/orgpolicy.policyAdmin`), on the organization
   - Organization Role Administrator (`roles/iam.organizationRoleAdmin`), on the organization
   - Tag Administrator (`roles/resourcemanager.tagAdmin`), on the organization
   - Billing Account User (`roles/billing.user`), on the billing account

   Then the gcloud commands that make the `terraform` folder, the boot
   project, the boot layer identity and its org-level roles, and the state
   bucket. Both are in [`0-bootstrap/README.md`](0-bootstrap/README.md), "1.
   Seed with gcloud". In a terminal, a billing administrator separately
   grants `roles/billing.user` on the billing account to the two layer
   identities; in the Billing console, the same administrator sets the spend
   budget (`0-bootstrap/README.md`, "5. Billing grants and the budget").
4. **The Cloud Build GitHub authorization.** In the boot project's Cloud
   Build console, as the bootstrap administrator (Owner on the boot project
   from the seed), signed in to GitHub as the machine account
   `imp-machine`, before the first apply of `0-bootstrap`
   (`0-bootstrap/README.md`, "2. The Cloud Build GitHub authorization"): the
   host connection, which installs the Google Cloud Build GitHub App on the
   organization and stores its token in the boot project. Its installation
   ID and the token secret's version go into `placement.json`
   (`cloudBuildInstallationId`, `cloudBuildTokenSecretVersion`), where every
   connection, the boot project's and each environment's, is rendered from.
   The bedrock the triggers run goes in beside them: `bedrock upgrade`, in
   this repository, which records the release as `bedrockVersion` and
   `bedrockSha256` and renders.
5. **`0-bootstrap`.** In a terminal, in `0-bootstrap/`, as the bootstrap
   administrator. The first apply runs on local state, with the backend block
   commented out, and adopts the seeded resources and the console's connection
   through `import` blocks and an import. The seed's values go into
   `placement.json` (`stateBucket`, `projects.boot`, `projectNumbers.boot`)
   and `bedrock org render` names the bucket in every backend block; the
   state is then moved in with `tofu init -migrate-state`. From here the plan
   and apply triggers exist, and their steps are the trigger's own.
6. **`1-org`.** In a terminal, in `1-org/`, as the same person, against the
   bucket directly, with their own GitHub token. Its `project_ids` and
   `project_numbers` outputs go into `placement.json` (`projects`,
   `projectNumbers`), and `bedrock org render` names every layer's identities
   and grants each environment's Cloud Build service agent the token's read.
   From here on the apply trigger applies every layer, these two included.
   The creator's grants that steps 3 to 6 leave behind (Owner on each project
   made, Folder Admin and Folder Editor on each folder, which Google gives
   whoever creates them) are temporary: once the apply trigger applies the
   layers, a hand step removes them (`1-org/README.md`, "Applying"), and
   `bedrock org check` lists each person still holding `roles/owner` on an
   environment project until then.
7. **The infrastructure GitHub App** (`0-bootstrap/README.md`, "The
   infrastructure GitHub App"). On GitHub, as an organization owner: the app,
   its private key and its installation. Then in a terminal, in this
   repository: its App ID recorded in `placement.json`
   (`githubInfrastructureAppId`), and its key added to the boot project's
   container and pinned with `bedrock secret add github-infrastructure-key`
   and `bedrock secret pin github-infrastructure-key <version>`, which records
   the version and renders. Until then a run of the triggers that plans or
   applies `1-org` says so and stops.
8. **The deployer app's key** (`0-bootstrap/README.md`, "The deployer GitHub
   App's key"). In a terminal, in this repository, once `0-bootstrap` has
   applied: its App ID recorded in `placement.json` (`githubDeployerAppId`),
   `bedrock secret add github-deployer-key` and `bedrock secret pin
   github-deployer-key <version>` ("The two GitHub Apps", below). Until then
   the plans are in the runs' logs alone and the pipelines talk back through
   nothing. `bedrock org register` refuses the first application while step
   4's values are not recorded, since the applications' triggers exist once
   `2-env` holds the connection and nothing is built by hand before them.
9. **The consent screen.** In each environment project's Google Cloud
   console, before the first application's OAuth client is made there: APIs &
   Services, OAuth consent screen, with the audience **Internal**, so only the
   organization's own users sign in. The project has one consent screen, which
   every application's client in it shares, so it carries the organization's
   name, not an application's. Each application's README ("Hand steps") then
   makes its client.
10. **The role groups.** In the Workspace Admin console, for each application
    before its first sign-in: one group per role its sign-in grants, named
    `<group prefix><role>@imp.example`, where the group prefix is the
    application's own (its stack's `<sign-in>_oidc_group_prefix` variable,
    under "Inputs" in its infrastructure README). When the group is
    created, set "Who can view members" so that the group's members can: the
    sign-in reads a person's groups with the person's own token, and Google
    leaves out a group whose member list the person may not view, so that
    membership, and the role it grants, is lost.

GitHub prerequisites: the organization's private repositories need rulesets
that GitHub enforces, environments and organization secrets; the organization
holds GitHub Apps; and its machine account (`githubMachineAccount`) is an
owner of it. bedrock's README lists each with what needs it
([GitHub prerequisites](https://github.com/cccteam/ccc/blob/master/bedrock/README.md#github-prerequisites)).

`bedrock org register <app> [<application checkout>]` prints the pull requests
a registration takes, and writes the application's first `placement.json`
into its checkout (`infrastructure/placement.json`), or prints it. The
registration goes in steps, since the grants on an application's identities
need `2-env` to have created them, and its hostnames and its repository's
required check need its stack's backends and check trigger: `placement.json`'s
`registration` records the step each
application has reached (`identities`, then `grants`; none once complete),
the layers render what that step may apply, and `bedrock org register <app>
--next` moves to the next step once the apply before it has happened. Every
pull request of the sequence carries every rendered file, and `bedrock org
check` passes on each. The completion records the hostnames the stack serves,
read from its state in every environment (`placement.json`'s `hostnames`):
the application declares each site's label in its own placement (its code by
default), `2-net`'s host rules render from the record, and a hostname another
application serves is refused. After a label change, `bedrock org hostnames
<app>` records them again.

Each layer's `terraform.tfvars` is committed. It holds IDs, not secrets, and
the values a layer runs with belong in the repository.

## Tearing the organization down

`bedrock org teardown` takes the applications out of service and keeps the
foundation: the projects, the folders, the identities, the roles and
policies, the state, the triggers, the Spanner instances, the registry, the
connection and the keys stay, and so does every application's data. It runs
twice. The first run sets `teardown` in `placement.json` and renders `2-net`
with no host and `serving = false` (`2-net/applications.auto.tfvars`), writes
the journal line, and leaves the working tree for a pull request: its merge
has the apply trigger take the hosts and the load balancer's listeners out of
`2-net`, while the address, the certificate and its DNS authorization stay.
The second run, once `2-net` routes nothing, tears every registered
application down to what it keeps (`bedrock teardown --all`, each environment
in promotion order, then the pull-request stacks) when the applications'
stacks live under `3-app/`, or, when each application carries its own stack,
says which still stand and the command to run from each checkout, and writes
the journal line once none does. The inverse is the same loop the other way:
each stack applied by hand, `teardown` removed from `placement.json` on a pull
request (a host names a backend service that must exist), then
`bedrock rerun <env> <release>` per environment.

One application goes out of service on its own, the others untouched:
`bedrock org teardown <app>` (or `<app> <env>...` for some of its environments)
records the environments under `outOfService` in `placement.json`, renders
`2-net` without that application's hostnames there while the listeners and
every other application's hosts stay, writes the journal line, and leaves the
working tree for a pull request; once the merge's apply has `2-net` routing
nothing to it, `bedrock teardown` or `bedrock destroy` runs from the
application's checkout. The way back is the operations workflow's `apply`
action and `bedrock rerun` per environment, then `bedrock org serve <app>`
(or `<app> <env>...`), which refuses an environment whose stack is still torn
down, removes the entry, renders the hostnames back and leaves the pull
request the same way.

## The two GitHub Apps

Besides the infrastructure app `1-org` acts as (`0-bootstrap/README.md`), the
organization holds two GitHub Apps that every application uses: the release
app, which cuts each release (release-please runs as it from each
application's release trigger in Cloud Build, so the tag and the GitHub
Release carry it as their author, and each repository's tag ruleset lets it
alone make a release tag), and the deployer app, which the pipeline talks
back on a pull request as (a deployment carrying the environment's URL, a
comment, the guard's refusals). Both keys live in Secret Manager in the boot
project, and neither reaches GitHub. Once, by a person:

1. On GitHub, as an owner of the `imp-example` organization: the
   organization's Settings, Developer settings, GitHub Apps, New GitHub App,
   once for each app. Give it any homepage URL, turn Webhook off, and choose
   only this account under Where can this app be installed. Repository
   permissions, with Metadata (read), which GitHub requires of every app:
   - the release app: Contents, Issues and Pull requests, each read and
     write;
   - the deployer app: Checks, Deployments, Issues and Pull requests, each
     read and write.

   Create it and note the App ID on its settings page.
2. On GitHub, on each app's settings page: Install App, on the organization,
   for all repositories, so a new application's repository is covered with no
   step of its own. Then Private keys, Generate a private key: the browser
   downloads a `.pem` file.
3. In `placement.json`: the release app's App ID as `githubReleaseAppId` and
   its slug, the name in its address `github.com/apps/<slug>`, as
   `githubReleaseAppSlug`; then `bedrock org render`. `1-org` names the app in
   each repository's tag ruleset, and every application's placement names the
   slug as the author of its releases.
4. In `placement.json`: the deployer app's App ID as `githubDeployerAppId`,
   one value for the organization.
5. In a terminal, in this repository, once `0-bootstrap` has applied and made
   the keys' containers, as the bootstrap administrator, for each of the two
   apps:

   ```bash
   bedrock secret add github-deployer-key --from-file <the deployer app's .pem file>
   bedrock secret pin github-deployer-key <version>
   bedrock secret add github-release-key --from-file <the release app's .pem file>
   bedrock secret pin github-release-key <version>
   ```

   `add` puts the key into its container in the boot project
   (`imp-boot-gbl-github-deployer-key`, `imp-boot-gbl-github-release-key`) and prints the version;
   `pin` checks that it is enabled, records it in `placement.json`
   (`githubDeployerKeyVersion`, `githubReleaseKeyVersion`) and renders. The
   pull request that carries them applies `2-env` in every environment, whose
   outputs name the versions, and each application's stack passes the keys
   to its pipeline and its release trigger at its next apply; the plan
   trigger posts the layers' plans as the deployer app from then on, and the
   release trigger exists from then on. Delete both `.pem` files once they
   are pinned. Until the values are recorded, the pipelines talk back through
   nothing, the plans are in the runs' logs alone, and no push cuts a
   release.

## Deviations from tf-gcp-setup

Decided before writing:

1. **No Scalr.** No `scalr` provider, workspaces, tags, or provider
   configurations. "Workspaces" become layers and their service accounts are
   layer identities. The runner is a pair of Cloud Build triggers in the boot
   project, whose steps live in the trigger and not in this repository, each
   impersonating the layer's service account for its tofu; no GitHub workflow
   runs a layer. The service accounts and their role grants are kept.
2. **GCS backend.** `backend "gcs"` in every layer, one bucket
   (`imp-boot-gbl-state-<suffix>`, created by the seed) with a prefix per
   layer. The bucket name is substituted into the backend blocks once.
   `0-bootstrap` is applied with local state first and migrated.
3. **Organization values.** Domain imp.example, organization 123456789012,
   prefix `imp`, billing account 012345-6789AB-CDEF01, regions us-central1
   and us-west3, contact domain `@imp.example`,
   no Essential Contact emails yet.
4. **Billing.** The boot layer identity gets `roles/billing.user`, not
   `roles/billing.admin`. The org layer identity gets `roles/billing.user` as in
   the model.
5. **Central logging off.** No log project, folder sinks, or audit-log bucket.
   `central_logging` in `1-org` gates all of it; the README there says what
   turning it on needs.
6. **Projects.** `shr`, `net`, `spn` under `shared`; `tst`, `stg`, `prd` each
   in a folder of its own name; every folder under the organization's folder
   `imp`, beside `terraform`.
7. **Role sets.** `roles/secretmanager.admin` is replaced by the custom
   organization role `secretContainerAdmin`, which manages secrets and their
   versions' lifecycle but can neither add nor read a version payload. The
   `app` set adds Spanner, Cloud Scheduler, Cloud Tasks, and Cloud Build roles;
   new `net` and `spn` sets. Each project also gets a read-only plan identity.
8. **API sets.** The `app` set adds Spanner, Cloud Scheduler, Cloud Tasks,
   Cloud Build, and Artifact Registry; new `net` and `spn` sets.
9. **Org policies as a variable.** The baseline is `var.org_policies`, a list
   of constraint definitions, trimmed to constraints that cost nothing
   operationally here. The `1-org` README lists what was dropped.
10. **Label.** `bedrock-lab = "true"` on every project and bucket.

Consequences of the above, not separately decided:

- Variables that existed in `0-bootstrap` only to be forwarded to the org
  workspace (`gcp_region`, `allowed_contact_domains`,
  `essential_contact_emails`) are gone from that layer; `1-org` reads them
  from its own `terraform.tfvars` and defaults.
- `roles/iam.workloadIdentityPoolAdmin` is dropped from both layer
  identities: the triggers need no pool, and the application identities'
  pools are 2-env's. `roles/cloudbuild.builds.editor` and
  `roles/cloudbuild.connectionAdmin` go to the boot layer identity, which
  makes the connection and the triggers. `roles/logging.configWriter` and
  `roles/storage.admin` are dropped from the org layer identity (no central
  logging). `roles/iam.organizationRoleAdmin` is added to it, because `1-org`
  creates custom organization roles, and `roles/resourcemanager.tagAdmin`,
  because it owns the public-invoker tag and the grants on its value.
- The billing grants are gated by `manage_billing_iam`, false here:
  the bootstrap administrator is a Billing Account User on the account and cannot set its
  IAM policy. See the `0-bootstrap` README.
- The state bucket is the one seeded resource left outside OpenTofu, for the
  reason the model leaves its Scalr environment unmanaged: a layer should not
  own the container of its own state.

## Contributing

1. Branch from `master`.
2. Follow the naming convention.
3. Run `tofu fmt -recursive` and `tofu validate` before committing.
4. Open a pull request describing the change. The plan trigger posts each
   touched layer's plan on it; the merge applies.

A few things resist deletion on purpose. Projects carry
`deletion_policy = "PREVENT"` and a lien, and folders carry
`deletion_protection`, so removing either takes a deliberate two-step change;
a project's removal starts with removing its lien (`1-org/README.md`,
"Removing a project"). Custom role IDs cannot be
reused for a period after deletion, so rename rather than delete and recreate.
