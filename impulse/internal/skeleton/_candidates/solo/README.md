# solo

A skeleton candidate for `impulse new`: the smallest application on the cccteam resource
stack that a person can sign in to. One site, one browser application, no tenancy, no
resources yet — the generated API carries only the session's permission digest, and the
console shows who is signed in and what they hold.

## Layout

- `main.go` serves the API and the console's built bundle.
- `pkg/config` is the configuration, as a chain of levels: core (every process), data
  (every process that opens the database), server (the served application). Each process
  constructs the level it needs — `cmd/bootstrap` and `cmd/deployment/migrate` stop at
  data — so a deploy step supplies exactly the variables its process reads.
- `app` holds the handlers: generated resource handlers plus the hand-written middleware
  and static-asset surface. `pkg/router` composes them behind the session.
- `pkg/resources` holds the resource structs the generator reads; `schema/migrations`
  the tables they describe; `schema/roles/staff.json` the role configuration the deployment
  reconciles (the Administrator role at each scope is implicit).
- `pkg/deploy` holds the database steps a deployment runs: schema migrations, then
  roles. `cmd/deployment/migrate` is the deploy step; `cmd/bootstrap` reuses it to stand
  up an emulator database and adds the development logins from `cmd/bootstrap/users.json`.
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

## Checks

    go generate ./...           # regenerate; cmd/generate's test fails on drift
    go test ./...               # needs podman for the emulator
    golangci-lint-v2 run
    cd web && bun run build && bun run lint
