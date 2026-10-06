locals {
  # Outputs of 1-org. Nothing there is a secret; see its outputs.tf.
  org = data.terraform_remote_state.org.outputs

  environment = "shr"
  prefix      = local.org.prefix
  project_id  = local.org.project_ids[local.environment]

  # Naming convention: {prefix}-{environment}-{region}-{purpose}. Artifact
  # Registry repositories are regional, so they carry the region code rather
  # than gbl.
  region      = local.org.gcp_region
  region_code = local.org.region_code
  name_prefix = "${local.prefix}-${local.environment}-${local.region_code}"

  # Docker hostname of the registry in this region. An image is addressed as
  # {registry_hostname}/{project}/{repository}/{image}:{tag}.
  registry_hostname = "${local.region}-docker.pkg.dev"

  labels = {
    terraform             = "true"
    terraform_source_path = "2-shr"
    source_repo           = "imp-impulse-infrastructure"
    environment           = local.environment
    bedrock-lab           = "true"
  }

  # Cloud Run pulls an image as the Cloud Run service agent of the project the
  # service runs in, service-{number}@serverless-robot-prod, not as the
  # service's runtime identity. Each environment's agent follows from its
  # project number, which 1-org publishes, so the reader grants are made here
  # without the environment layers having to name anything.
  pull_agents = {
    for env in var.pull_environments :
    env => "serviceAccount:service-${local.org.project_numbers[env]}@serverless-robot-prod.iam.gserviceaccount.com"
  }

  # Flatten application x environment so for_each can grant one reader per pair.
  reader_grants = {
    for pair in setproduct(var.applications, keys(local.pull_agents)) :
    "${pair[0]}__${pair[1]}" => { app = pair[0], member = local.pull_agents[pair[1]] }
  }

  # Flatten application x puller so for_each can grant one reader per pair,
  # beside the service agents above.
  puller_grants = {
    for pair in flatten([
      for app, members in var.pullers : [
        for member in members : {
          key    = "${app}__${member}"
          app    = app
          member = member
        }
      ]
    ]) : pair.key => pair
  }

  # Flatten application x pusher so for_each can grant one writer per pair.
  writer_grants = {
    for pair in flatten([
      for app, members in var.pushers : [
        for member in members : {
          key    = "${app}__${member}"
          app    = app
          member = member
        }
      ]
    ]) : pair.key => pair
  }
}
