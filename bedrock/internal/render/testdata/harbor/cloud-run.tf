# ---------------------------------------------------------------------------
# Cloud Run
#
# The site (main.go) as a service in the organization's two
# regions, and the job process (cmd/jobs) as a job in the primary
# region. All are created
# with a placeholder image on a template that only stands the service up: from
# the first deploy on the revision template is the pipeline's, which builds
# every revision from the release's image and what the revision_templates
# output says (outputs.tf: identity, scaling, variables, secret mounts, as the
# release's own stack evaluates them), so a release runs under its own
# configuration, forward or back, and an apply never starts a revision. The
# template, the labels and the annotations a deploy stamps are ignored here
# (lifecycle). The migration (cmd/deployment/migrate) is
# no Cloud Run resource: the pipeline takes the migrate command out of the
# release's image and runs it on the build worker as the deploy identity, with
# the variables locals.tf derives for it (migrate_env).
#
# Modeled on CCC's reference deployment, without the VPC egress (no
# connector and no NAT: the organization's network layer creates no network,
# and Cloud Run needs none) and without IAP.
# ---------------------------------------------------------------------------

resource "google_cloud_run_v2_service" "app" {
  for_each = local.regions

  project  = local.project_id
  location = each.value
  name     = local.is_pr ? local.pr_host : "${local.name}-${each.key}-${local.app}-app"

  # Reachable only through the load balancer in the net project (and from
  # inside the VPC); the org policy run.allowedIngress pins this value.
  ingress = "INGRESS_TRAFFIC_INTERNAL_LOAD_BALANCER"

  # prd resists deletion; the other environments can be rebuilt by a
  # pipeline. bedrock teardown and bedrock destroy lift it there, with an apply
  # before their destroy (var.teardown, var.destroy).
  deletion_protection = local.is_prd && !var.teardown && !var.destroy

  labels = local.labels

  # The template the service is created with: the placeholder image on the
  # site's identity, which starts on its own and lets the first apply stand the
  # service up before the first release. Nothing of a revision is set here
  # (revision_templates in outputs.tf says it; deploy service builds it).
  template {
    service_account       = google_service_account.app.email
    execution_environment = "EXECUTION_ENVIRONMENT_GEN2"
    timeout               = "300s"

    containers {
      image = var.placeholder_image

      ports {
        # h2c: the site's main package imports the framework's server
        # (github.com/cccteam/ccc/resource/server), which speaks HTTP/1.1 and unencrypted HTTP/2 on
        # one listener, so Cloud Run talks HTTP/2 to the container and the 32 MiB
        # bounds of HTTP/1 request and response bodies no longer apply. The
        # maintenance revision runs the same image, so it speaks it too. A site on
        # another server leaves the name out and keeps an HTTP/1 port.
        name           = "h2c"
        container_port = 8080
      }
    }
  }

  lifecycle {
    # The revision template is the pipeline's from the first deploy on (deploy
    # service builds it from the revision_templates output and the release's
    # image), and so are the labels and annotations a deploy stamps (commit,
    # build, trigger, client). Those are its.
    ignore_changes = [
      template,
      labels,
      annotations,
      client,
      client_version,
    ]

    precondition {
      condition     = alltrue([for key in keys(local.declared_versions) : contains(keys(local.secrets), key)])
      error_message = "secret_versions for ${var.environment} names a variable no secret in pkg/config declares. Known: ${join(", ", keys(local.secrets))}."
    }

    # A generated secret's entry is the word generated, never a number: its
    # version is derived at each build. The word on any other secret pins nothing.
    precondition {
      condition     = alltrue([for key in keys(local.generated_secrets) : lookup(local.declared_versions, key, "generated") == "generated"])
      error_message = "secret_versions for ${var.environment} pins a version number for a generated secret (${join(", ", [for key in keys(local.generated_secrets) : key if lookup(local.declared_versions, key, "generated") != "generated"])}), whose version the pipeline derives at each build: write generated there."
    }

    precondition {
      condition     = alltrue([for key, v in local.declared_versions : v != "generated" || contains(keys(local.generated_secrets), key)])
      error_message = "secret_versions for ${var.environment} says generated for a secret the code does not declare secret:\"generated\" (${join(", ", [for key, v in local.declared_versions : key if v == "generated" && !contains(keys(local.generated_secrets), key)])}): pin a version number, or declare it generated in the code."
    }
  }

  # The grants the first revision needs before it starts: a grant takes minutes
  # to take effect, and the file store's readiness probe runs at start.
  depends_on = [
    google_secret_manager_secret_iam_member.app_accessor,
    google_spanner_database_iam_member.app_user,
    google_storage_bucket_iam_policy.files,
    google_storage_bucket_iam_member.files_preview,
  ]
}

# The load balancer forwards to the service with no identity of its own, so
# invocation is open to all callers; ingress above keeps those callers to the
# load balancer and the VPC. CCC's reference deployment runs the same pair. The
# organization's domain policy refuses allUsers unless the service carries the
# public-invoker tag (1-org tags.tf), so the tag is bound first.
resource "google_tags_location_tag_binding" "public_invoker" {
  for_each = google_cloud_run_v2_service.app

  parent    = "//run.googleapis.com/projects/${local.env.project_number}/locations/${each.value.location}/services/${each.value.name}"
  tag_value = local.org.public_invoker_tag_value
  location  = each.value.location
}

# A tag binding takes minutes to reach the policy evaluation, and an invoker
# grant made before then is refused ("do not belong to a permitted customer").
# Both first applies were refused, and so were two pull-request stacks after a
# pause of 120 seconds, so the pause is five minutes. It is paid once, when a
# stack's services are created; a refusal costs a whole rerun of the build.
resource "time_sleep" "public_invoker_tag" {
  depends_on = [google_tags_location_tag_binding.public_invoker]

  create_duration = "300s"
}

resource "google_cloud_run_v2_service_iam_member" "invoker" {
  # checkov:skip=CKV_GCP_102: ingress is INGRESS_TRAFFIC_INTERNAL_LOAD_BALANCER; allUsers reaches the service only through the load balancer.
  for_each = google_cloud_run_v2_service.app

  project  = each.value.project
  location = each.value.location
  name     = each.value.name
  role     = "roles/run.invoker"
  member   = "allUsers"

  depends_on = [time_sleep.public_invoker_tag]
}

# ---------------------------------------------------------------------------
# The job process: cmd/jobs, the application's own code as a Cloud Run job
# in the primary region. This is the template: never run, never deployed to.
# Each build copies it (bedrock deploy jobs) into a job of its own, named
# after this one with the build's version (<name>-v0-1-15), on the build's
# image; the service carries this template's name as APP_JOBS_TEMPLATE
# and the image its version, and the framework names the job of its own build
# from the two: a revision starts the job of its own build, and a
# traffic rollback starts the earlier one. Only the running service starts
# the job process; the pipeline never runs it and a schedule calls an endpoint
# on the service. The jobs' variables, secrets, identity, timeout, retries and
# resources (var.jobs_timeout, var.jobs_retries, var.jobs_resources) reach each
# build's job through the revision_templates output (outputs.tf), and the
# pipeline deletes a build's job once no revision in any region carries its
# version (bedrock deploy sweep-jobs).
# ---------------------------------------------------------------------------

resource "google_cloud_run_v2_job" "jobs" {
  project  = local.project_id
  location = local.primary_region
  name     = local.jobs_job_name

  deletion_protection = false

  labels = local.labels

  # The template the job is created with: the placeholder image on the job
  # process's identity. Nothing of a build's job is set here: revision_templates
  # in outputs.tf says it (its variables, secret mounts, timeout, retries and
  # resources), and deploy jobs builds each build's job from it.
  template {
    task_count = 1
    labels     = local.labels

    template {
      service_account       = google_service_account.jobs.email
      execution_environment = "EXECUTION_ENVIRONMENT_GEN2"

      containers {
        image = var.placeholder_image
        # The image starts the site by default; the job's command runs the jobs
        # binary beside it instead (Dockerfile: /jobs).
        command = ["/jobs"]
      }
    }
  }

  lifecycle {
    # The template is the pipeline's from the first build on (deploy jobs builds
    # each build's job from the revision_templates output), and so are the
    # labels and annotations a build stamps.
    ignore_changes = [
      template,
      labels,
      annotations,
      client,
      client_version,
    ]
  }

  depends_on = [google_spanner_database_iam_member.jobs_user, google_secret_manager_secret_iam_member.jobs_accessor, google_storage_bucket_iam_policy.files, google_storage_bucket_iam_member.files_preview]
}

# The site starts the job of its own build through the Cloud Run API as its
# own identity (named from the template, which APP_JOBS_TEMPLATE carries,
# and the version the image bakes in). The grant sits on
# the template, which is never run: bedrock deploy jobs copies the template's
# IAM policy onto each build's job with its settings, so who may start the job
# process is decided here and nowhere else. (Cloud Run does not evaluate IAM
# conditions on a job's name when a job is run, so one grant on the project
# bounded to the builds' names would grant nothing.) The role is the jobs
# executor with overrides: a start passes the command and its flags as the
# container's arguments (run.jobs.runWithOverrides), which run.invoker does
# not grant, as the first start in the lab showed with its 403.
resource "google_cloud_run_v2_job_iam_member" "app_runs_jobs" {
  project  = google_cloud_run_v2_job.jobs.project
  location = google_cloud_run_v2_job.jobs.location
  name     = google_cloud_run_v2_job.jobs.name
  role     = "roles/run.jobsExecutorWithOverrides"
  member   = local.app_member

  depends_on = [google_service_account.app]
}
