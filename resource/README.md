# resource

The `resource` module provides permission-enforced CRUD over Spanner-backed resources:
the `resource` package is the runtime (query decoding, permission enforcement, patch
sets), and `resource/generation` is the Resource Generator that turns annotated source
structs and schema migrations into handlers, routes, request structs, and TypeScript —
including `zz_gen_api.ts`, the typed client surface the framework-neutral
[`@cccteam/resource`](https://github.com/cccteam/ccc-lib/tree/master/projects/resource)
runtime interprets (see the [lodestar demo](lodestar/README.md)).

## Annotation and Struct-Tag Reference

The generator and the runtime are driven by three small vocabularies, all defined in
this document:

1. **Comment annotations** (`@resource`, `@suppress(...)`, …) written in doc comments,
   parsed by the generator.
2. **Struct tags you write** on your source structs (`spanner`, `conditions`, …),
   read by the generator.
3. **Struct tags the generator writes** into `zz_gen` request structs (`json`, `perm`,
   `immutable:"true"`, …), read back by the `resource` runtime. You never write these
   by hand — they are listed so you can read generated code, not so you can author it.

Completeness is enforced by tests: every keyword registered in
[`resourceKeywords()`](generation/types.go) and every tag-key constant in
[`generation/annotations.go`](generation/annotations.go) and [`tags.go`](tags.go) must
appear in this document, so a new annotation cannot land undocumented.

The [lodestar demo app](lodestar/) is the living example; links below point into it.

## 1. Comment annotations

Annotations are written in the doc comment of the declaration they configure. Only
comment lines that **start with `@`** are parsed; anything after the annotation on the
same line is treated as a comment. Arguments go in parentheses, comma-separated;
annotations that take no arguments are written bare. The rule is positional: a prose
line that happens to begin with an `@` word is parsed as an annotation too, and one
that repeats a keyword is refused as used twice, so start such a line with another
word.

```go
// Ship is a starship registered with the port authority.
//
// @resource
type Ship struct { ... }
```

| Annotation | Attaches to | Arguments | Effect |
| --- | --- | --- | --- |
| `@resource` | struct in the resources package | none | Marks the struct as a resource backed by a Spanner table. The generator emits query builders, request structs, handlers, routes, and TypeScript for it. Example: [Ship](lodestar/pkg/resources/ships.go). |
| `@virtual` | struct in the resources package | none | A resource backed by a view instead of a base table. Because there is no table metadata, indexed fields must be declared with `index`/`uniqueindex` tags (see §2), and the read identity (if any) with `@primarykey`. Example: [OpenMissionsBySquadron](lodestar/pkg/virtualresources/open_missions_by_squadrons.go). |
| `@computed` | struct in the resources package | none | A read-only resource (List/Read only) whose rows are produced by hand-written query logic rather than a table. Primary-key fields are marked with `@primarykey`. Its permission checks run at decode time, where no row exists, so the generated Collection marks it computed and deploy-time role migration rejects grants on it whose conditions are not row-free (the same rule Execute grants follow). From the browser a computed resource answers a list query exactly as a table does, with no code in its List function: the generated handler applies the filter, then the sort, then the page (cursor, Link header, count) over the rows the function yields. A field is filterable when it carries `allow_filter`, as on a table; `index` and `uniqueindex` are refused (there is no database index), and `allow_filter` on a field the in-memory evaluator cannot compare fails generation with the field named. A List function that runs its own queries may take pieces of the work to push down — `qSet.Filter().Take(fields…)` returns the conditions on those fields and leaves the handler the rest (defined on an AND-only filter; an OR takes nothing), `qSet.TakeSort()` returns the total order and marks it the body's, and `qSet.TakePage()` returns the page bounds only when paging in the body is correct (nothing left to filter, the sort taken, no count asked) and otherwise answers false so the handler pages. Taking is the only way to opt out, so a body can never make the result wrong. Example: [SectorHazardBoard](lodestar/pkg/computedresources/sector_hazard_boards.go). |
| `@rpc` | struct in the rpc package | none | Declares an RPC method: the struct's fields are the request payload, and the struct declares `Execute` in one of two forms, read off its signature — `Execute(ctx context.Context, txn resource.ReadWriteTransaction, client *Client) error` runs inside the generated handler's read-write transaction; `Execute(ctx context.Context, client resource.Client, rpcClient *Client) error` runs outside one. The third parameter is the application's own RPC client type. `Execute` returns `error` alone, and the handler answers with an empty 200, or `(Result, error)` with `Result` a struct type or a pointer to one, and the handler answers with the result after the transaction commits, mirrored with generated wire names (a nil pointer answers empty). A method answers with identifiers and outcomes, never rows: a `@resource` or `@computed` struct in the result position is refused, since rows are read through their own routes, where permission masking applies. A struct with no `Execute`, or one of another shape, fails generation naming the struct; no interface declaration takes part. Example of an answering method: [InspectShip](lodestar/pkg/rpc/inspect_ship.go). The generated frame stamps the caller into the context before `Execute` (`resource.CallerFrom`), so a body may arm a typed patch or query with `.Enforce(caller)` and run the pipeline the resource routes run, or ask `caller.Check` for a decision to branch on; without either the body is trusted, the default. A transaction-form method honors `X-Dry-Run: true`: the whole frame and the body run and the transaction rolls back, refusing exactly as the real call would and answering an empty 200 where it would have succeeded; a client-form method refuses the header with 400. Examples: [HoldMission](lodestar/pkg/rpc/hold_mission.go) (armed), [CompleteMission](lodestar/pkg/rpc/complete_mission.go) (decision as data). Gated by the `Execute` permission. The struct lives in a file named after it in snake case (`claim_mission.go`), as every generated kind does; a name ending in `Test` carries the `_rpc` marker instead (`start_flight_test_rpc.go`), so neither it nor the `zz_gen_` files generated beside it are Go `_test.go` files. Example: [ClaimMission](lodestar/pkg/rpc/claim_mission.go). |
| `@enumerate` | named type with underlying type `string` | enum table name | Generates typed constants for the named type from the rows of an enum table. A table is an enum table when it has a `Description` column (the generator runs `SELECT DISTINCT Id, Description` against the migrated schema — avoid that column name on non-enum tables). |
| `@enumerate` | field of an `@rpc` struct | resource name | Ties the request field to an enumerated resource, which must exist: the generated TypeScript renders the field as that resource's picker. Only a request field declares this; a table-backed field's enumeration is inferred from the schema's foreign key. Example: [LaunchMission.ShipID](lodestar/pkg/rpc/launch_mission.go). |
| `@suppress` | `@resource`, `@computed`, or `@rpc` struct | one or more of `listHandler`, `readHandler`, `patchHandler`, `allHandlers`, `allRoutes` | Skips generating the named handlers, or all routes. Suppressing `patchHandler` also removes the resource from the consolidated patch handler. `allRoutes` is rejected on consolidated resources unless the patch handler is suppressed or the resource is excluded from consolidation. On an `@rpc` struct, any argument suppresses the generated handler. |
| `@defaultsCreateType` | `@resource` struct | type name | The generated create path calls `Defaults()` on the named type to set defaults when creating the resource. |
| `@defaultsUpdateType` | `@resource` struct | type name | As above, for updates. |
| `@validateCreateType` | `@resource` struct | type name | The generated create path calls `Validate()` on the named type to validate the incoming resource. |
| `@validateUpdateType` | `@resource` struct | type name | As above, for updates. |
| `@primarykey` | field of a `@computed` or `@virtual` struct | none | Marks the field as (part of) the resource's primary key; multiple annotated fields form a compound key in declaration order. Primary-key fields are exempt from field-level permission enforcement (their readability follows the resource-level grant). Rejected on table-backed `@resource` structs, whose keys come from the schema. Compound example: [SectorHazardBoard](lodestar/pkg/computedresources/sector_hazard_boards.go). |
| `@attribute` | field of a `@resource` struct | `name[, via: Remote.Segments]` | Declares an attribute binding: the vocabulary name grant conditions reference for this row attribute (ABAC). Bare, the annotated column itself carries the attribute; with `via:`, the binding is a join path leaving through the annotated foreign key — `via:` carries only the remote segments, Go field names on each successive struct, dotted for multi-hop (`via: StationId.Sector`), and every hop must resolve many-to-one through a real foreign key or generation fails. Names follow the condition language's identifier rules (`[A-Za-z_][A-Za-z0-9_]*`); `subject`, `now`, and `new` are reserved; a name is declared once per resource. |
| `@domain` | field of a `@resource` struct (bare form also on a `@virtual` struct) | none, or `via: Remote.Segments` | Declares the structural tenancy binding: how every row of a domain-scoped resource resolves to its tenant. Bare on the tenant-key column itself, or `via:` a foreign-key path to it (same grammar as `@attribute`). **Mandatory on every domain-scoped table-backed or virtual resource** — missing is a generation error, and so is declaring it on a resource that is not domain-scoped: global scope is the explicit opt-out (design plan §06). On a virtual resource only the bare form is valid, naming a column the view's projection carries. A bare `@domain` derives the tenant column's runtime behavior — never stated twice: the column decodes output-only (create and update closed, so the wire cannot express a tenant write or re-tenant a row) and the framework stamps it from the request's domain partition on create, so the checked domain and the written domain are the same value by construction; restating behavior through `conditions` or `default_create_fn` tags is rejected, and Create/Update on the column are ungrantable while reads stay grantable. Deliberately not an `@attribute` — it is consumed by tenancy injection and never referencable from grant conditions. At most one per resource. It also tenant-filters the subject subqueries anchored on the resource (see the subject rows). The `via:` path is resolved through foreign-key metadata, not through the remote resource's own bindings — a domain-scoped parent table does **not** transitively supply tenancy to resources referencing it; each resource declares its own `@domain`. |
| `@state` | field of a `@resource` struct | `default: <value>` | Marks the resource's state column (ABAC design plan §09). The column must be a foreign key to its state enum table (the ordinary Id/Description convention — the FK identifies the table, nothing is declared on it), and the declared default must be one of that table's values. The marker derives the field's behavior — never stated twice: the field decodes output-only (create and update closed, so the wire cannot express a state write; transitions happen inside RPC bodies), Create/Update on it are ungrantable, reads stay grantable, and the generated create patch applies the declared initial state on insert (never a database DEFAULT). State values change only by migration: a mutation permission registered against the state enum table is a generation error, while Read stays grantable. |
| `@stateRoot` | FK field of a `@resource` struct | `RootStructName` | Declares workflow membership (ABAC design plan §09) on the foreign key anchoring the member — the field is the hop, so only the workflow root's struct name is spelled. Each member declares only its immediate hop; chains compose through other members and must reach the root (a cycle or a hop outside the workflow fails generation), every hop is many-to-one through a real foreign key, and member and root permission scopes must match. The generator synthesizes the uniform `state` attribute binding on the root (a column binding on its `@state` field) and every member (a join path through the chain), so one condition text reads identically across the workflow, and emits the assembled graph as a committed, drift-tested `zz_gen_workflow_<root>.dot` file per stateful root: the whole tree — root and members as solid nodes with one edge per hop labeled with the anchoring FK field, dashed context nodes for every FK reference leaving the tree (the tenant record included), a state cluster with every value and the default marked, one labeled edge per declared `@transition`, and a legend; undeclared state changes stay RPC-body business rules the framework cannot see. The TypeScript resource metadata carries the same facts (root, members with hops, states, default, transitions) as a `Workflows` constant, so a frontend can render the graph itself. A resource belongs to at most one workflow. |
| `@transition` | `@rpc` struct | `RootStructName, from: a[, b…], to: c` | Declares the RPC method as a workflow state transition (ABAC design plan §09): the method moves rows of the named root resource along one edge. The root must carry `@state`, every `from`/`to` value must be a value of its state enum table, method and root permission scopes must match, and the struct's `Execute` must be the transaction form, carrying exactly one `@target` field. The generated handler owns the mechanical frame inside the transaction it already runs: before the body it locates the target row within the tenancy predicate (absent or cross-tenant is NotFound) and verifies the pre-image state is in the `from` set, then evaluates any row-referencing condition the caller's Execute grant carries against the same located row — either refusal is one uniform Forbidden naming the method and the row, so the wire never says whether the state or the condition said no (§12); after the body returns without error it stamps the `to` state as the last mutation. The body never reads or writes the state field — it carries only the edge's business effect. Who may run the method stays its Execute grant (grants-only, §09). The declared edge travels in the generated Collection, draws labeled edges in the workflow DOT file, rides the TypeScript method metadata, and answers `capabilities=Execute` per row. Example: [LaunchMission](lodestar/pkg/rpc/launch_mission.go). |
| `@target` | field of an `@rpc` struct | none, or `RootStructName` | Marks the field carrying the target row's key — exactly one per method, its type matching the target's single-column primary key. With `@transition` it is bare (the declared root is the target); without one, `@target(Root)` names the row resource directly and the method gets the plain located-row form (ABAC design plan §12): the generated handler locates the row inside its transaction (absent or cross-tenant is NotFound) and evaluates any row-referencing condition on the caller's Execute grant against it, with no state check and no stamp. Either way, a targeted method's Execute grants may carry row conditions — `access.MigrateRoles` validates them against the target resource's binding vocabulary — and the method joins the target resource's per-row `capabilities=Execute` answer. Requires the transaction form of `Execute`; method and target permission scopes must match. A domain-scoped target resolves tenancy through its `@domain` binding, either form: a bare tenant column is read off the located row, a join-path binding is verified with one query in the same transaction — absent and cross-tenant rows answer the same NotFound either way. Example: [HailShip](lodestar/pkg/rpc/hail_ship.go). |
| `@answers` | `@rpc` struct | `200, 409` | Declares the statuses the method may answer with; the result type carries `HTTPStatus() int` and chooses one per response. Allowed: `200`, `201`, `202`, `204`, and any 4xx except `401`, `403`, and `404`, which stay the frame's own refusals; at least one must be a 2xx. A 4xx answer is the method's refusal with its typed body: in the transaction form the transaction rolls back first, so nothing the body armed commits. `204` writes no body and requires a pointer result returned nil, or an answerless method, whose only permitted declaration is `@answers(204)`. A result declaring `HTTPStatus()` without `@answers`, or the reverse, is a generation error; an undeclared status at runtime answers 500. The TypeScript client resolves `{ status, result }` for a method with declared statuses and still throws on every undeclared 4xx. Example: [CompleteMission](lodestar/pkg/rpc/complete_mission.go). |
| `@upload` | `@rpc` struct | `max: 5MB` | Declares the method as a multipart upload. Its `Execute` takes `resource.Files` third — `Execute(ctx, txn resource.ReadWriteTransaction, files resource.Files, client *Client)`, the transaction form only, since the transaction is what claims the files — and the declaration and the signature go together (either alone is a generation error). The request is `multipart/form-data`: one part named `request` first, carrying the JSON the method's decoder reads exactly as for a JSON RPC, then one or more parts named `file`. `max` (a byte count or `KB`/`MB`/`GB`, 1024-based) bounds the whole body; over it is a 413 naming the maximum, no `file` part a 400, a body that is not multipart a 415. The frame streams each file to the application's `UploadStore` (asserted on the application as `UploadStore() resource.UploadStore` while any method uploads) under a key it minted, runs the body with the `Files`, promotes the keys after the transaction commits, and discards them on any failure before it; the body records the keys wherever its schema wants them, and reading a file back is the application's own route. A dry run streams nothing: the `Files` describe the parts with empty keys. The TypeScript handle gains `upload(body, files)`, which refuses locally over the maximum. Example: [AttachMissionDocument](lodestar/pkg/rpc/attach_mission_document.go). |
| `@subjectSet` | user-id field of a `@resource` struct | `name, value: Field` | Declares subject-side set vocabulary: `subject.<name>` in grant conditions is the set of `value:` values on this table's rows whose annotated column matches the requesting user (`crew IN subject.crews`). The annotation designates the user-id column — no separate marker — and is repeatable per anchor; `value:` names the sibling Go field the set yields, dotted to continue through foreign-key hops with the same many-to-one validation as `via:`. **Tenancy:** the rendered subject subquery is tenant-filtered by the anchor resource's own `@domain` binding, so a domain-scoped anchor must declare one — generation rejects it otherwise, because without it `subject.<name>` matches the user's rows from every tenant (a membership held at tenant B would satisfy conditions evaluated at tenant A). A global-scoped anchor is the deliberately shared pattern — a certification earned once applies everywhere — and stays unfiltered. Note the anchor's own binding is what counts: tenancy never arrives transitively from a domain-scoped parent table (see `@domain`). Example: [SquadronMembership](lodestar/pkg/resources/squadron_memberships.go). |
| `@subjectValue` | user-id field of a `@resource` struct | `name, value: Field` | Declares subject-side scalar vocabulary for threshold comparisons (`amount <= subject.approvalLimit`). Same grammar — and the same tenancy rule — as `@subjectSet`, valid only where the annotated user-id column is the primary key or unique-indexed, so the database enforces exactly one row per user. |
| `@manualAddResource` | `accesstypes.Resource` constant | `permission[, scope]` | Registers the permission on the resource in the generated Collection for a hand-written route with no generated handler. Repeatable. Scope is `global` or `domain`; omitted means the global default. An `@outlet` annotation on the same constant names the outlets the hand-written route is mounted under, so an outlet-filtered TypeScript target (`ForOutlet`) carries the registration only when it names that outlet; omitted means the default outlet. The constant's value is the resource name and must not contain `:` (reserved for access-defined markers like `accesstypes.GlobalResource`); generation rejects it. The registration reaches the TypeScript constants like a generated one: an `Execute` registration joins `Methods`, any other permission joins `Resources`. |
| `@manualAddResourceSet` | `@resource` struct | comma list of `listHandler`, `readHandler`, `patchHandler`, or `allHandlers` | Declares that hand-written handlers register this resource's permission Sets for the given handler types; validated against the set of generated handlers. |
| `@outlet` | `@resource`, `@virtual`, `@computed`, or `@rpc` struct; `accesstypes.Resource` constant carrying `@manualAddResource` | comma list of outlet names | Names the router outlets the struct's routes — or the constant's hand-written route — are registered under. Outlets are independent registration surfaces with their own route prefixes — the generator emits a `Generated<Name>Handlers` interface and `generated<Name>Routes` function per outlet, so the application composes different authentication and middleware around each (a browser app on one, a machine REST API on another). The default outlet is declared by `generation.GenerateRoutes` and carries the reserved name `default`; additional outlets are declared with `generation.WithRouterOutlet(name, routePrefix)`, and referencing an undeclared name is a generation error. An outlet serving browser sessions declares it (`WithRouterOutlet(name, prefix, ServesSessions())`): the generated router registers the permission-digest and user-domains routes under its prefix, and only a session-serving outlet may be the target of a `GenerateTypescript` call (`ForOutlet(name)`), which filters every emitted TypeScript file to that outlet's members — a client for a session-less outlet would have no permission channels and fail closed on every page, so it is a generation error. Without the annotation a struct is on the default outlet only; naming outlets replaces that default, so `@outlet(default, automation)` serves both while `@outlet(automation)` serves the automation outlet only. Consolidated resources get one consolidated patch dispatcher per outlet (`PatchResources`, `Patch<Name>Resources`), each bundling exactly that outlet's members. The generated router tests cover every outlet's routes and additionally prove isolation: a route's path under an outlet it is not attached to must 404. Example: [Consignment](lodestar/pkg/resources/consignments.go). |
| `@permissionScope` | `@resource`, `@virtual`, `@computed`, or `@rpc` struct | `global` or `domain` | Sets the permission scope used by all of the resource's registrations. Default: `global`. It also selects the domain the generated handlers evaluate permissions in: global-scoped handlers pass `accesstypes.GlobalDomain`, while domain-scoped handlers read it from a required `/domains/{domain}/` route segment pair between the route prefix and the resource path (pair-style, so domain values can never collide with resource or method route names). Both names are customizable via the `generation.WithDomainRoute` option, e.g. `WithDomainRoute("organizations", "organizationID")` → `/organizations/{organizationID}/`. Domain-scoped handlers validate the URL's domain against the application's `DomainExists` seam (a `Config` sibling of `UserPermissions`) and return 404 for unknown domains before decoding — the application owns its tenant list. With `generation.WithConcealedDomains()` that seam becomes `DomainVisible(ctx, user, domain)` instead: a domain where the requesting user holds zero grants answers identically to a domain that does not exist (404 on routes, 400 in consolidated op paths), so refusals never confirm a tenant; any grant in the domain restores ordinary 403s (`access.Client.UserHasGrants` answers the foothold question from the in-memory snapshot, so the seam stays legal inside the consolidated mutation transaction). Tenant identifiers must be a single URL-safe path segment and must never contain `:` — that character is reserved for access-defined markers (`accesstypes.GlobalDomain` is `access:global`), and the generated guard structurally rejects any `:`-bearing domain value before `DomainExists` is even consulted, so a misconfigured tenant list can never route a permission check into the global partition. In the consolidated patch handler, a domain-scoped resource's operations carry the domain in the path exactly as the URL grammar does (`{"op":"patch","path":"/stations/station-alpha/berths/…"}`); global operations stay domainless, an unknown domain in an operation path is a 400, and cross-domain batches are legal (each operation is checked in its own partition; the batch is one transaction). The tenant-record pattern — a global resource named like the domain segment, so `/api/stations` lists the tenants while `/api/stations/{stationID}/…` serves tenant-scoped routes — is supported with two validated requirements: the resource must have a single primary key (its operations stay at path depth ≤ 2, domain descents at depth ≥ 3), and its read-route parameter must equal the domain route parameter. Example: [Sector](lodestar/pkg/resources/sectors.go). |
| `@page` | `@resource`, `@virtual`, or `@computed` struct | `default: N` and/or `max: M` | Declares the list's page sizes: the page a request without `limit` receives, and the largest page a request may ask for. Both are positive integers; the default never exceeds the maximum, and a maximum alone must be at least the generator-wide default of 50. Undeclared, the list serves pages of 50 with no maximum. A request over the maximum is refused with a 400 naming it, never clamped, and a resource with a maximum refuses `limit=all`. The generated TypeScript descriptor carries both numbers. Example: [Mission](lodestar/pkg/resources/missions.go). |
| `@order` | `@resource`, `@virtual`, or `@computed` struct | comma list of `Field [asc\|desc]` | Declares the order a list takes when the request carries no `sort`, naming Go fields of the struct; the direction defaults to `asc`. The primary key is appended at runtime so the order is total, and a request's `sort` replaces the declared order for that request. A resource that declares nothing lists by primary key. On a computed resource only a leaf field may be named (a nested field is opaque). Example: [Mission](lodestar/pkg/resources/missions.go). |

Exactly one of `@resource`, `@virtual`, `@computed`, or `@rpc` may appear on a struct. The generator refuses a struct carrying more than one, naming the kinds it found.

## 2. Struct tags you write (source structs)

Field-level permissions are structural, not annotated: every non-primary-key field of a
resource implicitly requires the endpoint's permission on `Resource.field` (`List` and
`Read` for reads, `Create`/`Update` for mutations; `Delete` stays resource-level).
Primary keys are exempt — their readability follows the resource-level grant. There is
no per-field permission tag to write: a `perm:` tag on a source struct is a generation
error, and a stale one in a generated request struct fails Set construction at startup.

| Tag | Where | Effect |
| --- | --- | --- |
| `spanner:"ColumnName"` | every field of `@resource`/`@virtual` structs | Maps the field to its Spanner column. Required — a missing tag or unknown column is a generation error, and field nullability must match the column's. |
| `conditions:"…"` | resource fields | Comma-separated list of field conditions, see below. Values match exactly (no spaces); a value the generator does not recognize is refused, with the nearest recognized one suggested. |
| `default_create_fn:"pkg.Func"` | resource fields | The generated create path calls the referenced function to populate the field when the request doesn't supply it. A field with a default function is not treated as required. |
| `output_only_update_fn:"pkg.Func"` | resource fields | The generated update path sets the field by calling the referenced function on **every** update; implies output-only. This is the *mechanical enforcement stamp* — a field whose meaning is "this row was updated", like `UpdatedAt`. A timestamp with domain meaning (a "last serviced" written by one business transition) is not an update function: it is an explicit update in the code that owns the business event — see [Ship.LastRefitAt](lodestar/pkg/resources/ships.go). Declaring an update function on any field also gives the resource a generated `New<Resource>Touch(keys…)`: an update carried entirely by the update functions, running the full update pipeline (permission check, stamps, write conditions, change events) with no caller-set fields — the only way to express "bump the row" (an update patch with no fields set is a silent no-op). Example: [Ship.UpdatedAt](lodestar/pkg/resources/ships.go) using `resource.CommitTimestampPtr`. |
| `allow_filter:"true"` | resource fields | Permits `filter` expressions on a field that isn't indexed (indexed fields are filterable automatically). Copied through to the generated request structs. |
| `index:"true"` | `@virtual` struct fields only | Declares the field indexed (filterable/sortable). Rejected on table-backed resources, which get index information from the schema. |
| `uniqueindex:"true"` | `@virtual` struct fields only | As `index`, and marks the index unique. |

Values recognized in a `conditions` tag:

- `immutable` — the client sets the field on create, and it can never change afterward:
  an update touching it is rejected with a 400 (the generator emits `immutable:"true"`
  into the patch request struct — you write the condition, never the emitted tag), and
  the generated Collection never exposes `Update` on the field's tag as grantable.
- `pii` — marks the field as personally identifiable. Emitted as `pii:"true"`, surfaced
  in the TypeScript metadata, and the field is rejected in URL `filter` expressions
  (filter via the POST body instead, which doesn't land in access logs).
- `input_only` — write-only: accepted on create and update but never returned (read and
  list structs get `json:"-"`, and the field is omitted from the TypeScript metadata).
  Example: [DistressCall.Transcript](lodestar/pkg/resources/distress_calls.go).
- `output_only` — the server owns the value: returned to clients but never accepted from
  them (patch structs get `json:"-"`, excluding it from both create and update input).
  The value comes from the database or from `default_create_fn` /
  `output_only_update_fn` — and a field with an `output_only_update_fn` is output-only
  even without the condition. Example:
  [DistressCall.CaseNumber](lodestar/pkg/resources/distress_calls.go).

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

One contract to know when driving the patch layer from application code: **an update
patch with no fields set is a silent no-op.** `Apply` and `Buffer` return immediately —
no permission check, no `output_only_update_fn`, no mutation, no change event — so a
keys-only patch does nothing at all (and a REST `PATCH` with an empty body is a harmless
200 for the same reason). "Bump this row's enforcement stamps with no field changes" —
a *touch* — is therefore not expressible as a bare update patch: use the generated
`New<Resource>Touch(keys…)`, which exists exactly when the resource declares an
`output_only_update_fn` field and runs the full update pipeline with the update
functions supplying the write. Keep the two kinds of server-owned timestamp apart:
update functions enforce mechanical stamps (`UpdatedAt`), while a timestamp with domain
meaning (`LastServicedAt`) is an ordinary **explicit** update performed by the one piece
of application code that owns the business event — `output_only` keeps clients out, and
the writing code sets the field like any other.

## 3. Struct tags the generator writes (zz_gen request structs)

Read back at runtime by the `resource` package; listed here for reading generated code.

| Tag | Meaning |
| --- | --- |
| `json:"camelName"` | Wire name of the field and the key under which its permissions are registered. `json:"-"` hides the field (input-only fields in read structs; primary keys and output-only fields in patch structs). |
| `perm:"-"` | The primary-key exemption marker, emitted only on primary-key fields of list/read (and computed) structs: the field requires no field-level grant, and its readability follows the resource-level grant. Every field without the marker is enforced structurally with the endpoint's permission. `-` is the only legal value — any other perm value in a request struct is a startup error (the stale-struct guard). |
| `immutable:"true"` | From `conditions:"immutable"`; the patch decoder rejects updates to the field. On the grant side, the generated Collection never lists `Update` on an immutable field's tag, so the (unsatisfiable) update grant can never be assigned to a role. |
| `index:"true"` | From the schema's indexes (or `index`/`uniqueindex` tags on virtual resources); makes the field filterable and sortable. |
| `allow_filter:"true"` | Copied from the source struct; makes an unindexed field filterable. |
| `pii:"true"` | From `conditions:"pii"`; the field is rejected in URL filter expressions. |

## 4. Reserved query parameters

List/Read requests accept exactly these query parameters — anything else is a 400, and
none of them can be used as field names in filters:

| Parameter | Meaning |
| --- | --- |
| `columns` | Comma-separated JSON field names to return; omitted means all accessible fields. |
| `filter` | Filter expression over indexed/`allow_filter` fields, e.g. `name:eq:Vanta`. Operators: `eq`, `ne`, `gt`, `lt`, `gte`, `lte`, `in`, `notin`, `isnull`, `isnotnull`. On POST query routes the filter may be sent in the body as `{"filter": "…"}` instead (required for `pii` fields), but not in both places. The expression is validated when the request is decoded, with the rest of the request's shape: a field that is unknown or not filterable, a malformed condition, or a value that does not fit its field is a 400 before any permission check runs, any query executes, or a computed resource's List function is called. A table filter must also touch at least one indexed field, which the database parse enforces. |
| `sort` | Comma-separated `field[:direction]` entries, e.g. `name:asc,rank:desc`; direction is `asc` (default) or `desc`. |
| `limit` | The page size: rows returned per request. Omitted, the resource's declared default applies (`@page`, generator-wide default 50). A value over the resource's declared maximum is refused with a 400 naming the maximum, never clamped; `0` is refused. `limit=all` returns every row with no `Link` header, and is permitted only on a resource that declares no maximum. |
| `cursor` | The page position, copied from the `Link` header of the page that issued it (`rel="next"` or `rel="prev"`); never assembled by a client. A sealed token carrying the boundary row's sort values, the direction, and a fingerprint of the resource, scope, filter, sort, and limit — a cursor altered, sealed by another application, or presented with a different query, tenant, or page size is a 400. A request with a cursor carries the same `sort` and `limit` as the page that issued it. A resource with no `@order` and a request with no `sort` lists in primary-key order and issues no cursor: paging past the first page requires a sort. |
| `count` | `count=true` on a first page asks for the total number of rows the same WHERE admits, answered in the `Total-Count` header; later pages carry no count and refuse the parameter. A request that does not ask pays nothing. |
| `offset` | Removed. A request carrying it is refused with a 400 naming `cursor` as its replacement; pages are positioned by the row the cursor names, not by a count of rows to skip. |
| `capabilities` | Comma-separated write permissions (`Create`, `Update`, `Delete`, `Execute`) to evaluate per row — the §13 capability envelope. Each returned row gains the reserved `zzCapabilities` property: `Update` carries the positive list of editable JSON field names, `Delete` a boolean, `Execute` the positive list of `@target` methods that apply to the row — a declared `@transition` requires the row's pre-image state in its `from` set, a conditional Execute grant ANDs its condition into the same boolean (a plain `@target` method's is the condition alone; unconditional plain methods are structural, no SQL), and the user holds the method's Execute grant — and `Create` the positive list of workflow member resources the user may create beneath the row (§11): the members whose immediate `@stateRoot` hop is this resource, gated by the user's member Create grants, a conditional grant's state terms evaluated against this row's own uniform state binding while terms the parent row cannot answer count potentially-true (an unconditional member grant is structural, no SQL). Advisory hints computed from the same row image and decision instant as the read (conditions render as booleans in the same statement; pure RBAC adds no SQL; a `new.`-referencing term counts potentially-true while the rest of its condition still renders). Enforcement is unchanged. |

A sort or filter runs over the projection the caller can see. A field the caller is
denied cannot be sorted or filtered on: the request is a 403 naming the field, because
the order or the membership of the result would leak its values. A conditionally
granted field can be: the statement orders and filters on `CASE WHEN <condition> THEN
column END`, so a cell the caller may not see is `NULL` for the query — it sorts in the
`NULL` region (last ascending, first descending, the same as a genuinely `NULL` cell;
PostgreSQL states the placement, Spanner sorts on an `IS NULL` key ahead of the column),
matches `isnull` and nothing else, and a cursor positioned on it carries the `NULL` key.
When the field's condition covers the whole row predicate (every returned row shows
the field) the raw column is used instead. A `CASE` in `WHERE` or `ORDER BY` cannot use
an index, so only such a field pays for it; `index:"true"` stays the declaration of
which fields may be filtered. A `@computed` resource evaluates no conditions at read
time, so its sort and filter fields still require an unconditional grant.

A paged list answers with headers beside its JSON array body. `Link` (RFC 8288)
carries a complete URL per relation that exists — `rel="next"` and `rel="prev"`, each
the request's own URL with `cursor` set — so the first page has no `prev` and the last
no `next`, and a client follows the URLs as given. `Total-Count` carries the total when
the first page asked `count=true`. `Page-More: true` marks a list served in primary-key
order (no `@order`, no `sort`) whose rows did not fit the page; it issues no cursor, so
paging further requires a sort. `limit=all` answers with none of them. A list served
cross-origin must add `Link`, `Total-Count`, and `Page-More` to the CORS exposed
headers, or the browser client cannot read them.

## 5. The permission endpoints

Every generated router registers two library-owned endpoints on the default outlet
(applications wire nothing):

```
GET /<prefix>/permission-digest              → the session user's global digest
GET /<prefix>/permission-digest?domain={id}  → one tenant partition's digest
GET /<prefix>/user-domains                   → the domains the session user can see
```

An additional outlet declared with `ServesSessions()` gets the same two routes under
its own prefix, served by the same generated handlers behind whatever session
middleware the application composes around that outlet; an outlet without the
declaration gets neither (the generated router tests prove its prefix 404s them).

**The digest.** The payload is the user's structural grant enumeration for the requested scope:
resource → permission → `granted` | `conditional`, with field targets under their
dotted names (`"WorkOrders.title"`) and **absence meaning denied** — consumers fail
closed by construction. It is advisory UI material (which menus, routes, and form
inputs to render); enforcement stays with the endpoint gate, the read rules, and the
write stages. Nothing folds — no `now`, no row data — so a payload is stable for the
life of a policy snapshot and caches cleanly per scope. An unknown or grant-free
domain digests to `{}`, so the endpoint never confirms tenant existence under
`WithConcealedDomains`. `@cccteam/resource-angular/types` carries the matching `PermissionDigest` /
`PermissionDigestState` types (and `RowCapabilities` for the capability envelope).

**User domains.** The payload is the sorted list of domains where the user holds at
least one grant — the tenant picker's membership question, answered by the library so
no application hand-writes it. The predicate is concealed tenancy's own foothold test
(`access.Client.UserHasGrants`), so a domain listed here is exactly a domain whose
routes answer the user with ordinary 403s rather than a concealing 404: the picker and
the domain guard can never disagree. The global scope is never a domain. The answer
reports grants, not tenants — a domain the application has since removed still lists
while grants in it remain; existence stays the application's `DomainExists` seam. An
empty membership is `[]`, never `null`.

## 6. Impersonated sessions

`cccteam/session` can establish a session that operates as another user or as a role
on behalf of an authenticated actor, optionally attenuated by an
`accesstypes.PermissionMask`. Such a session is an ordinary session to every consumer;
this package supplies the two pieces that must know about it:

- **`SessionPermissions(ctx, forUser, forRole)`** composes the request's
  `UserPermissions` from the session: `forUser` (typically `access.Client.ForUser`) for a
  user principal — an ordinary session or an impersonated user — and `forRole`
  (typically `access.Client.ForRole`) for a role principal, then applies the session's
  mask. A role checker satisfies `RolePermissions` — `UserPermissions` without `User()`,
  because a role is not anyone — and the composition supplies `User()` itself: the
  session's effective identity, which for a role principal is the actor who established
  it, so a row condition's `subject` binds to the real person and nobody's identity is
  borrowed. An application that does not operate sessions as roles passes
  `RolePrincipalsUnsupported` as `forRole`; a role principal then fails closed (every
  check Denied, empty digest, no domains) with the same `User()`. `Masked(perms, mask)`
  is the attenuation on its own: a permission the mask does not allow is Denied for every
  resource before policy is consulted and dropped from the permission digest, so the
  frontend's digest agrees with what `Check` enforces. Forgetting the mask fails *open*,
  which is why the composition is here rather than hand-rolled per application.
- **`UserEvent(ctx)`** — the event source every generated write handler stamps onto
  `DataChangeEvents` — names the actor first for an impersonated session:
  `alice impersonating bob (session id)` and `alice as role PartnerViewer (session id)`,
  unchanged (`bob (session id)`) otherwise. Every tracked data change carries evidence of
  the real person with no regeneration.

## 7. RPC methods: what a method is for

The generator enforces a method's shape (`@rpc`, the `Execute` forms, the wire
vocabulary, the frame). Three conventions it cannot see are stated here, and hold
across every application on this stack:

- **A method answers with identifiers and outcomes, never rows.** `Execute` may return
  `(Result, error)`, and the result is for what the call decided or created: the id of
  the row it opened, the timestamp it stamped, the readings it folded. Rows are read
  through their resource routes, where permission masking applies field by field; a
  result that carried a row would carry it unmasked. The generator refuses a `@resource`
  or `@computed` struct in the result position; this sentence covers the shapes it
  cannot recognize.
- **A method that only answers a question is a computed resource, not an RPC.** A read
  with no write belongs on a `List`/`Read` route with a `List`/`Read` grant, where the
  digest, the client handles, and the masking all already apply. There is no read-only
  RPC form, by decision. A caller-scoped question ("my standing", "my current level") is
  a computed resource whose `List` yields the caller's one row: `QuerySet.User()` names
  the identity the permission check ran as — the viewed person under a view-as session,
  the real actor under an act-as-role session — beside `QuerySet.Scope()`. A table row
  that is the caller's own is a `List` grant whose condition binds the user column to
  `subject`; nothing new is needed.
- **A transaction-form body keeps its effects inside the transaction.** A message, a
  webhook, a file written from inside `Execute` cannot be rolled back, so a dry run of
  that method lies and a retried transaction repeats the effect. Write an outbox row
  the transaction commits and let a worker deliver it; if an effect truly cannot wait,
  the method is the client form, which the dry run refuses for exactly this reason.
  A file a request carries is the one exception, and it has its own form: an
  `@upload` method's frame streams the files to the application's store before the
  body runs, the body records the minted keys, and the frame promotes them after
  commit or discards them when nothing committed. The body never writes the store.
  What the frame cannot cover is a crash between commit and promotion, which leaves
  a pending object with a claiming row; the application sweeps pending objects older
  than its own window against its rows, and the window is the application's
  decision.

Two things a method deliberately cannot do. It cannot reach the response writer: no
cookies, no session started or ended, no header of its own. Its status is chosen
through `@answers` and the result's `HTTPStatus()`, never written directly, so a
declared 4xx still travels as a typed body with the transaction rolled back. A method
whose `Execute` returns only an error has no result to choose with, so the one status
it may declare is `@answers(204)`, and the frame writes No Content where the empty 200
would go; the response encoder has no status setter to reach for. The
moment a body can write the response, every method can start a session, and the
frame's guarantee — JSON in, JSON out, permission-gated, transactional — stops meaning
anything; sign-up flows that end in a login belong beside the login routes in the
auth package. And it cannot
be armed by default: inside `Execute`, typed queries and patches run trusted unless the
body calls `Enforce(caller)`, because `@state` columns are structurally unwritable from
the wire and the frame's own status stamp must stay privileged. The method's entry
contract with the permission system is `Execute` on itself, its target row, its
transition, and any row condition on the Execute grant, all visible in the collection.

