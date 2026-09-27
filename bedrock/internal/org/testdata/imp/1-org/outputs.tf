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

output "secret_container_admin_role" {
  description = "Full name of the secretContainerAdmin custom organization role, for layers that grant it to further identities."
  value       = google_organization_iam_custom_role.secret_container_admin.id
}

output "secret_operator_role" {
  description = "Full name of the secretOperator custom organization role: creates secrets and adds versions, never reads one. 2-env grants it on the environment project to its secret_operators."
  value       = google_organization_iam_custom_role.secret_operator.id
}
