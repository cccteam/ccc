variable "boot_project_id" {
  description = "(Required) Project ID of the boot project, the output boot_project_id of 0-bootstrap, suffix included: the zones live there, and every API call is billed to it."
  type        = string
}

variable "domains" {
  description = <<-EOT
    The domains this organization holds a zone for, by domain, each with the
    labels of it handed onward: a customer delegated the domain to this
    organization's zone once (at its registrar, or as NS records at the DNS
    provider that serves the domain above it; bedrock domain check prints the
    records), and each application's name under it is one label here, handed
    to the hosting organization that serves the application (its prefix) on the
    four name servers of the application's zone there, as Cloud DNS spells them
    (ns-cloud-a1.googledomains.com.). One zone per domain, one NS record set per
    label, nothing else (dns.tf). Rendered from placement.json (domains) into
    domains.auto.tfvars by bedrock org render; never set by hand.
  EOT
  type = map(object({
    labels = optional(map(object({
      organization = string
      name_servers = list(string)
    })), {})
  }))

  validation {
    condition     = alltrue([for d in keys(var.domains) : can(regex("^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\\.)+[a-z]{2,}$", d))])
    error_message = "Every domain must be a bare lowercase domain such as example.com, without a trailing dot."
  }

  validation {
    condition     = alltrue(flatten([for d in var.domains : [for label in keys(d.labels) : can(regex("^[a-z]([a-z0-9-]{0,52}[a-z0-9])?(\\.[a-z]([a-z0-9-]{0,52}[a-z0-9])?)*$", label))]]))
    error_message = "Every label handed onward is one or more DNS labels joined with dots, each of lowercase letters, digits and hyphens, starting with a letter and ending with a letter or digit."
  }

  validation {
    condition     = alltrue(flatten([for d in var.domains : [for to in d.labels : length(to.name_servers) == 4 && alltrue([for ns in to.name_servers : endswith(ns, ".")])]]))
    error_message = "Every label handed onward names the four name servers of the application's zone in its hosting organization, each with the trailing dot Cloud DNS writes."
  }
}

variable "ci_large_runner" {
  description = "The larger runner the applications' CI runs the jobs its //impulse:ci line chooses on (the two test legs and the image build without a line), in every hosting organization of the GitHub organization: a runner's name, a runner label or a runner group, set as the organization's Actions variable CI_LARGE_RUNNER. Empty for GitHub's standard runner and no variable."
  type        = string
  default     = "ci-large"
}

variable "ci_large_runner_size" {
  description = "The machine size of the larger runner when this layer creates it as a GitHub-hosted runner named var.ci_large_runner (4-core, 8-core and up, as GitHub's machine sizes list them). Empty when var.ci_large_runner names a runner or a runner group the organization already has, or is empty."
  type        = string
  default     = "8-core"
}

variable "ci_large_runner_maximum" {
  description = "How many of the created larger runner run at once, the spend's ceiling (GitHub's maximum_runners); 0 leaves GitHub's default."
  type        = number
  default     = 10
}

variable "github_default_branch" {
  description = "The default branch of this repository, from which alone the apply trigger applies the layers: the branch the ruleset rules (github.tf)."
  type        = string
  default     = "master"
}

variable "github_organization" {
  description = "The GitHub organization that holds this repository."
  type        = string
  default     = "imp-example"
}

variable "infrastructure_repository" {
  description = "This repository's name under the GitHub organization, whose default branch the ruleset rules (github.tf)."
  type        = string
  default     = "imr-root-infrastructure"
}

variable "prefix" {
  description = "Short org-wide prefix used in resource names (e.g. \"imr\")."
  type        = string
  default     = "imr"
}
