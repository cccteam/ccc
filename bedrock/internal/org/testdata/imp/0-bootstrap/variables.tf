variable "billing_account_id" {
  description = "(Required) Billing account ID to link projects to, in the form {billing_account_id} or billingAccounts/{billing_account_id}."
  type        = string
}

variable "boot_layer_roles" {
  description = "Organization-level roles granted to the boot layer identity. The gcloud seed in this layer's README grants the same list; change both together."
  type        = list(string)
  default = [
    "roles/cloudbuild.builds.editor",          # the two triggers that plan and apply the layers (cloud-build.tf)
    "roles/cloudbuild.connectionAdmin",        # the connection and the repository link the triggers watch (cloud-build.tf)
    "roles/iam.organizationRoleAdmin",         # the custom roles this layer creates (project updater, state bucket policy admin)
    "roles/iam.serviceAccountAdmin",           # the org layer identity, the two runner identities, the two plan identities, and the grants on each
    "roles/resourcemanager.folderAdmin",       # the terraform folder
    "roles/resourcemanager.organizationAdmin", # org-level IAM bindings for the org layer identity and the two plan identities
    "roles/resourcemanager.projectCreator",    # the boot project
    "roles/resourcemanager.projectDeleter",    # project lifecycle
    "roles/resourcemanager.projectIamAdmin",   # IAM bindings inside the boot project
    "roles/serviceusage.serviceUsageAdmin",    # API enablement in the boot project
  ]
}

variable "boot_plan_roles" {
  description = "Organization-level roles granted to the plan identities of the two boot layers (0-bootstrap and 1-org, both in the boot project): the reads a plan of either layer needs, at the organization node since the layers declare folders, projects, tags and custom roles across it. A starting set, completed by refusal."
  type        = list(string)
  default = [
    "roles/browser",                         # the organization, its folders and projects
    "roles/essentialcontacts.viewer",        # Essential Contacts per folder
    "roles/iam.securityReviewer",            # every IAM policy the grants are refreshed through, and the custom roles
    "roles/iam.serviceAccountViewer",        # the identities in every project
    "roles/orgpolicy.policyViewer",          # the folder-scoped constraints
    "roles/resourcemanager.tagViewer",       # the public-invoker tag
    "roles/secretmanager.viewer",            # the key container's metadata, never a version's payload
    "roles/serviceusage.serviceUsageViewer", # each project's enabled APIs
  ]
}

variable "boot_project_id" {
  description = "(Required) Project ID of the boot project created during the gcloud seed, including the random suffix chosen for it (e.g. \"imp-boot-gbl-core-a1b2\"). This layer adopts the project rather than creating it, so the ID has to be supplied rather than derived."
  type        = string
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
  default     = "imp-impulse-infrastructure"
}

variable "organization_folder_id" {
  description = "(Required) Numeric ID of the organization's folder created during the gcloud seed, without the folders/ prefix: the folder named after the prefix that holds the terraform folder and, from 1-org, the environment folders. Adopted, not created."
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
  description = "Whether this layer manages the roles/billing.user grants for the two layer identities on the billing account. Changing a billing account's IAM policy needs Billing Account Administrator, which the bootstrap administrator does not hold on the CCC account. While this is false the grants are made by hand by a billing administrator (README, step 2) and stay outside OpenTofu."
  type        = bool
  default     = true
}

variable "org_layer_roles" {
  description = "Organization-level roles granted to the org layer identity. Turning on central_logging in 1-org needs roles/logging.configWriter and roles/storage.admin added here."
  type        = list(string)
  default = [
    "roles/essentialcontacts.admin",         # Essential Contacts per folder
    "roles/iam.organizationRoleAdmin",       # the secretContainerAdmin custom role created by 1-org
    "roles/iam.serviceAccountAdmin",         # per-layer OpenTofu and plan service accounts
    "roles/iam.serviceAccountUser",          # act as those service accounts
    "roles/orgpolicy.policyAdmin",           # folder-scoped org policy constraints
    "roles/resourcemanager.folderAdmin",     # environment folders
    "roles/resourcemanager.lienModifier",    # the lien on every project (1-org projects.tf)
    "roles/resourcemanager.projectCreator",  # environment projects
    "roles/resourcemanager.projectDeleter",  # project lifecycle
    "roles/resourcemanager.projectIamAdmin", # IAM bindings inside those projects
    "roles/resourcemanager.tagAdmin",        # the public-invoker tag and the grants on its value (1-org tags.tf)
    "roles/serviceusage.serviceUsageAdmin",  # API enablement
  ]
}

variable "organization_domain" {
  description = "(Required) Domain name of the GCP Organization (e.g. \"imp.example\")."
  type        = string
}

variable "prefix" {
  description = "Short org-wide prefix used in resource names (e.g. \"imp\")."
  type        = string
  default     = "imp"
}

variable "state_bucket" {
  description = "The seeded state bucket every backend block names (placement.json, stateBucket). This layer grants on it for the two identities it owns (state-bucket.tf); the bucket itself stays outside OpenTofu."
  type        = string
  default     = "imp-boot-gbl-state-a1b2"
}
