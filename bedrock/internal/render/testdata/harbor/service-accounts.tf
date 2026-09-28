# ---------------------------------------------------------------------------
# Runtime identities
#
# One service account per process, derived from what the code runs:
#
#   imp-<env>-gbl-harbor-app      main.go, the served site (Cloud Run service)
#   imp-<env>-gbl-harbor-migrate  cmd/deployment/migrate (Cloud Run job)
#   imp-<env>-gbl-harbor-jobs     cmd/jobs, the job process (Cloud Run job)
#
# Each holds what its process needs while it runs and nothing more; the
# migrate identity alone holds database admin, on its own database, for DDL.
# No keys: they run only as the Cloud Run identity of their process.
# ---------------------------------------------------------------------------

resource "google_service_account" "app" {
  project      = local.project_id
  account_id   = local.app_account
  display_name = "Runtime SA - ${local.app_account}"
  description  = "Runtime identity of the harbor site (main.go) in ${var.environment}."
}

resource "google_service_account" "migrate" {
  project      = local.project_id
  account_id   = local.migrate_account
  display_name = "Runtime SA - ${local.migrate_account}"
  description  = "Runtime identity of the harbor migration job (cmd/deployment/migrate) in ${var.environment}."
}

resource "google_service_account" "jobs" {
  project      = local.project_id
  account_id   = local.jobs_account
  display_name = "Runtime SA - ${local.jobs_account}"
  description  = "Runtime identity of the harbor job process (cmd/jobs) in ${var.environment}."
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

# The migration writes logs and, through the Spanner client, its client-side
# metrics (the client logs a denial every minute without metricWriter);
# everything else it holds is on the database.
resource "google_project_iam_member" "migrate" {
  for_each = toset([
    "roles/logging.logWriter",
    "roles/monitoring.metricWriter",
  ])

  project = local.project_id
  role    = each.value
  member  = local.migrate_member

  depends_on = [google_service_account.migrate]
}

# The job process runs the application's own code: logs, traces and metrics
# like the site. Its database and secret grants are on those resources.
resource "google_project_iam_member" "jobs" {
  for_each = toset([
    "roles/logging.logWriter",
    "roles/cloudtrace.agent",
    "roles/monitoring.metricWriter",
  ])

  project = local.project_id
  role    = each.value
  member  = local.jobs_member

  depends_on = [google_service_account.jobs]
}

# The deploy identity from 2-env rolls out revisions and updates the jobs as
# these identities, and as no other application's: Service Account User is
# granted here, on each account, rather than at project level.
resource "google_service_account_iam_member" "deploy_uses_app" {
  service_account_id = local.app_account_name
  role               = "roles/iam.serviceAccountUser"
  member             = local.identities.deploy_identity_member

  depends_on = [google_service_account.app]
}

resource "google_service_account_iam_member" "deploy_uses_migrate" {
  service_account_id = local.migrate_account_name
  role               = "roles/iam.serviceAccountUser"
  member             = local.identities.deploy_identity_member

  depends_on = [google_service_account.migrate]
}

resource "google_service_account_iam_member" "deploy_uses_jobs" {
  service_account_id = local.jobs_account_name
  role               = "roles/iam.serviceAccountUser"
  member             = local.identities.deploy_identity_member

  depends_on = [google_service_account.jobs]
}

# The directory's groups carry role membership, read through the Admin SDK as the
# runtime identity with domain-wide delegation and no key: the service signs a JWT
# for itself as the administrator (APP_STAFF_OIDC_ADMIN_SUBJECT) through the IAM
# Credentials API, which takes Service Account Token Creator on its own account.
# The delegation itself is granted in the Workspace admin console (hand steps).
resource "google_service_account_iam_member" "app_signs_for_itself" {
  service_account_id = local.app_account_name
  role               = "roles/iam.serviceAccountTokenCreator"
  member             = local.app_member

  depends_on = [google_service_account.app]
}
