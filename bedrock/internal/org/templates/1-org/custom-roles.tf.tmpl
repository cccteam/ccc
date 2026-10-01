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
