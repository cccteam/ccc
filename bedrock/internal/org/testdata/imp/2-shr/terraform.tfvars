# Committed on purpose: nothing here is a secret. Project IDs come from
# 1-org's state, so nothing here needs a REPLACEME.

# The applications, their pushers (deploy identities) and their pullers (apply
# identities) are in applications.auto.tfvars, rendered from placement.json; a
# service account must exist before it can be bound, so this layer is applied
# after the environment layers have run for a new application.

# Defaults that are decisions, restated so they are visible here:
#   pull_environments       = ["tst", "stg", "prd"]
#   keep_tagged_versions    = 10
#   untagged_retention_days = 7
#   tagged_retention_days   = null   (tagged versions are never deleted)
#   cleanup_dry_run         = false
