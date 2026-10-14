# ---------------------------------------------------------------------------
# Secret containers
#
# One per secret the code declares (local.secrets, from pkg/config/data.go),
# named imp-<env>-gbl-harbor-<kebab name>. Containers only: the application
# apply identity holds secretContainerAdmin, which can create a secret and set
# its policy but neither add nor read a version. Someone adds the value as a
# version (Secret Version Adder on the environment's secrets) and
# var.secret_versions pins which version the environment runs; a generated
# secret's versions are the pipeline's (below). Automatic replication: nothing
# here has a residency requirement.
#
# Modeled on CCC's reference deployment, one resource per
# secret there, one for_each here.
# ---------------------------------------------------------------------------

resource "google_secret_manager_secret" "harbor" {
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

  to = google_secret_manager_secret.harbor[each.key]
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

  depends_on = [google_secret_manager_secret.harbor, google_service_account.app]
}

# Accessor for the job process too, on the secrets at the levels it constructs
# (local.jobs_secrets): it runs the application's own code, and what that code
# reads the derivation cannot know.
resource "google_secret_manager_secret_iam_member" "jobs_accessor" {
  for_each = local.jobs_secrets

  project   = local.project_id
  secret_id = local.secret_ids[each.key]
  role      = "roles/secretmanager.secretAccessor"
  member    = local.jobs_member

  depends_on = [google_secret_manager_secret.harbor, google_service_account.jobs]
}

# ---------------------------------------------------------------------------
# Generated secrets
#
# A secret the code declares secret:"generated" (local.generated_secrets) has
# a value nobody types: 32 bytes of cryptographically secure random data,
# base64. The pipeline's plan step, as the apply identity, adds version 1 when
# the container is empty, creating the container ahead of this apply (adopted
# above, with this grant already on it) and deriving the pin from the newest
# enabled version at every build (var.generated_secret_versions); a rotation
# (bedrock secret rotate, the operations workflow) adds the next version as the
# operations identity and runs the live release again. Both identities hold
# Secret Version Adder on these containers alone: adding, never reading. A
# pull-request stack reads tst's and grants nothing.
# ---------------------------------------------------------------------------

locals {
  generated_adders = {
    for pair in setproduct(keys(local.generated_secrets), ["apply", "operations"]) :
    "${pair[0]}/${pair[1]}" => {
      key    = pair[0]
      member = pair[1] == "apply" ? local.identities.apply_identity_member : local.identities.operations_identity_member
    }
  }
}

resource "google_secret_manager_secret_iam_member" "generated_adder" {
  for_each = local.is_pr ? {} : local.generated_adders

  project   = local.project_id
  secret_id = local.secret_ids[each.value.key]
  role      = "roles/secretmanager.secretVersionAdder"
  member    = each.value.member

  depends_on = [google_secret_manager_secret.harbor]
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
