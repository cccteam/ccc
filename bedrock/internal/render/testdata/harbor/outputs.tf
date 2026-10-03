# What the pipeline reads, and what 2-net reads to route this environment's
# hostnames. Readable by anything that can read the state bucket; no secrets.

output "file_stores" {
  description = "The file stores the site and the job process read and write: each variable and the gs:// URL of its bucket, as the processes receive them."
  value       = local.files_env
}

output "backend_service_id" {
  description = "Backend service URI in the form 2-net's hosts variable takes: projects/<project>/global/backendServices/<name>; null for a pull-request stack."
  value       = try(google_compute_backend_service.app[0].id, null)
}

output "backend_service_self_link" {
  description = "Full self link of the backend service the net project's URL map routes this environment's hostnames to (cross-project reference); null for a pull-request stack."
  value       = try(google_compute_backend_service.app[0].self_link, null)
}

output "database" {
  description = "The Spanner database the site opens: its project, instance, and name; for a pull-request stack in shared mode, tst's."
  value = {
    project  = local.instance.project
    instance = local.instance.name
    name     = local.database_name
  }
}

output "firestore_database" {
  description = "The Firestore database the site, the migrate command and the job process read and write, as APP_FIRESTORE_DATABASE names it to them."
  value       = google_firestore_database.firestore.name
}

output "hostnames" {
  description = "Hostnames this environment's site answers on; 2-net registers them with the load balancer and the certificate."
  value       = local.hostnames
}

output "identities" {
  description = "Runtime identities by process: app (the site), jobs (the job process) and migrate (the migration job)."
  value = {
    app     = google_service_account.app.email
    jobs    = google_service_account.jobs.email
    migrate = google_service_account.migrate.email
  }
}

output "jobs_job" {
  description = "The job process's Cloud Run job, which the application runs: its name, its region and the resource name the Cloud Run API takes."
  value = {
    name     = google_cloud_run_v2_job.jobs.name
    region   = google_cloud_run_v2_job.jobs.location
    resource = local.jobs_job
  }
}

output "net_hosts" {
  description = "The entries to add to 2-net's hosts variable for this environment: each hostname mapped to the backend service URI, and each next hostname (<app>-<env>-next) to the next revision's backend. Empty for a pull-request stack, whose hostname the wildcard rule serves."
  value = local.is_pr ? {} : merge(
    { for host in local.hostnames : host => google_compute_backend_service.app[0].id },
    { for host in local.next_hostnames : host => google_compute_backend_service.next[0].id },
  )
}

output "migrate_job" {
  description = "The migration job the pipeline runs before a release takes traffic: name and region."
  value = {
    name   = google_cloud_run_v2_job.migrate.name
    region = google_cloud_run_v2_job.migrate.location
  }
}

output "registry" {
  description = "Image repository the pipeline pushes to, as <hostname>/<project>/<repository>; null until 2-shr registers the application."
  value       = local.registry
}

output "secrets" {
  description = "Secret container ID by the environment variable it feeds, and the version this environment runs (null when not pinned yet)."
  value = {
    for key, s in local.secrets : key => {
      secret_id = local.secret_ids[key]
      version   = lookup(local.secret_versions, key, null)
    }
  }
}

output "services" {
  description = "Cloud Run service by region code: name, region, and the run.app URI (not the public one; that is the hostname through the load balancer)."
  value = {
    for code, svc in google_cloud_run_v2_service.app : code => {
      name   = svc.name
      region = svc.location
      uri    = svc.uri
    }
  }
}

output "staff_oidc_redirect_url" {
  description = "Redirect URI to register on the OAuth client in the environment project (APP_STAFF_OIDC_REDIRECT_URL)."
  value       = local.redirect_url
}

output "tasks_queue" {
  description = "The task queue the site and the job process enqueue on, as APP_TASKS_QUEUE names it to them; a pull-request stack's is tst's."
  value       = local.tasks_queue
}

output "substitutions" {
  description = "The substitutions the version trigger passes to cloudbuild.yaml, which a hand-submitted tag build passes too (_PR_NUMBER empty: no pull request). The pull-request trigger passes the same without _PR_NUMBER, which its event supplies."
  value       = merge(local.substitutions, local.custom_substitutions, { _PR_NUMBER = "" })
}

output "triggers" {
  description = "Cloud Build trigger IDs: version in every environment, pr in tst; null until 2-env holds the GitHub connection."
  value = {
    version = try(google_cloudbuild_trigger.version[0].id, null)
    pr      = try(google_cloudbuild_trigger.pr[0].id, null)
  }
}
