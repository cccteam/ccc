# outlets

A skeleton candidate for `impulse new`: the tenant-scoped application with every outlet
kind on one site. One hostname, three outlets — the console on `/api`, a second browser
application (the portal) on `/portal/api` behind the same session handling, and a
machine REST API on `/machines` behind an API key that binds requests to a service
identity with roles like any user. Tenancy as data: the `Tenants` table is the domain universe, tenant-scoped routes live
under `/api/tenants/{tenantID}/`, and tenant existence is concealed from logins that hold
nothing there. `Announcement` is the first tenant-scoped resource — a login's tenant list
is where it holds a grant, and a grant needs a tenant-scoped resource to land on, so the
skeleton ships one. `Reading` is machines-only. The console and the portal each show who
is signed in, the tenants they can pick, and the digest for the selected tenant.

## Layout

- `main.go` serves the API and the console's built bundle.
- `pkg/config` is the configuration, as a chain of levels: core (every process), data
  (every process that opens the database), server (the served application). Each process
  constructs the level it needs — `cmd/bootstrap` and `cmd/deployment/migrate` stop at
  data — so a deploy step supplies exactly the variables its process reads.
- `app` holds the handlers: generated resource handlers for every outlet plus the
  hand-written middleware, the machines outlet's API-key authentication, and the two
  static-asset surfaces. `pkg/router` composes them: a session group per browser outlet,
  an API-key group for the machines outlet, one Angular application per browser outlet.
- `pkg/resources` holds the resource structs the generator reads: `Tenant` (the tenant
  record, a global resource), `Announcement` (tenant-scoped, on the console and the
  portal), and `Reading` (tenant-scoped, machines-only). `schema/migrations` holds the tables they describe; `schema/devseed` the
  development tenants; `schema/roles.json` the role configuration the deployment
  reconciles across the tenant roster (the Administrator role at each scope is implicit).
- `pkg/deploy` holds the database steps a deployment runs: schema migrations, then
  roles across the tenants read from the table. `cmd/deployment/migrate` is the deploy
  step; `cmd/bootstrap` reuses it to stand up an emulator database, seeds the development
  tenants first (the roster MigrateRoles reconciles across is data), and adds the
  development logins and the machines service account from `cmd/bootstrap/users.json`.
- `test/authz` is the generated authorization matrix over the generated test router;
  `test/integration` drives the served stack (real router, session, engine) end to end.
- `web/` is the Angular workspace, one project per browser application (`console`,
  `portal`); each has its own generated client (`GenerateTypescript` per outlet).
  `web/go.mod` exists only to keep Go tooling out of `node_modules`.

## Running it

    cp .envrc.template .envrc && direnv allow
    overmind start

That starts a fresh emulator, bootstraps it (schema, roles, the `admin` / `password`
login), serves the API on `$PORT`, and runs `ng serve` for the console on :4300.

## Checks

    go generate ./...           # regenerate; cmd/generate's test fails on drift
    go test ./...               # needs podman for the emulator
    golangci-lint-v2 run
    cd web && npm run build && npm run lint
