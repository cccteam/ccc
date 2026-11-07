# ---------------------------------------------------------------------------
# The deployer GitHub App
#
# The pipeline talks back on a pull request as the organization's deployer
# GitHub App: a deployment carrying the environment's URL, a comment, the
# guard's refusals. The app's key is a secret in the root organization's root
# project (local.keys_project, local.github_deployer_key_secret), one for
# every organization, read by its alias current, which that organization pins
# and moves (never a number here, never latest). This layer grants the read
# of that secret to every application's deploy identity, and in tst to its
# apply identity too (the stack's check trigger and pull-request stack trigger
# run as the apply identity and talk back on the pull request: the plans'
# summaries, the guard's refusals, a database recreated); no runtime identity
# reads it. The grants take the environment layer identity's administration
# of the secret's policy, from 1-org (the root organization's
# secretPolicyAdmin), and a plan refreshes them through the plan identity's
# read of that policy, from 1-org too. The App ID (placement.json's
# githubDeployerAppId) is not a secret. Until it is recorded, the application
# stacks pass an empty substitution and the pipeline talks back through
# nothing.
#
# The release GitHub App's key (local.github_release_key_secret, the same
# way) is read by each application's release identity, in tst alone, where
# the release trigger runs: the trigger mints the app's token from it and
# runs release-please. No other identity reads it, and the key never reaches
# GitHub.
# ---------------------------------------------------------------------------


resource "google_secret_manager_secret_iam_member" "deploy_deployer_key" {
  for_each = local.apps

  project   = local.keys_project
  secret_id = local.github_deployer_key_secret
  role      = "roles/secretmanager.secretAccessor"
  member    = google_service_account.deploy[each.key].member
}

resource "google_secret_manager_secret_iam_member" "apply_deployer_key" {
  for_each = local.is_tst ? local.apps : toset([])

  project   = local.keys_project
  secret_id = local.github_deployer_key_secret
  role      = "roles/secretmanager.secretAccessor"
  member    = google_service_account.apply[each.key].member
}

resource "google_secret_manager_secret_iam_member" "release_release_key" {
  for_each = local.is_tst ? local.apps : toset([])

  project   = local.keys_project
  secret_id = local.github_release_key_secret
  role      = "roles/secretmanager.secretAccessor"
  member    = google_service_account.release[each.key].member
}
