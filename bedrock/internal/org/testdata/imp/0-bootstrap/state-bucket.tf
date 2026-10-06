# ---------------------------------------------------------------------------
# The state bucket's grants
#
# Every layer's state lives in the seeded bucket, one prefix per layer. An
# apply identity reads and writes its own prefix (roles/storage.objectUser
# under a condition on the object name), reads the upstream states its layer
# takes outputs from (roles/storage.objectViewer, conditioned the same way)
# and lists the bucket (roles/storage.legacyBucketReader; a list is a request
# on the bucket and cannot be conditioned). A plan identity reads and never
# writes, so a plan runs without the state lock.
#
# Who grants what: IAM refuses a binding for a service account that does not
# exist, so each layer grants on the bucket for the identities it creates.
# This layer grants for its own two, the boot identity the seed made and the
# org identity made here, both of which exist by the time the grant applies,
# and gives both the bucket's policy authority (the custom role below): the
# boot identity because it applies this layer through the workflow, the org
# identity so 1-org can grant for the fourteen identities it creates and hand
# the environment layer identities the same authority for the application
# identities 2-env creates. The first-time order holds by itself: the seed
# makes the bucket, this layer grants on it as the bootstrap administrator,
# 1-org grants on it as the same person, and from then on each layer's own
# identity holds what its grants need.
#
# The bucket itself is seeded and stays outside OpenTofu (bootstrap.tf).
# ---------------------------------------------------------------------------

locals {
  state_bucket_objects = "projects/_/buckets/${var.state_bucket}/objects"

  # The two identities this layer owns, with the state prefix each one applies.
  own_identities = {
    boot = { member = google_service_account.boot_tofu.member, prefix = "0-bootstrap" }
    org  = { member = google_service_account.org_tofu.member, prefix = "1-org" }
  }
}

# Reads and sets a bucket's IAM policy and nothing else about it: what a layer
# that grants on the state bucket for identities it creates holds there.
# roles/storage.admin would carry every object with it. Whoever sets a
# bucket's policy could grant themselves the rest; the role names the intent
# and keeps the day-to-day reach at the prefixes granted.
resource "google_organization_iam_custom_role" "state_bucket_policy_admin" {
  org_id      = local.org_id
  role_id     = "${var.prefix}GblStateBucketPolicyAdmin"
  title       = "State Bucket Policy Admin"
  description = "Reads and sets a bucket's IAM policy, for the layers that grant state-bucket access to the identities they create."
  permissions = [
    "storage.buckets.get",
    "storage.buckets.getIamPolicy",
    "storage.buckets.setIamPolicy",
  ]
}

resource "google_storage_bucket_iam_member" "policy_admin" {
  for_each = local.own_identities

  bucket = var.state_bucket
  role   = google_organization_iam_custom_role.state_bucket_policy_admin.id
  member = each.value.member
}

resource "google_storage_bucket_iam_member" "state_list" {
  for_each = local.own_identities

  bucket = var.state_bucket
  role   = "roles/storage.legacyBucketReader"
  member = each.value.member
}

resource "google_storage_bucket_iam_member" "state_own" {
  for_each = local.own_identities

  bucket = var.state_bucket
  role   = "roles/storage.objectUser"
  member = each.value.member

  condition {
    title       = "${var.prefix}-${each.key}-gbl-state"
    description = "The layer's own state prefix."
    expression  = "resource.name.startsWith(\"${local.state_bucket_objects}/${each.value.prefix}/\")"
  }
}

# 1-org reads this layer's state: the pool its bindings name, the policy role
# it grants, the key container it grants the read of.
resource "google_storage_bucket_iam_member" "org_tofu_state_upstream" {
  bucket = var.state_bucket
  role   = "roles/storage.objectViewer"
  member = google_service_account.org_tofu.member

  condition {
    title       = "${var.prefix}-org-gbl-upstream-state"
    description = "The upstream state 1-org reads outputs from."
    expression  = "resource.name.startsWith(\"${local.state_bucket_objects}/0-bootstrap/\")"
  }
}
