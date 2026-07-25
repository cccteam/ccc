# resource

The `resource` module provides permission-enforced CRUD over Spanner-backed resources:
the `resource` package is the runtime (query decoding, permission enforcement, patch
sets), and `resource/generation` is the Resource Generator that turns annotated source
structs and schema migrations into handlers, routes, request structs, and TypeScript.

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
| `perm:"List,Read,Create,Update,Delete"` | resource fields | Field-level permission requirements. The generator splits the list across request structs: `List`/`Read` guard reads, the rest guard mutations. An untagged field currently has no field-level check (fail-open; the resource-level grant still applies) — this default is migrating to fail-closed. Primary keys take no `perm` tag: their readability follows the resource-level grant. Example: [Ship](starport/pkg/resources/ships.go). |
| `conditions:"…"` | resource fields | Comma-separated list of field conditions, see below. |
| `default_create_fn:"pkg.Func"` | resource fields | The generated create path calls the referenced function to populate the field when the request doesn't supply it. A field with a default function is not treated as required. |
| `output_only_update_fn:"pkg.Func"` | resource fields | The generated update path sets the field by calling the referenced function; implies output-only. Example: [Ship.UpdatedAt](starport/pkg/resources/ships.go) using `resource.CommitTimestampPtr`. |
| `allow_filter:"true"` | resource fields | Permits `filter` expressions on a field that isn't indexed (indexed fields are filterable automatically). Copied through to the generated request structs. |
| `index:"true"` | `@virtual` struct fields only | Declares the field indexed (filterable/sortable). Rejected on table-backed resources, which get index information from the schema. |
| `uniqueindex:"true"` | `@virtual` struct fields only | As `index`, and marks the index unique. |
| `enumerated:"ResourceName"` | `@rpc` struct fields | Ties the field to an enumerated resource (which must exist); the generated TypeScript uses the enum type for the field. |

Values recognized in a `conditions` tag:

- `immutable` — the field may be set on create but is rejected on update (the generator
  emits `immutable:"true"` into the patch request struct — you write the condition, never
  the emitted tag).
- `pii` — marks the field as personally identifiable. Emitted as `pii:"true"`, surfaced
  in the TypeScript metadata, and the field is rejected in URL `filter` expressions
  (filter via the POST body instead, which doesn't land in access logs).
- `input_only` — accepted in requests but never returned (read structs get `json:"-"`).
- `output_only` — returned but not accepted in mutations (patch structs get `json:"-"`).

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
