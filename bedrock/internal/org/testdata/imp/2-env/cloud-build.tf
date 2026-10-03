# ---------------------------------------------------------------------------
# Cloud Build, second generation: the GitHub connection and the repository
# links
#
# One connection per environment project, authorized once. The Cloud Build
# GitHub App is installed on the impulseframework organization (one
# installation ID for every environment), and the connection proves it may act
# for that installation with a GitHub OAuth token held in Secret Manager. That
# token is produced by a browser step that cannot be scripted: in the tst
# project's Cloud Build console, Repositories > Create host connection >
# GitHub, authorize as the machine account bedrockbot-ccc. The console writes
# the token as a regional secret in the tst project, in the connection's
# region, and points the connection at its latest version; the values file
# pins version 1 instead, so a re-authorization is a visible change here
# rather than a silent one. That one version is then reused
# by the stg and prd connections, so the browser step happens once per
# organization rather than once per environment; the tst apply grants every
# environment's Cloud Build service agent accessor on the secret.
#
# Apply order: tst first (it makes the grants), then stg and prd.
#
# Until the browser step has run, github_app_installation_id and
# github_oauth_token_secret_version stay unset and everything below but the
# service agent is skipped: the environment (identities, registries, records
# bucket, the tst instance) applies without its connection, and the
# application layer's triggers wait on it.
# ---------------------------------------------------------------------------

# The Cloud Build service agent is the identity that reads the token. It is
# created when the API is enabled (1-org does that), and this forces it into
# existence where enabling ran ahead of provisioning.
resource "google_project_service_identity" "cloudbuild" {
  provider = google-beta

  project = local.project_id
  service = "cloudbuild.googleapis.com"
}

# tst only: the token secret lives here, so this apply is the one that can set
# its policy (secretContainerAdmin carries secrets.setIamPolicy). One grant per
# environment project's Cloud Build service agent, addressed by project
# number from 1-org.
locals {
  github_token_readers = {
    for env, number in local.org.project_numbers : env => number
    if local.connected && local.is_tst && contains(["tst", "stg", "prd"], env)
  }
}

# The console's host-connection flow writes the token as a regional secret in
# the connection's region, and a regional secret has its own IAM resource.
resource "google_secret_manager_regional_secret_iam_member" "github_token_accessor" {
  for_each = local.github_token_regional ? local.github_token_readers : {}

  project   = local.github_token_project
  location  = local.github_token_location
  secret_id = local.github_token_secret
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:service-${each.value}@gcp-sa-cloudbuild.iam.gserviceaccount.com"
}

# A token added by hand as a global secret (README) is granted the same way.
resource "google_secret_manager_secret_iam_member" "github_token_accessor" {
  for_each = local.github_token_regional ? {} : local.github_token_readers

  project   = local.github_token_project
  secret_id = local.github_token_secret
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:service-${each.value}@gcp-sa-cloudbuild.iam.gserviceaccount.com"
}

resource "google_cloudbuildv2_connection" "github" {
  count = local.connected ? 1 : 0

  project  = local.project_id
  location = local.region
  name     = "${local.name}-${local.region_code}-github"

  github_config {
    app_installation_id = var.github_app_installation_id

    authorizer_credential {
      oauth_token_secret_version = var.github_oauth_token_secret_version
    }
  }

  depends_on = [
    google_project_service_identity.cloudbuild,
    google_secret_manager_secret_iam_member.github_token_accessor,
    google_secret_manager_regional_secret_iam_member.github_token_accessor,
  ]
}

# The pipeline mints a short-lived GitHub token from the connection (the tag
# check reads the compare API, the pull-request build reads its /gcbrun
# comment). That is cloudbuild.repositories.accessReadToken, carried by the
# Read Token Accessor role and by no builder role; granted on the connection
# itself, to each application's deploy identity, and nowhere wider. Found by
# the first triggered build (2026-09-26): the mint answered 403.
resource "google_cloudbuildv2_connection_iam_member" "deploy_read_token" {
  for_each = local.connected ? local.apps : toset([])

  project  = local.project_id
  location = google_cloudbuildv2_connection.github[0].location
  name     = google_cloudbuildv2_connection.github[0].name
  role     = "roles/cloudbuild.readTokenAccessor"
  member   = google_service_account.deploy[each.key].member
}

# The repository link is per application and is registration, not
# application infrastructure: creating one needs cloudbuild.repositories.create,
# which only the connection administrator holds, and the design brief puts
# "the repository registration" with the environment layer. The application
# stack's triggers reference the link by ID (output applications[*].repository_id).
resource "google_cloudbuildv2_repository" "app" {
  for_each = local.connected ? local.apps : toset([])

  project           = local.project_id
  location          = local.region
  name              = "${local.name}-${local.region_code}-${each.key}-repo"
  parent_connection = google_cloudbuildv2_connection.github[0].id
  remote_uri        = "https://github.com/${var.github_organization}/${each.key}.git"
}
