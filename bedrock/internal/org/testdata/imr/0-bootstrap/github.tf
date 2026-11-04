# ---------------------------------------------------------------------------
# The GitHub Apps' keys
#
# The three GitHub Apps' private keys live here, in this organization's boot
# project, for every bedrock organization in the Google Cloud organization:
# one secret per app, each version a key, and on each secret the alias
# "current", which placement.json pins (githubInfrastructureKeyVersion,
# githubDeployerKeyVersion, githubReleaseKeyVersion) and this layer's apply
# moves. Every reader, this organization's runs and the hosting
# organizations' pipelines, names the alias, never a number and never latest,
# so adding a version changes nothing by itself and one pull request here
# switches every reader at once (README, "Rotating a GitHub App's key"):
#
#   imr-boot-gbl-github-infrastructure-key  the infrastructure GitHub App, whose
#     installation token 1-dns's GitHub provider needs here and the second
#     layer's in each hosting organization. A run of the triggers mints it from the key
#     (cloud-build.tf); a person applying by hand uses their own token.
#   imr-boot-gbl-github-deployer-key  the deployer GitHub App, which the plan trigger's
#     run posts each layer's plan on the pull request as, here and in each
#     hosting organization, and which every application's pipeline talks
#     back on its pull requests as.
#   imr-boot-gbl-github-release-key  the release GitHub App, which cuts every
#     application's releases in the hosting organizations; nothing here reads
#     it.
#
# A key is added by the key operators placement.json names (keyOperators: the
# adding of a version, and the read that proves it before the alias moves),
# never by a layer. Each created hosting organization's boot identity
# (hostingOrganizations, the entries with a folder recorded) holds the policy
# administration of the three secrets and of the Cloud Build token secret
# (cloud-build.tf; recorded once here, read by every organization's
# connections), secretPolicyAdmin: a secret's IAM policy and nothing else,
# which that organization's layers hand down to the identities that read a
# key or a connection's token. That grant is the only reach a hosting
# organization has into this one. The creating identity (bootstrap.tf) holds
# the same on the four, for the first apply of a hosting organization's
# 0-bootstrap, which makes its grants. The dns identity administers the two
# secrets it reads from (and reads the infrastructure key for 1-dns's apply);
# 1-dns's plan identity (bootstrap.tf) reads the infrastructure key too, since
# a plan of 1-dns reads the repository. The runners' reads are cloud-build.tf's.
# No key reaches GitHub.
# ---------------------------------------------------------------------------

locals {
  keys_project = module.boot_project.project_id
  key_secrets = {
    infrastructure = google_secret_manager_secret.github_infrastructure_key.secret_id
    deployer       = google_secret_manager_secret.github_deployer_key.secret_id
    release        = google_secret_manager_secret.github_release_key.secret_id
  }

  # The created hosting organizations that read the keys and the token, each
  # by its boot identity (placement.json, hostingOrganizations with a folder
  # recorded; one without is not created yet, and its identity does not exist).
  hosting_boot_identities = {
    imp = "serviceAccount:imp-boot-gbl-tofu@imp-boot-gbl-core-a1b2.iam.gserviceaccount.com"
  }
  hosting_key_grants = {
    for pair in setproduct(keys(local.hosting_boot_identities), keys(local.key_secrets)) :
    "${pair[0]}__${pair[1]}" => { organization = pair[0], key = pair[1] }
  }

  # The policy administrators of the token secret (cloud-build.tf): the
  # created hosting organizations' boot identities and the creating identity,
  # once the authorization is recorded.
  token_policy_admins = local.connected ? merge(local.hosting_boot_identities, { creation = google_service_account.create_tofu.member }) : {}

  # The key operators (placement.json, keyOperators), each with the adding of
  # a version and the read of one, on each of the three secrets.
  key_operators = toset(["group:key-operators@imp.example"])
  key_operator_grants = {
    for pair in setproduct(local.key_operators, keys(local.key_secrets), ["roles/secretmanager.secretVersionAdder", "roles/secretmanager.secretAccessor"]) :
    "${pair[0]}__${pair[1]}__${pair[2]}" => { member = pair[0], key = pair[1], role = pair[2] }
  }
}

resource "google_secret_manager_secret" "github_infrastructure_key" {
  project   = module.boot_project.project_id
  secret_id = "${var.prefix}-boot-gbl-github-infrastructure-key"

  replication {
    auto {}
  }

  labels          = local.labels
  version_aliases = { current = "1" }
}

resource "google_secret_manager_secret" "github_deployer_key" {
  project   = module.boot_project.project_id
  secret_id = "${var.prefix}-boot-gbl-github-deployer-key"

  replication {
    auto {}
  }

  labels          = local.labels
  version_aliases = { current = "1" }
}

resource "google_secret_manager_secret" "github_release_key" {
  project   = module.boot_project.project_id
  secret_id = "${var.prefix}-boot-gbl-github-release-key"

  replication {
    auto {}
  }

  labels          = local.labels
  version_aliases = { current = "2" }
}

# Reads and sets a secret's IAM policy, nothing else: no payload, no version,
# no change to the secret. The role the hosting organizations' identities
# hold on the three secrets, so each can grant its own identities the read. A
# role in this project, where the secrets are.
resource "google_project_iam_custom_role" "secret_policy_admin" {
  project     = module.boot_project.project_id
  role_id     = "secretPolicyAdmin"
  title       = "Secret Policy Admin"
  description = "Reads and sets a secret's IAM policy, for the organizations whose layers grant their identities the read of a GitHub App's key held here; no payload, no version, no change to the secret."
  permissions = [
    "secretmanager.secrets.get",
    "secretmanager.secrets.getIamPolicy",
    "secretmanager.secrets.setIamPolicy",
  ]
}

resource "google_secret_manager_secret_iam_member" "hosting_boot_tofu_keys" {
  for_each = local.hosting_key_grants

  project   = module.boot_project.project_id
  secret_id = local.key_secrets[each.value.key]
  role      = google_project_iam_custom_role.secret_policy_admin.id
  member    = local.hosting_boot_identities[each.value.organization]
}

resource "google_secret_manager_secret_iam_member" "key_operators" {
  for_each = local.key_operator_grants

  project   = module.boot_project.project_id
  secret_id = local.key_secrets[each.value.key]
  role      = each.value.role
  member    = each.value.member
}

# The creating identity grants on the three secrets while it applies a hosting
# organization's 0-bootstrap the first time (bootstrap.tf).
resource "google_secret_manager_secret_iam_member" "create_tofu_keys" {
  for_each = local.key_secrets

  project   = module.boot_project.project_id
  secret_id = each.value
  role      = google_project_iam_custom_role.secret_policy_admin.id
  member    = google_service_account.create_tofu.member
}

# The token secret's policy administration, for the same reach: a hosting
# organization grants its Cloud Build service agents the token's read
# (cloud-build.tf there), as this layer does here.
resource "google_secret_manager_regional_secret_iam_member" "token_policy_admin" {
  for_each = local.token_regional ? local.token_policy_admins : {}

  project   = local.token_project
  location  = local.token_location
  secret_id = local.token_secret
  role      = google_project_iam_custom_role.secret_policy_admin.id
  member    = each.value
}

resource "google_secret_manager_secret_iam_member" "token_policy_admin" {
  for_each = local.token_regional ? {} : local.token_policy_admins

  project   = local.token_project
  secret_id = local.token_secret
  role      = google_project_iam_custom_role.secret_policy_admin.id
  member    = each.value
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

# A plan of 1-dns reads this repository with the infrastructure app's token,
# minted from the key: the dns plan identity reads that one secret.
resource "google_secret_manager_secret_iam_member" "org_plan_infrastructure_key" {
  project   = module.boot_project.project_id
  secret_id = google_secret_manager_secret.github_infrastructure_key.secret_id
  role      = "roles/secretmanager.secretAccessor"
  member    = google_service_account.boot_plan["dns"].member
}

# The boot identity manages the secrets, their aliases and their policies when
# this layer runs through the apply trigger, and the token secret's policy
# (cloud-build.tf); the break-glass account's Owner covered the first apply.
resource "google_project_iam_member" "boot_tofu_secrets" {
  project = module.boot_project.project_id
  role    = "roles/secretmanager.admin"
  member  = google_service_account.boot_tofu.member
}
