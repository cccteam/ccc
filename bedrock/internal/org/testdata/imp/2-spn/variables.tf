variable "database_admins" {
  description = <<-EOT
    The apply identities that hold databases on the instance, each with the
    environment and application its grants are bounded to: the application
    apply identity of each application in stg and prd, which the environment
    layers create. Every member holds the organization's spannerDatabaseCreator
    role on the instance (creating a database and listing what the instance
    holds are checked on the instance) and roles/spanner.databaseAdmin and
    roles/spanner.backupAdmin under a condition naming its own database and
    backups, "<prefix>-<environment>-gbl-<application>-"; a member with
    restore_from set (every environment but production) also holds
    roles/spanner.restoreAdmin on that environment's backups of the same
    application, for the restore of its database from production's backup.
    Every member also holds roles/resourcemanager.projectIamAdmin on the
    project under a condition admitting changes to the grants of
    roles/monitoring.metricWriter alone, which its stack grants the
    application's runtime identities for the Spanner client's metrics.
    A member has to exist before it can be bound, so an application's
    identities are added here after the environment layers have run for it.

    Everything below the instance is the application layer's: the database
    user grants for runtime identities and the deploy identity's admin grant, for
    the migrations it runs, are made on each database by the layer that creates it.

    Example:
      {
        "serviceAccount:imp-stg-gbl-harbor-tofu@imp-stg-gbl-core-a1b2.iam.gserviceaccount.com" = { environment = "stg", application = "harbor", restore_from = "prd" },
        "serviceAccount:imp-prd-gbl-harbor-tofu@imp-prd-gbl-core-a1b2.iam.gserviceaccount.com" = { environment = "prd", application = "harbor" },
      }
  EOT
  type = map(object({
    environment  = string
    application  = string
    restore_from = optional(string, "")
  }))
  default = {}

  validation {
    condition     = alltrue([for m in keys(var.database_admins) : can(regex("^(serviceAccount|user|group|principal|principalSet):", m))])
    error_message = "Every database admin must be a full IAM member, such as serviceAccount:name@project.iam.gserviceaccount.com."
  }
}

variable "migration_readers" {
  description = <<-EOT
    The first environment's deploy identity of each application, once per
    environment on this instance whose database of that application it reads: a
    consolidation pull request's build runs the migration guard as it, which reads
    each environment's migration version through the migrations_reader database
    role the application's first migration creates (select on the SchemaMigrations
    table alone). Each entry (keyed "<environment>/<application>") holds
    roles/spanner.fineGrainedAccessUser on the application's own database in that
    environment and roles/spanner.databaseRoleUser on that database's
    migrations_reader role, the two roles Spanner's fine-grained access control
    asks of a principal that uses a database role, and nothing on the data.
    Rendered into applications.auto.tfvars from placement.json, like the admins.
  EOT
  type = map(object({
    member      = string
    environment = string
    application = string
  }))
  default = {}

  validation {
    condition     = alltrue([for v in values(var.migration_readers) : can(regex("^(serviceAccount|user|group|principal|principalSet):", v.member))])
    error_message = "Every migration reader's member must be a full IAM member, such as serviceAccount:name@project.iam.gserviceaccount.com."
  }
}

variable "database_planners" {
  description = <<-EOT
    IAM members granted the organization's spannerPlanReader role on the
    instance and roles/iam.securityReviewer on the project: a pull-request
    build plans each environment's application stack as that environment's
    plan identity, which refreshes the stack's database, its grants, its
    backup schedules and its metric writer grants here and may write nothing. The
    application plan identity of each application in stg and prd, which the
    environment layers create, added after they have run for it, like
    database_admins.
  EOT
  type        = list(string)
  default     = []

  validation {
    condition     = alltrue([for m in var.database_planners : can(regex("^(serviceAccount|user|group|principal|principalSet):", m))])
    error_message = "Every database planner must be a full IAM member, such as serviceAccount:name@project.iam.gserviceaccount.com."
  }
}

variable "edition" {
  description = <<-EOT
    Spanner edition. Left unset, it follows the configuration: a regional
    configuration (regional-*) gets STANDARD and a multi-region configuration
    gets ENTERPRISE_PLUS, because Google admits multi-region instances to that
    edition only (the first apply of this layer, 2026-09-25, was refused with
    "Feature MULTI_REGION is not available ... in Edition STANDARD"). Set it
    only to run a regional instance on a higher edition. Catalog prices on
    2026-09-25 for 100 processing units: nam10 on ENTERPRISE_PLUS is about
    $225 a month, regional-us-central1 on STANDARD about $66.
  EOT
  type        = string
  default     = null
  nullable    = true

  validation {
    condition     = var.edition == null || contains(["STANDARD", "ENTERPRISE", "ENTERPRISE_PLUS"], var.edition)
    error_message = "edition must be unset, STANDARD, ENTERPRISE, or ENTERPRISE_PLUS."
  }
}

variable "processing_units" {
  description = "Compute capacity of the instance in processing units (100 = a tenth of a node), by Spanner's own rule: at least 100, the smallest a multi-region instance can be and the size an organization starts at, in steps of 100 below a node and in whole nodes (multiples of 1000) from a node on. Two things raise it, and the larger wins: the databases the applications declare (ten for every hundred units below a node, a hundred from a node on; bedrock org register and bedrock org check count them and refuse a size too small) and load (Google: high-priority CPU at or below 45 percent per region on a multi-region instance, 65 percent on a regional one, the 24-hour smoothed aggregate under 90 percent). A raise is this value in terraform.tfvars, read in the pull request's plan (README.md, Capacity)."
  type        = number
  default     = 100

  validation {
    condition     = var.processing_units >= 100 && (var.processing_units < 1000 ? var.processing_units % 100 == 0 : var.processing_units % 1000 == 0)
    error_message = "processing_units follows Spanner's rule: at least 100, a multiple of 100 below 1000, and a multiple of 1000 (whole nodes) from 1000 on."
  }
}

variable "spanner_config" {
  description = "Instance configuration. nam10 is the multi-region configuration whose read-write replicas are in us-central1 and us-west3, the organization's two regions, with a witness in us-central2."
  type        = string
  default     = "nam10"
}
