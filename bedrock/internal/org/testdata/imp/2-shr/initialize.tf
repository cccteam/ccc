terraform {
  # Same bucket as the foundation layers, prefix = this directory. A backend
  # block cannot read a variable, so the bucket name is a placeholder that is
  # substituted once, by hand, with the name the seed step created (see
  # 0-bootstrap/README.md). The remote state block below carries the same
  # placeholder; one substitution covers both.
  backend "gcs" {
    bucket = "imp-boot-gbl-state-a1b2"
    prefix = "2-shr"
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

# Everything this layer needs to know about the organization comes from 1-org:
# the shr project, the environment projects' numbers, the boot project, the
# region, the prefix. Reading them here rather than restating them in
# terraform.tfvars means a project added in 1-org shows up on the next plan of
# this layer, and no ID is typed twice.
data "terraform_remote_state" "org" {
  backend = "gcs"
  config = {
    bucket = "imp-boot-gbl-state-a1b2"
    prefix = "1-org"
  }
}

# Every API call is billed and quota-counted against the boot project, the
# same arrangement as 1-org; the layer identity holds serviceUsageConsumer
# there for exactly this. The default project is the one this layer manages,
# and every resource names it explicitly anyway.
provider "google" {
  project               = local.project_id
  region                = local.region
  user_project_override = true
  billing_project       = local.org.boot_project_id
}

provider "google-beta" {
  project               = local.project_id
  region                = local.region
  user_project_override = true
  billing_project       = local.org.boot_project_id
}
