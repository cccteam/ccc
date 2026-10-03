terraform {
  # Same bucket as 0-bootstrap, different prefix. A backend block cannot read a
  # variable, so the bucket name is a placeholder: the seed step in
  # 0-bootstrap/README.md creates the bucket, and the name is substituted here
  # once, by hand.
  backend "gcs" {
    bucket = "imp-boot-gbl-state-a1b2"
    prefix = "1-org"
  }
  required_version = ">= 1.11.0"
  required_providers {
    github = {
      source  = "integrations/github"
      version = "~> 6.13"
    }
    google = {
      source  = "hashicorp/google"
      version = "~> 7.0"
    }
    google-beta = {
      source  = "hashicorp/google-beta"
      version = "~> 7.0"
    }
    time = {
      source  = "hashicorp/time"
      version = "~> 0.13"
    }
  }
}

# 0-bootstrap's outputs: the layers workflow's identity pool, the bucket policy
# role and the key container, which this layer binds and grants with
# (workflow.tf).
data "terraform_remote_state" "boot" {
  backend = "gcs"
  config = {
    bucket = var.state_bucket
    prefix = "0-bootstrap"
  }
}

# Every API call is billed and quota-counted against the boot project, so a
# project this layer creates needs no API enabled just to be created and
# populated.
provider "google" {
  user_project_override = true
  billing_project       = var.boot_project_id
}

provider "google-beta" {
  user_project_override = true
  billing_project       = var.boot_project_id
}

# The applications' repositories (github.tf) are configured with GITHUB_TOKEN:
# in the layers workflow an installation token of the infrastructure GitHub
# App, minted in the run (0-bootstrap/github.tf); by hand, the token of an
# owner of the organization (gh auth token). Nothing in this layer holds it.
provider "github" {
  owner = var.github_organization
}

data "google_organization" "this" {
  domain = var.organization_domain
}
