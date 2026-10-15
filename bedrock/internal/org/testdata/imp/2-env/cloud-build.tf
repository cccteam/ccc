# ---------------------------------------------------------------------------
# Cloud Build, second generation: the GitHub connection and the repository
# links
#
# One connection per environment project. The Cloud Build GitHub App is
# installed on the imp-example organization (one installation ID for
# every connection), and the connection proves it may act for that
# installation with a GitHub OAuth token held in Secret Manager. That token is
# produced once per organization by a browser step that cannot be scripted,
# in the boot project's Cloud Build console, signed in to GitHub as the
# machine account imp-machine, before the first apply of 0-bootstrap
# (0-bootstrap/README.md, "The Cloud Build GitHub authorization"). The console
# writes the token as a regional secret in the boot project, and placement.json
# records the installation and the token secret's version
# (cloudBuildInstallationId, cloudBuildTokenSecretVersion), which every
# connection is rendered from: the boot project's, which the layers' triggers
# watch this repository through, and each environment's. 0-bootstrap grants
# this project's Cloud Build service agent the token's read, by the project
# number placement.json records (projectNumbers).
#
# Until the browser step has run, placement.json records neither value and
# everything below but the service agent is skipped: the environment
# (identities, registries, records bucket, the tst instance) applies without
# its connection, and the application layer's triggers wait on it.
# ---------------------------------------------------------------------------

# The Cloud Build service agent is the identity that reads the token. It is
# created when the API is enabled (1-org does that), and this forces it into
# existence where enabling ran ahead of provisioning.
resource "google_project_service_identity" "cloudbuild" {
  provider = google-beta

  project = local.project_id
  service = "cloudbuild.googleapis.com"
}

resource "google_cloudbuildv2_connection" "github" {
  count = local.connected ? 1 : 0

  project  = local.project_id
  location = local.region
  name     = "${local.name}-${local.region_code}-github"

  github_config {
    app_installation_id = local.cloud_build_installation_id

    authorizer_credential {
      oauth_token_secret_version = local.cloud_build_token_secret_version
    }
  }

  depends_on = [google_project_service_identity.cloudbuild]
}


# The pipeline mints a short-lived GitHub token from the connection (the tag
# check reads the compare API, the pull-request build reads its /gcbrun
# comment). That is cloudbuild.repositories.accessReadToken, carried by the
# Read Token Accessor role and by no builder role; granted on the connection
# itself, to each application's deploy identity, and nowhere wider. Found by
# the first triggered build (2026-09-26): the mint answered 403.
resource "google_cloudbuildv2_connection_iam_member" "deploy_read_token" {
  for_each = local.connected ? local.apps : toset([])

  project  = local.project_id
  location = google_cloudbuildv2_connection.github[0].location
  name     = google_cloudbuildv2_connection.github[0].name
  role     = "roles/cloudbuild.readTokenAccessor"
  member   = google_service_account.deploy[each.key].member
}

# In tst the application stack's own triggers (the check trigger, the
# pull-request stack trigger and the sweep) run as the apply identity and mint
# the same token: the check trigger posts its plans on the pull request, the
# stack job reads the /gcbrun comment, the sweep asks GitHub which pull
# requests are closed.
resource "google_cloudbuildv2_connection_iam_member" "apply_read_token" {
  for_each = local.connected && local.is_tst ? local.apps : toset([])

  project  = local.project_id
  location = google_cloudbuildv2_connection.github[0].location
  name     = google_cloudbuildv2_connection.github[0].name
  role     = "roles/cloudbuild.readTokenAccessor"
  member   = google_service_account.apply[each.key].member
}

# The repository link is per application and is registration, not
# application infrastructure: creating one needs cloudbuild.repositories.create,
# which only the connection administrator holds, and the design brief puts
# "the repository registration" with the environment layer. The application
# stack's triggers reference the link by ID (output applications[*].repository_id).
resource "google_cloudbuildv2_repository" "app" {
  for_each = local.connected ? local.apps : toset([])

  project           = local.project_id
  location          = local.region
  name              = "${local.name}-${local.region_code}-${each.key}-repo"
  parent_connection = google_cloudbuildv2_connection.github[0].id
  remote_uri        = "https://github.com/${var.github_organization}/${each.key}.git"
}

# ---------------------------------------------------------------------------
# The stack trigger: an application's first apply in this environment
#
# An application's stack (the infrastructure directory of its repository) is
# applied by its releases' builds, as the apply identity, once it exists. Its
# first apply, before any release, is this trigger's: one per application,
# manual, its source the application's repository at the default branch, run
# as the deploy identity (which may become the apply identity, identities.tf)
# and started by the application's operations workflow (its apply action, as
# the operations identity, in the GitHub Environment named after this
# environment), so no person holds a cloud role for it and the audit log names
# who asked. Its steps are the organization's triggers' (0-bootstrap): fetch
# the bedrock the checkout's infrastructure/placement.json pins, build a commit
# pin, bedrock check, then bedrock deploy stack first, which refuses a stack a
# release has run on. In the environments whose releases wait for an approval
# in Cloud Build (team-group.tf), so does this build.
# ---------------------------------------------------------------------------

locals {
  stack_fetch_bedrock = <<-EOT
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
    with open(placement_path, encoding="utf-8") as f:
        placement = json.load(f)
    version = placement.get("bedrockVersion", "")
    digest = placement.get("bedrockSha256", "")
    if not version:
        sys.exit(placement_path + " pins no bedrock (bedrockVersion): run bedrock upgrade in this repository, which records the pin and renders, and commit what it changed")
    if re.search(r"[-.][0-9]{14}-[0-9a-f]{12}\Z", version):
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
  stack_build_bedrock = <<-EOT
    set -eu
    if [ ! -f /builder/home/bedrock-commit ]; then
      echo "a release pin: nothing to build"
      exit 0
    fi
    go install -ldflags="-s -w" github.com/cccteam/ccc/bedrock@"$$(cat /builder/home/bedrock-commit)"
    /builder/home/bedrock --version
  EOT
  # The build's own id and place, which the command reads its build through
  # the Cloud Build API by.
  stack_build_env = ["BUILD_ID=$BUILD_ID", "PROJECT_ID=$PROJECT_ID", "LOCATION=$LOCATION"]
}

resource "google_cloudbuild_trigger" "stack" {
  for_each = local.connected ? local.apps : toset([])

  project  = local.project_id
  location = local.region
  name     = "${local.name}-${local.region_code}-${each.key}-stack"

  service_account    = google_service_account.deploy[each.key].id
  include_build_logs = "INCLUDE_BUILD_LOGS_WITH_STATUS"
  substitutions = {
    _APP            = each.key
    _ENV            = var.environment
    _APPLY_IDENTITY = google_service_account.apply[each.key].email
    _RECORDS_BUCKET = google_storage_bucket.records.name
  }

  approval_config {
    approval_required = local.approval_required
  }

  source_to_build {
    repository = google_cloudbuildv2_repository.app[each.key].id
    ref        = "refs/heads/master"
    repo_type  = "GITHUB"
  }

  build {
    step {
      id         = "FetchBedrock"
      name       = "gcr.io/cloud-builders/gcloud"
      entrypoint = "python3"
      args       = ["-c", local.stack_fetch_bedrock, "/builder/home", "https://github.com", "https://github.com/cccteam/ccc.git", "infrastructure/placement.json"]
      timeout    = "300s"
    }

    step {
      id         = "BuildBedrock"
      name       = "cgr.dev/chainguard/go@sha256:2c20827746d752c1f18989847956637a0dc979c42507f4059e1d9559ee7733af"
      entrypoint = "sh"
      args       = ["-c", local.stack_build_bedrock]
      env        = ["GOBIN=/builder/home", "CGO_ENABLED=0", "GOTOOLCHAIN=local", "GOFLAGS=-trimpath", "GOPATH=/tmp/go", "GOCACHE=/tmp/go-build", "GOMODCACHE=/tmp/go-mod", "GOPROXY=https://proxy.golang.org", "GOSUMDB=sum.golang.org", "GONOSUMDB=", "GOPRIVATE=", "GOINSECURE="]
      timeout    = "600s"
    }

    step {
      id         = "Check"
      name       = "ghcr.io/opentofu/opentofu:1.12.6"
      entrypoint = "/builder/home/bedrock"
      args       = ["check"]
      timeout    = "600s"
    }

    step {
      id         = "FirstApply"
      name       = "ghcr.io/opentofu/opentofu:1.12.6"
      entrypoint = "/builder/home/bedrock"
      args       = ["deploy", "stack", "first"]
      env        = local.stack_build_env
      timeout    = "7200s"
    }

    timeout = "8700s"

    options {
      logging = "CLOUD_LOGGING_ONLY"
    }
  }
}
