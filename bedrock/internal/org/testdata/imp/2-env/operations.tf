# ---------------------------------------------------------------------------
# Operations from GitHub
#
# A restore of this environment to a release (bedrock restore <env> <release>)
# is started from GitHub: the application's operations workflow runs the
# environment's version trigger with the restore instruction, and the pipeline
# does the work as the deploy identity, as for any release. The workflow's job
# holds no key. It exchanges GitHub's short-lived token for the application's
# operations identity through this pool, whose provider trusts tokens of the
# organization's repositories alone, from the operations workflow file, run in
# the GitHub Environment named after this environment; the binding below
# narrows that to the application's own repository. The identity runs
# triggers and reads the builds they start (1-org's cloudBuildTriggerRunner)
# and acts as the deploy identity the trigger's builds run as, which starting
# a trigger requires, and nothing else. None of this exists in prd:
# production is never restored by a run.
# ---------------------------------------------------------------------------

resource "google_iam_workload_identity_pool" "github" {
  count = local.is_prd ? 0 : 1

  project                   = local.project_id
  workload_identity_pool_id = "${local.name}-github"
  display_name              = "${local.name} GitHub"
  description               = "GitHub Actions of ${var.github_organization}'s repositories, for operations on this environment."
}

resource "google_iam_workload_identity_pool_provider" "github" {
  count = local.is_prd ? 0 : 1

  project                            = local.project_id
  workload_identity_pool_id          = google_iam_workload_identity_pool.github[0].workload_identity_pool_id
  workload_identity_pool_provider_id = "github"
  display_name                       = "GitHub"
  description                        = "Tokens of the operations workflow in ${var.github_organization}'s repositories, run in the GitHub Environment ${var.environment}."

  attribute_mapping = {
    "google.subject"             = "assertion.sub"
    "attribute.repository"       = "assertion.repository"
    "attribute.repository_owner" = "assertion.repository_owner"
    "attribute.environment"      = "assertion.environment"
    "attribute.workflow"         = "assertion.job_workflow_ref"
  }
  attribute_condition = "assertion.repository_owner == \"${var.github_organization}\" && assertion.job_workflow_ref.contains(\"/.github/workflows/operations.yml@\") && assertion.environment == \"${var.environment}\""

  oidc {
    issuer_uri = "https://token.actions.githubusercontent.com"
  }
}

resource "google_service_account" "operations" {
  for_each = local.is_prd ? toset([]) : local.apps

  project      = local.project_id
  account_id   = "${local.name}-gbl-${each.key}-ops"
  display_name = "Operations SA - ${local.name}-gbl-${each.key}"
  description  = "Operations identity for ${each.key} in ${var.environment}: the application's operations workflow runs the version trigger as it, and nothing else."
}

# The operations workflow of the application's own repository, and no other
# repository of the organization, may become the identity.
resource "google_service_account_iam_member" "operations_workflow" {
  for_each = local.is_prd ? toset([]) : local.apps

  service_account_id = google_service_account.operations[each.key].name
  role               = "roles/iam.workloadIdentityUser"
  member             = "principalSet://iam.googleapis.com/${google_iam_workload_identity_pool.github[0].name}/attribute.repository/${var.github_organization}/${each.key}"
}

resource "google_project_iam_member" "operations_trigger_runner" {
  for_each = local.is_prd ? toset([]) : local.apps

  project = local.project_id
  role    = local.org.cloud_build_trigger_runner_role
  member  = google_service_account.operations[each.key].member
}

# Starting a trigger whose builds run as a service account needs
# iam.serviceAccounts.actAs on that account.
resource "google_service_account_iam_member" "operations_runs_deploy" {
  for_each = local.is_prd ? toset([]) : local.apps

  service_account_id = google_service_account.deploy[each.key].name
  role               = "roles/iam.serviceAccountUser"
  member             = google_service_account.operations[each.key].member
}
