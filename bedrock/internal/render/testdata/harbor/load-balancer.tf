# ---------------------------------------------------------------------------
# Load balancer backend
#
# A serverless network endpoint group per regional service and one global
# backend service over both, in the environment project. The URL map that
# routes this environment's hostnames to it lives in the net project (2-net)
# and references the backend across projects; 2-env granted the net project's
# principals compute.loadBalancerServiceUser here for that. Outlier detection
# ejects a region whose service answers errors (JOURNAL.md: outlier detection
# on), and request logging is on at full sample rate, which for a lab site is
# the cheapest observability there is. No Cloud Armor (opt-in, off).
#
# Modeled on CCC's reference deployment.
# ---------------------------------------------------------------------------

resource "google_compute_region_network_endpoint_group" "app" {
  for_each = local.regions

  project               = local.project_id
  region                = each.value
  name                  = "${local.name}-${each.key}-${local.app}-neg"
  network_endpoint_type = "SERVERLESS"

  cloud_run {
    service = google_cloud_run_v2_service.app[each.key].name
  }
}

resource "google_compute_backend_service" "app" {
  project     = local.project_id
  name        = "${local.name}-gbl-${local.app}-backend"
  description = "harbor site in ${var.environment}: ${join(", ", local.hostnames)}"

  load_balancing_scheme = "EXTERNAL_MANAGED"
  protocol              = "HTTPS"

  dynamic "backend" {
    for_each = google_compute_region_network_endpoint_group.app
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
