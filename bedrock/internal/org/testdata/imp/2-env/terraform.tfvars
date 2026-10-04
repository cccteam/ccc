# Committed on purpose: nothing here is a secret. The REPLACEME value comes
# from the 0-bootstrap output boot_project_id.
# environment is never set here: every run passes -var environment=<env>.

# boot_project_id is the output boot_project_id of 0-bootstrap; state_bucket
# the bucket in every backend block.
boot_project_id = "imp-boot-gbl-core-REPLACEME"
state_bucket    = "imp-boot-gbl-state-a1b2"

# From the browser authorization described in the README: the Cloud Build
# GitHub App installed on github.com/imp-example for all
# repositories, and the tst host connection authorized. One value each for the
# organization; every environment's connection reuses them. Until they are
# set, the connection and the repository links wait; the rest of the layer
# applies.
# github_app_installation_id        = <installation id>
# github_deployer_app_id            = <the deployer app's App ID, not its installation>
# github_deployer_key_secret_versions = { tst = "projects/<number>/secrets/imp-tst-gbl-github-deployer-key/versions/1" }
# github_oauth_token_secret_version = "projects/<tst project>/locations/us-central1/secrets/<connection token secret>/versions/1"

# The applications are in applications.auto.tfvars, rendered from placement.json
# (bedrock org register <app> adds one).

# The promotion order, as the environment after each: a release runs in the next
# environment only after this one holds a live deployment record of it, so the next
# environment's deploy identities read this environment's records.
next_environment = {
  tst = "stg"
  stg = "prd"
  prd = ""
}

# Nothing here names a person. Each environment's team group (placement.json,
# teamGroups) approves its releases and asks for its entitlements; a secret
# value is added under the secret operator entitlement (team-group.tf).
