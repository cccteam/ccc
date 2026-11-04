terraform {
  # State lives in a bucket in the boot project. A backend block cannot read a
  # variable, so the bucket name is a placeholder: the founding makes the
  # bucket and renders the name here before the first apply, which runs
  # against the bucket from the start; see the README.
  backend "gcs" {
    bucket = "imp-boot-gbl-state-a1b2"
    prefix = "0-bootstrap"
  }
  required_version = ">= 1.11.0"
  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "~> 7.0"
    }
    google-beta = {
      source  = "hashicorp/google-beta"
      version = "~> 7.0"
    }
  }
}

# Bootstrap's first apply is the root organization's founding identity's
# (GOOGLE_IMPERSONATE_SERVICE_ACCOUNT in the founding), because the identity
# every other layer runs as does not exist yet.
provider "google" {
  user_project_override = false
}

provider "google-beta" {
  user_project_override = false
}

data "google_organization" "this" {
  domain = var.organization_domain
}
