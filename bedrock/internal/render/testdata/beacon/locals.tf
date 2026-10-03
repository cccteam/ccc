locals {
  # The application code, fixed for this stack. It is the key 2-env registered
  # the identities and the repository link under, and the segment every name
  # below carries.
  app = "beacon"

  org = data.terraform_remote_state.org.outputs
  env = data.terraform_remote_state.env.outputs

  prefix     = local.env.prefix
  project_id = local.env.project_id
  name       = "${local.prefix}-${var.environment}"

  is_prd = var.environment == "prd"

  # The promotion order, and for each environment the plan identity a pull-request
  # build plans that environment's stack as (env=identity), read from its 2-env
  # state; empty until 2-env there makes it. The triggers pass both to every build.
  environments    = ["tst", "stg", "prd"]
  plan_identities = join(",", [for env in local.environments : "${env}=${try(data.terraform_remote_state.envs[env].outputs.applications[local.app].plan_identity_email, "")}"])
  # Each environment's deployment-records bucket (_RECORDS_BUCKETS): a pull-request
  # build against a hotfix line reads each environment's live record as that
  # environment's plan identity, to say where the hotfix will be refused.
  records_buckets = join(",", [for env in local.environments : "${env}=${try(data.terraform_remote_state.envs[env].outputs.records_bucket, "")}"])

  # A pull-request stack: this code in tst with var.pull_request set, into
  # its own state. Its resources carry the short name beacon-pr<N> (2-env's wildcard
  # backend picks the Cloud Run service by that name from the hostname), and it
  # shares tst's secret containers, identity registration and registry.
  is_pr   = var.pull_request != 0
  pr_name = "${local.app}-pr${var.pull_request}"

  # The runtime accounts' IDs: by the convention, or the short name. The
  # migration has none: the pipeline runs the migrate command on the build
  # worker as the deploy identity from 2-env (local.identities).
  app_account = local.is_pr ? "${local.pr_name}-app" : "${local.name}-gbl-${local.app}-app"

  # The same accounts as members and as resource names, spelled out rather than
  # read from the account resources, so a plan knows every membership before
  # the accounts exist: the pull-request build's guard reads the pull request's
  # name off each planned change, and a member known only after apply would
  # stop the first build of every pull request.
  app_email        = "${local.app_account}@${local.project_id}.iam.gserviceaccount.com"
  app_member       = "serviceAccount:${local.app_email}"
  app_account_name = "projects/${local.project_id}/serviceAccounts/${local.app_email}"

  # The environment before this one in the promotion order (tst, stg, prd) and
  # its deployment-records bucket: the pipeline runs a release here only after
  # that environment holds a live record of it. The first environment has none.
  previous_environment    = { tst = "", stg = "tst", prd = "stg" }[var.environment]
  previous_records_bucket = try(data.terraform_remote_state.previous_env[0].outputs.records_bucket, "")

  # The service runs in both lab regions. Keyed by region code because the
  # code is what names the regional resources.
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

  # The database the site opens: the environment's own, a pull request's own,
  # or in shared mode (var.shared_database) tst's, which the pull-request
  # stack then only reads by name and grants its app identity on.
  own_database  = !(local.is_pr && var.shared_database)
  database_name = local.own_database && local.is_pr ? "${local.pr_name}-db" : "${local.name}-gbl-${local.app}-db"

  # ---------------------------------------------------------------------------
  # What the code declares, read from pkg/config by bedrock render; each entry
  # names the field it comes from.
  # ---------------------------------------------------------------------------

  # Secrets: the fields of pkg/config tagged secret:"true". One container per
  # variable per environment, named imp-<env>-gbl-beacon-<kebab of the
  # variable without its APP_ prefix>. No versions: an operator adds the value
  # (Secret Version Adder), and var.secret_versions pins which one runs.
  #
  # readers: which runtime identity mounts it. Only the service: the migrate
  # command also constructs the data level, but the session library reads the
  # cookie key (falling back to an ephemeral one), the client secret, and the
  # admin credentials only when a browser signs in, which a migration never
  # does. So the deploy identity, which runs the migrate command, holds no
  # accessor grant on a runtime secret, and the command runs with none. The
  # design brief's rule of thumb ("every process that constructs the level")
  # would grant it; this is the narrower reading, and a fork for the
  # derivation to settle.
  secrets = {
    APP_COOKIE_KEY = {
      name    = "cookie-key"
      level   = "data"
      source  = "pkg/config/data.go dataConfig.CookieKey"
      purpose = "Signs session cookies and seals list cursors: Base64 of 32+ random bytes. Rotating it ends every session and cursor."
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
    # dataConfig.SessionTimeout is left to its code default (10m).
  }

  # site.go: PORT is set by Cloud Run itself (reserved; setting it is an
  # error) and APP_CONSOLE_DIST is where the image put the bundle, a build
  # detail the Dockerfile owns. Neither is set here.
  service_env = merge(local.core_env, local.data_env)

  # cmd/deployment/migrate reads core and data and nothing above them: what the pipeline
  # runs the migrate command with on the build worker (cloud-build.tf,
  # _MIGRATE_ENV). The release's own variable is the image's, which the
  # pipeline sets from the build.
  migrate_env = merge(local.core_env, local.data_env, {
    APP_SERVICE_NAME = "${local.app}-migrate"
  })

  # The databases the migrate command reaches, by resource name (cloud-build.tf,
  # _MIGRATE_DATABASES): deploy migrate reads each as the deploy identity until its
  # grants above are in effect, since IAM makes a grant this build's apply created
  # effective seconds to minutes later.
  migrate_databases = concat(
    ["projects/${local.instance.project}/instances/${local.instance.name}/databases/${local.database_name}"],
    [],
  )

  # Every resource carries these. terraform_source_path is the stack's state slot
  # in the organization's bucket (3-app/beacon) written as a label value, which
  # admits no slash (Secret Manager and Cloud Run refuse it); source_repo is the
  # application repository's name, from the placement.
  base_labels = {
    terraform             = "true"
    terraform_source_path = "3-app-beacon"
    source_repo           = "beacon"
    environment           = var.environment
    application           = local.app
    bedrock-lab           = "true"
  }
  # A pull-request stack also carries its number, for the sweep and the bill.
  labels = merge(local.base_labels, local.is_pr ? { pull_request = tostring(var.pull_request) } : {})
}
