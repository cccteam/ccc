# bedrock

`bedrock` is the command-line tool for the infrastructure of Impulse applications: the
organization foundation an application deploys into, the application's own stack, the
pipeline that deploys it, and the operations around a deployment (secrets, hotfix lines,
repository rules, migration renumbering).

The tool keeps no record of its own. It derives what an application needs from the
application's own code, through impulse's reader (the config struct tags, the main
packages, the auths, the generated router, the Dockerfile), and from a placement file that
records what the code cannot know: the organization's naming prefix, its environments and
regions, its domains, the bedrock the pipeline runs. The code and the placement are the
inputs; the stack and the pipeline are the output, rewritten on every render and
compared by every check.

## Install

Each release of the tool is a GitHub Release of this repository tagged `bedrock/vX.Y.Z`,
carrying one static binary per platform the tool runs on (`bedrock-linux-amd64`,
`bedrock-linux-arm64`, `bedrock-darwin-arm64`) and `checksums.txt`, the SHA-256 of each.
Download the binary for your machine from the release page, verify it against the
checksum and put it on your PATH. A pushed commit installs with Go:
`go install github.com/cccteam/ccc/bedrock@<commit>`. Or build it from a checkout of this
repository (`go build -o bedrock .` in `ccc/bedrock`).

Which bedrock an application runs is not the installed one's choice. The application's
placement pins it (`bedrockVersion`), one of two ways:

- **A release**, with the SHA-256 of its linux/amd64 binary (`bedrockSha256`). The
  pipeline and the infrastructure check download that binary and verify it against the
  checksum.
- **A commit pin**: the pseudo-version the Go module proxy gives a pushed commit of this
  module (`v0.0.0-lab.1.0.20260928222237-58b211dce544`), with no `bedrockSha256`. The
  pipeline and the infrastructure check build it with `go install` in a Go image pinned by
  digest, and Go's checksum database verifies the module in place of a checksum in the
  placement. This runs bedrock from an unreleased commit, for trying a change in an
  application before it is released; it costs each pipeline build and each hourly sweep
  a minute or two of building.

`render` and `check` refuse to run a release, or a commit installed with `go install`,
against a placement that pins another bedrock, so the installed bedrock is kept at the
pin and `bedrock upgrade` moves it. A build from a checkout is nobody's pin and renders
any pin.

A release is made in four steps, all in one run of this repository's release-please
workflow when bedrock's release pull request merges:

- **Draft**: release-please, as the release app, creates the tag `bedrock/vX.Y.Z` at the
  release commit and a draft release on it (`draft` and `force-tag-creation` in the
  bedrock block of `release-please-config.json`).
- **Binaries**: a job of the same run (`.github/workflows/bedrock-release.yml`) checks out
  the tag, refuses it if it names another commit than the release, builds the binaries
  there, and uploads them and `checksums.txt` to the draft.
- **Publish**: the job reads the draft's assets back and publishes only when they are
  exactly the files it built, each with the digest GitHub reports matching
  `checksums.txt`; while the release is a draft, anyone with write access could add or
  replace an asset.
- **Locked**: with the repository's immutable releases on, the published release and its
  assets cannot change.

A draft is not a release `bedrock upgrade` finds, so no application can move to a release
before its binaries are on it. If the job fails, the draft stays a draft: re-run the
run's failed jobs, or start the `bedrock release` workflow by hand from the Actions tab
with the tag, and it builds, checks and publishes that draft. Starting release-please
again does not, because its pull request is already labeled as tagged.

## Vocabulary

- **Organization foundation**: the six layers of the CCC provisioning model, one OpenTofu
  root each, that every application deploys into: `0-bootstrap` (the state bucket and the
  boot identity), `1-org` (the folders, the environment projects and the organization
  policies), `2-shr` (the shared project: the registries), `2-spn` (the
  Spanner instances), `2-net` (the load balancer, the certificates and the hostnames) and
  `2-env` (per environment: the application identities and their grants, the records
  bucket, the repository links, the team group's release approval and entitlements).
  Rendered by `bedrock org` from the organization's placement into the organization's
  infrastructure repository.
- **Application stack**: the OpenTofu root under the application repository's
  `infrastructure/` directory (or the one layer under `3-app/` of an infrastructure
  repository): the Cloud Run services (their port named `h2c` when the site's main
  package imports the framework's server, `resource/server`, which speaks HTTP/1.1 and
  unencrypted HTTP/2 on one listener, so bodies over Cloud Run's 32 MiB HTTP/1 bound
  pass; HTTP/1 otherwise), the database, the secret
  containers, the backend service, the triggers, the sweep schedule and the scheduled
  routes' jobs, per environment.
  Rendered by `bedrock render` from the code and the application's placement.
- **Placement**: `placement.json` beside the stack. For an application it records the
  organization's facts the stack needs, the bedrock the pipeline runs
  (`bedrockVersion`, and `bedrockSha256` for a release) and, when the organization
  sets one, the Cloud Build machine the pipeline's builds run on (`buildMachine`, one
  of Cloud Build's machine names; absent, Cloud Build's default), the most Cloud Run
  instances the service may run per region in an environment (`maxInstances`, by
  environment name; an environment it leaves out has no cap, and Cloud Run's default
  applies), the backend service's outlier detection thresholds (`outlierDetection`;
  absent, bedrock's defaults) and the Cloud Armor policy an environment may turn on
  (`cloudArmor`; absent, bedrock's defaults); for an organization it records the prefix,
  the domains, the organization and billing ids, the regions, the Spanner configuration,
  the outlier detection thresholds, the Cloud Armor policy its applications start with,
  the GitHub organization and its machine account, the
  applications, the environment projects and
  each environment's team group (`teamGroups`: the group whose members approve the
  environment's releases and ask for its entitlements, with `entitlementDurations` for
  the longest grants).
- **Environment**: `tst`, `stg` and `prd`, in promotion order. A pull request deploys to
  the first; a release goes through them in order, each after it is live in the previous
  one.
- **Owned file**: a file bedrock writes on every render and compares on every check: the
  stack's `.tf` files and its README, the pipeline files at the application root
  (`cloudbuild.yaml`, `cloudbuild-sweep.yaml`), the generate-time step
  (`cmd/generate/bedrock.go`, which runs the pinned bedrock through `go run` so `go
  generate` needs no bedrock installed) and the GitHub workflows (`.github/workflows/infrastructure.yml`,
  `release-please.yml`). A person never edits one; the code or the placement changes and
  the file is rendered again.
- **Seeded file**: a file bedrock writes once when it is absent and then leaves to a
  person: `terraform.tfvars` (the placement values per environment: the pins, the build
  secrets, the substitutions), the stack's `.gitignore`, and at the application root the
  Dockerfile with its `.dockerignore` and release-please's two files,
  `release-please-config.json` and `.release-please-manifest.json`.
- **Pipeline**: `cloudbuild.yaml`, the deploy sequence Cloud Build runs on a pull request's
  `/gcbrun` comment and on a release tag. Every step but the image build is a `bedrock
  deploy` command run by the pinned bedrock release; the application customizes it
  through hooks, build secrets, declared substitutions and its Dockerfile, never by editing
  the file. The steps run in order, except that in a pull-request build the image lane
  (the release check and the image build) runs beside the pull request's stack lane (its
  plan, guard and apply), the two joining before the application deploys.
- **Hook**: a shell script the application commits at `infrastructure/hooks/<stage>.sh`,
  run at that stage of the pipeline as the deploy identity with the build's facts in its
  environment: `before-build`, `before-migrate`, `after-migrate`, `before-traffic`,
  `after-traffic`, `after-down`. No file, nothing runs.
- **Deploy identity** and **apply identity**: per application and environment, the
  service account the pipeline runs as (`<prefix>-<env>-gbl-<app>-deploy`) and the one
  that applies the stack (`<prefix>-<env>-gbl-<app>-tofu`), both created by `2-env`.
- **Deployment record**: what a build deployed, written to the environment's records
  bucket as `<app>/<env>/<release>/<build id>.json`; the next environment's gate reads
  it, and the pipeline reads the newest one to tell a stale pull-request database.
- **Breaking release** and **maintenance window**: a release is breaking for an
  environment when the oldest release its outlets still answer (the release file beside
  the generated router) is newer than the release the environment runs, and deploys
  behind the maintenance page inside the environment's maintenance window, the time the
  client allows for a release that interrupts service (`placement.json`, `maintenance`).
- **Release**: a tag `v<major>.<minor>.<patch>` cut by release-please as the release app;
  the pipeline accepts a release from that author only. A **hotfix line** is the branch
  `hotfix/<major>.<minor>.x` on which release-please releases the line's next patch
  versions while the default branch moves on. A feature release advances the minor,
  below 1.0 too (release-please's `bump-patch-for-minor-pre-major` off; `check` refuses
  it on), so a feature production does not run yet opens a new line; a fix is a patch on
  the line, a breaking change advances the minor.

## The application's placement

An application's `placement.json`, in its repository's `infrastructure` directory beside
the stack, records what the code cannot know. `bedrock org register <app> <checkout>`
writes the first one from the organization's placement (`bedrock org`, below), and the
application's team owns it from then on: `bedrock upgrade` moves the pin, and a person
writes the optional fields. A field the file does not know is refused, and so is a value
of the wrong shape. Each field, what it means, where the first placement takes it from,
and what its absence means:

- `prefix`: the organization's naming prefix; every resource name starts with
  `<prefix>-<env>`. From the organization's `prefix`. Required.
- `environments`: the environments in promotion order; pull requests deploy to the first,
  and the last is production. The model's three, `tst`, `stg` and `prd`. Required.
- `regions`: the Cloud Run regions, each a `name` and the `code` regional resources are
  named by; the first is the primary. From the organization's `regions`. Required.
- `appsDomain`: the domain the application's hostnames are under: `<app>-<env>.<domain>`,
  and `<app>.<domain>` in production. From the organization's `appsDomain`. Required.
- `hostedDomain`: the Google Workspace domain a directory sign-in limits logins to. From
  the organization's `organizationDomain`. Required.
- `stateBucket`: the Cloud Storage bucket holding every layer's state, which the stack's
  backend writes to and its reads of the organization's layers read from. From the
  organization's `stateBucket`. Required.
- `placeholderImage`: the image the services and the job are created with before the
  first deploy, after which the pipeline owns the image. Cloud Run's public sample,
  `us-docker.pkg.dev/cloudrun/container/hello`. Required.
- `defaultBranch`: the branch pull requests target and releases are cut on. From the
  organization's `githubDefaultBranch`. Required.
- `repository`: the application repository's name, the `source_repo` label every
  resource carries. The application's code, the name `1-org` gives its repository.
  Required.
- `releaseApp`: the slug of the GitHub App release-please runs as; the pipeline accepts
  a release tag only from a GitHub Release that app authored. From the organization's
  `githubReleaseAppSlug`. Required.
- `bedrockVersion` and `bedrockSha256`: the bedrock the pipeline and the infrastructure
  check run (Install, above): a release with the SHA-256 of its linux/amd64 binary, or a
  commit pin with none. The bedrock that ran `org register`; `bedrock upgrade` moves it.
  Both absent, the placement is unpinned, and `render` and `check` refuse it.
- `labels`: labels on every resource beside the ones the stack derives. From the
  organization's `labels`. Absent, none.
- `seed`: the environments whose database takes the development seed (`schema/devseed`)
  at a release build. The first environment. Absent, none; production is never seeded.
- `releaseBackups`: the environments whose release builds keep a backup of the database
  as of the cut, the moment before the release's migrations run, for fourteen days, so
  `bedrock restore --before <release>` can return the database to the state before that
  release (bedrock restore, below). Not written. Absent, production alone.
- `spannerRetention`: how far back each environment's database keeps its past, by
  environment (`{"prd": "7d"}`), one hour to seven days (`1h` to `168h`, or `1d` to
  `7d`): the version retention period of the Spanner database, which bounds a restore
  to a moment (`bedrock restore --at`) and what a backup taken as of a past moment can
  hold. Not written. Absent, or for an environment it leaves out, seven days.
- `projects`: each environment project's `id` and `number`, by which the operations
  workflow, started from GitHub (`bedrock restore`, `bedrock rerun`, `bedrock rollback`,
  `bedrock backups`, `bedrock maintenance`),
  names the environment's identity provider and operations identity. From the organization's
  `projects` and `projectNumbers`, 1-org's outputs. An environment without an entry
  cannot be operated from GitHub.
- `approvals`: the environments whose release waits for a person's approval in Cloud
  Build. Not written. Absent, every environment but the first.
- `maintenance`: each environment's maintenance window, the time it may take a release
  that interrupts service (Maintenance windows, below). Not written. Absent, every
  environment but production is `anytime`; production has none, and a breaking release
  to it is refused until the team writes its window, which is done before the first
  breaking release.
- `buildMachine`: the Cloud Build machine the pipeline's builds run on, one of
  `E2_MEDIUM`, `E2_STANDARD_2`, `E2_HIGHCPU_8` and `E2_HIGHCPU_32`. Not written. Absent,
  Cloud Build's default machine.
- `maxInstances`: the most Cloud Run instances the service may run per region, by
  environment (`{"prd": 10}`), at least 1 each (bedrock render, below). Not written.
  Absent, or for an environment it leaves out, no cap: Cloud Run's default maximum.
- `outlierDetection`: the thresholds by which the load balancer takes a failing region
  out of the backend service. A serverless network endpoint group, the backend of each
  region, has no health check, so without outlier detection a region that fails keeps
  its share of the requests until it recovers. From the organization's
  `outlierDetection`, when it sets one. Absent, or for a field it leaves out, the
  default; a field written is a whole number, and one that would turn the ejection off
  is refused:
  - `consecutiveErrors`: how many errors in a row (a 5xx answer, or a request that gets
    none) take a region's endpoint group out. At least 1; default 5.
  - `enforcingConsecutiveErrors`: the chance, in percent, that a group reaching that
    count is taken out. 1 to 100; default 100, every time.
  - `maxEjectionPercent`: the most of the backend's groups out at once, in percent. 1 to
    100; default 50, one region of the two.
  - `intervalSeconds`: how often, in seconds, the load balancer looks at the counts,
    taking groups out and putting back those whose time is up. At least 1; default 1.
  - `baseEjectionSeconds`: how long, in seconds, a group stays out the first time; each
    further time, that multiplied by the number of times it was taken out. At least 1;
    default 30.
- `cloudArmor`: the Cloud Armor policy the stack renders on its backend services
  (`cloud-armor.tf`), which an environment turns on in the stack's `terraform.tfvars`
  (`cloud_armor`, by environment: `"preview"` evaluates the rules and logs what each
  would have done, `"enforce"` applies them, `"off"` keeps the policy and detaches it
  from the backend services; an environment left out has no policy, an environment
  goes off before its entry is removed, since an apply that deletes the policy while
  the backend services still name it fails, and a pull-request stack never has a
  policy, since the environment layer serves previews from one backend service).
  Rules run in priority order and the first match decides: a rule
  set that reads no body (scanner detection) first, on every path; then the bypasses,
  each a route whose body is a file or a third party's rather than the application's
  JSON, allowed so that no rule below reads it, the generated router's upload and
  stored-file routes (the release file's `fileRoutes`) and the placement's; then the
  other rule sets, each scoped to the outlets' routes; and every other request allowed.
  Each rule's description names its source. From the organization's `cloudArmor`, when it
  sets one. Absent, or for a field it leaves out, the default:
  - `ruleSets`: the preconfigured rule sets the policy evaluates, in order, each a `name`
    Cloud Armor gives the set (`sqli-v33-stable`, `xss-v33-stable`, `lfi-v33-stable`,
    `rfi-v33-stable`, `rce-v33-stable`, `methodenforcement-v33-stable`,
    `scannerdetection-v33-stable`, `protocolattack-v33-stable`, `php-v33-stable`,
    `sessionfixation-v33-stable`, `java-v33-stable`, `nodejs-v33-stable`, `cve-canary`,
    `json-sqli-canary`; any other is refused) and a `sensitivity` from 1, the rules least
    likely to misfire, to 4, every rule. Absent, the reference deployment's five at
    sensitivity 1: `scannerdetection-v33-stable`, `sqli-v33-stable`, `json-sqli-canary`,
    `xss-v33-stable` and `protocolattack-v33-stable`.
  - `fieldExclusions`: the request fields a rule set does not inspect, each the
    `ruleSet` (one of the policy's), the `field` (`header`, `cookie`, `queryParam` or
    `uri`), how the `value` names it (`operator`: `equals`, the default, `startsWith`,
    `endsWith`, `contains`, or `any`, which names every field of the kind and takes no
    value), the rules it is for (`rules`, Cloud Armor's rule ids such as
    `owasp-crs-v030301-id941100-xss`; absent, every rule of the set) and a `reason`. For a
    field whose legitimate values trip a rule: a session cookie of random bytes under the
    cross-site scripting rules. Absent, none.
  - `bypasses`: the paths allowed ahead of the rule sets beside the ones derived from the
    router, each a `path` as the application mounts it, parameters in braces and a last
    segment of `*` for the subtree (`/hooks/registry`, `/streams/*`), a `method` (absent,
    every method) and a `reason`: a webhook under the Root hook whose sender signs its
    body, a media stream. A bypass the router already derives is refused, naming the
    declaration. Absent, none.
- `buildArguments`: values the stack makes in each environment that the image build
  takes as build arguments (a build argument is a `NAME=value` the image build is given
  and a Dockerfile stage reads after it declares `ARG NAME`), as a map from the argument's
  name to the value's name: `{"FIREBASE_API_KEY": "firebaseApiKey", "PROJECT_ID":
  "projectId"}`. Not written. Absent, none. A name is an uppercase identifier, other than
  `VERSION` and `COMMIT`, which the pipeline passes itself. A value is one of
  the values bedrock knows:
  - `firebaseApiKey`: the key string of the Firebase web API key the stack makes for the
    application, which the browser presents to sign in (made when the code declares
    `APP_FIREBASE_API_KEY`; `render` refuses it otherwise);
  - `firestoreDatabase`: the id of the application's Firestore database (made when the
    code declares `APP_FIRESTORE_DATABASE`; `render` refuses it otherwise);
  - `projectId`: the environment project's id;
  - `environment`: the environment's name;
  - `hostname`: the service's canonical hostname in the environment.

  Such a value exists only once the stack is applied (the Firebase key is made by the
  apply), so it cannot be written per environment the way a declared substitution in
  `terraform.tfvars` is. The stack carries each on its triggers as `_BUILD_ARG_<NAME>`,
  read from its own resources, and the image build passes it as `--build-arg NAME=value`
  (`bedrock deploy`, below). The Dockerfile consumes it in the stage that builds with it:

  ```dockerfile
  FROM node AS web-build-env
  ARG FIREBASE_API_KEY
  RUN bun run build    # the build script reads FIREBASE_API_KEY from its environment
  ```

  `bedrock check` refuses a declared argument no stage of the Dockerfile declares. A build
  argument is part of the layer cache key: every layer after the `ARG` is built again
  when the value differs, so each environment builds those layers with its own value and
  never takes another environment's. A dependency stage that sees an argument (through
  the stage its `FROM` names) caches under a digest of the value. A build secret never
  carries such a value: a secret is not part of the cache key, and a layer built with
  one environment's value would be served to another. A release build passes what its
  trigger carries, as the stack's last apply set it, so an argument a release declares
  first reaches the image from the next release on, and the build's log says so; a
  pull-request build passes the pull request's own stack's values.

## bedrock render

`render` reads the application (its config struct tags, its main packages, its auths, its
generated router, its Dockerfile) and the placement, and writes the stack: the owned
files rewritten, the seeded files written when absent.

```sh
bedrock render                       # from anywhere inside the application repository
bedrock render --app . --out infrastructure
bedrock render --placement infrastructure/placement.json
```

Run from anywhere inside the repository, it finds both directories: the stack is the
application repository's `infrastructure` directory, or the one application layer under
`3-app` of an infrastructure root; `--out` overrides. The application is read from the
repository root when the stack is in its infrastructure directory, else from the working
directory; `--app` overrides.

The service scales from zero instances. Where the placement caps an environment
(`"maxInstances": {"prd": 10}`, the most instances per region, at least 1), the
service there runs at most that many: a cap bounds what the environment can cost when
traffic rises. An environment the placement leaves out, and every environment of a
placement without `maxInstances`, has no cap of bedrock's, and Cloud Run's own default
maximum applies. The stack reads the cap of the environment it is applied in from a
local, `max_instances` in `locals.tf`, so one rendered stack serves every environment.

What the stack carries comes from declarations in the code: a config variable tagged as
a secret becomes a Secret Manager container mounted at a pinned version; a directory auth
becomes the registration variables, the redirect output and the hand steps in the README;
a main package under `cmd/deployment/migrate` becomes the migrate command, which the
pipeline takes out of each build's image and runs on the build worker as the deploy
identity, granted database admin on the application's own database by the stack; a main package
under `cmd/jobs` becomes the job process: a template Cloud Run job in the primary
region (never run, never deployed to; each build copies it into a job of its own, named
after it with the build's version, on the build's image), its runtime identity with the
site's project roles and, when it constructs the data level, the database user grant and
accessor on that level's secrets, its timeout, retries and resources as stack variables,
and the template job's name on the service as `APP_JOBS_TEMPLATE`, which the framework's
job driver (resource/jobs/cloudrun) names the job of its own build from with the version the image bakes in,
with `run.jobsExecutorWithOverrides` for the site's identity on the template job (a start passes the command's arguments as container overrides), copied by the pipeline onto
each build's job, so the running service, and only it, starts the job of its own build
through the Cloud Run API
(a schedule calls an endpoint on the service; the pipeline never runs it). The variable's
name comes from the job driver's declaration (`resource/jobs/cloudrun/declaration`), which
the served site reads by embedding `cloudrun.Settings` in a configuration level, and
`bedrock check` refuses a configuration with one half and not the other: a job process
with no level declaring the variable, or the variable declared with no job process. A method the
code marks `@schedule("<cron>", zone: "<IANA zone>")` on an `@rpc` struct, which the
generated router serves at `POST /_scheduled/<method>` and lists in its release file,
becomes one Cloud Scheduler job per environment in the primary region (`scheduler.tf`),
`<prefix>-<env>-<region>-<app>-sched-<method>`, which calls the route on the environment's
canonical hostname on its schedule, in its time zone, with an OIDC token of the stack's
invoker identity `<prefix>-<env>-gbl-<app>-sched` minted for the route's URL; the service
receives that identity's email as `APP_SCHEDULER_INVOKER`, and the framework in front of
the route admits a token Google signed for the route's URL with that email and answers
every other call 401, so the identity holds no role and Cloud Run's IAM, which the load
balancer's tag opens, is not what guards the route; a pull-request stack creates neither
the identity nor the jobs and leaves the variable unset, so its scheduled routes refuse
every call; a config variable
`APP_FILE_STORE` (the application's default file store) or `APP_FILE_STORE_<NAME>` (a
named store, `APP_FILE_STORE_DOCUMENTS`) becomes a Cloud Storage bucket where the
environment's database is (the region of a regional instance, a dual-region over the two
regions of the shared instance's configuration), one per variable, named
`<prefix>-<env>-<location code>-<app>-files-<project number>` or
`...-files-<name>-<project number>` with the name in lower case and hyphens and the
location code the region's (`uc1`) or the instance configuration's (`nam10`), uniform
access, no public access, soft delete seven days in production and none elsewhere, the
variable set to the bucket's `gs://` URL for the processes that construct its level, with
`objectUser` for the site and, when it constructs that level, the job process, as the
bucket's whole permission list (set on every apply, so Cloud Storage's default grants to
the project's basic roles are gone from it and a grant added on the bucket by hand does
not outlive the next release), the service's first revision waiting for those grants
(its `depends_on`), a pull-request stack with its own database having its own bucket and
one sharing the integration environment's database using its bucket, its identities
granted on it, since the rows it reads name objects there, and
neither the URL nor a grant for the migrate command; a config variable `APP_TASKS_QUEUE` becomes a
Cloud Tasks queue in the primary region (a pull-request stack enqueues on the
integration environment's), the variable set to its resource name, with `enqueuer` on
the queue and Service Account User on its own account for the site and, when it
constructs that level, the job process, so a task calls the application back with the
enqueuer's OIDC token; a config variable `APP_FIRESTORE_DATABASE` becomes a Firestore
database in Native mode beside the Spanner database, the variable set to its id and
`GOOGLE_CLOUD_FIRESTORE_PROJECT` to the environment project on every process that
constructs the variable's level (the site, the job process and the migrate command; the
Spanner project is the shared instance's where one is shared, so the database's project is
told on its own, and the skeleton's data level refuses a database named without it), with
`datastore.user` under a condition naming that database alone for the site and, when they
construct that level, the migrate command (the data level opens the database's live
service when it is constructed, and the release's role migration signals the running
instances through it) and the job process; the generated router's outlets become the
service's paths on the backend. The rendered README of the
stack explains every file and names the declaration it comes from.

A Firestore database also brings what the resource package's live pages need of it. The
stack reads the two files beside the schema migrations, `schema/firestore/firestore.indexes.json`
and `schema/firestore/firestore.rules` (the live package ships them and the skeleton copies
them; a declared database without either is refused by name), and applies the composite
indexes and the time-to-live policies the first declares and releases the rules of the
second to the database (`cloud.firestore/<database id>`, from an owned copy
`firestore.rules` in the stack, so a change to the rules shows as drift); it grants the
site Token Creator on its own account for the custom tokens it mints through the IAM
Credentials API, and, when the config also declares `APP_FIREBASE_API_KEY`, makes the web
API key the browser presents, restricted to the Identity Toolkit and Secure Token APIs,
and sets the variable to it. Firebase Authentication on the environment project is
`2-env`'s, initialized once with no sign-in provider.

## bedrock check

`check` renders the stack afresh and compares every owned file with the committed one,
at the stack directory and at the application root. It exits 1 when any differs or is
missing, listing each with the first line that differs: the drift between the code and
the committed infrastructure. Seeded files are a person's and are not compared.

```sh
bedrock check
bedrock check --app . --dir infrastructure
```

It also refuses:

- a schema migrations directory, or the seed directory beside it (`schema/devseed`),
  whose files do not form the sequence the migrate command applies: six-digit indexes, one
  up file per index, at most one down, contiguous from the lowest present. The pipeline
  repeats that rule on every build and, in a pull-request build, also refuses a schema
  migration modified, renamed or removed against the default branch, and an index the
  default branch has taken since the branch was cut; `bedrock migration renumber` is the
  fix it names. Seed files are development data and may be edited or removed, the
  directory staying one sequence; a changed seed applies from the start by recreating
  the database: a pull request's on its next build, a seeded environment's by the next
  release, as a restore run the release asks for itself.
- an application root without release-please's configuration
  (`release-please-config.json`) or its manifest (`.release-please-manifest.json`),
  naming the file: the release workflow reads both, and without them no release is cut
  and nothing reaches an environment. `bedrock render` seeds both when absent, the
  configuration with `initial-version` 0.1.0 and a changelog section for every title
  type the CI's `title` check accepts (`upgrade`, `infra` and `config` shown, so a merge
  of only those releases a patch; `cleanup` hidden beside `refactor`), and the manifest
  at 0.0.0, so the first release pull request proposes 0.1.0; release-please moves the
  manifest from then on, and both are the application's to edit. An application seeded
  before the four types had sections adds them by hand, since render never rewrites the
  configuration.
- a release-please configuration whose `changelog-sections`, at the top level or a
  package's own, lacks a type the CI's `title` check accepts (impulse's `ci.TitleTypes`,
  the list the check reads), naming the types: release-please drops a merge whose type
  has no section exactly as it drops a hidden one, so a pull request of only such titles
  opens no release pull request and never reaches an environment, and an upgrade (new
  pins, new pipeline) or an infrastructure change (applied only in a tag build) must not
  wait for a later releasing merge. Whether a section is hidden is the application's
  choice; only an absent entry is refused.
- a release-please configuration (`release-please-config.json`) with
  `bump-patch-for-minor-pre-major` true, at the top level or for a package: below 1.0 a
  feature release would bump the patch and stay on production's hotfix line
  (`v<major>.<minor>.x`), so a hotfix of production's release could be neither numbered
  (the line's next patch is taken) nor passed by the hotfix check (the feature's
  migrations are what the environment would be restored to). Off, a feature opens a new
  line, a fix bumps the patch and a breaking change the minor.
- a Cloud Armor policy removed in one step: an environment's policy is on while
  `cloud_armor` in `terraform.tfvars` names the environment with `"preview"` or
  `"enforce"`, attached to the backend services, and removing the entry detaches the
  policy and destroys it in one apply, which fails while the policy is attached. The
  check reads the default branch's `terraform.tfvars` (origin's copy, or the local branch;
  the infrastructure workflow fetches the branch before it runs) and refuses an
  environment on there whose entry the working tree removes: set it to `"off"` first
  (the policy kept, detached), merge and apply, then remove the entry. Outside a git
  working tree, or without the default branch to read, nothing is compared.
- an authoritative IAM resource (`*_iam_binding`, `*_iam_policy`) anywhere in the stack:
  such a resource replaces every member of its role on each apply, so a pull-request stack
  applying one would remove the environment's members. A `*_iam_member` adds one member.
  The file stores' bucket policies (`google_storage_bucket_iam_policy.files`, one per
  store, as `storage.tf` declares them) are the one exception, admitted by address: the
  stack sets each bucket's whole permission list on purpose, so that Cloud Storage's
  default grants to the project's basic roles are gone from it, and a pull-request stack
  makes buckets of its own. The pipeline's test before every apply admits the same
  addresses, named from the buckets its trigger carries (`_FILE_STORES`).
- a Dockerfile whose browser build stage (the stage that runs `bun run build`) does not
  declare `ARG VERSION`: a build argument is visible inside a stage only after the stage
  declares it again, and the release is stamped into each bundle in that stage, so without
  the declaration every bundle is built as `dev` and never sends its release. The seeded
  Dockerfile declares it; an older one adds the line before its build.
- a build secret the Dockerfile mounts as required (`--mount=type=secret,id=NAME,required=true`)
  that some environment's `build_secrets` in `terraform.tfvars` does not declare, naming
  the environments: a release that passed the earlier environments would fail in the
  image build of the one lacking it. An optional mount passes with nothing said.
- a build argument `placement.json` declares (`buildArguments`) that no stage of the
  Dockerfile declares with `ARG`, naming the line to add (`ARG FIREBASE_API_KEY`) to the
  stage that builds with it: the image build would be given the value and drop it. An
  `ARG` before the first `FROM` does not count, since it reaches the `FROM` lines alone.
  `render` refuses a value the stack does not make for the code (`firebaseApiKey`
  without `APP_FIREBASE_API_KEY`, `firestoreDatabase` without `APP_FIRESTORE_DATABASE`).
- a Firestore database (`APP_FIRESTORE_DATABASE`) without its project variable
  (`GOOGLE_CLOUD_FIRESTORE_PROJECT`), which the stack sets to the environment project,
  or without `schema/firestore/firestore.indexes.json`
  or `schema/firestore/firestore.rules` beside the schema migrations, naming the missing
  file: the stack applies the database's indexes, time-to-live policies and rules from
  them, so render and check both stop before the stack is written. `APP_FIREBASE_API_KEY`
  without the database, or at another level than it, is refused the same way.

It also warns, without failing, about the maintenance windows (Maintenance windows,
below): production with no maintenance setting in `placement.json`, so that the refusal
of a breaking release at the start of its run is never the first sign; no release file
(`zz_gen_release.json`) in the router package, so that no release of the checkout is
breaking and the window never holds a run; a release file that does not read; and a
dated slot that has passed. A malformed setting (a time zone that does not load, a slot
that never opens, a from that is not before its to on a dated slot, an environment the
placement does not list) is refused by `render` and `check` alike, since both read the
placement.

It lists, as information, each secret that tracks `latest`: a secret whose version in an
environment's `secret_versions` in `terraform.tfvars` is the word `latest` in place of a
version number, which the environment then runs at whatever version is added next, with
no release. One line names each secret and environment, and a stack where every secret is
pinned says so. Pinning is the default and `latest` the exception for a secret that has
to follow its source, so the listing never fails the check; it keeps the exception
visible.

The application's infrastructure workflow, `.github/workflows/infrastructure.yml`, runs
`bedrock check` on every pull request and on the default branch. It is rendered too: the
job gets the bedrock the placement pins as the pipeline does (a release downloaded from
its GitHub Release and verified against the placement's checksum; a commit pin built with
`go install` in the pipeline's Go image, since the runner's Go is the application's and
may be older than bedrock's) and runs that bedrock's `check` on the runner, whose Go
toolchain the check reads the code with; no cloud identity, no service account, no key.
So the checker is the pipeline's, and a wording change in a newer bedrock fails no
application's check until that application moves its pin. `release-please.yml`
is rendered beside it: release-please as the release app the placement names, on the
default branch and the hotfix lines.

## bedrock upgrade

`bedrock upgrade [version|commit|branch]` moves the application's bedrock pin and renders
the stack and the pipeline with the bedrock it names, so the committed files come from
that bedrock. Commit `placement.json` with the rendered files: from that commit the
pipeline's first step and the infrastructure check get that bedrock, and every
`bedrock deploy` step runs it. The pin is the one place a bedrock version appears in an
application; nothing else names one.

- **A release** (`bedrock upgrade v0.4.0`, or no argument for the latest release; a
  pre-release is moved to by name). It writes the version and the release's linux/amd64
  checksum into `placement.json` (`bedrockVersion`, `bedrockSha256`; the checksum is read
  from the release's `checksums.txt`, never typed), and fetches that release for the
  machine it runs on (into the user's cache directory, verified against the same file)
  to render with.
- **A commit** (`bedrock upgrade 58b211dce544`, a full or abbreviated hash; a branch,
  `bedrock upgrade feature/my-change`, whose head commit is read from the GitHub API; or
  the pseudo-version itself). The Go module proxy names the commit's version, and
  `upgrade` writes it as `bedrockVersion` with no `bedrockSha256`, clearing the checksum
  a release pin left. It builds that commit with `go install` into the user's cache
  directory to render with, so a commit pin needs Go on the machine; the install turns
  Go's checksum database on, whatever the machine's Go settings say. A commit a release
  tag names moves to that release instead. A commit whose `go.mod` has a `replace` or
  `exclude` directive is refused, since `go install` cannot build it. A commit named by
  its hash or its pseudo-version must be on a branch of `cccteam/ccc`: GitHub serves the
  commits of the repository's forks by hash too, and the proxy would fetch one, so
  `upgrade` refuses a commit none of the repository's branches holds.

Push the commit before pinning it: the proxy knows only pushed commits, and when it is
asked about one too soon it remembers for about 30 minutes that it did not know it.
`upgrade` then says to push first and retry.

`render` and `check` refuse a placement with no pin at all (an application's first
placement, which `bedrock org register` writes, carries the pin of the bedrock that wrote
it), and a placement pinned to another bedrock than the one running them
when that one is held to its pin: a release, however it was built, and a commit installed
with `go install`. The refusal names how to install the pinned one: its release page, or
`go install github.com/cccteam/ccc/bedrock@<pin>`. A build from a checkout, at any
commit, and a `(devel)` build are nobody's pin and render any pin.

## bedrock deploy

`deploy` holds the pipeline's steps, one command each, run inside Cloud Build by the
bedrock its first step got: the release the placement pins, downloaded and verified
against its checksum, or the commit it pins, built with `go install` (in about one and a
half to two minutes, against seconds for a download) and verified by Go's checksum
database. Each step after it runs its command in the image whose tool the command drives
(gcloud's for most, OpenTofu's for the pull request's stack, docker's for the image
build), and none of them installs anything. The steps share a workspace, the checkout
(`/workspace`, `--workspace` overrides): `environment.sh` (the facts resolve exports, then what later
steps append), `build.json` (the build as Cloud Build describes it), `build-args.txt` (the
image build's arguments, `NAME=value` lines: the declared substitutions, then what a hook
before the build adds) and `revisions.txt` (the revisions the service step created). Each
command reads those files, does one thing and appends what it learned; that is the only
thing one step hands the next. In order:

- `deploy resolve`: reads the build through the Cloud Build API, mints the repository's
  GitHub token from the Cloud Build connection, and works out the facts: the trigger's
  kind (a tag's build, or a pull request's, which deploys only to the first environment),
  the pull request's instruction (the words after its latest `/gcbrun` comment:
  `shared-db`, `reload-db`, `down`), the image and its tags, whether the migrations run
  and traffic shifts, and whether a stale pull-request database is recreated (a migration
  the last build applied is no longer in the tree). A tag build may carry a restore
  instruction (`_RESTORE`: `empty`, or `production-backup` for the environment on
  production's instance, with `_REQUESTER` naming who asked): the environment's database
  is replaced before the release deploys. A pull-request build carries none, and
  production is never restored by a run. `_REQUESTER` alone is a rerun (`bedrock rerun`:
  the release's tag build again, production included), which the record names. Whether
  the build applies the development seed (`SEED`) is read from the placement in the
  checkout: every pull request seeds, and a tag build seeds where the placement's `seed`
  list names the environment, so a release that changes the list seeds with its own. The
  trigger's `_SEED` is what the stack said at its last apply; it stays among the
  substitutions as what the trigger said, and the log says when the two differ. In an
  environment on the seed list, a tag build decides a restore itself when the tree no
  longer carries a seed file as the environment's live release applied it (its record
  lists the seed files with their hashes; edited, renumbered or removed since): the
  release is the requester, the reason goes on the record (`RESTORE_REASON`), and a
  restore asked for takes precedence. A seed file added beside the applied ones
  recreates nothing. In staging a tag build decides a restore from production's backup
  itself, the staging rehearsal: staging runs a release against production's data before
  production does, so a release that carries migrations production has not applied, by
  the versions tst's live record of the release and production's live record say they
  applied (the highest schema migration's index in each; both records are read as the
  build, production's from the bucket its 2-env lets this environment's deploy identity
  read, and it also names production's live database and the backup a restore put it there
  from), restores staging's database from
  production's newest backup before it deploys there, as a restore run the release asked
  for, and so does a release when staging's own live record lists a migration file the
  release does not carry as applied (a failed release's), so that staging sits at
  production's release between releases; a release without such migrations deploys to
  staging as it stands, since there may be things to see against production's data
  anyway. The reason goes on the record, and the log says which versions it read. The substitutions the application declares for its hooks and its
  image build (`substitutions` in the stack's `terraform.tfvars`) and its build secrets'
  pins (`build_secrets` there) are read from the checkout the same way, the build
  secrets' containers alone from the trigger (`BUILD_SECRETS`), so a release that
  changes them builds with its own; `environment.sh` exports the contract's
  substitutions as the trigger passed them and the declared ones as the checkout
  declares them, and a name the trigger carries that the checkout no longer declares is
  left out. A declared name the contract carries is refused here, and so is one starting
  with `_BUILD_ARG_`. The build arguments `placement.json` in the checkout declares
  (`buildArguments`) are values of the stack's, so they come from the stack and never the
  checkout: a tag build exports the trigger's `_BUILD_ARG_<NAME>` for each name the
  checkout declares, says when the trigger does not carry one yet (it comes with the
  build's stack apply, after the image build, and reaches the image from the next
  release on) and leaves out one the checkout no longer declares; a pull-request build
  exports none, and `deploy pr-stack apply` appends the pull request's own stack's. The
  log names the arguments, never their values. What the trigger carries beyond these is
  what only the stack knows (the identities, the buckets, the
  services, made by the stack or the organization's layers) or a gate's own record:
  `_RELEASE_ACTORS` stays the trigger's, since a release must not name the actor that
  admits it, and the promotion order (`_ENVIRONMENTS`, `_PREVIOUS_ENV`) stays the
  trigger's beside the identities and buckets it is paired with.
- `deploy validate-release`: for a tag build, the tag belongs to a GitHub Release cut by
  an accepted release actor, the tagged commit is on the default branch or at the tip of
  a hotfix line, and the record gate holds: the release is live in the previous
  environment. A refusal starts with `Build REJECTED` and says why. A tag build's log first
  names the bedrock running it, and says when that is a commit pin; a release may deploy
  with a commit pin. A release whose notes carry release-please's breaking-changes section
  (a commit with `!` after its type, or a `BREAKING CHANGE:` footer) is said and recorded
  (`WINDOW_RELEASE`); whether the release waits for the maintenance window is decided
  from the release file, not from the notes: last, the step reads the oldest release the
  session outlets still answer from `zz_gen_release.json` in the router package
  (`--router-dir`, which the pipeline passes) and the environment's newest live record,
  decides whether the release is breaking and whether the run waits for the window
  (`WINDOW_NEEDED`, `WINDOW_BREAKING`, `WINDOW_REASON`), and refuses here what can never
  proceed (Maintenance windows, below). A pull-request build previews the window instead.
  A hotfix passes one more check
  in every environment: the environment's newest live deployment record lists the
  migration and seed files its database holds, each with its hash, and the hotfix is
  refused when the database holds a file the hotfix does not carry, or one whose content
  differs, naming the file; the environment is restored to the hotfix first (staging's
  own build restores it, as the staging rehearsal says, when its database is ahead of the
  release; tst is restored with `bedrock restore tst`; a restore
  run replaces the database and skips this check); at production's door the hotfix must
  also be on the line production runs, read from the same record.
- `deploy guard-migrations`: the schema migrations and the seed are each one sequence
  (the rule `bedrock check` applies), and in a pull-request build every schema migration
  the branch started from is still there unchanged and the sequence is read together with
  the default branch's. Seed files may be edited or removed; the resolve step recreates a
  pull request's database when a seed its last build applied changed, and restores a
  seeded environment's when a seed its live release applied changed. A refusal is posted
  on the pull request and names the fix.
- `deploy plan-environments`: in a pull-request build, plans the stack for every
  environment of the promotion order, each against that environment's state prefix as
  its plan identity (a reader, without the state lock, so a pull-request build in tst
  can change no environment), and runs the tests a tag build runs before its apply. One
  comment on the pull request carries every summary; a failing plan or test stops the
  build and the comment says so. The plan the reviewer approves is each
  environment's. An environment whose stack has never been applied (no state object
  under its prefix, which the plan identity reads) is skipped with the line "no stack
  yet: its first apply is by hand", in the log and in the comment, and the build goes
  on: a plan of a missing state would create it, a write a reader is refused, and the
  first apply is a person's step on purpose.
- `deploy pr-stack plan`, `guard`, `apply`: a pull request's own environment, the stack
  applied into its own state prefix as the apply identity. The plan is saved, the guard
  lets only the pull request's own resources through, the apply applies exactly that
  plan and leaves the pull request's services, jobs, hostname and build arguments
  (`_BUILD_ARG_<NAME>`, its own values of the stack's) for the steps after (a destroy on
  `/gcbrun down`). A tag build skips all three.
- `deploy check-release`: reads the registry before the image build. Neither tag exists,
  the build runs; the commit is built and the release tag is not, the release name is
  added to that build; both exist and agree, the build is reused; the release tag names
  another build, the run is refused.
- `deploy build-image`: builds the checkout's Dockerfile with docker and pushes the image
  under its two tags, with the build arguments (`VERSION`, `COMMIT`, the job of the
  build, the stack's values `placement.json` declares as `_BUILD_ARG_<NAME>` in
  `environment.sh`, passed as `NAME=value` and named in the log without their values,
  then `build-args.txt`) and the declared build secrets (read as the deploy identity
  into memory and passed as BuildKit secrets, never build arguments); the digest goes to
  `environment.sh`. In a pull-request build of an application that declares build
  arguments, the step waits for the pull request's stack to be applied, so the image
  carries the pull request's own values. The build runs in a BuildKit container
  (buildx's docker-container driver, created for the build: the one driver that
  exports a cache) and pushes a plain image. Three builds: the Dockerfile's two reserved
  stages, `go-modules` and `web-packages`, each exporting its layers alone to the registry
  under `cache-<commit>-go` and `cache-<commit>-web`, then the full build, reading those
  caches and exporting nothing. Only the downloads, which go.sum and bun.lock pin, are
  ever served from a cache; the compile and the bundles are built fresh in every
  environment, and since every stage copies its inputs by name, no per-build file of the
  pipeline is an input of any layer. A tag build reads this commit's caches and the live
  release's commit's; a pull-request build reads its own pull request's last build's and
  the live release's; a tag build never reads a pull-request build's. Before composing
  the builds the step asks the registry which candidate tags exist, names only those as
  sources and writes this commit's tags only when they are absent (the registry's tags
  are immutable), so the log carries no ERROR line for an import docker cannot find or an
  export the registry refuses; its "Layer cache:" line names what was read, by the commit
  it belongs to, and whether this commit's caches were written or were held already. A
  workspace whose package.json lists trustedDependencies has its install stage built with
  no cache in or out, and the log says why. A build argument is part of a layer's key, so
  a layer that sees one of the stack's per-environment values is never served to another
  environment; a reserved stage declares no `ARG` (`bedrock check` refuses one), and one
  that sees an argument through the stage its `FROM` names or a stage it copies from is
  built with it and caches under a digest of its value (`cache-<commit>-go-<digest>`), so
  each environment reads and writes a cache of its own. Every environment still builds
  its own image from the commit; nothing is promoted between environments.
- `deploy maintenance on`: puts the application into maintenance when the run needs it,
  at one of two places. Before the stack is applied, a run that replaces the database (a
  restore run); after the wait for the maintenance window (`--window`), a breaking
  release that is not in maintenance already, after a second look at the window, which
  must still be open, else the run stops with nothing changed and names the next opening.
  The release's own image starts as a revision with `APP_MAINTENANCE=1` in every region,
  under the tag `next` and with no traffic; the revision is probed through the load
  balancer's next hostname and must answer 503 with `X-Maintenance: 1`, else the run
  stops with nothing moved and names the application's missing switch (`impulse check
  maintenance-switch`); then all traffic moves to it, the task queue (`_TASKS_QUEUE`) is
  paused and, on a restore, purged, the running executions of the serving build's job are
  canceled, and the old revision's requests in flight are let finish (its active instances
  read from Cloud Monitoring until none is, or the service's request timeout). Any other
  run keeps the application serving: an ordinary release inside a window deploys the
  rolling way, with no maintenance revision, no probe, no pause and no cancel. The facts
  (`MAINTENANCE`, `MAINTENANCE_REVISIONS`, `MAINTENANCE_QUEUE`, `MAINTENANCE_PURGED`,
  `MAINTENANCE_CANCELED`, `MAINTENANCE_WAITED`) reach the record.
- `deploy stack plan`, `apply`: in a tag build, the environment's stack planned and
  applied as the apply identity, after the image build (a failed build changes no
  infrastructure) and before the jobs and the migrations (what they need exists first).
  `plan` saves the plan with its JSON, prints and appends the summary (`STACK_PLAN`), and
  runs the tests: no authoritative IAM resource in the stack other than the file stores'
  bucket policies, and every secret version a
  planned revision template pins exists and is enabled. `apply` applies exactly that plan.
  The plan is the build's own: a saved plan is bound to the state it was made from, and a
  release bundles several pull requests. An infrastructure change must be safe on the
  running service (add first, remove later); one that is not is declared breaking in
  release-please's way and becomes a window release. The first apply of an environment
  stays by hand, before any release exists there. In a restore run (`_RESTORE=empty`) the
  plan replaces the Spanner database and, in the first environment, the file stores'
  buckets (every one the stack's `_FILE_STORES` names), each when the stack has it, so
  the migrations apply afresh, and the seed where the placement's seed list names the
  environment; the environment on production's instance keeps its file stores'
  buckets. The Firestore database is not replaced (Firestore keeps a deleted database's id
  unavailable for minutes): after the apply, the step deletes its documents as the apply
  identity, since they refer to rows the restore replaced (`RESTORE_CLEARED`). In a
  restore from production's backup (`_RESTORE=production-backup`, the environment on
  production's instance) the plan step first drops the environment's database and
  restores it, under its own name, from the most recent backup of production's live
  database on the instance they share, as the apply identity (a backup Spanner is still
  taking, the release backup a release started minutes earlier, is that backup, since it
  holds the newest data: the maintenance step chooses it and waits for it before the
  maintenance page goes up, then names it for the plan step, `RESTORE_READY_BACKUP`, which
  restores that one); the plan then recreates the
  memberships the drop took with it, and the migrate command applies whatever production's
  backup predates. Production's live database is the one its deployment record names
  (`_RESTORE_DATABASE`, read by the operations workflow): after a restore to a backup, the
  generation restored into, not the stack's first database, and while that generation has
  no backup of its own (the next release or the schedule takes one) the backup it was
  restored from stands in (`_RESTORE_DATABASE_BACKUP`, the record's `restore.backup`); a record written
  before database generations names none, and production's first database is read. The
  backup and the moment its data is from reach the record
  (`RESTORE_BACKUP`, `RESTORE_BACKUP_TIME`). While the application is in maintenance the
  plan carries the maintenance variable's live value (`-var maintenance=1`): the stack
  declares `APP_MAINTENANCE` with `var.maintenance`, empty by default, and an entry of the
  service's env set cannot be ignored on its own, so declared and live agree and the apply
  leaves the service alone while the database is replaced.
- `deploy migrate`: runs the release's migrate command, which `deploy build-image` took
  out of the image (`/builder/home/migrate`), on the build worker as the deploy identity,
  once to completion, from the checkout's root (where the command finds the schema
  directory), with the variables the stack derives for it (`MIGRATE_ENV`, which the stack
  steps read from the applied stack's `_MIGRATE_ENV` output, so a release that changes
  them migrates with its own) and the release in the version variable the pipeline names
  (`--version-variable`); with the seed (`schema/devseed` as data migrations after the
  schema) where resolve's `SEED` fact is true: every pull request, and a release build
  only in the environments the seed list of the placement in the checkout names. Before the command runs, the step reads
  each database the stack names (`_MIGRATE_DATABASES`, read back with the settings) as
  the deploy identity until it may, for up to three minutes: the stack's apply in the
  same build may have just created the identity's grants (an application's first
  release, or the first after its grants change), and IAM makes a new grant effective
  seconds to minutes after the policy holds it. The command's lines are the build log's,
  so a failed migration's message is where the run is read, and the deployment record
  lists the migrations applied. A release build may carry a migration operation
  (`_MIGRATE_ACTION`, from the operations workflow: `version`, `rerun` or `force`, with
  `_MIGRATE_TABLE`, `_MIGRATE_VERSION` and `_REQUESTER`; `bedrock migration`, below):
  `version` runs the command once with `-version` and leaves `SKIP_DEPLOY` with the
  reason, so nothing else deploys; `force` runs it with `-force <n>` (or `-force-data
  <n>`), leaves the force for the record, then runs it as it always does; `rerun` is the
  command as it always does. An operation that cannot run (an unknown action or table, a
  force whose version is missing or not an integer, a force without a requester) is
  refused by `deploy resolve` and again here, before the command runs.
- `deploy jobs`: makes this build's job for the job process (`cmd/jobs`) after the
  stack's apply, before the migrations, as a copy of the stack's template job (`_JOBS_JOB`,
  read from the stack as this build applied it, so the first release with a job process
  makes one) named after it with the build's version, on this build's image with the
  pipeline's labels and the template's IAM policy (the site's `run.jobsExecutorWithOverrides`). It does not
  run it; the service carries the template's name (`APP_JOBS_TEMPLATE`, set by the stack)
  and the image its version, and the framework names the job of its own build from the
  two, so the revision starts the job of its own build and a traffic rollback starts the
  earlier one. Made before the migrations so that a failure here leaves the database
  untouched. The step is rendered
  only for an application with a job process.
- `deploy migrate --preflight`: in a run that waits for the maintenance window and
  replaces no database, runs the release's migrate command once with `-version` before
  the wait: the command starts on the build worker against the environment's database
  and prints what the migrations tables say, so a command that does not start, a
  configuration that does not load or a database that cannot be reached stops the run
  with nothing changed and the window not entered. Nothing is applied. Any other run says
  so and does nothing.
- `deploy window`: in a run that waits for the maintenance window, holds the run at the
  gate until the environment's window opens, with the image built and the stack applied, so
  the window holds only maintenance, the migrations and the rollout. It reads the
  setting from the checkout's `placement.json`, prints when the run will proceed and
  waits, reading the clock again at most every ten minutes; when the window opens it
  leaves when, how long it waited and which opening let it in (`WINDOW_OPENED`,
  `WINDOW_WAITED`, `WINDOW_SLOT`) for the record. An opening further away than the build
  can wait stops the run here. A run in maintenance already (a restore run, or a rerun
  after a window release that failed) passes at once.
- `deploy sweep-jobs`: after the traffic shift, deletes the builds' jobs nothing runs any
  more: a job of the job process whose version no revision in any region carries (a
  revision that exists can take a rollback, and then starts its own build's job). A job
  with an execution running, or made in the last three hours (its build may still be
  running), stays; the template always stays. The step is rendered only for an
  application with a job process. Nothing retires a revision: that is Cloud Run's own ceiling. Jobs are
  deleted because Cloud Run allows 1,000 per project and region, shared by every
  application and pull-request environment. How far back a rollback reaches is the shared
  registry's keep count: Cloud Run keeps an image only while a serving revision uses it,
  so an older revision needs the registry's copy to start again. The job process's part
  of the contract: end what it is doing on SIGTERM, the signal Cloud Run sends a job's
  container when its execution is canceled, within Cloud Run's grace.
- `deploy service`: puts a new revision of the service in every region, receiving no
  traffic yet, after repairing a service a failed earlier deploy left inconsistent. The
  new revision carries the tag `next` (or the pull request's tag), under which the
  stack's next backend serves it at `<app>-<env>-next`; `NEXT_URL` and the per-region
  `REVISION_URLS` are left in the workspace for the hook before traffic. The hostname
  `NEXT_URL` is named by is the one the stack this build applied names
  (`CANONICAL_HOSTNAME`, read back after the apply), so a release that changes it is
  reached under its own. The template is
  the live service's, so a maintenance revision's `APP_MAINTENANCE` is cleared on the new
  revision, which serves the application.
- `deploy shift-traffic`: moves every region to 100 percent on its new revision, keeping
  the tags other revisions carry; a pull-request revision served under its tag alone
  leaves the traffic where it is.
- `deploy maintenance off`: after traffic moved in a run that was in maintenance, resumes
  the task queue against the new release; the maintenance revisions stay, with no
  traffic, as any old revision does.
- `deploy record`: writes the deployment record once traffic has moved, with the plan of
  the stack the build applied (`stack`: counts and changes) and, in a restore run, what
  replaced the database, who asked and what the stack replaced (`restore`), the
  maintenance the run went through (`maintenance`: the revisions, the queue, the
  executions canceled, how the wait ended), and, for a release that needed the
  maintenance window, whether it was breaking, why, which opening let the run in, when
  and after how long a wait (`window`).
- `deploy talk-back`: in a pull-request build, tells the pull request what the build did
  as the deployer app: a GitHub deployment carrying the environment's URL and a comment.
  The app's token is minted from its key when there is something to say, by this step or
  by a guard with a refusal to post, and is kept nowhere.

`deploy hook <stage>` runs the application's hook for a stage, with the build's facts in
its environment: `before-build`, `before-migrate`, `after-migrate` (the schema migrated,
the service not yet deployed), `before-traffic` (the new revision deployed in every region
and the old one still serving; `NEXT_URL` is the new revision's public URL through the load
balancer, the `<app>-<env>-next` hostname over the revision tag `next`, and a failure here
stops the build with the old revision serving), `after-traffic` and `after-down` (on a
teardown). A hook is a script, `infrastructure/hooks/<stage>.sh`, or a function of the
application's hooks program: a Go program at `cmd/deployment/hooks` built on impulse's
`deployhook` package (`deployhook.Main(deployhook.Hooks{BeforeTraffic: checkNextRevision})`), which
takes the four stages after the image build. bedrock reads the program's stages from its
`Hooks` literal, `check` refuses a Dockerfile that does not build it as `/hooks` and a stage
that has both a script and a function, and the image build (`deploy build-image --hooks`)
takes the program out of the image for the hook steps (`deploy hook <stage> --program`).
The pipeline has a step for each stage the application implements. `deploy sweep`
is the hourly sweep's one step: the pull requests whose services stand, which of them
are closed, and each closed one's stack destroyed.

## Maintenance windows

A maintenance window is the time an environment may take a release that interrupts
service. It is not a deploy freeze: an ordinary release rolls in at any time, because the
old server keeps answering through the deploy, and only a breaking release waits for the
window and deploys behind the maintenance page.

A release is breaking for an environment when it turns away the release the environment
runs: the oldest release of the browser application its session outlets still answer
(`OldestAnswered` in the generator program, which the resource generator writes into
`zz_gen_release.json` beside the generated router) is newer than the environment's live
release (its newest live deployment record), or an outlet answers its own release alone
(`OldestAnswered(generation.ThisRelease)`, for a release whose migration the old server
cannot run on). The newest value over the session outlets counts; an API-key outlet
never does. The pipeline reads the file from the checkout, never from a binary. With
production on 1.4.0: 1.5.0 with oldest answered 1.5.0 is breaking; 1.6.0 with oldest
answered 1.5.0 is not once production runs 1.5.0, and is while production is still on
1.4.0, because a rolling step was skipped. A rerun of the release the environment runs
turns nothing away. A checkout without the file declares nothing, so no release of it is
breaking; `bedrock check` and the release check both say so.

The window is written in `placement.json`, per environment:

```json
"maintenance": {
  "tst": "anytime",
  "prd": {
    "timeZone": "America/Chicago",
    "weekly": [{"day": "Sunday", "from": "02:00", "to": "04:00"}],
    "dates": [{"on": "2026-11-15", "from": "22:00", "to": "23:30"}],
    "releases": "breaking"
  }
}
```

`"anytime"` takes a window release at any time. An object names the client's time zone
(an IANA name; the slots are the client's wall clock through its daylight-saving
changes), weekly slots (a day, from a time of day to another; a slot whose `to` is not
after its `from` crosses midnight and ends the next day) and dated slots (a one-off the
client agreed to, on one date, `from` before `to`; a slot past midnight is the next
date's own). `releases` says which releases wait for the window: `breaking` (the
default when absent) or `all`, under which every release from master waits, hotfixes
included, and an ordinary release then deploys the rolling way: the window says when a
release may go in, maintenance mode says what a breaking release does when it goes.
Every environment but production is `anytime` unless its setting is written. Production
has no default: a breaking release to production is refused at the start of its run
until its setting is written, and `"anytime"` is a setting to write, so a forgotten
setting never takes production offline in the middle of the day; ordinary releases to
production are unaffected. The release reads the placement of the commit it was tagged
on, like everything else in the run: changing a window is a commit merged before the
release is tagged, and a change after the tag needs a new release. A slot that never
opens, a time zone that does not load, a dated slot whose `from` is not before its
`to`, and an environment the placement does not list are refused by `render` and
`check`; `check` warns while production's setting is missing and when a dated slot has
passed.

How a window release runs, in the pipeline's order: the release check at the start
(`deploy validate-release`) decides whether the release is breaking and whether the run
waits for the window, and refuses what can never proceed, production without a setting
when the release is breaking, a window whose next opening is further away than the
build can wait (the build's timeout of 24 hours less three hours for the steps after the
window), a window with no opening ahead; the image is built and the stack applied (and
the build's job made, where the application has a job process), so nothing that can fail
on its own is left for the window; the pre-flight runs the migrate command once with
`-version` (`deploy migrate --preflight`);
the run waits inside the build for the window to open (`deploy window`), printing when
it will proceed, so the approver approves by day and the run proceeds at night on its
own; then, for a breaking release, maintenance goes on (`deploy maintenance on
--window`, after a second look at the window), the migrations run, the release's
revision deploys, traffic moves to it and maintenance ends; the record says it was a
window release. If the migration fails, the maintenance revision keeps answering and the
rerun's gate is open: an environment whose traffic is on a maintenance revision passes
the gate with the window closed, since the interruption has happened and waiting for the
next window would hold the client on the maintenance page meanwhile; the rerun still
needs its approval, as every production run does. A restore run passes the gate the
same way. A pull-request build never waits and never goes into maintenance; its release
check previews what the release will turn away in each environment and which
environments will hold it, read as each environment's plan identity.

What users see during a breaking release: the maintenance page (every request answered
503 with `Retry-After` and `X-Maintenance: 1`, from the framework's `maintenance`
package, with no database opened), the browser library's maintenance notice checking
back, and, once the new release answers, a reload. The task queue is paused meanwhile
and resumed against the new release; running executions of the old build's job are
canceled, since a breaking release is the decision to interrupt everything the
application is doing, and the window is when the client accepts that. The stack declares
`APP_MAINTENANCE` on the service, so a deploy that sets it and a later apply do not
fight.

## bedrock secret

Secrets are containers in Secret Manager, one per variable the code declares as a secret
(and one per build-time secret the placement declares), mounted by the stack at a pinned
version. Two commands move them along.

```sh
bedrock secret add tst APP_MAIL_API_KEY --from-file key.txt     # a new version, the container created when absent
bedrock secret pin tst APP_MAIL_API_KEY 2                       # terraform.tfvars secret_versions.tst
```

`secret add` stores a value as a new version of the container the stack names for the
variable in the environment (`<prefix>-<env>-gbl-<app>-<kebab name>`), creating the
container when the project has none by that name, so an operator puts the value in place
ahead of the release that first reads it; the stack's next apply adopts the container.
The value is read from `--from-file` (a path, or `-` for standard input), from standard
input when it is not a terminal, else asked for at the terminal without echo. Nothing is
pinned or rolled out; the command prints the pin to make.

`secret pin` writes the version an environment runs into the application layer's
`terraform.tfvars` under `secret_versions.<env>.<VARIABLE>`, after asking Secret Manager
whether the version exists and is enabled. The pull request's plan shows the revision
template change in that environment and nothing elsewhere; the pin promotes as a release
(a commit with a releasable type, `fix(<env>): …`), whose tag build applies the stack and
deploys. An argument left out is asked for at the terminal with the choices listed.

Both find the rest from where they run: the infrastructure root, the application, the
environment project (by its labels) and the container (by its labels and the variable);
`--dir`, `--app`, `--project` and `--container` override.

In the organization's infrastructure repository (the one whose `placement.json` at the root
names the organization, `organizationId`), the same two commands take one of the
organization's secrets, the private keys of its GitHub Apps, in place of an environment and
a variable:

```sh
bedrock secret add github-infrastructure-key --from-file infrastructure.pem   # the boot project's container
bedrock secret pin github-infrastructure-key 2                                # placement.json, then org render
bedrock secret add github-deployer-key tst --from-file deployer.pem          # the environment's container
bedrock secret pin github-deployer-key tst 1                                  # 2-env/terraform.tfvars
```

`github-infrastructure-key` is the key of the infrastructure GitHub App, the app the layers
workflow acts as when 1-org configures the applications' repositories. It lives in
0-bootstrap's container `<prefix>-boot-gbl-github-infrastructure-key` in the boot project,
and its pin is `githubInfrastructureKeyVersion` in `placement.json`: `pin` writes it and
runs `org render`, so the workflow mints its token from that version. `github-deployer-key`
is the key of the deployer GitHub App, the app the pipeline talks back on a pull request as.
It lives in 2-env's container `<prefix>-<env>-gbl-github-deployer-key` in each environment's
project, and its pin is the version's resource name under the environment in 2-env's
`github_deployer_key_secret_versions`, which the application stacks pass to their
pipelines. Both are pinned by number, never `latest`, after Secret Manager confirms the
version is enabled. `add` refuses a project that lacks the container rather than creating
it, because the container is the layer's own and its apply makes it. The projects come from
`placement.json` (`projects.boot`, `projects.<env>`); `--project` and `--container`
override. In an application's repository these names are refused, and in the
infrastructure repository an application's `[env] [VARIABLE]` is.

## bedrock migration renumber

`migration renumber` moves the migrations this branch added, up and down files together,
to follow the default branch's highest index with no gap, keeping their order. A schema
migration the default branch holds is never touched: git says which files are the
branch's own, and the default branch is read from origin's copy of it when the repository
has one, else from the local branch, so fetch first. `go generate ./...` runs it before the
application's own generators through the owned `cmd/generate/bedrock.go`, whose directive
is `go run <module>@<pinned version> migration renumber`: the Go toolchain builds the
pinned bedrock from the module proxy the first time and caches it, so neither a developer
nor the CI job installs bedrock for it. A tracked file moves with `git mv`;
an untracked one is renamed on disk. A branch cut from a hotfix line
(`hotfix/<major>.<minor>.x`, nearer to the branch in the history than the default branch
is) follows the line instead, since a line is behind the default branch on purpose; the
pipeline's guard compares such a pull request with the line too (Cloud Build's
`_BASE_BRANCH`), and the pull-request trigger covers the lines beside the default branch.

The seed directory beside the migrations is renumbered against its own sequence, and as
its files are editable, a committed seed file the branch removed leaves no gap: the seed
files after it move down, and the branch's own follow, around the indexes the default
branch took since the branch was cut. A committed seed file only moves down: when the
default branch's additions would push one up, or leave a gap below them, the directory is
left alone with a note that says to merge the default branch first and run the renumber
again.

```sh
bedrock migration renumber          # by hand
go generate ./...                   # through the rendered cmd/generate/bedrock.go, before the generators
```

It closes the holes the pipeline's guard refuses a pull request for: an index the
default branch took since the branch was cut (the branch's migration moves up), a gap
(the branch's migration moves down), and a removed seed file (the seed files after it
move down). Nothing to do prints nothing.

## bedrock migration version, rerun and force

The migrate command of every Impulse application answers three flags beside `-seed`:
`-version` prints what each migrations table (schema, data) says about the database and
exits; `-force <n>` and `-force-data <n>` set a table to a version, clean (`-1` for no
version), print the row before and after, and exit. None of them applies a migration, and
a force refuses `-seed` and `-version`. They are the instrument for the states the
migration runner refuses to guess at: a database the old library left dirty with no
progress recorded, an in-flight operation Spanner no longer has, a file changed in its
applied part. A migration that failed at a statement and recorded its progress needs none
of them: the next run of the same release continues from the failed statement once the
cause is fixed.

From GitHub, with no cloud credential, a person with write on the repository runs them on
an environment below production through the operations workflow's `migration` job, which
`bedrock migration` dispatches as the person signed in to gh (the Run workflow button on
the Actions tab starts the same job, with `action` set to the operation):

```sh
bedrock migration version tst --release v1.4.0              # print the database's migration version
bedrock migration rerun tst --release v1.4.0                # run the release again; the migrate command continues from where it stopped
bedrock migration force tst 40 --release v1.4.0             # set the schema version to 40, then let the release continue
bedrock migration force tst 2 --release v1.4.0 --table data # the same for the data migrations table
bedrock migration force tst none --release v1.4.0           # no version at all (the migrate command's -1)
```

The job runs in the GitHub Environment named after the target, exchanges its token for
the environment's operations identity as the restore job does, and runs the environment's
version trigger for the release with `_MIGRATE_ACTION`, `_MIGRATE_TABLE`,
`_MIGRATE_VERSION` and `_REQUESTER`. `deploy migrate` reads them after the usual steps
have run (the release check, the record gate, the image, the stack): `version` runs the
migrate command once with `-version`, and the run stops before the service, the traffic
shift and the record, so nothing in the environment changes; `rerun` is the release run
again, the command running as it always does; `force` runs the command with `-force <n>`
(or `-force-data <n>`), then runs it as it always does (with `-seed` where the build
seeds), and the release continues to the service, the traffic shift and the record, which
carries the force (the table, the version, the requester). A version that is missing or
not an integer, an unknown action or table, or a force without a requester is refused
before the command runs; a force whose second run fails leaves the run failed like any
failed migration, with the runner's message. The command runs on the build worker, so its
lines are the build log's; the workflow reads them back from Cloud Logging by the build's
id and the migration steps' names, through the view over the bucket the stack's
`logging.tf` fills with the application's builds (a sink on its triggers' ids; the
operations identity holds `roles/logging.viewAccessor` on that view and nothing wider, so
it reads neither the application's own logs nor another application's builds), and
prints them in the run and in its summary, so a failed migration's message reaches the
person who started the run without a console. Production has no door and no view: its
procedure is under When the migration fails, below. The command itself changes nothing:
it checks the environment (one of the placement's, not production, wired with a project),
the release (a tag that exists) and, for a force, the version, then dispatches and prints
where to watch.

## bedrock hotfix

`hotfix start <release>` starts the hotfix line of a release: the branch
`hotfix/<major>.<minor>.x` at the release's commit, on which release-please releases
fixes as the line's next patch versions while the default branch moves on. The pipeline's
release check accepts a tag at the tip of such a line whose base on the default branch
carries a release tag of the same line. The line's next patch must be free for the
hotfix, so a feature release on the default branch advances the minor, below 1.0 as
above it: `check` refuses release-please's `bump-patch-for-minor-pre-major`. When the
default branch has already cut a later patch of the line (production runs v0.1.21 while
v0.1.22 is out), the branch starts at a commit on the release's commit whose message names
the line's next release for release-please (its `Release-As` footer, the patch after the
highest one cut), so the line's first release skips to it; the manifest stays the
release's, since release-please counts the line's commits from the manifest's release on
the branch, and set to a version the line does not hold it would count the line's whole
history and let its feature commits bump the minor.

A hotfix is based on the release production runs, and it deploys like any release:
through tst and stg, then prd, each after the one before holds it live. What matters in
an environment that ran a later release is its database. When the later release applied
no migration and no seed file, the hotfix deploys into that environment as it is: its
migrate command finds a database at a version it knows, and the environment runs the hotfix
until the held-up release resumes. When the later release did move the database, the
environment holds a file the hotfix does not carry, and the release check refuses the
hotfix there, naming the file; the environment is restored to the hotfix first (staging's own build does
it, as the staging rehearsal says; tst is restored with `bedrock restore tst`), from an
empty database or from production's backup, since a restore run replaces the database
and skips the check. At production's door the hotfix must be on the
line production runs, and that is checked first; a hotfix from an older line that
happens to carry every file would roll production's application back. Production is
never restored by a run, so a hotfix behind production's release on its own line is
told to start the line from the release production runs. `hotfix start` prints the rule, and warns
when the release is not the repository's latest, which GitHub can tell; what production
runs, only its deployment record can.

`hotfix merge <release>` brings a released hotfix to the default branch. It creates the
branch `merge-back/<release>` at the release's commit (the tag's, so the merge-back
carries exactly what shipped and not unreleased commits on the line) and opens a pull
request from it into the default branch, titled `fix: <release>` (the conventional-commit
line the squash merge carries and release-please reads; the author may edit it) with the
release's notes as its body. Conflicts, such as release-please's manifest and changelog
when the default branch has released since the line's base, or code the default branch
has reworked, are resolved by commits on that branch, which is unprotected; the squash
merge deletes it, and the hotfix line is never the pull request's head, so it is never
deleted and never receives a conflict commit. Never a merge commit, and never a pull
request from the line's tip itself. The command refuses a release that is not on a
hotfix line or that the default branch already carries, and reports an existing
merge-back branch or pull request for the release instead of making a second.

A fix on a hotfix line gets its pull-request build like any change: the pull-request
trigger covers the lines beside the default branch, and the build's migration guard
compares the pull request with the line (Cloud Build's `_BASE_BRANCH`), which is behind
the default branch on purpose. The build also looks ahead for the line's next release:
`ValidateRelease` reads each environment's live deployment record as that environment's
plan identity (the identity the build already plans the environment as; `2-env` grants it
the read of its own environment's records) and says, environment by environment, whether
the hotfix would be taken or refused there, naming the file the database holds that the
pull request does not carry, or the line production runs, so the developer learns before
the merge that a restore comes first, and where. In an environment the placement's seed
list names (the placement in the pull request's tree), the preview reads the seed rule
first: when the environment's record holds a seed file that is not in the tree as
applied, the release's build will restore the environment itself and take the hotfix,
and the preview says so, naming the file; the schema migrations are compared only when
the seed matches. The preview warns and never refuses.

The restore is a run of the environment's version trigger for the release, carrying the
instruction `_RESTORE` (`empty`, or `production-backup` for the environment on
production's instance) and `_REQUESTER`. Everything that changes the environment happens
inside that run, in the pipeline's order: the release is validated (the record gate
holds; the hotfix check is skipped, since the database is about to be replaced), the
image is built, the stack's plan replaces the database (and, in the first environment,
the file stores' buckets) and the apply deletes the Firestore database's documents, the jobs are
created, the migrations apply
(and the seed, where the placement's seed list names the environment), the revision
deploys, traffic moves, and the record carries the reason and the requester. Before the
database goes, the application is put into maintenance (`deploy maintenance on`): the
release's own image serves the maintenance page with all traffic while the database is
away, the task queue is paused and purged, and the serving build's job executions are
canceled; the queue resumes once the release serves (`deploy maintenance off`, which also
resumes a queue an earlier run's maintenance left paused, so a restore run that failed
after maintenance on is healed by the next release that deploys; a pull-request build
leaves the environment's queue as it is, since a restore may be in maintenance while
the pull request builds). The run refuses this form of the instruction in production, whose database is restored to a backup alone. `bedrock restore` starts it from GitHub (below).
For the environment on production's instance the database is not emptied but restored
from the most recent backup of production's live database (the generation its deployment
record names; after a restore to a backup, the one the generation was restored from while it has
none of its own; a backup Spanner is still taking, a release's started minutes earlier, is
waited for, before the maintenance page goes up), at production's schema: the plan step drops it and restores it under
its own name as the apply identity, and the migrations production's backup predates then
apply. The environment's file objects are kept, and its Firestore documents are deleted
as in every restore.

## When the migration fails

A migration file that fails leaves the database at that file's version, dirty, and the
migrate command's message, in the build log and in the operations workflow run's summary
when the run was started from GitHub, says which of three cases it is. Each case names
the workflow's `action` input and the `bedrock migration` command that dispatches it.

1. **The migration stopped at a statement and can continue.** The message reads `<file>
   stopped at statement <n> of <m> (<statement>): <cause>; fix the cause and rerun,
   which continues from statement <n>, or force a version`: the runner recorded how far
   the file got. Usually the statement validated existing rows (a `NOT NULL`, a check
   constraint, a unique index) and a row failed it. Fix the rows, then run the release
   again: action `rerun`, `bedrock migration rerun <env> --release <tag>`. The migrate
   command continues from the failed statement, applies the rest of the file and the files
   after it, and the release deploys. Nothing is repeated: the statements before it are
   applied and recorded, and the failed one had not applied.
2. **The migration cannot continue.** The message says the database is dirty with no progress
   recorded (a database the old library left at cut-over), that a DDL operation it
   recorded is no longer there, or that the applied part of a file changed since. A
   person decides what the database really holds. First the state: action `version`,
   `bedrock migration version <env> --release <tag>`, which prints `schema: version 41,
   dirty` (and the data table's row) and changes nothing. Then the database: which of the
   tables, columns and indexes the file creates exist. Then the version the database is
   at: the version before the file when none of it applied, the file's own version when
   all of it did; action `force` with that version, `bedrock migration force <env> <n>
   --release <tag>` (`--table data` for the data migrations table, `none` for no version).
   The migrate command sets the row, prints it before and after, then runs the migrations
   from there, and the release deploys; its record carries the force and who asked. A
   force applies no statement itself: a file whose applied part changed is forced to the
   version before it and applied again whole, so its statements must be safe to repeat.
3. **The database is at a version the build does not carry.** The release check refuses
   the run, naming a file the database holds that the release does not: a hotfix behind
   an environment that ran a later release. No force helps. The environment is restored
   to the release: action `restore`, `bedrock restore <env> <release>`, which replaces
   the database and runs the migrations the release carries (bedrock restore, below).

In production the door is narrower. No developer credential reaches `prd`, and the
operations identity there serves one action: `run`, which `bedrock rerun prd <release>`
dispatches (bedrock rerun, below). That is the release's tag build again, from the start,
with no instruction, and it waits for its approval in Cloud Build as every production
release does; so a migration that stopped at a statement continues from it once the
cause is fixed, and the record names who asked and who approved. The version and the force do not reach
production, whose logs are read in the console: the platform operator runs those two with
their own credential, through the environment's version trigger, since that is where the
release's migrate command runs, on the build worker as the deploy identity. The trigger
run takes the same substitutions the door passes, and the pipeline does the rest as
everywhere, the record naming the operator:

```sh
# The version: the migrate command prints it and nothing else deploys.
gcloud builds triggers run <prefix>-prd-<region code>-<app>-version --tag <release> \
  --region <region> --project <project> --substitutions _MIGRATE_ACTION=version,_REQUESTER=<you>
# A force, then the migrations and the release.
gcloud builds triggers run <prefix>-prd-<region code>-<app>-version --tag <release> \
  --region <region> --project <project> \
  --substitutions _MIGRATE_ACTION=force,_MIGRATE_TABLE=schema,_MIGRATE_VERSION=40,_REQUESTER=<you>
```

The command's lines are in the build log, under the `RunMigrations` step, which the
operator reads in the console or with `gcloud builds log <build> --region <region>
--project <project>`. Nobody runs the trigger by hand for a rerun, in production or
anywhere: `bedrock rerun` is its door.

## bedrock restore

`restore <env> [<release>] [--reason <why>] [--before <release> | --at <moment> | --backup <name>] [--of <database>]`
returns an environment's database, started from GitHub: developers authenticate to GitHub
and nowhere else, and nobody sets up a cloud tool to operate an environment. It is the
database step after a release that went wrong: the code's rollback (`bedrock rollback`,
below) comes first, and when the database is wrong too, or the migration itself was the
fault, the restore follows in a run of its own. The two are never one run. There are two
kinds.

**A restore to a backup**, in any environment, production included. `--before <release>`
restores that release's pre-release backup, the database as it was before that release's
migrations (every release build in an environment on the placement's `releaseBackups`
starts one as of its cut, kept fourteen days); `--at <moment>` restores a backup made as
of the moment (RFC 3339), of the live database or, with `--of <database>`, of an earlier
generation whose history holds the moment (a generation's history begins when it was
restored, so a moment before the last restore is in the generation that restore left);
`--backup <name>` restores any backup on the instance by its resource name, a forensic
backup to undo a restore, say. `bedrock backups <env>` lists what there is. The command
prints the statement (what the database returns to, on which release, asked for by whom
and why), asks for the environment's name typed, and dispatches the repository's
operations workflow as the person signed in to gh. The workflow's job reads the
environment's deployment records for the live release, which stays unless the command
named one, and, for `--before`, the release's cut from its first run as a release, prints
them, refuses a moment whose data holds a migration above the release's (the newest
record at or before the moment lists what the database held then; the migrate command
would refuse the restored database as ahead of its files after the database step, with
the environment left in maintenance), naming the migration, the release that applied it
and the two ways on (a moment before it, or that release run again first), and runs
the environment's version trigger for the release with the backup (or `@<moment>`) as
`_RESTORE`, the generation as `_RESTORE_DATABASE`, the reason and the requester, waiting
for the build to its end: production's build waits for its approval in Cloud Build as a
release does, and thirty minutes without one the job cancels it. The build, as the deploy
identity: the resolve step says what returns; the application goes into maintenance
whatever the window; the stack plan, as the apply identity, finds the chosen backup (or
starts one as of the moment, kept fourteen days), refuses one that does not exist or
belongs to another application before anything is started, waits for it to be READY,
then starts the forensic backup of the live database (`<db>-forensic-<stamp>`, thirty
days; a backup start Spanner refuses because it is taking another waits for its turn),
restores the chosen backup into the database's next generation (`<db>-2`, then `-3`),
writes the generation beside the deployment records (`<app>/database/<env>/<n>.json`),
imports the restored database into the stack and plans with `database_generation =
<n>`; the apply points the service at it; the release's migrations run on it, which is
nothing when the backup is at the release's schema and the release's own files when it
is ahead; the release deploys and takes the traffic; the record names the requester, the
approver, the reason, the backup restored and the moment its data is from, the forensic
backup, the database restored into and the one kept, with the forensic backup standing
as the run's backup, which `bedrock backups` lists for a later restore to this release's
last data. The
live database stays, drop-protected, as the forensic copy: writes made after the
backup's moment are in it alone. No database an earlier run left is ever put back into
service, and nothing is dropped: a restore to the wrong place is followed by another
restore, from whichever generation holds the moment. Every later plan, a pull request's
included, reads the generation its records name, so the stack keeps pointing at the
restored database and the earlier generations stay protected; their removal is a later
item.

**The environment's own restore**, below production. Staging runs a release against
production's data before production does: a release that carries migrations production
has not applied restores staging from production's newest backup in its own build (the
staging rehearsal, decided by the resolve step from the versions the records say tst and
production applied), so between releases staging sits at production's release, and the
failure expected there is a migration meeting production's data. `restore stg` with no
source is the manual way to bring staging's data current between releases, migrations or
not, and the way back to production's release after a failed release there. So an
environment restored from production's backup (one on production's instance, off the
seed list) may leave the release out: the workflow's job reads production's live release
from production's deployment records, says which, and runs it; a release named is run as
named, and the job says whether it is production's. The first environment and a seeded
one restore to an empty database, which has no production state to return to, so they
name their release. The command checks that the environment is not production (whose
database is restored to a backup alone), that a release named exists, and that the
placement records the environment's project (`projects`, the id and the number, which
`bedrock org register` writes into the application's first placement), then dispatches
the operations workflow (`.github/workflows/operations.yml`, rendered and owned by
bedrock) as the person signed in to gh, and prints where to watch it; the Run workflow
button on the Actions tab starts the same job. The job runs in the GitHub Environment
named after the target environment, which the organization's `1-org` layer declares so
that it deploys from the default branch alone (the workflow file a restore runs is the
committed one; a reviewer for an environment is the repository's setting to add). It
holds no key: it exchanges GitHub's short-lived token for the environment's operations
identity through the environment's workload identity pool (`2-env`), whose provider
trusts tokens of the organization's repositories alone, from the operations workflow
file, run in that Environment, and whose binding on the identity narrows that to the
application's own repository. With that identity, which may start the environment's
triggers and read the builds they start and nothing else (`1-org`'s
`cloudBuildTriggerRunner`), the job runs the environment's version trigger for the
release with `_RESTORE` and `_REQUESTER`, and waits for the build to its end, an
approval in Cloud Build included. The build does the work as the deploy identity, as
for any release; the GitHub side never holds a deploy right. The workflow run names who
started it, Cloud Build records the operations identity, and the deployment record
carries the requester and the restore. An environment the placement records no project
for is not wired: the job stops before touching anything and says what to record.

## bedrock rollback

`rollback <env> --reason <why> [--to <release>]` returns an environment to an earlier
release with nothing of the database, started from GitHub. It is the first answer to a
release that went wrong, in any environment: a schema change migrates forward in a way
the running code still works with, so the earlier release runs on the database as the
release left it. When the database is wrong too, `bedrock restore` returns it, in a run
of its own, after this one. A release that cannot migrate that way is marked breaking
(its outlets no longer answer the environment's release), goes under maintenance,
migrates, deploys and comes out; a problem found after it is a restore, and the writes
since the release are lost with it.

**The command** checks the environment and the inputs (a reason is required and may not
carry `|`; `--to` names a release that exists), prints the statement (what returns to
what, asked for by whom and why), asks for the environment's name typed, and dispatches
the repository's operations workflow as the person signed in to gh with the action
`rollback`, the reason and the release (`--to`, or none). The command changes nothing
itself.

**The workflow's job** runs in the GitHub Environment named after the environment; no
Environment waits for a reviewer, since the build the job starts waits for its approval
in Cloud Build where the environment requires one, as a release does, and the record the
run writes names the approver. The job reads the environment's newest deployment records
(through the version trigger's `_RECORDS_BUCKET`; the operations identity reads the
bucket) for the live release, which the environment leaves, and, unless named, the
release to return to (the release live before the live one), prints the statement with
them, and runs the environment's **rollback trigger**
(`<prefix>-<env>-<region>-<app>-rollback`, in every environment, disabled for events and
run by this job alone; never the release trigger) for the release returned to with
`_ROLLBACK` (the release left), `_REASON` and `_REQUESTER`. The build waits for its
approval in Cloud Build as a release does; thirty minutes without one and the job
cancels it, so a rollback nobody approved is not left waiting.

**The build**, as the deploy identity, is the earlier release's build again: the resolve
step prints the statement first and switches the migrations off; the release check, the
guard and the image build run as for any release (the image is reused when the commit
built one); the stack is applied as that release had it, at the database's generation as
it is; no backup is taken and nothing is restored; the release deploys the way a release
deploys, behind the maintenance page where the return is breaking for the environment's
clients and never waiting for a window; traffic moves; and the record names the
requester, the approver, the reason and the release left, and lists the migrations the
database holds, by the record live when the rollback ran, since the rollback applied
none, so the release guard and the staging rehearsal read the environment where it is.

## bedrock backups

`backups <env>` lists, in the summary of an operations workflow run, what an
environment's database can be restored to: every run that went live there, newest
first, with the release, the kind of run (a release, a rollback and the release it left,
or a restore and what it restored), when, its cut and its backup, which for a release's
run is the pre-release backup that holds the database as it was before that release's
migrations (fourteen days) and for a restore's run the forensic backup of the data the
restore replaced (thirty days), a rollback's run having none; and every generation of
the database a restore made, from which backup, when, the generation it left as the
forensic copy and that copy's forensic backup. `restore --before <release>` reads the
release's first run as a release, never a rollback's or a restore's record of its
version. A generation's own history reaches back the
placement's `spannerRetention` while the generation exists; the backups are the fixed
points. The command dispatches the workflow's `list` action as the person signed in to
gh and prints where to read the summary; the job reads the deployment records as the
operations identity, and nothing on the developer's machine touches the cloud. From the
listing, `bedrock restore` takes `--before <release>`, `--backup <name>` or `--at
<moment>` (with `--of <database>` for a moment in an earlier generation).

## bedrock maintenance

`maintenance off <env>` takes an application out of the maintenance a failed run left it
in: a run that put the maintenance page up and then stopped (a restore refused at its
plan, a cancelled build) leaves every region's traffic on the maintenance revision and
the task queue paused. The command dispatches the workflow's `maintenance` action as the
person signed in to gh; the job reads the environment's live release from its deployment
records and runs its build with `_MAINTENANCE=off`, which the resolve step turns into the
skip facts: every step stands down, and the last one moves each service's traffic back
to the revision the maintenance revision displaced (named by the label the maintenance
step put on it, `bedrock-displaced`; for a maintenance revision from a build before the
label, the service's latest ready revision when that is another one), says what the
application comes back to (the environment's database READY and never restored, or
restored by the stopped run, which left it without its memberships, since those come with
the stack's apply, so the application cannot start on it until `bedrock rerun` finishes
the run; or still being restored, about twenty minutes here) and resumes the queue the
earlier run left paused. Nothing deploys and no record is written; production's
build waits for its approval in Cloud Build as a release does. The other way out is to
run the release again (`bedrock rerun`), which deploys and ends the maintenance on the
way, and is the answer when the failed run should be finished rather than undone.

## bedrock rerun

`rerun <env> <release>` runs a release again in an environment, production included,
through the same door as a restore: the command checks that the release exists and that
the placement records the environment's project, dispatches the operations workflow with
the `run` action as the person signed in to gh, and prints where to watch it. The job
exchanges its token for the environment's operations identity in the environment's
GitHub Environment, as a restore does, and runs the version trigger for the release with
`_REQUESTER` alone: no restore, no migration operation. The build is the release's tag
build again, from the start, as the deploy identity: the image is built, the stack
applied, the migrations run (a migration that stopped at a statement continues from it
once the cause is fixed), the revision deploys, traffic moves, and the record names who
asked and, where the build waited for an approval, who approved it. Nothing of a rerun is a restore, so production is reached like any environment:
its operations identity exists for this action alone, the workflow and the pipeline
refuse the restore instruction and the migration operations there, and a rerun in
production waits for its approval in Cloud Build as every production release does. The
migration job's `rerun` option (`bedrock migration rerun`) was named first and runs the
release again below production with the migrate command's lines printed in the run; the
release's own action is `run`, and `bedrock rerun` is its command. Nobody runs a trigger
or submits a build by hand: every build starts from a trigger, and a release is run again
through this door.

## bedrock domain

`domain add <domain>` puts a domain registration into the network layer's placement,
where the layer's `domains.tf` registers it through Cloud Domains. `domain check` says
whether the apps domain resolves to the network layer's zone and, where it does not,
prints what is missing and where to add it.

```sh
bedrock domain add example.dev   # the default for a new domain: 2-net registers it
bedrock domain check             # from the organization's infrastructure repository root
```

The applications' domain (`appsDomain` in the organization's placement) takes one of three
shapes, and the check finds which one from the live answers; nothing in the placement says
so:

- **Registered by 2-net, the default.** A new organization buys its apps domain through
  Cloud Domains in the network layer: one placement entry, registered and pointed at the
  zone by the next apply, with no registrar step. Cloud Domains accepts no transfer in, so
  a domain the client already owns takes one of the next two shapes.
- **Delegated at the apex.** A domain as it is registered (its apex, such as
  `example.com`) with nothing else on it, registered anywhere: its name servers at the
  registrar are set to the zone's, once.
- **Delegated as a label.** A domain that already carries a website or mail is never
  delegated whole. The applications live under a label of it (one more name in front,
  such as `apps.example.com`), and the client's DNS provider gets one NS record set for
  the label; everything under it is then the network layer's.

Application hostnames directly on a domain whose zone stays elsewhere are refused: every
application and environment would need its own records at that domain's DNS provider.
Pointing a label at the load balancer with static records, instead of delegating it, is
not supported.

`domain check` runs from the infrastructure repository's root (or `--dir`) once 2-net is
applied. It reads the zone in the network project (`projects.net` in `placement.json`)
through the Cloud DNS API with the run's Google credentials (`gcloud auth
application-default login`, reading the project's zones), and resolves the domain as the
world sees it. When the domain answers the zone's name servers, it passes. Otherwise it
prints the step and the place, each record on its own line as it is pasted: for a domain
2-net registers and whose registration is not active yet, the registrant's verification
mail, followed within fifteen days or the domain is suspended; for a domain 2-net registers
whose registration names name servers that are not the zone's (it answers another set, or
the servers it is delegated to do not answer for it), as after the zone was made again, the
step in Cloud Domains that points the registration at the zone (`gcloud domains
registrations configure dns <domain> --cloud-dns-zone=<zone> --project=<network project>`,
or the console's Cloud Domains page, the domain, Edit DNS details), since the apply never
changes the name servers of a registration that exists; for an apex, the zone's
name servers to set at the registrar where the domain is registered, as bare host names
(`ns-cloud-c1.googledomains.com`, since a registrar takes host names; at Squarespace
Domains: the domain's DNS settings, Domain Nameservers, Use Custom Nameservers, up to 48
hours to take effect); for a label, the NS records to add at the DNS provider that serves
the domain it belongs to, each as the provider's form takes it: the host relative to that
domain, the type, and the value without the trailing dot a zone file writes
(`apps NS ns-cloud-c1.googledomains.com`; Squarespace, for one, refuses the dot as a
character), as is the authorization record when it is to be added at a provider
(`_acme-challenge.apps CNAME ...`); a record the zone itself holds is shown as the
zone-file line it is. An apex that is not delegated and answers records that are not
the zone's is refused, with a label of it named instead. The check also resolves the
record that proves the domain to Certificate Manager and says when the certificate is
still waiting on it. It exits 1 when anything is missing or refused.

A domain 2-net registers has a registrant contact (`registrant_contact` in
`2-net/terraform.tfvars`) that is the client's, never that of a contractor who builds or
runs the foundation, with a mailbox a person reads: the verification mail and every notice
about the domain go there. `bedrock org check` reads each registration back through Cloud
Domains and reports, before anything else, a registration still waiting on the
verification, with the mailbox and the date its link must be followed by, then each
registration's state and expiry date (bedrock org, below). The registration lives in the network project and cannot move
to another project; deleting the project loses access to the domain, which is one reason
1-org puts a lien on every project it creates (a mark that refuses the project's deletion
until it is removed). If the client wants the domain at another registrar, it transfers it
out:

- sixty days after the registration at the earliest, to any registrar but Squarespace
  Domains, which already holds the domain at the registry on Cloud Domains' behalf;
- after Cloud Domains unlocks the domain, adding a year of registration at the new
  registrar's price;
- the name servers do not change in a transfer, so the zone keeps answering;
- once it is done, the registration leaves `registrations` and the state, with
  `prevent_destroy` on the registration in 2-net's `domains.tf` lifted for that one apply.

Cloud DNS gives a zone its name servers when it creates the zone, and a zone made again can
land on a different set, which the registration, the certificate's authorization record and
any delegation made by hand would no longer point at. So 2-net's apps zone and every zone a
registration points at carry `prevent_destroy`: a recreation is refused by default and is a
deliberate change of three renders and one hand step, which 2-net's README describes under
"Making a zone again". The organization's placement takes `"zoneReplacement": true` and
`bedrock org render` writes the zones without the rule; the change that recreates the zone
is rendered with the value still set; by hand, everything that pointed at the old name
servers is pointed at the new ones, `bedrock domain check` printing each step; and the
value is cleared and rendered, which writes the rule back. The files are never edited by
hand. While the value is set, `bedrock org check` names it and what is left to do, and a
`bedrock domain check` that passes says it can be cleared now; neither fails on it.

## bedrock org

`org` renders and checks the organization foundation from the organization's placement.

```sh
bedrock org new ../infrastructure --placement placement.json   # a foundation for an organization that has none
bedrock org preflight                                           # before the seed: the bootstrap administrator's roles
bedrock org render                                              # after a placement change
bedrock org check                                               # the committed layers against the placement
bedrock org register quill ../quill                             # an application joins the foundation; its first placement.json is written into its checkout
```

`org new` renders the six layers, each with its `.tf` files, its README and its seeded
`terraform.tfvars`, the layers workflow (`.github/workflows/layers.yml`), and at the root
the README, the journal, the ignore rules and the OpenTofu version, then prints the hand
steps the model needs before the workflow can run. Everything the seed decides is
`REPLACEME` in the seeded values until it has run.

A new organization is set up in this order. Each step opens with the place it happens,
and the rendered root README ("How the layers are applied", "The two GitHub Apps") and
`0-bootstrap/README.md` carry the commands:

1. In the Workspace Admin console: one team group per environment (`teamGroups`). On
   GitHub, as an organization owner: the organization, its machine account, the
   infrastructure repository, the release and deployer apps installed on all
   repositories, and the release app's two organization secrets (GitHub prerequisites,
   below). The team groups, the machine account and the release app's App ID and slug go
   into the placement, and `org new` renders.
2. In a terminal, as the bootstrap administrator: `gcloud auth application-default
   login`, then `bedrock org preflight`, which must find every role held, then the seed.
3. In this repository: the seed's values in the placement and `org render`; then, in a
   terminal, the first apply of 0-bootstrap on local state and its migration into the
   bucket.
4. In a terminal, as a billing administrator: the two billing grants. In the Billing
   console, as the same administrator: the spend budget, which nothing renders.
5. In a terminal: the first apply of 1-org, then its projects in the placement and
   `org render`.
6. In a terminal, in this repository: the infrastructure app's key, `bedrock secret add
   github-infrastructure-key` and `bedrock secret pin github-infrastructure-key
   <version>`, with its App ID in the placement.
7. In the first environment's Cloud Build console, signed in to GitHub as the machine
   account: the Cloud Build GitHub authorization, before the first application.
8. In a terminal, in this repository, per environment once 2-env has applied there: the
   deployer app's key, `bedrock secret add github-deployer-key <env>` and `bedrock secret
   pin github-deployer-key <env> <version>`.
9. In each environment project's Google Cloud console: the consent screen, with the
   audience Internal, before the first application's OAuth client is made there.
10. In the Workspace Admin console, for each application before its first sign-in: its
    role groups, `<group prefix><role>@<domain>`, each set so that its members can view
    its member list, since the sign-in reads a person's groups with the person's own
    token and Google leaves out a group whose member list the person may not view.
11. After the first apply of 2-net: `bedrock domain check`.

The organization's placement names its GitHub machine account (`githubMachineAccount`):
the login of a GitHub user that belongs to the organization as an owner and acts for no
person. In the browser step that authorizes the Cloud Build GitHub connection, before the
first application (`2-env/README.md`, "The GitHub authorization, before the first
application"), a person signs in to GitHub as that account, so the connection's token is
the account's and nobody's leaving breaks it; a personal access token of the same
account is the alternative to that step. `2-env` renders the login where it names the
account. The field is required: a placement without it is refused.

The organization's two regions (`regions`, the primary first) and its Spanner
configuration (`spanner.config`, the instance 2-spn creates for stg and prd) are chosen
together: the configuration is a multi-region one whose two read-write regions, where the
replicas that take writes are, are the two Cloud Run regions, so each region's service
writes to a replica in its own region and either region can lose the other. bedrock knows
these configurations: `nam10` (us-central1 and us-west3), `nam6` and `nam11` (us-central1
and us-east1), and `nam7`, `nam12` and `nam16` (us-central1 and us-east4). A placement
naming one whose read-write regions are not its two regions, or one bedrock does not know,
is refused, and the refusal names the configurations that fit the regions. An
environment's own instance (2-env's `spanner_instances` in its `terraform.tfvars`, by
default the first environment's) is regional in the primary region, where the primary
service runs, or on a multi-region configuration that fits the regions; 2-env's plan
refuses any other. The placement's `outlierDetection` is the thresholds of the backend
service 2-env renders for the pull-request environments, and those an application's first
placement takes (The application's placement, above, names each field); its `cloudArmor`
is the Cloud Armor policy an application's first placement starts with, which an
environment turns on in the application's `terraform.tfvars` (the organization's own
backends carry none).

`org preflight` is the check before the seed. The seed and the first applies of 0-bootstrap
and 1-org run as the bootstrap administrator, a person, before any layer identity exists, so
that person must hold the roles they need. Run with the administrator's Application Default
Credentials (`gcloud auth application-default login`), it asks Google which of the
permissions those runs need the caller holds: on the organization `placement.json` names
(Cloud Resource Manager's `testIamPermissions`) and on its billing account (Cloud Billing's).
The roles are Folder Creator, Project Creator, Organization Administrator, Organization
Policy Administrator, Organization Role Administrator and Tag Administrator at the
organization, and Billing Account User on the billing account. It prints one line per role,
`holds` or `missing` with the permissions tested, and exits 1 when a role is missing, saying
who grants it where, or when it could not check (no credentials, or an API that refused).

`org render` rewrites the owned files from the placement, seeds the absent ones and says
what the workflow still lacks in the placement; `org check` compares the owned files, the
workflow among them, and exits 1 on drift. Before anything else, `org check` reports each
domain 2-net registers (`registrations` in `2-net/terraform.tfvars`) as Cloud Domains
holds it in the network project: first every registration whose registrant mailbox is not
verified yet, naming the mailbox and the date the registrar's verification mail must be
followed by (fifteen days after the registration, or the domain is suspended), then each
registration's state and expiry date, an expiry within thirty days with the billing
account to look at (renewal is automatic while it is active), and any other issue the
registrar raises. It reads the registrations with the run's Google credentials
(`roles/domains.viewer` on the network project); without any, or when Cloud Domains
cannot be reached, it says so and never fails the check. `org check` then lists each person (a `user:`
member) holding `roles/owner` on an environment project the placement records, with the
project: the grant a project's creator receives, which the first apply of 1-org by hand
leaves the bootstrap administrator with on every project it creates, temporary by design
and removed by hand once the workflow applies the layers (`1-org/README.md`, "Applying").
The listing reads the projects' IAM policies with the run's Google credentials (`gcloud
auth application-default login`); without any it says so, and it never fails the check.
Last it lists each API key in an environment project that carries no API restriction (the
list of APIs that accept the key; a key with none is accepted by every API in the project
that takes an API key), with the same credentials and on the same terms: Firebase's browser
key, which initializing Identity Platform creates in each environment project, when
Firebase has made it again since the workflow restricted it, or any other key
(`2-env/README.md`, "Identity Platform").

The layers workflow plans every layer a pull request changes, as that layer's plan
identity, and posts each plan on the pull request; the merge applies those layers as their
apply identities, in layer order (0-bootstrap, 1-org, 2-shr, 2-spn and 2-net, then 2-env
for tst, stg and prd), one at a time, stopping at the first failure. The plans run side by
side (`tofu plan <layer>`) and each is judged once all have finished (`plan <layer>`): a
plan that fails only because it reads outputs the plan of an earlier layer in the same pull
request creates (an unsupported attribute that plan shows as a new output) passes, its
comment saying it is planned after that layer applies, since the merge applies that layer
first; any other failure fails. The judgment is a script bedrock renders into the workflow
(`internal/org/planverdict.sh`, run by its tests over both outcomes). Run workflow on the
Actions tab applies one layer again with no change to it. Each layer's apply holds a
concurrency group of its own (`layers-apply-<layer>`; GitHub runs one job of a concurrency
group at a time), so an apply waits for another apply of the same layer and for no other
layer; GitHub keeps one job of a group waiting behind the running one, and a third that
arrives cancels the one waiting, so where several layers go by Run workflow, start each
when the previous has finished. After each apply of 2-env it
restricts Firebase's browser key ("Browser key (auto created by Firebase)") in the
environment project to the two sign-in APIs, identitytoolkit.googleapis.com and
securetoken.googleapis.com, as the layer identity: no layer can declare the key, since
OpenTofu adopts a key only by the id Firebase assigns it and the Google provider finds no
key by its name. No key exists anywhere: a run
signs in through a workload identity pool in the boot project (0-bootstrap's `github.tf`)
whose provider trusts tokens of the infrastructure repository from that workflow file
alone and maps each token's event and ref to plan or apply, and each identity's binding
admits one of the two, so a pull request's code reads and never writes. The placement
names what the workflow runs with: every project under `projects` (`boot` from the seed,
the six others from 1-org's `project_ids`), the boot project's number under
`projectNumbers.boot` (the provider's name), and the infrastructure GitHub App whose token
1-org's GitHub provider takes (`githubInfrastructureAppId`, and
`githubInfrastructureKeyVersion`, the pinned version of its key in the boot project's
container; `0-bootstrap/README.md` has the person's steps). The first applies of
0-bootstrap and 1-org are the bootstrap administrator's, before the identities exist;
recovery is by hand, for 2-env by a member of the environment's team group under the
layer administrator entitlement (the environment layer identity's roles on the environment
project) and the layer state entitlement (its slot in the state bucket, on the boot
project), running the layer as themselves, for the other layers as the bootstrap
administrator.

`org register <app>` adds an application: its code goes into `placement.json`'s
applications, each layer's values are rendered from it (1-org's repositories and its
`public-invokers.auto.tfvars`, 2-env's list, 2-shr's pushers and pullers, 2-spn's database
admins, 2-net's hostnames with the wildcard for pull-request environments), and the pull
requests the registration takes through the workflow are printed, since one pass in layer
order does not follow the order the grants need: first 1-org's repository with 2-env's
identities, then 2-env again from the Actions tab (each environment grants the next
environment's deploy identity read on its records bucket, from state the first pass did not
have), then 1-org's public-invoker grants with 2-shr and 2-spn (grants on identities that
exist now), then the application's own stack per environment, then 2-net. A file a later
pull request carries stays in the working tree until then.

`org register` also makes the application's first `placement.json` (The application's
placement, above), every field from the organization's placement and none asked of the
person, pinned to the bedrock running it: a release with its linux/amd64 binary's
checksum, read from the release's `checksums.txt` as `bedrock upgrade` reads it, or a
commit installed with `go install`; a build from a checkout or a `(devel)` build is
nobody's pin, and register refuses to run on one. The optional second argument is the
application's checkout (`bedrock org register quill ../quill`, typically a sibling of the
infrastructure repository's checkout): register writes the file there as
`infrastructure/placement.json`, where `bedrock render` reads it, and refuses when one is
there already, since a placement is never overwritten. Without it, register prints the
file to be committed there. Register refuses, changing nothing, while the organization's
placement lacks the state bucket (`stateBucket`) or an environment project's id or number
(`projects`, `projectNumbers`), which the application's placement names.

The application's GitHub repository is configured by `1-org`, with the GitHub provider,
never by a bedrock command: the repository itself (private; squash the only merge method,
the squashed commit titled from the pull request; the head branch deleted on merge; never
destroyed by the layer), its three rulesets (a `v*` or `*/v*` tag created, moved or
deleted by the release app alone, with no bypass for the repository's admins, which is
what makes the pipeline's tag check sound; the default branch and the hotfix lines
changed by pull request alone, with the branch up to date with its base, the
infrastructure workflow's `bedrock check` and the six fixed jobs of the application's CI
workflow (`title`, `go`, `web`, `image`, `secrets`, `migrations`, read from impulse's `ci`
package; `web` gates the per-workspace browser jobs, so a failed browser build blocks the
merge) passing on its latest commit, never the pull-request build (the developer's preview
on `/gcbrun`),
squash the only merge and, when the placement names an infrastructure team, that team's
approval of a change to the workflow and Cloud Build files), and the GitHub Environments
the operations workflow runs in (every environment, production's for the rerun of a
release and the rollback, each deploying from the default branch alone). The workflow applies it with the
infrastructure GitHub App's
installation token, minted in the run; a person applying by hand uses their own sign-in,
`GITHUB_TOKEN` from `gh auth token`, after reading the plan; the placement names the release app by its
App ID (`githubReleaseAppId`, from the app's settings page: a private app cannot be read
by its slug) and also records the slug (`githubReleaseAppSlug`, the name in the app's
address, `github.com/apps/<slug>`), which each application's placement names as the
author of its releases, the default branch (`githubDefaultBranch`), the team
(`githubInfrastructureTeam`, empty for none) and the larger runner the applications' CI
runs the jobs their `//impulse:ci` lines choose on, the two test legs and the image build
without a line (`ciLargeRunner`, a runner's name, a runner label or a runner group set as
the organization's Actions variable `CI_LARGE_RUNNER`, which takes the infrastructure
app's Variables organization permission; empty for the standard runner). With
`ciLargeRunnerSize` (`8-core`, say) 1-org creates that runner itself: a GitHub-hosted
runner of the name and size on GitHub's current Ubuntu, in a runner group of the
application repositories alone, capped at `ciLargeRunnerMaximum` running at once (0 for
GitHub's default), through the app's Self-hosted runners organization permission; GitHub
bills it per minute by its size on the Team and Enterprise plans. A repository that existed before the layer
declared it is imported into the state first; `1-org/README.md` lists the commands.
bedrock's commands use the GitHub API only to act: `restore` dispatches a workflow,
`hotfix` creates branches and pull requests, the pipeline talks back on a pull request. OpenTofu reads `*.auto.tfvars` after `terraform.tfvars`, which keeps what a person
decides. Once `placement.json` records the environment projects' ids and numbers
(`projects` and `projectNumbers`, from 1-org's `project_ids` and `project_numbers`
outputs), `org render` prints the `projects` block an application's placement records
for the operations workflow, which starts a restore or a rerun of an environment from
GitHub (`bedrock restore`, `bedrock rerun`), and `org register` writes it into a new
application's first placement; production's entry serves the rerun alone.

## GitHub prerequisites

Every repository the layers make is private, so the GitHub organization that holds the
infrastructure repository and the applications' repositories must offer the four features
below on private repositories. Which of them a private repository gets depends on the
organization's GitHub plan, so its plan must include all four.

- **Rulesets that GitHub enforces on private repositories.** `1-org` gives each
  application's repository three rulesets. The tag ruleset lets the release app alone
  create, move or delete a release tag, which is what makes the pipeline's tag check
  sound; a ruleset GitHub shows but does not enforce leaves the check unsound.
- **Environments on private repositories.** The operations workflow runs a restore, a
  rerun or a rollback in a GitHub Environment per environment, each deploying from the
  default branch alone. No Environment requires reviewers: that rule needs GitHub
  Enterprise on a private repository, and the approval is Cloud Build's instead.
- **Organization secrets that reach private repositories.** The release app's
  `RELEASE_APP_ID` and `RELEASE_APP_PRIVATE_KEY` are set once for the organization,
  visible to all repositories, and each application's release workflow makes the app's
  token from them. Writing an organization secret takes an organization owner signed in,
  or an owner's token carrying the organization-administration scope; a token without
  that scope is refused.
- **GitHub Apps owned by the organization.** The release app (Contents, Issues and Pull
  requests, read and write), the deployer app (Checks, Deployments, Issues and Pull
  requests, read and write) and the infrastructure app (`0-bootstrap/README.md`:
  repository Actions, Administration and Metadata, organization Administration and
  Variables), each
  installed on all repositories, and the Google Cloud Build GitHub App, installed when
  the connection is authorized.

And one account: the machine account (`githubMachineAccount`, under bedrock org above), a
GitHub user that is an owner of the organization and acts for no person, which authorizes
the Cloud Build GitHub connection.

## The application's pipeline

The pipeline a render writes runs on two triggers per environment: a pull request's
`/gcbrun` comment (the first environment only) and a release tag (every environment, in
promotion order, the later ones by a person's approval where the placement says so). A
pull-request build stands its own environment up under `<app>-pr<N>` on the pull-request
hostname, with its own database (or the environment's, on `/gcbrun shared-db`), and tears
it down on `/gcbrun down` or by the hourly sweep once the pull request closes. The build
talks back on the pull request: a deployment the sidebar shows, a comment with the release
and the database mode, the guard's refusals. The pull-request build is the developer's
preview, not a check the merge waits for: a pull request merges on `bedrock check` and the
CI jobs (`1-org/github.tf` names them), so release-please's release pull request merges
with no build, and a release's first build is the first environment's tag build.

The guards run before anything deploys: the migration guard (the sequence, one up file
each, no gaps; a committed schema migration never changes, a seed file may; against the
default branch for a pull request; a refusal names `bedrock migration renumber` as the
fix), and the plan guard (a pull-request stack may only create and change what carries
its own number).

A run that replaces the database (a restore run) puts the application into maintenance
first: the release's own image starts as a revision with `APP_MAINTENANCE` set, which
the framework's `maintenance` package turns into a maintenance page and 503 answers
with no database opened; the pipeline probes it for the marker before any traffic moves,
then the task queue is paused, the serving build's job executions are canceled, the old
revision's requests finish, and only then is the database replaced. The queue resumes
once the release's revision serves. A breaking release (one whose outlets no longer
answer the release the environment runs, read from the release file beside the
generated router) does the same inside the environment's maintenance window: the run
builds its image and makes its jobs, waits inside the build for the window
(`placement.json`, `maintenance`), then goes into maintenance, migrates, deploys and
moves traffic; the whole-build timeout is Cloud Build's 24-hour ceiling for that wait,
and every step keeps its own timeout (Maintenance windows, above).

## Development

The tool is a Go module in this repository, `ccc/bedrock`, with `main.go` at its root:
`go build -o bedrock .` there. It requires impulse (`ccc/impulse`) by version, like any
other module, never by a path to the working tree: `go install` refuses a module whose
`go.mod` has a replace directive, and a commit pin is installed that way. A change to both
commits and pushes impulse first, then moves bedrock to it with
`GOWORK=off go get github.com/cccteam/ccc/impulse@<commit>` in `bedrock/`. To try the two
together before pushing, name an untracked workspace file with `GOWORK`, outside the
repository root. Its packages: `internal/derive` reads the application into
a model, `internal/render` writes the stack from templates (goldens under
`internal/render/testdata/<application>`), `internal/check` compares, `internal/release` is the tool's own distribution (its
release assets, their checksums, the verified fetch), `internal/deploy`
holds the pipeline's steps over the Cloud Build, Cloud Run, Artifact Registry and Cloud
Storage APIs, `internal/org` renders the foundation, `internal/secret`, `internal/hotfix`,
and `internal/migration` hold the operations, and `internal/cli` is the
command surface. Every rendered file carries a comment naming what it comes from; a
change to a template is a change to the goldens, and the render test reads every golden
file, comments included.
