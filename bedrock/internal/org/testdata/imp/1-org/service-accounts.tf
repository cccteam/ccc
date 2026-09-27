# ---------------------------------------------------------------------------
# Per-layer identities
#
# Two service accounts per project layer, both living in the project they act
# on. The layer identity ({prefix}-{env}-gbl-tofu) applies the project's own
# layer and holds the roles of its role set in var.layer_roles. The plan
# identity ({prefix}-{env}-gbl-plan) is read-only: roles/viewer and
# roles/browser, so a plan can run from a pull request without the power to
# apply. Both run from Cloud Build in the boot project once the runner is
# wired; neither has keys, and org policy forbids creating any.
# ---------------------------------------------------------------------------

resource "google_service_account" "tofu" {
  for_each = local.layers

  project      = module.project[each.key].project_id
  account_id   = "${local.layer_names[each.key]}-tofu"
  display_name = "OpenTofu SA - ${local.layer_names[each.key]}"
  description  = "Layer identity for ${local.layer_names[each.key]}. Applies the layer from Cloud Build in the boot project."

  depends_on = [time_sleep.apis_ready]
}

resource "google_service_account" "plan" {
  for_each = local.layers

  project      = module.project[each.key].project_id
  account_id   = "${local.layer_names[each.key]}-plan"
  display_name = "OpenTofu plan SA - ${local.layer_names[each.key]}"
  description  = "Read-only plan identity for ${local.layer_names[each.key]}. Plans the layer from Cloud Build in the boot project and never applies."

  depends_on = [time_sleep.apis_ready]
}

resource "google_project_iam_member" "tofu" {
  # checkov:skip=CKV_GCP_49: these are automation identities, not human accounts. Roles come from var.layer_roles rather than roles/owner and are scoped to a single project; the identity has no keys and runs only from Cloud Build in the boot project.
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

resource "google_project_iam_member" "plan" {
  for_each = {
    for pair in setproduct(keys(local.layers), ["roles/browser", "roles/viewer"]) :
    "${pair[0]}__${pair[1]}" => { proj = pair[0], role = pair[1] }
  }

  project = module.project[each.value.proj].project_id
  role    = each.value.role
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
