variable "environment" {
  description = "(Required) Environment this apply targets: tst, stg, or prd. Selects the 2-env state (2-env/<environment>) and this stack's own prefix (3-app/harbor/<environment>)."
  type        = string

  validation {
    condition     = contains(["tst", "stg", "prd"], var.environment)
    error_message = "environment must be one of tst, stg, prd."
  }
}

variable "pull_request" {
  description = <<-EOT
    (Optional) The pull request this stack is an environment for, in tst only;
    0 for the environment itself. A pull-request stack is applied by the
    pull-request build into its own prefix (3-app/harbor/tst/pr<N>). Its
    resources carry the short name harbor-pr<N>, which is how the wildcard backend
    2-env creates once in tst picks the Cloud Run service from the hostname
    harbor-pr<N>.impulseframework.dev; it has a database and identities of its own, reads
    tst's secret containers, and creates no triggers, no containers and no
    backend of its own.
  EOT
  type        = number
  default     = 0

  validation {
    condition     = var.pull_request >= 0 && floor(var.pull_request) == var.pull_request
    error_message = "pull_request is a pull request's number, or 0 for none."
  }

  validation {
    condition     = var.pull_request == 0 || var.environment == "tst"
    error_message = "A pull-request stack lives in tst only."
  }
}

variable "shared_database" {
  description = <<-EOT
    (Optional) For a pull-request stack only: the site runs against tst's
    database instead of one of its own (/gcbrun shared-db). The stack then
    creates no database and grants no DDL: the pull request's app identity gets
    database user on tst's database, and the pipeline never runs the
    migrations, and refuses shared-db when the pull request changes
    anything under schema/migrations against its base, because a migration
    on the shared database would change tst before any release.
  EOT
  type        = bool
  default     = false

  validation {
    condition     = !var.shared_database || var.pull_request != 0
    error_message = "shared_database is for a pull-request stack; the environment itself always has its own database."
  }
}

variable "database_generation" {
  description = <<-EOT
    Which generation of the environment's database the stack points the
    application at: 1, the database this stack created, or the number of the
    database a rollback restored a backup into ("<database>-2", then "-3").
    The pipeline passes it from the fact a rollback writes beside the
    deployment records (bedrock deploy stack plan reads it; nobody sets it by
    hand), so every build after a rollback keeps the generation; the earlier
    generations stay in the stack, protected where the first is, until a later
    change removes them.
  EOT
  type        = number
  default     = 1

  validation {
    condition     = var.database_generation >= 1 && floor(var.database_generation) == var.database_generation
    error_message = "database_generation is a whole number from 1."
  }
  validation {
    condition     = var.database_generation == 1 || var.pull_request == 0
    error_message = "A pull-request stack has one generation of its database; a rollback restores an environment's."
  }
}

variable "hostnames" {
  description = <<-EOT
    Hostnames the site answers on, per environment, in 2-net's convention:
    app.domain for prd, app-stg.domain and app-tst.domain below it, all one
    label under the apps domain because that is what its wildcard certificate
    covers (harbor.tst.<domain> would not be). The label is the one
    placement.json declares for the site (hostnames: harbor), the code by
    default; the organization records the hostnames the stack serves and refuses
    one another application has. The first one is the canonical
    host: each directory registration's redirect URL is built from it, the
    callback the generated router (pkg/router) registers for it:
    https://<host>/portal/api/user/directory/callback for the members auth's directory registration (APP_MEMBERS_DIRECTORY_REDIRECT_URL),
    https://<host>/api/user/directory/callback for the staff auth's directory registration (APP_STAFF_DIRECTORY_REDIRECT_URL).
    Every hostname here is registered with the
    load balancer by an entry in 2-net's hosts; output net_hosts is that entry.
  EOT
  type        = map(list(string))
  default = {
    tst = ["harbor-tst.impulseframework.dev"]
    stg = ["harbor-stg.impulseframework.dev"]
    prd = ["harbor.impulseframework.dev"]
  }

  validation {
    condition     = alltrue([for env, hosts in var.hostnames : length(hosts) > 0])
    error_message = "Every environment needs at least one hostname."
  }
}

variable "placeholder_image" {
  description = "Image every Cloud Run service and job is created with. The pipeline owns the image from the first deploy on (lifecycle ignore_changes), so this is only what runs before the first release."
  type        = string
  default     = "us-docker.pkg.dev/cloudrun/container/hello"
}

variable "cloud_armor" {
  description = "Cloud Armor's policy (cloud-armor.tf) in each environment, by environment name: \"preview\" evaluates the rules and logs what each would have done, \"enforce\" applies them, \"off\" keeps the policy and detaches it from the backend services; an environment left out has no policy. Turn an environment off before removing its entry: an apply that deletes the policy while the backend services still name it fails. A pull-request stack never has a policy."
  type        = map(string)
  default     = {}

  validation {
    condition     = alltrue([for env, mode in var.cloud_armor : contains(["off", "preview", "enforce"], mode)])
    error_message = "Each environment's cloud_armor is \"off\", \"preview\" or \"enforce\"."
  }
}

variable "cloud_armor_sign_in_count" {
  description = "The sign-in POSTs one client address may make to a sign-in route in cloud_armor_sign_in_interval seconds before Cloud Armor's policy answers its further ones 429 (cloud-armor.tf: each session outlet's password login and its pending sign-in's confirmation and cancel, each counted on its own), in every environment whose policy is on. 30 by default, chosen for a login page: a person types a handful of attempts in a minute and an office behind one address a few dozen at its busiest, so neither meets it, while a password guessed from one address is held to the count. Preview first: the request log names the throttle an address would have met."
  type        = number
  default     = 30

  validation {
    condition     = var.cloud_armor_sign_in_count > 0 && floor(var.cloud_armor_sign_in_count) == var.cloud_armor_sign_in_count
    error_message = "cloud_armor_sign_in_count is a positive whole number of requests."
  }
}

variable "cloud_armor_sign_in_interval" {
  description = "The seconds cloud_armor_sign_in_count is counted over, after which an address that met it signs in again. 60 by default: a minute holds a guessing script to the count and keeps a person who met it waiting little."
  type        = number
  default     = 60

  validation {
    condition     = var.cloud_armor_sign_in_interval > 0 && floor(var.cloud_armor_sign_in_interval) == var.cloud_armor_sign_in_interval
    error_message = "cloud_armor_sign_in_interval is a positive whole number of seconds."
  }
}

variable "jobs_timeout" {
  description = "How long one run of the job process (cmd/jobs) may take before Cloud Run stops it, as a duration. Thirty minutes by default."
  type        = string
  default     = "1800s"
}

variable "jobs_retries" {
  description = "How many times Cloud Run retries a failed run of the job process. None by default: a run that failed is looked at, and the application decides what runs again."
  type        = number
  default     = 0
}

variable "jobs_resources" {
  description = "CPU and memory of one run of the job process."
  type = object({
    cpu    = string
    memory = string
  })
  default = {
    cpu    = "1"
    memory = "512Mi"
  }
}

variable "tasks_max_concurrent" {
  description = "How many tasks of the queue (APP_TASKS_QUEUE) Cloud Tasks dispatches at once."
  type        = number
  default     = 10
}

variable "tasks_max_attempts" {
  description = "How many times Cloud Tasks attempts a task of the queue before giving it up, the first try included."
  type        = number
  default     = 5
}

variable "substitutions" {
  description = <<-EOT
    Extra trigger substitutions per environment, for the application's hooks
    and its image build: _NAME = value. The pipeline exports every substitution
    of the build to its hooks (infrastructure/hooks/<stage>.sh, when the
    application commits them) and passes the ones declared here to the image
    build as build arguments (ARG _NAME in the Dockerfile). A name starts with
    an underscore and is upper snake case, and may not be one the pipeline's
    contract already carries (the triggers refuse that) nor start with
    _BUILD_ARG_, the substitutions of the build arguments placement.json
    declares (buildArguments), which this stack renders from its own values.

      substitutions = {
        tst = { _FIREBASE_PROJECT = "acme-tst" }
      }
  EOT
  type        = map(map(string))
  default     = {}

  validation {
    condition     = alltrue([for env, subs in var.substitutions : alltrue([for k in keys(subs) : can(regex("^_[A-Z][A-Z0-9_]*$", k))])])
    error_message = "Every substitution name starts with an underscore and is upper snake case: _NAME."
  }

  validation {
    condition     = alltrue([for env, subs in var.substitutions : alltrue([for k in keys(subs) : !startswith(k, "_BUILD_ARG_")])])
    error_message = "A substitution name may not start with _BUILD_ARG_: those carry the build arguments placement.json declares (buildArguments), from this stack's own values."
  }
}

variable "build_secrets" {
  description = <<-EOT
    Secrets the image build reads, per environment: NAME = the pinned version
    of the container imp-<env>-gbl-harbor-<kebab name>, which this stack creates
    (an operator adds the value with bedrock secret add, and bedrock secret pin
    moves the pin here) and grants the deploy identity accessor on, never a
    runtime identity. The pipeline reads each as the deploy identity and passes
    it to the image build as a BuildKit secret the Dockerfile mounts
    (RUN --mount=type=secret,id=NAME), never as a build argument, which would
    land in the image's history. A pull-request build reads tst's. None by
    default. Example:

      build_secrets = { tst = { KENDO_UI_LICENSE = "1" }, stg = {}, prd = {} }
  EOT
  type        = map(map(string))
  default     = {}

  validation {
    condition     = alltrue([for env, secrets in var.build_secrets : alltrue([for name, v in secrets : can(regex("^[A-Z][A-Z0-9_]*$", name)) && can(regex("^[0-9]+$", v))])])
    error_message = "build_secrets names are UPPER_CASE and each pins a version number: a build reads one version, never latest."
  }
}

variable "secret_versions" {
  description = <<-EOT
    The version of each secret this environment runs, keyed by environment and
    then by the environment variable the secret feeds (the struct tag in
    pkg/config/data.go). A secret with no entry has its container created but
    is not mounted: the process starts without the variable, which is "not
    yet" for a value an operator has not added. tst alone runs
    that way; in every environment after it a release is refused at its plan
    until each secret the code declares has an entry here. A rotation is a
    new version added by an operator plus a one-line bump here, released like
    any change.

    A value is a version number. "latest" is allowed only where the map says
    so, and means the environment tracks whatever version is added next; the
    design brief reserves it for secrets whose placement marks them as
    tracking, and bedrock check names each one, per environment. The word
    "generated" is the entry of a secret the code declares secret:"generated"
    (bedrock render seeds it): the pipeline makes its value and passes its
    version in var.generated_secret_versions, so a number here pins nothing,
    and bedrock check refuses one there and the word on any other secret.

      secret_versions = {
        tst = { APP_COOKIE_KEY = "1", APP_MAIL_APP_PASSWORD = "1" }
      }
  EOT
  type        = map(map(string))
  default = {
    tst = {}
    stg = {}
    prd = {}
  }

  validation {
    condition = alltrue(flatten([
      for env, versions in var.secret_versions : [
        for name, version in versions : can(regex("^([1-9][0-9]*|latest|generated)$", version))
      ]
    ]))
    error_message = "Every secret version is a positive integer, the word latest, or the word generated for a secret the code declares secret:\"generated\"."
  }
}

variable "generated_secret_versions" {
  description = <<-EOT
    The version of each generated secret this environment runs (the fields of
    pkg/config/data.go tagged secret:"generated"), keyed by the environment
    variable: the number the pipeline's plan step derived from the container's
    newest enabled version (adding version 1 as the apply identity when the
    container was empty) and passed with -var, the way the image digest is
    derived at build time. Never written in terraform.tfvars. A plan run by
    hand without it mounts no generated secret and shows that as its change;
    the next release build mounts them again.
  EOT
  type        = map(string)
  default     = {}

  validation {
    condition     = alltrue([for name, version in var.generated_secret_versions : can(regex("^[1-9][0-9]*$", version))])
    error_message = "Every generated secret version is a positive integer, the number the build derived."
  }
}

variable "teardown" {
  description = <<-EOT
    Set by bedrock teardown alone, never in terraform.tfvars: the environment
    is being taken down to what it keeps (its database, its files, its secrets,
    its identities, its Firestore database, its task queue, its migrate logs
    and its id) and the Cloud Run services may go, prd's included. The
    command applies the stack with it before the destroy, so that the state
    records the services' deletion protection lifted, which is what the
    provider reads at the destroy.
  EOT
  type        = bool
  default     = false
}

variable "destroy" {
  description = <<-EOT
    Set by bedrock destroy alone, never in terraform.tfvars: the environment
    is being destroyed whole, its database, files, secrets and Firestore
    database included. The command applies the stack with it before the
    destroy, so that the state records every protection lifted: the
    services' deletion protection (prd's), and in every
    environment the database's deletion and drop protection, the file stores'
    force-destroy, the secret containers' deletion protection and the
    Firestore database's delete protection.
  EOT
  type        = bool
  default     = false
}

variable "restore" {
  description = <<-EOT
    Set by a restore run alone, never in terraform.tfvars: the environment's
    database is being replaced or dropped and restored (and in tst its
    file stores replaced with it). The run applies the stack with it, targeted
    at those resources, before its plan, so that the state records their
    protection lifted (the database's deletion and drop protection, the file
    stores' force-destroy) for the one apply that replaces them; the plan
    after it declares the protection again.
  EOT
  type        = bool
  default     = false
}

variable "retired" {
  description = <<-EOT
    The secrets and file stores a release retires, by the variable each is
    declared by (pkg/config/data.go; APP_FILE_STORE or APP_FILE_STORE_<NAME>).
    Anything the running release uses is retired in one release and removed
    in the next, never both at once: the database, the file stores and the
    secret containers hold data, so each is protected from a destroy in every
    environment, and a release whose code stops declaring one is refused at
    its plan (the pull request's check build plans the environment) unless
    the release before it named the variable here. Naming it lifts the
    protection; the next release removes the declaration and the name
    together. Nothing here is read for a pull-request stack, which is torn
    down whole, or for bedrock destroy, which lifts every protection itself.

      retired = ["APP_LEGACY_API_KEY", "APP_FILE_STORE_EXPORTS"]
  EOT
  type        = list(string)
  default     = []
}

variable "members_directory_client_id" {
  description = <<-EOT
    Per environment, the client ID of the members auth's directory registration with Microsoft
    (APP_MEMBERS_DIRECTORY_CLIENT_ID, pkg/config/data.go dataConfig.MembersDirectoryClientID).
    Created by hand in the Microsoft Entra admin center (App registrations), one
    per environment, with the redirect URI this stack outputs as
    members_directory_redirect_url and the front-channel logout URL it outputs as
    members_directory_logout_url.
    The client ID is public; its secret goes into the members-directory-client-secret
    container. Empty until registered.
  EOT
  type        = map(string)
  default = {
    tst = ""
    stg = ""
    prd = ""
  }
}

variable "members_directory_tenant_id" {
  description = "The Microsoft Entra tenant whose accounts the members auth's directory registration admits, by its tenant id, a GUID (APP_MEMBERS_DIRECTORY_TENANT_ID, dataConfig.MembersDirectoryTenantID): the directory (tenant) ID on the app registration's overview page. The session library validates it when the registration is made, at construction, so every data-level process carries it and the first release in an environment waits on it (README, \"Hand steps\")."
  type        = string
  default     = ""

  validation {
    condition     = var.members_directory_tenant_id == "" || can(regex("^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$", var.members_directory_tenant_id))
    error_message = "members_directory_tenant_id is a tenant id: a GUID, or empty before the registration."
  }
}

variable "members_directory_administered_tenants" {
  description = <<-EOT
    The Microsoft Entra tenants the deployment administers, among those the members auth's directory registration
    admits, by tenant id, comma-separated (APP_MEMBERS_DIRECTORY_ADMINISTERED_TENANTS, dataConfig.MembersDirectoryAdministeredTenants):
    a sign-in from one carries the tenant's own second factor, which the deployment
    vouches for (a Conditional Access policy requiring multifactor authentication of
    everyone), and is asked for no sign-in code. Empty when the deployment administers
    none, so every sign-in is asked for the code. The registration reads it when it is
    made, at construction, so every data-level process carries it, and refuses a tenant
    it does not admit.
  EOT
  type        = string
  default     = ""
}

variable "staff_directory_client_id" {
  description = <<-EOT
    Per environment, the client ID of the staff auth's directory registration with Google
    (APP_STAFF_DIRECTORY_CLIENT_ID, pkg/config/data.go dataConfig.StaffDirectoryClientID).
    Created by hand in the environment project's console (APIs & Services >
    Credentials > OAuth client ID, Web application) with the redirect URI this
    stack outputs as staff_directory_redirect_url.
    The client ID is public; its secret goes into the staff-directory-client-secret
    container. Empty until registered.
  EOT
  type        = map(string)
  default = {
    tst = ""
    stg = ""
    prd = ""
  }
}

variable "staff_directory_group_lookup" {
  description = "How far the groups read of the staff auth's directory registration reaches (APP_STAFF_DIRECTORY_GROUP_LOOKUP, dataConfig.StaffDirectoryGroupLookup): direct reads the Google Groups the person is a direct member of; nested climbs from those to the groups they are in, level by level, for a directory that nests its role groups. The read runs with the person's own sign-in token through the Cloud Identity Groups API, which 1-org enables in the environment project."
  type        = string
  default     = "direct"

  validation {
    condition     = contains(["direct", "nested"], var.staff_directory_group_lookup)
    error_message = "staff_directory_group_lookup is direct or nested."
  }
}

variable "staff_directory_group_prefix" {
  description = "Local-part prefix of the Google Groups that carry staff roles through the staff auth's directory registration: <prefix><role>@<domain> assigns <role> (APP_STAFF_DIRECTORY_GROUP_PREFIX, dataConfig.StaffDirectoryGroupPrefix). Required non-empty by the session library at construction, so the migrate command carries it too."
  type        = string
  default     = "staff-"

  validation {
    condition     = length(var.staff_directory_group_prefix) > 0
    error_message = "staff_directory_group_prefix must not be empty: it is the only filter between role groups and the rest of the directory."
  }
}

variable "staff_directory_hosted_domain" {
  description = "Google Workspace domain the logins of the staff auth's directory registration are restricted to (APP_STAFF_DIRECTORY_HOSTED_DOMAIN, dataConfig.StaffDirectoryHostedDomain). Required non-empty by the session library at construction, so the migrate command carries it too."
  type        = string
  default     = "impulseframework.com"
}

variable "staff_directory_administered_workspaces" {
  description = <<-EOT
    The Google Workspaces the deployment administers, among those the staff auth's directory registration
    admits, by domain, comma-separated (APP_STAFF_DIRECTORY_ADMINISTERED_WORKSPACES, dataConfig.StaffDirectoryAdministeredWorkspaces):
    a sign-in from one carries the Workspace's own second factor, which the deployment
    vouches for (2-Step Verification enforced for everyone), and is asked for no sign-in
    code. Empty when the deployment administers none, so every sign-in is asked for the
    code. The registration reads it when it is made, at construction, so every
    data-level process carries it, and refuses a Workspace it does not admit.
  EOT
  type        = string
  default     = ""
}

variable "mail_mailbox" {
  description = "The Google Workspace mailbox the auths' mail is sent as (APP_MAIL_MAILBOX, dataConfig.MailMailbox), through the Workspace SMTP relay with the mailbox's app password (the secret APP_MAIL_APP_PASSWORD): a no-reply address of the organization's domain, once for the organization. Empty until the organization names one: the application then writes its mail to the log and sends nothing (README, \"Hand steps\")."
  type        = string
  default     = ""
}

variable "members_administrator" {
  description = <<-EOT
    Per environment, the sign-in name of the members auth's first administrator
    (APP_MEMBERS_ADMINISTRATOR, pkg/config/data.go dataConfig.MembersAdministrator): the name the
    directory presents, the user principal name on Entra or the email on Google.
    The migrate command seeds the administrator role membership under it while
    the auth's users table holds no rows, so the first person to sign in holds
    the role; empty until named, and the first release in an environment refuses
    to seed without it (README, "Hand steps").
  EOT
  type        = map(string)
  default = {
    tst = ""
    stg = ""
    prd = ""
  }
}

variable "state_bucket" {
  description = "(Required) State bucket 0-bootstrap seeded, read for the upstream layers' outputs. The same name is substituted into the backend block by hand."
  type        = string
}
