# ---------------------------------------------------------------------------
# The database
#
# One database per application per environment, on the environment's
# instance (2-env output spanner_instance: tst's own, or the shared spn
# instance for stg and prd). GoogleSQL dialect, as the migrations under
# schema/migrations are written. No schema here: the migrations own it, run
# by the pipeline on the build worker (pkg/deploy MigrateSchema, the release's own
# migrate command) as the deploy identity.
#
# Modeled on CCC's reference deployment. Deletion protection in
# both its forms is on for prd: the provider-side flag stops a plan from
# destroying the database, and drop protection stops every interface,
# including the console, and with it the parent instance.
#
# A pull-request stack in shared mode (var.shared_database) creates none: the
# site runs against tst's database, named by local.database_name, and
# the app identity is granted on it below; no DDL grant, and the pipeline
# never runs the migrations.
# ---------------------------------------------------------------------------

# The first generation keeps its name whatever generation is current
# (local.database_base, not local.database_name): a rollback moves the current
# generation to a restored database below, and this one stays as it is, the
# forensic copy among them. Named after the current generation it would be
# replaced on the first build after a rollback, which the deletion protection
# refuses, as the lab's first production rollback showed.
resource "google_spanner_database" "harbor" {
  count = local.own_database ? 1 : 0

  project  = local.instance.project
  instance = local.instance.name
  name     = local.database_base

  database_dialect         = "GOOGLE_STANDARD_SQL"
  version_retention_period = local.spanner_retention[var.environment]
  deletion_protection      = local.is_prd
  enable_drop_protection   = local.is_prd
}

# The database gained a count when shared mode arrived; the one that exists
# keeps its state.
moved {
  from = google_spanner_database.harbor
  to   = google_spanner_database.harbor[0]
}

# The databases a rollback restored a backup into, generation 2 onwards
# (var.database_generation): each named after the first with its number, made
# by the rollback build that restored it and imported into this stack before
# its plan (bedrock deploy stack plan), with the first's protections. The
# current generation is local.database_name, which the grants, the schedules,
# the service and the migrate command follow; the earlier ones stay as they
# are, the broken database among them as the forensic copy, until a later
# change removes them. A pull-request stack has one generation.
resource "google_spanner_database" "restored" {
  for_each = local.own_database && !local.is_pr ? toset([for g in range(2, var.database_generation + 1) : tostring(g)]) : toset([])

  project  = local.instance.project
  instance = local.instance.name
  name     = "${local.database_base}-${each.key}"

  database_dialect         = "GOOGLE_STANDARD_SQL"
  version_retention_period = local.spanner_retention[var.environment]
  deletion_protection      = local.is_prd
  enable_drop_protection   = local.is_prd
}

# Rows, as the site reads and writes them (config.NewDataConfiguration opens
# the Spanner client as this identity). By name, so that in shared mode the
# membership lands on tst's database: an additive member naming the pull
# request's own account, destroyed with the stack.
resource "google_spanner_database_iam_member" "app_user" {
  project  = local.instance.project
  instance = local.instance.name
  database = local.database_name
  role     = "roles/spanner.databaseUser"
  member   = local.app_member

  depends_on = [google_spanner_database.harbor, google_spanner_database.restored, google_service_account.app]

  # A recreated database (/gcbrun reload-db replaces it) starts with no
  # members; the membership is recreated with it rather than believed to
  # exist.
  lifecycle {
    replace_triggered_by = [google_spanner_database.harbor]
  }
}

# The job process (cmd/jobs) constructs the data level and opens the
# database the way the site does, as its own identity.
resource "google_spanner_database_iam_member" "jobs_user" {
  project  = local.instance.project
  instance = local.instance.name
  database = local.database_name
  role     = "roles/spanner.databaseUser"
  member   = local.jobs_member

  depends_on = [google_spanner_database.harbor, google_spanner_database.restored, google_service_account.jobs]

  lifecycle {
    replace_triggered_by = [google_spanner_database.harbor]
  }
}

# DDL, for the migrations (pkg/deploy MigrateSchema), which the pipeline runs on
# the build worker as the deploy identity from 2-env: database admin on this
# database only, never on a shared one, and nothing on the instance. The grant
# is a member of the database's own policy, so the database's name bounds it
# the way the apply identity's instance-level roles are bounded by a condition
# naming the application's databases.
resource "google_spanner_database_iam_member" "deploy_admin" {
  count = local.own_database ? 1 : 0

  project  = local.instance.project
  instance = local.instance.name
  database = local.database_name
  role     = "roles/spanner.databaseAdmin"
  member   = local.identities.deploy_identity_member

  depends_on = [google_spanner_database.harbor, google_spanner_database.restored]

  lifecycle {
    replace_triggered_by = [google_spanner_database.harbor[0]]
  }
}

# Client-side metrics. The Spanner client in each process that opens the
# database (the site, the job process and the migrate command, which the
# pipeline runs on the build worker as the deploy identity) writes its own view
# of each request (operation and attempt latency, counts) to Cloud Monitoring
# in the project that owns the instance, and logs a denial at every export
# when it lacks roles/monitoring.metricWriter there. On an environment's own
# instance (tst's) that is the environment project, where the identities
# hold the role already (service-accounts.tf), and nothing is granted here; on
# the shared instance (stg and prd) it is the spn project, where this stack
# grants the role to them and nothing else: 2-spn lets the apply identity
# grant this one role there. A pull-request stack grants nothing to the
# deploy identity, which the environment's own stack covers.
locals {
  spanner_metrics_writers = local.instance.project == local.project_id ? {} : merge(
    { app = local.app_member },
    { jobs = local.jobs_member },
    local.is_pr ? {} : { deploy = local.identities.deploy_identity_member },
  )
}

resource "google_project_iam_member" "spanner_metrics" {
  for_each = local.spanner_metrics_writers

  project = local.instance.project
  role    = "roles/monitoring.metricWriter"
  member  = each.value

  depends_on = [google_service_account.app, google_service_account.jobs]
}

# ---------------------------------------------------------------------------
# Backups, prd only: a weekly full backup and a daily incremental one, each
# kept 90 days, the cadence and retention of the reference deployment with the
# incremental frequency relaxed from twice a day to once, on the current
# generation of the database. The instance's default backup schedule is NONE
# (2-env, 2-spn), so a database that wants backups says so here. Version times
# are UTC. The backups a release build takes as of its cut, kept fourteen days,
# and the ones a rollback takes are the pipeline's, not schedules.
# ---------------------------------------------------------------------------

resource "google_spanner_backup_schedule" "weekly_full" {
  count = local.is_prd ? 1 : 0

  project  = local.instance.project
  instance = local.instance.name
  database = local.database_name
  name     = "${local.database_name}-weekly-backup"

  retention_duration = "7776000s" # 90 days

  spec {
    cron_spec {
      text = "0 2 * * 0" # Sundays, 02:00 UTC
    }
  }

  full_backup_spec {}

  depends_on = [google_spanner_database.harbor, google_spanner_database.restored]
}

resource "google_spanner_backup_schedule" "daily_incremental" {
  count = local.is_prd ? 1 : 0

  project  = local.instance.project
  instance = local.instance.name
  database = local.database_name
  name     = "${local.database_name}-daily-backup"

  retention_duration = "7776000s" # 90 days

  spec {
    cron_spec {
      text = "0 2 * * *" # daily, 02:00 UTC
    }
  }

  incremental_backup_spec {}

  # An incremental chain hangs off a full backup.
  depends_on = [google_spanner_backup_schedule.weekly_full, google_spanner_database.harbor, google_spanner_database.restored]
}
