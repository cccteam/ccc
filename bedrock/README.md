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
  repository): the Cloud Run services and the migrate job, the database, the secret
  containers, the backend service, the triggers and the sweep schedule, per environment.
  Rendered by `bedrock render` from the code and the application's placement.
- **Placement**: `placement.json` beside the stack. For an application it records the
  organization's facts the stack needs, the bedrock the pipeline runs
  (`bedrockVersion`, and `bedrockSha256` for a release) and, when the organization
  sets one, the Cloud Build machine the pipeline's builds run on (`buildMachine`, one
  of Cloud Build's machine names; absent, Cloud Build's default); for an organization it records
  the prefix, the domains, the organization and billing ids, the regions, the Spanner
  configuration, the GitHub organization, the applications, the environment projects and
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
  secrets, the substitutions), the stack's `.gitignore` and the Dockerfile at the
  application root.
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

What the stack carries comes from declarations in the code: a config variable tagged as
a secret becomes a Secret Manager container mounted at a pinned version; a directory auth
becomes the registration variables, the redirect output and the hand steps in the README;
a main package under `cmd/deployment/migrate` becomes the migrate job's template (never
run; each build copies it into a job of its own, runs it once and deletes it); a main package
under `cmd/jobs` becomes the job process: a template Cloud Run job in the primary
region (never run, never deployed to; each build copies it into a job of its own, named
after it with the build's version, on the build's image), its runtime identity with the
site's project roles and, when it constructs the data level, the database user grant and
accessor on that level's secrets, its timeout, retries and resources as stack variables,
and, when the site's config declares `APP_JOBS_JOB`, that variable baked into each
build's image as the name of that build's job, with `run.invoker` for the site's
identity on the template job, copied by the pipeline onto each build's job, so the
running service, and only it, starts the job of its own build through the Cloud Run API
(a schedule calls an endpoint on the service; the pipeline runs only the migrate job); a config variable
`APP_FILE_STORE` (the application's default file store) or `APP_FILE_STORE_<NAME>` (a
named store, `APP_FILE_STORE_DOCUMENTS`) becomes a Cloud Storage bucket in the primary
region, one per variable, named `<app>-files-<project number>` or
`<app>-files-<name>-<project number>` with the name in lower case and hyphens, the
variable set to the bucket's `gs://` URL for the processes that construct its level, with
`objectUser` for the site and, when it constructs that level, the job process, as the
bucket's whole permission list (set on every apply, so Cloud Storage's default grants to
the project's basic roles are gone from it and a grant added on the bucket by hand does
not outlive the next release), and
neither the URL nor a grant for the migrate command; a config variable `APP_TASKS_QUEUE` becomes a
Cloud Tasks queue in the primary region (a pull-request stack enqueues on the
integration environment's), the variable set to its resource name, with `enqueuer` on
the queue and Service Account User on its own account for the site and, when it
constructs that level, the job process, so a task calls the application back with the
enqueuer's OIDC token; a config variable `APP_FIRESTORE_DATABASE` becomes a Firestore
database in Native mode beside the Spanner database, the variable set to its id and
`GOOGLE_CLOUD_FIRESTORE_PROJECT` to the environment project (the Spanner project is the
shared instance's where one is shared, so the database's project is told on its own), with
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
- a release-please configuration (`release-please-config.json`) with
  `bump-patch-for-minor-pre-major` true, at the top level or for a package: below 1.0 a
  feature release would bump the patch and stay on production's hotfix line
  (`v<major>.<minor>.x`), so a hotfix of production's release could be neither numbered
  (the line's next patch is taken) nor passed by the hotfix check (the feature's
  migrations are what the environment would be restored to). Off, a feature opens a new
  line, a fix bumps the patch and a breaking change the minor.
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

`render` and `check` refuse a placement with no pin at all (a new application runs
`upgrade` first), and a placement pinned to another bedrock than the one running them
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
  `shared-db`, `reload-db`, `down`), the image and its tags, whether the migrate job runs
  and traffic shifts, and whether a stale pull-request database is recreated (a migration
  the last build applied is no longer in the tree). A tag build may carry a restore
  instruction (`_RESTORE`: `empty`, or `production-backup` for the environment on
  production's instance, with `_REQUESTER` naming who asked): the environment's database
  is replaced before the release deploys. A pull-request build carries none, and
  production is never restored by a run. `_REQUESTER` alone is a rerun (`bedrock rerun`:
  the release's tag build again, production included), which the record names. In an
  environment on the placement's seed list
  (`_SEED` true), a tag build decides a restore itself when the tree no longer carries a
  seed file as the environment's live release applied it (its record lists the seed files
  with their hashes; edited, renumbered or removed since): the release is the requester,
  the reason goes on the record (`RESTORE_REASON`), and a restore asked for takes
  precedence. A seed file added beside the applied ones recreates nothing.
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
  differs, naming the file; the environment is restored to the hotfix first (a restore
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
  build, which is a required check. The plan the reviewer approves is each
  environment's.
- `deploy pr-stack plan`, `guard`, `apply`: a pull request's own environment, the stack
  applied into its own state prefix as the apply identity. The plan is saved, the guard
  lets only the pull request's own resources through, the apply applies exactly that
  plan and leaves the pull request's services, jobs and hostname for the steps after (a
  destroy on `/gcbrun down`). A tag build skips all three.
- `deploy check-release`: reads the registry before the image build. Neither tag exists,
  the build runs; the commit is built and the release tag is not, the release name is
  added to that build; both exist and agree, the build is reused; the release tag names
  another build, the run is refused.
- `deploy build-image`: builds the checkout's Dockerfile with docker and pushes the image
  under its two tags, with the build arguments and the declared build secrets (read as
  the deploy identity into memory and passed as BuildKit secrets, never build
  arguments); the digest goes to `environment.sh`. The build runs in a BuildKit container
  (buildx's docker-container driver, created for the build: the one driver that
  exports a cache) and pushes a plain image. It reads a layer cache from the
  registry and writes its own there (`cache-<commit>`): this commit's, the commit the
  environment runs live, and in a pull-request build the pull request's last build. Layers
  are content-addressed, so the cache changes nothing in what a build produces; it spares
  the work whose inputs are unchanged, and the environments after the first rebuild the
  same commit from the cache alone. Every environment still builds its own image from the
  commit; nothing is promoted between environments.
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
  restores it, under its own name, from the most recent backup of production's database
  on the instance they share, as the apply identity; the plan then recreates the
  memberships the drop took with it, and the migrate job applies whatever production's
  backup predates. The backup and the moment its data is from reach the record
  (`RESTORE_BACKUP`, `RESTORE_BACKUP_TIME`). While the application is in maintenance the
  plan carries the maintenance variable's live value (`-var maintenance=1`): the stack
  declares `APP_MAINTENANCE` with `var.maintenance`, empty by default, and an entry of the
  service's env set cannot be ignored on its own, so declared and live agree and the apply
  leaves the service alone while the database is replaced.
- `deploy migrate`: runs this build's migrate job, the copy `deploy jobs` made of the
  template on this image, once to completion, with the seed (`schema/devseed` as data
  migrations after the schema) where `_SEED` is true: every pull request, and a release
  build only in the environments the placement's seed list names; then deletes the job,
  whether the execution succeeded or failed (its logs stay in Cloud Logging, and the
  deployment record lists the migrations applied). The lines the job wrote are read from
  Cloud Logging through the view over the job's own log bucket (`_MIGRATE_LOGS`, the
  stack's `logging.tf`) and printed after the run, so a failed migration's message is in
  the build log; production has no view, and the step prints the query that finds them.
  A release build may carry a migration operation (`_MIGRATE_ACTION`, from the
  operations workflow: `version`, `rerun` or `force`, with `_MIGRATE_TABLE`,
  `_MIGRATE_VERSION` and `_REQUESTER`; `bedrock migration`, below): `version` runs the
  job once with `-version` and leaves `SKIP_DEPLOY` with the reason, so nothing else
  deploys; `force` runs it with `-force <n>` (or `-force-data <n>`), leaves the force for
  the record, then runs the job as it always does; `rerun` is the job as it always does.
  An operation that cannot run (an unknown action or table, a force whose version is
  missing or not an integer, a force without a requester) is refused by `deploy resolve`
  and again here, before any job runs.
- `deploy jobs`: makes this build's jobs right after the image build, before the
  migrations, as copies of the stack's template jobs (`_MIGRATE_JOB`, `_JOBS_JOB`) named
  after them with the build's version, on this build's image with the pipeline's labels:
  the migrate job, when the build runs migrations, and the job process's job (`cmd/jobs`)
  with the template's IAM policy (the site's `run.invoker`). It runs neither; the image the
  build made names the job process's job to the site (`APP_JOBS_JOB`), so the revision
  starts the job of its own build and a traffic rollback starts the earlier one. Made
  before the migrations so that a failure here leaves the database untouched.
- `deploy migrate --preflight`: in a run that waits for the maintenance window and
  replaces no database, runs this build's migrate job once with `-version` before the
  wait, and keeps it: the job starts on the release's image against the environment's
  database and prints what the migrations tables say, so an image that does not start, a
  configuration that does not load or a database that cannot be reached stops the run
  with nothing changed and the window not entered. Nothing is applied. Any other run says
  so and does nothing.
- `deploy window`: in a run that waits for the maintenance window, holds the run at the
  gate until the environment's window opens, with the image built and the jobs made, so
  the window holds only maintenance, the migrations and the rollout. It reads the
  setting from the checkout's `placement.json`, prints when the run will proceed and
  waits, reading the clock again at most every ten minutes; when the window opens it
  leaves when, how long it waited and which opening let it in (`WINDOW_OPENED`,
  `WINDOW_WAITED`, `WINDOW_SLOT`) for the record. An opening further away than the build
  can wait stops the run here. A run in maintenance already (a restore run, or a rerun
  after a window release that failed) passes at once.
- `deploy sweep-jobs`: after the traffic shift, deletes the builds' jobs nothing runs any
  more: a job of the job process whose version no revision in any region carries (a
  revision that exists can take a rollback, and then starts its own build's job), and a
  migrate job a run that did not finish left behind. A job with an execution running, or
  made in the last three hours (its build may still be running), stays; the templates
  always stay. Nothing retires a revision: that is Cloud Run's own ceiling. Jobs are
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
  `REVISION_URLS` are left in the workspace for the hook before traffic. The template is
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
window), a window with no opening ahead; the image is built, the stack applied and the
build's jobs made, so nothing that can fail on its own is left for the window; the
pre-flight runs the migrate job once with `-version` (`deploy migrate --preflight`);
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
bedrock migration rerun tst --release v1.4.0                # run the release again; the migrate job continues from where it stopped
bedrock migration force tst 40 --release v1.4.0             # set the schema version to 40, then let the release continue
bedrock migration force tst 2 --release v1.4.0 --table data # the same for the data migrations table
bedrock migration force tst none --release v1.4.0           # no version at all (the migrate command's -1)
```

The job runs in the GitHub Environment named after the target, exchanges its token for
the environment's operations identity as the restore job does, and runs the environment's
version trigger for the release with `_MIGRATE_ACTION`, `_MIGRATE_TABLE`,
`_MIGRATE_VERSION` and `_REQUESTER`. `deploy migrate` reads them after the usual steps
have run (the release check, the record gate, the image): `version` runs the job once
with `-version`, prints its lines, and the run stops before the service, the traffic shift
and the record, so nothing in the environment changes; `rerun` is the release run again,
the job running as it always does; `force` runs the job with `-force <n>` (or `-force-data
<n>`), prints its lines, then runs the job as it always does (with `-seed` where `_SEED`
is true), and the release continues to the service, the traffic shift and the record,
which carries the force (the table, the version, the requester). A version that is
missing or not an integer, an unknown action or table, or a force without a requester is
refused before any job runs; a force whose second run fails leaves the run failed like
any failed migrate job, with the runner's message. Cloud Run returns no output with an
execution, so the pipeline reads the job's lines from Cloud Logging through a view over
the migrate job's own log bucket (the stack's `logging.tf`: a sink on the job's name
fills the bucket, and the deploy identity and the operations identity hold
`roles/logging.viewAccessor` on that view and nothing wider, so neither reads the
application's own logs) and prints them into the build log, for a plain run too, so a
failed migration's message reaches the person who started the run without a console; the
workflow run's summary carries the same lines. Production has no door and no view: its
procedure is under When the migrate job fails, below. The command itself changes nothing:
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
migrate job finds a database at a version it knows, and the environment runs the hotfix
until the held-up release resumes. When the later release did move the database, the
environment holds a file the hotfix does not carry, and the release check refuses the
hotfix there, naming the file; the environment is restored to the hotfix first, from an
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
the pull request builds). The run refuses the instruction in production. `bedrock restore` starts it from GitHub (below).
For the environment on production's instance the database is not emptied but restored
from production's most recent backup, at production's schema: the plan step drops it
and restores it under its own name as the apply identity, and the migrations production's
backup predates then apply. The environment's file objects are kept, and its Firestore
documents are deleted as in every restore.

## When the migrate job fails

A migration file that fails leaves the database at that file's version, dirty, and the
job's message, in the build log and in the operations workflow run's summary when the
run was started from GitHub, says which of three cases it is. Each case names the
workflow's `action` input and the `bedrock migration` command that dispatches it.

1. **The job stopped at a statement and can continue.** The message reads `<file>
   stopped at statement <n> of <m> (<statement>): <cause>; fix the cause and rerun,
   which continues from statement <n>, or force a version`: the runner recorded how far
   the file got. Usually the statement validated existing rows (a `NOT NULL`, a check
   constraint, a unique index) and a row failed it. Fix the rows, then run the release
   again: action `rerun`, `bedrock migration rerun <env> --release <tag>`. The migrate
   job continues from the failed statement, applies the rest of the file and the files
   after it, and the release deploys. Nothing is repeated: the statements before it are
   applied and recorded, and the failed one had not applied.
2. **The job cannot continue.** The message says the database is dirty with no progress
   recorded (a database the old library left at cut-over), that a DDL operation it
   recorded is no longer there, or that the applied part of a file changed since. A
   person decides what the database really holds. First the state: action `version`,
   `bedrock migration version <env> --release <tag>`, which prints `schema: version 41,
   dirty` (and the data table's row) and changes nothing. Then the database: which of the
   tables, columns and indexes the file creates exist. Then the version the database is
   at: the version before the file when none of it applied, the file's own version when
   all of it did; action `force` with that version, `bedrock migration force <env> <n>
   --release <tag>` (`--table data` for the data migrations table, `none` for no version).
   The migrate job sets the row, prints it before and after, then runs the migrations
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
release does; so a migrate job that stopped at a statement continues from it once the
cause is fixed, and the record names who asked. The version and the force do not reach
production, and the pipeline reads no log view there: the platform operator runs those
two with their own credential, through the environment's version trigger, since that is
where a migrate job on the release's image exists (the stack's template job runs no image
of its own, and each build's copy of it is deleted at the end of the migrate step). The
trigger run takes the same substitutions the door passes, and the pipeline does the rest
as everywhere, the record naming the operator:

```sh
# The version: the migrate job prints it and nothing else deploys.
gcloud builds triggers run <prefix>-prd-<region code>-<app>-version --tag <release> \
  --region <region> --project <project> --substitutions _MIGRATE_ACTION=version,_REQUESTER=<you>
# A force, then the migrations and the release.
gcloud builds triggers run <prefix>-prd-<region code>-<app>-version --tag <release> \
  --region <region> --project <project> \
  --substitutions _MIGRATE_ACTION=force,_MIGRATE_TABLE=schema,_MIGRATE_VERSION=40,_REQUESTER=<you>
```

The build log prints the Cloud Logging query that finds the job's lines (the step reads
no view in production); the operator reads them in the console or with `gcloud logging
read '<query>' --project <project>`. Nobody runs the trigger by hand for a rerun, in
production or anywhere: `bedrock rerun` is its door.

## bedrock restore

`restore <env> <release>` restores an environment to a release, started from GitHub:
developers authenticate to GitHub and nowhere else, and nobody sets up a cloud tool to
operate an environment. The command checks that the environment is not production, that
the release exists, and that the placement records the environment's project
(`projects`, the id and the number, which `bedrock org register` prints), then
dispatches the repository's operations workflow (`.github/workflows/operations.yml`,
rendered and owned by bedrock) as the person signed in to gh, and prints where to watch
it; the Run workflow button on the Actions tab starts the same job. The job runs in the
GitHub Environment named after the target environment, which the organization's `1-org`
layer declares so that it deploys from the default branch alone (the workflow file a
restore runs is the committed one; a reviewer for an environment is the repository's
setting to add). It holds no key: it exchanges GitHub's short-lived token for the
environment's operations identity through the environment's workload identity pool
(`2-env`), whose provider trusts tokens of the organization's repositories alone, from
the operations workflow file, run in that Environment, and whose binding on the identity
narrows that to the application's own repository. With that identity, which may start
the environment's triggers and read the builds they start and nothing else (`1-org`'s
`cloudBuildTriggerRunner`), the job runs the environment's version trigger for the
release with `_RESTORE` and `_REQUESTER`, and waits for the build to its end, an
approval in Cloud Build included. The build does the work as the deploy identity, as
for any release; the GitHub side never holds a deploy right. The workflow run names who
started it, Cloud Build records the operations identity, and the deployment record
carries the requester and the restore. An environment the placement records no project
for is not wired: the job stops before touching anything and says what to record.

## bedrock rerun

`rerun <env> <release>` runs a release again in an environment, production included,
through the same door as a restore: the command checks that the release exists and that
the placement records the environment's project, dispatches the operations workflow with
the `run` action as the person signed in to gh, and prints where to watch it. The job
exchanges its token for the environment's operations identity in the environment's
GitHub Environment, as a restore does, and runs the version trigger for the release with
`_REQUESTER` alone: no restore, no migration operation. The build is the release's tag
build again, from the start, as the deploy identity: the image is built, the stack
applied, the migrations run (a migrate job that stopped at a statement continues from it
once the cause is fixed), the revision deploys, traffic moves, and the record names who
asked. Nothing of a rerun is a restore, so production is reached like any environment:
its operations identity exists for this action alone, the workflow and the pipeline
refuse the restore instruction and the migration operations there, and a rerun in
production waits for its approval in Cloud Build as every production release does. The
migration job's `rerun` option (`bedrock migration rerun`) was named first and runs the
release again below production with the migrate job's lines printed in the run; the
release's own action is `run`, and `bedrock rerun` is its command. Nobody runs a trigger
or submits a build by hand: every build starts from a trigger, and a release is run again
through this door.

## bedrock domain

`domain add <domain>` puts a domain registration into the network layer's placement,
where the layer's `domains.tf` registers it through Cloud Domains.

## bedrock org

`org` renders and checks the organization foundation from the organization's placement.

```sh
bedrock org new ../infrastructure --placement placement.json   # a foundation for an organization that has none
bedrock org render                                              # after a placement change
bedrock org check                                               # the committed layers against the placement
bedrock org register quill                                      # an application joins the foundation
```

`org new` renders the six layers, each with its `.tf` files, its README and its seeded
`terraform.tfvars`, the layers workflow (`.github/workflows/layers.yml`), and at the root
the README, the journal, the ignore rules and the OpenTofu version, then prints the hand
steps the model needs before the workflow can run (the seed, the bootstrap apply on local
state and its migration into the bucket, the billing grants, the first apply of 1-org).
Everything the seed decides is `REPLACEME` in the seeded values until it has run.

`org render` rewrites the owned files from the placement, seeds the absent ones and says
what the workflow still lacks in the placement; `org check` compares the owned files, the
workflow among them, and exits 1 on drift. `org check` then lists each person (a `user:`
member) holding `roles/owner` on an environment project the placement records, with the
project: the grant a project's creator receives, which the first apply of 1-org by hand
leaves the bootstrap administrator with on every project it creates, temporary by design
and removed by hand once the workflow applies the layers (`1-org/README.md`, "Applying").
The listing reads the projects' IAM policies with the run's Google credentials (`gcloud
auth application-default login`); without any it says so, and it never fails the check.

The layers workflow plans every layer a pull request changes, as that layer's plan
identity, and posts each plan on the pull request; the merge applies those layers as their
apply identities, in layer order (0-bootstrap, 1-org, 2-shr, 2-spn and 2-net, then 2-env
for tst, stg and prd), one at a time, stopping at the first failure; Run workflow on the
Actions tab applies one layer again with no change to it. No key exists anywhere: a run
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
recovery is by hand, for 2-env under the Layer administrator entitlement with
`GOOGLE_IMPERSONATE_SERVICE_ACCOUNT` set to the environment's apply identity, for the
other layers as the bootstrap administrator.

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

The application's GitHub repository is configured by `1-org`, with the GitHub provider,
never by a bedrock command: the repository itself (private; squash the only merge method,
the squashed commit titled from the pull request; the head branch deleted on merge; never
destroyed by the layer), its three rulesets (a `v*` or `*/v*` tag created, moved or
deleted by the release app alone, with no bypass for the repository's admins, which is
what makes the pipeline's tag check sound; the default branch and the hotfix lines
changed by pull request alone, with the branch up to date with its base, the pull-request
build (which Cloud Build reports under the trigger's name followed by the project in
parentheses) and the infrastructure workflow's `bedrock check` passing on its latest commit,
squash the only merge and, when the placement names an infrastructure team, that team's
approval of a change to the workflow and Cloud Build files), and the GitHub Environments
the operations workflow runs in (every environment, production's for the rerun of a
release, each deploying from the default branch alone). The workflow applies it with the
infrastructure GitHub App's
installation token, minted in the run; a person applying by hand uses their own sign-in,
`GITHUB_TOKEN` from `gh auth token`, after reading the plan; the placement names the release app by its
App ID (`githubReleaseAppId`, from the app's settings page: a private app cannot be read
by its slug), the default branch (`githubDefaultBranch`) and the team
(`githubInfrastructureTeam`, empty for none). A repository that existed before the layer
declared it is imported into the state first; `1-org/README.md` lists the commands.
bedrock's commands use the GitHub API only to act: `restore` dispatches a workflow,
`hotfix` creates branches and pull requests, the pipeline talks back on a pull request. OpenTofu reads `*.auto.tfvars` after `terraform.tfvars`, which keeps what a person
decides. Once `placement.json` records the environment projects' ids and numbers
(`projects` and `projectNumbers`, from 1-org's `project_ids` and `project_numbers`
outputs), `org register` and `org render` print the `projects` block an application's
placement records for the operations workflow, which starts a restore or a rerun of an
environment from GitHub (`bedrock restore`, `bedrock rerun`); production's entry serves
the rerun alone.

## The application's pipeline

The pipeline a render writes runs on two triggers per environment: a pull request's
`/gcbrun` comment (the first environment only) and a release tag (every environment, in
promotion order, the later ones by a person's approval where the placement says so). A
pull-request build stands its own environment up under `<app>-pr<N>` on the pull-request
hostname, with its own database (or the environment's, on `/gcbrun shared-db`), and tears
it down on `/gcbrun down` or by the hourly sweep once the pull request closes. The build
talks back on the pull request: a deployment the sidebar shows, a comment with the release
and the database mode, the guard's refusals.

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
