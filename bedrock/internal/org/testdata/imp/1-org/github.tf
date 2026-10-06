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
# runs in (one per environment, production included, since a release is run
# again and a rollback is run there from GitHub, each deploying from the
# default branch alone). No Environment waits for a reviewer: a rollback or a
# rerun waits for its approval in Cloud Build where the environment requires
# one (placement.json's approvals), as a release does, and the approver is
# written into the deployment record. GitHub's required reviewers on an
# Environment of a private repository need GitHub Enterprise, which bedrock
# does not require of an organization.
#
# The required checks are the infrastructure workflow's job, bedrock check,
# which GitHub Actions reports, and the fixed jobs of the application's own
# CI workflow, which impulse renders and GitHub Actions reports by their ids:
# title, go, web, image, secrets and migrations. Every application's workflow
# carries those; the list is read from impulse, so the names have one source.
# The workflow's browser jobs (angular-<workspace>, one per browser
# workspace) are named per application, so the rule does not name them; web,
# the gate over them, fails when any of them did, so a failed browser build
# blocks the merge all the same. A pull request merges once every required
# check passes on its latest commit. The pull-request build, which Cloud
# Build runs on /gcbrun, is not required: it is the developer's preview of a
# pull request in tst, and a pull request merges whether or
# not anyone built it, so release-please's release pull request merges with
# no build and tst's tag build is a release's first build.
# What that gives up: a pull request whose image build or migration is
# broken can merge, and the breakage shows in tst's tag
# build; the fix is another pull request and another release. A renamed job
# is one change here, timed with the release that renames it: requiring both
# names would block every pull request, since each reports one. The rule
# requires impulse's names once every application's workflow reports them:
# an application takes the workflow (impulse render) before this layer is
# applied, or its pull requests block.
#
# The organization's one Actions variable, CI_LARGE_RUNNER, names the larger
# runner every application's CI runs its test legs and image build on (a
# runner label or a runner group, var.ci_large_runner); while the variable is
# absent those jobs run on GitHub's standard runner, as the other jobs do.
#
# GitHub features this uses on a private repository: rulesets with required
# status checks and required reviewers, and deployment branch policies on
# Environments. Nothing here asks GitHub about the organization's plan; a
# feature the plan lacks is refused by GitHub, and that refusal is the
# message.
# ---------------------------------------------------------------------------

# The app that reports the required checks: GitHub Actions, for the
# infrastructure workflow's job and the application's CI jobs alike. It is
# public, so its id is read by slug; the release app, the organization's own,
# is named by its App ID (var.github_release_app_id).
data "github_app" "actions" {
  slug = "github-actions"
}

data "github_team" "infrastructure" {
  count = var.github_infrastructure_team == "" ? 0 : 1

  slug = var.github_infrastructure_team
}

locals {
  # The environments the operations workflow runs in: every one. A restore
  # reaches every environment but production, a rerun of a release every one.
  operations_environments = ["tst", "stg", "prd"]

  # The checks a pull request into the default branch or a hotfix line must
  # pass, each by the name it is reported under and the app that reports it:
  # bedrock check, then the application's CI jobs in the workflow's order. The
  # pull-request build is not among them: it is the preview on /gcbrun (the
  # header says what that gives up).
  required_checks = {
    for app in var.applications : app => [
      {
        context        = "bedrock check"
        integration_id = tonumber(data.github_app.actions.id)
      },
      {
        context        = "title"
        integration_id = tonumber(data.github_app.actions.id)
      },
      {
        context        = "go"
        integration_id = tonumber(data.github_app.actions.id)
      },
      {
        context        = "web"
        integration_id = tonumber(data.github_app.actions.id)
      },
      {
        context        = "image"
        integration_id = tonumber(data.github_app.actions.id)
      },
      {
        context        = "secrets"
        integration_id = tonumber(data.github_app.actions.id)
      },
      {
        context        = "migrations"
        integration_id = tonumber(data.github_app.actions.id)
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
    for pair in setproduct(var.applications, local.operations_environments) :
    "${pair[0]}-${pair[1]}" => { app = pair[0], environment = pair[1] }
  }
}

resource "github_actions_organization_variable" "ci_large_runner" {
  count = var.ci_large_runner == "" ? 0 : 1

  variable_name = "CI_LARGE_RUNNER"
  visibility    = "all"
  value         = var.ci_large_runner
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
    actor_id    = var.github_release_app_id
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
      # A hotfix line is created from a released commit on the default branch, which
      # carries none of the pull-request checks (they ran on the pull request's head,
      # not on its squash commit), so the checks are not enforced on the creation of a
      # branch: without this, `bedrock hotfix start` is refused with "required status
      # checks are expected". Every push to the line after that is a pull request,
      # which the checks gate as on the default branch.
      do_not_enforce_on_create = true

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

resource "github_repository_environment" "operations" {
  for_each = local.repository_environments

  repository  = github_repository.app[each.value.app].name
  environment = each.value.environment

  deployment_branch_policy {
    protected_branches     = false
    custom_branch_policies = true
  }
}

# The Environments were the restorable environments' alone at first; the
# state's instances keep their place under the new name.
moved {
  from = github_repository_environment.restorable
  to   = github_repository_environment.operations
}

resource "github_repository_environment_deployment_policy" "default_branch" {
  for_each = local.repository_environments

  repository     = github_repository.app[each.value.app].name
  environment    = github_repository_environment.operations[each.key].environment
  branch_pattern = var.github_default_branch
}
