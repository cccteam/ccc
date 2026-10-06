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

# Defaults that are decisions, restated so they are visible here:
#   placeholder_image = us-docker.pkg.dev/cloudrun/container/hello
