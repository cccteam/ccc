# ---------------------------------------------------------------------------
# The migrate job's lines, for a person without a console
#
# Cloud Run does not return a job's output with its execution, so the pipeline
# reads the lines the migrate job wrote from Cloud Logging and prints them into
# the build log, which the operations workflow's run shows: a failed
# migration's message, or the database's migration version, reaches the person
# who started the run without a console. The read is bounded. A sink routes the
# migrate job's entries alone (the template job's copies, named after it with
# each build's version, and in tst the pull requests' migrate jobs)
# into a log bucket of their own, and the deploy identity and the operations
# identity may read that bucket's view and nothing wider, so neither reads the
# application's own logs. A log view's filter cannot name a job (it admits a
# resource type, a log id and an origin, and no label), which is why the
# entries get a bucket of their own; they stay in the project's _Default bucket
# too, where they always were. None of this exists in prd: the door this
# serves does not reach production, whose logs are read in the console.
# ---------------------------------------------------------------------------

locals {
  # The environment's own stack, below production: a pull-request stack shares
  # tst's bucket, whose sink takes the pull requests' jobs too.
  migrate_logs = !local.is_pr && !local.is_prd

  # The jobs whose entries the sink routes, by name: the template job's copies
  # (<template>-<version key>), and in tst the pull requests' (<app>-pr<N>-migrate-<key>).
  migrate_logs_jobs = concat(
    ["^${google_cloud_run_v2_job.migrate.name}-"],
    var.environment == "tst" ? ["^${local.app}-pr[0-9]+-migrate-"] : [],
  )
  migrate_logs_filter = "resource.type=\"cloud_run_job\" AND resource.labels.job_name=~\"${join("|", local.migrate_logs_jobs)}\""

  # The view the pipeline and the workflow read through (_MIGRATE_LOGS); empty
  # where there is none.
  migrate_logs_view = try("projects/${local.project_id}/locations/global/buckets/${google_logging_project_bucket_config.migrate_logs[0].bucket_id}/views/_AllLogs", "")
}

resource "google_logging_project_bucket_config" "migrate_logs" {
  count = local.migrate_logs ? 1 : 0

  project        = local.project_id
  location       = "global"
  bucket_id      = "${local.name}-gbl-${local.app}-migrate-logs"
  description    = "The lines harbor's migrate job wrote in ${var.environment}: what the pipeline prints into the build log and the operations workflow shows."
  retention_days = 30
}

resource "google_logging_project_sink" "migrate_logs" {
  count = local.migrate_logs ? 1 : 0

  project     = local.project_id
  name        = "${local.name}-gbl-${local.app}-migrate-logs"
  description = "Routes the entries of harbor's migrate jobs into their own bucket, for the bounded read."
  destination = "logging.googleapis.com/${google_logging_project_bucket_config.migrate_logs[0].id}"
  filter      = local.migrate_logs_filter

  unique_writer_identity = true
}

# The deploy identity reads the view, so bedrock deploy migrate prints the
# job's lines; the operations identity reads it, so the workflow's migration
# job prints them in its summary. The grant is the view's alone: a condition on
# the view's name bounds roles/logging.viewAccessor to it.
resource "google_project_iam_member" "deploy_reads_migrate_logs" {
  count = local.migrate_logs ? 1 : 0

  project = local.project_id
  role    = "roles/logging.viewAccessor"
  member  = local.identities.deploy_identity_member

  condition {
    title       = "${local.name}-${local.app}-migrate-logs"
    description = "The view over the migrate job's own log bucket, and no other log."
    expression  = "resource.name == \"${local.migrate_logs_view}\""
  }
}

resource "google_project_iam_member" "operations_reads_migrate_logs" {
  count = local.migrate_logs && try(local.identities.operations_identity_member, null) != null ? 1 : 0

  project = local.project_id
  role    = "roles/logging.viewAccessor"
  member  = local.identities.operations_identity_member

  condition {
    title       = "${local.name}-${local.app}-migrate-logs"
    description = "The view over the migrate job's own log bucket, and no other log."
    expression  = "resource.name == \"${local.migrate_logs_view}\""
  }
}
