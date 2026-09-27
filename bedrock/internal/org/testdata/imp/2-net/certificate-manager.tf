# ---------------------------------------------------------------------------
# Certificates
#
# One Google-managed certificate for the apex and the wildcard, proven by a
# DNS authorization rather than by load balancer authorization: a wildcard
# can only be issued against DNS, and DNS authorization lets the certificate
# be issued before any traffic reaches the address. The proof is a CNAME in
# the zone (dns.tf), created from the authorization's own record. Order of
# creation is authorization, CNAME, certificate; the certificate stays in
# PROVISIONING until the CNAME resolves publicly, which needs the domain
# delegated to the zone's name servers.
# ---------------------------------------------------------------------------

resource "google_certificate_manager_dns_authorization" "apps" {
  project     = local.project_id
  name        = "${local.name_prefix}-dnsauth-apps"
  description = "DNS authorization for ${var.apps_domain} and ${local.wildcard}"
  domain      = var.apps_domain
  labels      = local.labels
}

resource "google_certificate_manager_certificate" "apps" {
  project     = local.project_id
  name        = "${local.name_prefix}-cert-apps"
  description = "Managed certificate for ${var.apps_domain} and ${local.wildcard}"
  labels      = local.labels

  managed {
    domains            = [var.apps_domain, local.wildcard]
    dns_authorizations = [google_certificate_manager_dns_authorization.apps.id]
  }

  depends_on = [google_dns_record_set.dns_authorization]
}

# The map is what the HTTPS proxy holds. Two entries, both to the one
# certificate: the apex by exact hostname, and everything one level below by
# wildcard hostname. No PRIMARY entry on purpose: a client presenting a
# hostname outside the map gets no certificate and a failed handshake, which
# is the right answer for a name this load balancer does not serve.
resource "google_certificate_manager_certificate_map" "apps" {
  project     = local.project_id
  name        = "${local.name_prefix}-certmap"
  description = "Certificate map of the global external Application Load Balancer"
  labels      = local.labels
}

resource "google_certificate_manager_certificate_map_entry" "apex" {
  project      = local.project_id
  name         = "${local.name_prefix}-cme-apex"
  description  = "${var.apps_domain} itself"
  map          = google_certificate_manager_certificate_map.apps.name
  hostname     = var.apps_domain
  certificates = [google_certificate_manager_certificate.apps.id]
  labels       = local.labels
}

resource "google_certificate_manager_certificate_map_entry" "wildcard" {
  project      = local.project_id
  name         = "${local.name_prefix}-cme-wildcard"
  description  = "Every hostname one level below ${var.apps_domain}"
  map          = google_certificate_manager_certificate_map.apps.name
  hostname     = local.wildcard
  certificates = [google_certificate_manager_certificate.apps.id]
  labels       = local.labels
}
