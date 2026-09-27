# Read by hand when substituting values into 1-org/terraform.tfvars, and by any
# later automation through data "terraform_remote_state" on the state bucket
# (prefix 0-bootstrap). Nothing here is a secret; keep it that way.

output "boot_project_id" {
  description = "Project ID of the boot project, used as the quota and billing project by every downstream layer."
  value       = module.boot_project.project_id
}

output "boot_service_account_email" {
  description = "Email of the boot layer identity. Cloud Build runs this layer as it once the runner is wired."
  value       = google_service_account.boot_tofu.email
}

output "org_id" {
  description = "Numeric ID of the GCP Organization."
  value       = local.org_id
}

output "org_service_account_email" {
  description = "Email of the org layer identity. Cloud Build runs 1-org as it once the runner is wired."
  value       = google_service_account.org_tofu.email
}

output "terraform_folder_id" {
  description = "Folder ID of the terraform folder holding the automation plane."
  value       = google_folder.terraform.folder_id
}
