# ---------------------------------------------------------------------------
# Cloud Build triggers
#
# Six triggers on the repository link 2-env registered under the
# environment's GitHub connection. Two run cloudbuild.yaml from the
# repository as the deploy identity:
#
#   version   a tag v<major>.<minor>.<patch>, in every environment; stg and prd
#             wait for an approval in Cloud Build.
#   pr        tst only: the preview of a pull request against master, run
#             only when a collaborator comments /gcbrun (COMMENTS_ENABLED), so a
#             pull-request build is a command, not a side effect of a push. It
#             builds the image and deploys it into the pull request's own
#             environment, and applies no Terraform.
#
# Two hold their steps here, in the trigger, and run as the apply identity,
# tst only. A pull request controls cloudbuild.yaml, so what applies
# Terraform for a pull request never runs from that file: the guard on a pull
# request's plan is only real when the pull request cannot edit the steps
# around it.
#
#   check     every push to a pull request against master or a patch line
#             (no comment control: every push): bedrock check, then the stack planned and
#             tested for every environment as that environment's plan identity
#             (the summaries go on the pull request), then the pull request's
#             own stack applied again when it already has a preview, so later
#             pushes keep it current. Its result is the check a merge waits
#             for (1-org's rulesets name it).
#   pr-stack  the same /gcbrun comment as pr: bedrock check, then the pull
#             request's own stack planned, guarded (only the pull request's
#             own resources pass) and applied, or destroyed on /gcbrun down.
#             What it applied is written to the records bucket
#             (<app>/stacks/<env>/pr<N>/<commit>.json), which the preview
#             build reads once its image is built.
#
# One holds its steps here and runs as the release identity, tst only:
#
#   release   every push to master or a patch line: release-please, pinned,
#             as the release GitHub App, whose token the step mints from the
#             key's pinned version in Secret Manager (2-env's output). It opens
#             or updates the release pull request and, on the push that merges
#             it, makes the tag and the GitHub Release; the tag fires version.
#             On master the merge-back guard runs first: no regular
#             release is cut while a patch release is not merged back.
#             The key never reaches GitHub.
#
# The sweep (below) runs cloudbuild-sweep.yaml from the default branch as the
# apply identity, hourly.
#
# The trigger-held steps are this file's text, so a change to them is a
# change to the stack: a release renders it, and it takes effect in an
# environment with that release's stack apply there, one apply behind the
# repository. The bedrock the steps run is never in the text: the first step
# reads the pin (bedrockVersion, bedrockSha256) from
# infrastructure/placement.json in the checkout at run time, so a pull request
# that moves the pin runs the bedrock it pins under the steps of the apply
# before. The rule that follows: a bedrock release runs under the previous
# release's steps, which is why the steps are few, name one command each, and
# the commands say so when the steps they run under are an older bedrock's.
#
# All six wait on the repository link: until 2-env holds the environment's
# GitHub connection (its two GitHub values set, the authorization that comes
# before the first application is registered), no trigger exists, and no
# build runs: every build starts from a trigger, and the pipeline refuses one
# whose connection is empty. The rest of this layer applies either way.
#
# The substitutions are everything the pipeline needs to know about this
# environment that the code does not: the environment, the project, the
# services by region and name, the variables the migrate command runs with,
# the registry, the records bucket. The revision template (variables, secret
# mounts, identity, scaling) is this layer's; a deploy changes the image and
# its labels, nothing else.
#
# Building as a user-specified service account needs the build logs sent to
# Cloud Logging (options.logging: CLOUD_LOGGING_ONLY, in cloudbuild.yaml and
# in the trigger-held builds); the deploy identity holds logging.logWriter
# for it, and the apply identity logging.admin.
#
# Modeled on CCC's reference deployment, with the dynamic
# blocks resolved to the two shapes actually used.
# ---------------------------------------------------------------------------

locals {
  substitutions = {
    _ENV                     = var.environment
    _APP                     = local.app
    _PROJECT                 = local.project_id
    _SERVICES                = join(",", [for code, service in google_cloud_run_v2_service.app : "${service.location}=${service.name}"])                                                                                  # region=service per region; the pipeline updates each
    _MIGRATE_ENV             = jsonencode(local.migrate_env)                                                                                                                                                              # the variables the migrate command runs with on the build worker (locals.tf), as a JSON object; the pipeline reads them back from this output after it applies the stack, so a release that changes them migrates with its own
    _MIGRATE_DATABASES       = jsonencode(local.migrate_databases)                                                                                                                                                        # the databases the migrate command reaches, by resource name, as a JSON list; deploy migrate reads each as the deploy identity until its grants are in effect
    _MIGRATE_SECRETS         = jsonencode({ for key, s in local.mounted_secrets : key => "projects/${local.project_id}/secrets/${local.secret_ids[key]}/versions/${s.version}" if contains(local.migrate_secrets, key) }) # the pinned version of each secret the migrate command reads (locals.tf, migrate_secrets), by variable, as a JSON object; deploy migrate reads each as the deploy identity, which the stack grants accessor on those containers alone
    _JOBS_JOB                = "${google_cloud_run_v2_job.jobs.location}=${google_cloud_run_v2_job.jobs.name}"                                                                                                            # region=job; the pipeline updates it to the image, the application runs it
    _REGISTRY                = coalesce(local.registry, "REGISTRY_NOT_REGISTERED_IN_2-SHR")
    _RECORDS_BUCKET          = local.env.records_bucket
    _REPO_CONNECTION_NAME    = try(coalesce(local.env.connection_name), "")                         # the pipeline mints a GitHub token from the connection for the tag check and the comment read; empty until 2-env holds the connection (a null output is absent from remote state, hence try), when no trigger exists and the pipeline refuses a build
    _REPO_NAME               = try(coalesce(local.env.applications[local.app].repository_name), "") # the repository's link under the connection; empty until 2-env holds it, with the triggers
    _RELEASE_ACTORS          = "impulseframework-release[bot]"                                      # the logins whose GitHub Releases the tag check accepts, comma-separated: release-please runs as the release app
    _PREVIOUS_ENV            = local.previous_environment                                           # the environment whose live deployment record a release needs first; empty in the first environment
    _PREVIOUS_RECORDS_BUCKET = local.previous_records_bucket                                        # that environment's records bucket, which 2-env there lets this deploy identity read
    _ENVIRONMENTS            = join(",", local.environments)                                        # the promotion order; a pull-request build plans the stack for each
    _PLAN_IDENTITIES         = local.plan_identities                                                # env=identity; the reader a pull-request build plans each environment as
    _RECORDS_BUCKETS         = local.records_buckets
    _MIGRATION_DATABASES     = local.migration_databases             # env=database in every other environment; the guard reads each one's migration version on a consolidation
    _APPLY_IDENTITY          = local.identities.apply_identity_email # the identity a pull-request build applies its stack as and a tag build applies the environment's stack as, impersonated by the deploy identity (2-env grants it in every environment)
    _TASKS_QUEUE             = local.tasks_queue                     # the task queue a maintenance step pauses while the database is replaced or migrated and resumes after traffic moves
    _FILE_STORES             = "google_storage_bucket.files[0]"      # the file stores' buckets as this stack addresses them, comma-separated; a restore run in tst replaces them with the database
    _ROLLBACK                = ""                                    # a rollback run (bedrock rollback, on the rollback trigger alone): the release the environment leaves; the earlier release's build runs again with nothing of the database, no migration run and no backup taken
    _REASON                  = ""                                    # why the rollback or the restore was asked for; the first step prints it and the record carries it
    _RESTORE                 = ""                                    # a restore run's instruction: empty or production-backup (the database replaced in place, below production), or a backup's resource name or @<moment> (restored into the database's next generation, the live database kept as the forensic copy; production included)
    _REQUESTER               = ""                                    # who asked for the restore or the migration operation; the record carries it
    _RESTORE_DATABASE        = ""                                    # production's live database for a restore from its backup (read by the operations workflow from production's deployment records), or, for a restore as of a moment, the earlier generation the backup is taken of
    _RESTORE_DATABASE_BACKUP = ""                                    # the backup production's live generation was restored from (its record's rollback.backup), restored while the generation has no backup of its own; empty otherwise
    _MAINTENANCE             = ""                                    # off: the run takes the application out of a maintenance an earlier run left on (traffic back to the revision the maintenance revision displaced, the queue resumed) and does nothing else (bedrock maintenance off)
    _MIGRATE_ACTION          = ""                                    # a migration operation (version, rerun or force) the operations workflow asks the migrate command for (bedrock migration); empty on a tag's own build
    _MIGRATE_TABLE           = ""                                    # the migrations table a force sets: schema, or data; empty means schema
    _MIGRATE_VERSION         = ""                                    # the version a force sets: an integer, or -1 for no version
    # The schema migrations, root-relative: /gcbrun shared-db is refused when a
    # pull request changes anything under it. The repository as GitHub names it
    # (owner/name, from the module path): the sweep asks GitHub about each pull
    # request by it. _SEED is what this stack says about the development seed
    # in this environment: true where the placement's seed list names it (none
    # by default, never production), and always on the pull-request trigger, a
    # pull request's database being new. The pipeline does not act on it: a
    # build reads the seed list from the placement in the checkout it runs, so
    # a release that changes the list seeds with its own, where this value is
    # the last apply's. It stays in the build's substitutions as what the
    # trigger said, and the build's log says when the two differ.
    _MIGRATIONS_DIR = "schema/migrations"
    _REPO_FULL_NAME = "impulseframework/harbor"
    _HOSTNAME       = local.hostnames[0]
    _SEED           = contains([], var.environment) ? "true" : "false"
    # The build-time secrets the image build reads (var.build_secrets), as
    # NAME=<secret version resource name>, comma-separated; empty when none.
    # The pipeline takes the containers from here and the pins from
    # terraform.tfvars in the commit it builds, so a release that moves a pin
    # builds with it; these pins are the last apply's.
    _BUILD_SECRETS = join(",", [for name, v in local.build_secrets : "${name}=projects/${local.project_id}/secrets/${local.build_secret_ids[name]}/versions/${v}"])
    # The deployer GitHub App the pipeline talks back on a pull request as (a
    # deployment carrying the environment's URL, a comment, the guard's
    # refusals): its App ID and the pinned secret version of its private key,
    # both from 2-env; empty until set there (the output absent or null, as in
    # an environment the key is not kept in), and then the pipeline talks back
    # through nothing.
    _DEPLOYER_APP_ID     = try(coalesce(tostring(local.env.github_deployer_app_id)), "")
    _DEPLOYER_KEY_SECRET = try(coalesce(local.env.github_deployer_key_secret_version), "")
    # The build arguments placement.json declares (buildArguments), each a
    # value this stack makes, as _BUILD_ARG_<NAME>: the image build passes
    # every one as --build-arg NAME=value, NAME being the argument the
    # Dockerfile declares with ARG. A pull-request build reads them from the
    # pull request's own stack (its substitutions output) once that stack is
    # applied, so its image carries the pull request's own values. A build
    # argument is part of the layer cache key, so a layer that sees one is
    # built per environment; a build secret never carries such a value.
    _BUILD_ARG_FIREBASE_API_KEY = nonsensitive(google_apikeys_key.firebase.key_string) # firebaseApiKey
    _BUILD_ARG_PROJECT_ID       = local.project_id                                     # projectId
  }

  # What the placement adds for this environment (var.substitutions): the
  # application's own, for its hooks and its image build. The pipeline reads
  # them from terraform.tfvars in the commit it builds, so a release that
  # changes them builds with its own; the triggers carry this apply's copy,
  # which the build's log compares.
  custom_substitutions = lookup(var.substitutions, var.environment, {})
  # A name the contract carries may not be redefined; the triggers refuse it.
  redefined_substitutions = setintersection(keys(local.custom_substitutions), keys(local.substitutions))

  # The apply identity as a trigger names its service account: its full
  # resource name, from the email 2-env outputs (2-env outputs the deploy
  # identity's name, for the triggers it ran, and the apply identity's email).
  apply_identity_id = "projects/${local.project_id}/serviceAccounts/${local.identities.apply_identity_email}"

  # The release identity's full resource name, from 2-env (null outside tst,
  # and on a 2-env before the identity), and the release app's key as 2-env
  # names its pinned version (null until pinned): the release trigger exists
  # once both are there.
  release_identity_id = try(coalesce(local.identities.release_identity_id), null)
  release_key_secret  = try(coalesce(local.env.github_release_key_secret_version), null)

  # The first two steps of the trigger-held jobs. The pinned bedrock is read
  # from infrastructure/placement.json in the checkout when the step runs, never
  # from this text: a release pin (bedrockVersion with bedrockSha256) is
  # downloaded from its GitHub Release and verified against the checksum here;
  # a commit pin (no checksum) is checked with git to be reachable from a
  # branch of the repository (a commits-only clone of its branches, no tags,
  # then merge-base --is-ancestor against each head; a commit no branch
  # reaches is refused, as bedrock upgrade refuses it by hand) and written to
  # a file for the next step, which builds it with go install in the Go image,
  # where Go's checksum database verifies it, and does nothing for a release
  # pin. Every $ is written $$: Cloud Build reads $NAME as a substitution and
  # $$ as a literal $, which the shell then expands.
  fetch_bedrock        = <<-EOT
    set -eo pipefail
    pin=$$(python3 -c 'import json; p = json.load(open("infrastructure/placement.json")); print(p["bedrockVersion"], p.get("bedrockSha256") or "-")')
    set -- $$pin
    version=$$1
    sha256=$$2
    rm -f /builder/home/bedrock /builder/home/bedrock.pin
    if [ "$$sha256" = "-" ]; then
      commit=$$(printf '%s' "$$version" | sed 's/.*-//')
      branches=$$(mktemp -d)
      if ! git clone --quiet --bare --no-tags --filter=tree:0 "https://github.com/cccteam/ccc.git" "$$branches"; then
        echo "could not read the branches of https://github.com/cccteam/ccc.git to check the commit pin $$version against: see git's output above."
        exit 1
      fi
      on=""
      for head in $$(git -C "$$branches" for-each-ref --format='%(refname:short)' refs/heads/); do
        if git -C "$$branches" merge-base --is-ancestor "$$commit" "refs/heads/$$head" 2>/dev/null; then
          on=$$head
          break
        fi
      done
      if [ -z "$$on" ]; then
        echo "bedrock $$version pins commit $$commit, which no branch of https://github.com/cccteam/ccc.git reaches: the triggers build only a commit on a branch of github.com/cccteam/ccc, as bedrock upgrade accepts; move the pin to one, or to a release."
        exit 1
      fi
      echo "$$version" > /builder/home/bedrock.pin
      echo "bedrock $$version is a commit pin on $$on: the next step builds it with go install."
      exit 0
    fi
    curl -fsSL --connect-timeout 10 --max-time 60 --retry 3 -o /builder/home/bedrock "https://github.com/cccteam/ccc/releases/download/bedrock/$$version/bedrock-linux-amd64"
    echo "$$sha256  /builder/home/bedrock" | sha256sum --check -
    chmod 0755 /builder/home/bedrock
    /builder/home/bedrock --version
  EOT
  build_bedrock_commit = <<-EOT
    set -eu
    if [ ! -f /builder/home/bedrock.pin ]; then
      echo "bedrock is a release pin, fetched by the step before: nothing to build."
      exit 0
    fi
    go install -ldflags="-s -w" github.com/cccteam/ccc/bedrock@$$(cat /builder/home/bedrock.pin)
    /builder/home/bedrock --version
  EOT
  go_install_env       = ["GOBIN=/builder/home", "CGO_ENABLED=0", "GOTOOLCHAIN=local", "GOFLAGS=-trimpath", "GOPATH=/tmp/go", "GOCACHE=/tmp/go-build", "GOMODCACHE=/tmp/go-mod", "GOPROXY=https://proxy.golang.org", "GOSUMDB=sum.golang.org", "GONOSUMDB=", "GOPRIVATE=", "GOINSECURE="]
  # The build's own id and place, which the trigger-held commands read their
  # build through the Cloud Build API by, as the pipeline's resolve step does.
  build_env = ["BUILD_ID=$BUILD_ID", "PROJECT_ID=$PROJECT_ID", "LOCATION=$LOCATION"]
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

# ---------------------------------------------------------------------------
# The rollback trigger, in every environment. bedrock rollback runs it, through
# the operations workflow, at the release the environment returns to, with the
# release it leaves (_ROLLBACK), who asked (_REQUESTER) and why (_REASON); the
# release's pipeline then runs that release's build again with nothing of the
# database (no migration, no backup, the generation unchanged), deploys it and
# records it. The database is returned by bedrock restore, in a run of its own
# on the version trigger. Disabled: no push, tag or comment ever starts it, a
# manual run does. Its name says what it is in the console and in the approval
# it waits for where a release waits for one.
# ---------------------------------------------------------------------------

resource "google_cloudbuild_trigger" "rollback" {
  count = local.identities.repository_id == null || local.is_pr ? 0 : 1

  project  = local.project_id
  location = local.primary_region
  name     = "${local.name}-${local.primary_region_code}-${local.app}-rollback"
  disabled = true

  service_account    = local.identities.deploy_identity_id
  filename           = "cloudbuild.yaml"
  include_build_logs = "INCLUDE_BUILD_LOGS_WITH_STATUS"
  substitutions      = merge(local.substitutions, local.custom_substitutions, { _PR_NUMBER = "" })

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

# The preview build: cloudbuild.yaml on the /gcbrun comment, as the deploy
# identity. The pull request's stack is the pr-stack trigger's, below, on the
# same comment, and the environments' plans are the check trigger's: the
# deploy identity may act as no plan identity (2-env grants that to the apply
# identity now). It may still act as the apply identity, since a release's
# tag build applies the environment's stack as it from this same file.
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
      # The default branch and the patch lines (patch/<major>.<minor>.x): a fix
      # on a line gets its pull-request build like any change, and the guard
      # compares its migrations with the line's (_BASE_BRANCH).
      branch          = "^(master|patch/[0-9]+\\.[0-9]+\\.x)$"
      comment_control = "COMMENTS_ENABLED"
    }
  }
}

# ---------------------------------------------------------------------------
# The check-and-plan job, tst only: on every push to a pull request, as the
# apply identity, with its steps held here. Its result is the required check
# the merge waits for, so a pull request merges only when bedrock check
# passes on it and every environment's stack plans and tests clean. When the
# pull request already has a preview, its stack is applied again at the end,
# so the preview's infrastructure follows the pushes (the preview's image
# does not: a /gcbrun comment deploys that).
# ---------------------------------------------------------------------------

resource "google_cloudbuild_trigger" "check" {
  count = var.environment == "tst" && local.identities.repository_id != null && !local.is_pr ? 1 : 0

  project  = local.project_id
  location = local.primary_region
  name     = "${local.name}-${local.primary_region_code}-${local.app}-check"

  service_account    = local.apply_identity_id
  include_build_logs = "INCLUDE_BUILD_LOGS_WITH_STATUS"
  substitutions      = merge(local.substitutions, local.custom_substitutions, { _SEED = "true" })

  approval_config {
    approval_required = false
  }

  repository_event_config {
    repository = local.identities.repository_id

    # Comment control is left unset, which is "comments disabled": the API stores
    # that value as unset, and a render that named it would show the trigger
    # changing in place on every plan.
    pull_request {
      branch = "^(master|patch/[0-9]+\\.[0-9]+\\.x)$"
    }
  }

  build {
    timeout = "6600s"

    step {
      id         = "FetchBedrock"
      name       = "gcr.io/cloud-builders/gcloud"
      entrypoint = "bash"
      args       = ["-c", local.fetch_bedrock]
      timeout    = "300s"
    }

    step {
      id         = "BuildBedrockCommit"
      name       = "cgr.dev/chainguard/go@sha256:2c20827746d752c1f18989847956637a0dc979c42507f4059e1d9559ee7733af"
      entrypoint = "sh"
      args       = ["-c", local.build_bedrock_commit]
      env        = local.go_install_env
      timeout    = "600s"
    }

    # The committed stack and pipeline against the code, in the checkout.
    step {
      id         = "CheckStack"
      name       = "gcr.io/cloud-builders/gcloud"
      entrypoint = "/builder/home/bedrock"
      args       = ["check"]
      timeout    = "600s"
    }

    # The stack planned and tested for every environment as its plan identity
    # (the apply identity may act as each), the summaries on the pull request,
    # with the patch release and window previews.
    step {
      id         = "PlanEnvironments"
      name       = "ghcr.io/opentofu/opentofu:1.12.6"
      entrypoint = "/builder/home/bedrock"
      args       = ["deploy", "plan-environments"]
      env        = local.build_env
      timeout    = "1800s"
    }

    # The pull request's own stack applied again when it has a preview.
    step {
      id         = "RefreshPullRequestStack"
      name       = "ghcr.io/opentofu/opentofu:1.12.6"
      entrypoint = "/builder/home/bedrock"
      args       = ["deploy", "pr-stack", "--if-exists"]
      env        = local.build_env
      timeout    = "2700s"
    }

    options {
      logging             = "CLOUD_LOGGING_ONLY"
      substitution_option = "ALLOW_LOOSE"
    }
  }

  lifecycle {
    precondition {
      condition     = length(local.redefined_substitutions) == 0
      error_message = "var.substitutions redefines a substitution the pipeline's contract carries; rename it: ${join(", ", local.redefined_substitutions)}."
    }
  }
}

# ---------------------------------------------------------------------------
# The pull-request stack job, tst only: on the same /gcbrun comment as the
# preview, as the apply identity, with its steps held here. It plans the pull
# request's own stack, lets only the pull request's own resources through
# (the guard), applies exactly that plan, and writes what it applied for the
# preview build to read; on /gcbrun down it destroys the stack and writes
# that. The preview build (the pr trigger) runs beside it and waits for the
# object where it needs the stack.
# ---------------------------------------------------------------------------

resource "google_cloudbuild_trigger" "pr_stack" {
  count = var.environment == "tst" && local.identities.repository_id != null && !local.is_pr ? 1 : 0

  project  = local.project_id
  location = local.primary_region
  name     = "${local.name}-${local.primary_region_code}-${local.app}-pr-stack"

  service_account    = local.apply_identity_id
  include_build_logs = "INCLUDE_BUILD_LOGS_WITH_STATUS"
  substitutions      = merge(local.substitutions, local.custom_substitutions, { _SEED = "true" })

  approval_config {
    approval_required = false
  }

  repository_event_config {
    repository = local.identities.repository_id

    pull_request {
      branch          = "^(master|patch/[0-9]+\\.[0-9]+\\.x)$"
      comment_control = "COMMENTS_ENABLED"
    }
  }

  build {
    timeout = "4800s"

    step {
      id         = "FetchBedrock"
      name       = "gcr.io/cloud-builders/gcloud"
      entrypoint = "bash"
      args       = ["-c", local.fetch_bedrock]
      timeout    = "300s"
    }

    step {
      id         = "BuildBedrockCommit"
      name       = "cgr.dev/chainguard/go@sha256:2c20827746d752c1f18989847956637a0dc979c42507f4059e1d9559ee7733af"
      entrypoint = "sh"
      args       = ["-c", local.build_bedrock_commit]
      env        = local.go_install_env
      timeout    = "600s"
    }

    step {
      id         = "CheckStack"
      name       = "gcr.io/cloud-builders/gcloud"
      entrypoint = "/builder/home/bedrock"
      args       = ["check"]
      timeout    = "600s"
    }

    # The pull request's stack: plan, guard, apply (a destroy on /gcbrun down),
    # and the outputs object for the preview build.
    step {
      id         = "ApplyPullRequestStack"
      name       = "ghcr.io/opentofu/opentofu:1.12.6"
      entrypoint = "/builder/home/bedrock"
      args       = ["deploy", "pr-stack"]
      env        = local.build_env
      timeout    = "2700s"
    }

    options {
      logging             = "CLOUD_LOGGING_ONLY"
      substitution_option = "ALLOW_LOOSE"
    }
  }

  lifecycle {
    precondition {
      condition     = length(local.redefined_substitutions) == 0
      error_message = "var.substitutions redefines a substitution the pipeline's contract carries; rename it: ${join(", ", local.redefined_substitutions)}."
    }
  }
}

# ---------------------------------------------------------------------------
# The release job, tst only: on every push to master or a patch release
# line, as the release identity (2-env makes it in tst alone), with its
# steps held here. release-please, pinned (bedrock deploy release), as the
# release GitHub App: it makes the tag and the GitHub Release of a release
# pull request the push merged, then opens or updates the release pull
# request from the commits since the last release. The app's key never leaves
# Secret Manager: the step mints the app's installation token from the pinned
# version 2-env names and hands it to release-please in the process's
# environment. The trigger exists once the key is pinned (bedrock secret pin
# github-release-key, in the organization's repository); until then a push
# cuts nothing.
# ---------------------------------------------------------------------------

resource "google_cloudbuild_trigger" "release" {
  count = var.environment == "tst" && local.identities.repository_id != null && !local.is_pr && local.release_identity_id != null && local.release_key_secret != null ? 1 : 0

  project  = local.project_id
  location = local.primary_region
  name     = "${local.name}-${local.primary_region_code}-${local.app}-release"

  service_account    = local.release_identity_id
  include_build_logs = "INCLUDE_BUILD_LOGS_WITH_STATUS"
  substitutions = {
    _RELEASE_APP_ID     = tostring(local.env.github_release_app_id)
    _RELEASE_KEY_SECRET = local.release_key_secret
    _REPO_FULL_NAME     = "impulseframework/harbor"
  }

  approval_config {
    approval_required = false
  }

  repository_event_config {
    repository = local.identities.repository_id

    push {
      branch = "^(master|patch/[0-9]+\\.[0-9]+\\.x)$"
    }
  }

  build {
    timeout = "2400s"

    step {
      id         = "FetchBedrock"
      name       = "gcr.io/cloud-builders/gcloud"
      entrypoint = "bash"
      args       = ["-c", local.fetch_bedrock]
      timeout    = "300s"
    }

    step {
      id         = "BuildBedrockCommit"
      name       = "cgr.dev/chainguard/go@sha256:2c20827746d752c1f18989847956637a0dc979c42507f4059e1d9559ee7733af"
      entrypoint = "sh"
      args       = ["-c", local.build_bedrock_commit]
      env        = local.go_install_env
      timeout    = "600s"
    }

    # release-please through npx, in the Node image, with the token the command
    # mints from the key: the step's environment names the app, the key's
    # version, the repository and the pushed branch, and nothing else.
    step {
      id         = "Release"
      name       = "docker.io/library/node@sha256:0a7108bf6c7bf5de370ffb1a3ed6be93d405b43ff159f681a8d18c0e2bc2e402"
      entrypoint = "/builder/home/bedrock"
      args       = ["deploy", "release"]
      env        = ["RELEASE_APP_ID=$_RELEASE_APP_ID", "RELEASE_KEY_SECRET=$_RELEASE_KEY_SECRET", "REPO_FULL_NAME=$_REPO_FULL_NAME", "BRANCH_NAME=$BRANCH_NAME"]
      timeout    = "900s"
    }

    options {
      logging             = "CLOUD_LOGGING_ONLY"
      substitution_option = "ALLOW_LOOSE"
    }
  }
}

# ---------------------------------------------------------------------------
# The sweep, tst only. A pull request's environment outlives the pull
# request when nobody comments /gcbrun down, and closing or merging one starts
# no build (Cloud Build has no event for it). Every hour Cloud Scheduler runs
# the sweep trigger as the apply identity: cloudbuild-sweep.yaml on the
# default branch lists this application's pull-request services by their
# pull_request label, asks GitHub whether each pull request is closed, and
# destroys the stacks of the closed ones from their own state prefixes, the
# way /gcbrun down does. The scheduler calls the trigger as the apply identity
# too, which runs a trigger whose builds run as itself through the roles 2-env
# gives it on the project (builds editor, Service Account User).
# ---------------------------------------------------------------------------

resource "google_cloudbuild_trigger" "sweep" {
  count = var.environment == "tst" && local.identities.repository_id != null && !local.is_pr ? 1 : 0

  project  = local.project_id
  location = local.primary_region
  name     = "${local.name}-${local.primary_region_code}-${local.app}-sweep"

  service_account = local.apply_identity_id
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
  description = "Hourly: destroy the environments of harbor's closed pull requests (the sweep trigger)."
  schedule    = "19 * * * *" # the minute is a hash of the application code: many applications' sweeps spread over the hour
  time_zone   = "Etc/UTC"

  http_target {
    http_method = "POST"
    uri         = "https://cloudbuild.googleapis.com/v1/${google_cloudbuild_trigger.sweep[0].id}:run"
    headers     = { "Content-Type" = "application/json" }
    body        = base64encode("{}")

    oauth_token {
      service_account_email = local.identities.apply_identity_email
      scope                 = "https://www.googleapis.com/auth/cloud-platform"
    }
  }
}
