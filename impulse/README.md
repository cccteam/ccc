# impulse

`impulse` is the command-line tool for Impulse applications: apps built on the cccteam
libraries with `ccc/resource` at the center.

The tool keeps no record of its own. Everything it needs it reads from files the
application already depends on: the generator program, `go.mod`, the browser apps'
`angular.json`, the process files, the test harnesses, and the config struct tags.

## Install

```sh
go install github.com/cccteam/ccc/impulse@latest
```

Inside an application, impulse is the `tool github.com/cccteam/ccc/impulse` directive in
`go.mod`, and `go tool impulse <command>` runs it at the version go.mod pins; the
installed binary is for creating applications and for development.

## Vocabulary

The words the tool uses on the command line, in check reports, in handoff briefs, and in
the code it lays in. They are frozen: one test holds the command surface (every command,
flag, and check name) to a golden file, and another keeps the retired words out of the
templates, this README, and the tool's source.

- **Application**: one Go module built on `ccc/resource`, with one database, one schema,
  and one deployment. Everything below is a part of one application.
- **Site**: a stand-alone server on a host of its own: a main package, handlers, a router,
  resources, and a browser application. Every application has at least one site.
- **Layout**: how the sites sit in the tree. **flat** has the one site at the root
  (`app/`, `pkg/`, `web/`); **sites** has every site under `apps/<site>/` with the shared
  packages at the root. An application starts flat and is promoted to sites when it grows
  a second site, and one left with a single site keeps the sites layout. "Multi-site"
  describes a deployment with several hosts, never a layout.
- **Outlet**: a second URL space on the same host, declared in the generator program. A
  session outlet is a browser surface bound to an auth; an API-key outlet is a machine
  surface. The default outlet is the one `GenerateRoutes` declares. A browser outlet
  takes one shape: its application at its own mount path (`WebApp("/portal")`) with its
  API under it (`/portal/api`), so the application's scope covers its own API, login,
  and callback routes. One application alone may sit at `/`; with two or more none does,
  since an installed application's scope is every URL under its start and one at `/`
  would own the origin, and the generated router answers the root alone with a redirect
  to the default outlet's application (`/console/`). The `outlets` skeleton carries the
  shape: the console at `/console` on `/console/api`, the portal at `/portal` on
  `/portal/api`.
- **Auth**: a population that signs in one way and holds roles in its own permission
  store. It is a package, `pkg/auth/<name>`, whose name prefixes its tables and cookies
  and whose role file (`roles.json`, embedded in the package) holds the
  release's default roles for it.
- **Flavor**: how an auth's people sign in: `password`, `oidc-azure`, `oidc-google`, or
  `preauth`.
- **Authority**: who owns an OIDC auth's role membership: `directory` (the directory's
  role claims are synchronized at every login) or `application` (roles are assigned in the
  application).
- **Domain** and **tenancy**: a domain is the scope a permission is granted in. Tenancy
  makes a table the domain universe: its resource struct is the tenant record (`@tenant`),
  from which the generator derives the tenant segment tenant-scoped resources live under
  and emits the constructor of the tenant roster, the tenants as the running application
  knows them, reloaded on the `tenants` signal so a tenant created on any instance is
  usable at once, without a restart; a role held in every domain reaches every tenant.
- **Levels**: the configuration chain in `pkg/config`, one level per kind of process, each
  embedding the one below it: **core** (every process), **data** (every process that opens
  the database), and **site** (one served site: its port and its built bundle). The site
  level is `SiteConfiguration` in both layouts, since a flat application is one site.
- **Live pages**: list pages and record pages that stay current without polling. A
  request the page asked to be live carries `X-Subscribe`; the server registers the
  subscription before the query runs and publishes each commit's rows into the
  subscribers' change sets, and the browser refetches with `_v`, an answer it caches for
  five minutes. The server side is the data level's Firestore settings
  (`APP_FIRESTORE_DATABASE` in a deployment, `FIRESTORE_EMULATOR_HOST` in development)
  and the App's `LiveService`; the generated router serves the live routes
  (`live/renew`, `live/unsubscribe`, `live/token`) on every session outlet. The live
  service is required: every application wires one, the data level refuses to start
  with neither a database nor the emulator configured, and the test harnesses wire
  `live.NewFake()`. The skeletons carry the server side, the Firestore emulator in the
  Procfile, and the rules and indexes the database needs under `schema/firestore`; the
  browser side is the client packages' live option, which a page opts into.
- **Change signal**: the one channel an application's instances tell each other on that
  something shared changed, carried by the live service: one document per application,
  `application/signals`, with a field per kind of change (`features`, `tenants`,
  `policy`; `resource.SignalKind`), written by `Signal(ctx, kind)` and followed by
  `Subscribe(kind, onSignal)`. A signal carries nothing but the fact of a change; every
  subscriber rereads what it keeps, and rereads at its own backstop regardless. A
  feature flag flip rides the `features` kind, and every permission engine rides the
  `policy` kind: each `access.New` takes `access.WithChangeSignal` over the live
  service, which an auth package builds from the `Settings.Signals` the data level
  passes (`access.ChangeSignalFunc(announcePolicy(signals), watchPolicy(signals))`), so
  a role, grant or membership written on one instance reaches every instance's snapshot
  at once rather than at the engine's next heartbeat. The `change-signal` check holds
  every engine to it.
- **Feature flag**: a release switch owned by the application: a feature's code ships
  before the feature is turned on, the switch differs per environment, and the flag is
  removed once the feature is permanent or abandoned. A flag is a `resource.Feature`
  constant in the resources package (`const Debriefs resource.Feature = "debriefs"`, the
  doc comment its description, declared by `impulse add feature`); `@feature(Debriefs)`
  gates a resource, a field or a method, and off means absent (404 on the routes, left
  out of the permission digest, the field unknown). Its value lives in the application's
  `FeatureFlags` table, one row per environment, written by the deploy's
  `MigrateFeatures` from the declarations and flipped through the generated `SetFeature`
  method, which the `FeatureAdministrator` role in the roles file holds; the development
  seed's row (`schema/devseed/..._dev_feature_flags.up.sql`) is the flag's state in
  development and the test environments. Per-user and per-tenant enablement are
  permissions, not flags.
- **Warning**: a schema finding the generator raises on every run and prints as a
  `Warning:` line (an index a listed tenant-scoped resource wants, a tenant resolved
  through a join path, an enumeration table too large to bake): a performance matter the
  application decides, never a refusal. The generate program's warnings test pins the
  accepted set as typed values, so a new warning fails the application's CI until it is
  fixed or pinned.
- **Audit finding**: an advisory finding about a shape the framework handles under a
  stated limitation (a table storing files whose rows the database deletes by cascade),
  printed as an `Audit:` line only when asked: the generate program's `-audit` flag, or
  `impulse audit`, which runs every program that way. A normal generation never prints
  one, and no test pins them.

## impulse new

`new` creates an application: the base skeleton (flat layout, one auth, nothing else on)
rendered under your module path, with the first auth named by you. The application is
named after the module path's last segment unless `--name` says otherwise: the name is the
web package (`<name>-web`), `APP_SERVICE_NAME`, and the development Spanner project,
instance, and database in `.envrc.template`.

```sh
impulse new ../beacon --module example.com/acme/beacon --auth staff
impulse new ../harbor --module example.com/acme/harbor            # asks for the auth name
impulse new ../harbor --module example.com/acme/harbor --auth members --dev-root ~/Development/github.com/cccteam
impulse new ../svc --module example.com/acme/beacon.service --auth staff --name beacon   # the last segment is not a name
```

An auth is a population that signs in one way and holds roles in its own permission
store, and it is a package, `pkg/auth/<name>`, whose name is also the prefix of its tables
(`<Name>Sessions`, `<Name>Roles`, ...), its cookies (`<name>` for the session, `<name>-xsrf`
for the XSRF token, so two auths on one host never overwrite each other's), and the stem of
the home of its role file (`pkg/auth/<name>/roles.json`, embedded and handed to the permission
engine as the release's default roles). The web app that binds to an auth names the same XSRF cookie in its
HttpClient configuration (`withXsrfConfiguration`), since the browser echoes that cookie
in the `X-XSRF-TOKEN` header. So the name is asked for when `--auth` is not given, and there is no default: a default word would
land in every application whose author skipped the question. A good name is the population
that signs in, plural, lowercase, one word: staff, members, partners, devices. It cannot be
a Go keyword, a package the application imports, or a directory it has.

`.envrc.template` carries the development ports (the server's, the Spanner emulator's,
and the Firestore emulator's, which the render report states) and, commented out, the
Firestore variables the live pages read, which a deployment supplies:
`APP_FIRESTORE_DATABASE`, `GOOGLE_CLOUD_FIRESTORE_PROJECT` (the database's project) and
`APP_FIREBASE_API_KEY`. A process given the database without its project refuses to start
and names both variables, since the database is not assumed to be in the Spanner project
(where environments share a Spanner instance, that project is the shared instance's). In
development the emulator stands in through `FIRESTORE_EMULATOR_HOST` and takes the Spanner
project; with neither a database nor the emulator set the process refuses to start, since
the live service is required.

The application's `go.mod` pins its impulse tool (the `tool github.com/cccteam/ccc/impulse`
directive and its require, which CI's `go tool impulse check` runs) at the impulse that
created it, so the templates the application starts from and the check its CI runs are
the same code:

- An impulse release pins its own version.
- An impulse installed from a commit (`go install github.com/cccteam/ccc/impulse@<commit>`)
  pins that commit's pseudo-version, the version Go gives a commit that has no release tag
  (`v0.0.0-20261004053859-89401d630235`).
- An impulse built from a checkout (`go build`, `go run`) names no commit the module proxy
  serves, so `new` refuses it before writing anything and says to install impulse from a
  release or a pushed commit.

`new` writes the pin into `go.mod` and resolves it with `go get -tool`, through the module
proxy: Go fetches that impulse, verifies it against its checksum database, raises any
requirement the application has that impulse needs higher, and records the checksums in
`go.sum`. With `--dev-root` the pin is the one the template carries, as before, since that
application builds against local checkouts.

The tree is committed as the application's first commit (`--skip-git` leaves it
uncommitted), so `impulse add` can start from a clean tree; with `--dev-root` the `go.work`
it writes is ignored by git. Options come afterwards, one reviewable change each:
`impulse add tenancy`, `impulse add outlet`, `impulse add auth`, `impulse add files`.

Options can be composed into the creation: `--tenancy` (with `--tenant-table`, default
`Tenants`), `--outlet <name>=<prefix>` (repeatable; `--api-outlet` for a machine surface),
`--files` (the file store, section add files) and `--site <name>` (two or more, the first
being what the base site becomes under `apps/`) run the same transitions `impulse add`
runs, on the fresh tree in order, tenancy first, the file store after the outlets and the
sites last, and end in one check and one handoff brief carrying every obligation, so the agent wires the whole shape in one sitting (`--agent` launches it). The
first commit is the base alone, so the composed options are one reviewable diff on top of
it; the brief's reference is the `sites` skeleton when sites are composed, `outlets` when
an outlet or a directory flavor is, else `tenanted`.

The first auth signs in with a password unless `--oidc-azure` or `--oidc-google` says its
people sign in through the organization's directory, with `--authority` (directory or
application, asked when not given) saying who owns role membership; the flavor is composed
first and the auth is born in the directory's shape, its session migrations written in that
shape rather than moved to it later.

```sh
impulse new ./harbor --module example.com/harbor --auth staff --tenancy --outlet portal=portal/api --agent
impulse new ./fleet --module example.com/fleet --auth crew --site console --site portal
```

The application ships its CI, and impulse owns it. Two workflows are rendered from the
code: `.github/workflows/ci.yml`, the checks on every pull request, and
`.github/workflows/ci-cache.yml`, the run after a push to the default branch (`main` or
`master`) or a `hotfix/` branch that fills the caches the checks restore. `impulse new`
writes them at creation, `impulse render` (with no arguments, inside the application)
rewrites them, `impulse add site` and `impulse remove site` rewrite them as part of their
change since the browser workspaces change, and `impulse check` (`ci-workflow`) compares
the committed files with the same rendering, so a hand edit fails the check. The pull
request workflow is plain jobs. No job calls a reusable workflow of another repository,
and each job's id is the check name the pull request reports, so a repository rule can
require them by name: six are fixed (`title`, `go`, `web`, `image`, `secrets`,
`migrations`; bedrock reads the list from impulse's `ci` package and its organization
layer requires them), and the browser jobs between `go` and `web` are named per workspace
and gated by `web`. The pull request is the one gate for every change, whoever opens it,
so the gate covers every job:

- `title`: the pull request's title is a conventional commit line, the one the squash
  merge carries and release-please reads, and its type decides what the merge does. A
  merge titled `feat` or `feature` releases a minor version; one titled `fix`, `perf`,
  `revert`, `docs`, `deps`, `upgrade`, `infra` or `config` releases a patch; a merge of
  only the hidden types (`style`, `chore`, `refactor`, `cleanup`, `test`, `build`, `ci`)
  opens no release pull request and reaches no environment until a releasing merge
  follows. The seeded `release-please-config.json` carries a changelog section per type
  (bedrock's `check` refuses one without), since release-please drops a merge whose type
  has no section exactly as it drops a hidden one; an application seeded before `upgrade`,
  `infra`, `config` and `cleanup` had sections adds the four by hand.
- `go-build`, `go-test`, `go-test-skipauth`, `go-lint`, `go-lint-skipauth`, `go-vuln`,
  `go-semgrep` and `go-check`, the Go legs, run at once and each reports on its own: the
  module builds and vets without tags and with `skipAuth` (the tag that simulates the
  directory an OIDC auth signs in through); its tests run under the race detector, one leg
  per tag; golangci-lint runs at the version the skeleton's `.golangci.yml` is written
  for, one leg per tag; govulncheck; Semgrep over the findings the pull request introduces
  (its baseline is the pull request's base branch, so a hotfix-line pull request diffs
  against its own base); and `go tool impulse check` with its regeneration (the generators
  start the Spanner emulator in a container, which the runner has), failing on a tree the
  checks changed. The pins fix the engines, not what they know: govulncheck reads
  vuln.go.dev, Grype (in `image`) downloads its vulnerability database and Semgrep fetches
  its registry rules when the job runs, so a workflow at an old pin still finds a
  vulnerability published after it; TruffleHog's detectors, golangci-lint's linters and
  the actions move with impulse releases.
- `go`: the gate over the Go legs, one fixed name a repository rule can require. It needs
  every leg, runs whether they passed or not, and fails when any of them did not succeed.
- The larger runner: the jobs that take the most machine run on the runner the GitHub
  Actions variable `CI_LARGE_RUNNER` names, a runner label or a runner group set on the
  repository or the organization (an organization sets it once for every application;
  bedrock's organization placement declares it, `ciLargeRunner`). While the variable is
  unset they run on GitHub's standard runner, as every other job does. Which jobs: the
  two test legs and `image` unless the application's `//impulse:ci` line says otherwise
  (below).
- `angular-<workspace>`, one per browser workspace (`angular-web` for the flat workspace,
  `angular-<site>` for a site's at `apps/<site>/web`): Bun at the version that wrote
  `bun.lock` installs from the lockfile exactly (`bun ci`), then the package scripts build,
  lint and test.
- `web`: the gate over the browser jobs, one fixed name a repository rule can require
  where the workspace jobs' names vary per application. It needs every `angular-<workspace>`
  job, runs whether they passed or not, and fails when any of them did not succeed, so a
  failed browser build, lint or test blocks the merge; an application without a browser
  workspace gets a `web` that needs nothing and passes with nothing to check.
- `image`: once the application has a Dockerfile (bedrock seeds it), hadolint over it, the
  build, and Grype over the built image, failing on a high or critical vulnerability.
  Without a Dockerfile the job passes with nothing to build, so the check exists on every
  pull request.
- `secrets`: TruffleHog over the whole history reachable from the pull request's head; a
  secret confirmed live, or one whose check could not finish, fails.
- `migrations`: against the base branch, `schema/migrations` gains files only; a committed
  migration is never modified or deleted.

The caches. A pull request's `go-build`, `go-test` and `go-test-skipauth` restore the Go
module cache and build cache from the nearest entry saved under their own job's key (the
same `go.sum` first, then any), and `go-vuln` and `go-check` restore `go-build`'s, whose
compile they share. Neither cache can serve a stale result: a module is verified against
`go.sum` when read, and a build output is addressed by the hash of its inputs (the
toolchain, the flags, the sources, the dependencies' outputs), so an entry from an older
commit is either exactly what this commit computes or unused. The entries come from
`ci-cache.yml`: after each push to the default branch or a hotfix branch it builds and
tests what those jobs build and test, at the branch's head (the tests run rather than
compile, since what a test builds as it runs is in the cache only when it ran), and
saves under their keys with the commit's, so a pull request's first run starts from the
branch it targets and compiles what it changed and nothing else. A pull request run
saves an entry of its own only when it found none for its `go.sum` (a dependency
change), so the caches grow with merges, not with pushes; GitHub keeps the newest ten
gigabytes and drops an entry unused for a week. Nothing else is cached: the image build
compiles fresh by design (its download stages are a minute of the job, and exporting
them to the Actions cache cost more than it saved), govulncheck, Grype and Semgrep fetch
their databases and rules when the job runs, the emulator images are pulled, and the lint
legs keep setup-go's cache and golangci-lint's own.

Go's cached test results are the one reuse whose inputs Go cannot see in full, so the test
legs run every test (`go test -count=1`) unless the application turns the reuse on. Go
reuses a passed test's result when the test binary, the files it read and the environment
variables it read are unchanged (a failure is never cached); it cannot see what a running
emulator's image holds. An application whose tests' inputs are all in the tree, in the
environment or pinned by digest turns it on with the `//impulse:ci` line, and then the
test legs restore every tracked file's modification time from git before the run (the
checkout gives every file the time of the checkout, which would miss every result that
read a file) and `ci-cache.yml`'s test runs on the branch save their results, so a pull
request reuses the results of the packages it did not touch.

The `//impulse:ci` line is a comment line in any non-test Go file of the application, at
most one in the tree:

```go
// The CI choices: the test legs and the image build on the larger runner, the test results reused.
//impulse:ci large-runner=go-test,go-test-skipauth,image test-cache=on
package main
```

`large-runner` lists the jobs on the runner `CI_LARGE_RUNNER` names, by job id, any job
the workflow renders but the gates (`none` puts every job on the standard runner);
`test-cache` is `on` or `off`. A setting the line leaves out keeps the default (the two
test legs and `image` on the larger runner; the test results not reused), and a line
with a setting it does not know, a job the workflow does not render, or a second line in
the tree fails `impulse check` and `impulse render`. The rendered workflows say in their
header what the line chose.

Every action is pinned by commit with its tag in a comment, and every tool version is a
constant in impulse, so the pins travel with impulse releases: a bump is an impulse
release, never an edit to the file, and dependabot must not run over the workflow files
impulse or bedrock own. The impulse an application runs is the `tool
github.com/cccteam/ccc/impulse` directive in `go.mod`, which `impulse check` (`pins`)
holds: CI runs `go tool impulse check` at that pin, verified by Go's checksum database, and
no version is written into the workflow. Moving the pin is three commands: `go get -tool
github.com/cccteam/ccc/impulse@<version>`, then `go tool impulse render` (the owned files
follow the new impulse), then `go tool impulse check`. Run the check locally before
pushing; the pull request is the one gate. CodeQL and the rest of GitHub's Code Security
are optional and not rendered.

A directory-flavored first auth under the `directory` authority with Google is born with
lowercase role names: the directory's groups assign roles by name and
`session.GoogleRoleSync` lowercases every derived name, so the base's `Administrator_Global`
becomes `administrator_global` in the role file, the bootstrap identities, the environment
template and the tests, and every new role is named in lowercase; `auths-wired` refuses one
that is not.

## impulse check

`check` verifies the agreements between an application's parts and exits non-zero when
any of them is broken. Run it from the application root, or point it at one:

```sh
impulse check
impulse check --app path/to/app --skip-generate
impulse check --only prettier-ignore,env-template --fix
impulse check --list
```

| Check | Verifies |
| --- | --- |
| `generator-program` | Every generator program uses options this release knows, with literal arguments. A program the tool cannot read completely is one it cannot later edit or migrate. An option the framework has since removed (`WithDomainRoute`) is named with where its declaration went. A program that never reads `Warnings()` after it generates (in its own package, or in the main package that runs a declaring package) warns: the schema warnings a generation raises go unseen. The skeletons' runner prints them and pins the accepted set in a test. |
| `options` | The generator programs declare one coherent option set, and the report states it: layout (flat or sites), sites, tenancy (the resource package's `@tenant` record, `WithConcealedDomains`), outlets, and targets. Handlers come with routes, `ForOutlet` names a declared session-serving outlet, the referenced directories exist, a `//go:generate` directive runs every program (its own directory, or a main package of the module that imports the package declaring it, the layout an application takes when its tests run the declaration in-process), the sites agree on tenancy, and a second site lives under `apps/<site>/`. |
| `tenancy-wired` | A site whose resource package declares a tenant record (`@tenant`) has at least one struct annotated `@permissionScope(domain)` and a tenant roster built outside tests with the record's generated constructor (`New<Record>Roster`), handed the live service's tenants signal (`resource.WithTenantSignals`) and started (`Start`) in the package that binds it; a site declaring no record has no tenant-scoped structs. The generator refuses the rest (a tenant-scoped resource with no record, a record that is not a global, table-backed resource with one string key, a second record), the compiler holds the App to the generated contract's `TenantRoster()`, and the permission engine reaches every tenant with the roles held in every domain, so roles need no provisioning per tenant. |
| `outlet-wired` | Every outlet a program declares (the default from `GenerateRoutes` and each `WithRouterOutlet`) has its generated routes mounted: by the generated router (`GenerateRouter`) from the declaration itself, or, in an application that kept a hand-written router, by a hand-written file in the router package calling `generated<Outlet>Routes`. A session-serving outlet has a `GenerateTypescript` target naming it, and that browser project's development proxy forwards the outlet's prefix. An outlet with no `@outlet` members yet is noted, not failed. Each session outlet's line says `live` when the site's App wires the live service (its `LiveService` returns more than nil), since the generated router mounts the live routes on every session outlet; an App whose `LiveService` returns nil fails: every session outlet serves the live routes, and the live service is required in every application. |
| `outlet-shape` | Every session outlet serving a browser application (`WebApp`) has its API prefix under the application's mount path: `<mount>/api`, `api` for an application at `/`. That is the one shape the skeletons carry, so each application's scope covers its own API, login, and callback routes; an outlet shaped otherwise works and the generator accepts it, so the check warns, naming the outlet, its prefix, and its mount path. The mount rule itself is the generator's: a browser application at `/` beside another is refused at generation, since an installed application's scope is every URL under its start. |
| `sites-wired` | In the sites layout, every site has a main package under `apps/<site>/`, a process in the Procfile or process-compose file running it on its own `PORT`, and every site's router is imported by some package passing a collection to `access.WithDefaultRoles` (the data level, or the callers of an auth package that makes the call itself), so the collection the permission engine validates the roles against (the union, or one per auth) knows the site's resources. |
| `maintenance-switch` | Every site's main checks `maintenance.Requested()` before it builds the site configuration (`config.NewSiteConfiguration`), and serves `maintenance.Serve(ctx)` when it is set: the deploy pipeline starts a maintenance revision of the application's own image with `APP_MAINTENANCE` set before a release that replaces or interrupts the database, and that revision must open no database, session store or secret. A main that builds the configuration first, or checks the switch after it, fails; a main that builds no site configuration is a warning, since the check cannot tell where the switch belongs. |
| `session-tables` | Every session authenticator constructed outside tests (`session.NewPasswordAuth`, `NewOIDCAzure`, `NewOIDCGoogle`, `NewPreauth`) reads tables a migration creates: its sessions table, its users table, and the impersonation table when the storage attaches one. Two flavors never share a sessions table. The report lists the auths, one per distinct flavor and table set, each named by its package when it lives in one (`pkg/auth/<name>`), with its session and XSRF cookies when named, and an OIDC auth's role-membership authority (directory for `RoleSync`, application for `DisableRoleSync`). |
| `auths-wired` | Every auth package (`pkg/auth/<name>`, constructing a session authenticator) is constructed by the data level (`<name>.New` called outside tests), carries its role file (`pkg/auth/<name>/roles.json`, embedded by a `//go:embed roles.json` directive, exported as `<name>.Roles()`, and handed to the permission engine by an `access.WithDefaultRoles` call outside tests, in the package or on the data level; the file exists), and bound by a surface: an outlet declaring `Auth("<module>/pkg/auth/<name>", <flavor>)` in the flavor the package constructs (the generated router mounts that flavor's login routes, so a disagreement is a finding), or a package outside `config` and `cmd/` taking `*<name>.Auth`. An outlet bound to a package that is no auth package is a finding. No two auth packages issue the same cookie, session or XSRF, a name left unset being the session library's default (`auth`, `XSRF-TOKEN`); the browser keeps one cookie of a name per host, so a login to one auth would overwrite the other's. An auth that hands role membership to its directory (`session.RoleSync`) has no role writer in the application reaching its store, since the directory removes those roles at the next login. A role file handed to the engine that no test validates warns: a `_test.go` calling `access.ValidateRoles` while parsing the auth's `Roles()` is where the warnings the deploy prints are pinned as typed values, so without it a warning is accepted nowhere in code. Authenticators outside auth packages warn. |
| `change-signal` | Every permission engine constructed outside tests (`access.New`) is handed a change signal: an `access.WithChangeSignal` option among the call's arguments, the skeletons' being `access.ChangeSignalFunc(announcePolicy(signals), watchPolicy(signals))` over the live service's `policy` kind. Without it a role, grant or membership written on one instance reaches the others at the engine's next heartbeat alone. A package constructing an engine without the option fails on its own line, naming each construction by file and line; a call forwarding its options (`opts...`) fails too, since the signal cannot be read there. Skipped when the application constructs no engine. |
| `conditions-proven` | Every conditional grant in a role file the release hands to the permission engine (`pkg/auth/<auth>/roles.json`, the `<auth>.Roles()` an `access.WithDefaultRoles` call takes) is named by a test case calling the harness helper `provesGrant(t, <auth>.Roles(), role, permission, resource, condition)` with literal coordinates. The generated authorization matrix runs a fake engine with unconditional grants and the deploy-time validation reads the grammar, so whether a condition does what its author meant on real rows is proven only by a case over seeded rows: a row the condition admits answers, a row it refuses is refused. The check reads the call; the helper, in `test/integration/harness_test.go`, parses the role file when the test runs and fails when the grant is gone or its condition reads differently, so the case and the file hold each other from both sides. A grant no case names fails the check on its own line; a call naming a grant the file does not carry, or one the check cannot read, is a finding too; a file with no conditional grant reads "0 conditional grants". Unconditional grants need no case: the generic test `test/integration/grants_test.go` parses the auths' role files and proves the engine serves every one of them. When the check fails under `impulse handoff`, the brief carries the pattern a case follows. |
| `skipauth` | When an auth signs in through a directory (the OIDC flavors), the simulated directory stays in development and tests: no application code reads `APP_USERNAME` or `APP_ROLES` (only the session library's `skipAuth` build does), and no build description (Dockerfile, cloudbuild, Makefile) carries the tag, which would let a deployed build accept any name as a login. |
| `emulator-version` | The generator option, the process files' image tags, and the test harnesses name one Spanner emulator version; the process files and test harnesses that start the Firestore emulator (the Cloud SDK emulators image, `google-cloud-cli:<version>-emulators`, or db-initiator's `NewFirestoreContainer`, which takes the same version) name one version of it. The summary states both; an application whose process files and test harnesses name no Firestore emulator fails, since development cannot start without it: the data level refuses to start with neither `APP_FIRESTORE_DATABASE` nor `FIRESTORE_EMULATOR_HOST` set. |
| `prettier-ignore` | Each browser app's `.prettierignore` excludes the generated TypeScript. Prettier reflowing generated files breaks generate idempotence. `--fix` adds the entry. |
| `eslint-ignore` | Each browser app receiving generated TypeScript ignores it in its eslint flat config (`ignores: ['**/zz_gen_*.ts']`) or `.eslintignore`. Generated shapes trip stylistic rules, and the output is not the developer's to change. |
| `resource-styles` | Each browser app that depends on `@cccteam/resource-angular` imports the library's stylesheet (`@use '@cccteam/resource-angular/styles';`) from a global stylesheet its `angular.json` build names. The form and list components request its classes by name; without it fields lose their grid and section labels pile up in a corner. `--fix` prepends the import. |
| `package-manager` | Every browser app carries the same kind of lockfile (bun, npm, yarn, or pnpm), and the process files and package scripts invoke that tool and no other. Two tools in one repository means two lockfiles drifting apart. |
| `registry-pins` | Every browser app installs its packages from the registry: a committed `file:.yalc/<package>` spec (or a lockfile recording one) is a local yalc attachment that a clean checkout cannot install, so the pipeline's install fails. `ccclib.sh restore` puts the registry pins back. |
| `test-runner` | Every browser application project runs its component specs on Angular's unit-test builder, the runner `ng new` scaffolds (`@angular/build:unit-test`: Vitest under jsdom in Node, no browser): a `test` target on that builder, the spec tsconfig it reads (named in the target, or `tsconfig.spec.json` in the project root), and a package script running `ng test <project>`, so `bun run test` runs every project's specs once. A project with no `*.spec.ts` under its source root warns: the runner is wired and nothing runs on it yet. |
| `installable` | Every browser application bound to a session outlet installs as a progressive web app, and the server serves it through the resource package's served browser app. On the browser side: `@angular/service-worker` is a dependency at the workspace's Angular line (the line of `@angular/core`); the project's production configuration names a worker config (`"serviceWorker": "ngsw-config.json"`) that exists, whose `navigationUrls` exclude the outlet's API under the mount (`!/api/**`, relative to the mount, so the login, callback and stored-file navigations reach the server; an API prefix outside the mount is outside the worker's scope and needs none); the app config provides the worker (`provideServiceWorker`) and the library's update provider (`provideAppUpdate`); `index.html` links the web app manifest, which parses with `id` the mount path with a trailing slash and `scope` and `start_url` `./`; and every icon the manifest declares is there with PNG dimensions matching its `sizes`, read from the file's header. The release reaches the browser: the project's build defines `APP_VERSION` (`"define": { "APP_VERSION": "'dev'" }` in its build options) and the workspace's `build` script redefines it from the `VERSION` environment variable (`ng build console --define \"APP_VERSION='${VERSION:-dev}'\"`, which the image's browser stage sets), so a release build stamps its release and any other `dev`; the app config provides it as `API_VERSION` (`{ provide: API_VERSION, useValue: APP_VERSION }`) and registers `apiVersionInterceptor` through `provideHttpClient(withInterceptors([...]))`, so every request carries the release in `X-Api-Version` and the server can refuse a build it no longer answers. On the server side: the application's hand-written handlers build the asset handlers from `resource.NewBrowserApp(dir, "<mount>")`, `github.com/jtwatson/spaassets` is imported nowhere and gone from `go.mod`. A project with none of the browser side warns as not installable; a project with part of it fails, naming the first missing piece by file. |
| `ci-workflow` | The committed `.github/workflows/ci.yml` and `.github/workflows/ci-cache.yml` equal what impulse renders from the code: one browser job per workspace with the `web` gate over them, the jobs the `//impulse:ci` line puts on the larger runner and whether the test results are reused, and the action and tool pins this impulse carries. A missing `ci.yml` fails: the pull requests run no checks at all; a missing `ci-cache.yml` fails: the checks start cold on every pull request. A differing file fails naming the first differing line, what the code renders and what the file has; the fix is `impulse render`, since the file is impulse's: change the code or impulse, not the file. A browser workspace without its job is a difference like any other. |
| `paging` | No application code positions a list by offset: the generated query builders have no `Offset`, the server refuses the `offset` parameter, and pages are positioned by the cursor the `Link` header carries. Go code calling `.Offset(` or `SetOffset(` and browser code sending an `offset` query parameter are reported, so a hand-written caller is found before the upgrade breaks it; tests and specs are not read, since a spec describes the server's answer (whose page state carries an `offset` field) as often as a request. |
| `rpc-execute` | Every `@rpc` struct declares `Execute` in one of the three forms the generator classifies by signature (`resource.ReadWriteTransaction` second for the transaction form, `resource.Client` for the client form, `resource.ReadWriteTransaction` second and `resource.Files` third for the upload form; `error` the only or last result), and every generated RPC handler calls it. A handler an older generator could not type-check decodes and returns without running the method. A `TxnRunner` or `DBRunner` interface left in the RPC package warns: the generator reads the signature and no longer consults it, so delete it. |
| `feature-flags` | The `FeatureFlags` and `FeatureFlagChanges` tables the generated feature flag routes and the deploy's `MigrateFeatures` read are created by a migration as the resource module the application pins declares them: the check reads `resource.FeatureFlagsDDL(resource.SpannerDBType)` from that module's source (found through `go list -m`, so the comparison is against the library the application builds with, a replace or a workspace included) and compares each statement with the migration's, whitespace aside; a table that differs is brought to the library's statement by a new migration. Every declared flag (`resource.Feature` constant) gates something (`@feature(<Constant>)` on a resource, a field or a method) or is read somewhere outside tests (`a.FeatureSet().Enabled(resources.<Constant>)` in Go, `Feature.<Constant>` in a browser application), or it is a switch wired to nothing and fails by name and position; a flag declared twice and an annotation naming no declared constant fail too. Skipped when the generator emits no feature flags (no `zz_gen_features.go` in a resources package) and none is declared. |
| `sites-generators` | In the sites layout, every generator reads the one schema and the shared generator's TypeScript reaches every site's browser app. |
| `env-template` | Every `env` struct tag without a default appears in the development environment template (`.envrc.template`, `.env.template`, or `.env.example`). `--fix` adds the missing lines. |
| `file-store` | Every file store variable the code declares (`APP_FILE_STORE`, `APP_FILE_STORE_<NAME>`) names, in the development environment template, a store the framework opens (`file://<dir>`, `gs://<bucket>` or `mem://`); a directory store's directory is in `.gitignore` (`--fix` adds it); and a resource or method recording files (`@file`, `@upload`) has a store wired, the failure naming `impulse add files`. |
| `pins` | Framework pins in `go.mod` are released versions; pseudo-versions and local replaces warn but do not fail. `go.mod` carries the `tool github.com/cccteam/ccc/impulse` directive and a require of impulse, or the check fails with the commands that add it (`go get -tool github.com/cccteam/ccc/impulse@<version>`, `go tool impulse render`, `go tool impulse check`). When the running impulse was built from a module version (`go tool impulse`, `go install github.com/cccteam/ccc/impulse@<version>`) and that version is not the pin, the check fails with the same three commands to move the pin; an impulse built from a checkout is a development build, noted and not compared. |
| `gowork-off` | `GOWORK=off go build ./...` and `go vet ./...` succeed, so the pins in `go.mod` resolve without the workspace. |
| `regen` | `go generate ./...` reproduces the generated files on disk (content compared before and after, so it holds in untracked trees too). The `Warning:` lines the generate programs printed are listed under the result and counted in its summary; they never fail the check, since the program's warnings test is what gates the accepted set. Needs the Spanner emulator and rewrites the working tree; `--skip-generate` leaves it out. |

Statuses: `PASS`, `FAIL`, `WARN` (reported, does not fail the run), `SKIP` (with the
reason).

## impulse audit

`audit` runs every generator program of the application with `-audit`, from the module
root, and prints what each raised: the schema warnings a generation always prints, and
the audit pass's findings, advisory findings about shapes the framework handles under a
stated limitation, which a normal generation never prints. Today's one finding names a
resource that stores files on a table whose rows the database deletes by cascade
(`ON DELETE CASCADE` on an interleave or a foreign key): those rows never pass through
the patch machinery, so the release of their objects never runs for them and the
application's sweep removes the objects later. The section is one heading per program,
`cmd/generate/resourcegenerator/generator.go (go run ./cmd/generate/resourcegenerator -audit)`,
with the lines beneath it or `no findings`. A section, `feature flags`, lists every
declared flag with its description and where it is used: the resources, fields and
methods its `@feature` gates and the Go and browser code that reads it (tests and specs
marked), or that it gates nothing and is read nowhere, which the `feature-flags` check
fails on; an application declaring none reads `none declared`. A last section,
`permission engines`, lists every engine the application constructs (`access.New`
outside tests) by package, file and line, and whether it is handed a change signal,
which the `change-signal` check fails without; an application constructing none reads
`none constructed`.

```sh
impulse audit
impulse audit --app path/to/app
```

Findings never fail the command: it exits 0 with or without them. A program that fails
exits 1 with its output tail, and a program that does not take `-audit` exits 1 saying to
adopt the runner shape: the skeletons' generate program is three files in one directory,
`generator.go` declaring the generator (`newGenerator`), `main.go` running it and
printing the `Warning:` lines and, under `-audit`, the `Audit:` lines, and
`warnings_test.go` pinning the accepted warnings. It regenerates the tree like
`go generate` does, so it needs the Spanner emulator. It is a command of its own and not
a check: the check is the gate on every change, and the audit is read by decision, before
a release, after a schema change to a table that stores files, or when a stated
limitation is in question.

## impulse advise

`advise` is the one command that asks an agent for judgment, and it runs only when asked:
the check gates every change and the audit is read by decision, and neither becomes slow
or expensive on an ordinary run. It runs every generator program with `-audit`, as
`impulse audit` does, and writes a brief for an agent to `.impulse-advice.md` at the
application root: the option set in force; the `Warning:` and `Audit:` lines each program
raised, with the warnings test that pins the accepted set beside it; the deploy test that
pins the accepted role warnings (`pkg/deploy/deploy_test.go` in the skeletons), whose
values the agent reads; impulse's own reading of each kind, fixed and deterministic (for a
concealing key, whether the field is the default order, so every page pays, or a sort or
filter key, so only a caller who asks pays, and why the CASE stands; for an index warning,
the `CREATE INDEX` it wants; for a join path, that every list scans the table; for an
enumeration, its size in the metadata; for a cascade, that the sweep removes the objects;
for a grant warning, the disclosure a Forbidden answer makes); and the question each kind
leaves to judgment (whether the field's rank is sensitive and the table will page at
volume, whether the list pages at volume, whether the table will grow and the tenant
column is worth adding while it is cheap, whether the table is really an enumeration,
whether lingering objects are acceptable, whether the disclosure is intended). impulse
reads no test file itself; the agent does.

```sh
impulse advise                      # write the brief and print the command to run the agent
impulse advise --agent              # ask Claude Code, print its answer, remove the brief
impulse advise --skip-generate      # no emulator: the brief names each program's warnings test instead
```

With `--agent` the tool launches Claude Code non-interactively with the brief on standard
input and the read-only tools alone (`Read`, `Glob`, `Grep`), prints the answer, and
removes the brief; `--agent-command` and `--agent-arg` are `impulse handoff`'s. Without
`--agent` the brief stays and the command to run the agent is printed. The brief's rules
are an answer and no edit: per warning and finding, in prose, which branch of the reading
holds for this application, the answer to the question from what the code tells, and the
edit recommended (`masking:"positional"`, the index migration, the tenant column, the
runtime resource, the Read grant, or the accepted pin and where it goes), with the
questions the code cannot answer stated for the developer; nothing is run that writes.
There is no verify step, since nothing was to be changed. A program that fails, or does
not take `-audit`, is an error with `impulse audit`'s message: the brief would miss its
input.

## impulse render

`render` has two forms, and the arguments decide which.

With no arguments, inside an application, `render` writes the files impulse owns from the
application's code: today the CI workflows, `.github/workflows/ci.yml` with one browser
job per workspace, the `//impulse:ci` line's choices and the pins this impulse carries,
and `.github/workflows/ci-cache.yml` beside it. It says for each file whether it was
written or already read as the code renders. `impulse check` compares the committed file
with the same rendering, so this is the command that brings the file back into agreement
after a change to the code, and the second step of moving the impulse pin.

```sh
go tool impulse render          # inside the application, at the pin go.mod names
impulse render                  # the same, with the installed impulse
```

With a candidate and a directory, `render` copies one embedded skeleton into a new or
empty directory under the module path `--module` names, rewriting every import and
`go.mod` to it. It is the primitive `new` builds on and the way the templates are
validated: render one, then build, test, and `impulse check` the result. The templates
carry `staff` as their placeholder auth; `new` renames it, `render` keeps it. Both name the
application after the module path's last segment unless `--name` says otherwise.

```sh
impulse render solo ../beacon --module example.com/acme/beacon
impulse render sites ../harbor --module example.com/acme/harbor --dev-root ~/Development/github.com/cccteam
```

`--dev-root` names a directory of cccteam checkouts laid out by repository
(`<root>/ccc/resource`, `<root>/session`, ...). The rendering then writes a `go.work`
using every framework module the application requires directly that has a checkout there,
so it builds against local framework work instead of the pins; indirect requirements
stay pinned, since a checkout of a library's own dependency can lag what the library needs. That `go.work` is for
development only; never commit it.

## impulse upgrade

`upgrade` moves an application forward through the impulse releases the ledger records,
one release at a time, and commits each. An impulse release is a coherent pin set (the
resource, access, session and accesstypes versions its skeleton's `go.mod` named) together
with the recipes an application at the release before it needs, or a note that none is
needed. Where the application stands is read, never recorded: the framework pins in its
`go.mod` say which release it builds against (the latest release whose pins they reach), and
every release after that up to the running impulse's is pending.

```sh
go get -tool github.com/cccteam/ccc/impulse@<version>   # the impulse to upgrade to
go tool impulse upgrade --dry-run                        # the releases and recipes the walk would apply
go tool impulse upgrade                                  # walk, one commit per release
go tool impulse upgrade --to v0.3.0                      # stop at a release short of the running impulse's
```

Each release is one step. Its recipes run first: a recipe detects the old form in the
application (the generator program, the annotations, the known seams) and edits only where
it finds it, so running it twice is safe, and so is running it on an application whose pins
were bumped by hand ahead of its code. Then the pins move to the release's set and the
impulse tool pin to the release (`go get`, then `go mod tidy`), the owned files are
rendered again from the code (what `impulse render` writes), `go generate ./...` runs, and
`impulse check` runs. A clean check is committed as `upgrade: upgrade to impulse <version>`
with the release's note and recipes in the body (the `upgrade` type releases a patch, as
the `title` check's list says, since an upgrade moves the pins and re-renders the owned
files and must not wait for a later releasing merge). A failing check stops the walk with the
step's changes staged and the handoff brief written (`impulse handoff`, below): fix the
obligations or hand them to the agent, commit, and run `upgrade` again; it resumes from
whatever `go.mod` says, since the pin is the checkpoint and nothing else records progress.
No release is skipped: a recipe is written against the shape the release before it left
behind. A release with no recipe is a pin bump and a commit.

The ledger (`internal/ledger`) records releases from the first published impulse beta on;
until that beta is cut it is empty and `upgrade` has nothing to walk. From then on, a
breaking change in resource, access, session or accesstypes is not done until the impulse
release that carries it records its recipe in the ledger, or says that no code change is
needed; the ledger's test holds every entry to its shape (a version newer than the one
before it, a pin set of framework modules, a note, no recipe named twice). Applications
that predate the first beta are adopted once by hand, not upgraded.

## impulse handoff

`handoff` is the protocol between the tool and an agent for the work the tool cannot do
mechanically. It runs `impulse check` on a clean working tree in a git repository and,
when checks fail, writes a brief for an agent to `.impulse-handoff.md` at the application
root: the failing checks verbatim as the obligations, the option set in force (read from
the generator programs), a reference application when one is given, and the rules. The
brief is Markdown any agent can read; the tool never reads it back. A failing check that
carries a meaning (`conditions-proven`: how a conditional grant is proven over seeded rows)
adds it to the brief under "What it means".

```sh
impulse handoff                      # write the brief and print the command to run the agent
impulse handoff --agent              # launch Claude Code on the brief, then verify
impulse handoff --verify             # after an agent run by hand: check + guardrails
impulse handoff --reference ../tenanted --skip-generate
```

With `--agent` the tool launches Claude Code non-interactively (`claude -p`) with the
brief on standard input and a tool set restricted to reading, editing, and the build
commands; `--agent-command` names another executable, and each `--agent-arg` appends an
argument to its command line (a model, a turn limit, a budget). When the agent returns, or on
`--verify`, the tool re-runs the check and adds a `guardrails` result: the generator
programs' option set and the lint configuration (`.golangci.*` at the root, the eslint
configuration of each browser app) must read the same as the git index holds them, and
with `--agent` the commit and the staged paths must not have moved. A weakened check is a
failure, not a pass. A clean verification removes the brief; the pull request is the
review, and there is no gate before it.

The brief's rules are the ones the verification enforces: run the check until it is
clean, do not edit generated files, the generator programs, or the lint configuration,
do not stage or commit, keep the tests table-driven, stop when the check is clean.

## impulse add

`add` makes the deterministic half of an option, stages it, runs the check, and hands
the failing checks to an agent exactly as `impulse handoff` does, with two additions to
the brief: what the tool changed (and what it could not), and what the option means in
this framework, alongside a rendered reference application with the option wired.

```sh
impulse add outlet portal --prefix portal/api --auth staff --agent
impulse add outlet kiosk --prefix kiosk/api --auth members
impulse add outlet machines --prefix machines --api-key
```

`add outlet` adds a router outlet to a flat application. A session outlet binds to an
auth, and `--auth <name>` says which: the generator program gains `WithRouterOutlet(name,
prefix, Auth(<pkg/auth/name>, <its flavor>), WebApp("/<name>"))` after `GenerateRoutes`
and a `GenerateTypescript` target for the outlet copied from the default target's, and the
console's browser project is copied to `web/<name>` with its API prefix, base path, and
compiler output rewritten (and its XSRF cookie, when the auth is not the console's) and
registered in `angular.json` (serving under `/<name>` on the next port), the package
scripts, and the Procfile. A console that was alone at `/` moves when the second browser
application arrives, since the generator refuses an application at `/` beside another (an
installed application's scope is every URL under its start): it goes to `/console`, the
name of its Angular project, with its API at `/console/api`, in the generator program
(`GenerateRoutes("pkg/router", "console/api", ..., WebApp("/console"))`), the project's
`baseHref` and `servePath`, its proxy, its environments, and its base element, and the
regenerated router answers the root alone with a redirect to `/console/`. The App's
`DeepLink` and `Assets` pair, a `LoginURL` naming the console's login page, the
hand-written routes and tests that name `/api`, and the README and Procfile lines follow
by hand, and the brief lists each by file and line. For an API-key outlet the program
gains `WithRouterOutlet(name, prefix, APIKey())`, and the console stays where it is. (An
application that kept a hand-written router gains `ServesSessions()` or nothing, as
before.) `go generate` then emits the outlet's routes, handlers, and client, and the
generated router mounts the outlet from its declaration: its group, its login routes, its
not-found handler, and its browser application. The App's handlers the generated
`Handlers` now requires (the outlet's session getter and deep-link and assets pair, or its
`<Outlet>Auth` middleware), the configuration, the members (`@outlet`), and the tests are
the agent's, and the compiler, `outlet-wired`, and `outlet-shape` hold it to them.

`add tenancy` makes a flat, untenanted application tenanted. The tenant table becomes the
next schema migration, with two development tenants as a data migration under
`schema/devseed`; the tenant-record struct joins the resource package as a global resource
annotated `@tenant`, from which the generator derives the tenant segment and emits
`New<Record>Roster`, the constructor of the tenant roster; the generator program gains
`WithConcealedDomains`; the data level gains the roster, built with that constructor over
the live service's tenants signal and started where the configuration is built (a new
`tenancy.go` plus a field and the start inserted into `DataConfiguration`), so a tenant
created on any instance is usable at once, without a restart; the app exposes the roster to
the generated code as `TenantRoster()` and hands its `Domains` to the session permissions
(a new `tenancy.go` plus the `Configurer`, `App` and `UserPermissions` edits); and the
reference's tenant service is copied into the browser app. Every edit
whose anchor the application lacks is recorded in the brief as the agent's. Which
resources become tenant-scoped and how their rows are assigned, the bootstrap order, the
harnesses, the tests, and the tenant picker are the agent's, and `tenancy-wired` holds it
to them.

```sh
impulse add tenancy --agent
impulse add tenancy --tenant-table Organizations
```

`add auth` adds an auth: a population that signs in one way and holds roles in its own
permission store. An auth is a package, `pkg/auth/<name>`, and the base has one, `staff`.
The new package is a copy of an existing auth's with every name substituted, so it owns
`<Name>Sessions`, `<Name>SessionUsers` (password only), the `<Name>` store prefix, the
`<name>` and `<name>-xsrf` cookies, and `pkg/auth/<name>/roles.json` (empty, embedded by the package) from the start; its table migrations are
copied under the new prefix; and the data level constructs it beside the auth it came
from, with an accessor in a new file. The copy carries the engine's change signal over
the live service as the auth it came from does (`access.WithChangeSignal`, built from
the `Settings.Signals` the copied construction passes), so the `change-signal` check
passes for the new package. `--preauth` swaps the constructor to the preauth
flavor. Binding a surface to it, checking its roles in the deploy, its development identities, and
the stranger tests are the agent's.

`--oidc-azure` adds an auth whose people sign in through the organization's directory over
OpenID Connect. It is copied from an OIDC auth the application already has, or else from
the reference: the `outlets` skeleton's `members` auth, bound to its portal outlet, with
every request in a session group bound to its auth (`pkg/auth.Bind`) so permission checks
and tenant visibility answer from that auth's store. The copy owns `<Name>Sessions` and
`<Name>OIDCUsers` (the user anchor keyed by the directory's immutable identifiers), the
data level reads its directory registration from `APP_<NAME>_OIDC_ISSUER_URL`,
`_CLIENT_ID`, `_CLIENT_SECRET`, and `_REDIRECT_URL` (the secret's field carries
`secret:"true"` beside its env tag, as the skeleton's cookie key does: the marker bedrock
reads to serve a variable from Secret Manager), and the Procfile builds with the
session library's `skipAuth` tag, which simulates the directory from `APP_USERNAME` until
the application is registered with one; the tests run under the same tag. `--authority`
says who owns role membership and is asked when not given, never defaulted, because the
wrong answer deletes hand-assigned roles at the next login: `directory` writes
`session.RoleSync` (the directory's role claims are reconciled at every login and roles
they do not name are removed; the bootstrap seeds no roles), `application` writes
`session.DisableRoleSync` (roles are assigned in the application). The OIDC session group
(login redirect, callback, front-channel logout), the login button, the harness login
helper, and the stranger tests are the agent's, with the reference showing each.

`--oidc-google` is the same auth against Google Workspace. When the application has no
Google auth to copy, the Azure reference is rewritten for it: `session.NewOIDCGoogle` over
`sessionstorage.NewSpannerGoogleOIDC`, a hosted domain (the Workspace domain logins are
restricted to) in place of the issuer, so the registration is `APP_<NAME>_OIDC_CLIENT_ID`,
`_CLIENT_SECRET`, `_REDIRECT_URL`, and `_HOSTED_DOMAIN`; a user anchor keyed by the
subject claim (`Sub`, `Hd`) in place of Azure's tenant and object identifiers; and no
front-channel logout, since Google has no directory-initiated logout. The rewrite is
textual, so the brief says to read the package over. With `--authority directory` the
directory's authority is its Google Groups: a group named `<prefix><role>@<domain>`
assigns `<role>`. The groups are read through Google's Cloud Identity Groups API with the
signing-in person's own access token, so the sign-in asks for one extra OAuth scope, the
groups read-only scope, beside the identity scopes. No service account, key, domain-wide
delegation, or administrator role is involved. The Cloud Identity API must be enabled in
the Google Cloud project that owns the OAuth client (the one `_CLIENT_ID` names), or every
sign-in fails. The package parses the lookup setting with
`session.ParseGroupLookup(settings.Directory.GroupLookup)` and hands
`session.GoogleRoleSync` the group prefix and that lookup. The registration gains
`_GROUP_PREFIX` (the `<prefix>`) and `_GROUP_LOOKUP`. `_GROUP_LOOKUP` is how far the
lookup reaches: `direct` (the default when unset) reads the groups the person is a direct
member of; `nested` climbs from those to the groups they are in, level by level, for a
directory that nests its role groups (a team group made a member of a role group). Google
leaves out a group whose member list the person may not view, so that one membership does
not count, and under `nested` the climb cannot go through it. That is not an error: the
sign-in goes on with the groups Google does return. Under the session library's `skipAuth`
tag the lookup is simulated: every login is in the groups `APP_ROLES` names and
`_GROUP_LOOKUP` has no effect, but the group prefix is still set from the start, since
`session.NewOIDCGoogle` refuses an empty one.

```sh
impulse add auth partners --agent
impulse add auth devices --preauth
impulse add auth members --oidc-azure --authority application --agent
impulse add auth staff2 --oidc-azure            # asks: directory or application?
impulse add auth alumni --oidc-google --authority application
```

### add site

`add site <name>` adds a site: a stand-alone application on a host of its own under
`apps/<name>/`, with its own main package, handlers, router, resources (empty to start),
authorization suite, and browser workspace, copied from the first site with the imports
renamed; its generator program and directive; its serve and browser processes on the next
ports; its TypeScript target in the shared generator; and its router collection in the union
the roles are reconciled against. On a flat application the first site added promotes the
layout to sites, the one non-additive transition: the existing site moves under
`apps/<existing>/` and every import of its packages follows, its generator becomes
`cmd/generate/<existing>generator`, the site level's bundle variable becomes `APP_DIST` (set
per site process, inline in the Procfile, as `PORT` is), the deployment's collection
becomes the union of the sites' router collections (`access.UnionCollection`, which
refuses sites that declare a shared resource differently), and a shared generator is laid
in over an empty `pkg/sharedresources`. The CI workflow is rewritten from the code, since
impulse owns it: an `angular-<name>` job builds, lints and tests the new site's browser
workspace, and on a promotion the existing site's job becomes `angular-<existing>`.
`--existing` names what the existing site becomes and is asked when not given, never
defaulted, since the name is the site's directory for good. Everything existing belongs to
that site. The new site's resources, its place in the integration suite, its browser
application's own titles and pages, and the deployment configuration outside the repository
(build path filters, source directories, a host) are the agent's; the reference is the
`sites` skeleton.

```sh
impulse add site portal --existing console   # promotes a flat application, then adds portal
impulse add site kiosk                    # a third site, copied from the first
```

### add feature

`add feature <name>` declares a feature flag. The resources package gains a
`resource.Feature` constant named after the flag (`cargo_manifest` becomes
`CargoManifest`) with a doc stub, in `features.go` (started when the package has none);
the development seed gains the flag's row, off, in
`schema/devseed/<n>_dev_feature_flags.up.sql` (started at the next data migration number
when the seed has none), where setting `Enabled` to `TRUE` starts development and the
test environments with the feature on, since the deploy's `MigrateFeatures` keeps
`Enabled` where a row exists and production has no seed; and `go generate` runs, so the
generated `Features()` lists the flag for the deploy and the browser's `Feature` union
carries it. The name is 1 to 64 characters of `[a-z0-9_]` opening with a letter, and a
name already declared is refused. In the sites layout `--site` names the site whose
resources package declares the flag, since each site's generator resolves `@feature`
against its own package.

The check then fails on `feature-flags`, because the new flag gates nothing and is read
nowhere, and the brief hands the agent what is left: what to gate (`@feature(<Constant>)`
on a resource, a field or a method, or a read in Go or in the browser), the description to
write in place of the stub, how a flag is flipped (the generated `SetFeature` method
under the `FeatureAdministrator` role, which the development login holds), and where the
library's `FeatureFlagsDialog` link lives (`openFeatureFlagsDialog(inject(MatDialog))`
from `@cccteam/resource-angular/ccc-feature-flags`; the application decides the place).

```sh
impulse add feature cargo_manifest --agent
impulse add feature debriefs --site console
```

### add files

`add files` wires the framework's file store (`resource/filestore`) into a flat
application, the whole of it, so the check is clean when it ends. The data level gains
`FileStoreSettings` and `LoadFileStoreSettings` in `pkg/config/files.go`, reads
`APP_FILE_STORE` into its environment struct, opens the store the variable names before
the configuration is built (`openFileStore`; unset leaves the store closed, so the migrate
and bootstrap commands run without one), builds the resource client over it
(`resource.NewSpannerClient(client, fileStoreOptions(files)...)`, which is
`resource.WithFileStore`) and releases it in `Close`. `.envrc.template` sets
`APP_FILE_STORE=file://uploads` in the data block and `.gitignore` ignores `uploads/`;
`cmd/bootstrap` gains `emptyFileStore`, called before the development seed, which empties
a `file://` store since no row holds a file then. `pkg/jobs` declares the cleanup
(`CleanupCommand`, `cleanup-files`, and `CleanupFiles`, `filestore.Cleanup` over the
default store through the generated `FileHolders()`), and `cmd/jobs` is the job process
running it with `-window` and `-dry-run`. The rpc package gains `CleanUpFiles`, a method
marked `@rpc` and `@schedule("0 9 * * *")` whose `Execute` starts the job process on the
cleanup command through the client's `Jobs()`; an application without an rpc package gains
`pkg/rpc` with a `Client` carrying the starter, and `WithRPC("pkg/rpc")` in the generator
program. The site level builds the scheduler guard (`scheduled.FromEnvironment`, from
`APP_SCHEDULER_INVOKER`) and the job starter (`jobs.FromEnvironment`, from `APP_JOBS_TEMPLATE` and `APP_VERSION`)
and exposes them as `Scheduler()` and `Jobs()`; the `Configurer` asks for both, the `App`
carries the guard and the RPC client built over the starter, and `app/scheduled.go`
declares `SchedulerAuth` (the middleware the generated router mounts the scheduled routes
behind) and `RPCClient`. Every test configurer (a type in a test file declaring
`LogExporter`) gains a nil guard, `jobs.NewFake()`, and a memory store its resource client
is built over (`files *filestore.Mem`, passed as `resource.WithFileStore`), so a file route
the application declares later is served in the suites. What the application wired already
(a scheduled method of its own brought the guard, the accessor, the middleware) is left as
it is: the editors add nothing the code declares, and the report says what was there. Then
`go generate` emits the method's handler, its route under `/_scheduled` and the router's
requirement.

An application already holding an rpc package keeps its `Client`: the method is written
into the package, and giving the client a `Jobs()` accessor fed from the configuration is
the agent's, as is the cleanup command where `cmd/jobs` exists, and the image: a Dockerfile
builds `/jobs` beside the other binaries (`bedrock check` asks for it once the job process
exists). Which resources record
files (`@file` on a `resource.Key` column, an `@upload` method) is the application's;
until one does, the store is wired and idle. The `file-store` check watches the variable
from then on, and the stack reads it to make the bucket (`gs://<bucket>` on Cloud Run),
deploy the job process beside the service and schedule the method.

```sh
impulse add files
impulse new ./harbor --module example.com/harbor --auth staff --files
```

### swap auth

`swap auth <name>` moves an existing auth to a directory: `--oidc-azure` or `--oidc-google`,
with `--authority` asked as for `add auth`. The auth keeps its name, its permission store,
its role file, and the surfaces bound to it. Its package file is rewritten from the
reference OIDC auth under its own name (what the file carried beyond the base's shape is in
git to re-apply); one migration drops its session tables and creates them in the new shape,
with a down that recreates the old tables from their own migrations; the data level's
construction gains the login page and the directory registration; the Procfile builds with
`skipAuth`; every outlet bound to the auth in the generator program (`Auth(<package>,
<flavor>)`) is rewritten to the new flavor, so regeneration mounts the directory's login
routes; and where the App (and a hand-written router) stand in the base's shape, the
handler types (`session.PasswordAuthHandlers`, the embedded `*session.PasswordAuth`) and,
in a hand-written router, the password login route are swapped for the directory's.
Everyone in the auth signs in again. Its role
assignments are dropped by recreating the assignment table, since they were keyed by
password usernames the directory need not present; `--carry-roles` keeps them when the
usernames were already the directory's. The bootstrap (identities without passwords, no
account creation), the login page (a button to the login route), the harness login helper
(the `skipAuth` twin), and the removal of password user management are the agent's, so the
tree does not build until the bootstrap follows; the brief says so and names the model for
each.

```sh
impulse swap auth staff --oidc-azure --authority application --agent
impulse swap auth staff --oidc-google --authority application --carry-roles
```

## impulse remove

`remove` takes an option out the way `add` puts one in: the deterministic half is made
(the option's files deleted, the generator program and the registrations edited, `go
generate` run), staged, and checked, and what still names the option in the hand-written
code is handed to an agent with the same brief, the rendered reference showing what the
option's wiring looks like so it is recognizable on the way out.

`remove outlet <name>` removes a router outlet from every site that declares it. The
generator program loses `WithRouterOutlet` (with the comment lines that introduced it) and,
for a session outlet, the `GenerateTypescript` target for the outlet; the outlet's browser
project is deleted and taken out of `angular.json`, the package scripts (`start:<name>`,
`build:<name>`, `lint:<name>`, `test:<name>`, and its part of `build`, `lint`, and `test`),
and the Procfile; and
an `@outlet` list that names the outlet beside others drops it, so those structs keep
their other outlets. A struct on the outlet alone keeps its annotation, which fails
generation, because whether it moves to the default outlet (the console's people reach it
then) or leaves the application with its table is a decision about who may reach it; the
brief lists each. Regeneration drops the outlet's group from the generated router along
with its `Hooks` field, so a hook naming it fails to compile until removed; the App's
handlers for the outlet, its configuration and environment lines (the brief quotes them),
and its tests are the agent's (and, under a hand-written router, the group that mounted
it). The auth the outlet was bound to stays. There is no data consequence.

`remove site <name>` removes a site from an application in the sites layout: `apps/<name>/` is
deleted with its generator program and directive, its TypeScript target leaves the shared
generator, its router collection leaves the union the roles are reconciled against, its
processes leave the Procfile, and the CI workflow is rewritten from the code without the
site's browser job. The remaining sites stay where they are: an application
left with one site keeps the sites layout, with the site under `apps/`, the
shared generator emitting into it, and a union of one element; nothing moves back to the
root. The application's last site is not removed. The integration suite that served the
site (and any other file still importing its packages, which the brief lists), the
deployment configuration outside the repository, and the tables only the site's resources
declared (declare them in the site that serves them now, or drop them by migration) are
the agent's.

`remove feature <name>` retires a feature flag, the step that makes a feature permanent or
abandons it. The constant goes (and `features.go` with it when it declared nothing else,
or the resource import when nothing else reads it), every `@feature(<Constant>)`
annotation naming it goes (a line of its own whole, a shared line keeping its other
text), so the resources, fields and methods it gated are served unconditionally, the
flag's row leaves the development seed, and `go generate` runs, so `Features()` and the
browser's `Feature` union lose the name. The hand-written Go and TypeScript that still
read the flag are listed by file and line: each fails to compile until it changes, and the
remaining code runs unconditionally, so the on branch is inlined to make the feature
permanent or the feature's code is deleted to abandon it. That list is handed to the agent
in the brief even when the check is clean (a browser use fails the browser's own build,
not the check), and `--agent` launches it. Nothing in the database refuses the removal:
the next deploy's `MigrateFeatures` deletes the row, and the `FeatureFlagChanges` rows
stay as the record of every flip.

```sh
impulse remove outlet portal --agent
impulse remove site kiosk
impulse remove feature debriefs --agent
```

## Templates

The application skeletons `new` and `add` will render live under
`internal/skeleton/_candidates/`, one directory per shape (`solo`, `tenanted`, `outlets`,
`sites`), and are embedded in the binary, so a release ships exactly the trees it was
tested with and renders them offline.

While embedded they are not Go modules: each carries its `go.mod` as `go.mod.tmpl`, and
the leading underscore keeps the tree out of `./...` so nothing compiles it in place.
Rendering writes `go.mod` back under the target module path and rewrites every import.
The impulse version a template's `go.mod` requires is only what `render <candidate>` and
`new --dev-root` keep: `new` replaces it with the version of the impulse that runs it, so
nobody moves it by hand.
A template names its application by its own candidate name in five places only — the
workspace name in `package.json` and `bun.lock`, the service name and development
database ids in `.envrc.template`, the README heading, and, in display form, each browser
project's titles (the page title, the component title fields, the login card) and the
`name` and `short_name` of its web app manifest (`public/manifest.webmanifest`, the
portal's ending in Portal) — and rendering puts the application's name there; everywhere
else the prose says "the application", since the candidate names are English words.
The templates are therefore validated by rendering them and running the rendered
application's build, tests, and `impulse check`, never in place, and under a module path
outside `github.com/cccteam/ccc/impulse` (ccc's CI renders them as
`example.com/ci/<candidate>`): the placeholder path lies inside impulse's module, which the
`tool` directive puts in the application's module graph, so under the placeholder path
every package of the rendered candidate would be an ambiguous import. Their browser workspaces
use bun, with `bun.lock` committed. Two yalc-era settings travel with them until
`@cccteam/resource` is published: an `overrides` entry in package.json that points
@cccteam/resource-angular's peer dependency on the client at the yalc link, and `peer = false` in
bunfig.toml, because bun would otherwise install a second copy of the client beneath
the Angular library and TypeScript would see two declarations of every client type. Every peer an
application needs is therefore a direct dependency. Build products
(`node_modules`, `.angular`, `dist`, `.ccc-cache`, `.yalc`, `go.work`) are never embedded;
`internal/skeleton`'s tests enforce that.

## Deploy hooks

An application's own work at a fixed point of a deploy (a check against a dependency after
the migrations, a check against the new revision before traffic moves to it, a smoke test
after) is a hook. A hook has no database and never starts the application's job process:
work on the data is the running service's, which starts the job of its own build.
The package `github.com/cccteam/ccc/impulse/deployhook` is the contract for writing hooks
in Go: a program at `cmd/deployment/hooks`, beside the migrate command, passes one
`deployhook.Hooks` literal to `deployhook.Main`, with a function for each stage it
implements:

```go
func main() {
	deployhook.Main(deployhook.Hooks{
		BeforeTraffic: checkNextRevision,
		AfterTraffic:  smokeTest,
	})
}
```

Each function receives the build's facts as typed values (`deployhook.Facts`: the
application, the environment, the release, the image and its digest, the pull request,
the next revision's URL) and answers an error to stop the build at its stage. bedrock
reads the stages from the literal, renders a pipeline step for each and checks that the
Dockerfile builds the program into the image as `/hooks`; the pipeline takes it out of the
image it built and runs it on the build worker, where a hook script runs. The contract
holds only the four stages after the image build (`BeforeMigrate`, `AfterMigrate`,
`BeforeTraffic`, `AfterTraffic`), so a program cannot implement the two stages that come
before an image exists (`before-build`, `after-down`); those take a script,
`infrastructure/hooks/<stage>.sh`. A program and scripts mix, one or the other per stage.

## Development

```sh
go test ./...
golangci-lint-v2 run
```

Fixture applications for the tests live under `app/testdata/`. They are
synthetic: a lighthouse-keeping flat application (`flat`), a harbor application in the sites
layout (`sites`), and a generator program the tool cannot read (`badprogram`).
