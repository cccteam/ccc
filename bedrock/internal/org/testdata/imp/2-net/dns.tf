# ---------------------------------------------------------------------------
# DNS
#
# The public zone for the apps domain. A domain listed in registrations is
# registered in domains.tf and its name servers are set to this zone's by the
# same apply, so the zone is visible as soon as the registration completes
# and the certificate provisions without a hand step. A domain registered
# elsewhere, or a label of a domain served elsewhere, is delegated to this
# zone by hand, once; bedrock domain check prints the records and where they
# go (README.md, "Delegating the domain").
# ---------------------------------------------------------------------------

resource "google_dns_managed_zone" "apps" {
  project     = local.project_id
  name        = "${local.name_prefix}-dns-apps"
  dns_name    = "${var.apps_domain}."
  description = "Public zone for ${var.apps_domain}; the registration points at it"
  labels      = local.labels

  # DNSSEC costs nothing here and a registrar that supports it gets the DS
  # record from the zone.
  dnssec_config {
    state = "on"
  }
}

# The apex and every hostname one level below it resolve to the load
# balancer. Two records cover every hostname the convention produces, and a
# new application or pull-request environment needs no DNS change at all;
# whether the load balancer serves a hostname is decided by var.hosts. A more
# specific record (the authorization CNAME, anything in extra_records) wins
# over the wildcard.
resource "google_dns_record_set" "lb" {
  for_each = toset([var.apps_domain, local.wildcard])

  project      = local.project_id
  managed_zone = google_dns_managed_zone.apps.name
  name         = "${each.value}."
  type         = "A"
  ttl          = 300
  rrdatas      = [google_compute_global_address.lb.address]
}

# The proof of ownership Certificate Manager asks for, copied from the
# authorization it belongs to so the two can never drift.
resource "google_dns_record_set" "dns_authorization" {
  project      = local.project_id
  managed_zone = google_dns_managed_zone.apps.name
  name         = google_certificate_manager_dns_authorization.apps.dns_resource_record[0].name
  type         = google_certificate_manager_dns_authorization.apps.dns_resource_record[0].type
  ttl          = 300
  rrdatas      = [google_certificate_manager_dns_authorization.apps.dns_resource_record[0].data]
}

# Everything else the domain needs: mail and verification records when the
# zone takes over a domain that already has them. See var.extra_records.
resource "google_dns_record_set" "extra" {
  for_each = { for r in var.extra_records : "${r.name}/${r.type}" => r }

  project      = local.project_id
  managed_zone = google_dns_managed_zone.apps.name
  name         = each.value.name == "@" ? "${var.apps_domain}." : "${each.value.name}.${var.apps_domain}."
  type         = each.value.type
  ttl          = each.value.ttl
  rrdatas      = each.value.rrdatas
}
