# ---------------------------------------------------------------------------
# The deployer GitHub App
#
# The pipeline talks back on a pull request as the organization's deployer
# GitHub App: a deployment carrying the environment's URL, a comment, the
# guard's refusals. The app, its key and its pin are 0-bootstrap's: the key is
# a secret version in the boot project's container
# (local.github_deployer_key_secret), added with bedrock secret add
# github-deployer-key and pinned by resource name in placement.json
# (githubDeployerKeyVersion, rendered into locals.tf) with bedrock secret pin
# github-deployer-key <version> (never latest). This layer grants the read of
# that container to every application's deploy identity, and in tst to its
# apply identity too (the stack's check trigger and pull-request stack trigger
# run as the apply identity and talk back on the pull request: the plans'
# summaries, the guard's refusals, a database recreated); no runtime identity
# reads it. The grants take the environment layer identity's administration
# of the container, from 1-org (secretContainerAdmin), and a plan refreshes
# them through the plan identity's read of its policy, from 1-org too. The App
# ID (placement.json's githubDeployerAppId) is not a secret. Until both are
# recorded, the application stacks pass empty substitutions and the pipeline
# talks back through nothing.
#
# An earlier bedrock held a copy of the key in a container of this
# environment's project, <name>-gbl-github-deployer-key, pinned per
# environment. The removed block destroys that container with its versions on
# the first apply of this render; the boot container holds the key from then
# on, and the two grants below move there with it.
# ---------------------------------------------------------------------------

removed {
  from = google_secret_manager_secret.github_deployer_key

  lifecycle {
    destroy = true
  }
}

resource "google_secret_manager_secret_iam_member" "deploy_deployer_key" {
  for_each = local.apps

  project   = var.boot_project_id
  secret_id = local.github_deployer_key_secret
  role      = "roles/secretmanager.secretAccessor"
  member    = google_service_account.deploy[each.key].member
}

resource "google_secret_manager_secret_iam_member" "apply_deployer_key" {
  for_each = local.is_tst ? local.apps : toset([])

  project   = var.boot_project_id
  secret_id = local.github_deployer_key_secret
  role      = "roles/secretmanager.secretAccessor"
  member    = google_service_account.apply[each.key].member
}
