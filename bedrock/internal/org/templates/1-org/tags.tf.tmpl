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

# One key per environment project, created in it: a tag key under a project
# is attached within that project, which is where the services are, and
# nothing of this organization's sits at the organization node, whose names
# are shared by every organization under it and held after a delete. The
# parent is the project's number, the form Google records.
resource "google_tags_tag_key" "public_invoker" {
  for_each = local.environment_layers

  parent      = "projects/${module.project[each.key].project_number}"
  short_name  = "public-invoker"
  description = "true on a Cloud Run service that may grant roles/run.invoker to allUsers, for the load balancer; see org-policies.tf."

  depends_on = [time_sleep.apis_ready]
}

resource "google_tags_tag_value" "public_invoker" {
  for_each = local.environment_layers

  parent      = google_tags_tag_key.public_invoker[each.key].id
  short_name  = "true"
  description = "The service may be invoked without an identity; its ingress keeps the callers to the load balancer."
}

# Each application's apply identity in every environment it is placed in may
# bind that environment's value to its own Cloud Run services
# (public-invokers.auto.tfvars, rendered from placement.json for the
# applications whose registration is past their identities, after 2-env has
# created them: a grant on an identity that does not exist is refused).
resource "google_tags_tag_value_iam_member" "public_invoker" {
  for_each = {
    for pair in flatten([
      for app, envs in var.public_invokers : [
        for env, member in envs : { key = "${app}__${env}", env = env, member = member }
      ]
    ]) : pair.key => pair
  }

  tag_value = google_tags_tag_value.public_invoker[each.value.env].id
  role      = "roles/resourcemanager.tagUser"
  member    = each.value.member
}
