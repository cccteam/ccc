# Consumed by the environment and application layers through
# data "terraform_remote_state" on the state bucket (prefix 2-spn). Nothing
# here is a secret; keep it that way.

output "instance_id" {
  description = "Full resource ID of the shared instance, projects/{project}/instances/{name}."
  value       = google_spanner_instance.shared.id
}

output "instance_name" {
  description = "Name of the shared instance, for google_spanner_database.instance."
  value       = google_spanner_instance.shared.name
}

output "processing_units" {
  description = "Compute capacity of the instance."
  value       = google_spanner_instance.shared.processing_units
}

output "project_id" {
  description = "Project holding the shared instance. A database resource names this project, not the application's."
  value       = local.project_id
}

output "spanner_config" {
  description = "Instance configuration (nam10)."
  value       = var.spanner_config # the resource reports the full projects/.../instanceConfigs/<name> path; the short name is what downstream layers and people use
}
