# Read by hand when substituting values into 1-org/terraform.tfvars and
# placement.json, and by 1-org through data "terraform_remote_state" on the
# state bucket (prefix 0-bootstrap). Nothing here is a secret; keep it that way.

output "boot_project_id" {
  description = "Project ID of the boot project, used as the quota and billing project by every downstream layer."
  value       = module.boot_project.project_id
}

output "boot_project_number" {
  description = "Number of the boot project; placement.json records it (projectNumbers.boot) so the layers workflow can name the identity provider."
  value       = module.boot_project.project_number
}

output "boot_service_account_email" {
  description = "Email of the boot layer identity. The layers workflow applies this layer as it, after the first apply."
  value       = google_service_account.boot_tofu.email
}

output "github_infrastructure_key_secret" {
  description = "Secret Manager container in the boot project holding the infrastructure GitHub App's private key; 1-org grants its plan identity the read."
  value       = google_secret_manager_secret.github_infrastructure_key.secret_id
}

output "github_pool_name" {
  description = "Full name of the boot project's workload identity pool for the layers workflow (projects/<number>/locations/global/workloadIdentityPools/<id>); 1-org binds the identities it creates to its principal sets."
  value       = google_iam_workload_identity_pool.github.name
}

output "github_provider_name" {
  description = "Full name of the pool's GitHub provider, the one the workflow's sign-in step names."
  value       = google_iam_workload_identity_pool_provider.github.name
}

output "org_id" {
  description = "Numeric ID of the GCP Organization."
  value       = local.org_id
}

output "org_service_account_email" {
  description = "Email of the org layer identity. The layers workflow applies 1-org as it, after the first apply."
  value       = google_service_account.org_tofu.email
}

output "state_bucket_policy_admin_role" {
  description = "Full name of the custom organization role that reads and sets a bucket's IAM policy; 1-org grants it on the state bucket to the environment layer identities, which grant the application identities their slots."
  value       = google_organization_iam_custom_role.state_bucket_policy_admin.id
}

output "terraform_folder_id" {
  description = "Folder ID of the terraform folder holding the automation plane."
  value       = google_folder.terraform.folder_id
}
