# sites

A skeleton candidate for `impulse new`: the multi-site application. Two sites — the
console and the portal — each its own server, hostname, router, browser application,
and generator, over one database, one policy store, and one session store. Tenancy as
data, as in the single-site candidates: the `Tenants` table is the domain universe,
tenant-scoped routes live under `/api/tenants/{tenantID}/`, and tenant existence is
concealed from logins that hold nothing there.

## Layout

- `apps/<site>/` is one site: `main.go`, `app` (its handlers), `pkg/router`, `pkg/resources`
  (the resources this site serves — a table both sites serve is declared in both, since a
  generator reads one package), `test/authz` (its generated authorization matrix), and
  `web/` (its Angular workspace, with its own `go.mod` fence).
- `pkg/config` is the shared configuration chain: core (every process), data (every
  process that opens the database), site (one served site: `PORT` and `APP_DIST`). The
  site level is declared once; each site's process supplies its own values, so the Procfile
  and each site's deployment carry them while `.envrc.template` carries the shared levels.
- `pkg/sharedresources` is what every site's browser application needs in the same shape
  (the enumerations). The shared generator emits its TypeScript into each site's web
  application and generates nothing else.
- `pkg/deploy` holds the database steps a deployment runs: schema migrations, then roles
  across the tenant roster against the union of the sites' generated collections — one
  policy store, one role configuration, one identity across sites. `cmd/deployment/migrate`
  is the deploy step; `cmd/bootstrap` reuses it for the emulator and adds the development
  tenants and logins.
- `cmd/generate` runs the three generators: console, portal, shared. `impulse check`'s
  multi-site check fails the build when a generator reads a different schema or the shared
  generator misses a site.
- `schema/migrations` is the one schema; `schema/roles/staff.json` the role configuration.
- `test/integration` serves both sites over one database and drives each the way its
  browser application does.

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

## Running it

    cp .envrc.template .envrc && direnv allow
    overmind start -l spanner,console,portal

That starts a fresh emulator, bootstraps it (schema, the `north` and `south` tenants,
roles, the logins `admin` — every tenant — `member` and `client` — north only — with
password `password`), and serves the console site on :8094 and the portal site on :8095.
The first run compiles and bootstraps before it listens; wait for "Starting Server" in
the console pane, then the portal pane.

### Browser apps

Each site has its own Angular workspace, `apps/console/web` and `apps/portal/web`, riding
the local ccc-lib checkout through yalc until `@cccteam/resource` is published. Attach
each once (the script publishes both packages to the local yalc store, links them, and
runs `bun install`):

    (cd apps/console/web && ./ccclib.sh local)
    (cd apps/portal/web && ./ccclib.sh local)

`ccclib.sh` expects the ccc-lib checkout beside this application; set `CCC_LIB` to point
elsewhere. Then `overmind start` runs everything, with `ng serve` for the console at
http://127.0.0.1:4304 and the portal at http://127.0.0.1:4305.

## Checks

    go generate ./...           # regenerate; cmd/generate's test fails on drift
    go test ./...               # needs podman for the emulator
    golangci-lint-v2 run
    (cd apps/console/web && bun run build && bun run lint)
    (cd apps/portal/web && bun run build && bun run lint)
