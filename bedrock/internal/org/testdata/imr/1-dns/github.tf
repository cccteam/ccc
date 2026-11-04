# ---------------------------------------------------------------------------
# This repository's default branch, and the GitHub organization's own settings
#
# What this layer configures on GitHub, with the GitHub provider: the rule on
# this repository's default branch, from which alone the apply trigger applies
# the layers (changed by pull request alone, no force push, no deletion,
# local.approving_reviews approving reviews, squash the only merge, and the
# plan trigger's build (0-bootstrap/cloud-build.tf) passing on the latest
# commit with the branch up to date with its base; the repository exists
# before 0-bootstrap and is adopted here, never destroyed; 1-org rules a
# hosting organization's repository the same way), and the GitHub
# organization's one setting of its own, the larger runner the applications'
# CI runs on, declared here once for every hosting organization in the GitHub
# organization (below, "The larger runner").
#
# The approving reviews are placement.json's approvingReviews: one unless it
# says otherwise, 0 for none (a writer merges alone once the checks pass).
# While the count is above 0, an approval given before the last push is
# dismissed and one given after it is needed. The count is rendered into this
# file, never read from terraform.tfvars: this file is bedrock's, and bedrock
# org check compares it with the render, so lowering the count takes a merge
# that met the count in force.
#
# In a run of the triggers the provider's token is an installation token of the
# infrastructure GitHub App, minted in the run from its key in the boot project
# (0-bootstrap/README.md, "The infrastructure GitHub App"); a person applying
# by hand uses an organization owner's.
# ---------------------------------------------------------------------------

# Google's Cloud Build GitHub App reports the plan trigger's build, the
# required check; it is public, so its id is read by slug.
data "github_app" "cloud_build" {
  slug = "google-cloud-build"
}

locals {
  approving_reviews = 1

  # The context the Cloud Build app reports the plan trigger's build under: the
  # trigger's name as 0-bootstrap/cloud-build.tf gives it (imr-boot-uc1-layers-plan;
  # a renamed trigger is a change in both files), then the boot project's id in
  # parentheses. The form is the app's, read off the checks it reports.
  layers_plan_check = "${var.prefix}-boot-uc1-layers-plan (${var.boot_project_id})"
}

# This repository, adopted so that its merge settings are what the triggers
# expect: squash the only merge, the squashed commit titled from the pull
# request, the head branch deleted on merge, and the update of a branch
# offered on the pull request, which the ruleset's strict checks need. It is
# never deleted by this layer, and what it says about itself stays its own.
import {
  id = var.infrastructure_repository
  to = github_repository.infrastructure
}

resource "github_repository" "infrastructure" {
  name       = var.infrastructure_repository
  visibility = "private"

  allow_merge_commit          = false
  allow_rebase_merge          = false
  allow_squash_merge          = true
  squash_merge_commit_title   = "PR_TITLE"
  squash_merge_commit_message = "PR_BODY"
  delete_branch_on_merge      = true
  allow_update_branch         = true

  lifecycle {
    prevent_destroy = true
    ignore_changes = [
      description, homepage_url, topics, archived, vulnerability_alerts,
      has_issues, has_projects, has_wiki, has_discussions,
    ]
  }
}

# The default branch's ruleset: the review settings above and the plan
# trigger's build as the required check, which Google's Cloud Build GitHub App
# reports under the trigger's name and the boot project's id. The apply
# trigger's build is not a check: it runs after the merge.
resource "github_repository_ruleset" "infrastructure" {
  name        = "default branch"
  repository  = github_repository.infrastructure.name
  target      = "branch"
  enforcement = "active"

  conditions {
    ref_name {
      include = ["~DEFAULT_BRANCH"]
      exclude = []
    }
  }

  rules {
    deletion         = true
    non_fast_forward = true

    pull_request {
      required_approving_review_count = local.approving_reviews
      require_last_push_approval      = local.approving_reviews > 0
      dismiss_stale_reviews_on_push   = local.approving_reviews > 0
      allowed_merge_methods           = ["squash"]
    }

    required_status_checks {
      strict_required_status_checks_policy = true

      required_check {
        context        = local.layers_plan_check
        integration_id = tonumber(data.github_app.cloud_build.id)
      }
    }
  }
}

# ---------------------------------------------------------------------------
# The larger runner
#
# The organization's one Actions variable, CI_LARGE_RUNNER, names the larger
# runner every application's CI, in every hosting organization of this GitHub
# organization, runs the jobs its //impulse:ci line chooses on (the two test
# legs and the image build without a line): a runner's name, a runner label
# or a runner group, var.ci_large_runner. While the variable is absent those
# jobs run on GitHub's standard runner, as the other jobs do. When
# var.ci_large_runner_size names a machine size, this layer creates the
# runner itself: a GitHub-hosted runner of that name and size on GitHub's
# current Ubuntu, in a runner group open to the organization's repositories
# (a hosted runner belongs to one group, and the application repositories
# are the hosting organizations' to make, so the group names none; a
# workflow bills minutes on the runner only by naming it), at most
# var.ci_large_runner_maximum running at once (the spend's ceiling). GitHub
# bills a larger runner per minute by its size, never from the plan's
# included minutes, and offers them on the Team and Enterprise plans; the
# infrastructure GitHub App creates it through its Self-hosted runners
# organization permission and sets the variable through its Variables
# permission (0-bootstrap/README.md).
# ---------------------------------------------------------------------------

resource "github_actions_organization_variable" "ci_large_runner" {
  count = var.ci_large_runner == "" ? 0 : 1

  variable_name = "CI_LARGE_RUNNER"
  visibility    = "all"
  value         = var.ci_large_runner
}

resource "github_actions_runner_group" "applications" {
  count = var.ci_large_runner_size == "" ? 0 : 1

  name       = "applications"
  visibility = "all"
}

# The larger runner this layer creates, named as the variable names it: GitHub's
# current Ubuntu (image 2306, Ubuntu Latest, what ubuntu-latest names on the
# standard runner) at the placement's size. The size changes in place; the
# image cannot, and a change to it is a new runner.
resource "github_actions_hosted_runner" "ci_large" {
  count = var.ci_large_runner_size == "" ? 0 : 1

  name = var.ci_large_runner
  image {
    id     = "2306"
    source = "github"
  }
  size            = var.ci_large_runner_size
  runner_group_id = github_actions_runner_group.applications[0].id
  maximum_runners = var.ci_large_runner_maximum == 0 ? null : var.ci_large_runner_maximum
}
