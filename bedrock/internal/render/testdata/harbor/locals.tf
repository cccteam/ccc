locals {
  # The application code, fixed for this stack. It is the key 2-env registered
  # the identities and the repository link under, and the segment every name
  # below carries.
  app = "harbor"
  # The site's hostname label under the apps domain, the first label of every hostname
  # and of a pull-request stack's service: the one placement.json declares for the site
  # (hostnames), the code by default. The code names every resource; the label names
  # the site on the network.
  label = "harbor"

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
  # its own state. Its resources carry the short name harbor-pr<N>, the pull request's
  # name, which the pr-stack trigger's plan guard holds every one of them to, and it
  # shares tst's secret containers, identity registration and registry.
  is_pr   = var.pull_request != 0
  pr_name = "${local.app}-pr${var.pull_request}"
  # The pull-request stack's hostname label and Cloud Run service name, the same word,
  # the pull request's name: 2-env's pull-request backend picks the service by the
  # hostname's first label, so the pull request's name serves there, not the site's
  # label, which names an environment's hostnames alone.
  pr_host = local.pr_name

  # Which spans the processes record: every one in a pull-request stack and in
  # tst, where every request should be traceable; beyond them the
  # edge's choice, a request Google's edge sampled and nothing else.
  trace_sampling = local.is_pr || var.environment == "tst" ? "all" : "edge"

  # The runtime accounts' IDs: by the convention, or the short name. The
  # migration has none: the pipeline runs the migrate command on the build
  # worker as the deploy identity from 2-env (local.identities).
  app_account  = local.is_pr ? "${local.pr_name}-app" : "${local.name}-gbl-${local.app}-app"
  jobs_account = local.is_pr ? "${local.pr_name}-jobs" : "${local.name}-gbl-${local.app}-jobs"

  # The same accounts as members and as resource names, spelled out rather than
  # read from the account resources, so a plan knows every membership before
  # the accounts exist: the pull-request build's guard reads the pull request's
  # name off each planned change, and a member known only after apply would
  # stop the first build of every pull request.
  app_email         = "${local.app_account}@${local.project_id}.iam.gserviceaccount.com"
  app_member        = "serviceAccount:${local.app_email}"
  app_account_name  = "projects/${local.project_id}/serviceAccounts/${local.app_email}"
  jobs_email        = "${local.jobs_account}@${local.project_id}.iam.gserviceaccount.com"
  jobs_member       = "serviceAccount:${local.jobs_email}"
  jobs_account_name = "projects/${local.project_id}/serviceAccounts/${local.jobs_email}"

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

  # The service runs in the organization's two regions; the job process's
  # job in the primary only (a job runs once, from one place). Keyed by
  # region code because the code is what names the regional resources.
  regions = {
    (local.env.region_code)           = local.env.region
    (local.env.secondary_region_code) = local.env.secondary_region
  }
  primary_region_code = local.env.region_code
  primary_region      = local.env.region

  # The most instances the service runs per region in this environment: the
  # placement's cap (maxInstances), which bounds what the environment can cost
  # when traffic rises; null in an environment the placement does not cap,
  # which leaves Cloud Run's default.
  max_instances = lookup({ tst = 2, stg = 2, prd = 2 }, var.environment, null)

  # What 2-env registered for this application.
  identities = local.env.applications[local.app]
  instance   = local.env.spanner_instance

  # The image path prefix 2-shr publishes for this application
  # (image_paths: <registry hostname>/<shr project>/<repository>); the
  # pipeline appends /<image>:<tag>. Null until 2-shr knows the application.
  registry = try(data.terraform_remote_state.shr.outputs.image_paths[local.app], null)

  hostnames = local.is_pr ? ["${local.pr_host}.impulseframework.dev"] : var.hostnames[var.environment]
  # The next revision's hostnames, the first label with -next: what the hook before
  # traffic calls while the old revision still serves. None for a pull-request stack.
  next_hostnames = local.is_pr ? [] : [for host in local.hostnames : replace(host, "/^([^.]+)\\./", "$1-next.")]

  # From .envrc.template: APP_MEMBERS_DIRECTORY_REDIRECT_URL is the browser-facing
  # callback of the members auth's directory registration, the route the generated router (pkg/router)
  # registers as GET /portal/api/user/directory/callback, on the environment's canonical host.
  members_directory_redirect_url = "https://${local.hostnames[0]}/portal/api/user/directory/callback"
  # The front-channel logout the app registration names, GET /portal/api/user/directory/logout.
  members_directory_logout_url = "https://${local.hostnames[0]}/portal/api/user/directory/logout"

  # From .envrc.template: APP_STAFF_DIRECTORY_REDIRECT_URL is the browser-facing
  # callback of the staff auth's directory registration, the route the generated router (pkg/router)
  # registers as GET /api/user/directory/callback, on the environment's canonical host.
  staff_directory_redirect_url = "https://${local.hostnames[0]}/api/user/directory/callback"

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

  # Where the file stores live: with the database. tst's instance
  # is regional, so its buckets are regional too, named by the region's code;
  # the shared instance's configuration spans the organization's two regions,
  # so the buckets there are dual-region over the same two, named after the
  # configuration (2-env's spanner_instance output carries it). A stack
  # rendered by an older bedrock named them gbl in the primary region; the
  # first apply with this one replaces them, which is why the rename came
  # while no application held files.
  spanner_config      = local.env.spanner_instance.config
  files_regional      = startswith(local.spanner_config, "regional-")
  files_region        = trimprefix(local.spanner_config, "regional-")
  files_location_code = local.files_regional ? { for code, name in local.regions : name => code }[local.files_region] : local.spanner_config
  files_location      = local.files_regional ? local.files_region : lookup({ nam = "US", eur = "EU", asia = "ASIA" }, regex("^[a-z]+", local.spanner_config), "US")

  # The default file store (dataConfig.FileStore): the environment's, named for the
  # environment, where the database is, the application, the store and the
  # environment project's number, which makes the name unique. A pull-request
  # stack with its own database has its own; one sharing tst's
  # database uses tst's bucket, since the rows it reads name
  # objects there (storage.tf grants its identities on it).
  files_bucket_name = "${local.name}-${local.files_location_code}-${local.own_database && local.is_pr ? local.pr_name : local.app}-files-${local.env.project_number}"

  # The task queue (dataConfig.TasksQueue), by name and as the Cloud Tasks API
  # names it: tst's for a pull-request stack, which enqueues on it (tasks.tf).
  tasks_queue_name = "${local.name}-${local.primary_region_code}-${local.app}-tasks-${random_id.stack.hex}"
  tasks_queue      = "projects/${local.project_id}/locations/${local.primary_region}/queues/${local.tasks_queue_name}"

  # The Firestore database (FirestoreSettings.DatabaseID), by id; a pull-request
  # stack has its own.
  firestore_database_id = local.is_pr ? "${local.pr_name}-fs" : "${local.name}-gbl-${local.app}-fs"
  # The browser's web API key (FirestoreSettings.APIKey), by name (firestore.tf); a
  # pull-request stack has its own.
  firebase_key_name = local.is_pr ? "${local.pr_name}-firebase-${random_id.stack.hex}" : "${local.name}-gbl-${local.app}-firebase-${random_id.stack.hex}"

  # The job process's template job (cmd/jobs), in the primary region, by name
  # and as the Cloud Run API names it; each build's job is named after it with the
  # build's version, and the service carries this name as APP_JOBS_TEMPLATE
  # (jobs_template_env below), which the framework names that job from.
  jobs_job_name = local.is_pr ? "${local.pr_name}-jobs" : "${local.name}-${local.primary_region_code}-${local.app}-jobs"
  jobs_job      = "projects/${local.project_id}/locations/${local.primary_region}/jobs/${local.jobs_job_name}"

  # ---------------------------------------------------------------------------
  # What the code declares, read from pkg/config by bedrock render; each entry
  # names the field it comes from.
  # ---------------------------------------------------------------------------

  # Secrets: the fields of pkg/config tagged secret:"true" or secret:"generated".
  # One container per variable per environment, named
  # imp-<env>-gbl-harbor-<kebab of the variable without its APP_ prefix>. No
  # versions here: someone adds the value (Secret Version Adder) and
  # var.secret_versions pins which one runs, or, for a generated secret, the
  # pipeline adds the first version and derives the pin at each build
  # (var.generated_secret_versions; secret-manager.tf, "Generated secrets").
  #
  # readers: which runtime identity mounts it. The service; and the migrate
  # command, which also constructs the data level, reads the generated password
  # of each auth's first administrator (migrate_secrets, below), which it seeds
  # the administrator user with while the auth's users table holds no rows, and
  # the mailbox's app password, which the data level opens the mail sender with.
  # So the deploy identity, which runs the migrate command, holds accessor on
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
      generated = false
    }
    APP_MAIL_APP_PASSWORD = {
      name    = "mail-app-password"
      level   = "data"
      source  = "pkg/config/data.go dataConfig.MailAppPassword"
      purpose = "A credential dataConfig.MailAppPassword reads."
      # generated: the tooling makes the value (secret:"generated"); nobody types it.
      generated = false
    }
    APP_STAFF_DIRECTORY_CLIENT_SECRET = {
      name    = "staff-directory-client-secret"
      level   = "data"
      source  = "pkg/config/data.go dataConfig.StaffDirectoryClientSecret"
      purpose = "Client secret of the staff auth's directory registration with Google (pairs with APP_STAFF_DIRECTORY_CLIENT_ID)."
      # generated: the tooling makes the value (secret:"generated"); nobody types it.
      generated = false
    }
    APP_MEMBERS_DIRECTORY_CLIENT_SECRET = {
      name    = "members-directory-client-secret"
      level   = "data"
      source  = "pkg/config/data.go dataConfig.MembersDirectoryClientSecret"
      purpose = "Client secret of the members auth's directory registration with Microsoft (pairs with APP_MEMBERS_DIRECTORY_CLIENT_ID)."
      # generated: the tooling makes the value (secret:"generated"); nobody types it.
      generated = false
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
  # password of each auth's first administrator and the mailbox's app password,
  # by variable, and the pinned version of each the command runs with, read
  # back by the pipeline from the applied stack (cloud-build.tf,
  # _MIGRATE_SECRETS). A secret with no version yet (a plan by hand) is left
  # out, and the command runs without it.
  migrate_secrets = toset(["APP_MAIL_APP_PASSWORD"])

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
    # coreConfig.LoggingProject: request logs ship to Cloud Logging here.
    GOOGLE_CLOUD_LOGGING_PROJECT = local.project_id
    # coreConfig.TraceSampling: which spans are recorded (local.trace_sampling).
    APP_TRACE_SAMPLING = local.trace_sampling
    # coreConfig.AppVersion is not set here: the pipeline bakes APP_VERSION
    # into the image at build (a Dockerfile ENV from the tag), so a deploy
    # never edits the template's variables and this stack stays their owner.
  }

  data_env = {
    # SpannerSettings, all required: the database identity.
    GOOGLE_CLOUD_SPANNER_PROJECT       = local.instance.project
    GOOGLE_CLOUD_SPANNER_INSTANCE_ID   = local.instance.name
    GOOGLE_CLOUD_SPANNER_DATABASE_NAME = local.database_name
    # The directory registrations' settings the session library reads when it makes
    # each registration, at construction, so every data-level process gets them.
    APP_MEMBERS_DIRECTORY_TENANT_ID   = var.members_directory_tenant_id
    APP_STAFF_DIRECTORY_HOSTED_DOMAIN = var.staff_directory_hosted_domain
    APP_STAFF_DIRECTORY_GROUP_PREFIX  = var.staff_directory_group_prefix
    # The first administrator of each auth whose roles the application assigns: the
    # sign-in name the migrate command seeds the administrator role membership under
    # while the auth's users table holds no rows.
    APP_MEMBERS_ADMINISTRATOR = var.members_administrator[var.environment]
    # dataConfig.PublicURL: the application's address as a browser reaches it, the
    # canonical hostname; every link the auths mail opens the login page under it.
    APP_PUBLIC_URL = "https://${local.hostnames[0]}"
    # dataConfig.MailMailbox: the Google Workspace mailbox the auths' mail is sent as
    # (var.mail_mailbox), empty until the organization names one, when the
    # application writes its mail to the log and sends nothing.
    APP_MAIL_MAILBOX = var.mail_mailbox
    # dataConfig.SessionTimeout is left to its code default (10m).
  }

  # The directory registrations' settings the served site needs and a migration
  # does not. They live at the data level in the code, but a process that never
  # signs anyone in has no use for them, so the migrate command goes without.
  site_directory_env = {
    # The members auth's directory registration: the client id, public half of the
    # registration, per environment; the redirect URL, built from the canonical
    # hostname.
    APP_MEMBERS_DIRECTORY_CLIENT_ID    = var.members_directory_client_id[var.environment]
    APP_MEMBERS_DIRECTORY_REDIRECT_URL = local.members_directory_redirect_url
    # The staff auth's directory registration: the client id, public half of the
    # registration, per environment; the redirect URL, built from the canonical
    # hostname; how far the groups read reaches, direct or nested.
    APP_STAFF_DIRECTORY_CLIENT_ID    = var.staff_directory_client_id[var.environment]
    APP_STAFF_DIRECTORY_REDIRECT_URL = local.staff_directory_redirect_url
    APP_STAFF_DIRECTORY_GROUP_LOOKUP = var.staff_directory_group_lookup
  }

  # siteConfig.Template: the job process's template job (cloud-run.tf), which
  # the pipeline copies per build into <template>-<version key> on that build's
  # image. The framework's job driver (resource/jobs/cloudrun), whose settings the
  # site level embeds, reads it with the version the image bakes in and
  # names the job of its own build from the two, so each revision starts the job of
  # its build, and a traffic rollback to an earlier revision starts that revision's.
  # A pull-request stack's is its own template.
  jobs_template_env = {
    APP_JOBS_TEMPLATE = local.jobs_job
  }

  # The file stores (storage.tf), each as the gs:// URL of its bucket, for the
  # processes that construct its level and run the application's own code:
  # dataConfig.FileStore, the default file store (data level).
  files_env = {
    APP_FILE_STORE = "gs://${local.files_bucket_name}"
  }

  # dataConfig.TasksQueue: the task queue (tasks.tf), for the processes that
  # construct the data level and run the application's own code.
  tasks_env = {
    APP_TASKS_QUEUE = local.tasks_queue
  }

  # FirestoreSettings.DatabaseID: the Firestore database (firestore.tf), for every
  # process that constructs the data level, the migrate command too:
  # the level opens the database when it is constructed, and the release's role
  # migration signals the running instances through it.
  # FirestoreSettings.APIKey: the web API key the browser presents to sign in
  # (firestore.tf), a public value by design, beside it.
  # FirestoreSettings.ProjectID: the database's project, this environment's; the
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
  # error) and APP_CONSOLE_DIST and APP_PORTAL_DIST is where the image put the bundle, a build
  # detail the Dockerfile owns. Neither is set here.
  service_env = merge(local.core_env, local.data_env, local.site_directory_env, local.files_env, local.tasks_env, local.firestore_env, local.scheduler_env, local.jobs_template_env)

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

  # cmd/jobs reads core and data and nothing above them: the job process, the
  # application's own code as a Cloud Run job.
  jobs_env = merge(local.core_env, local.data_env, local.files_env, local.tasks_env, local.firestore_env, {
    APP_SERVICE_NAME = "${local.app}-jobs"
  })

  # Every resource carries these. terraform_source_path is the stack's state slot
  # in the organization's bucket (3-app/harbor) written as a label value, which
  # admits no slash (Secret Manager and Cloud Run refuse it); source_repo is the
  # application repository's name, from the placement; organization is the
  # bedrock organization's prefix, which tells its projects from another's in
  # the same GCP organization.
  base_labels = {
    terraform             = "true"
    terraform_source_path = "3-app-harbor"
    source_repo           = "harbor"
    organization          = "imp"
    environment           = var.environment
    application           = local.app
    bedrock-lab           = "true"
  }
  # A pull-request stack also carries its number, for the sweep and the bill.
  labels = merge(local.base_labels, local.is_pr ? { pull_request = tostring(var.pull_request) } : {})
}
