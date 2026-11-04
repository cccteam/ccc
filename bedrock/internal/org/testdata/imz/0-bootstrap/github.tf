# ---------------------------------------------------------------------------
# The GitHub Apps' keys
#
# The organization's two GitHub Apps act from the boot project, and their
# private keys live here, each a version of a container this layer creates,
# pinned by number in placement.json and never read as latest. The apps are a
# person's to create and the keys a person's to add (README, "The
# infrastructure GitHub App" and "The deployer GitHub App's key"):
#
#   imz-boot-gbl-github-infrastructure-key  the infrastructure GitHub App, whose
#     installation token 1-dns's GitHub provider needs, with an organization
#     owner's rights over this repository, whose default branch it rules. A
#     run of the triggers mints it from the key (cloud-build.tf) for a plan or
#     an apply of 1-dns; a person applying 1-dns by hand uses their own token.
#   imz-boot-gbl-github-deployer-key  the deployer GitHub App, which the plan trigger's
#     run posts each layer's plan on the pull request as.
#
# The dns identity administers the two containers' policies (and reads the
# infrastructure key for 1-dns's apply); 1-dns's plan identity (bootstrap.tf)
# reads the infrastructure key too, since a plan of 1-dns reads the
# repository. The runners' reads are cloud-build.tf's. The triggers need no
# sign-in from GitHub.
# ---------------------------------------------------------------------------

resource "google_secret_manager_secret" "github_infrastructure_key" {
  project   = module.boot_project.project_id
  secret_id = "${var.prefix}-boot-gbl-github-infrastructure-key"

  replication {
    auto {}
  }

  labels = local.labels
}

resource "google_secret_manager_secret" "github_deployer_key" {
  project   = module.boot_project.project_id
  secret_id = "${var.prefix}-boot-gbl-github-deployer-key"

  replication {
    auto {}
  }

  labels = local.labels
}

resource "google_secret_manager_secret_iam_member" "org_tofu_infrastructure_key" {
  project   = module.boot_project.project_id
  secret_id = google_secret_manager_secret.github_infrastructure_key.secret_id
  role      = "roles/secretmanager.admin"
  member    = google_service_account.org_tofu.member
}

resource "google_secret_manager_secret_iam_member" "org_tofu_deployer_key" {
  project   = module.boot_project.project_id
  secret_id = google_secret_manager_secret.github_deployer_key.secret_id
  role      = "roles/secretmanager.admin"
  member    = google_service_account.org_tofu.member
}

# A plan of 1-dns reads this repository with the infrastructure
# app's token, minted from the key: the dns plan identity reads that one secret.
resource "google_secret_manager_secret_iam_member" "org_plan_infrastructure_key" {
  project   = module.boot_project.project_id
  secret_id = google_secret_manager_secret.github_infrastructure_key.secret_id
  role      = "roles/secretmanager.secretAccessor"
  member    = google_service_account.boot_plan["dns"].member
}

# The boot identity manages the containers when this layer runs through the
# apply trigger; the break-glass account's Owner covered the first apply.
resource "google_project_iam_member" "boot_tofu_secrets" {
  project = module.boot_project.project_id
  role    = "roles/secretmanager.admin"
  member  = google_service_account.boot_tofu.member
}



