# resource

The `resource` module provides permission-enforced CRUD over Spanner-backed resources:
the `resource` package is the runtime (query decoding, permission enforcement, patch
sets), and `resource/generation` is the Resource Generator that turns annotated source
structs and schema migrations into handlers, routes, request structs, and TypeScript.

- **`resource`** — the runtime apps import: decodes HTTP requests into
  `QuerySet` (reads) and `PatchSet` (mutations), checks the caller's permissions
  per-resource *and per-field*, builds SQL, and buffers Spanner mutations
  (optionally emitting change-tracking events).
- **`resource/generation`** — the Resource Generator: emits query builders, patch
  builders, HTTP handlers, chi routes, a permission `Collection`, and TypeScript
  client metadata (all in `zz_gen_*` files).

The [starport demo app](starport/) is the living example (and the generator's
regression baseline): when unsure how something is wired, find the equivalent in
starport.

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
   implementing `TxnRunner`, gated by `Execute`) per struct. The full annotation
   and tag vocabulary is tabled in the reference sections below.

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

## Using the runtime

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
  grant. This is documented as migrating to fail-closed — do not write code that
  depends on fail-open behavior.
- `perm:"Delete"` on a field is an error; a request struct may carry at most one
  non-mutating permission and may not mix mutating with non-mutating.
- `conditions:"immutable"` is implemented as "requires `Update`, and `Update` is
  never assignable" — the decoder rejects the field on update requests.
- Requesting a denied column explicitly → **403**; omitting `columns=` silently
  filters denied columns from the response. Same denial, different UX.

## Database backends

The backend is chosen by which `Client` is constructed — `NewSpannerClient` (the
only production backend), `NewPostgresClient` (**stub: every execution path
panics**; only SQL *generation* for Postgres is real), or
`NewMockClient(txnMock, readOnlyMocks, txnReadMocks)` for unit tests (with
`resource.MockIterSeq2[T]` to stub List results).

## Annotation and Struct-Tag Reference

The generator and the runtime are driven by three small vocabularies, all defined in
this document:

1. **Comment annotations** (`@resource`, `@suppress(...)`, …) written in doc comments,
   parsed by the generator.
2. **Struct tags you write** on your source structs (`spanner`, `perm`, `conditions`, …),
   read by the generator.
3. **Struct tags the generator writes** into `zz_gen` request structs (`json`, `perm`,
   `immutable:"true"`, …), read back by the `resource` runtime. You never write these
   by hand — they are listed so you can read generated code, not so you can author it.

Completeness is enforced by tests: every keyword registered in
[`resourceKeywords()`](generation/types.go) and every tag-key constant in
[`generation/annotations.go`](generation/annotations.go) and [`tags.go`](tags.go) must
appear in this document, so a new annotation cannot land undocumented.

The [starport demo app](starport/) is the living example; links below point into it.

## 1. Comment annotations

Annotations are written in the doc comment of the declaration they configure. Only
comment lines that **start with `@`** are parsed; anything after the annotation on the
same line is treated as a comment. Arguments go in parentheses, comma-separated;
annotations that take no arguments are written bare.

```go
// Ship is a starship registered with the port authority.
//
// @resource
type Ship struct { ... }
```

| Annotation | Attaches to | Arguments | Effect |
| --- | --- | --- | --- |
| `@resource` | struct in the resources package | none | Marks the struct as a resource backed by a Spanner table. The generator emits query builders, request structs, handlers, routes, and TypeScript for it. Example: [Ship](starport/pkg/resources/ships.go). |
| `@virtual` | struct in the resources package | none | A resource backed by a view instead of a base table. Because there is no table metadata, indexed fields must be declared with `index`/`uniqueindex` tags (see §2). |
| `@computed` | struct in the resources package | none | A read-only resource (List/Read only) whose rows are produced by hand-written query logic rather than a table. Primary-key fields are marked with `@primarykey`. |
| `@rpc` | struct in the rpc package | none | Declares an RPC method: the struct's fields are the request payload, and the struct must implement the hand-declared `TxnRunner` interface (`Method()` + `Execute()`). Gated by the `Execute` permission. Example: [AuthorizeLaunch](starport/pkg/rpc/authorize_launch.go). |
| `@enumerate` | named type with underlying type `string` | enum table name | Generates typed constants for the named type from the rows of an enum table. A table is an enum table when it has a `Description` column (the generator runs `SELECT DISTINCT Id, Description` against the migrated schema — avoid that column name on non-enum tables). |
| `@suppress` | `@resource`, `@computed`, or `@rpc` struct | one or more of `listHandler`, `readHandler`, `patchHandler`, `allHandlers`, `allRoutes` | Skips generating the named handlers, or all routes. Suppressing `patchHandler` also removes the resource from the consolidated patch handler. `allRoutes` is rejected on consolidated resources unless the patch handler is suppressed or the resource is excluded from consolidation. On an `@rpc` struct, any argument suppresses the generated handler. |
| `@defaultsCreateType` | `@resource` struct | type name | The generated create path calls `Defaults()` on the named type to set defaults when creating the resource. |
| `@defaultsUpdateType` | `@resource` struct | type name | As above, for updates. |
| `@validateCreateType` | `@resource` struct | type name | The generated create path calls `Validate()` on the named type to validate the incoming resource. |
| `@validateUpdateType` | `@resource` struct | type name | As above, for updates. |
| `@primarykey` | field of a `@computed` struct | none | Marks the field as (part of) the computed resource's primary key; multiple annotated fields form a compound key in declaration order. |
| `@manualAddResource` | `accesstypes.Resource` constant | `permission[, scope]` | Registers the permission on the resource in the generated Collection for a hand-written route with no generated handler. Repeatable. Scope is `global` or `domain`; omitted means the global default. |
| `@manualAddResourceSet` | `@resource` struct | comma list of `listHandler`, `readHandler`, `patchHandler`, or `allHandlers` | Declares that hand-written handlers register this resource's permission Sets for the given handler types; validated against the set of generated handlers. |
| `@permissionScope` | `@resource`, `@virtual`, `@computed`, or `@rpc` struct | `global` or `domain` | Sets the permission scope used by all of the resource's registrations. Default: `global`. |

Exactly one of `@resource`, `@virtual`, `@computed`, or `@rpc` may appear on a struct.

## 2. Struct tags you write (source structs)

| Tag | Where | Effect |
| --- | --- | --- |
| `spanner:"ColumnName"` | every field of `@resource`/`@virtual` structs | Maps the field to its Spanner column. Required — a missing tag or unknown column is a generation error, and field nullability must match the column's. |
| `perm:"List,Read,Create,Update,Delete"` | resource fields | Field-level permission requirements, enforced on the REST path only. The generator splits the list across request structs: `List`/`Read` guard reads, the rest guard mutations. An untagged field currently has no field-level check (fail-open; the resource-level grant still applies). Planned fail-closed migration: every non-primary-key field will implicitly require the endpoint's permission, at which point this tag is removed rather than reinterpreted. Primary keys take no `perm` tag: their readability follows the resource-level grant. Example: [Ship](starport/pkg/resources/ships.go). |
| `conditions:"…"` | resource fields | Comma-separated list of field conditions, see below. |
| `default_create_fn:"pkg.Func"` | resource fields | The generated create path calls the referenced function to populate the field when the request doesn't supply it. A field with a default function is not treated as required. |
| `output_only_update_fn:"pkg.Func"` | resource fields | The generated update path sets the field by calling the referenced function; implies output-only. Example: [Ship.UpdatedAt](starport/pkg/resources/ships.go) using `resource.CommitTimestampPtr`. |
| `allow_filter:"true"` | resource fields | Permits `filter` expressions on a field that isn't indexed (indexed fields are filterable automatically). Copied through to the generated request structs. |
| `index:"true"` | `@virtual` struct fields only | Declares the field indexed (filterable/sortable). Rejected on table-backed resources, which get index information from the schema. |
| `uniqueindex:"true"` | `@virtual` struct fields only | As `index`, and marks the index unique. |
| `enumerated:"ResourceName"` | `@rpc` struct fields | Ties the field to an enumerated resource (which must exist); the generated TypeScript uses the enum type for the field. |

Values recognized in a `conditions` tag:

- `immutable` — the client sets the field on create, and it can never change afterward:
  an update touching it is rejected with a 400 (the generator emits `immutable:"true"`
  into the patch request struct — you write the condition, never the emitted tag).
- `pii` — marks the field as personally identifiable. Emitted as `pii:"true"`, surfaced
  in the TypeScript metadata, and the field is rejected in URL `filter` expressions
  (filter via the POST body instead, which doesn't land in access logs).
- `input_only` — write-only: accepted on create and update but never returned (read and
  list structs get `json:"-"`, and the field is omitted from the TypeScript metadata).
  Example: [SupplyCrate.Notes](starport/pkg/resources/supply_crates.go).
- `output_only` — the server owns the value: returned to clients but never accepted from
  them (patch structs get `json:"-"`, excluding it from both create and update input).
  The value comes from the database or from `default_create_fn` /
  `output_only_update_fn` — and a field with an `output_only_update_fn` is output-only
  even without the condition. Example:
  [SupplyCrate.Barcode](starport/pkg/resources/supply_crates.go).

`immutable`, `input_only`, and `output_only` each answer the same question — what may a
REST client do with the field, and when — so they are easy to confuse. In particular,
`immutable` is not `output_only`: an immutable field is client-supplied exactly once
(e.g. an identifier chosen at creation), while an output-only field is never
client-supplied at all (e.g. a commit timestamp).

| `conditions:` | Client reads it | Client sets it on create | Client sets it on update |
| --- | --- | --- | --- |
| *(none)* | ✔ | ✔ | ✔ |
| `input_only` | ✘ | ✔ | ✔ |
| `output_only` | ✔ | ✘ | ✘ |
| `immutable` | ✔ | ✔ | ✘ (rejected with a 400) |

These conditions describe the REST contract only: what an untrusted client can read and
write over the wire. Application code calling the generated CRUD layer is not constrained
by them — it can write any field; that path is guarded by code review, not by these
rules. The `default_create_fn` / `output_only_update_fn` functions are not REST-specific,
however: they run inside the generated patch pipeline and fire for application code
exactly as for REST requests. They fill a field the caller left unset — explicitly
setting the field pre-empts them, which a REST client can never do for an output-only
field but application code can.

## 3. Struct tags the generator writes (zz_gen request structs)

Read back at runtime by the `resource` package; listed here for reading generated code.

| Tag | Meaning |
| --- | --- |
| `json:"camelName"` | Wire name of the field and the key under which its permissions are registered. `json:"-"` hides the field (input-only fields in read structs; primary keys and output-only fields in patch structs). |
| `perm:"…"` | The slice of the source `perm` list relevant to that request struct (`Read` in read structs, `List` in list structs, mutation permissions in patch structs). |
| `immutable:"true"` | From `conditions:"immutable"`; the patch decoder rejects updates to the field. |
| `index:"true"` | From the schema's indexes (or `index`/`uniqueindex` tags on virtual resources); makes the field filterable and sortable. |
| `allow_filter:"true"` | Copied from the source struct; makes an unindexed field filterable. |
| `pii:"true"` | From `conditions:"pii"`; the field is rejected in URL filter expressions. |

## 4. Reserved query parameters

List/Read requests accept exactly these query parameters — anything else is a 400, and
none of them can be used as field names in filters:

| Parameter | Meaning |
| --- | --- |
| `columns` | Comma-separated JSON field names to return; omitted means all accessible fields. |
| `filter` | Filter expression over indexed/`allow_filter` fields, e.g. `name:eq:Vanta`. Operators: `eq`, `ne`, `gt`, `lt`, `gte`, `lte`, `in`, `notin`, `isnull`, `isnotnull`. On POST query routes the filter may be sent in the body as `{"filter": "…"}` instead (required for `pii` fields), but not in both places. |
| `sort` | Comma-separated `field[:direction]` entries, e.g. `name:asc,rank:desc`; direction is `asc` (default) or `desc`. |
| `limit` | Maximum rows returned; defaults to 50. |
| `offset` | Rows to skip before returning results. |

Filter expressions combine with `,` (AND), `|` (OR), and parentheses for grouping:
`(name:eq:John|name:eq:Jane),age:gte:30`, `category:in:(books,movies)`.

## Notes and gotchas

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
  documenting it in this README **fails the build** (doc-coverage tests).
- The module pins `golang-migrate/migrate/v4` to a fork via `replace`; starport
  is a separate module deliberately excluded from release-please. Starport's
  parent-module test shells out, so run it with `-count=1` when iterating.

## The starport demo app

[starport/](starport/) is both the canonical end-to-end example (migrations →
annotated structs → generation → routes → enforcement) and the permanent
regression bed: `TestGeneratedCodeIsCommitted` fails on generator drift, and the
integration tests drive generated handlers against a real Spanner emulator with a
scriptable permission table. Copy its patterns; it is not a library.
