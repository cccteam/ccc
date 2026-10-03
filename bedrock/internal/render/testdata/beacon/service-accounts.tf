# ---------------------------------------------------------------------------
# Runtime identities
#
# One service account per deployed process, derived from what the code runs:
#
#   imp-<env>-gbl-beacon-app  main.go, the served site (Cloud Run service)
#
# Each holds what its process needs while it runs and nothing more. No keys:
# they run only as the Cloud Run identity of their process. The migration
# (cmd/deployment/migrate) has no identity of its own: the pipeline runs the migrate
# command on the build worker as the deploy identity from 2-env, which this
# stack grants database admin on this application's own database alone
# (spanner.tf).
# ---------------------------------------------------------------------------

resource "google_service_account" "app" {
  project      = local.project_id
  account_id   = local.app_account
  display_name = "Runtime SA - ${local.app_account}"
  description  = "Runtime identity of the beacon site (main.go) in ${var.environment}."
}

# The site writes request logs (coreConfig.LoggingProjectID), traces, and
# metrics. Its database and secret grants are on those resources
# (spanner.tf, secret-manager.tf).
resource "google_project_iam_member" "app" {
  for_each = toset([
    "roles/logging.logWriter",
    "roles/cloudtrace.agent",
    "roles/monitoring.metricWriter",
  ])

  project = local.project_id
  role    = each.value
  member  = local.app_member

  depends_on = [google_service_account.app]
}

# The deploy identity from 2-env rolls out revisions as these identities, and as no other application's: Service
# Account User is granted here, on each account, rather than at project level.
resource "google_service_account_iam_member" "deploy_uses_app" {
  service_account_id = local.app_account_name
  role               = "roles/iam.serviceAccountUser"
  member             = local.identities.deploy_identity_member

  depends_on = [google_service_account.app]
}
