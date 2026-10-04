# ---------------------------------------------------------------------------
# Projects
#
# Driven entirely by var.projects (plus the log project when central logging
# is on), so provisioning a new project is a variable change rather than a code
# change. See the variable description in variables.tf for the shape of an
# entry.
#
# random_project_id is on because project IDs are globally unique and permanent:
# a name collision with any other organization would otherwise block the apply,
# and the ID can never be changed afterwards.
#
# Every project carries a lien, which refuses the project's deletion to anyone
# until the lien is removed. Deleting the network project would also lose the
# domains registered in it, which cannot move to another project. Removing a
# project therefore starts with removing its lien (README.md, "Removing a
# project").
# ---------------------------------------------------------------------------

module "project" {
  source  = "terraform-google-modules/project-factory/google"
  version = "18.3.0"

  for_each = local.projects

  name              = "${var.prefix}-${each.key}-gbl-core"
  folder_id         = google_folder.this[each.value.folder].folder_id
  billing_account   = var.billing_account_id
  random_project_id = true

  # No per-project Terraform service account and no default compute service
  # account: the only identities that act in a project are the layer and plan
  # identities created in service-accounts.tf.
  create_project_sa       = false
  default_service_account = "delete"

  disable_dependent_services = true
  deletion_policy            = "PREVENT"
  lien                       = true
  activate_apis              = var.required_apis[each.value.api_set]

  labels = merge(local.labels, { environment = each.key })
}

# Enabling a service returns before its API is consistently available, and the
# IAM and resource calls that follow will fail intermittently without a pause.
resource "time_sleep" "apis_ready" {
  depends_on = [module.project]

  create_duration = "60s"
}
