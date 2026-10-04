# ---------------------------------------------------------------------------
# Per-layer identities
#
# Two service accounts per project layer, both living in the project they act
# on. The layer identity ({prefix}-{env}-gbl-tofu) applies the project's own
# layer and holds the roles of its role set in var.layer_roles. The plan
# identity ({prefix}-{env}-gbl-plan) is read-only and holds the roles of its
# role set in var.plan_roles, so a plan can run from a pull request without
# the power to apply: the custom organization role of its project's kind
# (custom-roles.tf), which reads the resource types the layer declares and
# nothing of their data, and roles/iam.securityReviewer for the IAM policies
# the layer's grants are refreshed through. roles/viewer is not among them:
# the cloud's bundle for a reader reads data as well as resources (the rows
# of every Spanner database in the project, every container image, and the
# objects of any bucket that still carries Cloud Storage's default grants to
# the project's basic roles), none of which a plan reads. Both run from the
# infrastructure repository's layers workflow, signed in through the boot
# project's identity pool (workflow.tf); neither has keys, and org policy
# forbids creating any.
# ---------------------------------------------------------------------------

resource "google_service_account" "tofu" {
  for_each = local.layers

  project      = module.project[each.key].project_id
  account_id   = "${local.layer_names[each.key]}-tofu"
  display_name = "OpenTofu SA - ${local.layer_names[each.key]}"
  description  = "Layer identity for ${local.layer_names[each.key]}. Applies the layer from the infrastructure repository's workflow."

  depends_on = [time_sleep.apis_ready]
}

resource "google_service_account" "plan" {
  for_each = local.layers

  project      = module.project[each.key].project_id
  account_id   = "${local.layer_names[each.key]}-plan"
  display_name = "OpenTofu plan SA - ${local.layer_names[each.key]}"
  description  = "Read-only plan identity for ${local.layer_names[each.key]}. Plans the layer on a pull request of the infrastructure repository and never applies."

  depends_on = [time_sleep.apis_ready]
}

resource "google_project_iam_member" "tofu" {
  # checkov:skip=CKV_GCP_49: these are automation identities, not human accounts. Roles come from var.layer_roles rather than roles/owner and are scoped to a single project; the identity has no keys and runs only from the infrastructure repository's workflow.
  for_each = {
    for pair in flatten([
      for key, cfg in local.layers : [
        for role in var.layer_roles[cfg.role_set] : {
          key  = "${key}__${role}"
          proj = key
          role = role
        }
      ]
    ]) : pair.key => pair
  }

  project = module.project[each.value.proj].project_id

  # A bare ID names a custom role from custom-roles.tf; a predefined role
  # passes through unchanged. The for_each key keeps the bare ID so it is known
  # before the custom role exists.
  role   = lookup(local.custom_roles, each.value.role, each.value.role)
  member = google_service_account.tofu[each.value.proj].member
}

# Cloud Storage on an environment project, for its layer identity: 2-env
# declares one bucket there, the deployment records
# ({prefix}-{env}-gbl-records-<hex>), and nothing else in Cloud Storage, so
# its storage admin is held under a condition naming that bucket, with its
# objects, and not the applications' file stores in the same project, which
# are each application's apply identity's (2-env bounds those the same way).
# Creating a bucket is checked on the project, which no bucket's name can
# admit: that is storageBucketCreator in the app role set (above), beside
# this. A grant added on the records bucket by hand is removed by 2-env's
# next apply, which sets the bucket's policy whole; a grant on the project
# is not.
locals {
  records_bucket_conditions = { for k, v in local.environment_layers : k => "resource.name.startsWith(\"projects/_/buckets/${local.layer_names[k]}-records-\")" }
}

resource "google_project_iam_member" "tofu_storage_admin" {
  for_each = local.environment_layers

  project = module.project[each.key].project_id
  role    = "roles/storage.admin"
  member  = google_service_account.tofu[each.key].member

  condition {
    title       = "${each.key} records bucket"
    description = "The environment's deployment-records bucket, the one bucket 2-env declares, with its objects."
    expression  = local.records_bucket_conditions[each.key]
  }
}

resource "google_project_iam_member" "plan" {
  for_each = {
    for pair in flatten([
      for key, cfg in local.layers : [
        for role in var.plan_roles[cfg.role_set] : {
          key  = "${key}__${role}"
          proj = key
          role = role
        }
      ]
    ]) : pair.key => pair
  }

  project = module.project[each.value.proj].project_id
  role    = lookup(local.custom_roles, each.value.role, each.value.role)
  member  = google_service_account.plan[each.value.proj].member
}

# Every layer uses the boot project as its quota and billing project via
# user_project_override, which requires serviceUsageConsumer on that project.
# The plan identity makes the same API calls, read-only, so it needs the same.
resource "google_project_iam_member" "tofu_quota" {
  for_each = local.layers

  project = var.boot_project_id
  role    = "roles/serviceusage.serviceUsageConsumer"
  member  = google_service_account.tofu[each.key].member
}

resource "google_project_iam_member" "plan_quota" {
  for_each = local.layers

  project = var.boot_project_id
  role    = "roles/serviceusage.serviceUsageConsumer"
  member  = google_service_account.plan[each.key].member
}
