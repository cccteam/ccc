# starport

A synthetic "starport logistics" application with two roles:

- **Demo app**: a canonical, end-to-end example of building on
  `github.com/cccteam/ccc/resource` — schema migrations, annotated resource structs,
  code generation, routing, permission enforcement, a served REST application
  (`main.go`), and an Angular frontend under `gui/`.
- **Regression baseline**: the permanent test bed for the `resource` package and its
  code generators. Everything here is intentionally fictional; it exercises the full
  generator and runtime surface.

## What it covers

- **Generator regression**: the `zz_gen_*` files are committed golden output.
  `TestGeneratedCodeIsCommitted` re-runs the generator and fails if the output drifts
  from what is committed.
- **Router outlets**: a second registration surface (`@outlet`, `WithRouterOutlet`)
  served as a machine REST API under `/automation` behind API-key authentication —
  see "The automation outlet" below.
- **Permission enforcement regression**: integration tests drive the generated HTTP
  handlers against a real Spanner emulator with a scriptable permission table. Both
  generated mutation surfaces are exercised: the consolidated `PATCH /api/resources`
  handler (Ships, DockingBays, CargoManifests) and a standalone per-resource
  `PATCH /api/crew-members` handler (CrewMembers is excluded from consolidation for
  this purpose).
  - `permissions_invariant_test.go` asserts behavior of *fully tagged* resources
    (`Ships`, `CrewMembers`, RPC methods). Every non-primary-key field carries an
    explicit `perm` tag, so the fail-open/fail-closed default is never consulted.
    These assertions must hold unchanged across the planned migration of field
    permissions from fail-open to fail-closed. Primary keys follow the resource-level
    grant by rule.
  - `permissions_failopen_test.go` pins the *current* fail-open behavior of untagged
    fields (`DockingBays`, and the untagged fields of `CargoManifests`). This suite is
    expected to be deliberately rewritten when field permissions become fail closed.
  - `permissions_domain_test.go` asserts domain partitioning over the domain-scoped
    surfaces (`Berths`, `AuthorizeDocking`), served under the station segment pair
    (`WithDomainRoute("stations", "stationID")` →
    `/api/stations/{stationID}/berths`): a grant authorizes requests only in the
    station named by the URL, and grants never bleed between stations or between the
    global domain and any station. Both surfaces are fully tagged, so this suite is as
    invariant as the invariant suite. The `Berths` table is deliberately domain-blind
    (no station column): `@permissionScope(domain)` partitions permissions, not data.
    `Berth` is excluded from handler consolidation because the consolidated payload
    cannot carry a domain yet.
  - `permissions_virtual_test.go` asserts the virtual resources (`pkg/virtualresources`):
    list-only projections backed by embedded subqueries instead of table metadata.
    Their primary keys come from `@primarykey` field annotations rather than the
    schema, so the suite pins that the generated `perm:"-"` key exemption flows from
    annotations: key fields follow the resource-level grant (`ShipCargoSummaries` has
    a single annotated key, `ManifestLines` a compound one — both key fields exempt),
    while every non-key field fails closed without its own field grant. Indexed
    virtual fields (`index`/`uniqueindex` tags) are exercised through filter and sort
    parameters, and the `Subquery()` data path is asserted end to end against the
    seeded base tables.

## Requirements

Tests and generation require podman (or docker) for the Spanner emulator, matching the
requirements of the example projects.

## Regenerating

```
go generate ./...
```

The module builds against the local `resource` package via the committed `go.work`.

## Bootstrapping a demo database

`cmd/bootstrap` provisions a starport database in a running Spanner emulator: it
creates the instance and database, applies `schema/migrations`, provisions roles and
demo users through the `cccteam/access` engine, and loads the demo data from
`schema/demoseed`.

```
podman run -d --rm --name spanner -p 127.0.0.1:9010:9010 gcr.io/cloud-spanner-emulator/emulator:1.5.55
SPANNER_EMULATOR_HOST=127.0.0.1:9010 go run ./cmd/bootstrap
```

`SPANNER_EMULATOR_HOST` is required — the bootstrap refuses to run without it, so it
can never target real infrastructure. Project, instance, database, and the
access-config/data paths are overridable via `STARPORT_*` environment variables (see
`cmd/bootstrap`). The bootstrap expects a fresh emulator; restart the container to
bootstrap again.

`cmd/bootstrap/demo_access.json` defines the demo roles (provisioned via
`access.MigrateRoles` against `router.Collection()`, which validates every grant —
unknown resources, unregistered permissions, and Update grants on immutable fields
fail the bootstrap) and the demo users: their login credentials (seeded as session
users through the session library, so passwords are stored as real Argon2 hashes; the
plaintext in the JSON is deliberate — fictional credentials for an emulator-only demo)
and their per-domain role assignments. `pkg/stations` is the demo's tenancy source:
the stations that serve as permission domains for the domain-scoped resources, passed
to `MigrateRoles` as the domain universe. The access engine's policy tables and the
session library's tables ride the schema migrations (`000008_AccessTables`,
`000009_Sessions`, `000010_SessionUsers`), copied from each library's canonical DDL.
`app.NewAccessUserPermissions` adapts the access engine to the resource package's
`UserPermissions` seam for a served app; the integration tests keep injecting
scriptable fakes instead.

`TestBootstrap` runs the full bootstrap against an emulator and asserts the
provisioned access state end to end, including that the demo user's station role
authorizes only in the assigned station and that global and station grants never
bleed into each other.

## Running the application

The starport serves a full web application: the generated resource API behind
password-auth sessions (`cccteam/session`), plus the Angular frontend. Like the
bootstrap, it refuses to start without `SPANNER_EMULATOR_HOST` — the demo never
targets real infrastructure.

The quickest way is the committed `Procfile` with
[overmind](https://github.com/DarthSim/overmind) (after a one-time
`cd gui && npm install`):

```
overmind start
```

It starts a fresh emulator, waits for it, bootstraps the database, serves the app,
and runs `ng serve` — browse http://127.0.0.1:4200 (the frontend dev server, `/api`
proxied to the Go server) and sign in with `demo` / `starport`. Every start
re-bootstraps from scratch because the emulator container is ephemeral.

The equivalent manual steps, serving the production build from the Go server instead
of `ng serve`:

```
podman run -d --rm --name spanner -p 127.0.0.1:9010:9010 gcr.io/cloud-spanner-emulator/emulator:1.5.55
SPANNER_EMULATOR_HOST=127.0.0.1:9010 go run ./cmd/bootstrap
(cd gui && npm install && npm run build)
SPANNER_EMULATOR_HOST=127.0.0.1:9010 go run .
```

Then open http://localhost:8080 and sign in with the seeded demo credentials
(`demo` / `starport`). Configuration lives in `pkg/config` and is entirely optional:
`STARPORT_HOST`/`STARPORT_PORT` for the listen address, `STARPORT_SESSION_TIMEOUT`,
`STARPORT_COOKIE_KEY` (ephemeral when unset), `STARPORT_GUI_DIST` (defaults to
`gui/dist`), `STARPORT_AUTOMATION_API_KEY` (the automation outlet's bearer key;
ephemeral — and therefore unreachable — when unset), and the same `STARPORT_SPANNER_*`
variables the bootstrap uses.

For frontend development, `cd gui && npm start` serves the Angular app with `/api`
proxied to the Go server (see `gui/README.md`).

The served router (`router.New`) wraps the generated routes with session validation
and adds three hand-written endpoints: `POST /api/user/login` (the session library's
password login), `GET /api/user/session-data` (the session user's permission
collection from the access engine, consumed by the frontend's permission gating), and
`GET /api/station-directory` (the demo tenancy list). The bare
`router.NewTestRouter`/`app.New` seam the integration tests exercise is unchanged.

### The automation outlet

The starport also demonstrates router outlets: resources annotated
`@outlet(default, automation)` (`Ships`, `GantryCranes`, `ShipCargoSummaries`, and the
`AuthorizeLaunch` RPC method) are additionally served as a machine REST API under
`/automation`, declared by `generation.WithRouterOutlet("automation", "automation")`.
The router composes the generated `generatedAutomationRoutes` behind API-key
authentication (`Authorization: Bearer $STARPORT_AUTOMATION_API_KEY`) instead of the
browser's session and XSRF guards; a valid key binds the request to the `automation`
service account, which `cmd/bootstrap/demo_access.json` seeds with roles but no login,
so the machine surface runs the same fail-closed permission checks as the browser.
Consolidated mutations get a per-outlet dispatcher: `PATCH /automation/resources`
bundles exactly the consolidated resources on the outlet (Ships, GantryCranes). The
generated router tests cover the outlet's dispatch and prove isolation: a route's path
under an outlet its resource is not attached to must 404.

```
STARPORT_AUTOMATION_API_KEY=dev-automation-key SPANNER_EMULATOR_HOST=127.0.0.1:9010 go run .
curl -H "Authorization: Bearer dev-automation-key" http://localhost:8080/automation/ships
```
