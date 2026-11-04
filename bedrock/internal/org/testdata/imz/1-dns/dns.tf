# ---------------------------------------------------------------------------
# The zones, and the labels handed onward
#
# One public zone per domain (var.domains, from placement.json's domains): a
# domain a customer delegated to this organization once, at its registrar for
# a domain as registered or as NS records at the DNS provider that serves the
# domain above it for a label of one (bedrock domain check prints the records
# and where they go), so that every application's name under it can be handed
# to the hosting organization that serves the application, wherever that is.
# The zone holds nothing but those delegations: one NS record set per label,
# pointing at the four name servers of the application's zone in its hosting
# organization, where that organization's 2-net serves the name as a vanity
# domain with the wildcard certificate, the authorization record and the
# address records. A lookup walks from the customer's registrar to this zone,
# from this zone to the application's, whose wildcard answers; the environment
# names nest under the application's (stg.acme.mydomain.com). No address, no
# certificate, no load balancer here (README.md, "How a name resolves").
# ---------------------------------------------------------------------------

resource "google_dns_managed_zone" "domain" {
  for_each = var.domains

  project     = var.boot_project_id
  name        = "${local.name_prefix}-dns-${replace(each.key, ".", "-")}"
  dns_name    = "${each.key}."
  description = "Public zone for ${each.key}, delegated to this organization; its labels are handed onward to the hosting organizations' zones"
  labels      = local.labels

  # DNSSEC costs nothing here and a registrar that supports it gets the DS
  # record from the zone. The labels handed onward carry no DS record: each is
  # an unsigned delegation, which resolves, and an application's zone made
  # again in its hosting organization breaks nothing here.
  dnssec_config {
    state = "on"
  }

  # Cloud DNS assigns a zone its name servers when it creates the zone, and a
  # zone made again can land on a different set. The customer's delegation
  # then points at servers that no longer answer for the domain, and every
  # application under it stops resolving until the customer points it again.
  # So a recreation is refused by default and is a deliberate change in steps:
  # set zoneReplacement in placement.json and render, which lifts this rule,
  # recreate the zone in the next change, then have the delegation pointed at
  # the new set (README.md, "Making a zone again").
  lifecycle {
    prevent_destroy = true
  }
}

# One NS record set per label handed onward: the name under the domain, the
# four name servers of the application's zone in its hosting organization. The
# time to live is an hour, so a move to another hosting organization (a new
# zone there, this record set changed) is seen everywhere within the hour.
resource "google_dns_record_set" "delegation" {
  for_each = local.delegations

  project      = var.boot_project_id
  managed_zone = google_dns_managed_zone.domain[each.value.domain].name
  name         = "${each.key}."
  type         = "NS"
  ttl          = 3600
  rrdatas      = each.value.name_servers
}
