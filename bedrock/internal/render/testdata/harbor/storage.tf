# ---------------------------------------------------------------------------
# Cloud Storage
#
# The file stores: one bucket per file-store variable (APP_FILE_STORE, the
# default store; APP_FILE_STORE_<NAME>, a named one), handed to the processes
# that construct the variable's level as a gs:// URL, and the site and the job
# process read and write its objects as their own identities. One bucket per
# environment, where its database is (the region of a regional instance, a
# dual-region over the two regions of the shared one), uniform access, no
# public access, unversioned, soft delete seven days in prd and none
# elsewhere: what the application keeps in it and for how long is the
# application's. A pull-request stack with its own database has its own
# bucket; one sharing tst's database uses tst's, since
# the rows it reads name objects there. The name ends in the environment
# project's number, unique among every bucket in Google Cloud without a
# random suffix the code would have to learn. The migrate command receives
# neither the URL nor a grant: its work is known.
# ---------------------------------------------------------------------------

# The default file store: dataConfig.FileStore (pkg/config/data.go) names it to the
# processes that construct the data level. The environment's
# own bucket, or a pull-request stack's with its own database; a pull-request
# stack sharing tst's database makes none and uses
# tst's (locals.tf).
resource "google_storage_bucket" "files" {
  count = local.own_database ? 1 : 0

  project  = local.project_id
  name     = local.files_bucket_name
  location = local.files_location

  uniform_bucket_level_access = true
  public_access_prevention    = "enforced"

  # Dual-region over the two regions the shared instance's configuration
  # spans; a regional bucket where the instance is regional (tst).
  dynamic "custom_placement_config" {
    for_each = local.files_regional ? [] : [1]
    content {
      data_locations = [for region in values(local.regions) : upper(region)]
    }
  }

  # Soft delete, set on purpose rather than left to Cloud Storage's default:
  # prd keeps a deleted object restorable for seven days, the
  # recovery window should a cleanup or a release delete a file a row still
  # holds; every other environment and a pull-request stack keep none, since
  # a pipeline rebuilds their files.
  soft_delete_policy {
    retention_duration_seconds = local.is_prd ? 604800 : 0
  }

  # prd keeps its objects through a destroy; every other environment can be
  # rebuilt by a pipeline, and a pull-request stack is torn down with its objects.
  force_destroy = !local.is_prd

  labels = local.labels
}

# The bucket and its policy were unconditional before a pull-request stack
# could share tst's bucket; the state's instances keep their place.
moved {
  from = google_storage_bucket.files
  to   = google_storage_bucket.files[0]
}

moved {
  from = google_storage_bucket_iam_policy.files
  to   = google_storage_bucket_iam_policy.files[0]
}

# In tst, the identities of the application's pull-request
# stacks (harbor-pr<N>-app, -jobs), which share this bucket
# when they share the database: read when this stack is planned, so a
# release's apply keeps their grants in the policy it sets whole. A
# pull-request stack grants its own identities too (below), effective at
# once; one opened between a release's plan and its apply grants itself
# again at its next build.
data "google_service_accounts" "files_previews" {
  count = local.own_database && !local.is_pr && var.environment == "tst" ? 1 : 0

  project = local.project_id
  prefix  = "${local.app}-pr"
}

locals {
  files_preview_members = [for a in try(data.google_service_accounts.files_previews[0].accounts, []) : "serviceAccount:${a.email}" if endswith(a.account_id, "-app") || endswith(a.account_id, "-jobs")]
}

# The bucket's permission list, set whole: the site and the job process on its
# objects (create, read, list and delete, and nothing of the bucket itself),
# in tst the pull-request stacks' identities too, and nobody else,
# the project's basic roles included. Cloud Storage's default grants to those
# roles are not listed, so they are removed, and a grant added on the bucket
# by hand is removed by the next release's apply; a grant added on the
# project is not. This is the one authoritative IAM resource the stack
# declares (bedrock check and the pipeline's test admit it by this address):
# a pull-request stack with its own bucket makes its own policy, which removes
# nobody else's members, and one sharing tst's bucket sets none.
data "google_iam_policy" "files" {
  count = local.own_database ? 1 : 0

  binding {
    role    = "roles/storage.objectUser"
    members = concat([local.app_member, local.jobs_member], local.files_preview_members)
  }
}

resource "google_storage_bucket_iam_policy" "files" {
  count = local.own_database ? 1 : 0

  bucket      = google_storage_bucket.files[0].name
  policy_data = data.google_iam_policy.files[0].policy_data

  depends_on = [google_service_account.app, google_service_account.jobs]

  # A recreated bucket (a restore of the first environment replaces it)
  # starts with Cloud Storage's default grants; the policy is set again with
  # it rather than believed to hold.
  lifecycle {
    replace_triggered_by = [google_storage_bucket.files[0]]
  }
}

# A pull-request stack sharing tst's database: its identities on
# tst's bucket, one member each, effective at once and gone with
# the stack (tst's own policy lists them too, above).
resource "google_storage_bucket_iam_member" "files_preview" {
  for_each = local.own_database ? toset([]) : toset(["app", "jobs"])

  bucket = local.files_bucket_name
  role   = "roles/storage.objectUser"
  member = each.key == "app" ? local.app_member : local.jobs_member
}

# The member resources the policy above replaced, taken out of the state
# without being destroyed. Destroying one would remove its member from the
# bucket's live permission list, which the policy has just set whole, and the
# member would stay missing until the next release's apply set the list again;
# taken out of the state, the member stays, and the policy holds every member
# after one apply. Carried for one bedrock release, so a stack that moves to
# the policy is applied once; dropped in the next release, when no state holds
# them any more.
removed {
  from = google_storage_bucket_iam_member.files_app

  lifecycle {
    destroy = false
  }
}

removed {
  from = google_storage_bucket_iam_member.files_jobs

  lifecycle {
    destroy = false
  }
}
