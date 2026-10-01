# ---------------------------------------------------------------------------
# Per-application identities
#
# Two service accounts per application, in the environment project, mirroring
# 1-org's layer/plan pair one level down:
#
#   imp-<env>-gbl-<app>-tofu    applies the application's stack (state slot 3-app/<app>) for this environment
#   imp-<env>-gbl-<app>-deploy  runs every build and deploy of the application
#
# Neither has keys (org policy forbids them); both run from Cloud Build. The
# design brief's "Who may do what" table is the source of each role set, and
# every set is a starting hypothesis to tighten by reading denials.
# ---------------------------------------------------------------------------

resource "google_service_account" "apply" {
  for_each = local.apps

  project      = local.project_id
  account_id   = "${local.name}-gbl-${each.key}-tofu"
  display_name = "OpenTofu SA - ${local.name}-gbl-${each.key}"
  description  = "Application apply identity for ${each.key} in ${var.environment}. Applies 3-app/${each.key} from Cloud Build."
}

resource "google_service_account" "deploy" {
  for_each = local.apps

  project      = local.project_id
  account_id   = "${local.name}-gbl-${each.key}-deploy"
  display_name = "Deploy SA - ${local.name}-gbl-${each.key}"
  description  = "Deploy identity for ${each.key} in ${var.environment}. Runs the application's Cloud Build triggers; never writes infrastructure."
}

# ---------------------------------------------------------------------------
# Application apply identity
# ---------------------------------------------------------------------------

resource "google_project_iam_member" "apply" {
  # checkov:skip=CKV_GCP_49: automation identity with a named role set on one shared project, no keys, runs only from Cloud Build.
  for_each = local.apply_grants

  project = local.project_id
  role    = each.value.role
  member  = google_service_account.apply[each.value.app].member
}

# Database admin on the environment's instance rather than on the instance
# project: enough to create the application's database and set its IAM, and
# nothing on the instance itself. Only when the instance is this layer's
# (var.spanner_instances, placement own); for an environment on the shared
# instance the same grant is 2-spn's, from its database_admins variable,
# which takes this layer's applications[*].apply_identity_member.
resource "google_spanner_instance_iam_member" "apply_database_admin" {
  for_each = { for app in var.applications : app => app if local.own_instance }

  project  = local.instance_project
  instance = local.instance_name
  role     = "roles/spanner.databaseAdmin"
  member   = google_service_account.apply[each.key].member
}

# Only when 2-net publishes a shared VPC: a stack that attaches a Cloud Run
# service to it needs the apply identity to use the network. This organization runs
# without one (no connector, no NAT), so this is normally empty.
resource "google_project_iam_member" "apply_network_user" {
  for_each = { for app in var.applications : app => app if local.shared_vpc != null }

  project = local.project_id
  role    = "roles/compute.networkUser"
  member  = google_service_account.apply[each.key].member
}

# ---------------------------------------------------------------------------
# Application deploy identity
# ---------------------------------------------------------------------------

resource "google_project_iam_member" "deploy" {
  for_each = local.deploy_grants

  project = local.project_id
  role    = each.value.role
  member  = google_service_account.deploy[each.value.app].member
}

# Writer on the application's own image repository in shr is 2-shr's grant:
# its pushers variable lists this identity's member (output applications),
# added after this layer has created it. The warning below fires while 2-shr
# does not know the application at all.
check "repositories_registered" {
  assert {
    condition     = alltrue([for app in var.applications : contains(keys(local.repositories), app)])
    error_message = "An application has no image repository in 2-shr's repository_names output yet. Add it to 2-shr's applications, and its deploy identity member to 2-shr's pushers."
  }
}

# ---------------------------------------------------------------------------
# The application's slot in the state bucket
#
# The apply identity reads and writes its own state under
# 3-app/<app>/<env>/ (the pull-request stacks sit below it, at
# 3-app/<app>/<env>/pr<N>/) and reads the upstream states its stack takes
# outputs from (local.upstream_state_prefixes). Both are conditions on the
# object name. Listing cannot be conditioned (a list is a request on the
# bucket, not on an object), so it is granted on its own, unconditionally,
# through legacyBucketReader: the identity can name every object in the
# bucket and open only its own. The bucket is the boot project's; this layer
# grants on it because this layer creates the identity, and when the runner
# is wired the environment layer identity needs bucket IAM authority on the
# state bucket for it (0-bootstrap README, "Not yet wired").
# ---------------------------------------------------------------------------

resource "google_storage_bucket_iam_member" "apply_state_list" {
  for_each = local.apps

  bucket = var.state_bucket
  role   = "roles/storage.legacyBucketReader"
  member = google_service_account.apply[each.key].member
}

resource "google_storage_bucket_iam_member" "apply_state_slot" {
  for_each = local.apps

  bucket = var.state_bucket
  role   = "roles/storage.objectUser"
  member = google_service_account.apply[each.key].member

  condition {
    title       = "${local.name}-${each.key}-state-slot"
    description = "The application's own state prefix in ${var.environment}, pull-request stacks included."
    expression  = "resource.name.startsWith(\"projects/_/buckets/${var.state_bucket}/objects/3-app/${each.key}/${var.environment}/\")"
  }
}

resource "google_storage_bucket_iam_member" "apply_state_upstream" {
  for_each = local.apps

  bucket = var.state_bucket
  role   = "roles/storage.objectViewer"
  member = google_service_account.apply[each.key].member

  condition {
    title       = "${local.name}-${each.key}-upstream-state"
    description = "The upstream states the application stack reads outputs from."
    expression  = join(" || ", [for p in local.upstream_state_prefixes : "resource.name.startsWith(\"projects/_/buckets/${var.state_bucket}/objects/${p}/\")"])
  }
}

# Create and read on the deployment records. The deploy writes its record once
# and reads its own environment's records: the newest record of a pull request,
# for the stale-database check of a pull-request build that migrates, and the
# environment's newest live record, where its live version is. The bucket's own
# grants are these two, neither of which overwrites or deletes a record, and the
# bucket's versioning keeps the history. roles/cloudbuild.builds.builder, granted
# on the project (locals.tf), reaches every bucket in the project too, these
# records included: the records are write-once only once that role gives way to
# what a build needs.
resource "google_storage_bucket_iam_member" "deploy_records" {
  for_each = local.apps

  bucket = google_storage_bucket.records.name
  role   = "roles/storage.objectCreator"
  member = google_service_account.deploy[each.key].member
}

resource "google_storage_bucket_iam_member" "deploy_records_viewer" {
  for_each = local.apps

  bucket = google_storage_bucket.records.name
  role   = "roles/storage.objectViewer"
  member = google_service_account.deploy[each.key].member
}

# In tst only: the deploy identity may act as the apply identity, because the
# pull-request build applies the application's pull-request stack (its own
# state prefix, short names) before it deploys into it, and destroys it on
# /gcbrun down or when the pull request closes. Nowhere else does a deploy
# touch infrastructure.
resource "google_service_account_iam_member" "deploy_impersonates_apply" {
  for_each = local.is_tst ? local.apps : toset([])

  service_account_id = google_service_account.apply[each.key].name
  role               = "roles/iam.serviceAccountTokenCreator"
  member             = google_service_account.deploy[each.key].member
}

# In tst only: the deploy identity may act as itself. Cloud Scheduler runs the
# application's sweep trigger (tst only) as the deploy identity, and running a
# trigger whose builds run as a service account needs iam.serviceAccounts.actAs
# on that account, which no builder role carries. Without it every scheduled
# sweep was refused (PERMISSION_DENIED, from the first one on 2026-09-27).
resource "google_service_account_iam_member" "deploy_runs_sweep" {
  for_each = local.is_tst ? local.apps : toset([])

  service_account_id = google_service_account.deploy[each.key].name
  role               = "roles/iam.serviceAccountUser"
  member             = google_service_account.deploy[each.key].member
}

# Reader on this environment's deployment records for the next environment's
# deploy identities: that environment's pipeline admits a release only after a
# live record of it exists here (the record gate).
resource "google_storage_bucket_iam_member" "next_deploy_records_viewer" {
  for_each = local.next_deploy_members

  bucket = google_storage_bucket.records.name
  role   = "roles/storage.objectViewer"
  member = each.value
}

# Accessor on the named build-time secrets only (var.build_time_secrets),
# never on a runtime secret: those are read by the runtime identities, which
# the application stack grants. Empty by default.
resource "google_secret_manager_secret_iam_member" "deploy_build_secrets" {
  for_each = local.build_secret_grants

  project   = local.project_id
  secret_id = each.value.secret
  role      = "roles/secretmanager.secretAccessor"
  member    = google_service_account.deploy[each.value.app].member
}

# tst only: the deploy identity creates and drops pull-request databases on
# the tst instance. In stg and prd it holds nothing on Spanner. When tst is
# placed on the shared instance (var.spanner_instances), this grant cannot be
# made here: it goes into 2-spn's database_admins as a deliberate, visible
# choice, since it reaches every database on that instance.
resource "google_spanner_instance_iam_member" "deploy_database_admin" {
  for_each = { for app in var.applications : app => app if local.is_tst && local.own_instance }

  project  = local.instance_project
  instance = local.instance_name
  role     = "roles/spanner.databaseAdmin"
  member   = google_service_account.deploy[each.key].member
}

# Service Account User on the application's runtime identities is granted in
# the application's stack, where those identities are created: the deploy identity may act
# as this application's services and jobs and no other's.

# The application apply identity binds the public-invoker tag (1-org tags.tf)
# to its own Cloud Run services, which is what lets it grant allUsers
# roles/run.invoker there under the environment folder's domain policy.
resource "google_tags_tag_value_iam_member" "apply_public_invoker" {
  for_each = local.apps

  tag_value = local.org.public_invoker_tag_value
  role      = "roles/resourcemanager.tagUser"
  member    = google_service_account.apply[each.key].member
}

# ---------------------------------------------------------------------------
# Secret operators
#
# The people (a group, normally) who own the secret values: they create a
# container ahead of the release that first reads it (bedrock secret add)
# and add versions to it, on this environment's project, and never read one.
# The application stack adopts a container that exists at its next apply.
# ---------------------------------------------------------------------------

resource "google_project_iam_member" "secret_operator" {
  for_each = toset(var.secret_operators)

  project = local.project_id
  role    = local.org.secret_operator_role
  member  = each.value
}
