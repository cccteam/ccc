# Committed on purpose: nothing here is a secret. The REPLACEME value is the
# seeded state bucket, the same name as in every backend block.
# environment is never set here: every run passes -var environment=<env>.

state_bucket = "imp-boot-gbl-state-REPLACEME"

# Placement per environment, filled as each environment is set up. Every map
# is keyed by environment so one file serves all three applies.

# OAuth client IDs, from each environment project's console (README).
staff_oidc_client_id = {
  tst = ""
  stg = ""
  prd = ""
}

# The Workspace administrator the groups read impersonates.
staff_oidc_admin_subject = {
  tst = ""
  stg = ""
  prd = ""
}

# The version of each secret an environment runs. Empty means "not yet": the
# container exists, nothing is mounted. Bump here after an operator adds a
# version. "latest" only for a secret the placement marks as tracking it.
secret_versions = {
  tst = {}
  stg = {}
  prd = {}
}

# Defaults that are decisions, restated so they are visible here:
#   hostnames                = harbor-tst. / harbor-stg. / harbor.impulseframework.dev
#   placeholder_image        = us-docker.pkg.dev/cloudrun/container/hello
#   staff_oidc_group_prefix  = "staff-"
#   staff_oidc_hosted_domain = "impulseframework.com"
