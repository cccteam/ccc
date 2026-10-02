# ---------------------------------------------------------------------------
# Per-application identities
#
# Three service accounts per application, in the environment project, mirroring
# 1-org's layer/plan pair one level down:
#
#   imp-<env>-gbl-<app>-tofu    applies the application's stack (state slot 3-app/<app>) for this environment
#   imp-<env>-gbl-<app>-deploy  runs every build and deploy of the application
#   imp-<env>-gbl-<app>-plan    plans the application's stack for this environment on a pull request; reads, never writes
#
# None has keys (org policy forbids them); all run from Cloud Build. The
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

resource "google_service_account" "plan" {
  for_each = local.apps

  project      = local.project_id
  account_id   = "${local.name}-gbl-${each.key}-plan"
  display_name = "Plan SA - ${local.name}-gbl-${each.key}"
  description  = "Plan identity for ${each.key} in ${var.environment}. Plans 3-app/${each.key} for this environment on a pull request, from tst's Cloud Build; reads, never writes."
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

# The backup schedules a production stack makes on its database are read and
# changed with spanner.backupSchedules.*, which databaseAdmin does not carry
# (2-spn grants the same on the shared instance).
resource "google_spanner_instance_iam_member" "apply_backup_admin" {
  for_each = { for app in var.applications : app => app if local.own_instance }

  project  = local.instance_project
  instance = local.instance_name
  role     = "roles/spanner.backupAdmin"
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
# and reads its own environment's records: today the newest record of a pull
# request, for the stale-database check of a pull-request build that migrates;
# the environment's newest live record, where its live version is, once a step
# needs it. The bucket's own
# grants are these two, neither of which overwrites or deletes a record, and the
# bucket's versioning keeps the history. None of the deploy identity's project
# roles (locals.tf) reaches a bucket since roles/cloudbuild.builds.builder gave
# way to cloudBuildBuildReader; what still reaches the records is the apply
# identity, which the deploy identity may act as and whose roles/storage.admin
# on the project reaches every bucket in it.
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

# Read on the deployment records for the application plan identity: a pull-request
# build against a hotfix line previews what each environment's release check will say
# to the line's next release, reading the environment's live record as that
# environment's plan identity, the identity the build already plans the environment as.
resource "google_storage_bucket_iam_member" "plan_records" {
  for_each = local.apps

  bucket = google_storage_bucket.records.name
  role   = "roles/storage.objectViewer"
  member = google_service_account.plan[each.key].member
}

# The deploy identity may act as the apply identity: a release's tag build
# applies the environment's application stack (plan, tests, apply, as the
# apply identity) after the image build and before the migrations, and in tst
# the pull-request build applies the application's pull-request stack (its own
# state prefix, short names) before it deploys into it, and destroys it on
# /gcbrun down or when the pull request closes. The deploy identity itself
# never applies: everything that touches infrastructure runs as the apply
# identity.
resource "google_service_account_iam_member" "deploy_impersonates_apply" {
  for_each = local.apps

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

# ---------------------------------------------------------------------------
# Application plan identity
#
# A pull-request build plans the application's stack for every environment, so
# the plan a reviewer approves is the plan of each environment, and it does so
# as that environment's plan identity: a reader. On the project it holds the
# custom role applicationPlanReader (the reads of the resource types the stack
# declares, and nothing of their data; locals.tf) beside roles/iam.securityReviewer
# for the IAM policies the stack's grants are refreshed through; on the state
# bucket, the list and a read of
# the application's own state prefix and of the upstream states, the same
# prefixes the apply identity reads, and no write (the plan runs without the
# state lock). tst's deploy identity of the same application may impersonate
# it, since tst's Cloud Build runs the pull-request builds; nothing else may.
# ---------------------------------------------------------------------------

resource "google_project_iam_member" "plan" {
  for_each = local.plan_grants

  project = local.project_id
  role    = each.value.role
  member  = google_service_account.plan[each.value.app].member
}

# On the environment's own instance, the plan identity reads the application's
# database, its grants and its backup schedules with the organization's
# spannerPlanReader role; on the shared instance the same grant is 2-spn's,
# from its database_planners.
resource "google_spanner_instance_iam_member" "plan_spanner_reader" {
  for_each = { for app in var.applications : app => app if local.own_instance }

  project  = local.instance_project
  instance = local.instance_name
  role     = local.org.spanner_plan_reader_role
  member   = google_service_account.plan[each.key].member
}

resource "google_storage_bucket_iam_member" "plan_state_list" {
  for_each = local.apps

  bucket = var.state_bucket
  role   = "roles/storage.legacyBucketReader"
  member = google_service_account.plan[each.key].member
}

resource "google_storage_bucket_iam_member" "plan_state_read" {
  for_each = local.apps

  bucket = var.state_bucket
  role   = "roles/storage.objectViewer"
  member = google_service_account.plan[each.key].member

  condition {
    title       = "${local.name}-${each.key}-plan-state"
    description = "The application's own state prefix in ${var.environment} and the upstream states its stack reads."
    expression  = join(" || ", concat(["resource.name.startsWith(\"projects/_/buckets/${var.state_bucket}/objects/3-app/${each.key}/${var.environment}/\")"], [for p in local.upstream_state_prefixes : "resource.name.startsWith(\"projects/_/buckets/${var.state_bucket}/objects/${p}/\")"]))
  }
}

resource "google_service_account_iam_member" "tst_deploy_impersonates_plan" {
  for_each = local.tst_deploy_members

  service_account_id = google_service_account.plan[each.key].name
  role               = "roles/iam.serviceAccountTokenCreator"
  member             = each.value
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
