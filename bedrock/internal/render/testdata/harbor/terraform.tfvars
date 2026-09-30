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

# Extra trigger substitutions for the application's hooks and its image build,
# per environment (_NAME = value); none by default (README, "Customizing the
# pipeline").
substitutions = {
  tst = {}
  stg = {}
  prd = {}
}

# Secrets the image build reads, per environment (NAME = pinned version of the
# container imp-<env>-gbl-harbor-<kebab name>, which the stack creates and only
# the deploy identity may read); none by default (README, "Build secrets").
build_secrets = {
  tst = {}
  stg = {}
  prd = {}
}

# Defaults that are decisions, restated so they are visible here:
#   hostnames                = harbor-tst. / harbor-stg. / harbor.impulseframework.dev
#   placeholder_image        = us-docker.pkg.dev/cloudrun/container/hello
#   staff_oidc_group_lookup  = "direct"
#   staff_oidc_group_prefix  = "staff-"
#   staff_oidc_hosted_domain = "impulseframework.com"
#   jobs_timeout             = "1800s"
#   jobs_retries             = 0
#   jobs_resources           = cpu 1, memory 512Mi
#   tasks_max_concurrent     = 10
#   tasks_max_attempts       = 5
