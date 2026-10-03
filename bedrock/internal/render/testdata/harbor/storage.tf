# ---------------------------------------------------------------------------
# Cloud Storage
#
# The file stores: one bucket per file-store variable (APP_FILE_STORE, the
# default store; APP_FILE_STORE_<NAME>, a named one), handed to the processes
# that construct the variable's level as a gs:// URL, and the site and the job
# process read and write its objects as their own identities. One bucket per
# environment, a pull-request stack's own for a pull request, in the primary
# region, uniform access, no public access, unversioned: what the application
# keeps in it and for how long is the application's. The name ends in the
# environment project's number, unique among every bucket in Google Cloud
# without a random suffix the code would have to learn. The migrate command
# receives neither the URL nor a grant: its work is known.
# ---------------------------------------------------------------------------

# The default file store: dataConfig.FileStore (pkg/config/data.go) names it to the
# processes that construct the data level.
resource "google_storage_bucket" "files" {
  project  = local.project_id
  name     = local.files_bucket_name
  location = local.primary_region

  uniform_bucket_level_access = true
  public_access_prevention    = "enforced"

  # prd keeps its objects through a destroy; every other environment can be
  # rebuilt by a pipeline, and a pull-request stack is torn down with its objects.
  force_destroy = !local.is_prd

  labels = local.labels
}

# Objects, as the site reads and writes them: create, read, list and delete,
# and nothing of the bucket itself.
resource "google_storage_bucket_iam_member" "files_app" {
  bucket = google_storage_bucket.files.name
  role   = "roles/storage.objectUser"
  member = local.app_member

  depends_on = [google_service_account.app]

  # A recreated bucket (a restore of the first environment replaces it)
  # starts with no members; the membership is recreated with it rather than
  # believed to exist.
  lifecycle {
    replace_triggered_by = [google_storage_bucket.files]
  }
}

resource "google_storage_bucket_iam_member" "files_jobs" {
  bucket = google_storage_bucket.files.name
  role   = "roles/storage.objectUser"
  member = local.jobs_member

  depends_on = [google_service_account.jobs]

  lifecycle {
    replace_triggered_by = [google_storage_bucket.files]
  }
}
