# Consumed by the environment and application layers through
# data "terraform_remote_state" on the state bucket (prefix 2-shr). Nothing
# here is a secret; keep it that way.

output "gcp_region" {
  description = "Region the repositories live in."
  value       = local.region
}

output "image_paths" {
  description = "Image path prefix by application: {registry_hostname}/{project}/{repository}. Append /{image}:{tag}."
  value       = { for k, r in google_artifact_registry_repository.app : k => "${local.registry_hostname}/${local.project_id}/${r.repository_id}" }
}

output "project_id" {
  description = "Shared services project."
  value       = local.project_id
}

output "registry_hostname" {
  description = "Docker hostname of the registry, for docker login and image references."
  value       = local.registry_hostname
}

output "repository_ids" {
  description = "Full resource ID of each application's repository, by application."
  value       = { for k, r in google_artifact_registry_repository.app : k => r.id }
}

output "repository_names" {
  description = "Repository ID (the last path segment) of each application's repository, by application."
  value       = { for k, r in google_artifact_registry_repository.app : k => r.repository_id }
}

output "tools_image_path" {
  description = "Image path prefix of the tools repository: {registry_hostname}/{project}/{repository}. bedrock's image lives at /bedrock, pinned by digest in each application's placement."
  value       = "${local.registry_hostname}/${local.project_id}/${google_artifact_registry_repository.tools.repository_id}"
}
