variable "applications" {
  description = <<-EOT
    Applications that get a Docker repository in the shared project, by short
    name. Registering an application here is the provisioning step: add its
    name, re-run this layer, and once its deploy identities exist add them to
    var.pushers. The repository is {prefix}-shr-{region}-{name}, one per
    application so that a deploy identity's writer grant is bounded to its own
    images.
  EOT
  type        = list(string)
  default     = ["harbor", "beacon"]

  validation {
    condition     = alltrue([for a in var.applications : can(regex("^[a-z][a-z0-9-]{0,30}$", a))])
    error_message = "Application names must be 1-31 lowercase alphanumeric characters or hyphens, starting with a letter; they are embedded in repository IDs."
  }

  validation {
    condition     = length(distinct(var.applications)) == length(var.applications)
    error_message = "Each application may appear only once."
  }
}

variable "cleanup_dry_run" {
  description = "Whether the cleanup policies only log what they would delete. Set true to watch a policy before it deletes anything, for instance after changing a retention."
  type        = bool
  default     = false
}

variable "keep_tagged_versions" {
  description = "How many of the newest versions of each image are exempt from every delete policy. This is the floor a rollback can count on; it only bites once tagged_retention_days is set, because untagged versions are the only ones deleted by default."
  type        = number
  default     = 10

  validation {
    condition     = var.keep_tagged_versions >= 1
    error_message = "keep_tagged_versions must be at least 1."
  }
}

variable "pull_environments" {
  description = "Environment codes (keys of 1-org's project_numbers) whose Cloud Run service agent may pull from every repository. Every environment that runs application images belongs here."
  type        = list(string)
  default     = ["tst", "stg", "prd"]
}

variable "pushers" {
  description = <<-EOT
    IAM members allowed to push to an application's repository
    (roles/artifactregistry.writer), keyed by application name. Meant for the
    application's deploy identity in each environment project. Those service
    accounts are created by the environment layers, and a member has to exist
    before it can be bound, so an application's entry is added here after the
    environment layers have run for it. Pulls need no entry: every pull
    environment's Cloud Run service agent is granted reader from remote state.

    Example:
      {
        harbor = [
          "serviceAccount:imp-tst-gbl-harbor-deploy@imp-tst-gbl-core-a1b2.iam.gserviceaccount.com",
          "serviceAccount:imp-stg-gbl-harbor-deploy@imp-stg-gbl-core-a1b2.iam.gserviceaccount.com",
        ]
      }
  EOT
  type        = map(list(string))
  default     = {}

  validation {
    condition     = alltrue([for app in keys(var.pushers) : contains(var.applications, app)])
    error_message = "Every key of pushers must be an entry of applications."
  }

  validation {
    condition     = alltrue(flatten([for app, members in var.pushers : [for m in members : can(regex("^(serviceAccount|user|group|principal|principalSet):", m))]]))
    error_message = "Every pusher must be a full IAM member, such as serviceAccount:name@project.iam.gserviceaccount.com."
  }
}

variable "pullers" {
  description = <<-EOT
    IAM members allowed to read an application's repository
    (roles/artifactregistry.reader), keyed by application name, beside the
    Cloud Run service agents this layer grants on its own. Meant for the
    application's apply identity in each environment project: Cloud Run checks
    that the principal creating a revision can read the revision's image, and a
    revision that comes from a placement change (a variable, a secret pin, a
    pull request switching databases) is created by the apply identity, with
    the image the last deploy left on the service. Same rule as pushers: the
    member must exist, so an application's entry is added after the
    environment layers have run for it.

    Example:
      {
        harbor = [
          "serviceAccount:imp-tst-gbl-harbor-tofu@imp-tst-gbl-core-a1b2.iam.gserviceaccount.com",
        ]
      }
  EOT
  type        = map(list(string))
  default     = {}

  validation {
    condition     = alltrue([for app in keys(var.pullers) : contains(var.applications, app)])
    error_message = "Every key of pullers must be an entry of applications."
  }

  validation {
    condition     = alltrue(flatten([for app, members in var.pullers : [for m in members : can(regex("^(serviceAccount|user|group|principal|principalSet):", m))]]))
    error_message = "Every puller must be a full IAM member, such as serviceAccount:name@project.iam.gserviceaccount.com."
  }
}

variable "tagged_retention_days" {
  description = "When set, tagged versions older than this many days are deleted, except the newest keep_tagged_versions of each image. Unset by default: a tag is a release, and a release stays until someone decides how long releases are kept."
  type        = number
  default     = null

  validation {
    condition     = var.tagged_retention_days == null || try(var.tagged_retention_days >= 1, false)
    error_message = "tagged_retention_days must be at least 1 when set."
  }
}

variable "untagged_retention_days" {
  description = "Untagged versions older than this many days are deleted. Untagged versions are what a push leaves behind (superseded manifests, layers referenced by nothing); nothing deploys them."
  type        = number
  default     = 7

  validation {
    condition     = var.untagged_retention_days >= 1
    error_message = "untagged_retention_days must be at least 1."
  }
}
