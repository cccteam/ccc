variable "billing_account_id" {
  description = "(Required) Billing account ID to link projects to, in the form {billing_account_id} or billingAccounts/{billing_account_id}."
  type        = string
}

variable "boot_layer_roles" {
  description = "Roles granted to the boot layer identity on the organization's folder, and nowhere higher. The gcloud seed in this layer's README grants the same list; change both together."
  type        = list(string)
  default = [
    "roles/cloudbuild.builds.editor",        # the two triggers that plan and apply the layers (cloud-build.tf)
    "roles/cloudbuild.connectionAdmin",      # the connection and the repository link the triggers watch (cloud-build.tf)
    "roles/iam.serviceAccountAdmin",         # the dns layer identity, the two runner identities, the two plan identities, and the grants on each
    "roles/resourcemanager.folderAdmin",     # the terraform folder, and the bindings on the organization's folder for the dns layer identity and the two plan identities
    "roles/resourcemanager.projectCreator",  # the boot project
    "roles/resourcemanager.projectDeleter",  # project lifecycle
    "roles/resourcemanager.projectIamAdmin", # IAM bindings inside the boot project
    "roles/serviceusage.serviceUsageAdmin",  # API enablement in the boot project
  ]
}

variable "boot_plan_roles" {
  description = "Roles granted on the organization's folder to the plan identities of the two boot layers (0-bootstrap and 1-dns, both in the boot project): the reads a plan of either layer needs, on the folder since 0-bootstrap declares folders, grants and custom roles under it. A starting set, completed by refusal."
  type        = list(string)
  default = [
    "roles/browser",                         # the organization's folder, its folders and projects
    "roles/essentialcontacts.viewer",        # Essential Contacts per folder
    "roles/iam.securityReviewer",            # every IAM policy the grants are refreshed through, and the custom roles
    "roles/iam.serviceAccountViewer",        # the identities in every project
    "roles/orgpolicy.policyViewer",          # the folder-scoped constraints
    "roles/resourcemanager.tagViewer",       # the public-invoker tag keys in the environment projects
    "roles/secretmanager.viewer",            # a secret's metadata in the boot project (the token secret), never a version's payload
    "roles/serviceusage.serviceUsageViewer", # each project's enabled APIs
  ]
}

variable "boot_project_id" {
  description = "(Required) Project ID of the boot project created during the gcloud seed, including the random suffix chosen for it (e.g. \"imr-boot-gbl-core-a1b2\"). This layer adopts the project rather than creating it, so the ID has to be supplied rather than derived."
  type        = string
}

variable "boot_project_roles" {
  description = "Roles granted to the boot layer identity on the boot project alone: the roles Google binds on a project or at the organization node and never on a folder. The gcloud seed in this layer's README grants the same list; change both together."
  type        = list(string)
  default = [
    "roles/iam.roleAdmin", # the custom roles this layer creates in the boot project (project updater, state bucket policy admin, secret policy admin)
  ]
}

variable "github_default_branch" {
  description = "The default branch of the infrastructure repository: the plan trigger runs on a pull request into it, the apply trigger on a push to it (cloud-build.tf)."
  type        = string
  default     = "master"
}

variable "github_organization" {
  description = "The GitHub organization that holds the infrastructure repository and the application repositories."
  type        = string
  default     = "imp-example"
}

variable "infrastructure_repository" {
  description = "This repository's name under the GitHub organization: the repository the boot project's connection links and the triggers watch (cloud-build.tf)."
  type        = string
  default     = "imr-root-infrastructure"
}

variable "organization_folder_id" {
  description = "(Required) Numeric ID of the organization's folder created during the gcloud seed, without the folders/ prefix: the folder named after the prefix that holds the terraform folder. Adopted, not created."
  type        = string

  validation {
    condition     = can(regex("^[0-9]+$", var.organization_folder_id))
    error_message = "organization_folder_id must be the bare numeric ID, not the folders/{id} resource name."
  }
}

variable "terraform_folder_id" {
  description = "(Required) Numeric ID of the terraform folder created during the gcloud seed, without the \"folders/\" prefix. Adopted by this layer on its first run."
  type        = string

  validation {
    condition     = can(regex("^[0-9]+$", var.terraform_folder_id))
    error_message = "terraform_folder_id must be the bare numeric ID, not the folders/{id} resource name."
  }
}

variable "manage_billing_iam" {
  description = "Whether this layer manages the roles/billing.user grant for the boot identity on the billing account. Changing a billing account's IAM policy needs Billing Account Administrator, which the break-glass account does not hold on the CCC account. While this is false the grant is made by hand by a billing administrator (README, step 2) and stays outside OpenTofu."
  type        = bool
  default     = true
}

variable "organization_domain" {
  description = "(Required) Domain name of the GCP Organization (e.g. \"imp.example\")."
  type        = string
}

variable "prefix" {
  description = "Short org-wide prefix used in resource names (e.g. \"imr\")."
  type        = string
  default     = "imr"
}

variable "state_bucket" {
  description = "The seeded state bucket every backend block names (placement.json, stateBucket). This layer grants on it for the two identities it owns (state-bucket.tf); the bucket itself stays outside OpenTofu."
  type        = string
  default     = "imr-boot-gbl-state-c3d4"
}

variable "destroy" {
  description = <<-EOT
    Set by bedrock org destroy alone, on its own runs: the boot project's deletion
    policy becomes DELETE, no API is disabled on the destroy (the project's
    deletion takes everything in it, and disabling an API a resource outside
    this layer's state uses would refuse), and the two folders' deletion protection
    comes off, all three recorded by an apply before the destroy, since the
    provider reads them from the state. Every other run leaves it false.
  EOT
  type        = bool
  default     = false
}
