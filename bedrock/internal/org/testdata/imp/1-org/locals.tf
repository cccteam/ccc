locals {
  # The organization, from placement.json rather than looked up: an identity of
  # this organization holds nothing at the organization node, not even the read
  # that resolves the domain.
  org_id   = "123456789012"
  org_name = "organizations/123456789012"

  # Naming convention: {prefix}-{environment}-{region}-{purpose}
  # Use "gbl" only for truly global resources: projects, folders, service
  # accounts, IAM. Regional resources (GCS, Spanner, subnets) carry the region
  # code.
  region_short = {
    "us-central1" = "uc1"
    "us-east1"    = "ue1"
    "us-east4"    = "ue4"
    "us-west1"    = "uw1"
    "us-west2"    = "uw2"
    "us-west3"    = "uw3"
  }
  region_code           = local.region_short[var.gcp_region]
  secondary_region_code = local.region_short[var.gcp_secondary_region]

  labels = {
    terraform             = "true"
    terraform_source_path = "1-org"
    source_repo           = "imp-impulse-infrastructure"
    organization          = "imp"
    environment           = "org"
    bedrock-lab           = "true"
  }

  # Folder hierarchy created beneath the org root, alongside the terraform
  # folder from 0-bootstrap. The folders exist ahead of the projects that will
  # fill them: they are where org policy and Essential Contacts attach, so a
  # project inherits the guardrails the moment it is created rather than after
  # a follow-up run.
  folders = {
    shared = "shared"
    tst    = "tst"
    stg    = "stg"
    prd    = "prd"
  }

  # The log project exists only with central logging on. It is added here
  # rather than in var.projects so the flag is the single switch, and it has
  # no layer identity: it is managed end to end from this layer so the audit
  # trail has one owner. A for-expression with a filter rather than a
  # conditional, because the two arms of a conditional would have different
  # object types.
  log_project = {
    for key, cfg in {
      log = {
        folder      = "shared"
        description = "Central audit log sink destination"
        api_set     = "log"
        role_set    = "app"
        layer       = false
      }
    } : key => cfg if var.central_logging
  }

  projects = merge(var.projects, local.log_project)

  # Projects that get their own layer identity and plan identity.
  layers = { for k, v in local.projects : k => v if v.layer }

  layer_names = { for k, v in local.layers : k => "${var.prefix}-${k}-gbl" }

  # The projects the custom roles are created in (custom-roles.tf): every
  # project of this layer, by key, with its id.
  custom_role_projects = { for k, v in local.projects : k => module.project[k].project_id }

  # Custom roles a role set may name by bare ID, by project key then by ID. A
  # variable default cannot reference a resource, so var.layer_roles and
  # var.plan_roles carry the ID and service-accounts.tf resolves it through
  # this map, in the project the identity is granted on.
  custom_roles = {
    for k in keys(local.projects) : k => {
      secretContainerAdmin        = google_project_iam_custom_role.secret_container_admin[k].id
      identityPlatformInitializer = google_project_iam_custom_role.identity_platform_initializer[k].id
      secretOperator              = google_project_iam_custom_role.secret_operator[k].id
      storageBucketCreator        = google_project_iam_custom_role.storage_bucket_creator[k].id
      runJobPolicyAdmin           = google_project_iam_custom_role.run_job_policy_admin[k].id
      environmentLayerPlanReader  = google_project_iam_custom_role.environment_layer_plan_reader[k].id
      servicesLayerPlanReader     = google_project_iam_custom_role.services_layer_plan_reader[k].id
      networkLayerPlanReader      = google_project_iam_custom_role.network_layer_plan_reader[k].id
      spannerLayerPlanReader      = google_project_iam_custom_role.spanner_layer_plan_reader[k].id
    }
  }
}
