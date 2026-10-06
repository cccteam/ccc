state_bucket = "lab-boot-state-1234"

# The version of each secret an environment runs.
secret_versions = {
  tst = {
    APP_COOKIE_KEY   = "2"
    APP_MAIL_API_KEY = "4"
  }
  stg = {}
  prd = {}
}
