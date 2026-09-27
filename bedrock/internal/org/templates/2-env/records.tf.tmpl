# ---------------------------------------------------------------------------
# Deployment records
#
# One bucket per environment where every deploy writes what it did: the
# release, the image digests, the migration outcome, the approval decision.
# Environments chain by these records rather than by one environment's
# identity holding build-create in the next, so the bucket is the seam
# between the tst deploy and the stg pipeline. Deploy identities may only
# create objects (identities.tf): a record is written once and never
# rewritten, and versioning keeps the history if one ever is.
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
