# ---------------------------------------------------------------------------
# Cross-project load balancing
#
# The global external Application Load Balancer, its URL map, certificates,
# and DNS live in the net project (2-net). Each application stack creates its
# own backend service with serverless NEGs in the environment project, and the
# URL map in net references it across projects. Google Cloud requires
# compute.backendServices.use in the backend's project for two principals in
# the net project: the identity that writes the URL map, and the Compute
# Engine service agent that serves traffic. 2-net publishes both members
# (load_balancer_service_user, compute_service_agent); the same values are
# derived from 1-org when 2-net is not applied yet, so nothing here waits on
# it. The service agent is belt and braces: the documentation asks only for
# the identity that writes the URL map, the reference deployment grants both. Modeled on
# CCC's reference deployment.
#
# Apply order: this layer before 2-net picks up a new backend in its hosts.
# ---------------------------------------------------------------------------

resource "google_project_iam_member" "net_lb_tofu" {
  project = local.project_id
  role    = "roles/compute.loadBalancerServiceUser"
  member  = local.net_lb_service_user
}

resource "google_project_iam_member" "net_lb_compute_agent" {
  project = local.project_id
  role    = "roles/compute.loadBalancerServiceUser"
  member  = local.net_compute_agent
}
