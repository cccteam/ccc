# Committed on purpose: nothing here is a secret.

state_bucket = "lab-boot-state-1234"

# The version of each secret an environment runs. Empty means "not yet": the
# container exists, nothing is mounted.
secret_versions = {
  tst = {
    APP_COOKIE_KEY = "2"
  }
  stg = {}
  prd = {}
}

# Secrets the image build reads, NAME = pinned version.
build_secrets = {
  tst = {
    UI_LICENSE = "1"
  }
  stg = {}
  prd = {}
}
