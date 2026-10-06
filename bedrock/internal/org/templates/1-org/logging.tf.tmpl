# ---------------------------------------------------------------------------
# Central audit logging: off by default, see var.central_logging
#
# The GCP counterpart to the org-wide CloudTrail in tf-aws-setup. With the
# flag on, every folder exports its Cloud Audit Logs to a single bucket in the
# log project, which becomes the system of record. Encrypted with
# Google-managed keys rather than a CMEK, since the bucket holds no data more
# sensitive than what Cloud Audit Logs already retains.
#
# Sinks are folder-scoped with include_children rather than org-scoped, so the
# terraform folder and the boot project keep their own retention posture.
#
# Every resource here is gated on the flag. With it off nothing below exists:
# no log project, no bucket, no sinks. Turning it on also needs
# roles/logging.configWriter and roles/storage.admin on the org layer identity
# (org_layer_roles in 0-bootstrap); the README has the full list.
# ---------------------------------------------------------------------------

locals {
  log_project_id = var.central_logging ? module.project["log"].project_id : null
  audit_log_name = "${var.prefix}-log-${local.region_code}-audit-logs"
}

# Bucket names are globally unique, so the log project's own suffix is reused
# here rather than generating a second one.
resource "google_storage_bucket" "audit_logs" {
  count = var.central_logging ? 1 : 0

  project  = local.log_project_id
  name     = "${local.audit_log_name}-${element(split("-", local.log_project_id), length(split("-", local.log_project_id)) - 1)}"
  location = upper(var.gcp_region)
  labels   = local.labels

  uniform_bucket_level_access = true
  public_access_prevention    = "enforced"
  force_destroy               = false

  versioning {
    enabled = true
  }

  # Enforced by the bucket itself, not just by lifecycle rules, so a compromised
  # writer cannot shorten retention or delete history.
  retention_policy {
    retention_period = var.audit_log_retention_days * 24 * 60 * 60
  }

  lifecycle_rule {
    condition {
      age = var.audit_log_retention_days
    }
    action {
      type = "Delete"
    }
  }

  depends_on = [time_sleep.apis_ready]
}

resource "google_logging_folder_sink" "audit_logs" {
  for_each = { for k, v in local.folders : k => v if var.central_logging }

  name             = "${var.prefix}-${each.key}-gbl-audit-logs"
  folder           = google_folder.this[each.key].folder_id
  include_children = true
  destination      = "storage.googleapis.com/${google_storage_bucket.audit_logs[0].name}"

  # Admin Activity and System Event logs are always on and cannot be disabled.
  # Data Access logs are included wherever a project has opted in.
  filter = "logName:\"logs/cloudaudit.googleapis.com\""
}

resource "google_storage_bucket_iam_member" "audit_logs_writer" {
  for_each = { for k, v in local.folders : k => v if var.central_logging }

  bucket = google_storage_bucket.audit_logs[0].name
  role   = "roles/storage.objectCreator"
  member = google_logging_folder_sink.audit_logs[each.key].writer_identity
}
