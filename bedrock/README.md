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
  bucket, the repository links). Rendered by `bedrock org` from the organization's
  placement into the organization's infrastructure repository.
- **Application stack**: the OpenTofu root under the application repository's
  `infrastructure/` directory (or the one layer under `3-app/` of an infrastructure
  repository): the Cloud Run services and the migrate job, the database, the secret
  containers, the backend service, the triggers and the sweep schedule, per environment.
  Rendered by `bedrock render` from the code and the application's placement.
- **Placement**: `placement.json` beside the stack. For an application it records the
  organization's facts the stack needs and the bedrock the pipeline runs
  (`bedrockVersion`, and `bedrockSha256` for a release); for an organization it records
  the prefix, the domains, the organization and billing ids, the regions, the Spanner
  configuration, the GitHub organization, the applications and the environment projects.
- **Environment**: `tst`, `stg` and `prd`, in promotion order. A pull request deploys to
  the first; a release goes through them in order, each after it is live in the previous
  one.
- **Owned file**: a file bedrock writes on every render and compares on every check: the
  stack's `.tf` files and its README, the pipeline files at the application root
  (`cloudbuild.yaml`, `cloudbuild-sweep.yaml`), the generate-time step
  (`cmd/generate/bedrock.go`) and the GitHub workflows (`.github/workflows/infrastructure.yml`,
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
  the file.
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
- **Release**: a tag `v<major>.<minor>.<patch>` cut by release-please as the release app;
  the pipeline accepts a release from that author only. A **hotfix line** is the branch
  `hotfix/<major>.<minor>.x` on which release-please releases the line's next patch
  versions while the default branch moves on.

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
a main package under `cmd/deployment/migrate` becomes the migrate job; a main package
under `cmd/jobs` becomes the job process: its own Cloud Run job in the primary region,
its runtime identity with the site's project roles and, when it constructs the data
level, the database user grant and accessor on that level's secrets, its timeout,
retries and resources as stack variables, and, when the site's config declares
`APP_JOBS_JOB`, that variable set to the job's resource name with `run.invoker` for the
site's identity, so the site runs it through the Cloud Run API; a config variable
`APP_ASSETS_BUCKET` becomes a Cloud Storage bucket in the primary region, named to the
processes that construct its level, with `objectUser` for the site and, when it
constructs that level, the job process; a config variable `APP_TASKS_QUEUE` becomes a
Cloud Tasks queue in the primary region (a pull-request stack enqueues on the
integration environment's), the variable set to its resource name, with `enqueuer` on
the queue and Service Account User on its own account for the site and, when it
constructs that level, the job process, so a task calls the application back with the
enqueuer's OIDC token; a config variable `APP_FIRESTORE_DATABASE` becomes a Firestore
database in Native mode beside the Spanner database, the variable set to its id, with
`datastore.user` under a condition naming that database alone for the site and, when it
constructs that level, the job process; the generated router's outlets become the
service's paths on the backend. The rendered README of the
stack explains every file and names the declaration it comes from.

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
  repeats that rule on every build and, in a pull-request build, also refuses a migration
  modified, renamed or removed against the default branch, and an index the default branch
  has taken since the branch was cut; `bedrock migration renumber` is the fix it names.
- an authoritative IAM resource (`*_iam_binding`, `*_iam_policy`) anywhere in the stack:
  such a resource replaces every member of its role on each apply, so a pull-request stack
  applying one would remove the environment's members. A `*_iam_member` adds one member.
- a build secret the Dockerfile mounts as required (`--mount=type=secret,id=NAME,required=true`)
  that some environment's `build_secrets` in `terraform.tfvars` does not declare, naming
  the environments: a release that passed the earlier environments would fail in the
  image build of the one lacking it. An optional mount passes with nothing said.

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
  the last build applied is no longer in the tree).
- `deploy validate-release`: for a tag build, the tag belongs to a GitHub Release cut by
  an accepted release actor, the tagged commit is on the default branch or at the tip of
  a hotfix line, and the record gate holds: the release is live in the previous
  environment. A refusal starts with `Build REJECTED` and says why. A tag build's log first
  names the bedrock running it, and says when that is a commit pin; a release may deploy
  with a commit pin.
- `deploy guard-migrations`: the schema migrations and the seed are each one sequence
  (the rule `bedrock check` applies), and in a pull-request build every migration the
  branch started from is still there unchanged and the sequence is read together with
  the default branch's. A refusal is posted on the pull request and names the fix.
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
  arguments); the digest goes to `environment.sh`.
- `deploy migrate`: updates the migrate job to this build's image and runs it to
  completion, with the seed (`schema/devseed` as data migrations after the schema) where
  `_SEED` is true: every pull request, and a release build only in the environments the
  placement's seed list names.
- `deploy jobs`: updates the job process's Cloud Run job (`cmd/jobs`, named by the
  stack's `_JOBS_JOB`) to this build's image and the pipeline's labels and does not run
  it; the application runs its job process. Rendered into the pipeline only when the
  application has one, after the migrations.
- `deploy service`: puts a new revision of the service in every region, receiving no
  traffic yet, after repairing a service a failed earlier deploy left inconsistent. The
  new revision carries the tag `next` (or the pull request's tag), under which the
  stack's next backend serves it at `<app>-<env>-next`; `NEXT_URL` and the per-region
  `REVISION_URLS` are left in the workspace for the hook before traffic.
- `deploy shift-traffic`: moves every region to 100 percent on its new revision, keeping
  the tags other revisions carry; a pull-request revision served under its tag alone
  leaves the traffic where it is.
- `deploy record`: writes the deployment record once traffic has moved.
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
`deployhook` package (`deployhook.Main(deployhook.Hooks{AfterMigrate: backfill})`), which
takes the four stages after the image build. bedrock reads the program's stages from its
`Hooks` literal, `check` refuses a Dockerfile that does not build it as `/hooks` and a stage
that has both a script and a function, and the image build (`deploy build-image --hooks`)
takes the program out of the image for the hook steps (`deploy hook <stage> --program`).
The pipeline has a step for each stage the application implements. `deploy sweep`
is the hourly sweep's one step: the pull requests whose services stand, which of them
are closed, and each closed one's stack destroyed.

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
template change and nothing elsewhere; after the apply the release is re-run in the
environment to move traffic to the new revision. An argument left out is asked for at
the terminal with the choices listed.

Both find the rest from where they run: the infrastructure root, the application, the
environment project (by its labels) and the container (by its labels and the variable);
`--dir`, `--app`, `--project` and `--container` override.

## bedrock migration renumber

`migration renumber` moves the migrations this branch added, up and down files together,
to follow the default branch's highest index with no gap, keeping their order; the seed
directory beside the migrations is renumbered the same way against its own sequence. A
migration the default branch holds is never touched: git says which files are the
branch's own, and the default branch is read from origin's copy of it when the repository
has one, else from the local branch, so fetch first. A tracked file moves with `git mv`;
an untracked one is renamed on disk.

```sh
bedrock migration renumber          # by hand
go generate ./...                   # through the rendered cmd/generate/bedrock.go, before the generators
```

It closes the two holes the pipeline's guard refuses a pull request for: an index the
default branch took since the branch was cut (the branch's migration moves up), and a gap
(the branch's migration moves down). Nothing to do prints nothing.

## bedrock hotfix

`hotfix start <release>` starts the hotfix line of a release: the branch
`hotfix/<major>.<minor>.x` at the release's commit, on which release-please releases
fixes as the line's next patch versions while the default branch moves on. The pipeline's
release check accepts a tag at the tip of such a line whose base on the default branch
carries a release tag of the same line.

## bedrock repository

`repository protect` puts the release and branch rules on the application's GitHub
repository, the one the command runs in, addressed through its origin remote: the
default branch and the hotfix lines change by pull request only, and a `v*` or `*/v*`
tag is created, moved or deleted by the release app alone, with no bypass for the
repository's admins, which is what makes the pipeline's tag check sound.

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
`terraform.tfvars`, and at the root the README, the journal, the ignore rules and the
OpenTofu version, then prints the hand steps the model needs before the first apply (the
seed, the bootstrap apply on local state and its migration into the bucket, the billing
grants). Everything the seed decides is `REPLACEME` in the seeded values until it has run.

`org render` rewrites the owned files from the placement and seeds the absent ones;
`org check` compares them and exits 1 on drift.

`org register <app>` adds an application: its code goes into `placement.json`'s
applications, each layer's `applications.auto.tfvars` is rendered from it (2-env's list,
2-shr's pushers and pullers, 2-spn's database admins, 2-net's hostnames with the
wildcard for pull-request environments), and the apply sequence is printed: 2-env for
every environment, then for every environment but the last again (each grants the next
environment's deploy identity read on its records bucket, from state the first pass did
not have), then 2-shr and 2-spn, then the application's own stack per environment, then
2-net. OpenTofu reads `*.auto.tfvars` after `terraform.tfvars`, which keeps what a person
decides.

## The application's pipeline

The pipeline a render writes runs on two triggers per environment: a pull request's
`/gcbrun` comment (the first environment only) and a release tag (every environment, in
promotion order, the later ones by a person's approval where the placement says so). A
pull-request build stands its own environment up under `<app>-pr<N>` on the pull-request
hostname, with its own database (or the environment's, on `/gcbrun shared-db`), and tears
it down on `/gcbrun down` or by the hourly sweep once the pull request closes. The build
talks back on the pull request: a deployment the sidebar shows, a comment with the release
and the database mode, the guard's refusals.

The guards run before anything deploys: the migration guard (the sequence, never
modified, no gaps, and against the default branch for a pull request; a refusal names
`bedrock migration renumber` as the fix, and for a modified committed seed file a new
file after it with `/gcbrun reload-db`), and the plan guard (a pull-request stack may only
create and change what carries its own number).

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
`internal/protect` and `internal/migration` hold the operations, and `internal/cli` is the
command surface. Every rendered file carries a comment naming what it comes from; a
change to a template is a change to the goldens, and the render test reads every golden
file, comments included.
