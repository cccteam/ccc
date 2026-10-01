variable "environment" {
  description = "(Required) Environment this apply targets: tst, stg, or prd. Selects the 2-env state (2-env/<environment>) and this stack's own prefix (3-app/beacon/<environment>)."
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
    pull-request build into its own prefix (3-app/beacon/tst/pr<N>). Its
    resources carry the short name beacon-pr<N>, which is how the wildcard backend
    2-env creates once in tst picks the Cloud Run service from the hostname
    beacon-pr<N>.impulseframework.dev; it has a database and identities of its own, reads
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
    database user on tst's database, and the migrate job exists but the
    pipeline never runs it, and refuses shared-db when the pull request changes
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

variable "hostnames" {
  description = <<-EOT
    Hostnames the site answers on, per environment, in 2-net's convention:
    app.domain for prd, app-stg.domain and app-tst.domain below it, all one
    label under the apps domain because that is what its wildcard certificate
    covers (beacon.tst.<domain> would not be). The first one is the canonical
    host. Every hostname here is registered with the
    load balancer by an entry in 2-net's hosts; output net_hosts is that entry.
  EOT
  type        = map(list(string))
  default = {
    tst = ["beacon-tst.impulseframework.dev"]
    stg = ["beacon-stg.impulseframework.dev"]
    prd = ["beacon.impulseframework.dev"]
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

variable "maintenance" {
  description = "Value of the service's APP_MAINTENANCE variable: empty, the application serving. The pipeline passes 1 to the plan it makes while the application is in maintenance (a restore run), the value its maintenance revision carries, so that the apply leaves the service alone; nothing else sets it."
  type        = string
  default     = ""
}

variable "substitutions" {
  description = <<-EOT
    Extra trigger substitutions per environment, for the application's hooks
    and its image build: _NAME = value. The pipeline exports every substitution
    of the build to its hooks (infrastructure/hooks/<stage>.sh, when the
    application commits them) and passes the ones declared here to the image
    build as build arguments (ARG _NAME in the Dockerfile). A name starts with
    an underscore and is upper snake case, and may not be one the pipeline's
    contract already carries (the triggers refuse that).

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
}

variable "build_secrets" {
  description = <<-EOT
    Secrets the image build reads, per environment: NAME = the pinned version
    of the container imp-<env>-gbl-beacon-<kebab name>, which this stack creates
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
    tracking, and the check that bedrock adds later names each one.

      secret_versions = {
        tst = { APP_COOKIE_KEY = "1" }
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

variable "state_bucket" {
  description = "(Required) State bucket 0-bootstrap seeded, read for the upstream layers' outputs. The same name is substituted into the backend block by hand."
  type        = string
}
