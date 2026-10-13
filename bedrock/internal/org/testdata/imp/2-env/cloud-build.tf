# ---------------------------------------------------------------------------
# Cloud Build, second generation: the GitHub connection and the repository
# links
#
# One connection per environment project. The Cloud Build GitHub App is
# installed on the imp-example organization (one installation ID for
# every connection), and the connection proves it may act for that
# installation with a GitHub OAuth token held in Secret Manager. That token is
# produced once per organization by a browser step that cannot be scripted,
# in the boot project's Cloud Build console, signed in to GitHub as the
# machine account imp-machine, before the first apply of 0-bootstrap
# (0-bootstrap/README.md, "The Cloud Build GitHub authorization"). The console
# writes the token as a regional secret in the boot project, and placement.json
# records the installation and the token secret's version
# (cloudBuildInstallationId, cloudBuildTokenSecretVersion), which every
# connection is rendered from: the boot project's, which the layers' triggers
# watch this repository through, and each environment's. 0-bootstrap grants
# this project's Cloud Build service agent the token's read, by the project
# number placement.json records (projectNumbers).
#
# Until the browser step has run, placement.json records neither value and
# everything below but the service agent is skipped: the environment
# (identities, registries, records bucket, the tst instance) applies without
# its connection, and the application layer's triggers wait on it.
# ---------------------------------------------------------------------------

# The Cloud Build service agent is the identity that reads the token. It is
# created when the API is enabled (1-org does that), and this forces it into
# existence where enabling ran ahead of provisioning.
resource "google_project_service_identity" "cloudbuild" {
  provider = google-beta

  project = local.project_id
  service = "cloudbuild.googleapis.com"
}

resource "google_cloudbuildv2_connection" "github" {
  count = local.connected ? 1 : 0

  project  = local.project_id
  location = local.region
  name     = "${local.name}-${local.region_code}-github"

  github_config {
    app_installation_id = local.cloud_build_installation_id

    authorizer_credential {
      oauth_token_secret_version = local.cloud_build_token_secret_version
    }
  }

  depends_on = [google_project_service_identity.cloudbuild]
}

# The token's accessor grants an earlier bedrock made from tst, when the token
# secret lived there, are retired: 0-bootstrap grants on the secret in the
# boot project now. Taken out of the state; the secret they named is gone
# with the old connection.
removed {
  from = google_secret_manager_regional_secret_iam_member.github_token_accessor

  lifecycle {
    destroy = false
  }
}

removed {
  from = google_secret_manager_secret_iam_member.github_token_accessor

  lifecycle {
    destroy = false
  }
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

# In tst the application stack's own triggers (the check trigger, the
# pull-request stack trigger and the sweep) run as the apply identity and mint
# the same token: the check trigger posts its plans on the pull request, the
# stack job reads the /gcbrun comment, the sweep asks GitHub which pull
# requests are closed.
resource "google_cloudbuildv2_connection_iam_member" "apply_read_token" {
  for_each = local.connected && local.is_tst ? local.apps : toset([])

  project  = local.project_id
  location = google_cloudbuildv2_connection.github[0].location
  name     = google_cloudbuildv2_connection.github[0].name
  role     = "roles/cloudbuild.readTokenAccessor"
  member   = google_service_account.apply[each.key].member
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
