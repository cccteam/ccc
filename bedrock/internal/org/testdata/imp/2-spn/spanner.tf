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

# Instance-level grants. spanner.databases.create is checked on the instance,
# so each identity that creates databases holds databaseAdmin here. That role
# at instance level reaches every database on the instance, other
# applications' included; bounding it to an application's own databases by
# name is one of the open IAM questions, and until it is
# answered the list stays short and the members stay apply identities that
# run only after approval.
resource "google_spanner_instance_iam_member" "database_admin" {
  for_each = toset(var.database_admins)

  project  = local.project_id
  instance = google_spanner_instance.shared.name
  role     = "roles/spanner.databaseAdmin"
  member   = each.value
}

# The backup schedules a production stack makes on its database are read and
# changed with spanner.backupSchedules.*, which databaseAdmin does not carry:
# the first tag build that planned a production stack as its apply identity
# was refused the schedule's read. backupAdmin carries them, and the backups.
resource "google_spanner_instance_iam_member" "backup_admin" {
  for_each = toset(var.database_admins)

  project  = local.project_id
  instance = google_spanner_instance.shared.name
  role     = "roles/spanner.backupAdmin"
  member   = each.value
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
