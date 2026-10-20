# ---------------------------------------------------------------------------
# Load balancer backend
#
# Per site, a serverless network endpoint group per regional service and one
# global backend service over its regions, in the environment project. The URL map that
# routes this environment's hostnames to it lives in the net project (2-net)
# and references the backend across projects; 2-env granted the net project's
# principals compute.loadBalancerServiceUser here for that. Outlier detection
# ejects a region whose service answers errors, and request logging is on at
# full sample rate, the cheapest observability there is. Cloud Armor's policy
# (cloud-armor.tf) is attached where terraform.tfvars turns it on in this
# environment, to this backend and the next revision's alike.
#
# Modeled on CCC's reference deployment.
# ---------------------------------------------------------------------------

resource "google_compute_region_network_endpoint_group" "app" {
  # A pull-request stack has no backend of its own: 2-env's wildcard backend in
  # tst reaches its services by name from the hostname.
  for_each = local.is_pr ? {} : local.services

  project               = local.project_id
  region                = each.value.region
  name                  = "${local.name}-${each.value.code}-${local.app}-${each.value.site}-neg"
  network_endpoint_type = "SERVERLESS"

  cloud_run {
    service = google_cloud_run_v2_service.app[each.key].name
  }
}

resource "google_compute_backend_service" "app" {
  for_each = local.is_pr ? toset([]) : toset(local.sites)

  project     = local.project_id
  name        = "${local.name}-gbl-${local.app}-${each.key}-backend"
  description = "lantrn's ${each.key} site in ${var.environment}: ${join(", ", local.site_hostnames[each.key])}"

  load_balancing_scheme = "EXTERNAL_MANAGED"
  protocol              = "HTTPS"
  security_policy       = local.cloud_armor_on ? google_compute_security_policy.app[0].self_link : null

  dynamic "backend" {
    for_each = { for key, neg in google_compute_region_network_endpoint_group.app : key => neg if local.services[key].site == each.key }
    content {
      group = backend.value.id
    }
  }

  # Five consecutive errors in a one-second window eject the region for
  # thirty seconds, never more than half the backends at once; enforcing 100
  # means every detection counts.
  outlier_detection {
    consecutive_errors           = 5
    enforcing_consecutive_errors = 100
    max_ejection_percent         = 50

    interval {
      seconds = 1
    }

    base_ejection_time {
      seconds = 30
    }
  }

  log_config {
    enable      = true
    sample_rate = 1.0
  }
}

# The next revision's backend: the same services under the revision tag "next",
# which bedrock deploy service puts on each new revision before traffic moves.
# The hostnames <app>-<env>-next reach it through the load balancer (2-net's
# hosts, from this stack's net_hosts output), so a hook before traffic can call
# the new revision at a public URL while the old one still serves; the
# services' ingress admits the load balancer alone, so the revision's own
# tagged run.app URL answers nothing from outside.
resource "google_compute_region_network_endpoint_group" "next" {
  for_each = local.is_pr ? {} : local.services

  project               = local.project_id
  region                = each.value.region
  name                  = "${local.name}-${each.value.code}-${local.app}-${each.value.site}-next-neg"
  network_endpoint_type = "SERVERLESS"

  cloud_run {
    service = google_cloud_run_v2_service.app[each.key].name
    tag     = "next"
  }
}

resource "google_compute_backend_service" "next" {
  for_each = local.is_pr ? toset([]) : toset(local.sites)

  project     = local.project_id
  name        = "${local.name}-gbl-${local.app}-${each.key}-next-backend"
  description = "lantrn's ${each.key} site in ${var.environment}, the next revision before traffic: ${join(", ", local.site_next_hostnames[each.key])}"

  load_balancing_scheme = "EXTERNAL_MANAGED"
  protocol              = "HTTPS"
  security_policy       = local.cloud_armor_on ? google_compute_security_policy.app[0].self_link : null

  dynamic "backend" {
    for_each = { for key, neg in google_compute_region_network_endpoint_group.next : key => neg if local.services[key].site == each.key }
    content {
      group = backend.value.id
    }
  }

  log_config {
    enable      = true
    sample_rate = 1.0
  }
}
