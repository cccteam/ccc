# ---------------------------------------------------------------------------
# Secret containers
#
# One per secret the code declares (local.secrets, from pkg/config/data.go),
# named imp-<env>-gbl-beacon-<kebab name>. Containers only: the application
# apply identity holds secretContainerAdmin, which can create a secret and set
# its policy but neither add nor read a version. An operator adds the value
# as a version (Secret Version Adder on the environment's secrets), and
# var.secret_versions pins which version the environment runs. Automatic
# replication: nothing here has a residency requirement.
#
# Modeled on CCC's reference deployment, one resource per
# secret there, one for_each here.
# ---------------------------------------------------------------------------

resource "google_secret_manager_secret" "beacon" {
  # A pull-request stack reads tst's containers and creates none.
  for_each = local.is_pr ? {} : local.secrets

  project   = local.project_id
  secret_id = "${local.name}-gbl-${local.app}-${each.value.name}"

  replication {
    auto {}
  }

  labels = merge(local.labels, {
    # The variable the secret feeds, so a value can be traced back to the field.
    variable = lower(each.key)
  })
}

# ---------------------------------------------------------------------------
# Adoption
#
# A container an operator created ahead of the release that first reads it
# (bedrock secret add, or the console with the name above) is adopted, not
# fought over: every declared container the project holds and this state
# does not is imported at plan time, so the apply reconciles it (its labels)
# instead of failing to create it. The import is idempotent: a container the
# state already holds imports nothing. A pull-request stack creates no
# containers and adopts none.
# ---------------------------------------------------------------------------

data "google_secret_manager_secrets" "existing" {
  count = local.is_pr ? 0 : 1

  project = local.project_id
}

locals {
  existing_containers = local.is_pr ? [] : [for s in data.google_secret_manager_secrets.existing[0].secrets : s.secret_id]
  adopted_containers  = { for key, id in local.secret_ids : key => id if contains(local.existing_containers, id) }
}

import {
  for_each = local.adopted_containers

  to = google_secret_manager_secret.beacon[each.key]
  id = "projects/${local.project_id}/secrets/${each.value}"
}

# Accessor for the site, which is the only process that reads a secret value
# (see the readers note in locals.tf). The deploy identity never holds this:
# a runtime secret is not a build-time one.
resource "google_secret_manager_secret_iam_member" "app_accessor" {
  for_each = local.secrets

  project   = local.project_id
  secret_id = local.secret_ids[each.key]
  role      = "roles/secretmanager.secretAccessor"
  member    = local.app_member

  depends_on = [google_secret_manager_secret.beacon, google_service_account.app]
}

# ---------------------------------------------------------------------------
# Build-time secret containers
#
# One per name var.build_secrets declares for this environment, named like
# the runtime containers and adopted the same way when an operator created
# them first (bedrock secret add). The pinned version is the map's value. The
# deploy identity, and no runtime identity, holds accessor on them: the
# pipeline reads each as the deploy identity and passes it to the image build
# as a BuildKit secret (README, "Build secrets"). A name the code declares as
# a runtime secret is refused: the container would be the same one.
# ---------------------------------------------------------------------------

resource "google_secret_manager_secret" "build" {
  for_each = local.is_pr ? {} : local.build_secrets

  project   = local.project_id
  secret_id = local.build_secret_ids[each.key]

  replication {
    auto {}
  }

  labels = merge(local.labels, {
    variable = lower(each.key)
  })

  lifecycle {
    precondition {
      condition     = !contains(keys(local.secrets), each.key)
      error_message = "var.build_secrets names ${each.key}, which the code declares as a runtime secret; a build-time secret is a container of its own, so give it another name."
    }
  }
}

locals {
  adopted_build_containers = { for name, id in local.build_secret_ids : name => id if contains(local.existing_containers, id) }
}

import {
  for_each = local.is_pr ? {} : local.adopted_build_containers

  to = google_secret_manager_secret.build[each.key]
  id = "projects/${local.project_id}/secrets/${each.value}"
}

resource "google_secret_manager_secret_iam_member" "deploy_build_accessor" {
  for_each = local.is_pr ? {} : local.build_secrets

  project   = local.project_id
  secret_id = local.build_secret_ids[each.key]
  role      = "roles/secretmanager.secretAccessor"
  member    = local.identities.deploy_identity_member

  depends_on = [google_secret_manager_secret.build]
}
