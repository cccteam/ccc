# outlets

A tenant-scoped application on the cccteam resource stack with every outlet kind on one
site. One hostname, three outlets — the console on `/api`, a second browser
application (the portal) on `/portal/api` behind the same session handling, and a
machine REST API on `/machines` behind an API key that binds requests to a service
identity with roles like any user. Tenancy as data: the `Tenants` table is the domain universe, tenant-scoped routes live
under `/api/tenants/{tenantID}/`, and tenant existence is concealed from logins that hold
nothing there. `Announcement` is the first tenant-scoped resource — a login's tenant list
is where it holds a grant, and a grant needs a tenant-scoped resource to land on, so the
application ships one. `Reading` is machines-only. The console and the portal each show who
is signed in, the tenants they can pick, and the digest for the selected tenant.

## Layout

- `main.go` serves the API and the console's built bundle.
- `pkg/config` is the configuration, as a chain of levels: core (every process), data
  (every process that opens the database), server (the served application). Each process
  constructs the level it needs — `cmd/bootstrap` and `cmd/deployment/migrate` stop at
  data — so a deploy step supplies exactly the variables its process reads.
- `pkg/auth` holds the auths, one package per population: `staff` (the console's people,
  who sign in with a password) and `members` (the portal's people, who sign in through the
  organization's directory over OpenID Connect). Each owns its session tables and cookie,
  its permission store, and its roles file, so a member and a staff login with the same
  name are two unrelated principals. The members auth leaves role membership to the
  application (`session.DisableRoleSync`): the bootstrap assigns the development members
  their roles.
- `app` holds the handlers: generated resource handlers for every outlet plus the
  hand-written middleware, the machines outlet's API-key authentication, and the two
  static-asset surfaces. `pkg/router` composes them: the staff auth's session group
  around the console, the members auth's around the portal (login redirect, directory
  callback, front-channel logout), an API-key group for the machines outlet, one Angular
  application per browser outlet.
- `pkg/resources` holds the resource structs the generator reads: `Tenant` (the tenant
  record, a global resource), `Announcement` (tenant-scoped, on the console and the
  portal), and `Reading` (tenant-scoped, machines-only). `schema/migrations` holds the tables they describe; `schema/devseed` the
  development tenants; `schema/roles/staff.json` and `schema/roles/members.json` the role
  configuration the deployment reconciles into each auth's store across the tenant roster
  (the Administrator role at each scope is implicit).
- `pkg/deploy` holds the database steps a deployment runs: schema migrations, then
  roles across the tenants read from the table. `cmd/deployment/migrate` is the deploy
  step; `cmd/bootstrap` reuses it to stand up an emulator database, seeds the development
  tenants first (the roster MigrateRoles reconciles across is data), and adds the
  development logins, the development member, and the machines service account from
  `cmd/bootstrap/users.json`.
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

## Lists and paging

A list answers one page at a time: `limit` rows, or the resource's declared default
(`@page(default: N, max: M)`, otherwise 50), positioned by a sealed cursor the server
issues in its `Link` header (`rel="next"`, `rel="prev"`), never by an offset. A resource
declares the order a sort-less list takes with `@order(Field asc, …)`; the primary key is
appended so the order is total, and a list with no declared order and no requested sort
issues no cursor. `count=true` on a first page answers the total in `Total-Count`;
`limit=all` returns every row where the resource declares no maximum. The cursors are
sealed under a key derived from the cookie key: `pkg/config` builds it with
`resource.NewCursorKey` and the App hands it to every generated decoder through
`CursorKey()`. In the browser, `@cccteam/resource` follows the relations with `page()`
and reads every row with `all()`. Computed resources answer the same query surface;
their List function may take the conditions, sort, or page it pushes down and the
generated handler applies the rest.

## Running it

    cp .envrc.template .envrc && direnv allow
    overmind start -l spanner,server

That starts a fresh emulator, bootstraps it (schema, tenants, both auths' roles, the
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
elsewhere. Then `overmind start` runs everything: the console at http://127.0.0.1:4302
and the portal at http://127.0.0.1:4303/portal/ (the sign-in button signs in as the
simulated directory's `client`).

## Checks

    go generate ./...           # regenerate; cmd/generate's test fails on drift
    go test -tags skipAuth ./...  # needs podman for the emulator; the tag simulates the
                                  # portal's directory, and the portal login tests skip without it
    golangci-lint-v2 run
    cd web && bun run build && bun run lint
