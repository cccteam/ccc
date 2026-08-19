---
name: resource
description: Use the github.com/cccteam/ccc/resource module for anything involving CCC's permission-enforced CRUD layer over Spanner — defining @resource/@virtual/@computed/@rpc structs, spanner/perm/conditions struct tags, running or configuring the Resource Generator (zz_gen files, handlers, routes, TypeScript), decoding HTTP requests into QuerySets/PatchSets, filter expressions, change tracking, or the starport demo app. Reach for it whenever a task touches resource structs, generated handlers, patch operations, or field-level permission enforcement.
---

# resource

Permission-enforced CRUD over Spanner-backed resources. Two parts:

- **`resource`** — the runtime apps import: decodes HTTP requests into
  `QuerySet` (reads) and `PatchSet` (mutations), checks the caller's permissions
  per-resource *and per-field*, builds SQL, and buffers Spanner mutations
  (optionally emitting change-tracking events).
- **`resource/generation`** — the Resource Generator: turns annotated source
  structs plus schema migrations into query builders, patch builders, HTTP
  handlers, chi routes, a permission `Collection`, and TypeScript client metadata
  (all in `zz_gen_*` files).

**The complete annotation / struct-tag / query-parameter reference lives in this
module's [README.md](README.md)** — read it before authoring or editing resource
structs; tests enforce that every keyword is documented there, so it is always
current. The **[starport/](starport/) demo app is the living example** (and the
generator's regression baseline): when unsure how something is wired, find the
equivalent in starport.

## The workflow

1. **Write a schema migration** (`schema/migrations/NNN_Name.up.sql`). Indexes in
   the schema become filterable/sortable fields in generated request structs.
2. **Declare an annotated struct** in the resources package:

```go
// Ship is a starship registered with the port authority.
//
// @resource
type Ship struct {
    ID           ccc.UUID     `spanner:"Id"`
    RegistryCode string       `spanner:"RegistryCode" conditions:"immutable" perm:"Read,List,Create"`
    Name         string       `spanner:"Name"         perm:"Read,List,Create,Update"`
    DockingBayID ccc.NullUUID `spanner:"DockingBayId" perm:"Read,List,Create,Update"`
    CargoValue   int64        `spanner:"CargoValue"   perm:"Read,List,Create,Update"`
    UpdatedAt    *time.Time   `spanner:"UpdatedAt"    output_only_update_fn:"resource.CommitTimestampPtr" perm:"Read,List"`
}
```

   Exactly one of `@resource` (table-backed), `@virtual` (view-backed, needs
   `Subquery()` and explicit `index:"true"` tags), `@computed` (read-only,
   hand-written query logic, `@primarykey` fields), or `@rpc` (request payload
   implementing `TxnRunner`, gated by `Execute`) per struct. Other annotations
   (`@suppress`, `@enumerate`, `@validateCreateType`, `@permissionScope`, …) are
   tabled in README §1.

3. **Run the generator** (`go generate ./...`). The generator main constructs:

```go
generator, err := generation.NewResourceGenerator(ctx,
    "pkg/resources",                      // annotated structs
    []string{"file://schema/migrations"}, // migration source
    []string{/* local import paths generated code may reference */},
    generation.GenerateHandlers("app"),
    generation.GenerateRoutes("pkg/router", "api"),
    generation.WithRPC("pkg/rpc"),
    generation.WithConsolidatedHandlers("resources", true, "CrewMember"),
    generation.WithSpannerEmulatorVersion("1.5.55"),
    generation.GenerateTypescript("frontend/src/service",
        generation.GenerateMetadata(), generation.GeneratePermissions()),
)
defer generator.Close()
err = generator.Generate()
```

   **Generation requires podman or docker** — it boots a Spanner emulator and
   applies the migrations to read real column/index/nullability metadata.
   Never edit `zz_gen_*` files by hand; regenerate.

4. **Wire the app**: supply a `resource.Client`
   (`resource.NewSpannerClient(*spanner.Client)`), a
   `func(*http.Request) resource.UserPermissions` permission checker (see
   `accesstypes`), and a `*validator.Validate`; build decoders per handler via
   `resource.NewSet[Resource, Request](perms...)` →
   `resource.NewQueryDecoder`/`NewDecoder`. Starport's `app/decode.go` is the
   template — construction errors panic at startup by design.

## Runtime essentials

**Reads** — `QueryDecoder.Decode(r, userPerms)` produces a `*QuerySet[R]`; run it
with `Read`, `List`, or `BatchList`. Generated typed builders wrap this:

```go
ship, err := resources.NewShipQuery().SetID(id).Read(ctx, client)

q := resources.NewShipQuery().
    AddColumns(resources.NewShipColumns().ID().Name()).
    Where(resources.NewShipQueryClause().Name().Equal("Vanta").
        And().CargoValue().GreaterThan(100)).   // Where panics without an indexed column
    Sort(resources.NewShipSort().Name().Desc()).Limit(25)
for s, err := range q.List(ctx, client) { ... }
```

`BatchList` contract (see `queryset_example_test.go`): iterate each batch at
least once before advancing; breaking early is fine; handing batches to
goroutines while advancing is illegal.

**Mutations** — `Decoder.Decode` (or `DecodeOperation` for batch requests)
produces a `*PatchSet[R]`; `Apply` runs its own transaction, `Buffer` joins an
existing one:

```go
p, err := resources.NewShipCreatePatch()      // typed generated wrapper
p.SetName("Vanta").SetCargoValue(42)
err = p.Apply(ctx, client, resource.UserEvent(ctx))   // eventSource required if TrackChanges

err = client.ExecuteFunc(ctx, func(ctx context.Context, txn resource.ReadWriteTransaction) error {
    return p.Buffer(ctx, txn, resource.UserEvent(ctx)) // atomic multi-resource writes
})
```

**Batch patch endpoint** — the consolidated `PATCH /api/resources` handler takes
a JSON array of `{"op": "add"|"patch"|"remove", "path": "/ships[/<id>]", "value": {...}}`,
iterated via `resource.Operations(r, "/{resource}", resource.MatchPrefix())`.
Inside `ExecuteFunc`, always `resource.CloneRequest(r)` first — Spanner may
re-run the closure and the body must be re-readable.

**Query parameters** — exactly five are reserved: `columns`, `filter`, `sort`,
`limit` (defaults to 50!), `offset`. Any other query param is a 400. Filter
grammar: `,`=AND, `|`=OR, parens group, conditions like `name:eq:John`,
`age:gte:30`, `category:in:(books,movies)`, `email:isnotnull`. Fields tagged
`conditions:"pii"` are rejected in URL filters (use the POST body filter).

**Change tracking** — `Config() resource.Config` on a resource (or the generated
`DefaultConfig()`) with `TrackChanges: true` writes `DataChangeEvent` rows to a
`DataChangeEvents` table; an `eventSource` (`resource.UserEvent(ctx)`,
`ProcessEvent`, `UserProcessEvent`) then becomes mandatory on Apply/Buffer.
Metadata (including config) is cached per type per process — runtime config
changes have no effect.

## Permission model

- **Resource level is fail-closed**: every enforced operation needs the base
  permission (`List`, `Read`, `Create`, `Update`, `Delete`, `Execute`).
- **Field level is currently fail-open**: untagged fields require no field
  grant. This is documented as migrating to fail-closed — never write code that
  depends on fail-open behavior.
- `perm:"Delete"` on a field is an error; a request struct may carry at most one
  non-mutating permission and may not mix mutating with non-mutating.
- `conditions:"immutable"` is implemented as "requires `Update`, and `Update` is
  never assignable" — the decoder rejects the field on update requests.
- Requesting a denied column explicitly → **403**; omitting `columns=` silently
  filters denied columns from the response. Same denial, different UX.

## Backends

Chosen by which `Client` you construct — `NewSpannerClient` (the only production
backend), `NewPostgresClient` (**stub: every execution path panics**; only SQL
*generation* for Postgres is real), or `NewMockClient(txnMock, readOnlyMocks,
txnReadMocks)` for unit tests (with `resource.MockIterSeq2[T]` to stub List
results).

## Gotchas

- The generated handler pattern converts DB rows to response structs with a
  direct pointer cast (`(*ship)(row)`), so a request struct must be a
  field-for-field layout twin of its resource struct — order and types matter.
- A `QuerySet` cannot combine a `KeySet` with a filter/where clause (400).
- `KeySet` is immutable — `Add` returns a new value; discarding the return
  loses the key part.
- `PatchSet.Resolve` errors without a primary key; `Update`/`CreateOrUpdate`
  patches with zero fields are silent no-ops; `BufferStruct` panics for deletes.
- Decoders reject unknown JSON fields, nulls for non-nullable types, and
  immutable fields (update only); PATCH validates only the set fields
  (`StructPartial`), other methods validate the whole struct.
- Hand-built `QuerySet`s (not from a decoder) have no requestable-field
  restriction — even `json:"-"` fields become returnable. Prefer decoders at
  the HTTP boundary.
- `Config.ChangeTrackingTable` is currently inert — events always target
  `DataChangeEvents`.
- Adding any struct-tag key, query param, or generator keyword without
  documenting it in README.md **fails the build** (doc-coverage tests).
- The module pins `golang-migrate/migrate/v4` to a fork via `replace`; starport
  is a separate module deliberately excluded from release-please. Starport's
  parent-module test shells out, so run it with `-count=1` when iterating.

## starport

`starport/` is both the canonical end-to-end demo (migrations → annotated
structs → generation → routes → enforcement) and the permanent regression bed:
`TestGeneratedCodeIsCommitted` fails on generator drift, and the integration
tests drive generated handlers against a real Spanner emulator with a
scriptable permission table. Copy its patterns; don't depend on it as a library.
