locals {
  # The application code, fixed for this stack. It is the key 2-env registered
  # the identities and the repository link under, and the segment every name
  # below carries.
  app = "lantrn"
  # The sites, in the generators' order, and what the stack knows of each: its hostname
  # label under the apps domain (the one placement.json declares for the site under
  # hostnames, <code>-<site> by default), the binary its service runs (the image builds
  # one per site; the entrypoint is the first site's), whether its main package speaks
  # h2c (github.com/cccteam/ccc/resource/server), and the variables its service alone carries (the bundle the
  # image holds for the site). The code names what the sites share; the site's name is
  # the second segment of every per-site resource name.
  sites = ["console", "portal"]
  site_settings = {
    console = {
      label   = "lantrn-console"
      command = "/console"
      h2c     = true
      env     = { APP_DIST = "/home/nonroot/apps/console/web/dist" }
    }
    portal = {
      label   = "lanterns"
      command = "/portal"
      h2c     = true
      env     = { APP_DIST = "/home/nonroot/apps/portal/web/dist" }
    }
  }
  # The first site's label: what names the stack's things that take one hostname (the
  # build argument hostname, the pipeline's _HOSTNAME).
  label = local.site_settings[local.sites[0]].label

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
  # build against a patch line reads each environment's live record as that
  # environment's plan identity, to say where the patch release will be refused.
  records_buckets = join(",", [for env in local.environments : "${env}=${try(data.terraform_remote_state.envs[env].outputs.records_bucket, "")}"])

  # The application's database in every other environment, env=resource name, under the
  # first generation's name (a restore puts a database into the next generation, -2, -3,
  # which the reader probes). The migration guard reads each environment's migration
  # version from it on a consolidation pull request, through the migrations_reader
  # database role the application's first migration creates, which 2-env (an
  # environment's own instance) and 2-spn (the shared instance) grant tst's
  # deploy identity on the application's databases; the tag build's migrate step reads
  # its own environment's the same way, as database admin.
  migration_databases = join(",", [for env in local.environments : "${env}=projects/${try(data.terraform_remote_state.envs[env].outputs.spanner_instance.project, "")}/instances/${try(data.terraform_remote_state.envs[env].outputs.spanner_instance.name, "")}/databases/${local.prefix}-${env}-gbl-${local.app}-db" if env != var.environment])

  # A pull-request stack: this code in tst with var.pull_request set, into
  # its own state. Its resources carry the short name lantrn-pr<N> (2-env's wildcard
  # backend picks the Cloud Run service by that name from the hostname), and it
  # shares tst's secret containers, identity registration and registry.
  is_pr   = var.pull_request != 0
  pr_name = "${local.app}-pr${var.pull_request}"
  # Each site's pull-request hostname label and Cloud Run service name, the same word:
  # 2-env's pull-request backend picks the service by the hostname's first label.
  site_pr_hosts = { for site, s in local.site_settings : site => "${s.label}-pr${var.pull_request}" }
  pr_host       = local.site_pr_hosts[local.sites[0]]

  # Which spans the processes record: every one in a pull-request stack and in
  # tst, where every request should be traceable; beyond them the
  # edge's choice, a request Google's edge sampled and nothing else.
  trace_sampling = local.is_pr || var.environment == "tst" ? "all" : "edge"

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

  # The invoker identity Cloud Scheduler calls the scheduled routes as
  # (scheduler.tf), spelled out the same way. An environment's stack alone
  # creates it: a pull-request stack has no scheduler jobs.
  scheduler_account = "${local.name}-gbl-${local.app}-sched"
  scheduler_email   = "${local.scheduler_account}@${local.project_id}.iam.gserviceaccount.com"

  # The environment before this one in the promotion order (tst, stg, prd) and
  # its deployment-records bucket: the pipeline runs a release here only after
  # that environment holds a live record of it. The first environment has none.
  previous_environment    = { tst = "", stg = "tst", prd = "stg" }[var.environment]
  previous_records_bucket = try(data.terraform_remote_state.previous_env[0].outputs.records_bucket, "")

  # The service runs in the organization's two regions. Keyed by
  # region code because the code is what names the regional resources.
  regions = {
    (local.env.region_code)           = local.env.region
    (local.env.secondary_region_code) = local.env.secondary_region
  }
  primary_region_code = local.env.region_code
  primary_region      = local.env.region

  # Each site's service per region, keyed <site>-<region code>: what the Cloud Run
  # services, the endpoint groups and the pipeline's _SERVICES are made from.
  services = {
    for pair in setproduct(local.sites, keys(local.regions)) : "${pair[0]}-${pair[1]}" => {
      site   = pair[0]
      code   = pair[1]
      region = local.regions[pair[1]]
    }
  }

  # The most instances the service runs per region in this environment: the
  # placement's cap (maxInstances), which bounds what the environment can cost
  # when traffic rises; null in an environment the placement does not cap,
  # which leaves Cloud Run's default.
  max_instances = lookup({ tst = 2, stg = 2, prd = 4 }, var.environment, null)

  # What 2-env registered for this application.
  identities = local.env.applications[local.app]
  instance   = local.env.spanner_instance

  # The image path prefix 2-shr publishes for this application
  # (image_paths: <registry hostname>/<shr project>/<repository>); the
  # pipeline appends /<image>:<tag>. Null until 2-shr knows the application.
  registry = try(data.terraform_remote_state.shr.outputs.image_paths[local.app], null)

  # Each site's hostnames in this environment, the canonical first (var.hostnames), or
  # the pull-request stack's one per site; hostnames is every site's, the first site's
  # first: what the stack serves as a whole.
  site_hostnames = local.is_pr ? { for site, host in local.site_pr_hosts : site => ["${host}.impulseframework.dev"] } : var.hostnames[var.environment]
  hostnames      = flatten([for site in local.sites : local.site_hostnames[site]])
  # The next revision's hostnames, per site and as a whole, the first label with -next:
  # what the hook before traffic calls while the old revision still serves. None for a
  # pull-request stack.
  site_next_hostnames = { for site in local.sites : site => local.is_pr ? [] : [for host in local.site_hostnames[site] : replace(host, "/^([^.]+)\\./", "$1-next.")] }
  next_hostnames      = flatten([for site in local.sites : local.site_next_hostnames[site]])

  # The database the site opens: the environment's own, a pull request's own,
  # or in shared mode (var.shared_database) tst's, which the pull-request
  # stack then only reads by name and grants its app identity on.
  own_database = !(local.is_pr && var.shared_database)
  # The database by generation (var.database_generation): the first is named
  # for the environment and the application, and each rollback restores a
  # backup into the next, "-2", then "-3" (spanner.tf). The current one is what
  # the site, the migrate command and the grants use.
  database_base = local.own_database && local.is_pr ? "${local.pr_name}-db" : "${local.name}-gbl-${local.app}-db"
  database_name = var.database_generation == 1 ? local.database_base : "${local.database_base}-${var.database_generation}"
  # How far back Spanner keeps the database's row versions, from
  # placement.json (spannerRetention; seven days, the most Spanner allows,
  # unless it says otherwise): how far back a backup as of a moment and a
  # point-in-time rollback reach.
  spanner_retention = { tst = "7d", stg = "7d", prd = "7d" }

  # The Firestore database (LiveSettings.DatabaseID), by id; a pull-request
  # stack has its own.
  firestore_database_id = local.is_pr ? "${local.pr_name}-fs" : "${local.name}-gbl-${local.app}-fs"
  # The browser's web API key (LiveSettings.APIKey), by name (firestore.tf); a
  # pull-request stack has its own.
  firebase_key_name = local.is_pr ? "${local.pr_name}-firebase-${random_id.stack.hex}" : "${local.name}-gbl-${local.app}-firebase-${random_id.stack.hex}"

  # ---------------------------------------------------------------------------
  # What the code declares, read from pkg/config by bedrock render; each entry
  # names the field it comes from.
  # ---------------------------------------------------------------------------

  # Secrets: the fields of pkg/config tagged secret:"true" or secret:"generated".
  # One container per variable per environment, named
  # imp-<env>-gbl-lantrn-<kebab of the variable without its APP_ prefix>. No
  # versions here: someone adds the value (Secret Version Adder) and
  # var.secret_versions pins which one runs, or, for a generated secret, the
  # pipeline adds the first version and derives the pin at each build
  # (var.generated_secret_versions; secret-manager.tf, "Generated secrets").
  #
  # readers: which runtime identity mounts it. The service; and the migrate
  # command, which also constructs the data level, reads the generated password
  # of each auth's first administrator (migrate_secrets, below), which it seeds
  # the administrator user with while the auth's users table holds no rows. So
  # the deploy identity, which runs the migrate command, holds accessor on
  # those containers alone and on no other runtime secret: the session library
  # reads the cookie key (falling back to an ephemeral one), the client secrets
  # and the people's credentials only when a browser signs in, which a
  # migration never does.
  secrets = {
    APP_COOKIE_KEY = {
      name    = "cookie-key"
      level   = "data"
      source  = "pkg/config/data.go dataConfig.CookieKey"
      purpose = "Signs session cookies and seals list cursors: Base64 of 32+ random bytes. Rotating it ends every session and cursor."
      # generated: the tooling makes the value (secret:"generated"); nobody types it.
      generated = true
    }
    APP_STAFF_ADMINISTRATOR_PASSWORD = {
      name    = "staff-administrator-password"
      level   = "data"
      source  = "pkg/config/data.go dataConfig.StaffAdministratorPassword"
      purpose = "StaffAdministratorPassword is the password of the staff auth's first administrator (staff.Administrator), which the migrate command seeds while the auth's users table holds no rows: a secret the stack generates, read like any other setting, that nobody types and the first sign-in changes."
      # generated: the tooling makes the value (secret:"generated"); nobody types it.
      generated = true
    }
  }

  # The container each secret lives in, by the naming convention: this
  # environment's, which a pull-request stack shares with tst and never creates.
  secret_ids = { for key, s in local.secrets : key => "${local.name}-gbl-${local.app}-${s.name}" }

  # The secrets the tooling makes (secret:"generated"): their entry in
  # var.secret_versions is the word generated, and their version the number the
  # pipeline's plan step derived and passed as var.generated_secret_versions.
  generated_secrets = { for key, s in local.secrets : key => s if s.generated }

  # The secrets the migrate command reads (readers, above): the generated
  # password of each auth's first administrator, by variable, and the pinned
  # version of each the command runs with, read back by the pipeline from the
  # applied stack (cloud-build.tf, _MIGRATE_SECRETS). A secret with no version
  # yet (a plan by hand) is left out, and the command runs without it.
  migrate_secrets = toset(["APP_STAFF_ADMINISTRATOR_PASSWORD"])

  # The versions this environment runs: the placement's pins (a number, or
  # latest) for the secrets someone puts in place, and the derived numbers for
  # the generated ones. A generated secret the build passed no number for (a plan
  # run by hand) is not mounted, as a secret with no pin is not.
  declared_versions = lookup(var.secret_versions, var.environment, {})
  secret_versions = merge(
    { for key, v in local.declared_versions : key => v if v != "generated" },
    { for key, v in var.generated_secret_versions : key => v if contains(keys(local.generated_secrets), key) },
  )

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
    # coreConfig.LoggingProject: request logs ship to Cloud Logging here.
    GOOGLE_CLOUD_LOGGING_PROJECT = local.project_id
    # coreConfig.TraceSampling: which spans are recorded (local.trace_sampling).
    APP_TRACE_SAMPLING = local.trace_sampling
    # coreConfig.AppVersion is not set here: the pipeline bakes APP_VERSION
    # into the image at build (a Dockerfile ENV from the tag), so a deploy
    # never edits the template's variables and this stack stays their owner.
  }

  data_env = {
    # DatabaseSettings, all required: the database identity.
    GOOGLE_CLOUD_SPANNER_PROJECT       = local.instance.project
    GOOGLE_CLOUD_SPANNER_INSTANCE_ID   = local.instance.name
    GOOGLE_CLOUD_SPANNER_DATABASE_NAME = local.database_name
    # dataConfig.SessionTimeout is left to its code default (10m).
  }

  # LiveSettings.DatabaseID: the Firestore database (firestore.tf), for every
  # process that constructs the data level, the migrate command too:
  # the level opens the database when it is constructed, and the release's role
  # migration signals the running instances through it.
  # LiveSettings.APIKey: the web API key the browser presents to sign in
  # (firestore.tf), a public value by design, beside it.
  # LiveSettings.ProjectID: the database's project, this environment's; the
  # Spanner project is the shared instance's in an environment that shares one.
  firestore_env = {
    GOOGLE_CLOUD_FIRESTORE_PROJECT = local.project_id
    APP_FIRESTORE_DATABASE         = google_firestore_database.firestore.name
    # The provider marks the key string sensitive; it is the public value every
    # browser receives, and marked it would make the substitutions output below
    # sensitive whole, which the pipeline reads for the migrate command.
    APP_FIREBASE_API_KEY = nonsensitive(google_apikeys_key.firebase.key_string)
  }

  # APP_SCHEDULER_INVOKER: the invoker identity's email, which the framework requires
  # of the token on every call to a scheduled route (scheduler.tf). No configuration
  # level declares it: the framework reads it itself. A pull-request stack leaves it
  # unset, so its scheduled routes refuse every call.
  scheduler_env = local.is_pr ? {} : {
    APP_SCHEDULER_INVOKER = local.scheduler_email
  }

  # site.go: PORT is set by Cloud Run itself (reserved; setting it is an
  # error); each site's bundle variable is its service's alone
  # (site_settings, cloud-run.tf), since the image holds one bundle per site. Neither
  # is set here.
  service_env = merge(local.core_env, local.data_env, local.firestore_env, local.scheduler_env)

  # cmd/deployment/migrate reads core and data and nothing above them: what the pipeline
  # runs the migrate command with on the build worker (cloud-build.tf,
  # _MIGRATE_ENV). The release's own variable is the image's, which the
  # pipeline sets from the build.
  migrate_env = merge(local.core_env, local.data_env, local.firestore_env, {
    APP_SERVICE_NAME = "${local.app}-migrate"
  })

  # The databases the migrate command reaches, by resource name (cloud-build.tf,
  # _MIGRATE_DATABASES): deploy migrate reads each as the deploy identity until its
  # grants above are in effect, since IAM makes a grant this build's apply created
  # effective seconds to minutes later.
  migrate_databases = concat(
    ["projects/${local.instance.project}/instances/${local.instance.name}/databases/${local.database_name}"],
    ["projects/${local.project_id}/databases/${google_firestore_database.firestore.name}"],
  )

  # Every resource carries these. terraform_source_path is the stack's state slot
  # in the organization's bucket (3-app/lantrn) written as a label value, which
  # admits no slash (Secret Manager and Cloud Run refuse it); source_repo is the
  # application repository's name, from the placement; organization is the
  # bedrock organization's prefix, which tells its projects from another's in
  # the same GCP organization.
  base_labels = {
    terraform             = "true"
    terraform_source_path = "3-app-lantrn"
    source_repo           = "lantern"
    organization          = "imp"
    environment           = var.environment
    application           = local.app
    bedrock-lab           = "true"
  }
  # A pull-request stack also carries its number, for the sweep and the bill.
  labels = merge(local.base_labels, local.is_pr ? { pull_request = tostring(var.pull_request) } : {})
}
