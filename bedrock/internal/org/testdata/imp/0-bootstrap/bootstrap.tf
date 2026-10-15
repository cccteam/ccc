locals {
  org_id   = trimprefix(data.google_organization.this.name, "organizations/")
  org_name = data.google_organization.this.name

  # A layer is one directory of this repository, applied as one identity. Layer
  # names follow {prefix}-{environment}-gbl; their service accounts add -tofu.
  boot_layer_name = "${var.prefix}-boot-gbl"
  org_layer_name  = "${var.prefix}-org-gbl"

  labels = {
    terraform             = "true"
    terraform_source_path = "0-bootstrap"
    source_repo           = "imp-impulse-infrastructure"
    organization          = "imp"
    environment           = "boot"
    bedrock-lab           = "true"
  }
}

# ---------------------------------------------------------------------------
# Seeded resources
#
# The folder, project, and boot service account below already exist. They are
# created by hand with gcloud during first-time setup, because this layer
# cannot create the identity it will later run as. They are adopted here so
# that from the first run onward they are ordinary managed resources rather
# than undocumented console state. The commands are in this layer's README.
#
# The state bucket is seeded too, and is the one seeded resource deliberately
# left outside OpenTofu: this layer's own state lives in it, and a stack that
# owns the container of its own state can destroy that container out from
# under itself. tf-gcp-setup leaves its Scalr environment unmanaged for the
# same reason.
#
# Everything else in this layer is created normally.
# ---------------------------------------------------------------------------

# The terraform folder sits alongside the environment folders created by 1-org
# and holds nothing but the automation plane. Keeping it out of the environment
# hierarchy means an environment folder can be reorganised without touching the
# identity that provisions it, and the org policies 1-org applies to the
# environment folders never reach the identities that manage them.
# The organization's suffix: a random value born with this state on the first
# apply and gone with it, telling this bedrock organization from another in
# the same GCP organization and from its own predecessor. It is the suffix of
# every name the layers put at the organization node, where names must differ
# between organizations and where Google holds a deleted name for weeks: the
# custom roles (here and in 1-org) and the public-invoker tag key (1-org).
# Nothing inside a project carries it; the project ids carry their own.
resource "random_id" "organization" {
  byte_length = 2
}

# The organization's folder, named after its prefix, holds everything the
# layers make under the GCP organization: the terraform folder here, the
# environment folders 1-org creates. Sibling folders must differ in name, so
# two bedrock organizations in one GCP organization each keep their own tree.
# Seeded, like the terraform folder, since the boot project must exist before
# this layer can run.
resource "google_folder" "organization" {
  display_name        = var.prefix
  parent              = local.org_name
  deletion_protection = !var.destroy
}

import {
  id = "folders/${var.organization_folder_id}"
  to = google_folder.organization
}

resource "google_folder" "terraform" {
  display_name        = "terraform"
  parent              = google_folder.organization.name
  deletion_protection = !var.destroy
}

import {
  id = "folders/${var.terraform_folder_id}"
  to = google_folder.terraform
}

# The boot project hosts the layer identities, the state bucket, the Cloud
# Build connection and triggers that plan and apply the layers (cloud-build.tf),
# the two GitHub Apps' keys (github.tf), and acts as the quota and billing
# project for every downstream layer.
#
# random_project_id is off and the ID comes from a variable: the project already
# exists with a suffix the seed script chose, and the module would otherwise
# generate a different one and try to create a second project. folder_id comes
# from the same variable rather than from google_folder.terraform so that a
# tainted or replaced folder cannot cascade into replacing the project.
module "boot_project" {
  source  = "terraform-google-modules/project-factory/google"
  version = "18.3.0"

  name                       = var.boot_project_id
  folder_id                  = var.terraform_folder_id
  billing_account            = var.billing_account_id
  random_project_id          = false
  create_project_sa          = false
  default_service_account    = "delete"
  disable_dependent_services = true
  # bedrock org destroy deletes the project, which takes everything in it (the
  # state bucket included); an API disabled first refuses while a resource
  # outside this layer's state still uses it, so a destroy run disables none.
  # The provider reads both settings from the state, which the command records
  # before its destroy (var.destroy).
  disable_services_on_destroy = !var.destroy
  deletion_policy             = var.destroy ? "DELETE" : "PREVENT"
  labels                      = local.labels

  activate_apis = [
    "cloudbilling.googleapis.com",            # link the billing account to projects created by 1-org
    "cloudresourcemanager.googleapis.com",    # folders, projects, IAM
    "essentialcontacts.googleapis.com",       # Essential Contacts set by 1-org
    "cloudbuild.googleapis.com",              # the connection and the triggers that plan and apply the layers (cloud-build.tf)
    "iam.googleapis.com",                     # service accounts
    "iamcredentials.googleapis.com",          # the short-lived tokens a trigger's run impersonates a layer identity with
    "logging.googleapis.com",                 # the triggers' build logs; the log sinks if 1-org's central logging is turned on
    "orgpolicy.googleapis.com",               # org policy constraints
    "privilegedaccessmanager.googleapis.com", # the environments' layer state entitlements 1-org declares here
    "serviceusage.googleapis.com",            # API enablement in downstream projects
    "storage.googleapis.com",                 # the state bucket

    # Every downstream layer reaches its APIs through this project as the quota project
    # (user_project_override), and an API refuses a quota project where it is disabled:
    # Spanner did, on the first 2-spn apply. The set below is every API a downstream
    # layer manages, enabled here once so no layer trips on it.
    "artifactregistry.googleapis.com",
    "certificatemanager.googleapis.com",
    "cloudscheduler.googleapis.com",
    "cloudtasks.googleapis.com",
    "compute.googleapis.com",
    "dns.googleapis.com",
    "domains.googleapis.com",         # Cloud Domains: registrations live in the net project (2-net), queried through this quota project
    "identitytoolkit.googleapis.com", # Identity Platform: 2-env initializes Firebase Authentication on each environment project through this quota project
    "monitoring.googleapis.com",
    "run.googleapis.com",
    "secretmanager.googleapis.com",
    "spanner.googleapis.com",
  ]
}

import {
  id = var.boot_project_id
  to = module.boot_project.module.project-factory.google_project.main
}

# The identity this layer runs as through the apply trigger, after the
# bootstrap administrator's first apply (cloud-build.tf). The seed script
# creates it; it is adopted rather than recreated, because destroying it
# mid-run would cut off the run doing the destroying.
resource "google_service_account" "boot_tofu" {
  project      = var.boot_project_id
  account_id   = "${var.prefix}-boot-gbl-tofu"
  display_name = "OpenTofu SA - ${local.boot_layer_name}"
  description  = "Layer identity for ${local.boot_layer_name}. Applies the 0-bootstrap layer from the infrastructure repository's apply trigger."
}

import {
  id = "projects/${var.boot_project_id}/serviceAccounts/${var.prefix}-boot-gbl-tofu@${var.boot_project_id}.iam.gserviceaccount.com"
  to = google_service_account.boot_tofu
}

# The gcloud seed grants this same list so the identity can run this layer at
# all. Declaring it here makes the grants visible and reviewable instead of
# leaving them as console state nobody can audit. Keep the two lists in sync:
# shrinking boot_layer_roles revokes permissions from the identity running the
# apply, and the run that does it may not survive to finish.
resource "google_organization_iam_member" "boot_tofu" {
  # checkov:skip=CKV_GCP_117: the boot layer identity has to create folders, projects, org-level IAM bindings, and custom roles before any narrower scope exists. It has no keys and runs only from the infrastructure repository's apply trigger, on its default branch.
  # checkov:skip=CKV_GCP_49: See CKV_GCP_117 above.
  # checkov:skip=CKV_GCP_45: iam.serviceAccountAdmin is granted at the org node because this layer creates service accounts in a project that is itself created here.
  for_each = toset(var.boot_layer_roles)

  org_id = local.org_id
  role   = each.value
  member = google_service_account.boot_tofu.member
}

# roles/billing.user rather than the roles/billing.admin tf-gcp-setup grants:
# linking a project to the account is all a layer ever does with billing, and
# the account belongs to CCC rather than to this organization. Gated because
# only a Billing Account Administrator can apply it; see var.manage_billing_iam.
resource "google_billing_account_iam_member" "boot_tofu" {
  count = var.manage_billing_iam ? 1 : 0

  billing_account_id = var.billing_account_id
  role               = "roles/billing.user"
  member             = google_service_account.boot_tofu.member
}

# ---------------------------------------------------------------------------
# Org layer identity
#
# 1-org creates folders, projects, org policies, a custom role, and per-project
# service accounts. Those grants have to sit at the org node because the
# resources they act on do not exist yet.
# ---------------------------------------------------------------------------

resource "google_service_account" "org_tofu" {
  project      = module.boot_project.project_id
  account_id   = "${var.prefix}-org-gbl-tofu"
  display_name = "OpenTofu SA - ${local.org_layer_name}"
  description  = "Layer identity for ${local.org_layer_name}. Applies the 1-org layer from the infrastructure repository's apply trigger."
}

resource "google_organization_iam_member" "org_tofu" {
  # checkov:skip=CKV_GCP_117: the org layer identity must manage folders, projects, org policies, IAM, and a custom role across the hierarchy this repo owns. It has no keys and runs only from the infrastructure repository's apply trigger, on its default branch.
  # checkov:skip=CKV_GCP_49: See CKV_GCP_117 above.
  # checkov:skip=CKV_GCP_45: iam.serviceAccountAdmin is granted at the org node because 1-org creates service accounts inside projects that do not exist at bootstrap time, so project-level scoping is not possible.
  for_each = toset(var.org_layer_roles)

  org_id = local.org_id
  role   = each.value
  member = google_service_account.org_tofu.member
}

# ---------------------------------------------------------------------------
# The two boot layers' plan identities
#
# A pull request plans this layer and 1-org as read-only identities of their
# own, in the boot project beside the apply identities: a plan as an apply
# identity would run a pull request's code with the power to apply. They are
# this layer's to make, with the apply identities, and their organization-level
# reads are this layer's to grant: a binding at the organization node is, like
# every one of them, since the org layer identity holds no role there that
# sets the organization's policy (var.org_layer_roles), by design.
# ---------------------------------------------------------------------------

locals {
  # The two plan identities, named after their layers (boot, org), with the
  # state prefixes each one reads (state-bucket.tf).
  boot_plans = {
    boot = { layer = "0-bootstrap", reads = ["0-bootstrap"] }
    org  = { layer = "1-org", reads = ["0-bootstrap", "1-org"] }
  }
}

resource "google_service_account" "boot_plan" {
  for_each = local.boot_plans

  project      = module.boot_project.project_id
  account_id   = "${var.prefix}-${each.key}-gbl-plan"
  display_name = "OpenTofu plan SA - ${var.prefix}-${each.key}-gbl"
  description  = "Read-only plan identity for ${var.prefix}-${each.key}-gbl. Plans ${each.value.layer} on a pull request of the infrastructure repository and never applies."
}

# Organization-level reads (var.boot_plan_roles): the layers they plan declare
# folders, projects, tags and custom roles across the organization.
resource "google_organization_iam_member" "boot_plan" {
  for_each = {
    for pair in setproduct(keys(local.boot_plans), var.boot_plan_roles) :
    "${pair[0]}__${pair[1]}" => { plan = pair[0], role = pair[1] }
  }

  org_id = local.org_id
  role   = each.value.role
  member = google_service_account.boot_plan[each.value.plan].member
}

# The boot project is a plan's quota project, as it is every layer's.
resource "google_project_iam_member" "boot_plan_quota" {
  for_each = local.boot_plans

  project = module.boot_project.project_id
  role    = "roles/serviceusage.serviceUsageConsumer"
  member  = google_service_account.boot_plan[each.key].member
}

# resourcemanager.projects.update is not carried by any narrow predefined role
# at org scope, so a custom role is the only way to grant it short of owner.
resource "google_organization_iam_custom_role" "project_updater" {
  org_id      = local.org_id
  role_id     = "orgProjectUpdater_${random_id.organization.hex}"
  title       = "Project Updater"
  description = "Grants resourcemanager.projects.update so the org layer identity can manage project labels and metadata."
  permissions = ["resourcemanager.projects.update"]
}

resource "google_organization_iam_member" "org_tofu_project_updater" {
  org_id = local.org_id
  role   = google_organization_iam_custom_role.project_updater.id
  member = google_service_account.org_tofu.member
}

# The boot identity manages the boot project's labels and metadata through the
# project module when this layer runs through the apply trigger; the bootstrap
# administrator's Owner covered the first apply.
resource "google_organization_iam_member" "boot_tofu_project_updater" {
  org_id = local.org_id
  role   = google_organization_iam_custom_role.project_updater.id
  member = google_service_account.boot_tofu.member
}

# 1-org declares on this project each environment's layer state entitlement:
# the environment layer identity's slot in the state bucket and this project as
# its quota project, for a short time, to a member of the environment's team
# group recovering 2-env by hand (1-org/entitlements.tf). Declaring an
# entitlement takes roles/privilegedaccessmanager.admin on the project it is
# declared on: granted here, on this project alone.
resource "google_project_iam_member" "org_tofu_entitlements" {
  project = module.boot_project.project_id
  role    = "roles/privilegedaccessmanager.admin"
  member  = google_service_account.org_tofu.member
}

# A pull request plans 1-org as its plan identity, which refreshes the
# entitlements: their read, on the boot project alone.
resource "google_project_iam_member" "org_plan_entitlements" {
  project = module.boot_project.project_id
  role    = "roles/privilegedaccessmanager.viewer"
  member  = google_service_account.boot_plan["org"].member
}

# Billing is granted on the billing account, not the org node. Gated for the
# same reason as the boot grant above.
resource "google_billing_account_iam_member" "org_tofu" {
  count = var.manage_billing_iam ? 1 : 0

  billing_account_id = var.billing_account_id
  role               = "roles/billing.user"
  member             = google_service_account.org_tofu.member
}
