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

# Secret Manager for the person who owns the values. A member of the
# environment's team group holds this role for a short time through the secret
# operator entitlement (2-env/team-group.tf): they create a container ahead of
# the release that first reads it (bedrock secret add) and add versions to it,
# and the application stack adopts the container at its next apply. Creating
# and adding only: no payload access, no IAM, and no lifecycle of versions
# beyond adding (those stay with the stack's identity, secretContainerAdmin).
# roles/secretmanager.secretVersionAdder adds versions to a container that
# exists and creates none, which would put the container back with the
# pipeline and its creation a release behind the value.
resource "google_organization_iam_custom_role" "secret_operator" {
  org_id      = local.org_id
  role_id     = "secretOperator"
  title       = "Secret Operator"
  description = "Creates Secret Manager secrets and adds versions to them, without access to any version payload."
  permissions = [
    # bedrock secret add and pin find the environment project by its labels and
    # bill Secret Manager to it: the project's read, and the use of its services.
    "resourcemanager.projects.get",
    "secretmanager.secrets.create",
    "secretmanager.secrets.get",
    "secretmanager.secrets.list",
    "secretmanager.versions.add",
    "secretmanager.versions.get",
    "secretmanager.versions.list",
    "serviceusage.services.use",
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

# Creating a database is checked on the instance (spanner.databases.create),
# as is listing what the instance holds: a list is a request on the instance,
# so no condition on a database's or a backup's name can admit it. Each
# application's apply identity holds this role on its instance without
# condition, and the admin roles under a condition naming its own database
# and backups (2-spn for the shared instance, 2-env for an environment's
# own), so an identity makes its own database and sees what else exists, and
# changes nothing it did not make.
resource "google_organization_iam_custom_role" "spanner_database_creator" {
  org_id      = local.org_id
  role_id     = "spannerDatabaseCreator"
  title       = "Spanner Database Creator"
  description = "Creates a database on an instance and lists the instance's databases, backups and operations; changes nothing that exists."
  permissions = [
    "spanner.backupOperations.list",
    "spanner.backups.list",
    "spanner.databaseOperations.list",
    "spanner.databases.create",
    "spanner.databases.list",
    "spanner.instances.get",
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
# production, which is never restored by a run; and in tst to each
# application's deploy identity, as which Cloud Scheduler runs the hourly
# sweep trigger.
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
# read of every resource type the application stack declares, found in
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
    # Firestore: the database's metadata (what its read checks; databases.get is not),
    # and its composite indexes and field settings (the time-to-live policies).
    "datastore.databases.getMetadata",
    "datastore.indexes.get",
    # Firebase Rules: the database's security rules, the ruleset and its release.
    "firebaserules.releases.get",
    "firebaserules.rulesets.get",
    # Cloud Logging: the migrate job's log bucket and the sink that fills it (the stack's
    # logging.tf); never an entry.
    "logging.buckets.get",
    "logging.sinks.get",
    # Cloud Run: the services, the jobs, and a service's tag bindings, which Cloud Run
    # checks on its own permission rather than Resource Manager's.
    "run.jobs.get",
    "run.services.get",
    "run.services.listTagBindings",
    # Secret Manager: the secrets, and the metadata of the versions a revision pins (the
    # pipeline checks after the plan that each exists and is enabled); never a payload.
    "secretmanager.secrets.get",
    "secretmanager.versions.get",
    # API keys: the web API key the application's browsers present, by name and string
    # (a public value by design). The API Keys service answers on its own permissions,
    # not Service Usage's: the key's read asks apikeys.keys.get and, for the string the
    # stack hands the site, apikeys.keys.getKeyString.
    "apikeys.keys.get",
    "apikeys.keys.getKeyString",
    # Cloud Storage: the file stores' buckets, and not their objects.
    "storage.buckets.get",
  ]
}

# ---------------------------------------------------------------------------
# The layer plan identities' roles
#
# A pull request plans each project's layer as that project's plan identity
# ({prefix}-{key}-gbl-plan, service-accounts.tf), a reader. roles/viewer, the
# cloud's bundle for a reader, reads data as well as resources: the rows of
# every Spanner database in the project (tst's own instance, and stg's and prd's
# databases on the shared instance in spn's project), every container image in
# shr's registry, and the deployment records and the applications' uploaded
# files through the buckets' default grants to project viewers. A plan
# refreshes what the layer manages and reads no data, so each plan identity
# holds, in place of roles/viewer, the role of its project's kind below: the
# read of every resource type the layer declares, found in the lab from the
# plans' refusals (the layer planned as its plan identity, each refused
# permission added, the list narrowed by taking permissions out again), and
# nothing of what those resources hold. The IAM policies the layer's grants
# are refreshed through are roles/iam.securityReviewer's, granted beside it
# (var.plan_roles).
# ---------------------------------------------------------------------------

# The environment layer (2-env, planned once per environment: tst, stg, prd).
resource "google_organization_iam_custom_role" "environment_layer_plan_reader" {
  org_id      = local.org_id
  role_id     = "environmentLayerPlanReader"
  title       = "Environment Layer Plan Reader"
  description = "Reads the resources an environment's layer declares, for a plan, and nothing of their data."
  permissions = [
    # Cloud Build: the GitHub connection and each application's repository link.
    "cloudbuild.connections.get",
    "cloudbuild.repositories.get",
    # The pull-request load balancer's backend service and serverless network
    # endpoint groups (tst alone holds them).
    "compute.backendServices.get",
    "compute.regionNetworkEndpointGroups.get",
    # Identity Platform: the project's configuration. The admin API answers the
    # read on this permission and names none when it refuses.
    "firebaseauth.configs.get",
    # The operations workflow's identity pool, its provider and the pool's
    # attestation rules, which the provider reads beside the pool (the
    # environments a run may restore; production has no pool).
    "iam.workloadIdentityPoolProviders.get",
    "iam.workloadIdentityPools.get",
    "iam.workloadIdentityPools.getAttestationRules",
    # Privileged Access Manager: the team group's entitlements (team-group.tf).
    "privilegedaccessmanager.entitlements.get",
    # Secret Manager: the GitHub deployer key's container; never a payload.
    "secretmanager.secrets.get",
    # Spanner: the environment's own instance (tst), and nothing it holds.
    "spanner.instances.get",
    # Cloud Storage: the deployment records bucket, and not its objects. The
    # state bucket's policy, which the slots this layer grants there refresh
    # through, is bucketPolicyReader's, granted on that bucket (workflow.tf).
    "storage.buckets.get",
  ]
}

# The shared services layer (2-shr: the container registry).
resource "google_organization_iam_custom_role" "services_layer_plan_reader" {
  org_id      = local.org_id
  role_id     = "servicesLayerPlanReader"
  title       = "Services Layer Plan Reader"
  description = "Reads the resources the shared services layer declares, for a plan, and nothing of their data."
  permissions = [
    # Artifact Registry: each application's image repository. The repositories'
    # IAM policies come from securityReviewer, and nothing here reads an image.
    "artifactregistry.repositories.get",
  ]
}

# The shared network layer (2-net: the load balancer, DNS and certificates).
resource "google_organization_iam_custom_role" "network_layer_plan_reader" {
  org_id      = local.org_id
  role_id     = "networkLayerPlanReader"
  title       = "Network Layer Plan Reader"
  description = "Reads the resources the shared network layer declares, for a plan, and nothing of their data."
  permissions = [
    # Certificate Manager: the certificate, its DNS authorization, the
    # certificate map and its entries.
    "certificatemanager.certmapentries.get",
    "certificatemanager.certmaps.get",
    "certificatemanager.certs.get",
    "certificatemanager.dnsauthorizations.get",
    # The global load balancer: the sink backend service, the address, the
    # forwarding rules, the SSL policy, the proxies and the URL maps.
    "compute.backendServices.get",
    "compute.globalAddresses.get",
    "compute.globalForwardingRules.get",
    "compute.sslPolicies.get",
    "compute.targetHttpProxies.get",
    "compute.targetHttpsProxies.get",
    "compute.urlMaps.get",
    # Cloud DNS: the zones (the API names no permission when it refuses). The
    # record sets refresh through securityReviewer's list of them. The domain
    # registrations are roles/domains.viewer's, granted beside this role
    # (var.plan_roles): Cloud Domains permissions are not supported in custom roles.
    "dns.managedZones.get",
  ]
}

# The shared Spanner layer (2-spn: the instance stg and prd share).
resource "google_organization_iam_custom_role" "spanner_layer_plan_reader" {
  org_id      = local.org_id
  role_id     = "spannerLayerPlanReader"
  title       = "Spanner Layer Plan Reader"
  description = "Reads the resources the shared Spanner layer declares, for a plan, and nothing of their data."
  permissions = [
    # Privileged Access Manager: the environments' Spanner entitlements on this
    # project (entitlements.tf).
    "privilegedaccessmanager.entitlements.get",
    # Spanner: the shared instance. Its IAM policy comes from securityReviewer,
    # and nothing here reads a database.
    "spanner.instances.get",
  ]
}

# The state bucket's IAM policy, for the environment layers' plan identities.
# 2-env grants the application identities it creates their slots in the state
# bucket, which lives in the boot project, where the environment project's
# roles do not reach, and a plan of 2-env refreshes those grants through the
# bucket's policy. roles/iam.securityReviewer is granted on the project, and
# the bucket roles that read a policy also set it (legacyBucketOwner) or carry
# every object (admin). 1-org grants this role on the state bucket to each
# environment layer's plan identity (workflow.tf), beside the list and the
# conditioned reads of the state.
resource "google_organization_iam_custom_role" "bucket_policy_reader" {
  org_id      = local.org_id
  role_id     = "bucketPolicyReader"
  title       = "Bucket Policy Reader"
  description = "Reads a bucket's IAM policy, and nothing else about it."
  permissions = [
    "storage.buckets.getIamPolicy",
  ]
}
