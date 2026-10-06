# ---------------------------------------------------------------------------
# The layers workflow's sign-in
#
# The layers of this repository are planned on every pull request and applied
# on its merge by the workflow .github/workflows/layers.yml, each as its own identity and
# with no key anywhere: a run presents GitHub's short-lived token to the pool
# below, whose provider trusts tokens of this repository
# (imp-example/imp-impulse-infrastructure) from that workflow file alone, and Google
# answers with a token for the identity the run asked for. The provider maps
# the token's event and ref to attribute.purpose: plan for a pull request into
# master, apply for a push to it or a run started from it,
# none for anything else. A plan identity's federation binding admits plan and
# an apply identity's admits apply, so a pull request's code reads and never
# writes. This layer binds the two identities it owns (the boot identity and
# the org identity); 1-org binds the ones it creates, its own plan identity
# among them. The first applies of this layer and of 1-org are the bootstrap
# administrator's, before any of this exists; from then on both go through
# the workflow too.
#
# 1-org's GitHub provider needs a token with an organization owner's rights,
# for the applications' repositories. In the workflow that token is an
# installation token of the infrastructure GitHub App, minted in the run from
# the private key held in the container below at the version placement.json
# pins (githubInfrastructureKeyVersion, never latest). The app is a person's
# to create and the key a person's to add (README, "The infrastructure GitHub
# App"); the org identity reads the key and administers the container's
# policy, so 1-org can grant its own plan identity the same read, since a plan
# of 1-org reads the repositories too.
# ---------------------------------------------------------------------------

resource "google_iam_workload_identity_pool" "github" {
  project                   = module.boot_project.project_id
  workload_identity_pool_id = "${var.prefix}-boot-github"
  display_name              = "${var.prefix}-boot GitHub"
  description               = "GitHub Actions of ${var.github_organization}/${var.infrastructure_repository}, for the layers workflow."
}

resource "google_iam_workload_identity_pool_provider" "github" {
  project                            = module.boot_project.project_id
  workload_identity_pool_id          = google_iam_workload_identity_pool.github.workload_identity_pool_id
  workload_identity_pool_provider_id = "github"
  display_name                       = "GitHub"
  description                        = "Tokens of the layers workflow in ${var.github_organization}/${var.infrastructure_repository}: plan from a pull request, apply from the default branch."

  attribute_mapping = {
    "google.subject"       = "assertion.sub"
    "attribute.repository" = "assertion.repository"
    "attribute.workflow"   = "assertion.job_workflow_ref"
    "attribute.ref"        = "assertion.ref"
    "attribute.purpose"    = "assertion.event_name == \"pull_request\" && assertion.base_ref == \"${var.github_default_branch}\" ? \"plan\" : ((assertion.event_name == \"push\" || assertion.event_name == \"workflow_dispatch\") && assertion.ref == \"refs/heads/${var.github_default_branch}\" ? \"apply\" : \"none\")"
  }
  attribute_condition = "assertion.repository == \"${var.github_organization}/${var.infrastructure_repository}\" && assertion.job_workflow_ref.startsWith(\"${var.github_organization}/${var.infrastructure_repository}/.github/workflows/layers.yml@\")"

  oidc {
    issuer_uri = "https://token.actions.githubusercontent.com"
  }
}

locals {
  # The two principal sets a binding selects: every run of the workflow that
  # plans, and every run that applies.
  workflow_plan  = "principalSet://iam.googleapis.com/${google_iam_workload_identity_pool.github.name}/attribute.purpose/plan"
  workflow_apply = "principalSet://iam.googleapis.com/${google_iam_workload_identity_pool.github.name}/attribute.purpose/apply"
}

# The two identities this layer owns apply from the default branch alone. Their
# plan identities are 1-org's to make (workflow.tf there), since this layer's
# own identity exists before it and a plan as an apply identity would run a
# pull request's code with the power to apply.
resource "google_service_account_iam_member" "boot_tofu_workflow" {
  service_account_id = google_service_account.boot_tofu.name
  role               = "roles/iam.workloadIdentityUser"
  member             = local.workflow_apply
}

resource "google_service_account_iam_member" "org_tofu_workflow" {
  service_account_id = google_service_account.org_tofu.name
  role               = "roles/iam.workloadIdentityUser"
  member             = local.workflow_apply
}

# The infrastructure GitHub App's private key: the container here, the
# versions a person's (README, "The infrastructure GitHub App").
resource "google_secret_manager_secret" "github_infrastructure_key" {
  project   = module.boot_project.project_id
  secret_id = "${var.prefix}-boot-gbl-github-infrastructure-key"

  replication {
    auto {}
  }

  labels = local.labels
}

# The org identity reads the key for 1-org's apply and sets the container's
# policy, which is how 1-org grants its plan identity the read; nothing else
# in the boot project is a secret.
resource "google_secret_manager_secret_iam_member" "org_tofu_infrastructure_key" {
  project   = module.boot_project.project_id
  secret_id = google_secret_manager_secret.github_infrastructure_key.secret_id
  role      = "roles/secretmanager.admin"
  member    = google_service_account.org_tofu.member
}

# The boot identity manages the container when this layer runs through the
# workflow; the bootstrap administrator's Owner covered the first apply.
resource "google_project_iam_member" "boot_tofu_secrets" {
  project = module.boot_project.project_id
  role    = "roles/secretmanager.admin"
  member  = google_service_account.boot_tofu.member
}
