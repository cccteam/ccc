state_bucket = "lab-boot-state-1234"

# The version of each secret an environment runs.
secret_versions = {
  tst = {
    APP_COOKIE_KEY = "2"
  }
  stg = {
    APP_COOKIE_KEY = "1"
  }
  prd = {}
}
