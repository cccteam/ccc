# ---------------------------------------------------------------------------
# The deployer GitHub App
#
# The pipeline talks back on a pull request as the organization's deployer
# GitHub App: a deployment carrying the environment's URL, a comment, the
# guard's refusals. The app's private key is a secret version in this
# environment's project, held in the container this layer creates, added by
# an operator and pinned by resource name in
# var.github_deployer_key_secret_versions (never latest). Every application's
# deploy identity reads it; no runtime identity does. The App ID
# (var.github_deployer_app_id) is not a secret. Until both are set for an
# environment, the application stacks pass empty substitutions and the
# pipeline talks back through nothing.
# ---------------------------------------------------------------------------

resource "google_secret_manager_secret" "github_deployer_key" {
  project   = local.project_id
  secret_id = "${local.name}-gbl-github-deployer-key"

  replication {
    auto {}
  }

  labels = local.labels
}

resource "google_secret_manager_secret_iam_member" "deploy_deployer_key" {
  for_each = local.apps

  project   = local.project_id
  secret_id = google_secret_manager_secret.github_deployer_key.secret_id
  role      = "roles/secretmanager.secretAccessor"
  member    = google_service_account.deploy[each.key].member
}
