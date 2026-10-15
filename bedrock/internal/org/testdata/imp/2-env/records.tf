# ---------------------------------------------------------------------------
# Deployment records
#
# One bucket per environment where every deploy writes what it did: the
# release, the image digests, the migration outcome, the approval decision.
# Environments chain by these records rather than by one environment's
# identity holding build-create in the next, so the bucket is the seam
# between the tst deploy and the stg pipeline.
#
# The bucket's permission list is set whole (the policy below), so it holds
# exactly what this layer names and nothing a project role implies: the
# environment's deploy identities create and read objects (a record is
# written once and never rewritten, and versioning keeps the history if one
# ever is), its application plan identities read them (a pull-request build
# against a hotfix line previews each environment's release check from its
# live record), the next environment's deploy identities read them (the
# record gate), and the environment's team group reads them (a person reads
# a record through the group, never through a project role). The application
# stacks write what is theirs under <app>/stacks/ as the applications' apply
# identities (objectUser, under a condition on the prefix): the object naming
# the stack's id (<app>/stacks/<env>/stack.json, which the pipeline reads
# before it reads or writes a record), and in tst the pull-request stack jobs'
# outputs for the preview build, one object per pull request and commit,
# written again on a new /gcbrun comment. The deploy identities' creator
# binding stops at that prefix, so a preview build, which runs a file the
# pull request edits, can only read what the job wrote.
# Cloud Storage's default grants to the project's basic roles (projectOwner,
# projectEditor and projectViewer) are not listed, so they are removed, and a
# grant added on the bucket by hand is removed by the next apply; a grant
# added on the project is not. This layer's own identity reaches the bucket
# through its storage admin, which 1-org bounds to this bucket's name; the
# applications' apply identities, bounded to their own buckets
# (identities.tf), reach it under <app>/stacks/ alone.
#
# The name carries a random four-hex suffix because bucket names are global
# and permanent, as with the state bucket. US multi-region, since nothing
# reads it on a latency budget and gbl in the name should mean what it says.
# ---------------------------------------------------------------------------

resource "random_id" "records" {
  byte_length = 2
}

resource "google_storage_bucket" "records" {
  project  = local.project_id
  name     = "${local.name}-gbl-records-${random_id.records.hex}"
  location = "US"

  uniform_bucket_level_access = true
  public_access_prevention    = "enforced"

  versioning {
    enabled = true
  }

  labels = local.labels
}

# The bucket's bindings, one per role, with every member of each; a role
# nobody holds has no binding (an environment with no application registered
# has no deploy identity yet). The next environment's deploy identities are
# empty after the last environment and until the next environment's 2-env has
# been applied (locals.tf), which is why a registration applies this layer
# twice.
locals {
  records_deploy_members = [for app in var.applications : google_service_account.deploy[app].member]
  records_plan_members   = [for app in var.applications : google_service_account.plan[app].member]
  # The operations identities read the records too: a rollback asked for with
  # no release or backup named takes the live release's predecessor and the live
  # release's pre-release backup from them (operations.tf, the workflow). In
  # production, the lower environments' operations identities read them as
  # well: a restore from production's backup that names no release runs
  # production's live release, which the workflow reads from here as that
  # environment's operations identity. Their emails follow the model's naming
  # and 1-org's project ids; the identities exist once each 2-env has applied.
  records_ops_members = [for app in var.applications : google_service_account.operations[app].member]
  records_lower_ops_members = local.next_environment != "" ? [] : [
    for pair in setproduct([for env, next in var.next_environment : env if next != ""], var.applications) :
    "serviceAccount:${local.prefix}-${pair[0]}-gbl-${pair[1]}-ops@${local.org.project_ids[pair[0]]}.iam.gserviceaccount.com"
  ]
  # In production, the deploy identities of the environment before it (the one
  # on production's instance) read the records too: the staging rehearsal. A
  # release's build there reads production's live record to see whether the
  # release carries migrations production has not applied, and restores its
  # database from production's newest backup first when it does (bedrock deploy
  # resolve). Named the way the lower operations identities are.
  records_rehearsal_members = local.next_environment != "" || local.previous_environment == null ? [] : [
    for app in var.applications :
    "serviceAccount:${local.prefix}-${local.previous_environment}-gbl-${app}-deploy@${local.org.project_ids[local.previous_environment]}.iam.gserviceaccount.com"
  ]
  # What each application's stack writes itself, under <app>/stacks/: the
  # object naming the stack's id (<app>/stacks/<env>/stack.json), from which
  # the pipeline learns where the stack's deployment records lie, and in tst
  # the stack jobs' outputs for the pull requests. The prefix the application's
  # apply identity writes under, which the deploy identities' creator binding
  # excludes. The team group's members write there too: bedrock teardown and
  # bedrock destroy, run by a person with their own sign-in, leave their record
  # beside the id (<app>/stacks/<env>/teardown.json), and a destroy takes the
  # id object with the stack.
  records_stack_prefix = { for app in var.applications : app => "projects/_/buckets/${google_storage_bucket.records.name}/objects/${app}/stacks/" }
  records_creator_condition = length(local.records_stack_prefix) == 0 ? null : {
    title       = "${local.name}-records-not-stacks"
    description = "Every object but what the applications' stacks write themselves (their id objects, the stack jobs' outputs), which their apply identities write."
    expression  = join(" && ", [for app, prefix in local.records_stack_prefix : "!resource.name.startsWith(\"${prefix}\")"])
  }
  records_stack_bindings = [
    for app, prefix in local.records_stack_prefix : {
      role    = "roles/storage.objectUser"
      members = [google_service_account.apply[app].member, local.team_group]
      condition = {
        title       = "${local.name}-${app}-stacks"
        description = "What the application's stack writes itself: its id object, and the stack jobs' outputs of its pull requests; its apply identity writes them, and the team group's members running bedrock teardown or destroy write the teardown record and take the id object with a destroyed stack."
        expression  = "resource.name.startsWith(\"${prefix}\")"
      }
    }
  ]
  records_bindings = concat([
    for b in [
      { role = "roles/storage.objectCreator", members = local.records_deploy_members, condition = local.records_creator_condition },
      { role = "roles/storage.objectViewer", members = concat(local.records_deploy_members, local.records_plan_members, local.records_ops_members, local.records_lower_ops_members, local.records_rehearsal_members, values(local.next_deploy_members), [local.team_group]), condition = null },
    ] : b if length(b.members) > 0
  ], local.records_stack_bindings)
}

data "google_iam_policy" "records" {
  dynamic "binding" {
    for_each = local.records_bindings
    content {
      role    = binding.value.role
      members = binding.value.members

      dynamic "condition" {
        for_each = binding.value.condition == null ? [] : [binding.value.condition]
        content {
          title       = condition.value.title
          description = condition.value.description
          expression  = condition.value.expression
        }
      }
    }
  }
}

resource "google_storage_bucket_iam_policy" "records" {
  bucket      = google_storage_bucket.records.name
  policy_data = data.google_iam_policy.records.policy_data
}



