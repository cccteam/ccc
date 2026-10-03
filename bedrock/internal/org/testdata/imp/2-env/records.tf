# ---------------------------------------------------------------------------
# Deployment records
#
# One bucket per environment where every deploy writes what it did: the
# release, the image digests, the migration outcome, the approval decision.
# Environments chain by these records rather than by one environment's
# identity holding build-create in the next, so the bucket is the seam
# between the tst deploy and the stg pipeline.
#
# The bucket's permission list is set whole (the policy below), so it holds
# exactly what this layer names and nothing a project role implies: the
# environment's deploy identities create and read objects (a record is
# written once and never rewritten, and versioning keeps the history if one
# ever is), its application plan identities read them (a pull-request build
# against a hotfix line previews each environment's release check from its
# live record), the next environment's deploy identities read them (the
# record gate), and the environment's team group reads them (a person reads
# a record through the group, never through a project role). Cloud Storage's
# default grants to the project's basic roles (projectOwner, projectEditor
# and projectViewer) are not listed, so they are removed, and a grant added
# on the bucket by hand is removed by the next apply; a grant added on the
# project is not. This layer's own identity reaches the bucket through its
# storage admin, which 1-org bounds to this bucket's name; the applications'
# apply identities, bounded to their own buckets (identities.tf), do not.
#
# The name carries a random four-hex suffix because bucket names are global
# and permanent, as with the state bucket. US multi-region, since nothing
# reads it on a latency budget and gbl in the name should mean what it says.
# ---------------------------------------------------------------------------

resource "random_id" "records" {
  byte_length = 2
}

resource "google_storage_bucket" "records" {
  project  = local.project_id
  name     = "${local.name}-gbl-records-${random_id.records.hex}"
  location = "US"

  uniform_bucket_level_access = true
  public_access_prevention    = "enforced"

  versioning {
    enabled = true
  }

  labels = local.labels
}

# The bucket's bindings, one per role, with every member of each; a role
# nobody holds has no binding (an environment with no application registered
# has no deploy identity yet). The next environment's deploy identities are
# empty after the last environment and until the next environment's 2-env has
# been applied (locals.tf), which is why a registration applies this layer
# twice.
locals {
  records_deploy_members = [for app in var.applications : google_service_account.deploy[app].member]
  records_plan_members   = [for app in var.applications : google_service_account.plan[app].member]
  records_bindings = [
    for b in [
      { role = "roles/storage.objectCreator", members = local.records_deploy_members },
      { role = "roles/storage.objectViewer", members = concat(local.records_deploy_members, local.records_plan_members, values(local.next_deploy_members), [local.team_group]) },
    ] : b if length(b.members) > 0
  ]
}

data "google_iam_policy" "records" {
  dynamic "binding" {
    for_each = local.records_bindings
    content {
      role    = binding.value.role
      members = binding.value.members
    }
  }
}

resource "google_storage_bucket_iam_policy" "records" {
  bucket      = google_storage_bucket.records.name
  policy_data = data.google_iam_policy.records.policy_data
}
