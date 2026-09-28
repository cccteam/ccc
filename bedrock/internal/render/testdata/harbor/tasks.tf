# ---------------------------------------------------------------------------
# Cloud Tasks
#
# The task queue: dataConfig.TasksQueue (pkg/config/data.go) names it to the
# processes that construct the data level, which enqueue HTTP tasks on it as
# their own identities and are called back by them: a task carries the OIDC
# token of the identity that enqueued it, and the application verifies the
# token on the way back in. One queue per environment, in the primary region;
# a pull-request stack enqueues on tst's queue and grants its own identity
# on it, because a deleted queue's name stays reserved for seven days, which a
# pull request torn down and rebuilt within the week would trip over. The
# migrate command receives neither the name nor a grant: its work is known.
# ---------------------------------------------------------------------------

resource "google_cloud_tasks_queue" "tasks" {
  count = local.is_pr ? 0 : 1

  project  = local.project_id
  location = local.primary_region
  name     = local.tasks_queue_name

  rate_limits {
    max_concurrent_dispatches = var.tasks_max_concurrent
  }

  retry_config {
    max_attempts = var.tasks_max_attempts
  }
}

# Enqueuers, by the queue's name (a pull-request stack binds on tst's
# queue): the site and the job process.
resource "google_cloud_tasks_queue_iam_member" "tasks_app" {
  project  = local.project_id
  location = local.primary_region
  name     = local.tasks_queue_name
  role     = "roles/cloudtasks.enqueuer"
  member   = local.app_member

  depends_on = [google_cloud_tasks_queue.tasks, google_service_account.app]
}

# A task calls the application back with an OIDC token of the identity that
# enqueued it, which takes Service Account User on that identity's own account.
resource "google_service_account_iam_member" "app_acts_as_itself" {
  service_account_id = local.app_account_name
  role               = "roles/iam.serviceAccountUser"
  member             = local.app_member

  depends_on = [google_service_account.app]
}

resource "google_cloud_tasks_queue_iam_member" "tasks_jobs" {
  project  = local.project_id
  location = local.primary_region
  name     = local.tasks_queue_name
  role     = "roles/cloudtasks.enqueuer"
  member   = local.jobs_member

  depends_on = [google_cloud_tasks_queue.tasks, google_service_account.jobs]
}

resource "google_service_account_iam_member" "jobs_acts_as_itself" {
  service_account_id = local.jobs_account_name
  role               = "roles/iam.serviceAccountUser"
  member             = local.jobs_member

  depends_on = [google_service_account.jobs]
}
