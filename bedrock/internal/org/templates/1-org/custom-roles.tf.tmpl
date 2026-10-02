# ---------------------------------------------------------------------------
# Custom organization roles
#
# Roles granted to layer identities where no predefined role has the right
# shape. Created at the org node so one definition serves every project, and
# named in var.layer_roles by bare ID (see local.custom_roles).
# ---------------------------------------------------------------------------

# Secret Manager without the payloads. A layer identity creates secrets as
# containers and manages their IAM and the lifecycle of their versions, but
# never reads or writes a version: values are added by the operator that owns
# them (secretmanager.versions.add) and read by the workload that needs them
# (secretmanager.versions.access). roles/secretmanager.admin carries both, which
# would put every secret value within reach of the deploy pipeline.
resource "google_organization_iam_custom_role" "secret_container_admin" {
  org_id      = local.org_id
  role_id     = "secretContainerAdmin"
  title       = "Secret Container Admin"
  description = "Creates and manages Secret Manager secrets and the lifecycle of their versions, without access to any version payload."
  permissions = [
    "secretmanager.locations.get",
    "secretmanager.locations.list",
    "secretmanager.secrets.create",
    "secretmanager.secrets.delete",
    "secretmanager.secrets.get",
    "secretmanager.secrets.getIamPolicy",
    "secretmanager.secrets.list",
    "secretmanager.secrets.setIamPolicy",
    "secretmanager.secrets.update",
    "secretmanager.versions.destroy",
    "secretmanager.versions.disable",
    "secretmanager.versions.enable",
    "secretmanager.versions.get",
    "secretmanager.versions.list",
  ]
}

# Secret Manager for the operator who owns the values. The operator group
# creates a container ahead of the release that first reads it (bedrock
# secret add) and adds versions to it; the application stack adopts the
# container at its next apply. Creating and adding only: no payload access,
# no IAM, and no lifecycle of versions beyond adding (those stay with the
# stack's identity, secretContainerAdmin). roles/secretmanager.
# secretVersionAdder adds versions to a container that exists and creates
# none, which would put the container back with the pipeline and its
# creation a release behind the value.
resource "google_organization_iam_custom_role" "secret_operator" {
  org_id      = local.org_id
  role_id     = "secretOperator"
  title       = "Secret Operator"
  description = "Creates Secret Manager secrets and adds versions to them, without access to any version payload."
  permissions = [
    "secretmanager.secrets.create",
    "secretmanager.secrets.get",
    "secretmanager.secrets.list",
    "secretmanager.versions.add",
    "secretmanager.versions.get",
    "secretmanager.versions.list",
  ]
}

# Cloud Run job policies for the deploy pipeline. bedrock deploy jobs makes a
# job per build for an application's job process and copies the template job's
# IAM policy onto it, so the site's identity may start the job of its own build.
# Setting a job's policy is run.jobs.setIamPolicy, which roles/run.developer
# lacks and roles/run.admin carries together with the policy of every service,
# which the pipeline has no business setting. Read and set the policy of a job,
# nothing else; 2-env grants it to each application's deploy identity beside
# roles/run.developer.
resource "google_organization_iam_custom_role" "run_job_policy_admin" {
  org_id      = local.org_id
  role_id     = "runJobPolicyAdmin"
  title       = "Cloud Run Job Policy Admin"
  description = "Reads and sets the IAM policy of Cloud Run jobs, and nothing else about them."
  permissions = [
    "run.jobs.getIamPolicy",
    "run.jobs.setIamPolicy",
  ]
}

# A pull-request build plans each environment's application stack as that
# environment's plan identity, a reader, and the stack's database, its grants
# and its backup schedules live on a Spanner instance: the environment's own,
# or the shared one in 2-spn's project, which the environment project's reader
# roles do not reach. Reading a database, its IAM policy and its backup
# schedules is spread over roles that also write (databaseAdmin, backupAdmin)
# or read the data (databaseReader), so a role of the reads alone, granted on
# the instance to each application's plan identity by 2-spn (the shared
# instance) and 2-env (an environment's own).
resource "google_organization_iam_custom_role" "spanner_plan_reader" {
  org_id      = local.org_id
  role_id     = "spannerPlanReader"
  title       = "Spanner Plan Reader"
  description = "Reads an instance's databases, their IAM policies and their backup schedules, and nothing of their data."
  permissions = [
    "spanner.instances.get",
    "spanner.databases.get",
    "spanner.databases.list",
    "spanner.databases.getIamPolicy",
    "spanner.backupSchedules.get",
    "spanner.backupSchedules.list",
  ]
}

# A restore of an environment to a release is started from GitHub: the
# application's operations workflow exchanges its token for the environment's
# operations identity and runs the environment's version trigger with the
# restore instruction, and the pipeline does the work as the deploy identity.
# That identity starts a trigger's build and reads how it went, and nothing
# else: roles/cloudbuild.builds.editor would also cancel and retry builds and
# write every trigger. Granted on the environment project to each
# application's operations identity by 2-env, in every environment but
# production, which is never restored by a run.
resource "google_organization_iam_custom_role" "cloud_build_trigger_runner" {
  org_id      = local.org_id
  role_id     = "cloudBuildTriggerRunner"
  title       = "Cloud Build Trigger Runner"
  description = "Runs Cloud Build triggers and reads the builds they start, and nothing else."
  permissions = [
    "cloudbuild.builds.create",
    "cloudbuild.builds.get",
    "cloudbuild.builds.list",
    "cloudbuild.triggers.get",
    "cloudbuild.triggers.list",
  ]
}

# The task queue operator: pauses, resumes and purges a Cloud Tasks queue, and
# reads it, nothing else. Granted on an application's own queue, in the
# application's stack, to its deploy identity: a release that puts the
# application into maintenance (a restore run, a breaking release) pauses the
# queue while the database is replaced or migrated and resumes it after
# traffic moves, so no task is delivered to a server that cannot take it; a
# restore purges it too, since every queued task refers to rows that are about
# to disappear. It is a custom role because the cloud's own
# roles/cloudtasks.queueAdmin would also create, change and delete queues.
resource "google_organization_iam_custom_role" "cloud_tasks_queue_operator" {
  org_id      = local.org_id
  role_id     = "cloudTasksQueueOperator"
  title       = "Cloud Tasks Queue Operator"
  description = "Pauses, resumes and purges a Cloud Tasks queue, and reads it, and nothing else."
  permissions = [
    "cloudtasks.queues.get",
    "cloudtasks.queues.pause",
    "cloudtasks.queues.resume",
    "cloudtasks.queues.purge",
  ]
}

# The pipeline's first step reads the build it runs in (bedrock deploy resolve:
# the trigger's kind, the tag or the pull request, the restore instruction and
# who asked for it) through the Cloud Build API, and that is all a build needs
# of Cloud Build. roles/cloudbuild.builds.builder, the cloud's bundle for a
# build's service account, carries that read together with every object of
# every bucket in the project (list, read, overwrite, delete): in an
# environment project the deployment records and the applications' file
# stores, so with it a record is not written once and an application's
# uploaded files are the build's to delete. The deploy identity holds this
# role instead and reaches Cloud Storage through its bucket grants alone
# (2-env: creator and viewer on the records bucket). In tst, where Cloud
# Scheduler runs each application's sweep trigger as its deploy identity,
# 2-env grants cloudBuildTriggerRunner beside it.
resource "google_organization_iam_custom_role" "cloud_build_build_reader" {
  org_id      = local.org_id
  role_id     = "cloudBuildBuildReader"
  title       = "Cloud Build Build Reader"
  description = "Reads Cloud Build builds, and nothing else."
  permissions = [
    "cloudbuild.builds.get",
  ]
}

# A pull-request build plans each environment's application stack as that
# environment's plan identity, a reader. roles/viewer, the cloud's bundle for a
# reader, reads data as well as resources: Spanner rows where the database is
# in the environment project (tst), and the application's uploaded files through
# the bucket's default grants to project viewers. A plan refreshes what the stack
# manages and reads no data, so the plan identity holds this role instead: the
# get and list of every resource type the application stack declares, found in
# the lab from the plans' refusals, and nothing of what those resources hold.
# The IAM policies the stack's grants are refreshed through are
# roles/iam.securityReviewer's, granted beside it.
resource "google_organization_iam_custom_role" "application_plan_reader" {
  org_id      = local.org_id
  role_id     = "applicationPlanReader"
  title       = "Application Plan Reader"
  description = "Reads the resources an application stack declares, for a plan, and nothing of their data."
  permissions = [
    # Cloud Build: the application's triggers. A regional trigger's read is answered on
    # builds.get; triggers.get is not asked for.
    "cloudbuild.builds.get",
    # Cloud Scheduler: the sweep's schedule.
    "cloudscheduler.jobs.get",
    # Cloud Tasks: the application's queue.
    "cloudtasks.queues.get",
    # The load balancer's backend services and serverless network endpoint groups.
    "compute.backendServices.get",
    "compute.regionNetworkEndpointGroups.get",
    # Firestore: the database's metadata (what its read checks; databases.get is not).
    "datastore.databases.getMetadata",
    # Cloud Run: the services, the jobs, and a service's tag bindings, which Cloud Run
    # checks on its own permission rather than Resource Manager's.
    "run.jobs.get",
    "run.services.get",
    "run.services.listTagBindings",
    # Secret Manager: the secrets, and the metadata of the versions a revision pins (the
    # pipeline checks after the plan that each exists and is enabled); never a payload.
    "secretmanager.secrets.get",
    "secretmanager.versions.get",
    # Cloud Storage: the assets bucket, and not its objects.
    "storage.buckets.get",
  ]
}
