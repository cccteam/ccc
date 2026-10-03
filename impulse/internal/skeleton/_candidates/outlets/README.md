# outlets

A tenant-scoped application on the cccteam resource stack with every outlet kind on one
site. One hostname, three outlets — the console on `/console/api` with its browser
application at `/console`, a second browser application (the portal) on `/portal/api`
behind the same session handling with its application at `/portal`, and a machine REST
API on `/machines` behind an API key that binds requests to a service identity with roles
like any user. Every browser outlet's API sits under its application's mount path, and no
application is mounted at `/`: an installed browser application's scope is every URL under
its start, so one at `/` would own the origin and the other would never get its own install
prompt. The root alone redirects to `/console/`; every other unmatched path is 404.
Tenancy as data: the `Tenant` record (`@tenant`) over the `Tenants` table is the domain
universe, tenant-scoped routes live under `/console/api/tenants/{tenantID}/` (and under each
outlet's prefix), the data level's tenant roster keeps the tenants current on every
instance (a tenant created through the API is usable at once, without a restart), and
tenant existence is concealed from logins that hold nothing there. `Announcement` is the first tenant-scoped resource — a login's tenant list
is where it holds a grant, and a grant needs a tenant-scoped resource to land on, so the
application ships one. `Reading` is machines-only. The console and the portal each show who
is signed in, the tenants they can pick, and the digest for the selected tenant.

## Layout

- `main.go` serves the API and the console's built bundle.
- `pkg/config` is the configuration, as a chain of levels: core (every process), data
  (every process that opens the database), site (the served site). Each process
  constructs the level it needs — `cmd/bootstrap` and `cmd/deployment/migrate` stop at
  data — so a deploy step supplies exactly the variables its process reads.
- `pkg/auth` holds the auths, one package per population: `staff` (the console's people,
  who sign in with a password) and `members` (the portal's people, who sign in through the
  organization's directory over OpenID Connect). Each owns its session tables and cookie,
  its permission store, and its role file, so a member and a staff login with the same
  name are two unrelated principals. The members auth leaves role membership to the
  application (`session.DisableRoleSync`): the bootstrap assigns the development members
  their roles.
- `app` holds the handlers: generated resource handlers for every outlet plus the
  hand-written middleware, the machines outlet's API-key authentication, and the two
  static-asset surfaces. `pkg/router` composes them: the staff auth's session group
  around the console, the members auth's around the portal (login redirect, directory
  callback, front-channel logout), an API-key group for the machines outlet, one Angular
  application per browser outlet under its mount path, and the root redirect to the
  console's.
- `pkg/resources` holds the resource structs the generator reads: `Tenant` (the tenant
  record, a global resource), `Announcement` (tenant-scoped, on the console and the
  portal), and `Reading` (tenant-scoped, machines-only). `schema/migrations` holds the tables they describe; `schema/devseed` the
  development tenants; `pkg/auth/staff/roles.json` and `pkg/auth/members/roles.json` each
  auth's role file: the default roles the release ships, embedded in the binary and
  validated against the generated collection when the auth's engine opens. A domain role
  is held in every tenant domain, so nothing provisions a file per tenant. The staff file
  authors `Administrator_Global` over the tenant record, `Administrator_Domain` over the
  console's announcements, and `Machines_Domain`, the service account's role, over the
  readings; the members file authors `Administrator_Domain` over the portal's
  announcements. A new resource stays invisible until a role in its auth's file is granted
  it. Every grant in either file is proven live by `test/integration/grants_test.go`, which
  opens each auth the way the deployment does and asks its engine about each
  unconditional grant; a conditional grant is proven by a test case that names it through
  `provesGrant`, and `impulse check` fails on one no case names.
- `pkg/deploy` holds the database steps a deployment runs: the schema migrations, then
  each auth's role policy check (`CheckRoles`), which prints what its store holds that
  this release cannot use as written. Then `MigrateFeatures` brings the `FeatureFlags` table
  to the flags the release declares (section Feature flags). `cmd/deployment/migrate` is the deploy step;
  `cmd/bootstrap` reuses it to stand up an emulator database, seeds the development
  tenants (tenancy is data), and adds the development logins, the development member, and
  the machines service account from `cmd/bootstrap/users.json`, each with its
  memberships by where they are held (`global`, `everyDomain`, `domains`). Its test runs
  each auth's role file through the validation the engine performs when it opens
  (`access.ValidateRoles`) and pins the warnings the deploy would print as typed values,
  none expected: a warning is accepted by pinning it there, or the role is fixed.
  A development seed under `schema/devseed` (data files as migrations, tracked apart from
  the schema, so a seeded database takes nothing twice) is applied by `cmd/bootstrap` and
  by the migrate command with `-seed`, which the pipeline passes in test environments and
  never in production; an application without the directory has nothing to apply. The
  migrate command's `-version` prints what each migrations table says about the database,
  and `-force <n>` and `-force-data <n>` set a table to a version (-1 for no version), for
  the states the migration runner refuses to guess at; the pipeline passes them from the
  operations workflow (`bedrock migration`), and none of them applies a migration.
- `test/authz` is the generated authorization matrix over the generated test router;
  `test/integration` drives the served stack (real router, session, engine) end to end.
- `web/` is the Angular workspace, one project per browser application (`console`,
  `portal`); each has its own generated client (`GenerateTypescript` per outlet).
  `web/go.mod` exists only to keep Go tooling out of `node_modules`.

## RPC methods

When a write is not a row mutation — a workflow transition, a batch, a command with an
answer — it is an RPC method: an `@rpc` struct in an rpc package whose `Execute`
signature says how it runs (inside the handler's transaction, or against the resource
client outside one) and what it answers (`error`, or `(Result, error)`). The generator
mirrors the request and the result into the handler, gates the route on `Execute`, and
types the browser client's handle. Three conventions it cannot enforce are stated in
the resource package's README (§7): a method answers with identifiers and outcomes,
never rows; a method that only answers a question is a computed resource; and a
transaction-form body keeps its effects inside the transaction, so `X-Dry-Run` tells
the truth. Bodies are trusted by default; `Enforce(caller)` on a generated builder
arms a write or read against the caller's own grants.

## Feature flags

A feature flag is a release switch: a `resource.Feature` constant in `pkg/resources`
(`impulse add feature <name>` declares one, with a doc comment that is its description),
`@feature(<Constant>)` on a resource, a field or an RPC method to put it behind the flag,
and off means absent: 404 on its routes, left out of the permission digest, a gated field
unknown to the decoders. The value lives in the `FeatureFlags` table (migration
`000011_FeatureFlags`, the library's own statements, which `impulse check` compares
with `resource.FeatureFlagsDDL`): the deploy's `MigrateFeatures` writes a new flag off,
keeps a known flag's state, and deletes what the release no longer declares, and every
flip is recorded in `FeatureFlagChanges`. The App reads the table when it is built
(`FeatureSet`) and follows it from `Start` (the live service's signals document, the
`features` kind, and the five-minute backstop reread), so the generated routes, decoders,
digest and features route answer from one copy. Every outlet serves `features` under its prefix (`/console/api`, `/portal/api`, `/machines`), and the two session outlets serve `feature-flags` and `set-feature`. Flipping is the generated
`SetFeature` method, held by the `FeatureAdministrator` role in the staff role file
(Execute on `SetFeature`, List and Read on `FeatureFlags`), which the development `admin`
login holds, so a member of the portal sees the flags' effect and flips nothing; the library's `FeatureFlagsDialog` calls it, and the console decides
where its link lives once the browser side takes the released client. A flag's
development state is its row in `schema/devseed/<n>_dev_feature_flags.up.sql`, which
`impulse add feature` writes off (set it to TRUE to start development and the test
environments with the feature on) and `impulse remove feature` deletes with the constant
and every `@feature` naming it. The application declares no flag yet.

## Lists and paging

A list answers one page at a time: `limit` rows, or the resource's declared default
(`@page(default: N, max: M)`, otherwise 50), positioned by a sealed cursor the server
issues in its `Link` header (`rel="next"`, `rel="prev"`), never by an offset. A resource
declares the order a sort-less list takes with `@order(Field asc, …)`; the primary key is
appended so the order is total. Every paged request needs an order from somewhere: a
listed resource declares its `@order` or every page of it names a `sort`, and a request
with neither is refused with a 400 unless it asks `limit=all`, the whole list, unsorted.
The resources here declare theirs, on the column a person reads first, and an index
leading with the tenant column and then the order columns serves each tenant-scoped one's
pages (the generator warns where one is missing). `count=true` on a first page answers the
total in `Total-Count`; `limit=all` returns every row where the resource declares no
maximum. The cursors are sealed under a key derived from the cookie key: `pkg/config`
builds it with `resource.NewCursorKey` and the App hands it to every generated decoder
through `CursorKey()`. In the browser, `@cccteam/resource` follows the relations with
`page()` and reads every row with `all()`. Computed resources answer the same query
surface; their List function may take the conditions, sort, or page it pushes down and the
generated handler applies the rest.

## Running it

    cp .envrc.template .envrc && direnv allow
    overmind start -l spanner,firestore,server

That starts fresh Spanner and Firestore emulators, bootstraps the database (schema, tenants, both auths' roles, the
`admin` and `member` staff logins with password `password`, the `client` member, and the
machines service account), and serves every outlet on `$PORT` (:8093). The first run
compiles and bootstraps before it listens; wait for "Starting Server" in the server pane.

The server builds with the session library's `skipAuth` tag (see the Procfile), which
simulates the portal's directory: every portal login is `APP_USERNAME` from `.envrc`
(`client`), no directory is contacted, and only `APP_MEMBERS_OIDC_REDIRECT_URL` is read.
To sign in through a real directory, register the application there, fill in the
`APP_MEMBERS_OIDC_*` variables, and drop the tag. A deployed build never carries it.

### Browser apps

The console and the portal are two projects in one Angular workspace under `web/`, riding
the local ccc-lib checkout through yalc until `@cccteam/resource` is published. Attach it
once (the script publishes both packages to the local yalc store, links them, and runs
`bun install`):

    (cd web && ./ccclib.sh local)

`ccclib.sh` expects the ccc-lib checkout beside this application; set `CCC_LIB` to point
elsewhere. Then `overmind start` runs everything: the console at
http://127.0.0.1:4301/console/ (sign in as `admin` / `password`) and the portal at
http://127.0.0.1:4303/portal/ (the sign-in button signs in as the simulated directory's
`client`).

After a change in ccc-lib, push the rebuilt packages, restart the dev server, and reload the
page:

    (cd web && ./ccclib.sh push)
    overmind restart console portal

The dev server does not watch `node_modules`, so the restart is what picks the new build up.
The two packages are bundled with the application code rather than prebundled by Vite (the
`prebundle` exclusion in `angular.json`), so a plain reload shows the new build in any
browser profile: nothing is held behind an immutable URL, and no cache needs clearing.

Both projects' component specs run on Angular's unit-test builder (`@angular/build:unit-test`)
with Vitest under jsdom in Node: no browser, no Karma. `bun run test` runs the console's and
then the portal's once, the form the Checks section and CI use; `bun ng test console` or
`bun ng test portal` watches one. The specs beside the skeleton's components are the
pattern for the application's own: each dashboard renders a permission digest over a
scripted client from `@cccteam/resource-angular/testing` (`provideResourceTesting` puts the
generated client on a transport that records every request and answers from the test),
and the login pages, headers, top bars, footers, and shells have creation specs with the
same providers; the portal's login spec is the directory sign-in's.

## Live pages

A list page or a record page can stay current without polling. A request the page asked
to be live carries `X-Subscribe: <tab>`; a permitted list or read registers the page's
interest (who, which tab, which resource, which row or which tenant, until when) before
the query runs, every commit publishes the rows it touched into its subscribers' change
sets, and the browser refetches with `_v=<the change's timestamp>`, an answer the browser
caches for five minutes (`Cache-Control: private, max-age=300`). Nothing is polled and
the server holds no connection open.

The server side is wired. The data level opens the live service over the Firestore
database the configuration names: `APP_FIRESTORE_DATABASE` in a deployment, the emulator
through `FIRESTORE_EMULATOR_HOST` in development (`pkg/config`, `FirestoreSettings`;
the project defaults to the Spanner project). The live service is required: with neither
a database nor the emulator configured the data level refuses to start, naming the two
variables. The App hands it to the generated handlers
through `LiveService()`, and the generated router serves the live routes on both browser outlets (`/console/api/live/...` and `/portal/api/live/...`: `renew`, `unsubscribe`, `token`), while the machines outlet, a machine surface, refuses a request carrying the header. The
content security policy the App sends names the change feed's origins beside the
application (the emulator in development, Firebase's hosts in production), since the
browser's feed connects to them directly rather than through the API.
`schema/firestore` holds the rules the browser's reads run under and the indexes and
time-to-live policies the subscription record needs; the Procfile's emulator runs the
rules, and a deployment applies both files to the database.

The live service also carries the one channel the application's instances signal each
other on: one document per application, `application/signals`, with a field per kind of
change the resource package declares (`features`, `tenants`, `policy`), written by
`Signal(ctx, kind)` and followed by `Subscribe(kind, onSignal)`. This application uses
two. A feature flag flip signals the `features` kind and every instance's `FeatureSet`
rereads the table; the staff and members auths' permission engines are
constructed with `access.WithChangeSignal` over the `policy` kind (`pkg/auth/staff`,
`pkg/auth/members`), so a role, grant or membership written on one
instance reaches every instance's policy snapshot at once rather than at the engine's
next heartbeat. The `change-signal` check proves every engine the application constructs
is handed it, and the authorization and integration suites wire the in-memory
`live.NewFake()`.

The browser side is the client packages' live option: a list view or record view opting
in (`live: true` in its view configuration) with the Firestore change feed provided at
the application root, started after login and stopped at logout. It follows the client
packages' release that carries the option; until then the pages read as they do today.
The resource package's README describes the design (section 14, "Live pages").

## Checks

    go generate ./...           # regenerate; cmd/generate's test fails on drift, and each
                                # program prints its schema warnings as Warning: lines
    go test -tags skipAuth ./...  # needs podman for the emulator; the tag simulates the
                                  # portal's directory, and the portal login tests skip without it
    golangci-lint-v2 run
    cd web && bun run build && bun run lint && bun run test

A generate program is three files in its directory: `generator.go` declares the generator
(`newGenerator`), `main.go` runs it and prints every schema warning the run raised (an index a
listed tenant-scoped resource wants, a tenant resolved through a join path, an enumeration
table too large to bake), and `warnings_test.go` pins the accepted set as typed values, none
to start: a new warning fails `go test` until the schema is fixed or the warning's value is
added to the test, which records the acceptance in code. The two tests that regenerate
(`cmd/generate`'s and each program's) take the module's generation lock first
(`cmd/generate/internal/regen`): `go test` runs packages in parallel processes, and one
module is regenerated once at a time. Run with `-audit`, the program also
prints the audit pass's findings (`Audit:` lines, advisory and never printed by a plain
generation); `impulse audit` runs every program that way, and `impulse check` lists the
warning lines under its regen result.
