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
# metrics. The traces go through the Telemetry API (telemetry.tracesWriter;
# tracer 0.2 and later) or the Cloud Trace API (cloudtrace.agent; tracer
# before 0.2), so both are granted. Its database and secret grants are on
# those resources (spanner.tf, secret-manager.tf), and so is the metric
# writer role on the shared Spanner instance's project, where its Spanner
# client's metrics go in an environment on that instance (spanner.tf).
resource "google_project_iam_member" "app" {
  for_each = toset([
    "roles/logging.logWriter",
    "roles/cloudtrace.agent",
    "roles/telemetry.tracesWriter",
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

# The migrate command runs on the build worker as the deploy identity (the pipeline
# takes it out of the release's image): its output is the build's log, and the
# Spanner client writes its client-side metrics, which log a denial every minute
# without metricWriter on the instance's project (this one on an environment's
# own instance; spanner.tf grants the role on the shared instance's project).
# The identity is 2-env's, so nothing here is depended on.
# The environment's stack alone grants it: a pull-request build runs as the same
# identity, which the environment's grant covers, and a pull-request stack applies
# only what is the pull request's own.
resource "google_project_iam_member" "deploy_metrics" {
  count = local.is_pr ? 0 : 1

  project = local.project_id
  role    = "roles/monitoring.metricWriter"
  member  = local.identities.deploy_identity_member
}
