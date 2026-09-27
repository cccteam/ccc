# 0-bootstrap

The chicken-and-egg layer. It adopts the resources the seed step created by
hand, creates the org layer identity, and grants both identities what they
need at the org node. It is the one layer a human applies.

## What it creates

- Adopts, through `import` blocks: the `terraform` folder at the org root, the
  boot project `imp-boot-gbl-core-<suffix>` inside it, and the boot layer
  identity `imp-boot-gbl-tofu`. From the first apply on they are ordinary
  managed resources.
- The boot project's API set (billing, Cloud Build, resource manager,
  Essential Contacts, IAM, IAM credentials, logging, org policy, service usage,
  storage), its labels, and the deletion of its default compute service
  account.
- Org-level role bindings for the boot layer identity (`var.boot_layer_roles`)
  and for the org layer identity (`var.org_layer_roles`).
- The org layer identity `imp-org-gbl-tofu`, in the boot project.
- The custom organization role `impGblOrgProjectUpdater`
  (`resourcemanager.projects.update`), granted to the org layer identity.
- `roles/billing.user` on the billing account for both identities, only when
  `manage_billing_iam` is true. See step 4.

Not created here, on purpose: the state bucket. It is seeded and left outside
OpenTofu because this layer's own state lives in it.

## First-time setup

Steps 1 and 4 happen once, by hand. Everything step 1 creates is adopted by an
`import` block in step 2, so nothing stays outside OpenTofu except the bucket
and, while `manage_billing_iam` is false, the two billing grants.

### 1. Seed with gcloud

In a terminal, as the bootstrap administrator (`bedrock@impulseframework.com`), who holds
Organization Administrator, Project Creator, and Organization Policy
Administrator on the organization and Billing Account User on the billing
account:

```bash
gcloud auth login
gcloud auth application-default login

export ORG_ID=123456789012
export BILLING_ACCOUNT_ID=012345-6789AB-CDEF01
export PREFIX=imp
export REGION=us-central1
```

Create the `terraform` folder at the org root:

```bash
gcloud resource-manager folders create \
  --display-name=terraform \
  --organization="${ORG_ID}"

export TERRAFORM_FOLDER_ID=$(gcloud resource-manager folders list \
  --organization="${ORG_ID}" --filter="displayName=terraform" \
  --format='value(name)' | sed 's|folders/||')
```

Create the boot project. The four-hex suffix is what the project factory would
have generated, and the ID can never be changed afterwards:

```bash
export BOOT_PROJECT_ID="${PREFIX}-boot-gbl-core-$(od -An -tx1 -N2 /dev/urandom | tr -d ' \n')"

gcloud projects create "${BOOT_PROJECT_ID}" \
  --folder="${TERRAFORM_FOLDER_ID}" \
  --labels=terraform=true,terraform_source_path=0-bootstrap,source_repo=imp-impulse-infrastructure,environment=boot,bedrock-lab=true

gcloud billing projects link "${BOOT_PROJECT_ID}" --billing-account="${BILLING_ACCOUNT_ID}"

gcloud services enable \
  cloudbilling.googleapis.com cloudresourcemanager.googleapis.com \
  iam.googleapis.com iamcredentials.googleapis.com \
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

Create the boot layer identity and grant it the org roles. This list is the
same one in `var.boot_layer_roles`; change both together:

```bash
export SA_EMAIL="${PREFIX}-boot-gbl-tofu@${BOOT_PROJECT_ID}.iam.gserviceaccount.com"

gcloud iam service-accounts create "${PREFIX}-boot-gbl-tofu" \
  --project="${BOOT_PROJECT_ID}" \
  --display-name="OpenTofu SA - ${PREFIX}-boot-gbl" \
  --description="Layer identity for ${PREFIX}-boot-gbl. Applies the 0-bootstrap layer from Cloud Build in this project."

for role in \
  roles/iam.organizationRoleAdmin \
  roles/iam.serviceAccountAdmin \
  roles/resourcemanager.folderAdmin \
  roles/resourcemanager.organizationAdmin \
  roles/resourcemanager.projectCreator \
  roles/resourcemanager.projectDeleter \
  roles/resourcemanager.projectIamAdmin \
  roles/serviceusage.serviceUsageAdmin
do
  gcloud organizations add-iam-policy-binding "${ORG_ID}" \
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
  --update-labels=terraform=true,terraform_source_path=0-bootstrap,source_repo=imp-impulse-infrastructure,environment=boot,bedrock-lab=true
```

Keep `BOOT_PROJECT_ID`, `TERRAFORM_FOLDER_ID`, and `STATE_BUCKET`. Steps 2 and
3 need them.

### 2. Apply with local state

In a terminal, in this directory. Fill in `terraform.tfvars` first:
`boot_project_id` and `terraform_folder_id` from step 1.

The first apply runs on local state, so the `backend "gcs"` block in
`initialize.tf` has to be commented out for it. A configuration that declares
a backend refuses to plan until that backend is initialised, and the bucket
name is not in the block yet.

```bash
cd 0-bootstrap
# comment out the backend "gcs" { ... } block in initialize.tf
tofu init
tofu plan
```

The plan should show the folder, project, and boot service account as imports
rather than creates. If any of them shows as a create, the ID in
`boot_project_id` or `terraform_folder_id` is wrong. Stop and fix it before
applying; creating a second boot project is a mess to unwind.

```bash
tofu apply
```

This writes `terraform.tfstate` in the directory (ignored by git). `init` also
writes `.terraform.lock.hcl`, which is committed: it cannot be generated
without an `init`, so this first one produces it.

### 3. Migrate the state into the bucket

Restore the `backend "gcs"` block and substitute the bucket name from step 1
for `imp-boot-gbl-state-REPLACEME`, here and in `1-org/initialize.tf`. Then:

```bash
tofu init -migrate-state
```

Answer yes to copying the local state into the bucket, confirm with
`tofu plan` (no changes), and delete the local `terraform.tfstate` and its
backup. From here on every run inits against the bucket directly.

Record `boot_project_id` in `1-org/terraform.tfvars` and add a line to
`JOURNAL.md`.

### 4. Billing grants, by a billing administrator

The bootstrap administrator holds Billing Account User on the billing account. That role
links projects to the account but cannot change the account's IAM policy, so
the two `google_billing_account_iam_member` resources in this layer cannot be
applied as the bootstrap administrator. `manage_billing_iam = false` in `terraform.tfvars`
skips them. Instead, in a terminal, a Billing Account Administrator makes
the grants by hand once step 2 has created the org layer identity:

```bash
export BILLING_ACCOUNT_ID=012345-6789AB-CDEF01
export BOOT_PROJECT_ID=<from step 1>

for sa in imp-boot-gbl-tofu imp-org-gbl-tofu; do
  gcloud billing accounts add-iam-policy-binding "${BILLING_ACCOUNT_ID}" \
    --member="serviceAccount:${sa}@${BOOT_PROJECT_ID}.iam.gserviceaccount.com" \
    --role=roles/billing.user
done
```

Nothing depends on these grants until Cloud Build runs a layer as its identity;
the bootstrap administrator's own Billing Account User role covers project creation until
then. Set `manage_billing_iam = true` once the identity applying this layer can
manage billing IAM; the resources then assert the same two members.

## Inputs

| Name | Description | Type | Default | Required |
|---|---|---|---|:---:|
| `billing_account_id` | Billing account ID to link projects to. | `string` | n/a | yes |
| `boot_layer_roles` | Org-level roles granted to the boot layer identity. The seed grants the same list; change both together. | `list(string)` | eight roles, see `variables.tf` | no |
| `boot_project_id` | Project ID of the seeded boot project, suffix included. Adopted, not created. | `string` | n/a | yes |
| `manage_billing_iam` | Whether this layer manages the two `roles/billing.user` grants. Needs Billing Account Administrator to apply. | `bool` | `true` | no |
| `org_layer_roles` | Org-level roles granted to the org layer identity. | `list(string)` | ten roles, see `variables.tf` | no |
| `organization_domain` | Domain of the GCP organization. | `string` | n/a | yes |
| `prefix` | Short org-wide prefix used in resource names. | `string` | `"imp"` | no |
| `terraform_folder_id` | Numeric ID of the seeded `terraform` folder, without `folders/`. Adopted, not created. | `string` | n/a | yes |

`terraform.tfvars` sets the required ones and `manage_billing_iam = false`.

## Outputs

| Name | Description |
|---|---|
| `boot_project_id` | The boot project, quota and billing project for every downstream layer. |
| `boot_service_account_email` | The boot layer identity. |
| `org_id` | Numeric organization ID. |
| `org_service_account_email` | The org layer identity. |
| `terraform_folder_id` | The folder holding the automation plane. |

## Not yet wired: the Cloud Build runner

The layer identities exist and hold their roles, but nothing runs as them yet.
Wiring the runner is a later change and needs, per layer:

- A Cloud Build trigger in the boot project with `service_account` set to the
  layer identity, connected to this repository through the GitHub App.
- `roles/storage.objectAdmin` on the state bucket for the layer identity
  (both layers share one bucket; scoping to a prefix needs an IAM condition
  plus an unconditional object-list grant, and is a decision for then).
- Build logs routed to Cloud Logging (`CLOUD_LOGGING_ONLY`), which needs
  `roles/logging.logWriter` in the boot project for the identity.

Until then the bootstrap administrator applies both layers by hand.
