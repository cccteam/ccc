# imp-impulse-infrastructure

OpenTofu configurations for the impulseframework.com Google Cloud organization,
rendered by bedrock. The name says whose and what: `imp` is the
organization prefix every resource here carries, and this is the Impulse
infrastructure, one repository per (organization, kind), so a GitHub
organization can hold infrastructure for more than one Google Cloud
organization without the names colliding. Modeled on
[cccteam/tf-gcp-setup](https://github.com/cccteam/tf-gcp-setup): numbered
layers, one identity per layer, no static credentials anywhere. The difference
is the control plane. There is no Scalr; state lives in a Cloud Storage bucket
in the boot project, and the layers run from a GitHub workflow in this
repository as their own service accounts, signed in with no key: a pull
request plans, its merge applies.

The running log is [JOURNAL.md](JOURNAL.md). Every step that changes the organization
gets a line there first.

## Layers

| Layer | Directory | Applied as | Creates |
|---|---|---|---|
| 0 | [`0-bootstrap/`](0-bootstrap/) | the bootstrap administrator the first time (local state, then migrated); then the workflow as `imp-boot-gbl-tofu` | adopts the seeded `terraform` folder, boot project, and boot identity; the org layer identity and its org-level grants; the project-updater and state-bucket-policy custom roles; the workflow's identity pool and provider; the state bucket's grants for its two identities |
| 1 | [`1-org/`](1-org/) | the bootstrap administrator the first time; then the workflow as `imp-org-gbl-tofu` | folders `shared`, `tst`, `stg`, `prd`; the org policy baseline; Essential Contacts; the six projects; a layer identity and a plan identity per project and the two boot layers' plan identities, each with its federation binding and state bucket grants; the `secretContainerAdmin` custom role; the applications' repositories |
| 2 | [`2-shr/`](2-shr/) | the workflow as `imp-shr-gbl-tofu` | one Artifact Registry Docker repository per application, immutable tags, cleanup policies; reader for each environment's Cloud Run, writer for each application's deploy identities |
| 2 | [`2-spn/`](2-spn/) | the workflow as `imp-spn-gbl-tofu` | the shared Spanner instance `imp-spn-gbl-spanner` (nam10, 100 processing units, capped at 200); instance-level database admin grants; the Spanner entitlements of the environments on it |
| 2 | [`2-net/`](2-net/) | the workflow as `imp-net-gbl-tofu` | the global external Application Load Balancer, its address, SSL policy, managed certificate and map, the apps domain zone, and a URL map routed from `hosts`; no VPC |
| 2 | [`2-env/`](2-env/) | the workflow as `imp-<env>-gbl-tofu`, once per environment (`-var environment=`) | what every application in an environment shares: the tst Spanner instance, the Cloud Build GitHub connection and repository links, the deployment-record bucket, the team group's release approval and entitlements, the per-application apply and deploy identities with their grants |
| 3 | the application's own repository, `infrastructure/` (state slot `3-app/<app>/<env>` in this organization's bucket) | `imp-<env>-gbl-<app>-tofu`, once per environment | the application's stack, derived from its code by bedrock: runtime identities, database, secret containers, Cloud Run services and job, load balancer backend, Cloud Build triggers; harbor's is at impulseframework/harbor |

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
random four-hex suffix. Every project and bucket carries the label
`bedrock-lab = "true"`.

## How the layers are applied

Routine changes go through pull requests. The workflow
[`.github/workflows/layers.yml`](.github/workflows/layers.yml), rendered by bedrock like the layers,
plans every layer a pull request changes as that layer's plan identity and
posts the plan on the pull request; the merge applies those layers as their
apply identities, in layer order (0-bootstrap, 1-org, 2-shr, 2-spn and 2-net, then 2-env for tst, stg and prd), one at a time,
stopping at the first failure. No key exists anywhere: a run presents
GitHub's short-lived token to the boot project's workload identity pool
(`0-bootstrap/github.tf`), and Google answers with a token for the identity;
a plan identity may be used from a pull request's run and an apply identity
from the default branch alone. Run workflow on the Actions tab applies one
layer again with no change to it, which a registration's second pass of
`2-env` needs. Recovery is by hand (`0-bootstrap/README.md`, "Recovery, by
hand"): for `2-env` under the Layer administrator entitlement, acting as the
environment's apply identity through `GOOGLE_IMPERSONATE_SERVICE_ACCOUNT`;
for the other layers as the bootstrap administrator.

First-time setup, by hand, before any identity can run:

1. **The team groups**, in the Workspace Admin console: one group per
   environment, named in `placement.json` (`teamGroups`, a group's address
   each; the same group may serve several environments, but production's is
   not the first environment's). Each person's access to an environment comes
   from its group: the group approves the environment's releases where a
   release waits for an approval, and its members ask for its entitlements
   (`2-env/README.md`, "The team group"). Every render names them, and `2-env`
   grants to them, so they exist before anything else.
2. **Seed**, by hand with gcloud, as the bootstrap administrator: the `terraform` folder, the
   boot project, the boot layer identity and its org-level roles, and the state
   bucket. The commands are in [`0-bootstrap/README.md`](0-bootstrap/README.md).
   A billing administrator separately grants `roles/billing.user` on the
   billing account to the two layer identities.
3. **`0-bootstrap`**, by the bootstrap administrator. The first apply runs on local state,
   with the backend block commented out, and adopts the seeded resources
   through `import` blocks. The seed's values go into `placement.json`
   (`stateBucket`, `projects.boot`, `projectNumbers.boot`) and `bedrock org
   render` names the bucket in every backend block; the state is then moved
   in with `tofu init -migrate-state`.
4. **`1-org`**, by the same person, against the bucket directly, with their
   own GitHub token. Its `project_ids` and `project_numbers` outputs go into
   `placement.json` (`projects`, `projectNumbers`), and `bedrock org render`
   names every layer's identities in the workflow. From here on the workflow
   applies every layer, these two included.
5. **The infrastructure GitHub App** (`0-bootstrap/README.md`, "The
   infrastructure GitHub App"): created on GitHub, its key added to the boot
   project's container, its App ID and the key's version recorded in
   `placement.json`. Until then a run of `1-org` through the workflow says so
   and stops.
6. **The Cloud Build GitHub App's browser authorization**, in the tst
   project's console, before the first application is registered
   (`2-env/README.md`, "The GitHub token, once").

`bedrock org register <app>` prints the pull requests a registration takes.

Each layer's `terraform.tfvars` is committed. It holds IDs, not secrets, and
the values a layer runs with belong in the repository.

## Deviations from tf-gcp-setup

Decided before writing:

1. **No Scalr.** No `scalr` provider, workspaces, tags, or provider
   configurations. "Workspaces" become layers and their service accounts are
   layer identities. The runner is a GitHub workflow in this repository,
   signed in through one Workload Identity Federation pool in the boot
   project as the layer's service account; no Cloud Build trigger runs a
   layer. The service accounts and their role grants are kept.
2. **GCS backend.** `backend "gcs"` in every layer, one bucket
   (`imp-boot-gbl-state-<suffix>`, created by the seed) with a prefix per
   layer. The bucket name is substituted into the backend blocks once.
   `0-bootstrap` is applied with local state first and migrated.
3. **Organization values.** Domain impulseframework.com, organization 123456789012,
   prefix `imp`, billing account 012345-6789AB-CDEF01, regions us-central1
   and us-west3, contact domains `@impulseframework.com` and `@cloud-team.com`,
   no Essential Contact emails yet.
4. **Billing.** The boot layer identity gets `roles/billing.user`, not
   `roles/billing.admin`. The org layer identity gets `roles/billing.user` as in
   the model.
5. **Central logging off.** No log project, folder sinks, or audit-log bucket.
   `central_logging` in `1-org` gates all of it; the README there says what
   turning it on needs.
6. **Projects.** `shr`, `net`, `spn` under `shared`; `tst`, `stg`, `prd` each
   in a folder of its own name.
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
- `roles/iam.workloadIdentityPoolAdmin` stays with the boot layer identity,
  which makes the workflow's pool, and is dropped from the org layer
  identity, which makes none. `roles/logging.configWriter` and
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
4. Open a pull request describing the change. The workflow posts each touched
   layer's plan on it; the merge applies.

A few things resist deletion on purpose. Projects carry
`deletion_policy = "PREVENT"` and folders carry `deletion_protection`, so
removing either takes a deliberate two-step change. Custom role IDs cannot be
reused for a period after deletion, so rename rather than delete and recreate.
