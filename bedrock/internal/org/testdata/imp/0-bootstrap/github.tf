# ---------------------------------------------------------------------------
# The GitHub Apps' keys
#
# The three GitHub Apps' private keys live in the zones organization
# imz's boot project (imz-boot-gbl-core-c3d4; keys in placement.json), one
# secret per app for every bedrock organization in the Google Cloud
# organization, and every reader here names a secret's alias "current", which
# that organization's placement pins and its apply moves: a key is never added
# or pinned in this repository, and a rotation there switches every reader at
# once (that organization's 0-bootstrap/README.md, "Rotating a GitHub App's
# key"):
#
#   imz-boot-gbl-github-infrastructure-key  the infrastructure GitHub App, whose
#     installation token 1-org's GitHub provider needs, with an organization
#     owner's rights over the applications' repositories. A run of the
#     triggers mints it from the key (cloud-build.tf) for a plan or an apply
#     of 1-org; a person applying 1-org by hand uses their own token.
#   imz-boot-gbl-github-deployer-key  the deployer GitHub App, which the plan trigger's
#     run posts each layer's plan on the pull request as, and which every
#     application's pipeline talks back on its pull requests as: 2-env
#     carries the alias to the applications through its output, and grants
#     their identities the read.
#   imz-boot-gbl-github-release-key  the release GitHub App, which cuts every
#     application's releases: each application's release trigger, in tst's
#     Cloud Build as the application's release identity, mints the app's
#     token from the key and runs release-please. 2-env carries the alias
#     through its output and grants the release identities the read.
#
# This layer makes no secret. It grants on those three what its own
# identities and 1-org's need, with the policy administration the zones
# organization gave this organization's boot identity (secretPolicyAdmin, a
# role of that project: a secret's IAM policy and nothing else), which is the
# only reach this organization has into that one: the org identity gets the
# deployer and release secrets' policy administration, which 1-org hands to
# each environment layer identity so 2-env can grant the applications'
# identities their read; 1-org's plan identity reads the infrastructure key,
# since a plan of 1-org reads the repositories; both boot plan identities read
# the three secrets' policies, which a plan of this layer or of 1-org
# refreshes the grants through. The runners' reads are cloud-build.tf's. No
# key reaches GitHub.
# ---------------------------------------------------------------------------

locals {
  keys_project = "imz-boot-gbl-core-c3d4"
  key_secrets = {
    infrastructure = "imz-boot-gbl-github-infrastructure-key"
    deployer       = "imz-boot-gbl-github-deployer-key"
    release        = "imz-boot-gbl-github-release-key"
  }
  keys_policy_admin_role = "projects/imz-boot-gbl-core-c3d4/roles/secretPolicyAdmin"
  boot_plan_key_reads = {
    for pair in setproduct(keys(local.boot_plans), keys(local.key_secrets)) :
    "${pair[0]}__${pair[1]}" => { plan = pair[0], key = pair[1] }
  }
}

resource "google_secret_manager_secret_iam_member" "org_tofu_keys" {
  for_each = toset(["deployer", "release"])

  project   = local.keys_project
  secret_id = local.key_secrets[each.key]
  role      = local.keys_policy_admin_role
  member    = google_service_account.org_tofu.member
}

# A plan of 1-org reads the applications' repositories with the infrastructure
# app's token, minted from the key: the org plan identity reads that one secret.
resource "google_secret_manager_secret_iam_member" "org_plan_infrastructure_key" {
  project   = local.keys_project
  secret_id = local.key_secrets.infrastructure
  role      = "roles/secretmanager.secretAccessor"
  member    = google_service_account.boot_plan["org"].member
}

# The viewer role reads a secret and its versions' metadata and its policy,
# never a payload.
resource "google_secret_manager_secret_iam_member" "boot_plan_keys" {
  for_each = local.boot_plan_key_reads

  project   = local.keys_project
  secret_id = local.key_secrets[each.value.key]
  role      = "roles/secretmanager.viewer"
  member    = google_service_account.boot_plan[each.value.plan].member
}

# The boot identity manages the token secret's policy (cloud-build.tf) when
# this layer runs through the apply trigger; the break-glass account's Owner
# covered the first apply.
resource "google_project_iam_member" "boot_tofu_secrets" {
  project = module.boot_project.project_id
  role    = "roles/secretmanager.admin"
  member  = google_service_account.boot_tofu.member
}
