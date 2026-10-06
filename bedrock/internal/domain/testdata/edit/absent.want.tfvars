# Committed on purpose: nothing here is a secret.

apps_domain = "lab.example.com" # a delegated subdomain of the identity domain

# One entry per hostname the load balancer serves.
hosts = {
  "harbor.lab.example.com" = "projects/lab-prd/global/backendServices/harbor"
}

# Defaults that are decisions, restated so they are visible here:
#   ssl_policy_profile = "MODERN"

registrations = {
  "example.dev" = {
    yearly_price_usd = 12
    notices          = ["HSTS_PRELOADED"]
  }
}
