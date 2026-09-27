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
# application apply identity may use the value (2-env grants tagUser).
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
