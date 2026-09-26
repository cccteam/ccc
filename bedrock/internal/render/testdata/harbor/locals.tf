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

  hostnames = var.hostnames[var.environment]

  # From .envrc.template: APP_STAFF_OIDC_REDIRECT_URL is the browser-facing
  # callback, the route pkg/router/zz_gen_router.go registers as
  # GET /api/user/callback, on the environment's canonical host.
  redirect_url = "https://${local.hostnames[0]}/api/user/callback"

  database_name = "${local.name}-gbl-${local.app}-db"

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
      source  = "pkg/config/data.go dataConfig.CookieKey"
      purpose = "Signs session cookies and seals list cursors: Base64 of 32+ random bytes. Rotating it ends every session and cursor."
    }
    APP_STAFF_OIDC_CLIENT_SECRET = {
      name    = "staff-oidc-client-secret"
      source  = "pkg/config/data.go dataConfig.StaffClientSecret"
      purpose = "OAuth client secret of the application's registration with Google (pairs with APP_STAFF_OIDC_CLIENT_ID)."
    }
    APP_STAFF_OIDC_ADMIN_CREDENTIALS = {
      name    = "staff-oidc-admin-credentials"
      source  = "pkg/config/data.go dataConfig.StaffAdminCredentials"
      purpose = "Service-account key (JSON) with domain-wide delegation for the Admin SDK groups scope, through which the staff auth reads role groups."
    }
  }

  secret_versions = lookup(var.secret_versions, var.environment, {})

  # Only a secret with a pinned version is mounted.
  mounted_secrets = {
    for key, s in local.secrets : key => merge(s, { version = local.secret_versions[key] })
    if contains(keys(local.secret_versions), key)
  }

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

  # site.go: PORT is set by Cloud Run itself (reserved; setting it is an
  # error) and APP_CONSOLE_DIST is where the image put the bundle, a build
  # detail the Dockerfile owns. Neither is set here.
  service_env = merge(local.core_env, local.data_env, local.site_directory_env)

  # cmd/deployment/migrate reads core and data and nothing above them.
  job_env = merge(local.core_env, local.data_env, {
    APP_SERVICE_NAME = "${local.app}-migrate"
  })

  # Every resource carries these. terraform_source_path is the stack's state slot
  # in the organization's bucket (3-app/harbor) written as a label value, which
  # admits no slash (Secret Manager and Cloud Run refuse it); source_repo is the
  # application repository's name, from the placement.
  labels = {
    terraform             = "true"
    terraform_source_path = "3-app-harbor"
    source_repo           = "harbor"
    environment           = var.environment
    application           = local.app
    bedrock-lab           = "true"
  }
}
