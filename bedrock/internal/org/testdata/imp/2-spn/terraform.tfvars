# Committed on purpose: nothing here is a secret. Project IDs come from
# 1-org's state, so nothing here needs a REPLACEME.

processing_units = 100 # never above 200 without explicit agreement; variables.tf enforces it

# The database admins (each application's apply identity in stg and prd) are in
# applications.auto.tfvars, rendered from placement.json; a service account must
# exist before it can be bound, so this layer is applied after the environment
# layers have run for a new application.

# Defaults that are decisions, restated so they are visible here:
#   spanner_config = "nam10"
#   edition        = (unset: ENTERPRISE_PLUS for a multi-region configuration, STANDARD for a regional one)
