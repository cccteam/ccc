# ---------------------------------------------------------------------------
# The migration's lines, for a person without a console
#
# The pipeline runs the migrate command on the build worker, so what it
# writes (a failed migration's message, the database's migration version) is
# in the build log, which the operations workflow's run reads and shows to
# the person who started the run without a console. The read is bounded. A
# sink routes the entries of this application's builds alone (the builds of
# its version trigger and, in tst, its pull-request trigger) into a log
# bucket of their own, and the operations identity may read that bucket's view
# and nothing wider, so it reads neither the application's own logs nor
# another application's builds. A log view's filter cannot name a trigger (it
# admits a resource type, a log id and an origin, and no label), which is why
# the entries get a bucket of their own; they stay in the project's _Default
# bucket too, where they always were. The deploy identity reads nothing here:
# the lines are its own step's. None of this exists in prd: the door this
# serves does not reach production, whose logs are read in the console.
# ---------------------------------------------------------------------------

locals {
  # The environment's own stack, below production: a pull-request stack shares
  # tst's bucket, whose sink takes the pull requests' builds too.
  migrate_logs = !local.is_pr && !local.is_prd

  # The triggers whose builds the sink routes, by id: the version trigger, and
  # in tst the pull-request trigger. While 2-env holds no connection yet
  # neither exists, and the filter names an id no build carries.
  migrate_logs_triggers = coalescelist(compact([
    try(google_cloudbuild_trigger.version[0].trigger_id, ""),
    try(google_cloudbuild_trigger.pr[0].trigger_id, ""),
  ]), ["none"])
  migrate_logs_filter = "resource.type=\"build\" AND resource.labels.build_trigger_id=~\"^(${join("|", local.migrate_logs_triggers)})$\""

  # The view the operations workflow reads through; empty where there is none.
  migrate_logs_view = try("projects/${local.project_id}/locations/global/buckets/${google_logging_project_bucket_config.migrate_logs[0].bucket_id}/views/_AllLogs", "")
}

resource "google_logging_project_bucket_config" "migrate_logs" {
  count = local.migrate_logs ? 1 : 0

  project        = local.project_id
  location       = "global"
  bucket_id      = "${local.name}-gbl-${local.app}-migrate-logs"
  description    = "The build logs of beacon's pipeline in ${var.environment}, the migration's lines among them: what the operations workflow shows."
  retention_days = 30
}

resource "google_logging_project_sink" "migrate_logs" {
  count = local.migrate_logs ? 1 : 0

  project     = local.project_id
  name        = "${local.name}-gbl-${local.app}-migrate-logs"
  description = "Routes the entries of beacon's builds into their own bucket, for the bounded read."
  destination = "logging.googleapis.com/${google_logging_project_bucket_config.migrate_logs[0].id}"
  filter      = local.migrate_logs_filter

  unique_writer_identity = true
}

# The operations identity reads the view, so the workflow's migration job
# prints the migration's lines in its summary. The grant is the view's alone: a
# condition on the view's name bounds roles/logging.viewAccessor to it.
resource "google_project_iam_member" "operations_reads_migrate_logs" {
  count = local.migrate_logs && try(local.identities.operations_identity_member, null) != null ? 1 : 0

  project = local.project_id
  role    = "roles/logging.viewAccessor"
  member  = local.identities.operations_identity_member

  condition {
    title       = "${local.name}-${local.app}-migrate-logs"
    description = "The view over the bucket holding the application's build logs, and no other log."
    expression  = "resource.name == \"${local.migrate_logs_view}\""
  }
}

# ---------------------------------------------------------------------------
# The request log's exclusion
#
# Nothing here yet. Cloud Run and the load balancer each write an entry for
# every request the service answers, and so does the application. An
# exclusion appears when the code declares a request log word that drops
# some (on event, sampled or never: WithRequestLog, OutletRequestLog,
# WithMountedRoutes, or log: on @rpc), which the generated router lists in
# the release file beside the router (zz_gen_release.json, surfaces). The
# stack then creates, in every environment but never in a pull-request
# stack, one project-level exclusion imp-<env>-gbl-beacon-log-excl
# scoped to this service's entries, which drops what the words drop.
# ---------------------------------------------------------------------------
