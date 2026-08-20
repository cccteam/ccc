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
