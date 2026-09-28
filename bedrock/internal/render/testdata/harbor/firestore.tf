# ---------------------------------------------------------------------------
# Firestore
#
# The document database: dataConfig.FirestoreDatabase (pkg/config/data.go) names it
# to the processes that construct the data level, and the site and the
# job process read and write it as their own identities, on this database alone:
# the project's Firestore role is granted under a condition naming it, so an
# application in the shared environment project reaches no other application's
# documents. One database per environment beside the Spanner database, a
# pull-request stack's own for a pull request, Native mode, in the primary
# region; prd keeps point-in-time recovery on and resists deletion. The
# migrate command receives neither the name nor a grant: its work is known.
# ---------------------------------------------------------------------------

resource "google_firestore_database" "firestore" {
  project     = local.project_id
  name        = local.firestore_database_id
  location_id = local.primary_region
  type        = "FIRESTORE_NATIVE"

  point_in_time_recovery_enablement = local.is_prd ? "POINT_IN_TIME_RECOVERY_ENABLED" : "POINT_IN_TIME_RECOVERY_DISABLED"
  delete_protection_state           = local.is_prd ? "DELETE_PROTECTION_ENABLED" : "DELETE_PROTECTION_DISABLED"
  deletion_policy                   = "DELETE"
}

# Documents, as the site reads and writes them, on this database only.
resource "google_project_iam_member" "firestore_app" {
  project = local.project_id
  role    = "roles/datastore.user"
  member  = local.app_member

  condition {
    title       = "${local.firestore_database_id} only"
    description = "The application's own Firestore database in this project."
    expression  = "resource.name == \"projects/${local.project_id}/databases/${local.firestore_database_id}\""
  }

  depends_on = [google_firestore_database.firestore, google_service_account.app]
}

resource "google_project_iam_member" "firestore_jobs" {
  project = local.project_id
  role    = "roles/datastore.user"
  member  = local.jobs_member

  condition {
    title       = "${local.firestore_database_id} only"
    description = "The application's own Firestore database in this project."
    expression  = "resource.name == \"projects/${local.project_id}/databases/${local.firestore_database_id}\""
  }

  depends_on = [google_firestore_database.firestore, google_service_account.jobs]
}
