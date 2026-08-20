# starport

A synthetic "starport logistics" application with two roles:

- **Demo app**: a canonical, end-to-end example of building on
  `github.com/cccteam/ccc/resource` — schema migrations, annotated resource structs,
  code generation, routing, and permission enforcement. An Angular frontend is planned
  under `frontend/` for a full-stack integration demonstration.
- **Regression baseline**: the permanent test bed for the `resource` package and its
  code generators. Everything here is intentionally fictional; it exercises the full
  generator and runtime surface.

## What it covers

- **Generator regression**: the `zz_gen_*` files are committed golden output.
  `TestGeneratedCodeIsCommitted` re-runs the generator and fails if the output drifts
  from what is committed.
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

`config/demo_access.json` defines the demo roles (provisioned via `access.MigrateRoles`
against `router.Collection()`, which validates every grant — unknown resources,
unregistered permissions, and Update grants on immutable fields fail the bootstrap)
and the demo users' per-domain role assignments. `pkg/stations` is the demo's tenancy
source (`access.Domains`): the stations that serve as permission domains for the
domain-scoped resources. `app.NewAccessUserPermissions` adapts the access engine to
the resource package's `UserPermissions` seam for a served app; the integration tests
keep injecting scriptable fakes instead.

`TestBootstrap` runs the full bootstrap against an emulator and asserts the
provisioned access state end to end, including that the demo user's station role
authorizes only in the assigned station and that global and station grants never
bleed into each other.
