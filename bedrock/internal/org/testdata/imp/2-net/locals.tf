locals {
  # Outputs of 1-org. Nothing there is a secret; see its outputs.tf.
  org = data.terraform_remote_state.org.outputs

  environment    = "net"
  prefix         = local.org.prefix
  project_id     = local.org.project_ids[local.environment]
  project_number = local.org.project_numbers[local.environment]

  # Naming convention: {prefix}-{environment}-{region}-{purpose}. Everything
  # in this layer is global (a global load balancer, Certificate Manager,
  # Cloud DNS), so every name carries gbl. None of these names is globally
  # unique, so none carries a random suffix.
  name_prefix = "${local.prefix}-${local.environment}-gbl"

  labels = {
    terraform             = "true"
    terraform_source_path = "2-net"
    source_repo           = "imp-impulse-infrastructure"
    environment           = local.environment
    bedrock-lab           = "true"
  }

  # The certificate covers the apex and one level below it, which is every
  # hostname the convention produces: app.domain, app-stg.domain,
  # app-tst.domain, prN-app-tst.domain.
  wildcard = "*.${var.apps_domain}"

  # One path matcher per host in the URL map, named after the host: dots and
  # the wildcard character are not allowed in a path matcher name, hyphens
  # are. The keys of var.hosts are validated to keep the result inside
  # [a-z0-9-] and 63 characters.
  path_matchers = {
    for host, _ in var.hosts : host => replace(replace(host, "*", "wildcard"), ".", "-")
  }

  # The identity that writes the URL map, and therefore the one that needs
  # compute.backendServices.use on every backend service it references. It is
  # granted roles/compute.loadBalancerServiceUser in each environment project
  # by that project's own layer; this layer publishes the member so those
  # layers bind it from remote state rather than spelling it out.
  load_balancer_service_user = "serviceAccount:${local.org.layer_service_accounts[local.environment]}"

  # The Compute Engine service agent of this project. Not required by the
  # documentation for cross-project service referencing; published because
  # the reference deployment grants it the same role alongside the layer identity, and an
  # environment layer that wants that belt and braces can bind it too.
  compute_service_agent = "serviceAccount:service-${local.project_number}@compute-system.iam.gserviceaccount.com"
}
