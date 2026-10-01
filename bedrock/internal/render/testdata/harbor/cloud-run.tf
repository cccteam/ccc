# ---------------------------------------------------------------------------
# Cloud Run
#
# The site (main.go) as a service in both lab regions, and the migration
# (cmd/deployment/migrate) as a job in the primary region, with the job
# process (cmd/jobs) as a second job beside it. All are created
# with a placeholder image: the pipeline owns the image from the first deploy
# on, so the image and the labels and annotations a deploy stamps are ignored
# here, and everything else about the revision template (identity, scaling,
# variables, secrets) stays this stack's.
#
# Modeled on CCC's reference deployment, without the VPC egress
# (no connector, no NAT in the lab) and without IAP.
# ---------------------------------------------------------------------------

resource "google_cloud_run_v2_service" "app" {
  for_each = local.regions

  project  = local.project_id
  location = each.value
  name     = local.is_pr ? local.pr_name : "${local.name}-${each.key}-${local.app}-app"

  # Reachable only through the load balancer in the net project (and from
  # inside the VPC); the org policy run.allowedIngress pins this value.
  ingress = "INGRESS_TRAFFIC_INTERNAL_LOAD_BALANCER"

  # prd resists deletion; the other environments can be rebuilt by a pipeline.
  deletion_protection = local.is_prd

  labels = local.labels

  template {
    service_account       = google_service_account.app.email
    execution_environment = "EXECUTION_ENVIRONMENT_GEN2"
    timeout               = "300s"
    labels                = local.labels

    # Scale to zero; two instances per region is plenty for a lab site, and it
    # bounds the bill (JOURNAL.md: alerts only, cap $700/month).
    scaling {
      min_instance_count = 0
      max_instance_count = 2
    }

    containers {
      image = var.placeholder_image

      ports {
        container_port = 8080
      }

      resources {
        # CPU only while a request is in flight: no always-on allocation.
        cpu_idle          = true
        startup_cpu_boost = true
        limits = {
          cpu    = "1"
          memory = "512Mi"
        }
      }

      dynamic "env" {
        for_each = local.service_env
        content {
          name  = env.key
          value = env.value
        }
      }

      # Secrets by reference, at the version var.secret_versions pins. A secret
      # with no pin is not mounted: the process starts without it.
      dynamic "env" {
        for_each = local.mounted_secrets
        content {
          name = env.key
          value_source {
            secret_key_ref {
              secret  = local.secret_ids[env.key]
              version = env.value.version
            }
          }
        }
      }
    }
  }

  lifecycle {
    # The pipeline deploys the image and stamps the revision with its own
    # labels and annotations (commit, build, trigger, client). Those are its.
    ignore_changes = [
      template[0].containers[0].image,
      template[0].labels,
      template[0].annotations,
      labels,
      annotations,
      client,
      client_version,
    ]

    precondition {
      condition     = alltrue([for key in keys(local.secret_versions) : contains(keys(local.secrets), key)])
      error_message = "secret_versions for ${var.environment} names a variable no secret in pkg/config declares. Known: ${join(", ", keys(local.secrets))}."
    }
  }

  depends_on = [
    google_secret_manager_secret_iam_member.app_accessor,
    google_spanner_database_iam_member.app_user,
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
# The migration job's template: cmd/deployment/migrate. This job is never run and
# never deployed to. Each build copies it (bedrock deploy jobs) into a job of
# its own, named after this one with the build's version, on the build's
# image; the pipeline runs that copy once before the release's revisions take
# traffic and deletes it at the end of the step, so no migrate job of a build
# remains. One task, no retries (a migration that failed is looked at, not
# rerun blind), a generous timeout for a long DDL: the stack keeps the job's
# identity, variables, timeout, retries and resources here.
# ---------------------------------------------------------------------------

resource "google_cloud_run_v2_job" "migrate" {
  project  = local.project_id
  location = local.primary_region
  name     = local.is_pr ? "${local.pr_name}-migrate" : "${local.name}-${local.primary_region_code}-${local.app}-migrate"

  deletion_protection = false

  labels = local.labels

  template {
    task_count = 1
    labels     = local.labels

    template {
      service_account       = google_service_account.migrate.email
      execution_environment = "EXECUTION_ENVIRONMENT_GEN2"
      max_retries           = 0
      timeout               = "900s"

      containers {
        image = var.placeholder_image
        # The image's entrypoint is the site; the job runs the migrate binary
        # beside it (Dockerfile: /migrate). Without this the job served the
        # site until its timeout, silently, on the first deploy.
        command = ["/migrate"]

        resources {
          limits = {
            cpu    = "1"
            memory = "512Mi"
          }
        }

        dynamic "env" {
          for_each = local.job_env
          content {
            name  = env.key
            value = env.value
          }
        }
        # No secret is mounted: the migration constructs the data level
        # without one (locals.tf, readers).
      }
    }
  }

  lifecycle {
    ignore_changes = [
      template[0].template[0].containers[0].image,
      template[0].labels,
      template[0].annotations,
      labels,
      annotations,
      client,
      client_version,
    ]
  }

  depends_on = [google_spanner_database_iam_member.migrate_admin]
}

# ---------------------------------------------------------------------------
# The job process: cmd/jobs, the application's own code as a Cloud Run job
# in the primary region. This is the template: never run, never deployed to.
# Each build copies it (bedrock deploy jobs) into a job of its own, named
# after this one with the build's version (<name>-v0-1-15), on the build's
# image, and bakes that job's name into the image as the site's
# APP_JOBS_JOB: a revision starts the job of its own build, and a
# traffic rollback starts the earlier one. Only the running service starts
# the job process; the pipeline never runs it and a schedule calls an endpoint
# on the service. The stack keeps the jobs' variables, secrets, identity,
# timeout, retries and resources here (var.jobs_timeout, var.jobs_retries,
# var.jobs_resources), and the pipeline deletes a build's job once no revision
# in any region carries its version (bedrock deploy sweep-jobs).
# ---------------------------------------------------------------------------

resource "google_cloud_run_v2_job" "jobs" {
  project  = local.project_id
  location = local.primary_region
  name     = local.jobs_job_name

  deletion_protection = false

  labels = local.labels

  template {
    task_count = 1
    labels     = local.labels

    template {
      service_account       = google_service_account.jobs.email
      execution_environment = "EXECUTION_ENVIRONMENT_GEN2"
      max_retries           = var.jobs_retries
      timeout               = var.jobs_timeout

      containers {
        image = var.placeholder_image
        # The image's entrypoint is the site; the job runs the jobs binary
        # beside it (Dockerfile: /jobs).
        command = ["/jobs"]

        resources {
          limits = {
            cpu    = var.jobs_resources.cpu
            memory = var.jobs_resources.memory
          }
        }

        dynamic "env" {
          for_each = local.jobs_env
          content {
            name  = env.key
            value = env.value
          }
        }

        # Secrets by reference, at the version var.secret_versions pins: the
        # ones at the levels the job process constructs (locals.tf, jobs_secrets).
        dynamic "env" {
          for_each = local.jobs_mounted_secrets
          content {
            name = env.key
            value_source {
              secret_key_ref {
                secret  = local.secret_ids[env.key]
                version = env.value.version
              }
            }
          }
        }
      }
    }
  }

  lifecycle {
    ignore_changes = [
      template[0].template[0].containers[0].image,
      template[0].labels,
      template[0].annotations,
      labels,
      annotations,
      client,
      client_version,
    ]
  }

  depends_on = [google_spanner_database_iam_member.jobs_user, google_secret_manager_secret_iam_member.jobs_accessor]
}

# The site starts the job of its own build through the Cloud Run API as its
# own identity (APP_JOBS_JOB names it from the image). The grant sits on
# the template, which is never run: bedrock deploy jobs copies the template's
# IAM policy onto each build's job with its settings, so who may start the job
# process is decided here and nowhere else. (Cloud Run does not evaluate IAM
# conditions on a job's name when a job is run, so one grant on the project
# bounded to the builds' names would grant nothing.)
resource "google_cloud_run_v2_job_iam_member" "app_runs_jobs" {
  project  = google_cloud_run_v2_job.jobs.project
  location = google_cloud_run_v2_job.jobs.location
  name     = google_cloud_run_v2_job.jobs.name
  role     = "roles/run.invoker"
  member   = local.app_member

  depends_on = [google_service_account.app]
}
