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

## impulse new

`new` creates an application: the base skeleton (flat layout, one auth, nothing else on)
rendered under your module path, with the first auth named by you.

```sh
impulse new ../beacon --module example.com/acme/beacon --auth staff
impulse new ../harbor --module example.com/acme/harbor            # asks for the auth name
impulse new ../harbor --module example.com/acme/harbor --auth members --dev-root ~/Development/github.com/cccteam
```

An auth is a population that signs in one way and holds roles in its own permission
store, and it is a package, `pkg/auth/<name>`, whose name is also the prefix of its tables
(`<Name>Sessions`, `<Name>Roles`, ...), its cookies (`<name>` for the session, `<name>-xsrf`
for the XSRF token, so two auths on one host never overwrite each other's), and the stem of
its roles file. The web app that binds to an auth names the same XSRF cookie in its
HttpClient configuration (`withXsrfConfiguration`), since the browser echoes that cookie
in the `X-XSRF-TOKEN` header. So the name is asked for when `--auth` is not given, and there is no default: a default word would
land in every application whose author skipped the question. A good name is the population
that signs in, plural, lowercase, one word: staff, members, partners, devices. It cannot be
a Go keyword, a package the application imports, or a directory it has.

The tree is committed as the application's first commit (`--skip-git` leaves it
uncommitted), so `impulse add` can start from a clean tree; with `--dev-root` the `go.work`
it writes is ignored by git. Options come afterwards, one reviewable change each:
`impulse add tenancy`, `impulse add outlet`, `impulse add auth`.

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
| `generator-program` | Every generator program uses options this release knows, with literal arguments. A program the tool cannot read completely is one it cannot later edit or migrate. |
| `options` | The generator programs declare one coherent option set, and the report states it: layout (flat or multi-site), sites, tenancy (`WithDomainRoute`, `WithConcealedDomains`), outlets, and targets. Handlers come with routes, `ForOutlet` names a declared session-serving outlet, the referenced directories exist, a `//go:generate` directive runs every program, the sites agree on tenancy, and a second site lives under `apps/<site>/`. |
| `tenancy-wired` | A program with `WithDomainRoute` has a migration creating the tenant-record table the segment names, at least one struct annotated `@permissionScope(domain)`, and every `access.MigrateRoles` call outside tests passing domains. A program without it has no tenant-scoped structs and passes no domains. The compiler and the generator hold the rest of the seam. |
| `outlet-wired` | Every outlet a program declares (the default from `GenerateRoutes` and each `WithRouterOutlet`) has its generated routes mounted by a hand-written file in the router package, a session-serving outlet has a `GenerateTypescript` target naming it, and that browser project's development proxy forwards the outlet's prefix. An outlet with no `@outlet` members yet is noted, not failed. |
| `sites-wired` | In the multi-site layout, every site has a main package under `apps/<site>/`, a process in the Procfile or process-compose file running it on its own `PORT`, and every site's router is imported by some package calling `access.MigrateRoles`, so a role migration (the union collection, or one per auth) reconciles against the site's resources. |
| `auth-wired` | Every session authenticator constructed outside tests (`session.NewPasswordAuth`, `NewOIDCAzure`, `NewOIDCGoogle`, `NewPreauth`) reads tables a migration creates: its sessions table, its users table, and the impersonation table when the storage attaches one. Two flavors never share a sessions table. The report lists the auths, one per distinct flavor and table set, each named by its package when it lives in one (`pkg/auth/<name>`), with its session and XSRF cookies when named, and an OIDC auth's role-membership authority (directory for `RoleSync`, application for `DisableRoleSync`). |
| `auths-wired` | Every auth package (`pkg/auth/<name>`, constructing a session authenticator) is constructed by the data level (`<name>.New` called outside tests), provisioned from its roles file (`<name>.RolesPath` read by a file that migrates roles, and the file exists), and bound by a surface (a package outside `config` and `cmd/` takes `*<name>.Auth`). No two auth packages issue the same cookie, session or XSRF, a name left unset being the session library's default (`auth`, `XSRF-TOKEN`); the browser keeps one cookie of a name per host, so a login to one auth would overwrite the other's. An auth that hands role membership to its directory (`session.RoleSync`) has no role writer in the application reaching its store, since the directory removes those roles at the next login. Authenticators outside auth packages warn. |
| `skipauth` | When an auth signs in through a directory (the OIDC flavors), the simulated directory stays in development and tests: no application code reads `APP_USERNAME` or `APP_ROLES` (only the session library's `skipAuth` build does), and no build description (Dockerfile, cloudbuild, Makefile) carries the tag, which would let a deployed build accept any name as a login. |
| `emulator-version` | The generator option, the process files' image tags, and the test harnesses name one Spanner emulator version. |
| `prettier-ignore` | Each browser app's `.prettierignore` excludes the generated TypeScript. Prettier reflowing generated files breaks generate idempotence. `--fix` adds the entry. |
| `eslint-ignore` | Each browser app receiving generated TypeScript ignores it in its eslint flat config (`ignores: ['**/zz_gen_*.ts']`) or `.eslintignore`. Generated shapes trip stylistic rules, and the output is not the developer's to change. |
| `package-manager` | Every browser app carries the same kind of lockfile (bun, npm, yarn, or pnpm), and the process files and package scripts invoke that tool and no other. Two tools in one repository means two lockfiles drifting apart. |
| `rpc-execute` | Every generated RPC handler calls the method's `Execute`. A handler the generator could not type-check decodes and returns without running the method. |
| `multi-site` | In a multi-site application, every generator reads the one schema and the shared generator's TypeScript reaches every site's browser app. |
| `env-template` | Every `env` struct tag without a default appears in the development environment template (`.envrc.template`, `.env.template`, or `.env.example`). `--fix` adds the missing lines. |
| `pins` | Framework pins in `go.mod` are released versions. Pseudo-versions and local replaces warn but do not fail. |
| `gowork-off` | `GOWORK=off go build ./...` and `go vet ./...` succeed, so the pins in `go.mod` resolve without the workspace. |
| `regen` | `go generate ./...` reproduces the generated files on disk (content compared before and after, so it holds in untracked trees too). Needs the Spanner emulator and rewrites the working tree; `--skip-generate` leaves it out. |

Statuses: `PASS`, `FAIL`, `WARN` (reported, does not fail the run), `SKIP` (with the
reason).

## impulse render

`render` copies one embedded skeleton into a new or empty directory under the module
path you name, rewriting every import and `go.mod` to it. It is the primitive `new`
builds on and the way the templates are validated: render one, then build, test, and
`impulse check` the result. The templates carry `staff` as their placeholder auth; `new`
renames it, `render` keeps it.

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

## impulse handoff

`handoff` is the protocol between the tool and an agent for the work the tool cannot do
mechanically. It runs `impulse check` on a clean working tree in a git repository and,
when checks fail, writes a brief for an agent to `.impulse-handoff.md` at the application
root: the failing checks verbatim as the obligations, the option set in force (read from
the generator programs), a reference application when one is given, and the rules. The
brief is Markdown any agent can read; the tool never reads it back.

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
impulse add outlet portal --prefix portal/api --sessions --agent
impulse add outlet machines --prefix machines --api-key
```

`add outlet` adds a router outlet to a flat application. For a session outlet the
generator program gains `WithRouterOutlet(name, prefix, ServesSessions())` after
`GenerateRoutes` and a `GenerateTypescript` target for the outlet copied from the default
target's, and the console's browser project is copied to `web/<name>` with its API prefix,
base path, and compiler output rewritten and registered in `angular.json` (serving under
`/<name>` on the next port), the package scripts, and the Procfile. For an API-key outlet
the program gains `WithRouterOutlet(name, prefix)` alone. `go generate` then emits the
outlet's routes, handlers, and client. The router mount, the served assets, the
configuration, the members (`@outlet`), and the tests are the agent's, and `outlet-wired`
holds it to them.

`add tenancy` makes a flat, untenanted application tenanted. The generator program gains
`WithDomainRoute` (the table's kebab-case name) and `WithConcealedDomains`; the tenant
table becomes the next schema migration, with two development tenants as a data migration
under `schema/devseed`; the tenant-record struct joins the resource package as a global
resource; the data level gains the roster read at startup and the `DomainVisible` seam
(a new `tenancy.go` plus a field and the load inserted into `DataConfiguration`); the app
exposes the seam to the generated code (a new `tenancy.go` plus the `Configurer` and `App`
edits); and the reference's tenant service is copied into the browser app. Every edit
whose anchor the application lacks is recorded in the brief as the agent's. Which
resources become tenant-scoped and how their rows are assigned, the bootstrap order, the
harnesses, the tests, and the tenant picker are the agent's, and `tenancy-wired` holds it
to them.

```sh
impulse add tenancy --agent
impulse add tenancy --table Organizations
```

`add auth` adds an auth: a population that signs in one way and holds roles in its own
permission store. An auth is a package, `pkg/auth/<name>`, and the base has one, `staff`.
The new package is a copy of an existing auth's with every name substituted, so it owns
`<Name>Sessions`, `<Name>SessionUsers` (password only), the `<Name>` store prefix, the
`<name>` and `<name>-xsrf` cookies, and `schema/roles/<name>.json` from the start; its table migrations are
copied under the new prefix; and the data level constructs it beside the auth it came
from, with an accessor in a new file. `--preauth` swaps the constructor to the preauth
flavor. Binding a surface to it, provisioning its roles and development identities, and
the stranger tests are the agent's.

`--oidc-azure` adds an auth whose people sign in through the organization's directory over
OpenID Connect. It is copied from an OIDC auth the application already has, or else from
the reference: the `outlets` skeleton's `members` auth, bound to its portal outlet, with
every request in a session group bound to its auth (`pkg/auth.Bind`) so permission checks
and tenant visibility answer from that auth's store. The copy owns `<Name>Sessions` and
`<Name>OIDCUsers` (the user anchor keyed by the directory's immutable identifiers), the
data level reads its directory registration from `APP_<NAME>_OIDC_ISSUER_URL`,
`_CLIENT_ID`, `_CLIENT_SECRET`, and `_REDIRECT_URL`, and the Procfile builds with the
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
textual, so the brief says to read the package over. Only `--authority application` is
laid in for Google: its directory authority is a Groups lookup (`session.GoogleRoleSync`
over `googlegroups.NewDirectory`, with Admin SDK credentials) that the session library's
simulated directory does not simulate, so a directory-run Google auth could not be signed
in to in development; it is refused with that reason.

```sh
impulse add auth partners --agent
impulse add auth devices --preauth
impulse add auth members --oidc-azure --authority application --agent
impulse add auth staff2 --oidc-azure            # asks: directory or application?
impulse add auth alumni --oidc-google --authority application
```

### swap auth

`swap auth <name>` moves an existing auth to a directory: `--oidc-azure` or `--oidc-google`,
with `--authority` asked as for `add auth`. The auth keeps its name, its permission store,
its roles file, and the surfaces bound to it. Its package file is rewritten from the
reference OIDC auth under its own name (what the file carried beyond the base's shape is in
git to re-apply); one migration drops its session tables and creates them in the new shape,
with a down that recreates the old tables from their own migrations; the data level's
construction gains the login page and the directory registration; the Procfile builds with
`skipAuth`; and where the App and the router stand in the base's shape, the handler types
(`session.PasswordAuthHandlers`, the embedded `*session.PasswordAuth`) and the password
login route are swapped for the directory's. Everyone in the auth signs in again. Its role
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

## Templates

The application skeletons `new` and `add` will render live under
`internal/skeleton/_candidates/`, one directory per shape (`solo`, `tenanted`, `outlets`,
`sites`), and are embedded in the binary, so a release ships exactly the trees it was
tested with and renders them offline.

While embedded they are not Go modules: each carries its `go.mod` as `go.mod.tmpl`, and
the leading underscore keeps the tree out of `./...` so nothing compiles it in place.
Rendering writes `go.mod` back under the target module path and rewrites every import.
The templates are therefore validated by rendering them and running the rendered
application's build, tests, and `impulse check`, never in place. Their browser workspaces
use bun, with `bun.lock` committed. Two yalc-era settings travel with them until
`@cccteam/resource` is published: an `overrides` entry in package.json that points
ccc-lib's peer dependency on the client at the yalc link, and `peer = false` in
bunfig.toml, because bun would otherwise install a second copy of the client beneath
ccc-lib and TypeScript would see two declarations of every client type. Every peer an
application needs is therefore a direct dependency. Build products
(`node_modules`, `.angular`, `dist`, `.ccc-cache`, `.yalc`, `go.work`) are never embedded;
`internal/skeleton`'s tests enforce that.

## Development

```sh
go test ./...
golangci-lint-v2 run
```

Fixture applications for the tests live under `internal/app/testdata/`. They are
synthetic: a lighthouse-keeping single-site app, a harbor multi-site app, and a generator
program the tool cannot read.
