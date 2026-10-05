# ---------------------------------------------------------------------------
# Domain registrations
#
# The apps domain and any other domain the organization holds are registered
# here through Cloud Domains, beside their zones, so a domain is one more
# declared resource: the plan shows the name and its yearly price, the apply
# registers it and points it at the zone this layer created, and from then
# on nothing is delegated by hand. Certificate Manager's proof records land
# in the zone and the certificate provisions on its own.
#
# The price is placement (var.registrations): Cloud Domains refuses a
# registration whose stated price differs from the current one, so a price
# change is a visible diff and a failed apply, never a silent charge. The
# contact is placement too (var.registrant_contact), published redacted; the
# registrant mailbox must be read by a person, because the registrar's
# verification mail goes there and an unverified domain is suspended;
# bedrock org check reads each registration back and names a mailbox still
# waiting on the verification first, then each registration's state and
# expiry date.
#
# A registration is never removed by a plan: deleting one deletes the domain
# after its grace period. Removal is a deliberate two-step change, as with
# the Spanner instance. Nor does the apply change the name servers of a
# registration that exists: the provider would replace the registration to do
# it, so after a zone is made again the registration is pointed at the new
# zone in Cloud Domains, the step bedrock domain check prints (README.md,
# "Making a zone again").
# ---------------------------------------------------------------------------

locals {
  # Every registered domain other than the apps domain gets a parked zone:
  # registered, resolvable, serving nothing until a layer gives it records.
  parked_domains = { for d, r in var.registrations : d => r if d != var.apps_domain }

  contact = {
    email        = var.registrant_contact.email
    phone_number = var.registrant_contact.phone_number
    postal_address = {
      region_code         = var.registrant_contact.region_code
      postal_code         = var.registrant_contact.postal_code
      administrative_area = var.registrant_contact.administrative_area
      locality            = var.registrant_contact.locality
      address_lines       = var.registrant_contact.address_lines
      organization        = var.registrant_contact.organization
      recipients          = var.registrant_contact.recipients
    }
  }
}

resource "google_dns_managed_zone" "parked" {
  for_each = local.parked_domains

  project     = local.project_id
  name        = "${local.name_prefix}-dns-${replace(each.key, ".", "-")}"
  dns_name    = "${each.key}."
  description = "Public zone for ${each.key}; registered here, parked until a layer serves it"
  labels      = local.labels

  dnssec_config {
    state = "on"
  }

  # The registration points at this zone's name servers, which Cloud DNS
  # assigns when it creates the zone; a zone made again can land on a
  # different set, so a recreation is refused by default and is a deliberate
  # change in steps, as for the apps zone (dns.tf).
  lifecycle {
    prevent_destroy = true
  }
}

resource "google_clouddomains_registration" "this" {
  for_each = var.registrations

  project     = local.project_id
  location    = "global"
  domain_name = each.key
  labels      = local.labels

  yearly_price {
    currency_code = "USD"
    units         = each.value.yearly_price_usd
  }

  # .app and .dev are on the HSTS preload list (HTTPS only), which the
  # registrar asks to be acknowledged; the notice list is placement so the
  # acknowledgement is visible in the repository.
  domain_notices  = each.value.notices
  contact_notices = var.registrant_contact.public ? ["PUBLIC_CONTACT_DATA_ACKNOWLEDGEMENT"] : []

  # The registration's name servers are the zone's: the apps zone for the
  # apps domain, a parked zone for any other. No registrar step, ever.
  dns_settings {
    custom_dns {
      name_servers = each.key == var.apps_domain ? google_dns_managed_zone.apps.name_servers : google_dns_managed_zone.parked[each.key].name_servers
    }
  }

  contact_settings {
    privacy = var.registrant_contact.public ? "PUBLIC_CONTACT_DATA" : "REDACTED_CONTACT_DATA"

    registrant_contact {
      email        = local.contact.email
      phone_number = local.contact.phone_number
      postal_address {
        region_code         = local.contact.postal_address.region_code
        postal_code         = local.contact.postal_address.postal_code
        administrative_area = local.contact.postal_address.administrative_area
        locality            = local.contact.postal_address.locality
        address_lines       = local.contact.postal_address.address_lines
        organization        = local.contact.postal_address.organization
        recipients          = local.contact.postal_address.recipients
      }
    }
    admin_contact {
      email        = local.contact.email
      phone_number = local.contact.phone_number
      postal_address {
        region_code         = local.contact.postal_address.region_code
        postal_code         = local.contact.postal_address.postal_code
        administrative_area = local.contact.postal_address.administrative_area
        locality            = local.contact.postal_address.locality
        address_lines       = local.contact.postal_address.address_lines
        organization        = local.contact.postal_address.organization
        recipients          = local.contact.postal_address.recipients
      }
    }
    technical_contact {
      email        = local.contact.email
      phone_number = local.contact.phone_number
      postal_address {
        region_code         = local.contact.postal_address.region_code
        postal_code         = local.contact.postal_address.postal_code
        administrative_area = local.contact.postal_address.administrative_area
        locality            = local.contact.postal_address.locality
        address_lines       = local.contact.postal_address.address_lines
        organization        = local.contact.postal_address.organization
        recipients          = local.contact.postal_address.recipients
      }
    }
  }

  # No transfer lock: Cloud Domains refuses to set one on .dev and .app at
  # registration ("Transfer lock functionality is unavailable", 2026-09-26);
  # a transfer out of a Google-registry name is protected by the account
  # instead. Renewal is automatic so a lapse cannot happen by forgetting.
  management_settings {
    preferred_renewal_method = "AUTOMATIC_RENEWAL"
  }

  lifecycle {
    # Cloud Domains answers no contact details back (the registrations are
    # redacted) and reports the name servers in its own form, so every read
    # looks like a change of both, and either would replace the registration:
    # the provider changes neither in place. Both are set at registration and
    # kept; the zone's name servers are the ones registered (the hostnames
    # resolve). A zone made again is the one case they part, and the
    # registration is then pointed at it in Cloud Domains (README.md, "Making a
    # zone again"), never by this apply.
    ignore_changes  = [contact_settings, dns_settings]
    prevent_destroy = true

    precondition {
      condition     = each.key == var.apps_domain || contains(keys(local.parked_domains), each.key)
      error_message = "Every registered domain is either the apps domain or parked."
    }
  }
}
