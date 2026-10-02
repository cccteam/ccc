# ---------------------------------------------------------------------------
# Shared Spanner instance
#
# One instance for stg and prd, in its own project, so the cost of a
# multi-region instance is paid once and the instance outlives any one
# application. tst holds its own instance in its own project (pull-request
# databases come and go there), so nothing here is reachable from a
# pull-request build. Databases are not created here: each application's
# layer creates its own databases on this instance, in stg and in prd, and
# grants on them. No autoscaler: 100 processing units is the floor for a
# multi-region instance and 200 the ceiling by decision, and an autoscaler
# would only move between the two. A multi-region instance is Enterprise
# Plus edition by Google's rule, about $225 a month at 100 processing units
# (2026-09-25 catalog), against $66 for a regional Standard instance; that
# difference is why tst (2-env) runs a regional instance and this one, the
# production topology, is the multi-region one.
# ---------------------------------------------------------------------------

locals {
  edition = coalesce(var.edition, startswith(var.spanner_config, "regional-") ? "STANDARD" : "ENTERPRISE_PLUS")
}

resource "google_spanner_instance" "shared" {
  project          = local.project_id
  name             = "${local.name_prefix}-spanner"
  display_name     = "Shared Spanner instance"
  config           = var.spanner_config
  processing_units = var.processing_units
  edition          = local.edition
  labels           = local.labels

  # The databases on this instance belong to the application layers. A plan
  # here must never be able to delete them, so the instance refuses to go
  # while any database exists, and refuses to go at all below.
  force_destroy = false

  # NONE, so whether a database gets a backup schedule is decided per database
  # by the layer that creates it (prd yes, and the rest as each application
  # chooses). Left unset, Spanner attaches its own daily schedule to every new
  # database, which the first prd apply showed beside the layer's two.
  default_backup_schedule_type = "NONE"

  # Deleting the instance takes a deliberate two-step change: remove this
  # block, then destroy.
  lifecycle {
    prevent_destroy = true

    precondition {
      condition     = startswith(var.spanner_config, "regional-") || local.edition == "ENTERPRISE_PLUS"
      error_message = "A multi-region configuration (${var.spanner_config}) needs the ENTERPRISE_PLUS edition; leave edition unset."
    }
  }
}

# Instance-level grants, bounded to each application's own database and
# backups. Creating a database and listing what the instance holds are checked
# on the instance, so every apply identity holds the organization's
# spannerDatabaseCreator role here without condition; the admin roles are
# conditioned on the resource's name, so an identity reaches its own database
# ("<prefix>-<environment>-gbl-<application>-", with the schedules and
# operations under it) and its own backups, and nothing of another
# environment's or another application's. stg's identity can neither drop
# production's database nor restore over it; it may restore from production's
# backups alone (restore_admin below).
locals {
  instance_path = "projects/${local.project_id}/instances/${google_spanner_instance.shared.name}"

  # The names each member's grants are bounded to.
  own_databases = { for m, v in var.database_admins : m => "${local.instance_path}/databases/${local.prefix}-${v.environment}-gbl-${v.application}-" }
  own_backups   = { for m, v in var.database_admins : m => "${local.instance_path}/backups/${local.prefix}-${v.environment}-gbl-${v.application}-" }
}

resource "google_spanner_instance_iam_member" "database_creator" {
  for_each = var.database_admins

  project  = local.project_id
  instance = google_spanner_instance.shared.name
  role     = local.org.spanner_database_creator_role
  member   = each.key
}

resource "google_spanner_instance_iam_member" "database_admin" {
  for_each = var.database_admins

  project  = local.project_id
  instance = google_spanner_instance.shared.name
  role     = "roles/spanner.databaseAdmin"
  member   = each.key

  condition {
    title       = "${each.value.application} ${each.value.environment} database"
    description = "The application's own database in this environment, with the schedules and operations under it."
    expression  = "resource.name.startsWith(\"${local.own_databases[each.key]}\")"
  }
}

# The backup schedules a production stack makes on its database are read and
# changed with spanner.backupSchedules.*, which databaseAdmin does not carry:
# the first tag build that planned a production stack as its apply identity
# was refused the schedule's read. backupAdmin carries them, and the backups,
# which are named after the database they are taken from.
resource "google_spanner_instance_iam_member" "backup_admin" {
  for_each = var.database_admins

  project  = local.project_id
  instance = google_spanner_instance.shared.name
  role     = "roles/spanner.backupAdmin"
  member   = each.key

  condition {
    title       = "${each.value.application} ${each.value.environment} backups"
    description = "The application's own database in this environment and the backups taken from it."
    expression  = "resource.name.startsWith(\"${local.own_databases[each.key]}\") || resource.name.startsWith(\"${local.own_backups[each.key]}\")"
  }
}

# A restore from production's backup creates the environment's database afresh
# from a backup of production's, on this instance, as the environment's apply
# identity: spanner.backups.restoreDatabase on the backup, which neither
# databaseAdmin nor backupAdmin carries and restoreAdmin adds alone, bounded
# to production's backups of the same application (the other permissions
# restoreAdmin carries, the roles above already hold). Production's own
# identity holds no restore right: no run restores production.
resource "google_spanner_instance_iam_member" "restore_admin" {
  for_each = { for m, v in var.database_admins : m => v if v.restore_from != "" }

  project  = local.project_id
  instance = google_spanner_instance.shared.name
  role     = "roles/spanner.restoreAdmin"
  member   = each.key

  condition {
    title       = "${each.value.application} ${each.value.environment} restores from ${each.value.restore_from}"
    description = "Production's backups of the application, which this environment's database is restored from."
    expression  = "resource.name.startsWith(\"${local.instance_path}/backups/${local.prefix}-${each.value.restore_from}-gbl-${each.value.application}-\")"
  }
}

# The plan identities read the databases, their IAM policies and their backup
# schedules when a pull-request build plans the environment's stack, and write
# nothing: the organization's spannerPlanReader role (1-org).
resource "google_spanner_instance_iam_member" "plan_reader" {
  for_each = toset(var.database_planners)

  project  = local.project_id
  instance = google_spanner_instance.shared.name
  role     = local.org.spanner_plan_reader_role
  member   = each.value
}
