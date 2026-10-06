terraform {
  # State lives in a bucket in the boot project. A backend block cannot read a
  # variable, so the bucket name is a placeholder: the seed step in the README
  # creates the bucket, and the name is substituted here once, by hand. The
  # first apply of this layer runs on local state with this block commented
  # out, and the state is moved into the bucket with `tofu init -migrate-state`
  # once the name is in; see the README.
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

# Bootstrap is the one layer applied by a human. It runs locally against the
# the bootstrap administrator's ADC (gcloud auth application-default login) because the identity
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
