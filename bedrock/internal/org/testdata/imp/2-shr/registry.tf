# ---------------------------------------------------------------------------
# Container images
#
# One Docker repository per application, all in the shared project: every
# environment's build of a commit pushes its own image there and reads the
# layer cache the builds before it left (no image is promoted from one
# environment to the next; each builds its own). Per application rather than one
# for the organization (the tf-gcp-setup shape) because a deploy identity's
# writer grant is then bounded to its own images; nothing an application's
# pipeline can do reaches another application's repository. Google-managed
# encryption, as in the model. Vulnerability scanning is left at the project
# default rather than pinned here.
# ---------------------------------------------------------------------------

resource "google_artifact_registry_repository" "app" {
  for_each = toset(var.applications)

  project       = local.project_id
  location      = local.region
  repository_id = "${local.name_prefix}-${each.key}"
  description   = "Container images for ${each.key}, all environments"
  format        = "DOCKER"
  labels        = local.labels

  # A tag names one digest forever. A release is a tag, so what stg approved
  # is exactly what prd runs; a rebuild gets a new tag.
  docker_config {
    immutable_tags = true
  }

  # Cleanup: DELETE policies select versions, KEEP policies exempt them, and a
  # version no DELETE policy selects is kept. By default only untagged
  # versions are ever deleted; the KEEP policy is the floor that stays in
  # force if a tagged retention is switched on later.
  cleanup_policy_dry_run = var.cleanup_dry_run

  cleanup_policies {
    id     = "delete-untagged"
    action = "DELETE"
    condition {
      tag_state  = "UNTAGGED"
      older_than = "${var.untagged_retention_days * 24 * 60 * 60}s"
    }
  }

  cleanup_policies {
    id     = "keep-newest"
    action = "KEEP"
    most_recent_versions {
      keep_count = var.keep_tagged_versions
    }
  }

  dynamic "cleanup_policies" {
    for_each = var.tagged_retention_days == null ? [] : [var.tagged_retention_days]
    content {
      id     = "delete-tagged"
      action = "DELETE"
      condition {
        tag_state  = "TAGGED"
        older_than = "${cleanup_policies.value * 24 * 60 * 60}s"
      }
    }
  }
}

# Push. An application's deploy identity in each environment project, as listed
# in var.pushers, gets writer on that application's repository and nothing
# else here. Writer rather than admin: a pipeline pushes images; it never
# deletes them or changes the repository.
resource "google_artifact_registry_repository_iam_member" "writer" {
  for_each = local.writer_grants

  project    = local.project_id
  location   = google_artifact_registry_repository.app[each.value.app].location
  repository = google_artifact_registry_repository.app[each.value.app].name
  role       = "roles/artifactregistry.writer"
  member     = each.value.member
}

# Pull. Cloud Run in each environment project, through its service agent, on
# every repository: an environment runs whichever applications are deployed
# to it, and a reader grant per application per environment costs nothing.
resource "google_artifact_registry_repository_iam_member" "reader" {
  for_each = local.reader_grants

  project    = local.project_id
  location   = google_artifact_registry_repository.app[each.value.app].location
  repository = google_artifact_registry_repository.app[each.value.app].name
  role       = "roles/artifactregistry.reader"
  member     = each.value.member
}

# Pull, by the application's apply identity in each environment project
# (var.pullers): Cloud Run checks that the principal creating a revision can
# read the revision's image, and a revision that comes from a placement
# change is the apply identity's, carrying the image the last deploy left.
# Found by the organization's first shared-database switch (2026-09-27).
resource "google_artifact_registry_repository_iam_member" "puller" {
  for_each = local.puller_grants

  project    = local.project_id
  location   = google_artifact_registry_repository.app[each.value.app].location
  repository = google_artifact_registry_repository.app[each.value.app].name
  role       = "roles/artifactregistry.reader"
  member     = each.value.member
}
