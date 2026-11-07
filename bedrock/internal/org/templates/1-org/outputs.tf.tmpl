# Consumed by the project layers through data "terraform_remote_state" on the
# state bucket (prefix 1-org). Anything that can read the bucket can read these,
# so keep secrets out.

output "audit_log_bucket" {
  description = "Name of the central audit log bucket in the log project; null while central_logging is off."
  value       = var.central_logging ? google_storage_bucket.audit_logs[0].name : null
}

output "billing_account_id" {
  description = "Billing account the projects are linked to."
  value       = var.billing_account_id
}

output "boot_project_id" {
  description = "Boot project from 0-bootstrap, used as the quota and billing project for API calls."
  value       = var.boot_project_id
}

output "environment_layer_grants" {
  description = "What each environment layer identity holds on its environment project, by environment code: the roles of its role set (a custom role by its full name) and the condition its roles/storage.admin is held under (the records bucket). 2-env's layer administrator entitlement grants a person the same for a recovery, read from here so the two cannot drift."
  value = {
    for k, cfg in local.environment_layers : k => {
      roles                   = [for role in var.layer_roles[cfg.role_set] : lookup(local.custom_roles[k], role, role)]
      storage_admin_condition = local.records_bucket_conditions[k]
    }
  }
}

output "folder_ids" {
  description = "Folder ID by folder key (shared, tst, stg, prd)."
  value       = { for k, f in google_folder.this : k => f.folder_id }
}

output "gcp_region" {
  description = "Primary region for regional resources."
  value       = var.gcp_region
}

output "gcp_secondary_region" {
  description = "Secondary region for resources that run in two regions."
  value       = var.gcp_secondary_region
}

output "layer_service_accounts" {
  description = "Layer identity (apply) service account email by environment code."
  value       = { for k, sa in google_service_account.tofu : k => sa.email }
}

output "log_project_id" {
  description = "Project holding the central audit log bucket; null while central_logging is off."
  value       = local.log_project_id
}

output "org_id" {
  description = "Numeric GCP organization ID."
  value       = local.org_id
}

output "plan_service_accounts" {
  description = "Read-only plan identity service account email by environment code."
  value       = { for k, sa in google_service_account.plan : k => sa.email }
}

output "prefix" {
  description = "Org-wide naming prefix."
  value       = var.prefix
}

output "project_ids" {
  description = "Project ID by environment code."
  value       = { for k, p in module.project : k => p.project_id }
}

output "project_numbers" {
  description = "Project number by environment code. Needed for service agent members, which are keyed by number rather than ID."
  value       = { for k, p in module.project : k => p.project_number }
}

output "public_invoker_tag_key" {
  description = "By environment code, the resource name (tagKeys/<id>) of that environment project's public-invoker tag key; with public_invoker_tag_value, what the root organization names in the exception of the environment folder's domain-restricted sharing policy, recorded in placement.json by the creation (publicInvokerTags)."
  value       = { for k, v in google_tags_tag_key.public_invoker : k => v.id }
}

output "public_invoker_tag_value" {
  description = "By environment code, the resource name (tagValues/<id>) of that environment project's public-invoker=true tag; an application stack binds its environment's to a Cloud Run service before granting allUsers roles/run.invoker (tags.tf)."
  value       = { for k, v in google_tags_tag_value.public_invoker : k => v.id }
}

output "region_code" {
  description = "Short code for the primary region, used in resource names."
  value       = local.region_code
}

output "secondary_region_code" {
  description = "Short code for the secondary region, used in resource names."
  value       = local.secondary_region_code
}

output "cloud_build_trigger_runner_role" {
  description = "By project key, the full name of the cloudBuildTriggerRunner custom role in that project: runs Cloud Build triggers and reads the builds they start, nothing else. 2-env grants it on the environment project to each application's operations identity, and in tst to each application's deploy identity, which runs the hourly sweep trigger."
  value       = { for k, r in google_project_iam_custom_role.cloud_build_trigger_runner : k => r.id }
}

output "cloud_build_build_reader_role" {
  description = "By project key, the full name of the cloudBuildBuildReader custom role in that project: reads Cloud Build builds, nothing else. 2-env grants it on the environment project to each application's deploy identity, which reads the build it runs in, in place of roles/cloudbuild.builds.builder."
  value       = { for k, r in google_project_iam_custom_role.cloud_build_build_reader : k => r.id }
}

output "application_plan_reader_role" {
  description = "By project key, the full name of the applicationPlanReader custom role in that project: reads the resources an application stack declares, nothing of their data. 2-env grants it on the environment project to each application's plan identity in place of roles/viewer."
  value       = { for k, r in google_project_iam_custom_role.application_plan_reader : k => r.id }
}

output "cloud_tasks_queue_operator_role" {
  description = "By project key, the full name of the cloudTasksQueueOperator custom role in that project: pauses, resumes and purges a Cloud Tasks queue, nothing else. An application's stack grants it on the application's own queue to its deploy identity, for the maintenance a restore run or a breaking release puts the application into."
  value       = { for k, r in google_project_iam_custom_role.cloud_tasks_queue_operator : k => r.id }
}

output "secret_container_admin_role" {
  description = "By project key, the full name of the secretContainerAdmin custom role in that project, for layers that grant it to further identities."
  value       = { for k, r in google_project_iam_custom_role.secret_container_admin : k => r.id }
}

output "secret_operator_role" {
  description = "By project key, the full name of the secretOperator custom role in that project: creates secrets and adds versions, never reads one. 2-env's secret operator entitlement grants it on the environment project to a member of the team group for a short time."
  value       = { for k, r in google_project_iam_custom_role.secret_operator : k => r.id }
}

output "run_job_policy_admin_role" {
  description = "By project key, the full name of the runJobPolicyAdmin custom role in that project: reads and sets the IAM policy of Cloud Run jobs, nothing else. 2-env grants it on the environment project to each application's deploy identity."
  value       = { for k, r in google_project_iam_custom_role.run_job_policy_admin : k => r.id }
}

output "spanner_plan_reader_role" {
  description = "By project key, the full name of the spannerPlanReader custom role in that project: reads an instance's databases, their IAM policies and their backup schedules, nothing of their data. 2-spn grants it on the shared instance and 2-env on an environment's own instance to each application's plan identity."
  value       = { for k, r in google_project_iam_custom_role.spanner_plan_reader : k => r.id }
}

output "spanner_database_creator_role" {
  description = "By project key, the full name of the spannerDatabaseCreator custom role in that project: creates a database on an instance and lists what the instance holds, and changes nothing that exists. 2-spn grants it on the shared instance and 2-env on an environment's own instance to each application's apply identity, beside the admin roles bounded to that application's own database and backups."
  value       = { for k, r in google_project_iam_custom_role.spanner_database_creator : k => r.id }
}

output "storage_bucket_creator_role" {
  description = "By project key, the full name of the storageBucketCreator custom role in that project: creates a bucket in a project and lists the project's buckets, the two of storage admin's permissions Cloud Storage checks on the project, and changes nothing that exists. 2-env grants it on the environment project to each application's apply identity, beside storage admin bounded to that application's own buckets; the app role set holds it for the environment layer identity, whose storage admin this layer bounds to the records bucket."
  value       = { for k, r in google_project_iam_custom_role.storage_bucket_creator : k => r.id }
}
