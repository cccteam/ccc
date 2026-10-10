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
  description    = "The build logs of harbor's pipeline in ${var.environment}, the migration's lines among them: what the operations workflow shows."
  retention_days = 30
}

resource "google_logging_project_sink" "migrate_logs" {
  count = local.migrate_logs ? 1 : 0

  project     = local.project_id
  name        = "${local.name}-gbl-${local.app}-migrate-logs"
  description = "Routes the entries of harbor's builds into their own bucket, for the bounded read."
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
# Cloud Run and the load balancer each write an entry for every request the
# service answers, whatever the application's own logger decides. The code
# declares per surface which requests get an entry, with the request log
# words the generated router applies to the application's entries and lists
# in pkg/router/zz_gen_release.json (surfaces); this exclusion holds the
# cloud's own entries to the same words, so a surface logged on event alone
# costs no entry for its quiet requests anywhere. Its scope is the service,
# Cloud Run's entries by the regional services' names and the load
# balancer's by the backend's, and never the project: the project holds
# several applications, and another's entries are not this stack's to drop.
# The path is matched after any host (^https://[^/]+<prefix>), since the
# environment's hostnames and the next revision's all reach the same
# service and the word is the path's; a route is matched to the end of its
# path, a query string allowed, and a prefix by prefix, as the router
# matches them. The next revision's own backend is left out: its entries
# are the hook's few calls before traffic moves, each worth seeing. A
# pull-request stack has none: its service answers through 2-env's wildcard
# backend, not one of its own, and every one of its few requests is worth
# seeing.
# ---------------------------------------------------------------------------

locals {
  # The surfaces whose word excludes an entry, from pkg/router/zz_gen_release.json:
  #   /api/, sampled at 0.1
  #   /api/manifests/{id}/file, never logged
  # A surface logged always has no clause. On event drops the entry of a
  # request that answered below 400 and keeps every failure, since the edge
  # cannot know whether a line attached; sampled drops the same but the
  # declared fraction of them, decided by the entry's insertId; never drops
  # every entry under the prefix. Each clause excepts the surfaces declared
  # beneath its surface, whatever their words, since the nearest declaration
  # decides a request's entry: an entry under a child is the child's own
  # clause's to drop, and kept whole when the child is logged always.
  request_log_clause = "((httpRequest.requestUrl =~ \"^https://[^/]+/api/\" AND NOT httpRequest.requestUrl =~ \"^https://[^/]+/api/manifests/[^/]+/file([?]|$)\" AND httpRequest.status < 400 AND NOT sample(insertId, 0.1)) OR (httpRequest.requestUrl =~ \"^https://[^/]+/api/manifests/[^/]+/file([?]|$)\"))"
}

resource "google_logging_project_exclusion" "request_log" {
  count = local.is_pr ? 0 : 1

  project     = local.project_id
  name        = "${local.name}-gbl-${local.app}-log-excl"
  description = "Holds the request log entries Cloud Run and the load balancer write for harbor in ${var.environment} to the request log words its code declares per surface (pkg/router/zz_gen_release.json)."
  filter = join(" AND ", [
    "((resource.type=\"cloud_run_revision\" AND (${join(" OR ", [for service in google_cloud_run_v2_service.app : "resource.labels.service_name=\"${service.name}\""])})) OR (resource.type=\"http_load_balancer\" AND resource.labels.backend_service_name=\"${google_compute_backend_service.app[0].name}\"))",
    local.request_log_clause,
  ])
}
