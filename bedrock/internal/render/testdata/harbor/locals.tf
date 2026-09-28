locals {
  # The application code, fixed for this stack. It is the key 2-env registered
  # the identities and the repository link under, and the segment every name
  # below carries.
  app = "harbor"

  org = data.terraform_remote_state.org.outputs
  env = data.terraform_remote_state.env.outputs

  prefix     = local.env.prefix
  project_id = local.env.project_id
  name       = "${local.prefix}-${var.environment}"

  is_prd = var.environment == "prd"

  # A pull-request stack: this code in tst with var.pull_request set, into
  # its own state. Its resources carry the short name harbor-pr<N> (2-env's wildcard
  # backend picks the Cloud Run service by that name from the hostname), and it
  # shares tst's secret containers, identity registration and registry.
  is_pr   = var.pull_request != 0
  pr_name = "${local.app}-pr${var.pull_request}"

  # The runtime accounts' IDs: by the convention, or the short name.
  app_account     = local.is_pr ? "${local.pr_name}-app" : "${local.name}-gbl-${local.app}-app"
  migrate_account = local.is_pr ? "${local.pr_name}-migrate" : "${local.name}-gbl-${local.app}-migrate"
  jobs_account    = local.is_pr ? "${local.pr_name}-jobs" : "${local.name}-gbl-${local.app}-jobs"

  # The same accounts as members and as resource names, spelled out rather than
  # read from the account resources, so a plan knows every membership before
  # the accounts exist: the pull-request build's guard reads the pull request's
  # name off each planned change, and a member known only after apply would
  # stop the first build of every pull request.
  app_email            = "${local.app_account}@${local.project_id}.iam.gserviceaccount.com"
  migrate_email        = "${local.migrate_account}@${local.project_id}.iam.gserviceaccount.com"
  app_member           = "serviceAccount:${local.app_email}"
  migrate_member       = "serviceAccount:${local.migrate_email}"
  app_account_name     = "projects/${local.project_id}/serviceAccounts/${local.app_email}"
  migrate_account_name = "projects/${local.project_id}/serviceAccounts/${local.migrate_email}"
  jobs_email           = "${local.jobs_account}@${local.project_id}.iam.gserviceaccount.com"
  jobs_member          = "serviceAccount:${local.jobs_email}"
  jobs_account_name    = "projects/${local.project_id}/serviceAccounts/${local.jobs_email}"

  # The environment before this one in the promotion order (tst, stg, prd) and
  # its deployment-records bucket: the pipeline runs a release here only after
  # that environment holds a live record of it. The first environment has none.
  previous_environment    = { tst = "", stg = "tst", prd = "stg" }[var.environment]
  previous_records_bucket = try(data.terraform_remote_state.previous_env[0].outputs.records_bucket, "")

  # The service runs in both lab regions; the migrate job in the primary only
  # (a job runs once, from one place). Keyed by region code because the code
  # is what names the regional resources.
  regions = {
    (local.env.region_code)           = local.env.region
    (local.env.secondary_region_code) = local.env.secondary_region
  }
  primary_region_code = local.env.region_code
  primary_region      = local.env.region

  # What 2-env registered for this application.
  identities = local.env.applications[local.app]
  instance   = local.env.spanner_instance

  # The image path prefix 2-shr publishes for this application
  # (image_paths: <registry hostname>/<shr project>/<repository>); the
  # pipeline appends /<image>:<tag>. Null until 2-shr knows the application.
  registry = try(data.terraform_remote_state.shr.outputs.image_paths[local.app], null)

  hostnames = local.is_pr ? ["${local.pr_name}.impulseframework.dev"] : var.hostnames[var.environment]
  # The next revision's hostnames, the first label with -next: what the hook before
  # traffic calls while the old revision still serves. None for a pull-request stack.
  next_hostnames = local.is_pr ? [] : [for host in local.hostnames : replace(host, "/^([^.]+)\\./", "$1-next.")]

  # From .envrc.template: APP_STAFF_OIDC_REDIRECT_URL is the browser-facing
  # callback, the route pkg/router/zz_gen_router.go registers as
  # GET /api/user/callback, on the environment's canonical host.
  redirect_url = "https://${local.hostnames[0]}/api/user/callback"

  # The database the site opens: the environment's own, a pull request's own,
  # or in shared mode (var.shared_database) tst's, which the pull-request
  # stack then only reads by name and grants its app identity on.
  own_database  = !(local.is_pr && var.shared_database)
  database_name = local.own_database && local.is_pr ? "${local.pr_name}-db" : "${local.name}-gbl-${local.app}-db"

  # The assets bucket (dataConfig.AssetsBucket): a pull-request stack has its
  # own, and the environment project's number makes the name unique.
  assets_bucket_name = "${local.name}-gbl-${local.is_pr ? local.pr_name : local.app}-assets-${local.env.project_number}"

  # The task queue (dataConfig.TasksQueue), by name and as the Cloud Tasks API
  # names it: tst's for a pull-request stack, which enqueues on it (tasks.tf).
  tasks_queue_name = "${local.name}-${local.primary_region_code}-${local.app}-tasks"
  tasks_queue      = "projects/${local.project_id}/locations/${local.primary_region}/queues/${local.tasks_queue_name}"

  # The Firestore database (dataConfig.FirestoreDatabase), by id; a pull-request
  # stack has its own.
  firestore_database_id = local.is_pr ? "${local.pr_name}-fs" : "${local.name}-gbl-${local.app}-fs"

  # The job process's Cloud Run job (cmd/jobs), in the primary region, by name
  # and as the Cloud Run API names it: what APP_JOBS_JOB tells the site.
  jobs_job_name = local.is_pr ? "${local.pr_name}-jobs" : "${local.name}-${local.primary_region_code}-${local.app}-jobs"
  jobs_job      = "projects/${local.project_id}/locations/${local.primary_region}/jobs/${local.jobs_job_name}"

  # ---------------------------------------------------------------------------
  # What the code declares, read from pkg/config by bedrock render; each entry
  # names the field it comes from.
  # ---------------------------------------------------------------------------

  # Secrets: the fields of pkg/config tagged secret:"true". One container per
  # variable per environment, named imp-<env>-gbl-harbor-<kebab of the
  # variable without its APP_ prefix>. No versions: an operator adds the value
  # (Secret Version Adder), and var.secret_versions pins which one runs.
  #
  # readers: which runtime identity mounts it. Only the service: the migrate
  # step also constructs the data level, but the session library reads the
  # cookie key (falling back to an ephemeral one), the client secret, and the
  # admin credentials only when a browser signs in, which a migration never
  # does. So the migrate identity holds no accessor grant. The design brief's
  # rule of thumb ("every process that constructs the level") would grant it;
  # this is the narrower reading, and a fork for the derivation to settle.
  secrets = {
    APP_COOKIE_KEY = {
      name    = "cookie-key"
      level   = "data"
      source  = "pkg/config/data.go dataConfig.CookieKey"
      purpose = "Signs session cookies and seals list cursors: Base64 of 32+ random bytes. Rotating it ends every session and cursor."
    }
    APP_STAFF_OIDC_CLIENT_SECRET = {
      name    = "staff-oidc-client-secret"
      level   = "data"
      source  = "pkg/config/data.go dataConfig.StaffClientSecret"
      purpose = "OAuth client secret of the application's registration with Google (pairs with APP_STAFF_OIDC_CLIENT_ID)."
    }
    APP_STAFF_OIDC_ADMIN_CREDENTIALS = {
      name    = "staff-oidc-admin-credentials"
      level   = "data"
      source  = "pkg/config/data.go dataConfig.StaffAdminCredentials"
      purpose = "Service-account key (JSON) with domain-wide delegation for the Admin SDK groups scope, through which the staff auth reads role groups."
    }
  }

  # The container each secret lives in, by the naming convention: this
  # environment's, which a pull-request stack shares with tst and never creates.
  secret_ids = { for key, s in local.secrets : key => "${local.name}-gbl-${local.app}-${s.name}" }

  secret_versions = lookup(var.secret_versions, var.environment, {})

  # Only a secret with a pinned version is mounted.
  mounted_secrets = {
    for key, s in local.secrets : key => merge(s, { version = local.secret_versions[key] })
    if contains(keys(local.secret_versions), key)
  }

  # The job process's share: the secrets at the levels it constructs (core and data).
  # It runs the application's own code, and what that code reads the derivation
  # cannot know, so it gets the levels' secrets the way the site does; the
  # migrate command, whose work is known, gets none.
  jobs_secrets         = { for key, s in local.secrets : key => s if contains(["core", "data"], s.level) }
  jobs_mounted_secrets = { for key, s in local.mounted_secrets : key => s if contains(keys(local.jobs_secrets), key) }

  # Build-time secrets (var.build_secrets): what the image build reads in this
  # environment, NAME = pinned version, their containers named like the runtime
  # secrets' (the variable without its APP_ prefix, in kebab case). A
  # pull-request stack reads tst's and creates none.
  build_secrets    = lookup(var.build_secrets, var.environment, {})
  build_secret_ids = { for name in keys(local.build_secrets) : name => "${local.name}-gbl-${local.app}-${lower(replace(trimprefix(name, "APP_"), "_", "-"))}" }

  # Environment variables the infrastructure knows, by configuration level
  # (pkg/config/config.go: core, every process; data.go: every process that
  # opens the database; site.go: the served site). A process receives the
  # levels it constructs and nothing above them.
  core_env = {
    # coreConfig.ServiceName (required): the name the process reports in logs.
    APP_SERVICE_NAME = local.app
    # coreConfig.LoggingProjectID: request logs ship to Cloud Logging here.
    GOOGLE_CLOUD_LOGGING_PROJECT = local.project_id
    # coreConfig.AppVersion is not set here: the pipeline bakes APP_VERSION
    # into the image at build (a Dockerfile ENV from the tag), so a deploy
    # never edits the template's variables and this stack stays their owner.
  }

  data_env = {
    # SpannerSettings, all required: the database identity.
    GOOGLE_CLOUD_SPANNER_PROJECT       = local.instance.project
    GOOGLE_CLOUD_SPANNER_INSTANCE_ID   = local.instance.name
    GOOGLE_CLOUD_SPANNER_DATABASE_NAME = local.database_name
    # dataConfig.StaffHostedDomain and StaffGroupPrefix: the session library
    # refuses to construct without them, so every data-level process gets both.
    APP_STAFF_OIDC_HOSTED_DOMAIN = var.staff_oidc_hosted_domain
    APP_STAFF_OIDC_GROUP_PREFIX  = var.staff_oidc_group_prefix
    # dataConfig.SessionTimeout is left to its code default (10m).
  }

  # The directory registration the served site needs and a migration does not.
  # They live at the data level in the code, but a process that never signs
  # anyone in has no use for them, so the job goes without.
  site_directory_env = {
    # dataConfig.StaffClientID: public half of the OAuth client.
    APP_STAFF_OIDC_CLIENT_ID = var.staff_oidc_client_id[var.environment]
    # dataConfig.StaffRedirectURL: built from the canonical hostname.
    APP_STAFF_OIDC_REDIRECT_URL = local.redirect_url
    # dataConfig.StaffAdminSubject: the administrator the groups read impersonates.
    APP_STAFF_OIDC_ADMIN_SUBJECT = var.staff_oidc_admin_subject[var.environment]
  }

  # siteConfig.JobsJob: the job process's Cloud Run job, which the site runs
  # through the Cloud Run API (run.invoker on the job, cloud-run.tf).
  site_jobs_env = {
    APP_JOBS_JOB = local.jobs_job
  }

  # dataConfig.AssetsBucket: the assets bucket (storage.tf), for the processes that
  # construct the data level and run the application's own code.
  assets_env = {
    APP_ASSETS_BUCKET = google_storage_bucket.assets.name
  }

  # dataConfig.TasksQueue: the task queue (tasks.tf), for the processes that
  # construct the data level and run the application's own code.
  tasks_env = {
    APP_TASKS_QUEUE = local.tasks_queue
  }

  # dataConfig.FirestoreDatabase: the Firestore database (firestore.tf), for the
  # processes that construct the data level and run the application's own code.
  firestore_env = {
    APP_FIRESTORE_DATABASE = google_firestore_database.firestore.name
  }

  # site.go: PORT is set by Cloud Run itself (reserved; setting it is an
  # error) and APP_CONSOLE_DIST and APP_PORTAL_DIST is where the image put the bundle, a build
  # detail the Dockerfile owns. Neither is set here.
  service_env = merge(local.core_env, local.data_env, local.site_directory_env, local.site_jobs_env, local.assets_env, local.tasks_env, local.firestore_env)

  # cmd/deployment/migrate reads core and data and nothing above them.
  job_env = merge(local.core_env, local.data_env, {
    APP_SERVICE_NAME = "${local.app}-migrate"
  })

  # cmd/jobs reads core and data and nothing above them: the job process, the
  # application's own code as a Cloud Run job.
  jobs_env = merge(local.core_env, local.data_env, local.assets_env, local.tasks_env, local.firestore_env, {
    APP_SERVICE_NAME = "${local.app}-jobs"
  })

  # Every resource carries these. terraform_source_path is the stack's state slot
  # in the organization's bucket (3-app/harbor) written as a label value, which
  # admits no slash (Secret Manager and Cloud Run refuse it); source_repo is the
  # application repository's name, from the placement.
  base_labels = {
    terraform             = "true"
    terraform_source_path = "3-app-harbor"
    source_repo           = "harbor"
    environment           = var.environment
    application           = local.app
    bedrock-lab           = "true"
  }
  # A pull-request stack also carries its number, for the sweep and the bill.
  labels = merge(local.base_labels, local.is_pr ? { pull_request = tostring(var.pull_request) } : {})
}
