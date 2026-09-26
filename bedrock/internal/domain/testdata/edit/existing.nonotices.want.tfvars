apps_domain = "lab.example.com"

# Domains this layer registers, keyed by name. Each carries the yearly price
# Cloud Domains quoted and the notices it asked to acknowledge.
registrations = {
  # The apps domain, pointed at the apps zone.
  "example.app" = {
    yearly_price_usd = 14
    notices          = ["HSTS_PRELOADED"]
  }
  "example.com" = {
    yearly_price_usd = 12
  }
} # the first purchase

extra_records = []
