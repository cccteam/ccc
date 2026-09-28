# ---------------------------------------------------------------------------
# Cloud Storage
#
# The assets bucket: dataConfig.AssetsBucket (pkg/config/data.go) names it to the
# processes that construct the data level, and the site and the job
# process read and write its objects as their own identities. One bucket per
# environment, a pull-request stack's own for a pull request, in the primary
# region, uniform access, no public access, unversioned: what the application
# keeps in it and for how long is the application's. The name ends in the
# environment project's number, unique among every bucket in Google Cloud
# without a random suffix the code would have to learn. The migrate command
# receives neither the name nor a grant: its work is known.
# ---------------------------------------------------------------------------

resource "google_storage_bucket" "assets" {
  project  = local.project_id
  name     = local.assets_bucket_name
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
resource "google_storage_bucket_iam_member" "assets_app" {
  bucket = google_storage_bucket.assets.name
  role   = "roles/storage.objectUser"
  member = local.app_member

  depends_on = [google_service_account.app]
}

resource "google_storage_bucket_iam_member" "assets_jobs" {
  bucket = google_storage_bucket.assets.name
  role   = "roles/storage.objectUser"
  member = local.jobs_member

  depends_on = [google_service_account.jobs]
}
