# Read by hand when substituting values into 1-org/terraform.tfvars and
# placement.json, and by 1-org through data "terraform_remote_state" on the
# state bucket (prefix 0-bootstrap). Nothing here is a secret; keep it that way.

output "boot_project_id" {
  description = "Project ID of the boot project, used as the quota and billing project by every downstream layer."
  value       = module.boot_project.project_id
}

output "boot_project_number" {
  description = "Number of the boot project; placement.json records it (projectNumbers.boot)."
  value       = module.boot_project.project_number
}

output "boot_service_account_email" {
  description = "Email of the boot layer identity. The apply trigger's run applies this layer as it, after the first apply."
  value       = google_service_account.boot_tofu.email
}

output "github_infrastructure_key_secret" {
  description = "Secret Manager container in the boot project holding the infrastructure GitHub App's private key; 1-org grants its plan identity the read."
  value       = google_secret_manager_secret.github_infrastructure_key.secret_id
}

output "github_deployer_key_secret" {
  description = "Secret Manager container in the boot project holding the deployer GitHub App's private key; 1-org grants each environment layer identity its administration, so 2-env can grant the applications' identities the read."
  value       = google_secret_manager_secret.github_deployer_key.secret_id
}

output "planner_member" {
  description = "The plan trigger's runner identity as an IAM member; 1-org lets it impersonate the plan identities it creates (roles/iam.serviceAccountTokenCreator)."
  value       = google_service_account.planner.member
}

output "applier_member" {
  description = "The apply trigger's runner identity as an IAM member; 1-org lets it impersonate the apply identities it creates."
  value       = google_service_account.applier.member
}

output "connection_name" {
  description = "Short name of the boot project's Cloud Build GitHub connection, the one the triggers watch the infrastructure repository through; null until the authorization is recorded in placement.json."
  value       = one(google_cloudbuildv2_connection.github[*].name)
}

output "plan_trigger_name" {
  description = "The plan trigger, which runs on a pull request into the default branch; null until the authorization is recorded."
  value       = one(google_cloudbuild_trigger.plan[*].name)
}

output "apply_trigger_name" {
  description = "The apply trigger, which runs on a push to the default branch and, started by hand with _LAYER, applies one layer again; null until the authorization is recorded."
  value       = one(google_cloudbuild_trigger.apply[*].name)
}

output "org_id" {
  description = "Numeric ID of the GCP Organization."
  value       = local.org_id
}

output "org_service_account_email" {
  description = "Email of the org layer identity. The apply trigger's run applies 1-org as it, after the first apply."
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
