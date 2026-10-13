# Committed on purpose: nothing here is a secret. The REPLACEME value comes
# from the 0-bootstrap output boot_project_id.
# environment is never set here: every run passes -var environment=<env>.

# boot_project_id is the output boot_project_id of 0-bootstrap; state_bucket
# the bucket in every backend block.
boot_project_id = "imp-boot-gbl-core-REPLACEME"
state_bucket    = "imp-boot-gbl-state-a1b2"

# The Cloud Build GitHub authorization (the connection's installation id and
# token secret version) and the deployer GitHub App (its App ID and the pinned
# version of its key) are placement.json's (cloudBuildInstallationId,
# cloudBuildTokenSecretVersion, githubDeployerAppId, githubDeployerKeyVersion),
# rendered into locals.tf; nothing of them is set here.

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
