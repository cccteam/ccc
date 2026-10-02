# ---------------------------------------------------------------------------
# Cloud Build triggers
#
# Two triggers on the repository link 2-env registered under the
# environment's GitHub connection, both running cloudbuild.yaml from the
# repository as the deploy identity:
#
#   version  a tag v<major>.<minor>.<patch>, in every environment; stg and prd
#            wait for an approval in Cloud Build (the design brief's gate for
#            now: "Cloud Build's approval for now").
#   pr       tst only: a pull request against master, run only when a
#            collaborator comments /gcbrun (COMMENTS_ENABLED), so a pull
#            request build is a command, not a side effect of a push.
#
# Both wait on the repository link: until 2-env holds the environment's
# GitHub connection (its two GitHub values set), neither trigger exists and
# a build is submitted by hand with the same substitutions (README). The rest
# of this layer applies either way.
#
# The substitutions are everything the pipeline needs to know about this
# environment that the code does not: the environment, the project, the
# services and the migrate job by region and name, the registry, the records
# bucket. The revision template (variables, secret mounts, identity, scaling)
# is this layer's; a deploy changes the image and its labels, nothing else.
#
# Building as a user-specified service account needs the build logs sent to
# Cloud Logging (options.logging: CLOUD_LOGGING_ONLY in cloudbuild.yaml); the
# deploy identity holds logging.logWriter for it.
#
# Modeled on CCC's reference deployment, with the dynamic
# blocks resolved to the two shapes actually used.
# ---------------------------------------------------------------------------

locals {
  substitutions = {
    _ENV                     = var.environment
    _APP                     = local.app
    _PROJECT                 = local.project_id
    _SERVICES                = join(",", [for code, service in google_cloud_run_v2_service.app : "${service.location}=${service.name}"]) # region=service per region; the pipeline updates each
    _MIGRATE_JOB             = "${google_cloud_run_v2_job.migrate.location}=${google_cloud_run_v2_job.migrate.name}"                     # region=job; the pipeline updates it to the image and runs it
    _REGISTRY                = coalesce(local.registry, "REGISTRY_NOT_REGISTERED_IN_2-SHR")
    _RECORDS_BUCKET          = local.env.records_bucket
    _REPO_CONNECTION_NAME    = coalesce(try(local.env.connection_name, null), "CONNECTION_NOT_AUTHORIZED_IN_2-ENV")          # the pipeline mints a GitHub token from the connection for the tag check and the comment read; a null output is absent from remote state, hence try
    _REPO_NAME               = coalesce(local.env.applications[local.app].repository_name, "REPOSITORY_NOT_LINKED_IN_2-ENV") # null until 2-env holds the connection
    _RELEASE_ACTORS          = "impulseframework-release[bot]"                                                               # the logins whose GitHub Releases the tag check accepts, comma-separated: release-please runs as the release app
    _PREVIOUS_ENV            = local.previous_environment                                                                    # the environment whose live deployment record a release needs first; empty in the first environment
    _PREVIOUS_RECORDS_BUCKET = local.previous_records_bucket                                                                 # that environment's records bucket, which 2-env there lets this deploy identity read
    _ENVIRONMENTS            = join(",", local.environments)                                                                 # the promotion order; a pull-request build plans the stack for each
    _PLAN_IDENTITIES         = local.plan_identities                                                                         # env=identity; the reader a pull-request build plans each environment as
    _APPLY_IDENTITY          = local.identities.apply_identity_email                                                         # the identity a pull-request build applies its stack as and a tag build applies the environment's stack as, impersonated by the deploy identity (2-env grants it in every environment)
    _RESTORE                 = ""                                                                                            # a restore run's instruction (empty, or production-backup): the environment's database is replaced before the release deploys; set by bedrock restore when it runs the trigger, never on a tag's own build, and refused in prd
    _REQUESTER               = ""                                                                                            # who asked for the restore; the record carries it
    # The schema migrations, root-relative: /gcbrun shared-db is refused when a
    # pull request changes anything under it. The repository as GitHub names it
    # (owner/name, from the module path): the sweep asks GitHub about each pull
    # request by it. _SEED says whether the migrate job applies the development
    # seed to this environment's database at a release build: the placement's
    # seed environments, none by default, never production. The pull-request
    # trigger overrides it: a pull request's database is new and always seeded.
    _MIGRATIONS_DIR = "schema/migrations"
    _REPO_FULL_NAME = "impulseframework/beacon"
    _HOSTNAME       = local.hostnames[0]
    _SEED           = contains([], var.environment) ? "true" : "false"
    # The build-time secrets the image build reads (var.build_secrets), as
    # NAME=<secret version resource name>, comma-separated; empty when none.
    _BUILD_SECRETS = join(",", [for name, v in local.build_secrets : "${name}=projects/${local.project_id}/secrets/${local.build_secret_ids[name]}/versions/${v}"])
    # The deployer GitHub App the pipeline talks back on a pull request as (a
    # deployment carrying the environment's URL, a comment, the guard's
    # refusals): its App ID and the pinned secret version of its private key,
    # both from 2-env; empty until set there (the output absent or null, as in
    # an environment the key is not kept in), and then the pipeline talks back
    # through nothing.
    _DEPLOYER_APP_ID     = try(coalesce(tostring(local.env.github_deployer_app_id)), "")
    _DEPLOYER_KEY_SECRET = try(coalesce(local.env.github_deployer_key_secret_version), "")
  }

  # What the placement adds for this environment (var.substitutions): the
  # application's own, for its hooks and its image build.
  custom_substitutions = lookup(var.substitutions, var.environment, {})
  # A name the contract carries may not be redefined; the triggers refuse it.
  redefined_substitutions = setintersection(keys(local.custom_substitutions), keys(local.substitutions))
}

resource "google_cloudbuild_trigger" "version" {
  count = local.identities.repository_id == null || local.is_pr ? 0 : 1

  project  = local.project_id
  location = local.primary_region
  name     = "${local.name}-${local.primary_region_code}-${local.app}-version"

  service_account    = local.identities.deploy_identity_id
  filename           = "cloudbuild.yaml"
  include_build_logs = "INCLUDE_BUILD_LOGS_WITH_STATUS"
  # A tag build has no pull request; the empty value says so explicitly, since
  # the pipeline reads _PR_NUMBER and Cloud Build refuses an unset substitution.
  # The pull-request trigger passes nothing: its event supplies the number.
  substitutions = merge(local.substitutions, local.custom_substitutions, { _PR_NUMBER = "" })

  approval_config {
    approval_required = contains(["stg", "prd"], var.environment)
  }

  repository_event_config {
    repository = local.identities.repository_id

    push {
      tag = "^v\\d+\\.\\d+\\.\\d+$"
    }
  }

  lifecycle {
    precondition {
      condition     = length(local.redefined_substitutions) == 0
      error_message = "var.substitutions redefines a substitution the pipeline's contract carries; rename it: ${join(", ", local.redefined_substitutions)}."
    }
  }
}

resource "google_cloudbuild_trigger" "pr" {
  count = var.environment == "tst" && local.identities.repository_id != null && !local.is_pr ? 1 : 0

  project  = local.project_id
  location = local.primary_region
  name     = "${local.name}-${local.primary_region_code}-${local.app}-pr"

  service_account    = local.identities.deploy_identity_id
  filename           = "cloudbuild.yaml"
  include_build_logs = "INCLUDE_BUILD_LOGS_WITH_STATUS"
  substitutions      = merge(local.substitutions, local.custom_substitutions, { _SEED = "true" })

  approval_config {
    approval_required = false
  }

  repository_event_config {
    repository = local.identities.repository_id

    pull_request {
      # The default branch and the hotfix lines (hotfix/<major>.<minor>.x): a fix
      # on a line gets its pull-request build like any change, and the guard
      # compares its migrations with the line's (_BASE_BRANCH).
      branch          = "^(master|hotfix/[0-9]+\\.[0-9]+\\.x)$"
      comment_control = "COMMENTS_ENABLED"
    }
  }
}

# ---------------------------------------------------------------------------
# The sweep, tst only. A pull request's environment outlives the pull
# request when nobody comments /gcbrun down, and closing or merging one starts
# no build (Cloud Build has no event for it). Every hour Cloud Scheduler runs
# the sweep trigger as the deploy identity: cloudbuild-sweep.yaml in the
# repository lists this application's pull-request services by their
# pull_request label, asks GitHub whether each pull request is closed, and
# destroys the stacks of the closed ones as the apply identity, the way
# /gcbrun down does. The design brief gives the sweep an identity of its own;
# the lab runs it as the deploy identity, which already holds the impersonation
# the pull-request build uses. The scheduler runs the trigger as that identity
# too, which needs Service Account User on the identity itself: 2-env grants it
# in tst (deploy_runs_sweep).
# ---------------------------------------------------------------------------

resource "google_cloudbuild_trigger" "sweep" {
  count = var.environment == "tst" && local.identities.repository_id != null && !local.is_pr ? 1 : 0

  project  = local.project_id
  location = local.primary_region
  name     = "${local.name}-${local.primary_region_code}-${local.app}-sweep"

  service_account = local.identities.deploy_identity_id
  substitutions   = merge(local.substitutions, local.custom_substitutions, { _PR_NUMBER = "" })
  # No include_build_logs: Cloud Build accepts the log link on a GitHub-event
  # trigger only, and this one is run by the scheduler.

  source_to_build {
    repository = local.identities.repository_id
    ref        = "refs/heads/master"
    repo_type  = "GITHUB"
  }

  git_file_source {
    path       = "cloudbuild-sweep.yaml"
    repository = local.identities.repository_id
    revision   = "refs/heads/master"
    repo_type  = "GITHUB"
  }
}

resource "google_cloud_scheduler_job" "sweep" {
  count = length(google_cloudbuild_trigger.sweep)

  project     = local.project_id
  region      = local.primary_region
  name        = "${local.name}-${local.primary_region_code}-${local.app}-sweep"
  description = "Hourly: destroy the environments of beacon's closed pull requests (the sweep trigger)."
  schedule    = "29 * * * *" # the minute is a hash of the application code: many applications' sweeps spread over the hour
  time_zone   = "Etc/UTC"

  http_target {
    http_method = "POST"
    uri         = "https://cloudbuild.googleapis.com/v1/${google_cloudbuild_trigger.sweep[0].id}:run"
    headers     = { "Content-Type" = "application/json" }
    body        = base64encode("{}")

    oauth_token {
      service_account_email = local.identities.deploy_identity_email
      scope                 = "https://www.googleapis.com/auth/cloud-platform"
    }
  }
}
