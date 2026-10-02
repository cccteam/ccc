# 1-org

Where the organization is described: folders, the org policy baseline,
Essential Contacts, every project, and the identities that will manage each
project's own layer. Applied by the bootstrap administrator for now, later by Cloud Build in
the boot project as `imp-org-gbl-tofu`.

## What it creates

- Folders `shared`, `tst`, `stg`, and `prd` at the org root, beside the
  `terraform` folder from `0-bootstrap`. They exist ahead of the projects
  because they are where org policy and Essential Contacts attach.
- The org policy baseline on each folder, from `var.org_policies`, plus
  `essentialcontacts.managed.allowedContactDomains` from
  `var.allowed_contact_domains`. Folder-scoped, so the `terraform` folder and
  the identities in it are never locked out by a constraint they manage.
- Essential Contacts on each folder, from `var.essential_contact_emails`
  (empty for now).
- Six projects from `var.projects`, `imp-<key>-gbl-core-<suffix>`, each with
  its API set, labels (`bedrock-lab`, `environment = <key>`), and no default
  compute service account:

  | Key | Folder | Purpose | API set | Role set |
  |---|---|---|---|---|
  | `shr` | shared | Shared services: container images | shr | shr |
  | `net` | shared | Shared network: global load balancer, DNS, certificates | net | net |
  | `spn` | shared | Shared Spanner instance for stg and prd | spn | spn |
  | `tst` | tst | Environment project: tst (holds its own Spanner instance) | app | app |
  | `stg` | stg | Environment project: stg | app | app |
  | `prd` | prd | Environment project: prd | app | app |

- Per project, two service accounts in the project: the layer identity
  `imp-<key>-gbl-tofu`, holding the roles of its role set, and the read-only
  plan identity `imp-<key>-gbl-plan`, holding `roles/viewer` and
  `roles/browser`. Both get `roles/serviceusage.serviceUsageConsumer` on the
  boot project, which is their quota project.
- The custom organization role `secretContainerAdmin`: creates, updates,
  deletes, and lists secrets, manages their IAM, and enables, disables, and
  destroys versions, but can neither add a version (`versions.add`) nor read
  one (`versions.access`). It replaces `roles/secretmanager.admin` in the
  `app` and `shr` role sets so a deploy pipeline can shape secrets without
  seeing their values.
- The custom organization role `secretOperator`: creates secrets and adds
  versions to them (`secrets.create`, `secrets.get`, `secrets.list`,
  `versions.add`, `versions.get`, `versions.list`), and can neither read a
  version nor touch a secret's IAM or lifecycle. It is the operator group's
  role, granted on each environment project by `2-env` (`secret_operators`):
  an operator creates a container ahead of the release that first reads it
  (`bedrock secret add`) and the application stack adopts it.
- The custom organization role `spannerPlanReader`: reads an instance's
  databases, their IAM policies and their backup schedules
  (`spanner.instances.get`, `spanner.databases.get`, `spanner.databases.list`,
  `spanner.databases.getIamPolicy`, `spanner.backupSchedules.get`,
  `spanner.backupSchedules.list`) and nothing of their data. A pull-request
  build plans each environment's application stack as that environment's
  plan identity, a reader, and the stack's database, its grants and its
  backup schedules live on a Spanner instance that the environment project's
  reader roles do not reach; the predefined roles that read them also write
  (`databaseAdmin`, `backupAdmin`) or read the data (`databaseReader`).
  `2-spn` grants it on the shared instance and `2-env` on an environment's
  own instance to each application's plan identity.
- The custom organization role `cloudBuildTriggerRunner`: runs Cloud Build
  triggers and reads the builds they start (`cloudbuild.builds.create`,
  `cloudbuild.builds.get`, `cloudbuild.builds.list`, `cloudbuild.triggers.get`,
  `cloudbuild.triggers.list`) and nothing else. A restore of an environment to
  a release is started from GitHub: the application's operations workflow
  exchanges its token for the environment's operations identity and runs the
  environment's version trigger with the restore instruction, and the pipeline
  does the work as the deploy identity. `2-env` grants the role on each
  environment project to each application's operations identity, in every
  environment but production, and in tst to each application's deploy
  identity as well, as which Cloud Scheduler runs the hourly sweep trigger.
- The custom organization role `cloudBuildBuildReader`: reads Cloud Build
  builds (`cloudbuild.builds.get`) and nothing else. The pipeline's first step
  reads the build it runs in (the trigger's kind, the tag or the pull request,
  the restore instruction and who asked for it). `2-env` grants it on each
  environment project to each application's deploy identity in place of
  `roles/cloudbuild.builds.builder`, the cloud's bundle for a build's service
  account, which carries that read together with every object of every bucket
  in the project: with it a deployment record is not written once and an
  application's uploaded files are the build's to delete.
- The custom organization role `applicationPlanReader`: the reads a plan of an
  application stack needs, found in the lab from the plans' refusals: Cloud
  Build builds (`cloudbuild.builds.get`, which answers a regional trigger's
  read), Cloud Scheduler jobs, Cloud Tasks queues, backend services and
  serverless network endpoint groups, a Firestore database's metadata, Cloud
  Run services and jobs and a service's tag bindings, secrets and the metadata
  of their versions, and buckets; nothing of what those resources hold. `2-env`
  grants it on each environment project to each application's plan identity
  in place of `roles/viewer`, which reads data as well: the rows of a Spanner
  database in the project, and the application's uploaded files through the
  bucket's default grants to project viewers. The IAM policies the stack's
  grants are refreshed through come from `roles/iam.securityReviewer`, granted
  beside it.
- The custom organization role `cloudTasksQueueOperator`: pauses, resumes and
  purges a Cloud Tasks queue, and reads it, nothing else. An application's stack
  grants it on the application's own queue to its deploy identity: a release that
  puts the application into maintenance (a restore run, a breaking release) pauses
  the queue while the database is replaced or migrated and resumes it after traffic
  moves; a restore purges it too. The cloud's own `roles/cloudtasks.queueAdmin`
  would also create, change and delete queues.
- The custom organization role `runJobPolicyAdmin`: reads and sets the IAM
  policy of Cloud Run jobs (`run.jobs.getIamPolicy`, `run.jobs.setIamPolicy`)
  and nothing else. `2-env` grants it to each application's deploy identity
  beside `roles/run.developer`, which lacks the setting: `bedrock deploy jobs`
  makes a job per build for the application's job process and copies the
  template job's policy onto it, so the site's identity may start the job of
  its own build. `roles/run.admin` would carry that permission together with
  the policy of every service.

- The applications' GitHub repositories, from `applications.auto.tfvars`
  (rendered by `bedrock org register`): each repository (private; squash the
  only merge method, the squashed commit titled from the pull request; the head
  branch deleted on merge; never destroyed by this layer), its three rulesets
  (release tags `v*` and `*/v*` created, moved or deleted by the release app
  alone, with no bypass for the repository's admins; the default branch and the
  hotfix lines `hotfix/*` changed by pull request alone, no force push, no
  deletion, with the branch up to date with its base, the required checks
  passing and squash the only merge) and the GitHub Environments the operations
  workflow runs in (`tst` and `stg`, each deploying from the
  default branch alone). See "The applications' repositories" below.

## Applying

In a terminal, in this directory, after `0-bootstrap` has been applied and its
state migrated. `terraform.tfvars` needs `boot_project_id` from the
`0-bootstrap` output, and `initialize.tf` needs the bucket name in its backend
block (the same substitution as in `0-bootstrap`).

```bash
cd 1-org
tofu init
tofu plan
tofu apply
```

Review the plan carefully. It creates folders, org policy, and projects with
`deletion_policy = "PREVENT"`, which is hard to undo. The first `init` writes
`.terraform.lock.hcl`; commit it.

Set `essential_contact_emails` in `terraform.tfvars` before applying if you
want Essential Contacts registered. Addresses must be in one of
`allowed_contact_domains`.

## The applications' repositories

GitHub is configured here, by OpenTofu with the GitHub provider, never by a
bedrock command: `github.tf` declares each application's repository, its rules
and its Environments, and the operator applies it with their own sign-in. Set
`GITHUB_TOKEN` to the token of an owner of the organization before `tofu plan`
or `tofu apply` (`export GITHUB_TOKEN=$(gh auth token)` with gh signed in as
that owner); the audit log then names the person. bedrock commands use the
GitHub API only to act: `bedrock restore` dispatches a workflow, `bedrock
hotfix` creates branches and pull requests, the pipeline talks back on a pull
request.

The required checks on the default branch and the hotfix lines are the
pull-request build, which Cloud Build reports under the trigger's name,
`imp-tst-<region>-<app>-pr`, followed by tst's project
id in parentheses (the trigger is set by the application's stack in
tst, and the project is this layer's), the infrastructure
workflow's job, `bedrock check`, which GitHub Actions reports, and the jobs of
the application's own CI workflow, `.github/workflows/ci.yml`, which `impulse
render` writes and GitHub Actions reports by their ids:
`title`, `go`, `image`, `secrets` and `migrations`. Every application's workflow
carries those; bedrock reads the list from impulse when it renders
`github.tf`, so the names have one source. The workflow's browser jobs
(`angular-<workspace>`, one per browser workspace) are not required by the
rule, since the placement does not carry each application's workspaces. A
pull request merges once every required check passes on its latest commit and
its branch holds every commit of its base; the pull-request build runs on
`/gcbrun`, so a pull request nobody built never merges, release-please's
release pull requests included. A renamed check is one change to `github.tf`,
timed with the release that renames the trigger; requiring both names would
block every pull request. The rule requires impulse's names once every
application's workflow reports them: an application takes the workflow
(`impulse render`) before this layer is applied, or its pull requests block.

When `github_infrastructure_team` names a team of the organization (its slug;
empty by default), a change to the files that define what the checks run
(`.github/workflows/**` and `cloudbuild*.yaml`) needs one approval from that
team, given after the last push, so no author merges a check change alone.
The team and its members are the organization's own setting; this layer
reads it.

GitHub features this uses, on a private repository: rulesets with required
status checks and required reviewers, and deployment branch policies on
Environments. The layer asks GitHub nothing about the organization's plan; a
feature the plan lacks is refused by GitHub, and that refusal is the message.

A repository that exists before this layer declares it (one made by hand, or
configured by the retired `bedrock repository protect`) is imported into the
state before the first apply, so the apply changes it instead of making a
second one. For each such application, in this directory, with `GITHUB_TOKEN`
set (the ruleset ids come from `gh api repos/<org>/<app>/rulesets`, the
deployment policy ids from
`gh api repos/<org>/<app>/environments/<env>/deployment-branch-policies`):

```bash
tofu import 'github_repository.app["<app>"]' <app>
tofu import 'github_repository_ruleset.release_tags["<app>"]' <app>:<id>
tofu import 'github_repository_ruleset.branch["<app>:default"]' <app>:<id>
tofu import 'github_repository_ruleset.branch["<app>:hotfix"]' <app>:<id>
tofu import 'github_repository_environment.restorable["<app>-<env>"]' <app>:<env>
tofu import 'github_repository_environment_deployment_policy.default_branch["<app>-<env>"]' <app>:<env>:<id>
```

The plan then shows what the declaration changes: the merge settings, the
required checks, the rulesets' names.

## Inputs

| Name | Description | Type | Default | Required |
|---|---|---|---|:---:|
| `allowed_contact_domains` | Domains permitted as Essential Contacts. | `list(string)` | `["@impulseframework.com", "@cloud-team.com"]` | no |
| `audit_log_retention_days` | Retention for the audit log bucket; only with `central_logging`. | `number` | `400` | no |
| `billing_account_id` | Billing account to link projects to. | `string` | n/a | yes |
| `boot_project_id` | Boot project from `0-bootstrap`; quota and billing project for API calls. | `string` | n/a | yes |
| `central_logging` | Create the log project, audit log bucket, and folder sinks. | `bool` | `false` | no |
| `essential_contact_emails` | Addresses registered as Essential Contacts on every folder. | `list(string)` | `[]` | no |
| `gcp_region` | Primary region. | `string` | `"us-central1"` | no |
| `gcp_secondary_region` | Secondary region, published as an output. | `string` | `"us-west3"` | no |
| `layer_roles` | Project roles per role set; a bare ID names a custom role from `custom-roles.tf`. | `map(list(string))` | `app`, `net`, `shr`, `spn`; see below | no |
| `org_policies` | Constraints applied to every folder. | `list(object)` | see below | no |
| `organization_domain` | Domain of the GCP organization. | `string` | n/a | yes |
| `prefix` | Short org-wide prefix used in resource names. | `string` | `"imp"` | no |
| `projects` | Every project, keyed by environment code. | `map(object)` | the six above | no |
| `required_apis` | APIs per API set. | `map(list(string))` | `app`, `log`, `net`, `shr`, `spn` | no |

`terraform.tfvars` sets the required ones.

### Role sets

| Set | Roles |
|---|---|
| `app` | artifactregistry.admin, cloudbuild.builds.editor, cloudbuild.connectionAdmin, cloudscheduler.admin, cloudtasks.queueAdmin, iam.serviceAccountAdmin, iam.serviceAccountUser, iam.workloadIdentityPoolAdmin, logging.admin, monitoring.admin, resourcemanager.projectIamAdmin, run.admin, serviceusage.serviceUsageAdmin, spanner.admin, storage.admin, `secretContainerAdmin` |
| `shr` | artifactregistry.admin, iam.serviceAccountAdmin, iam.serviceAccountUser, iam.workloadIdentityPoolAdmin, logging.admin, monitoring.admin, resourcemanager.projectIamAdmin, serviceusage.serviceUsageAdmin, storage.admin, `secretContainerAdmin` |
| `net` | certificatemanager.editor, compute.loadBalancerAdmin, compute.networkAdmin, compute.securityAdmin, dns.admin, iam.serviceAccountAdmin, iam.serviceAccountUser, logging.admin, monitoring.admin, resourcemanager.projectIamAdmin, serviceusage.serviceUsageAdmin, storage.admin |
| `spn` | iam.serviceAccountAdmin, iam.serviceAccountUser, logging.admin, monitoring.admin, resourcemanager.projectIamAdmin, serviceusage.serviceUsageAdmin, spanner.admin, storage.admin |

### API sets

| Set | APIs |
|---|---|
| `app` | artifactregistry, cloudbuild, cloudidentity, cloudscheduler, cloudtasks, cloudtrace, compute, iam, iamcredentials, logging, monitoring, run, secretmanager, spanner, sts |
| `shr` | artifactregistry, iam, iamcredentials, logging, monitoring, secretmanager, sts |
| `net` | certificatemanager, compute, dns, iam, logging, monitoring |
| `spn` | iam, logging, monitoring, spanner |
| `log` | iam, logging, monitoring, storage (only with `central_logging`) |

### Org policies

The default `var.org_policies` keeps these from the tf-gcp-setup baseline.
Each one either closes a door nothing here uses (service account keys,
VM serial consoles, external IPs, HMAC keys) or pins a decision already made
(one load balancer type, Cloud Run behind it, GitHub as the only Cloud Build
source):

| Constraint | Shape |
|---|---|
| `iam.managed.disableServiceAccountKeyCreation` | enforce |
| `iam.managed.disableServiceAccountKeyUpload` | enforce |
| `iam.serviceAccountKeyExpiryHours` | allow `2160h` |
| `iam.allowServiceAccountCredentialLifetimeExtension` | deny all |
| `iam.automaticIamGrantsForDefaultServiceAccounts` | enforce |
| `iam.disableAuditLoggingExemption` | enforce |
| `storage.publicAccessPrevention` | enforce |
| `storage.secureHttpTransport` | enforce |
| `storage.uniformBucketLevelAccess` | enforce |
| `storage.restrictAuthTypes` | deny `in:ALL_HMAC_SIGNED_REQUESTS` |
| `compute.disableGlobalSerialPortAccess` | enforce |
| `compute.disableSerialPortAccess` | enforce |
| `compute.requireOsLogin` | enforce |
| `compute.requireShieldedVm` | enforce |
| `compute.skipDefaultNetworkCreation` | enforce |
| `compute.vmCanIpForward` | deny all |
| `compute.vmExternalIpAccess` | deny all |
| `compute.restrictLoadBalancerCreationForTypes` | allow `GLOBAL_EXTERNAL_MANAGED_HTTP_HTTPS` |
| `run.allowedIngress` | allow `internal-and-cloud-load-balancing` |
| `gcp.restrictTLSVersion` | deny `TLS_VERSION_1`, `TLS_VERSION_1_1` |
| `cloudbuild.disableCreateDefaultServiceAccount` | enforce |
| `cloudbuild.allowedIntegrations` | allow `github.com` |
| `essentialcontacts.managed.allowedContactDomains` | parameters from `var.allowed_contact_domains`, set separately |

Dropped from the model, and why. Add any of them back as an entry in
`var.org_policies`:

- `ainotebooks.disableFileDownloads`, `ainotebooks.disableRootAccess`,
  `ainotebooks.restrictPublicIp`, `appengine.disableCodeDownload`,
  `datastream.disablePublicConnectivity`,
  `firestore.requireP4SAforImportExport`, `sql.restrictAuthorizedNetworks`,
  `sql.restrictPublicIp`, `cloudfunctions.allowedIngressSettings`,
  `cloudfunctions.allowedVpcConnectorEgressSettings`,
  `cloudfunctions.restrictAllowedGenerations`, `dataform.restrictGitRemotes`,
  `compute.restrictDedicatedInterconnectUsage`,
  `compute.restrictNonConfidentialComputing`,
  `iam.workloadIdentityPoolAwsAccounts`: services not used here. An
  organization that adopts one of those services adds the constraint with it.
- `cloudkms.disableBeforeDestroy`: no customer-managed keys here.
- `compute.restrictVpcPeering`: interacts with Private Service Access, which
  a later layer may need; a decision for then.
- `compute.restrictPrivateServiceConnectConsumer`: same reason.
- `iam.managed.preventPrivilegedBasicRolesForDefaultServiceAccounts`: the
  default service accounts are deleted at project creation, and
  `iam.automaticIamGrantsForDefaultServiceAccounts` already covers the window
  before that.
- `iam.managed.allowedPolicyMembers` (domain restricted sharing, in dry run
  in the model): needs a violation-log review loop before it can be enforced;
  not started.

## Central logging

`central_logging = false` here. With it `true` this layer would create:

- The `log` project under `shared` (API set `log`, no layer identity).
- A versioned, uniform-access bucket `imp-log-uc1-audit-logs-<suffix>` in it
  with a `audit_log_retention_days` retention policy, labelled `bedrock-lab`.
- One `google_logging_folder_sink` per folder with `include_children`,
  filtered to `logs/cloudaudit.googleapis.com`, and `roles/storage.objectCreator`
  on the bucket for each sink's writer identity.

The resources are gated with `count` and `for_each` on the flag, so flipping it
is the whole change here. Turning it on also needs, in `0-bootstrap`,
`roles/logging.configWriter` (folder sinks) and `roles/storage.admin` (the
bucket) added to `org_layer_roles`, and a decision on retention. TODO when the
organization needs a system of record for audit logs.

## Outputs

Read by the project layers through `data "terraform_remote_state"` on the state
bucket, prefix `1-org`.

| Name | Description |
|---|---|
| `audit_log_bucket` | Audit log bucket name; `null` while `central_logging` is off. |
| `billing_account_id` | Billing account the projects are linked to. |
| `boot_project_id` | Boot project, quota and billing project for API calls. |
| `folder_ids` | Folder ID by key (shared, tst, stg, prd). |
| `gcp_region`, `region_code` | Primary region and its short code. |
| `gcp_secondary_region`, `secondary_region_code` | Secondary region and its short code. |
| `layer_service_accounts` | Layer identity email by environment code. |
| `log_project_id` | Log project; `null` while `central_logging` is off. |
| `org_id` | Numeric organization ID. |
| `plan_service_accounts` | Plan identity email by environment code. |
| `prefix` | Org-wide naming prefix. |
| `project_ids`, `project_numbers` | Project ID and number by environment code. |
| `secret_container_admin_role` | Full name of the `secretContainerAdmin` role. |
| `secret_operator_role` | Full name of the `secretOperator` role. |
| `run_job_policy_admin_role` | Full name of the `runJobPolicyAdmin` role. |
| `spanner_plan_reader_role` | Full name of the `spannerPlanReader` role. |
| `cloud_build_trigger_runner_role` | Full name of the `cloudBuildTriggerRunner` role. |
| `cloud_build_build_reader_role` | Full name of the `cloudBuildBuildReader` role. |
| `application_plan_reader_role` | Full name of the `applicationPlanReader` role. |
| `cloud_tasks_queue_operator_role` | Full name of the `cloudTasksQueueOperator` role. |
