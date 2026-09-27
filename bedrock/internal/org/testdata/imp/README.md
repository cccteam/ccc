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
in the boot project, and the layers run from Cloud Build in that project as
their own service accounts once the runner is wired, and by hand until then.

The running log is [JOURNAL.md](JOURNAL.md). Every step that changes the organization
gets a line there first.

## Layers

| Layer | Directory | Applied as | Creates |
|---|---|---|---|
| 0 | [`0-bootstrap/`](0-bootstrap/) | the bootstrap administrator, local state then migrated | adopts the seeded `terraform` folder, boot project, and boot identity; the org layer identity and its org-level grants; the project-updater custom role |
| 1 | [`1-org/`](1-org/) | the bootstrap administrator for now, later Cloud Build as `imp-org-gbl-tofu` | folders `shared`, `tst`, `stg`, `prd`; the org policy baseline; Essential Contacts; the six projects; a layer identity and a plan identity per project; the `secretContainerAdmin` custom role |
| 2 | [`2-shr/`](2-shr/) | the bootstrap administrator for now, later Cloud Build as `imp-shr-gbl-tofu` | one Artifact Registry Docker repository per application, immutable tags, cleanup policies; reader for each environment's Cloud Run, writer for each application's deploy identities |
| 2 | [`2-spn/`](2-spn/) | the bootstrap administrator for now, later Cloud Build as `imp-spn-gbl-tofu` | the shared Spanner instance `imp-spn-gbl-spanner` (nam10, 100 processing units, capped at 200); instance-level database admin grants |
| 2 | [`2-net/`](2-net/) | the bootstrap administrator for now, later Cloud Build as `imp-net-gbl-tofu` | the global external Application Load Balancer, its address, SSL policy, managed certificate and map, the apps domain zone, and a URL map routed from `hosts`; no VPC |
| 2 | [`2-env/`](2-env/) | `imp-<env>-gbl-tofu`, once per environment (`-var environment=`) | what every application in an environment shares: the tst Spanner instance, the Cloud Build GitHub connection and repository links, the deployment-record bucket, the per-application apply and deploy identities with their grants |
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

1. **Seed**, by hand with gcloud, as the bootstrap administrator: the `terraform` folder, the
   boot project, the boot layer identity and its org-level roles, and the state
   bucket. The commands are in [`0-bootstrap/README.md`](0-bootstrap/README.md).
   A billing administrator separately grants `roles/billing.user` on the
   billing account to the two layer identities.
2. **`0-bootstrap`**, by the bootstrap administrator. The first apply runs on local state,
   with the backend block commented out, and adopts the seeded resources
   through `import` blocks. The bucket name is then substituted into both
   layers' backend blocks and the state is moved in with
   `tofu init -migrate-state`.
3. **`1-org`**, by the same user for now, against the bucket directly. Later a
   Cloud Build trigger in the boot project runs it as `imp-org-gbl-tofu`, with
   `imp-org-gbl-plan`-style read-only planning on pull requests. That wiring is
   a later change; nothing in the layers depends on it.

Each layer's `terraform.tfvars` is committed. It holds IDs, not secrets, and
the values a layer runs with belong in the repository.

## Deviations from tf-gcp-setup

Decided before writing:

1. **No Scalr.** No `scalr` provider, workspaces, tags, or provider
   configurations. "Workspaces" become layers and their service accounts are
   layer identities. No Workload Identity Federation pools or providers: the
   runner is Cloud Build in the boot project, executing directly as the layer
   service account. The service accounts and their role grants are kept.
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
- `roles/iam.workloadIdentityPoolAdmin` is dropped from both layer identities'
  org roles (no WIF). `roles/logging.configWriter` and `roles/storage.admin`
  are dropped from the org layer identity (no central logging).
  `roles/iam.organizationRoleAdmin` is added to it, because `1-org` now creates
  a custom organization role.
- The billing grants are gated by `manage_billing_iam`, false here:
  the bootstrap administrator is a Billing Account User on the account and cannot set its
  IAM policy. See the `0-bootstrap` README.
- The state bucket is the one seeded resource left outside OpenTofu, for the
  reason the model leaves its Scalr environment unmanaged: a layer should not
  own the container of its own state.

## Contributing

1. Branch from `main`.
2. Follow the naming convention.
3. Run `tofu fmt -recursive` and `tofu validate` before committing.
4. Open a pull request describing the change.

A few things resist deletion on purpose. Projects carry
`deletion_policy = "PREVENT"` and folders carry `deletion_protection`, so
removing either takes a deliberate two-step change. Custom role IDs cannot be
reused for a period after deletion, so rename rather than delete and recreate.
