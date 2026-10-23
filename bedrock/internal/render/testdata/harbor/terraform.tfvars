# Committed on purpose: nothing here is a secret. The REPLACEME value is the
# seeded state bucket, the same name as in every backend block.
# environment is never set here: every run passes -var environment=<env>.

state_bucket = "imp-boot-gbl-state-REPLACEME"

# Placement per environment, filled as each environment is set up. Every map
# is keyed by environment so one file serves all three applies.

# The members auth's directory registration with Microsoft (README, "Hand
# steps"): the client id of each environment's registration, and the tenant whose
# accounts it admits, by its tenant id (the directory (tenant) ID on the app
# registration's overview page), once for the organization.
members_directory_client_id = {
  tst = ""
  stg = ""
  prd = ""
}
members_directory_tenant_id = ""

# The staff auth's directory registration with Google (README, "Hand
# steps"): the client id of each environment's registration.
staff_directory_client_id = {
  tst = ""
  stg = ""
  prd = ""
}

# The members auth's first administrator, per environment (README, "Hand steps"):
# the sign-in name the directory presents, which the migrate command seeds the
# administrator role membership under while the auth has no users.
members_administrator = {
  tst = ""
  stg = ""
  prd = ""
}

# The Google Workspace mailbox the auths' mail is sent as, once for the
# organization (README, "Hand steps"); empty, the application writes its mail
# to the log and sends nothing.
mail_mailbox = ""

# The version of each secret an environment runs. Empty means "not yet": the
# container exists, nothing is mounted; tst alone runs that way,
# and a release to any other environment is refused until every secret the
# code declares is pinned there. Bump here after someone adds a version
# (bedrock secret pin). "latest" only for a secret the placement marks as
# tracking it. "generated" for a secret the code declares secret:"generated":
# the pipeline makes its value and derives its version at each build, so the
# word stays, and bedrock secret rotate adds the next version.
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

# Cloud Armor per environment (README, "Cloud Armor"): "preview" evaluates the
# policy and logs what each rule would have done, "enforce" applies it. Off for
# an environment left out. Preview first, read the logs, then enforce. The
# policy throttles the sign-in routes by client address, at the
# cloud_armor_sign_in_count and cloud_armor_sign_in_interval restated below;
# write either here to change it.
cloud_armor = {
}

# Defaults that are decisions, restated so they are visible here:
#   hostnames                     = harbor-tst. / harbor-stg. / harbor.impulseframework.dev
#   placeholder_image             = us-docker.pkg.dev/cloudrun/container/hello
#   staff_directory_group_lookup  = "direct"
#   staff_directory_group_prefix  = "staff-"
#   staff_directory_hosted_domain = "impulseframework.com"
#   mail_mailbox                  = ""
#   jobs_timeout                  = "1800s"
#   jobs_retries                  = 0
#   jobs_resources                = cpu 1, memory 512Mi
#   tasks_max_concurrent          = 10
#   tasks_max_attempts            = 5
#   cloud_armor_sign_in_count     = 30
#   cloud_armor_sign_in_interval  = 60
