# ---------------------------------------------------------------------------
# The applications' GitHub repositories
#
# Everything that configures an application's repository is declared here and
# applied by the operator with their own GitHub sign-in (GITHUB_TOKEN, the
# token of an owner of the organization, from gh auth token) after reading
# the plan, the way the Google side of this layer is applied. bedrock
# commands use the GitHub API only to act at run time (a restore's dispatch,
# a hotfix line's branches and pull requests, the pipeline's talk-back on a
# pull request); none of them configures a repository.
#
# Per application: the repository itself (private; squash the only merge
# method, the squashed commit titled from the pull request; the head branch
# deleted on merge; never destroyed by this layer), three rulesets (release
# tags created, moved or deleted by the release app alone, with no bypass for
# the repository's admins; the default branch and the hotfix lines changed by
# pull request alone, no force push, no deletion, with the branch up to date
# with its base, the required checks passing on its latest commit, squash the
# only merge and, when var.github_infrastructure_team names a team, that
# team's approval of a change to the workflow and Cloud Build files, given
# after the last push), and the GitHub Environments the operations workflow
# runs in (one per environment a restore may be started for, every one but
# production, each deploying from the default branch alone; a reviewer is
# the repository's setting to add).
#
# The required checks are the pull-request build, which Cloud Build reports
# under the trigger's name (set by the application's stack in the first
# environment), and the infrastructure workflow's job, bedrock check, which
# GitHub Actions reports. The pull-request build runs on /gcbrun, so a pull
# request nobody built never merges. A renamed check is one change here,
# timed with the release that renames the trigger: requiring both names would
# block every pull request, since each reports one.
#
# GitHub features this uses on a private repository: rulesets with required
# status checks and required reviewers, and deployment branch policies on
# Environments. Nothing here asks GitHub about the organization's plan; a
# feature the plan lacks is refused by GitHub, and that refusal is the
# message.
# ---------------------------------------------------------------------------

data "github_app" "release" {
  slug = var.github_release_app
}

# The apps that report the required checks: GitHub Actions the infrastructure
# workflow's job, Google Cloud Build the pull-request build.
data "github_app" "actions" {
  slug = "github-actions"
}

data "github_app" "cloud_build" {
  slug = "google-cloud-build"
}

data "github_team" "infrastructure" {
  count = var.github_infrastructure_team == "" ? 0 : 1

  slug = var.github_infrastructure_team
}

locals {
  # The environments a restore may be started for: every one but production.
  restorable_environments = ["tst", "stg"]

  # The checks a pull request into the default branch or a hotfix line must
  # pass, each by the name it is reported under and the app that reports it.
  required_checks = {
    for app in var.applications : app => [
      {
        context        = "bedrock check"
        integration_id = tonumber(data.github_app.actions.id)
      },
      {
        context        = "${var.prefix}-tst-${local.region_code}-${app}-pr"
        integration_id = tonumber(data.github_app.cloud_build.id)
      },
    ]
  }

  # The files that define what the checks run. A change to them needs the
  # infrastructure team's approval when the placement names one.
  check_files = [".github/workflows/**", "cloudbuild*.yaml"]

  # The two branch rulesets of every repository: the default branch, and the
  # hotfix lines (hotfix/<major>.<minor>.x), on which release-please releases
  # a line's patches while the default branch moves on.
  lines = {
    default = { name = "default branch", include = ["~DEFAULT_BRANCH"] }
    hotfix  = { name = "hotfix lines", include = ["refs/heads/hotfix/**"] }
  }
  branch_rulesets = {
    for pair in setproduct(var.applications, keys(local.lines)) :
    "${pair[0]}:${pair[1]}" => { app = pair[0], name = local.lines[pair[1]].name, include = local.lines[pair[1]].include }
  }

  repository_environments = {
    for pair in setproduct(var.applications, local.restorable_environments) :
    "${pair[0]}-${pair[1]}" => { app = pair[0], environment = pair[1] }
  }
}

resource "github_repository" "app" {
  for_each = toset(var.applications)

  name       = each.key
  visibility = "private"

  allow_merge_commit          = false
  allow_rebase_merge          = false
  allow_squash_merge          = true
  squash_merge_commit_title   = "PR_TITLE"
  squash_merge_commit_message = "PR_BODY"
  delete_branch_on_merge      = true
  # The branch must hold every commit of its base before it merges (the
  # rulesets' strict checks); this offers the update on the pull request.
  allow_update_branch = true

  lifecycle {
    # A repository is never deleted by this layer.
    prevent_destroy = true
    # What the repository says about itself and offers (its description,
    # issues, wiki, projects, discussions, its archive flag, its security
    # alerts) is the repository's own setting.
    ignore_changes = [
      description, homepage_url, topics, archived, vulnerability_alerts,
      has_issues, has_projects, has_wiki, has_discussions,
    ]
  }
}

resource "github_repository_ruleset" "release_tags" {
  for_each = toset(var.applications)

  name        = "release tags"
  repository  = github_repository.app[each.key].name
  target      = "tag"
  enforcement = "active"

  bypass_actors {
    actor_id    = tonumber(data.github_app.release.id)
    actor_type  = "Integration"
    bypass_mode = "always"
  }

  conditions {
    ref_name {
      include = ["refs/tags/v*", "refs/tags/*/v*"]
      exclude = []
    }
  }

  rules {
    creation         = true
    update           = true
    deletion         = true
    non_fast_forward = true
  }
}

resource "github_repository_ruleset" "branch" {
  for_each = local.branch_rulesets

  name        = each.value.name
  repository  = github_repository.app[each.value.app].name
  target      = "branch"
  enforcement = "active"

  conditions {
    ref_name {
      include = each.value.include
      exclude = []
    }
  }

  rules {
    deletion         = true
    non_fast_forward = true

    pull_request {
      required_approving_review_count = 0
      require_last_push_approval      = var.github_infrastructure_team != ""
      allowed_merge_methods           = ["squash"]

      dynamic "required_reviewers" {
        for_each = data.github_team.infrastructure
        content {
          file_patterns     = local.check_files
          minimum_approvals = 1
          reviewer {
            type = "Team"
            id   = tonumber(required_reviewers.value.id)
          }
        }
      }
    }

    required_status_checks {
      strict_required_status_checks_policy = true

      dynamic "required_check" {
        for_each = local.required_checks[each.value.app]
        content {
          context        = required_check.value.context
          integration_id = required_check.value.integration_id
        }
      }
    }
  }
}

resource "github_repository_environment" "restorable" {
  for_each = local.repository_environments

  repository  = github_repository.app[each.value.app].name
  environment = each.value.environment

  deployment_branch_policy {
    protected_branches     = false
    custom_branch_policies = true
  }
}

resource "github_repository_environment_deployment_policy" "default_branch" {
  for_each = local.repository_environments

  repository     = github_repository.app[each.value.app].name
  environment    = github_repository_environment.restorable[each.key].environment
  branch_pattern = var.github_default_branch
}
