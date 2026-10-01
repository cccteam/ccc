# Read by the application stacks (each in its own repository, state slot 3-app/<code>) through data
# "terraform_remote_state" on the state bucket, prefix 2-env/<environment>.
# Anything that can read the bucket can read these, so keep secrets out.

output "applications" {
  description = "Per application: the apply, deploy and plan identities (email, member, and the deploy identity's full resource name for Cloud Build triggers) and the Cloud Build repository link (ID and name)."
  value = {
    for app in var.applications : app => {
      apply_identity_email   = google_service_account.apply[app].email
      apply_identity_member  = google_service_account.apply[app].member
      deploy_identity_email  = google_service_account.deploy[app].email
      deploy_identity_member = google_service_account.deploy[app].member
      deploy_identity_id     = google_service_account.deploy[app].name
      plan_identity_email    = google_service_account.plan[app].email
      plan_identity_member   = google_service_account.plan[app].member
      repository_id          = try(google_cloudbuildv2_repository.app[app].id, null)
      repository_name        = try(google_cloudbuildv2_repository.app[app].name, null)
    }
  }
}

output "connection_id" {
  description = "Full resource name of the Cloud Build GitHub connection in this environment (projects/.../locations/.../connections/...); null until the GitHub values are set."
  value       = one(google_cloudbuildv2_connection.github[*].id)
}

output "connection_name" {
  description = "Short name of the Cloud Build GitHub connection; null until the GitHub values are set."
  value       = one(google_cloudbuildv2_connection.github[*].name)
}

output "environment" {
  description = "Environment code this state describes."
  value       = var.environment
}

output "prefix" {
  description = "Org-wide naming prefix, restated from 1-org."
  value       = local.prefix
}

output "project_id" {
  description = "Environment project."
  value       = local.project_id
}

output "project_number" {
  description = "Environment project number, for service agent members."
  value       = local.project_number
}

output "records_bucket" {
  description = "Deployment-record bucket every deploy in this environment writes to."
  value       = google_storage_bucket.records.name
}

output "region" {
  description = "Primary region."
  value       = local.region
}

output "region_code" {
  description = "Short code of the primary region."
  value       = local.region_code
}

output "secondary_region" {
  description = "Secondary region."
  value       = local.secondary_region
}

output "secondary_region_code" {
  description = "Short code of the secondary region."
  value       = local.secondary_region_code
}

output "spanner_instance" {
  description = "The Spanner instance applications in this environment put their databases on: its project and name. tst's own instance, or the shared one from 2-spn for stg and prd."
  value = {
    project = local.instance_project
    name    = local.instance_name
  }
}

output "pull_request_backend_id" {
  description = "In tst only: the backend service that serves every pull-request environment of every application by hostname (*.<apps domain>, the URL mask picks the Cloud Run service named by the first label); 2-net's hosts maps the wildcard to it. Null elsewhere."
  value       = try(google_compute_backend_service.pull_requests[0].id, null)
}

output "github_deployer_app_id" {
  description = "App ID of the deployer GitHub App the pipeline talks back on a pull request as; null when unset."
  value       = var.github_deployer_app_id
}

output "github_deployer_key_secret_version" {
  description = "Resource name of the pinned Secret Manager version holding the deployer app's private key in this environment's container; null until an operator has added it and pinned it here."
  value       = lookup(var.github_deployer_key_secret_versions, var.environment, null)
}
