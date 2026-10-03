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

output "layer_service_account_unique_ids" {
  description = "Layer identity (apply) service account unique id by environment code: the form beside the email that a condition's resource.name names a service account by, for the layer administrator entitlement 2-env declares on the environment's own apply identity."
  value       = { for k, sa in google_service_account.tofu : k => sa.unique_id }
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

output "boot_plan_service_accounts" {
  description = "Read-only plan identity email of the two boot layers (boot for 0-bootstrap, org for 1-org), in the boot project; a pull request plans those layers as them."
  value       = { for k, sa in google_service_account.boot_plan : k => sa.email }
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

output "public_invoker_tag_value" {
  description = "Resource name (tagValues/<id>) of the public-invoker=true tag; the application layers bind it to a Cloud Run service before granting allUsers roles/run.invoker (tags.tf)."
  value       = google_tags_tag_value.public_invoker.id
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
  description = "Full name of the cloudBuildTriggerRunner custom organization role: runs Cloud Build triggers and reads the builds they start, nothing else. 2-env grants it on the environment project to each application's operations identity, and in tst to each application's deploy identity, which runs the hourly sweep trigger."
  value       = google_organization_iam_custom_role.cloud_build_trigger_runner.id
}

output "cloud_build_build_reader_role" {
  description = "Full name of the cloudBuildBuildReader custom organization role: reads Cloud Build builds, nothing else. 2-env grants it on the environment project to each application's deploy identity, which reads the build it runs in, in place of roles/cloudbuild.builds.builder."
  value       = google_organization_iam_custom_role.cloud_build_build_reader.id
}

output "application_plan_reader_role" {
  description = "Full name of the applicationPlanReader custom organization role: reads the resources an application stack declares, nothing of their data. 2-env grants it on the environment project to each application's plan identity in place of roles/viewer."
  value       = google_organization_iam_custom_role.application_plan_reader.id
}

output "cloud_tasks_queue_operator_role" {
  description = "Full name of the cloudTasksQueueOperator custom organization role: pauses, resumes and purges a Cloud Tasks queue, nothing else. An application's stack grants it on the application's own queue to its deploy identity, for the maintenance a restore run or a breaking release puts the application into."
  value       = google_organization_iam_custom_role.cloud_tasks_queue_operator.id
}

output "secret_container_admin_role" {
  description = "Full name of the secretContainerAdmin custom organization role, for layers that grant it to further identities."
  value       = google_organization_iam_custom_role.secret_container_admin.id
}

output "secret_operator_role" {
  description = "Full name of the secretOperator custom organization role: creates secrets and adds versions, never reads one. 2-env's secret operator entitlement grants it on the environment project to a member of the team group for a short time."
  value       = google_organization_iam_custom_role.secret_operator.id
}

output "run_job_policy_admin_role" {
  description = "Full name of the runJobPolicyAdmin custom organization role: reads and sets the IAM policy of Cloud Run jobs, nothing else. 2-env grants it on the environment project to each application's deploy identity."
  value       = google_organization_iam_custom_role.run_job_policy_admin.id
}

output "spanner_plan_reader_role" {
  description = "Full name of the spannerPlanReader custom organization role: reads an instance's databases, their IAM policies and their backup schedules, nothing of their data. 2-spn grants it on the shared instance and 2-env on an environment's own instance to each application's plan identity."
  value       = google_organization_iam_custom_role.spanner_plan_reader.id
}

output "spanner_database_creator_role" {
  description = "Full name of the spannerDatabaseCreator custom organization role: creates a database on an instance and lists what the instance holds, and changes nothing that exists. 2-spn grants it on the shared instance and 2-env on an environment's own instance to each application's apply identity, beside the admin roles bounded to that application's own database and backups."
  value       = google_organization_iam_custom_role.spanner_database_creator.id
}
