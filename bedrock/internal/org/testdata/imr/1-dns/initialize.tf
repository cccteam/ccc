terraform {
  # Same bucket as 0-bootstrap, different prefix. A backend block cannot read a
  # variable, so the bucket name is a placeholder: the seed step in
  # 0-bootstrap/README.md creates the bucket, and the name is substituted here
  # once, by hand.
  backend "gcs" {
    bucket = "imr-boot-gbl-state-c3d4"
    prefix = "1-dns"
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
  }
}

# The zones live in the boot project, and every API call is billed and
# quota-counted against it, as every layer's are.
provider "google" {
  project               = var.boot_project_id
  user_project_override = true
  billing_project       = var.boot_project_id
}

# This repository's ruleset (github.tf) is configured with GITHUB_TOKEN: in a
# run of the triggers an installation token of the infrastructure GitHub App,
# minted in the run from the key the boot project holds (0-bootstrap/github.tf);
# by hand, the token of an owner of the organization (gh auth token). Nothing in
# this layer holds it.
provider "github" {
  owner = var.github_organization
}
