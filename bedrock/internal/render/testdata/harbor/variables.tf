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
    covers (harbor.tst.<domain> would not be). The first one is the canonical
    host: the staff sign-in's redirect URL is built from it
    (https://<host>/api/user/callback, the route pkg/router registers for
    APP_STAFF_OIDC_REDIRECT_URL). Every hostname here is registered with the
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
  description = "Whether Cloud Armor's policy (cloud-armor.tf) is on in each environment, by environment name: \"preview\" evaluates the rules and logs what each would have done, \"enforce\" applies them. An environment left out, or set to \"off\", has no policy; a pull-request stack never has one. Turn an environment to preview first, read its request logs, then enforce."
  type        = map(string)
  default     = {}

  validation {
    condition     = alltrue([for env, mode in var.cloud_armor : contains(["off", "preview", "enforce"], mode)])
    error_message = "Each environment's cloud_armor is \"off\", \"preview\" or \"enforce\"."
  }
}

variable "maintenance" {
  description = "Value of the service's APP_MAINTENANCE variable: empty, the application serving. The pipeline passes 1 to the plan it makes while the application is in maintenance (a restore run), the value its maintenance revision carries, so that the apply leaves the service alone; nothing else sets it."
  type        = string
  default     = ""
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
    yet" for a value an operator has not added. A rotation is a new version
    added by an operator plus a one-line bump here, released like any change.

    A value is a version number. "latest" is allowed only where the map says
    so, and means the environment tracks whatever version is added next; the
    design brief reserves it for secrets whose placement marks them as
    tracking, and bedrock check names each one, per environment.

      secret_versions = {
        tst = { APP_COOKIE_KEY = "1", APP_STAFF_OIDC_CLIENT_SECRET = "1" }
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
        for name, version in versions : can(regex("^([1-9][0-9]*|latest)$", version))
      ]
    ]))
    error_message = "Every secret version is a positive integer or the word latest."
  }
}

variable "staff_oidc_client_id" {
  description = <<-EOT
    Per environment, the OAuth client ID of the application's registration with
    Google (APP_STAFF_OIDC_CLIENT_ID, pkg/config/data.go
    dataConfig.StaffClientID). Created by hand in the environment project's
    console (APIs & Services > Credentials > OAuth client ID, Web application)
    with the redirect URI this stack outputs as staff_oidc_redirect_url. The
    client ID is public; its secret goes into the staff-oidc-client-secret
    container. Empty until registered.
  EOT
  type        = map(string)
  default = {
    tst = ""
    stg = ""
    prd = ""
  }
}

variable "staff_oidc_group_lookup" {
  description = "How far the staff sign-in's groups read reaches (APP_STAFF_OIDC_GROUP_LOOKUP, dataConfig.StaffGroupLookup): direct reads the Google Groups the person is a direct member of; nested climbs from those to the groups they are in, level by level, for a directory that nests its role groups. The read runs with the person's own sign-in token through the Cloud Identity Groups API, which 1-org enables in the environment project."
  type        = string
  default     = "direct"

  validation {
    condition     = contains(["direct", "nested"], var.staff_oidc_group_lookup)
    error_message = "staff_oidc_group_lookup is direct or nested."
  }
}

variable "staff_oidc_group_prefix" {
  description = "Local-part prefix of the Google Groups that carry staff roles: <prefix><role>@<domain> assigns <role> (APP_STAFF_OIDC_GROUP_PREFIX, dataConfig.StaffGroupPrefix). Required non-empty by the session library at construction, so the migrate command carries it too."
  type        = string
  default     = "staff-"

  validation {
    condition     = length(var.staff_oidc_group_prefix) > 0
    error_message = "staff_oidc_group_prefix must not be empty: it is the only filter between role groups and the rest of the directory."
  }
}

variable "staff_oidc_hosted_domain" {
  description = "Google Workspace domain staff logins are restricted to (APP_STAFF_OIDC_HOSTED_DOMAIN, dataConfig.StaffHostedDomain). Required non-empty by the session library at construction, so the migrate command carries it too."
  type        = string
  default     = "impulseframework.com"
}

variable "state_bucket" {
  description = "(Required) State bucket 0-bootstrap seeded, read for the upstream layers' outputs. The same name is substituted into the backend block by hand."
  type        = string
}
