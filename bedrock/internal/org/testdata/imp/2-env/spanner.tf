# ---------------------------------------------------------------------------
# The tst Spanner instance
#
# By default (ruled 2026-09-26) tst holds its own instance so pull-request
# databases and test data never share an instance with stg or prd, which
# share the instance 2-spn creates in the spn project; and the tst instance
# is regional, in the primary region, on the Standard edition: tst has no
# failover to exercise and its databases come and go, and a multi-region
# instance must be Enterprise Plus edition by Google's rule, about $225 a
# month at 100 processing units (2026-09-25 catalog) against $66 for regional
# Standard. The production topology (nam10, Enterprise Plus) is exercised on
# the 2-spn instance. var.spanner_instances overrides any of it per
# environment: tst on the shared instance, or an own instance on nam10
# (recreated: a configuration cannot change in place). The smallest size, no
# autoscaler, no default backup schedule (a database that wants backups
# declares its own schedule, as harbor's stack does for prd).
# ---------------------------------------------------------------------------

locals {
  spanner          = lookup(var.spanner_instances, var.environment, { placement = "shared", config = null, edition = null, processing_units = 100 })
  own_instance     = local.spanner.placement == "own"
  spanner_config   = coalesce(local.spanner.config, "regional-${local.region}")
  spanner_edition  = coalesce(local.spanner.edition, startswith(local.spanner_config, "regional-") ? "STANDARD" : "ENTERPRISE_PLUS")
  processing_units = local.spanner.processing_units
}

# The resource keeps its name "tst" from the first shape (tst was the only
# environment with an instance of its own); the placement variable decides.
resource "google_spanner_instance" "tst" {
  count = local.own_instance ? 1 : 0

  project      = local.project_id
  name         = "${local.name}-gbl-spanner"
  display_name = "${local.name}-gbl-spanner"
  config       = local.spanner_config

  processing_units             = local.processing_units
  edition                      = local.spanner_edition
  default_backup_schedule_type = "NONE"

  # An instance holding databases must be emptied on purpose, not by a plan.
  force_destroy = false

  labels = local.labels

  lifecycle {
    precondition {
      condition     = startswith(local.spanner_config, "regional-") || local.spanner_edition == "ENTERPRISE_PLUS"
      error_message = "A multi-region configuration (${local.spanner_config}) needs the ENTERPRISE_PLUS edition; leave spanner_edition unset."
    }
  }
}
