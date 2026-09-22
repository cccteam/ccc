# solo

An application on the cccteam resource stack: the smallest one a person can sign in to.
One site, one browser application, no tenancy, no resources yet — the generated API
carries only the session's permission digest, and the console shows who is signed in and
what they hold.

## Layout

- `main.go` serves the API and the console's built bundle.
- `pkg/config` is the configuration, as a chain of levels: core (every process), data
  (every process that opens the database), site (the served site). Each process
  constructs the level it needs — `cmd/bootstrap` and `cmd/deployment/migrate` stop at
  data — so a deploy step supplies exactly the variables its process reads.
- `app` holds the handlers: generated resource handlers plus the hand-written middleware
  and static-asset surface. `pkg/router` composes them behind the session.
- `pkg/resources` holds the resource structs the generator reads; `schema/migrations`
  the tables they describe; `schema/roles/staff.json` the role configuration the deployment
  reconciles. It authors `Administrator_Global`, the development login's role, with no
  grants yet: a new resource stays invisible to every login until a role in this file is
  granted it.
- `pkg/deploy` holds the database steps a deployment runs: schema migrations, then roles.
  `cmd/deployment/migrate` is the deploy step; `cmd/bootstrap` reuses it to stand up an
  emulator database and adds the development logins from `cmd/bootstrap/users.json`. Its
  test runs each roles file through the deploy-time validation (`access.ValidateRoles`)
  and pins the warnings the deploy would print as typed values, none expected: a warning
  is accepted by pinning it there, or the role is fixed.
- `test/authz` is the generated authorization matrix over the generated test router;
  `test/integration` drives the served stack (real router, session, engine) end to end.
- `web/` is the Angular workspace, one project per browser application (`console`).
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

## Lists and paging

A list answers one page at a time: `limit` rows, or the resource's declared default
(`@page(default: N, max: M)`, otherwise 50), positioned by a sealed cursor the server
issues in its `Link` header (`rel="next"`, `rel="prev"`), never by an offset. A resource
declares the order a sort-less list takes with `@order(Field asc, …)`; the primary key is
appended so the order is total. Every paged request needs an order from somewhere: a
listed resource declares its `@order` or every page of it names a `sort`, and a request
with neither is refused with a 400 unless it asks `limit=all`, the whole list, unsorted.
Declare it on the column a person reads first; on a tenant-scoped resource, an index
leading with the tenant column and then the order columns serves the pages (the generator
warns where one is missing). `count=true` on a first page answers the total in
`Total-Count`; `limit=all` returns every row where the resource declares no maximum. The
cursors are sealed under a key derived from the cookie key: `pkg/config` builds it with
`resource.NewCursorKey` and the App hands it to every generated decoder through
`CursorKey()`. In the browser, `@cccteam/resource` follows the relations with `page()` and
reads every row with `all()`. Computed resources answer the same query surface; their List
function may take the conditions, sort, or page it pushes down and the generated handler
applies the rest.

## Running it

    cp .envrc.template .envrc && direnv allow
    overmind start -l spanner,server

That starts a fresh emulator, bootstraps it (schema, roles, the `admin` / `password`
login), and serves the API on `$PORT` (:8090). The first run compiles and bootstraps
before it listens; wait for "Starting Server" in the server pane.

### Browser apps

The console is an Angular workspace under `web/` that rides the local ccc-lib checkout
through yalc until `@cccteam/resource` is published. Attach it once (the script publishes
both packages to the local yalc store, links them, and runs `bun install`):

    (cd web && ./ccclib.sh local)

`ccclib.sh` expects the ccc-lib checkout beside this application; set `CCC_LIB` to point
elsewhere. Then `overmind start` runs everything, with `ng serve` for the console at
http://127.0.0.1:4300 proxying `/api` to the server.

After a change in ccc-lib, push the rebuilt packages, restart the dev server, and reload the
page:

    (cd web && ./ccclib.sh push)
    overmind restart console

The dev server does not watch `node_modules`, so the restart is what picks the new build up.
The two packages are bundled with the application code rather than prebundled by Vite (the
`prebundle` exclusion in `angular.json`), so a plain reload shows the new build in any
browser profile: nothing is held behind an immutable URL, and no cache needs clearing.

The console's component specs run on Angular's unit-test builder (`@angular/build:unit-test`)
with Vitest under jsdom in Node: no browser, no Karma. `bun run test` runs them once, the
form the Checks section and CI use; `bun ng test console` watches. The specs beside the
skeleton's components are the pattern for the application's own: the dashboard renders a
permission digest over a scripted client from `@cccteam/resource-angular/testing`
(`provideResourceTesting` puts the generated client on a transport that records every
request and answers from the test), and the login page, header, top bar, footer, and shell
have creation specs with the same providers.

## Checks

    go generate ./...           # regenerate; cmd/generate's test fails on drift, and each
                                # program prints its schema warnings as Warning: lines
    go test ./...               # needs podman for the emulator
    golangci-lint-v2 run
    cd web && bun run build && bun run lint && bun run test

A generate program is three files in its directory: `generator.go` declares the generator
(`newGenerator`), `main.go` runs it and prints every schema warning the run raised (an index a
listed tenant-scoped resource wants, a tenant resolved through a join path, an enumeration
table too large to bake), and `warnings_test.go` pins the accepted set as typed values, none
to start: a new warning fails `go test` until the schema is fixed or the warning's value is
added to the test, which records the acceptance in code. Run with `-audit`, the program also
prints the audit pass's findings (`Audit:` lines, advisory and never printed by a plain
generation); `impulse audit` runs every program that way, and `impulse check` lists the
warning lines under its regen result.
