# ---------------------------------------------------------------------------
# The Cloud Build triggers that plan and apply the layers
#
# Every layer of this repository is planned on a pull request and applied on
# a push to master by two Cloud Build triggers in the boot
# project, whose steps are declared here and nowhere a pull request can
# change them: the steps live in the trigger, not in the repository. A run
# fetches the bedrock placement.json pins (its first two steps, which read
# the pin at run time), checks the committed layers against it (bedrock org
# check, which refuses a .tf file no render produces) and then plans or
# applies (bedrock org plan, bedrock org apply) as the trigger's runner
# identity, which impersonates each layer's own identity for that layer's
# tofu: the planner the plan identities, the applier the apply identities.
# Nothing a pull request carries runs with the runner's credentials before
# the check has passed.
#
# The steps lag one apply behind the repository: a change to them is
# rendered by the new bedrock, planned and applied by a run on the old steps,
# and the next run is the first on the new ones. The pin is read at run
# time, so a pull request that moves it already runs the new bedrock under
# the old steps; the one rule is that a bedrock release works under the
# previous release's steps. Both triggers pass _BEDROCK_STEPS, the shape of
# these steps and the kind of pin they were rendered for, and the commands say
# when they run under another. The one move the lag does not cover is a pin
# that changes kind: this layer is applied by hand, never by a trigger, and
# the release shape's fetch step refuses a commit pin, since no step follows
# to build it. bedrock upgrade applies this layer itself when it moves the
# pin across kinds, after yes typed back, before the pull request is opened.
#
# The connection and the repository link exist once placement.json records
# the Cloud Build GitHub authorization (cloudBuildInstallationId,
# cloudBuildTokenSecretVersion; README, "The Cloud Build GitHub
# authorization"): the browser step, in this project's Cloud Build console,
# before this layer's first apply. Until then the rest of the layer applies
# and nothing runs. The token secret the browser step writes lives in this
# project; the environment projects' Cloud Build service agents read it for
# their 2-env connections, granted here by the numbers placement.json
# records (projectNumbers).
# ---------------------------------------------------------------------------

locals {
  region      = "us-central1"
  region_code = "uc1"

  # The Cloud Build GitHub App's installation on the organization and the
  # token secret's version, from placement.json; null until recorded.
  cloud_build_installation_id      = 12345678
  cloud_build_token_secret_version = "projects/imp-boot-gbl-core-a1b2/locations/us-central1/secrets/github-github-oauthtoken-abcdef/versions/1"
  connected                        = local.cloud_build_installation_id != null && local.cloud_build_token_secret_version != null

  # The token secret's resource name without its version, in either of the
  # two shapes Secret Manager uses: the console's host-connection flow writes
  # a regional secret in the connection's region (projects/P/locations/L/
  # secrets/N), whose IAM resource is the regional one; a global secret
  # (projects/P/secrets/N) added by hand works the same way.
  token_parts    = local.connected ? split("/", local.cloud_build_token_secret_version) : []
  token_regional = length(local.token_parts) == 8
  token_project  = local.connected ? local.token_parts[1] : null
  token_location = local.token_regional ? local.token_parts[3] : null
  token_secret   = local.connected ? local.token_parts[local.token_regional ? 5 : 3] : null

  # The Cloud Build service agents that read the token: this project's, for
  # the triggers' connection, and each environment project's placement.json
  # records a number for (projectNumbers), for the environment's 2-env
  # connection. A project not recorded yet is granted once it is.
  environment_project_numbers = {
    tst = "100000000002"
    stg = "100000000003"
    prd = "100000000004"
  }
  token_readers = local.connected ? merge(
    { boot = module.boot_project.project_number },
    local.environment_project_numbers,
  ) : {}

  # The two runner identities, with the keys each reads: the planner posts
  # the plans as the deployer app and mints 1-org's GitHub token from the
  # infrastructure app's key; the applier mints the same token.
  runners = {
    planner = google_service_account.planner
    applier = google_service_account.applier
  }
  runner_keys = {
    for pair in setproduct(keys(local.runners), ["infrastructure", "deployer"]) :
    "${pair[0]}__${pair[1]}" => { runner = pair[0], key = pair[1] }
  }
  key_containers = {
    infrastructure = google_secret_manager_secret.github_infrastructure_key.secret_id
    deployer       = google_secret_manager_secret.github_deployer_key.secret_id
  }
}

# The Cloud Build service agent is the identity that reads the token. It is
# created when the API is enabled, and this forces it into existence.
resource "google_project_service_identity" "cloudbuild" {
  provider = google-beta

  project = module.boot_project.project_id
  service = "cloudbuild.googleapis.com"
}

resource "google_secret_manager_regional_secret_iam_member" "token_accessor" {
  for_each = local.token_regional ? local.token_readers : {}

  project   = local.token_project
  location  = local.token_location
  secret_id = local.token_secret
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:service-${each.value}@gcp-sa-cloudbuild.iam.gserviceaccount.com"
}

resource "google_secret_manager_secret_iam_member" "token_accessor" {
  for_each = local.token_regional ? {} : local.token_readers

  project   = local.token_project
  secret_id = local.token_secret
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:service-${each.value}@gcp-sa-cloudbuild.iam.gserviceaccount.com"
}

resource "google_cloudbuildv2_connection" "github" {
  count = local.connected ? 1 : 0

  project  = module.boot_project.project_id
  location = local.region
  name     = "${var.prefix}-boot-${local.region_code}-github"

  github_config {
    app_installation_id = local.cloud_build_installation_id

    authorizer_credential {
      oauth_token_secret_version = local.cloud_build_token_secret_version
    }
  }

  depends_on = [
    google_project_service_identity.cloudbuild,
    google_secret_manager_secret_iam_member.token_accessor,
    google_secret_manager_regional_secret_iam_member.token_accessor,
  ]
}

# The infrastructure repository's link under the connection, which both
# triggers watch.
resource "google_cloudbuildv2_repository" "infrastructure" {
  count = local.connected ? 1 : 0

  project           = module.boot_project.project_id
  location          = local.region
  name              = "${var.prefix}-boot-${local.region_code}-infrastructure-repo"
  parent_connection = google_cloudbuildv2_connection.github[0].id
  remote_uri        = "https://github.com/${var.github_organization}/${var.infrastructure_repository}.git"
}

# ---------------------------------------------------------------------------
# The runner identities
#
# The plan trigger's builds run as the planner, the apply trigger's as the
# applier. Neither holds a layer's roles: each impersonates the layer
# identities of its kind for that layer's tofu (roles/iam.serviceAccountTokenCreator
# on each, granted here for the four identities this layer owns and in 1-org
# for the ones it creates), so a plan runs with a plan identity's reads and an
# apply with the apply identity's writes, as before. What each holds itself:
# the build logs (Cloud Logging, which a build as a user-specified service
# account writes to), the connection's read token (bedrock org plan and apply
# read the pull request's and the pushes' changed files through it), the two
# apps' keys, and, for the applier, the markers under applied/ in the state
# bucket that record each layer's last successful apply.
# ---------------------------------------------------------------------------

resource "google_service_account" "planner" {
  project      = module.boot_project.project_id
  account_id   = "${var.prefix}-org-gbl-planner"
  display_name = "Layers planner - ${var.prefix}-org-gbl"
  description  = "Runs the plan trigger's builds: plans each layer a pull request changes as that layer's plan identity and posts the plan."
}

resource "google_service_account" "applier" {
  project      = module.boot_project.project_id
  account_id   = "${var.prefix}-org-gbl-applier"
  display_name = "Layers applier - ${var.prefix}-org-gbl"
  description  = "Runs the apply trigger's builds: applies each layer a push to ${var.github_default_branch} changes as that layer's apply identity, in layer order."
}

resource "google_project_iam_member" "runner_logs" {
  for_each = local.runners

  project = module.boot_project.project_id
  role    = "roles/logging.logWriter"
  member  = each.value.member
}

# A run reads its own build through the Cloud Build API (its substitutions, the
# pull request, the commit) before it plans or applies anything.
resource "google_project_iam_member" "runner_builds" {
  for_each = local.runners

  project = module.boot_project.project_id
  role    = "roles/cloudbuild.builds.viewer"
  member  = each.value.member
}

# Cloud Build runs a build as the trigger's service account on the service
# agent's say-so, and whoever creates the trigger acts as that account: the
# bootstrap administrator's Owner covers the first apply, the boot identity
# every apply after it.
resource "google_service_account_iam_member" "runner_agent_user" {
  for_each = local.runners

  service_account_id = each.value.name
  role               = "roles/iam.serviceAccountUser"
  member             = "serviceAccount:service-${module.boot_project.project_number}@gcp-sa-cloudbuild.iam.gserviceaccount.com"

  depends_on = [google_project_service_identity.cloudbuild]
}

resource "google_service_account_iam_member" "runner_boot_tofu_user" {
  for_each = local.runners

  service_account_id = each.value.name
  role               = "roles/iam.serviceAccountUser"
  member             = google_service_account.boot_tofu.member
}

# The applier applies this layer as the boot identity and 1-org as the org
# identity; the planner plans them as the two plan identities (bootstrap.tf).
# 1-org grants the same on the identities it creates.
resource "google_service_account_iam_member" "applier_boot_tofu" {
  service_account_id = google_service_account.boot_tofu.name
  role               = "roles/iam.serviceAccountTokenCreator"
  member             = google_service_account.applier.member
}

resource "google_service_account_iam_member" "applier_org_tofu" {
  service_account_id = google_service_account.org_tofu.name
  role               = "roles/iam.serviceAccountTokenCreator"
  member             = google_service_account.applier.member
}

resource "google_service_account_iam_member" "boot_plan_planner" {
  for_each = local.boot_plans

  service_account_id = google_service_account.boot_plan[each.key].name
  role               = "roles/iam.serviceAccountTokenCreator"
  member             = google_service_account.planner.member
}

# A plan of this layer reads the connection, the repository link and the two
# triggers as the boot plan identity, whose organization-level reads
# (var.boot_plan_roles) carry nothing of Cloud Build: the connection and the
# link through roles/cloudbuild.connectionViewer, a regional trigger through
# roles/cloudbuild.builds.viewer (its read is answered on cloudbuild.builds.get;
# cloudbuild.triggers.get is not asked for), both on this project alone, where
# every one of them lives.
resource "google_project_iam_member" "boot_plan_cloud_build" {
  for_each = toset(["roles/cloudbuild.connectionViewer", "roles/cloudbuild.builds.viewer"])

  project = module.boot_project.project_id
  role    = each.value
  member  = google_service_account.boot_plan["boot"].member
}

resource "google_cloudbuildv2_connection_iam_member" "runner_read_token" {
  for_each = local.connected ? local.runners : {}

  project  = module.boot_project.project_id
  location = google_cloudbuildv2_connection.github[0].location
  name     = google_cloudbuildv2_connection.github[0].name
  role     = "roles/cloudbuild.readTokenAccessor"
  member   = each.value.member
}

resource "google_secret_manager_secret_iam_member" "runner_keys" {
  for_each = local.runner_keys

  project   = module.boot_project.project_id
  secret_id = local.key_containers[each.value.key]
  role      = "roles/secretmanager.secretAccessor"
  member    = local.runners[each.value.runner].member
}

# The markers: one object per layer run under applied/ (applied/<layer>,
# applied/2-env/<env>), holding the commit of the run's last successful
# apply, which bedrock org apply reads to decide what a push changed and
# writes after each apply. The applier reaches nothing else in the bucket.
resource "google_storage_bucket_iam_member" "applier_markers" {
  bucket = var.state_bucket
  role   = "roles/storage.objectUser"
  member = google_service_account.applier.member

  condition {
    title       = "${var.prefix}-org-gbl-applied-markers"
    description = "The markers of the layers' last successful applies."
    expression  = "resource.name.startsWith(\"${local.state_bucket_objects}/applied/\")"
  }
}

# ---------------------------------------------------------------------------
# The triggers
#
# Both run the same steps, declared once (local.steps) and followed by one
# command each. The first two steps put the pinned bedrock in place: a
# release's binary downloaded and verified against the checksum placement.json
# records, or a commit pin, once git finds a branch of the repository that
# reaches it, built with go install in the Go image, where Go's checksum
# database verifies it; the third runs bedrock org check; the last
# runs bedrock org plan or bedrock org apply, which read the build (its pull
# request, its commit, _LAYER and _ENVIRONMENT) through the Cloud Build API.
# The apply trigger also applies one layer again by hand, with no change to
# it (README, "The Cloud Build triggers"):
#
#   gcloud builds triggers run imp-boot-uc1-layers-apply --region=us-central1 --project=imp-boot-gbl-core-a1b2 --branch=master --substitutions=_LAYER=2-env,_ENVIRONMENT=tst
# ---------------------------------------------------------------------------

locals {
  bedrock = "/builder/home/bedrock"
  run_env = ["BUILD_ID=$BUILD_ID", "PROJECT_ID=$PROJECT_ID", "LOCATION=$LOCATION"]
  substitutions = {
    _LAYER         = ""          # a layer to apply again by hand, with no change check; empty for the push's changes
    _ENVIRONMENT   = ""          # with _LAYER=2-env, one environment; empty for the three in order
    _BEDROCK_STEPS = "2-release" # the shape of these steps, which bedrock org plan and apply compare with their own
  }

  # FetchBedrock reads the pin off placement.json in the checkout, at run
  # time, with python3 in the gcloud image, and refuses a commit pin no branch
  # of the repository reaches (a commits-only clone of its branches, with git).
  # The steps follow the pin's kind at render time, as the application
  # pipeline's do: a release pin renders FetchBedrock alone, and a commit pin
  # adds BuildBedrock, which builds it in the Go image. The step reads the
  # shape it runs in from BEDROCK_STEPS, and in the release shape it refuses a
  # commit pin, which no step would build. bedrock upgrade renders this layer
  # with the pin it moves, and applies it when the pin changes kind.
  fetch_bedrock = <<-EOT
    import hashlib
    import json
    import os
    import re
    import subprocess
    import sys
    import tempfile
    import time
    import urllib.request

    home, host, repository = sys.argv[1], sys.argv[2], sys.argv[3]
    placement_path = sys.argv[4] if len(sys.argv) > 4 else "placement.json"
    steps = os.environ.get("BEDROCK_STEPS", "")
    with open(placement_path, encoding="utf-8") as f:
        placement = json.load(f)
    version = placement.get("bedrockVersion", "")
    digest = placement.get("bedrockSha256", "")
    if not version:
        sys.exit(placement_path + " pins no bedrock (bedrockVersion): run bedrock upgrade in this repository, which records the pin and renders, and commit what it changed")
    if re.search(r"[-.][0-9]{14}-[0-9a-f]{12}\Z", version):
        if steps.endswith("-release"):
            sys.exit(placement_path + " pins bedrock " + version + ", a commit pin, and this trigger's steps are in the release shape (" + steps + "): this step downloads a release, and no step follows to build a commit. A commit pin needs the commit shape, which bedrock upgrade applies to 0-bootstrap when it moves the pin from a release to a commit; when the pin was moved without that apply, apply 0-bootstrap from this branch by hand (in 0-bootstrap: tofu init, then tofu apply, as the bootstrap administrator) before this pull request's plan, or the merge's apply, can run")
        commit = version[-12:]
        branches = tempfile.mkdtemp(prefix="bedrock-branches-")
        cloned = subprocess.run(["git", "clone", "--quiet", "--bare", "--no-tags", "--filter=tree:0", repository, branches], capture_output=True, text=True, check=False)
        if cloned.returncode != 0:
            sys.exit("could not read the branches of " + repository + " to check the commit pin " + version + " against: " + cloned.stderr.strip())
        heads = subprocess.run(["git", "-C", branches, "for-each-ref", "--format=%(refname)", "refs/heads/"], capture_output=True, text=True, check=False)
        if heads.returncode != 0:
            sys.exit("could not list the branches of " + repository + " to check the commit pin " + version + " against: " + heads.stderr.strip())
        on = ""
        for head in heads.stdout.split():
            if subprocess.run(["git", "-C", branches, "merge-base", "--is-ancestor", commit, head], capture_output=True, check=False).returncode == 0:
                on = head[len("refs/heads/"):]
                break
        if not on:
            sys.exit("bedrock " + version + " pins commit " + commit + ", which no branch of " + repository + " reaches: the triggers build only a commit on a branch of github.com/cccteam/ccc, as bedrock upgrade accepts; move the pin to one, or to a release")
        with open(home + "/bedrock-commit", "w", encoding="utf-8") as f:
            f.write(version)
        print("bedrock " + version + " is a commit pin on " + on + ": the next step builds it with go install")
        sys.exit(0)
    if not re.fullmatch(r"[0-9a-f]{64}", digest):
        sys.exit(placement_path + " pins bedrock " + version + " without the checksum of its linux/amd64 binary (bedrockSha256): bedrock upgrade " + version + " records it from the release's checksums.txt")
    url = host + "/cccteam/ccc/releases/download/bedrock/" + version + "/bedrock-linux-amd64"
    data = None
    for attempt in range(3):
        try:
            with urllib.request.urlopen(url, timeout=60) as response:
                data = response.read()
            break
        except OSError as err:
            print("attempt " + str(attempt + 1) + " of 3 to download " + url + " failed: " + str(err))
            time.sleep(10)
    if data is None:
        sys.exit("could not download " + url)
    if hashlib.sha256(data).hexdigest() != digest:
        sys.exit(url + " does not match bedrockSha256 in " + placement_path + ": the file downloaded is not the release's binary")
    with open(home + "/bedrock", "wb") as f:
        f.write(data)
    os.chmod(home + "/bedrock", 0o755)
    os.execv(home + "/bedrock", [home + "/bedrock", "--version"])
  EOT

  steps = [
    {
      id         = "FetchBedrock"
      name       = "gcr.io/cloud-builders/gcloud"
      entrypoint = "python3"
      args       = ["-c", local.fetch_bedrock, "/builder/home", "https://github.com", "https://github.com/cccteam/ccc.git"]
      env        = ["BEDROCK_STEPS=2-release"]
      timeout    = "300s"
    },
    {
      id         = "Check"
      name       = "ghcr.io/opentofu/opentofu:1.12.6"
      entrypoint = local.bedrock
      args       = ["org", "check"]
      env        = []
      timeout    = "600s"
    },
  ]
}

# The plan trigger's name is also the check 1-org requires on this
# repository's default branch (1-org/github.tf, local.layers_plan_check): a
# renamed trigger is a change in both files.
resource "google_cloudbuild_trigger" "plan" {
  count = local.connected ? 1 : 0

  project  = module.boot_project.project_id
  location = local.region
  name     = "${var.prefix}-boot-${local.region_code}-layers-plan"

  service_account    = google_service_account.planner.id
  include_build_logs = "INCLUDE_BUILD_LOGS_WITH_STATUS"
  substitutions      = local.substitutions

  repository_event_config {
    repository = google_cloudbuildv2_repository.infrastructure[0].id

    # Comment control is left unset, which is "comments disabled": the API stores
    # that value as unset, and a render that named it would show the trigger
    # changing in place on every plan.
    pull_request {
      branch = "^${var.github_default_branch}$"
    }
  }

  build {
    dynamic "step" {
      for_each = local.steps
      content {
        id         = step.value.id
        name       = step.value.name
        entrypoint = step.value.entrypoint
        args       = step.value.args
        env        = step.value.env
        timeout    = step.value.timeout
      }
    }

    step {
      id         = "Plan"
      name       = "ghcr.io/opentofu/opentofu:1.12.6"
      entrypoint = local.bedrock
      args       = ["org", "plan"]
      env        = local.run_env
      timeout    = "3600s"
    }

    timeout = "5100s"

    options {
      logging = "CLOUD_LOGGING_ONLY"
    }
  }

  depends_on = [google_service_account_iam_member.runner_agent_user]
}

resource "google_cloudbuild_trigger" "apply" {
  count = local.connected ? 1 : 0

  project  = module.boot_project.project_id
  location = local.region
  name     = "${var.prefix}-boot-${local.region_code}-layers-apply"

  service_account    = google_service_account.applier.id
  include_build_logs = "INCLUDE_BUILD_LOGS_WITH_STATUS"
  substitutions      = local.substitutions

  repository_event_config {
    repository = google_cloudbuildv2_repository.infrastructure[0].id

    push {
      branch = "^${var.github_default_branch}$"
    }
  }

  build {
    dynamic "step" {
      for_each = local.steps
      content {
        id         = step.value.id
        name       = step.value.name
        entrypoint = step.value.entrypoint
        args       = step.value.args
        env        = step.value.env
        timeout    = step.value.timeout
      }
    }

    step {
      id         = "Apply"
      name       = "ghcr.io/opentofu/opentofu:1.12.6"
      entrypoint = local.bedrock
      args       = ["org", "apply"]
      env        = local.run_env
      timeout    = "7200s"
    }

    timeout = "8700s"

    options {
      logging = "CLOUD_LOGGING_ONLY"
    }
  }

  depends_on = [google_service_account_iam_member.runner_agent_user]
}
