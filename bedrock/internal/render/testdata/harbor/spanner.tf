# ---------------------------------------------------------------------------
# The database
#
# One database per application per environment, on the environment's
# instance (2-env output spanner_instance: tst's own, or the shared spn
# instance for stg and prd). GoogleSQL dialect, as the migrations under
# schema/migrations are written. No schema here: the migrations own it, run
# by the migrate job (pkg/deploy MigrateSchema) as the migrate identity.
#
# Modeled on CCC's reference deployment. Deletion protection in
# both its forms is on for prd: the provider-side flag stops a plan from
# destroying the database, and drop protection stops every interface,
# including the console, and with it the parent instance.
#
# A pull-request stack in shared mode (var.shared_database) creates none: the
# site runs against tst's database, named by local.database_name, and
# the app identity is granted on it below; no DDL grant, and the pipeline
# never runs the migrate job.
# ---------------------------------------------------------------------------

resource "google_spanner_database" "harbor" {
  count = local.own_database ? 1 : 0

  project  = local.instance.project
  instance = local.instance.name
  name     = local.database_name

  database_dialect         = "GOOGLE_STANDARD_SQL"
  version_retention_period = "1h"
  deletion_protection      = local.is_prd
  enable_drop_protection   = local.is_prd
}

# The database gained a count when shared mode arrived; the one that exists
# keeps its state.
moved {
  from = google_spanner_database.harbor
  to   = google_spanner_database.harbor[0]
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

  depends_on = [google_spanner_database.harbor, google_service_account.app]
}

# DDL, for the migrations (pkg/deploy MigrateSchema), on this database only, and
# never on a shared one.
resource "google_spanner_database_iam_member" "migrate_admin" {
  count = local.own_database ? 1 : 0

  project  = local.instance.project
  instance = local.instance.name
  database = google_spanner_database.harbor[0].name
  role     = "roles/spanner.databaseAdmin"
  member   = local.migrate_member

  depends_on = [google_service_account.migrate]
}

moved {
  from = google_spanner_database_iam_member.migrate_admin
  to   = google_spanner_database_iam_member.migrate_admin[0]
}

# ---------------------------------------------------------------------------
# Backups, prd only: a weekly full backup and a daily incremental one, each
# kept 90 days, the cadence and retention of the reference deployment with the
# incremental frequency relaxed from twice a day to once. The instance's
# default backup schedule is NONE (2-env, 2-spn), so a database that wants
# backups says so here. Version times are UTC.
# ---------------------------------------------------------------------------

resource "google_spanner_backup_schedule" "weekly_full" {
  count = local.is_prd ? 1 : 0

  project  = local.instance.project
  instance = local.instance.name
  database = google_spanner_database.harbor[0].name
  name     = "${local.database_name}-weekly-backup"

  retention_duration = "7776000s" # 90 days

  spec {
    cron_spec {
      text = "0 2 * * 0" # Sundays, 02:00 UTC
    }
  }

  full_backup_spec {}
}

resource "google_spanner_backup_schedule" "daily_incremental" {
  count = local.is_prd ? 1 : 0

  project  = local.instance.project
  instance = local.instance.name
  database = google_spanner_database.harbor[0].name
  name     = "${local.database_name}-daily-backup"

  retention_duration = "7776000s" # 90 days

  spec {
    cron_spec {
      text = "0 2 * * *" # daily, 02:00 UTC
    }
  }

  incremental_backup_spec {}

  # An incremental chain hangs off a full backup.
  depends_on = [google_spanner_backup_schedule.weekly_full]
}
