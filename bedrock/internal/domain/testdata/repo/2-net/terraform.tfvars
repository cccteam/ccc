# Committed on purpose: nothing here is a secret.

apps_domain = "lab.example.com" # a delegated subdomain of the identity domain

# The registrant, administrative and technical contact of every registration.
# The mailbox must be read by a person: the registrar's verification mail goes there.
registrant_contact = {
  organization        = "Example Lab"
  recipients          = ["Hostmaster"]
  email               = "hostmaster@example.com"
  phone_number        = "+1.5555550100"
  address_lines       = ["1 Main Street"]
  locality            = "Springfield"
  administrative_area = "IL"
  postal_code         = "62701"
  region_code         = "US"
}

# Domains this layer registers, keyed by name.
registrations = {
  "example.app" = {
    yearly_price_usd = 14
    notices          = ["HSTS_PRELOADED"]
  }
}
