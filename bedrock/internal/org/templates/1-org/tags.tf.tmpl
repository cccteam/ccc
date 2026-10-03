# ---------------------------------------------------------------------------
# Tags
#
# A new organization comes with Google's default policies, and one of them,
# iam.allowedPolicyMemberDomains, refuses allUsers in any IAM policy. A Cloud
# Run service behind the load balancer needs exactly that (roles/run.invoker
# for allUsers; ingress keeps the callers to the load balancer), so the
# environment folders carry a copy of the policy with one exception: a
# resource tagged public-invoker=true may name allUsers. The tag is bound by
# the application layer to its own services and to nothing else, and the
# application apply identity may use the value: the grant below, made here
# where the tag is owned, from var.public_invokers.
# ---------------------------------------------------------------------------

resource "google_tags_tag_key" "public_invoker" {
  parent      = local.org_name
  short_name  = "public-invoker"
  description = "true on a Cloud Run service that may grant roles/run.invoker to allUsers, for the load balancer; see org-policies.tf."
}

resource "google_tags_tag_value" "public_invoker" {
  parent      = google_tags_tag_key.public_invoker.id
  short_name  = "true"
  description = "The service may be invoked without an identity; its ingress keeps the callers to the load balancer."
}

# Each application's apply identity in every environment may bind the value to
# its own Cloud Run services (public-invokers.auto.tfvars, rendered from
# placement.json once 2-env has created the identities: a grant on an identity
# that does not exist is refused).
resource "google_tags_tag_value_iam_member" "public_invoker" {
  for_each = toset(flatten(values(var.public_invokers)))

  tag_value = google_tags_tag_value.public_invoker.id
  role      = "roles/resourcemanager.tagUser"
  member    = each.value
}
