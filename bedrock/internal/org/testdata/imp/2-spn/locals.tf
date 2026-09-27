locals {
  # Outputs of 1-org. Nothing there is a secret; see its outputs.tf.
  org = data.terraform_remote_state.org.outputs

  environment = "spn"
  prefix      = local.org.prefix
  project_id  = local.org.project_ids[local.environment]

  # Naming convention: {prefix}-{environment}-{region}-{purpose}. A
  # multi-region Spanner instance belongs to no single region, so it carries
  # gbl; the configuration name (nam10) says where it runs.
  name_prefix = "${local.prefix}-${local.environment}-gbl"

  labels = {
    terraform             = "true"
    terraform_source_path = "2-spn"
    source_repo           = "imp-impulse-infrastructure"
    environment           = local.environment
    bedrock-lab           = "true"
  }
}
