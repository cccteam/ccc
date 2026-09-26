# ---------------------------------------------------------------------------
# Secret containers
#
# One per secret the code declares (local.secrets, from pkg/config/data.go),
# named imp-<env>-gbl-harbor-<kebab name>. Containers only: the application
# apply identity holds secretContainerAdmin, which can create a secret and set
# its policy but neither add nor read a version. An operator adds the value
# as a version (Secret Version Adder on the environment's secrets), and
# var.secret_versions pins which version the environment runs. Automatic
# replication: nothing here has a residency requirement.
#
# Modeled on CCC's reference deployment, one resource per
# secret there, one for_each here.
# ---------------------------------------------------------------------------

resource "google_secret_manager_secret" "harbor" {
  for_each = local.secrets

  project   = local.project_id
  secret_id = "${local.name}-gbl-${local.app}-${each.value.name}"

  replication {
    auto {}
  }

  labels = merge(local.labels, {
    # The variable the secret feeds, so a value can be traced back to the field.
    variable = lower(each.key)
  })
}

# Accessor for the site, which is the only process that reads a secret value
# (see the readers note in locals.tf). The deploy identity never holds this:
# a runtime secret is not a build-time one.
resource "google_secret_manager_secret_iam_member" "app_accessor" {
  for_each = local.secrets

  project   = local.project_id
  secret_id = google_secret_manager_secret.harbor[each.key].secret_id
  role      = "roles/secretmanager.secretAccessor"
  member    = google_service_account.app.member
}
