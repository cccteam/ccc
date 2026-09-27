# ---------------------------------------------------------------------------
# The one global external Application Load Balancer
#
# Frontend only: a static address, HTTPS with the certificate map from
# certificate-manager.tf, HTTP that only redirects, and the URL map that sends
# each hostname to its backend service. The backend services are not here.
# They live in the environment projects, next to the Cloud Run services and
# serverless NEGs they front, and the URL map references them across projects
# (cross-project service referencing). For the global external Application
# Load Balancer that needs no Shared VPC and no VPC at all: the frontend and
# URL map "can reference backend services or backend buckets from any project
# within the same organization. No VPC network restrictions apply."
# (docs.cloud.google.com/load-balancing/docs/https, cross-project service
# referencing.) So this project has no network, and the environment projects
# need none for Cloud Run either.
#
# What the environment projects owe this layer: roles/compute.loadBalancerServiceUser
# for this layer's identity, granted there, at project or backend service
# level. The org policy compute.restrictLoadBalancerCreationForTypes in 1-org
# allows exactly this load balancer type (GLOBAL_EXTERNAL_MANAGED_HTTP_HTTPS),
# and run.allowedIngress keeps Cloud Run reachable only through it.
#
# Not here, on purpose: Cloud Armor (opt-in later, attached to backend
# services by the application layer) and outlier detection (a backend service
# setting, so the application layer's).
# ---------------------------------------------------------------------------

# One address for every hostname; DNS points the apex and the wildcard at it.
# Static, because the address is what the zone publishes and what an operator
# writes at a registrar; an ephemeral one would change on recreation.
resource "google_compute_global_address" "lb" {
  project      = local.project_id
  name         = "${local.name_prefix}-ip"
  description  = "Static IPv4 address of the global external Application Load Balancer"
  address_type = "EXTERNAL"
  ip_version   = "IPV4"
  labels       = local.labels
}

# TLS 1.2 at least, matching the org policy gcp.restrictTLSVersion, which
# already denies 1.0 and 1.1; the policy here is what the proxy enforces at
# the edge, the constraint is what stops anyone loosening it.
resource "google_compute_ssl_policy" "lb" {
  project         = local.project_id
  name            = "${local.name_prefix}-ssl-policy"
  description     = "TLS 1.2 minimum, ${var.ssl_policy_profile} cipher profile, for the global external Application Load Balancer"
  profile         = var.ssl_policy_profile
  min_tls_version = "TLS_1_2"
}

# Where a request goes when no host rule matches: a backend service with no
# backends. A request to it would answer 502, so the default route action
# aborts every request with 404 before any backend is chosen. Scanners hitting
# the bare address, or a hostname in the wildcard that no application owns,
# learn nothing. The service exists only because a URL map must name a
# default service; nothing is ever attached to it.
resource "google_compute_backend_service" "sink" {
  project               = local.project_id
  name                  = "${local.name_prefix}-sink"
  description           = "Empty default backend of the URL map; every request to it is answered 404 by the URL map"
  load_balancing_scheme = "EXTERNAL_MANAGED"
  protocol              = "HTTPS"

  log_config {
    enable = false
  }
}

# The routing table. Data-driven from var.hosts: one host rule and one path
# matcher per hostname, each sending everything under that host to the one
# backend service the map names. The application layer adds a host by
# providing its backend service; nothing in this file changes.
resource "google_compute_url_map" "lb" {
  project     = local.project_id
  name        = "${local.name_prefix}-urlmap"
  description = "Hostname routing for the global external Application Load Balancer"

  default_service = google_compute_backend_service.sink.id

  # 404 for anything no host rule claims. A fault-injection abort is the one
  # URL map action that answers without a backend; default_service above is
  # never reached but must be set.
  default_route_action {
    fault_injection_policy {
      abort {
        http_status = 404
        percentage  = 100
      }
    }
  }

  dynamic "host_rule" {
    for_each = var.hosts
    content {
      hosts        = [host_rule.key]
      path_matcher = local.path_matchers[host_rule.key]
    }
  }

  dynamic "path_matcher" {
    for_each = var.hosts
    content {
      name            = local.path_matchers[path_matcher.key]
      default_service = path_matcher.value
    }
  }
}

# Port 80 exists only to send clients to 443. A separate URL map, because a
# redirect is a URL map action and the HTTPS map must not carry it.
resource "google_compute_url_map" "redirect" {
  project     = local.project_id
  name        = "${local.name_prefix}-urlmap-redirect"
  description = "Redirects every HTTP request to HTTPS"

  default_url_redirect {
    https_redirect         = true
    redirect_response_code = "MOVED_PERMANENTLY_DEFAULT"
    strip_query            = false
  }
}

resource "google_compute_target_http_proxy" "lb" {
  project     = local.project_id
  name        = "${local.name_prefix}-http-proxy"
  description = "HTTP listener; redirects to HTTPS"
  url_map     = google_compute_url_map.redirect.id
}

# The certificate map, not a list of certificates: Certificate Manager picks
# the certificate by SNI from the map, and a certificate added to the map
# later needs no change to the proxy.
resource "google_compute_target_https_proxy" "lb" {
  project         = local.project_id
  name            = "${local.name_prefix}-https-proxy"
  description     = "HTTPS listener of the global external Application Load Balancer"
  url_map         = google_compute_url_map.lb.id
  certificate_map = "//certificatemanager.googleapis.com/${google_certificate_manager_certificate_map.apps.id}"
  ssl_policy      = google_compute_ssl_policy.lb.id
}

# EXTERNAL_MANAGED is the global external Application Load Balancer (the
# envoy-based one, not the classic), the only type the org policy allows and
# the only one that supports cross-project service referencing without a
# Shared VPC.
resource "google_compute_global_forwarding_rule" "http" {
  project               = local.project_id
  name                  = "${local.name_prefix}-http-rule"
  description           = "Port 80 of the global external Application Load Balancer"
  load_balancing_scheme = "EXTERNAL_MANAGED"
  ip_address            = google_compute_global_address.lb.id
  port_range            = "80"
  target                = google_compute_target_http_proxy.lb.id
  labels                = local.labels
}

resource "google_compute_global_forwarding_rule" "https" {
  project               = local.project_id
  name                  = "${local.name_prefix}-https-rule"
  description           = "Port 443 of the global external Application Load Balancer"
  load_balancing_scheme = "EXTERNAL_MANAGED"
  ip_address            = google_compute_global_address.lb.id
  port_range            = "443"
  target                = google_compute_target_https_proxy.lb.id
  labels                = local.labels
}
