# resource

The `resource` module provides permission-enforced CRUD over Spanner-backed resources:
the `resource` package is the runtime (query decoding, permission enforcement, patch
sets), and `resource/generation` is the Resource Generator that turns annotated source
structs and schema migrations into handlers, routes, request structs, and TypeScript —
including `zz_gen_api.ts`, the typed client surface the framework-neutral
[`@cccteam/resource`](https://github.com/cccteam/ccc-lib/tree/master/projects/resource)
runtime interprets (see the [lodestar demo](lodestar/README.md)).

Every generated handler is a **frame** around the application's code: the generated
part of one request, from reading the route parameters and the body, through the
permission check at the gate and the row lookup within the caller's tenancy, to the
call into the application and the response, refusals included. What sits inside the
frame is the application's own: an RPC method's `Execute`, a computed resource's
`Read<Name>` and `List<Name>`, a file's content function. The frame owns the response
writer, the transaction, and every status the application does not choose through
`@answers`, which is the guarantee the rest of this document leans on: JSON in, JSON
out, permission-gated, transactional. This document says "the generated frame", or just
"the frame", for that part and "the body" for the application's code inside it.

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
| `@virtual` | struct in the resources package | none | A resource backed by a view instead of a base table. Because there is no table metadata, indexed fields must be declared with `index`/`uniqueindex` tags (see §2), and the read identity (if any) with `@primarykey`. A view that declares its key serves a keyed read route beside its list, as a table does (`@suppress(readHandler)` removes it). Without a key the view is a whole read-only list, with no read route and no paging: one list route serves every row the filter admits in one response, sorted when a `sort` is asked or an `@order` declared, a numeric `limit` or a `cursor` is refused with a 400 naming `@primarykey` as the way to page (section 4), `@page` is refused at generation, and its metadata says `readDisabled`. A view that carries a table's rows one to one declares the table with `@rowsOf`, so a page listing the view writes to the table; without it the view is a read-only list. Example: [OpenMissionsBySquadron](lodestar/pkg/virtualresources/open_missions_by_squadrons.go). |
| `@computed` | struct in the resources package | none | A read-only resource (List/Read only) whose rows are produced by hand-written query logic rather than a table. Primary-key fields are marked with `@primarykey`, and the key is optional: without one the resource is a whole read-only list, with no read route and no paging (one list route, every row the filter admits in one response, sorted when asked or declared; a numeric `limit` or a `cursor` is refused naming `@primarykey` as the way to page, `@page` is refused at generation, and the Collection registers List only, so a Read grant on it is refused at deploy). Example: [StandingOrder](lodestar/pkg/computedresources/standing_orders.go). A computed view that carries a table's rows one to one declares the table with `@rowsOf`, so a page listing it writes to the table. Its permission checks run at decode time, where no row exists, so the generated Collection marks it computed and deploy-time role migration rejects grants on it whose conditions are not row-free (the same rule Execute grants follow). From the browser a computed resource answers a list query exactly as a table does, with no code in its List function: the generated handler applies the filter, then the sort when the request asks one or the struct declares one (`@order`), then the page (cursor, Link header, count) over the rows the function yields; with neither a `sort` nor an `@order` only the whole list (`limit=all`) is served, the rows in the order the function yielded them, after the filter; a paged request without an order is refused (section 4). A field is filterable when it carries `allow_filter`, as on a table; `index` and `uniqueindex` are refused (there is no database index), and `allow_filter` on a field the in-memory evaluator cannot compare fails generation with the field named. A List function that runs its own queries may take pieces of the work to push down — `qSet.Filter().Take(fields…)` returns the conditions on those fields and leaves the handler the rest (defined on an AND-only filter; an OR takes nothing), `qSet.TakeSort()` returns the total order (empty when the list is not sorted: the body's own order stands) and marks it the body's, and `qSet.TakePage()` returns the page bounds only when paging in the body is correct (nothing left to filter, the sort taken, no count asked) and otherwise answers false so the handler pages. Taking is the only way to opt out, so a body can never make the result wrong. A computed list sorts and pages `NULL` where the application's database does, the same end as the tables beside it (Spanner first ascending and last descending, PostgreSQL the reverse); a body's plain `ORDER BY` already produces that placement, and the generated decoder carries the database type so the handler's in-memory sort and page boundary agree with the rows a body yields. Example: [SectorHazardBoard](lodestar/pkg/computedresources/sector_hazard_boards.go). |
| `@rpc` | struct in the rpc package | none | Declares an RPC method: the struct's fields are the request payload, and the struct declares `Execute` in one of two forms, read off its signature — `Execute(ctx context.Context, txn resource.ReadWriteTransaction, client *Client) error` runs inside the generated handler's read-write transaction; `Execute(ctx context.Context, client resource.Client, rpcClient *Client) error` runs outside one. The third parameter is the application's own RPC client type. `Execute` returns `error` alone, and the handler answers with an empty 200, or `(Result, error)` with `Result` a struct type or a pointer to one, and the handler answers with the result after the transaction commits, mirrored with generated wire names (a nil pointer answers empty). A method answers with identifiers and outcomes, never rows: a `@resource` or `@computed` struct in the result position is refused, since rows are read through their own routes, where permission masking applies. A struct with no `Execute`, or one of another shape, fails generation naming the struct; no interface declaration takes part. Example of an answering method: [InspectShip](lodestar/pkg/rpc/inspect_ship.go). The generated frame stamps the caller into the context before `Execute` (`resource.CallerFrom`), so a body may arm a typed patch or query with `.Enforce(caller)` and run the pipeline the resource routes run, or ask `caller.Check` for a decision to branch on; without either the body is trusted, the default. A transaction-form method honors `X-Dry-Run: true`: the whole frame and the body run and the transaction rolls back, refusing exactly as the real call would and answering an empty 200 where it would have succeeded; a client-form method refuses the header with 400. Examples: [HoldMission](lodestar/pkg/rpc/hold_mission.go) (armed), [CompleteMission](lodestar/pkg/rpc/complete_mission.go) (decision as data). Gated by the `Execute` permission. The struct lives in a file named after it in snake case (`claim_mission.go`), as every generated kind does; a name ending in `Test` carries the `_rpc` marker instead (`start_flight_test_rpc.go`), so neither it nor the `zz_gen_` files generated beside it are Go `_test.go` files. Example: [ClaimMission](lodestar/pkg/rpc/claim_mission.go). |
| `@enumerate` | named type with underlying type `string` | enum table name | Generates typed constants for the named type from the rows of an enum table, and declares the table an enumeration: its rows are the program's constants, fixed at generation time. Two rules follow. A table-backed field whose foreign key points into the table is emitted as an `enumerated` picker whose values ride in the TypeScript metadata (`enumeration: [{ id, display }]`, Description as the display), so the browser needs neither a request nor a List grant, and this wins even when a struct also exposes the table as a resource. A `@resource` struct backing the table is read-only by derivation: no patch handler, no Create/Update/Delete in the collection, fields marked `readOnly`, and create or update defaults, validators, or a manual patch Set on it fail generation. A table is an enum table when it has a `Description` column (the generator runs `SELECT DISTINCT Id, Description` against the migrated schema — avoid that column name on non-enum tables). An enum table with more than 500 rows warns at every generation, since each field keyed into it carries every row in the TypeScript metadata; the warning names the bytes and the fields, and the way out is to drop `@enumerate` from the type and expose the table with a `@resource` struct, whose picker reads it whole or, under a `@page` maximum, one page at a time. Example: [ShipRole](lodestar/pkg/resources/enums.go) behind [ShipClass.RoleID](lodestar/pkg/resources/ship_classes.go). |
| `@enumerate` | field of an `@rpc`, `@resource`, `@virtual`, or `@computed` struct | resource or enum table name | Declares which resource a picker for the field lists — the field holds another resource's identifier, and the generator alone says which. The generated metadata emits `displayType: 'enumerated'` with `enumeratedResource: Resources.X`; the browser narrows and presents X's rows and never chooses another resource. X must exist — a table-backed, virtual, or computed resource — and be keyed by a single column, since a picker stores one key. A picker reads X in one of two ways, chosen by X's `@page` maximum: with none, the whole list in one request (`limit=all`), the chosen row resolved from it, so X may suppress its read; with a maximum, one server page at a time, the chosen row read by key, so X must serve a read — a bounded X with a suppressed read fails generation naming the two ways out (keep the read, or drop the maximum), on this path and on an undeclared foreign key into a routed resource alike. A list field cannot declare one: the field would store many keys where a picker stores one, so `@enumerate` on a slice, an array, or a named list type fails generation on every path naming the struct and field (`struct Ship field Tags: @enumerate on a list field; a picker stores one key`), and a byte slice, one value on the wire, is not a list here. Four rules: a **plain column** (no foreign key) names the resource the picker lists, with no Go validation and no database check, as for an identifier held from outside the schema (example: [Mission.BriefingTemplateID](lodestar/pkg/resources/missions.go) → [BriefingTemplates](lodestar/pkg/computedresources/briefing_templates.go)); a **foreign key** may name a resource other than its target whose rows key into the same space, typically a virtual view over the target carrying display columns the target lacks, while the constraint stays the guard at write time — naming the target itself is refused as redundant, the schema already says so (example: [Mission.ClientID](lodestar/pkg/resources/missions.go) → [ClientRosters](lodestar/pkg/virtualresources/client_rosters.go)); naming an **enum table** — one an `@enumerate` type declares, with or without a struct over it — renders the field from the inline `enumeration` values exactly as an inferred key into that table does, one rule for declared and inferred (example: [FailMission.ReasonID](lodestar/pkg/rpc/fail_mission.go)); and a declaration on a foreign key that **already points into an enum table** is refused as a contradiction, since those values are program constants baked into the metadata. A request field has no schema, so it always declares (example: [CompileBriefing.TemplateID](lodestar/pkg/rpc/compile_briefing.go), naming a computed resource). A declared enumeration follows the RPC outlet rule: naming a resource that is not on the target outlet fails generation with the fix named, where an inferred key degrades to its plain type. |
| `@suppress` | `@resource`, `@computed`, or `@rpc` struct | one or more of `listHandler`, `readHandler`, `patchHandler`, `allHandlers`, `allRoutes` | Skips generating the named handlers, or all routes. Suppressing `readHandler` on a resource a picker lists is refused where the resource declares a `@page` maximum (`@enumerate`). Suppressing `patchHandler` also removes the resource from the consolidated patch handler. `allRoutes` is rejected on consolidated resources unless the patch handler is suppressed or the resource is excluded from consolidation. On an `@rpc` struct, any argument suppresses the generated handler. |
| `@defaultsCreateType` | `@resource` struct | type name | The generated create path calls `Defaults()` on the named type to set defaults when creating the resource. |
| `@defaultsUpdateType` | `@resource` struct | type name | As above, for updates. |
| `@validateCreateType` | `@resource` struct | type name | The generated create path calls `Validate()` on the named type to validate the incoming resource. |
| `@validateUpdateType` | `@resource` struct | type name | As above, for updates. |
| `@primarykey` | field of a `@computed` or `@virtual` struct | none | Marks the field as (part of) the resource's primary key; multiple annotated fields form a compound key in declaration order. Primary-key fields are exempt from field-level permission enforcement (their readability follows the resource-level grant). The key is what gives a row identity: the read route (`/{key}`), the cursor's tie-break (the key fields end every order, so a keyset cursor walks a total order), and the browser's row identity (`keyOf`). Without a key the resource is a whole read-only list, with no read route and no paging: served in one response, sorted when asked or declared, refusing a numeric `limit` or a `cursor` with a 400 that names `@primarykey` as the way to page, and refusing `@page` at generation. Rejected on table-backed `@resource` structs, whose keys come from the schema. Compound example: [SectorHazardBoard](lodestar/pkg/computedresources/sector_hazard_boards.go); key-less example: [StandingOrder](lodestar/pkg/computedresources/standing_orders.go). |
| `@rowsOf` | `@virtual` or `@computed` struct | table resource name | The view declares its backing table, a create goes into the table, and the new row shows up in the view on the next list because the view's SQL reads that table. The declaration states row identity, not columns: every row of the view is one row of the named table under the same key, and the view's other columns are its own — joined from other tables, aggregated, or computed in the projection; the generator compares none of them to the table. It checks, each refusal naming the fix: the named resource exists and is a table-backed `@resource` (a `@virtual` or `@computed` target is refused, since a view cannot back a view, and an `@enumerate` table — with or without a struct over it — is refused as a contradiction, since its rows are the program's constants and a create button for them would be policy about nothing); the view declares `@primarykey` fields matching the table's key in count, order, Go field name, and Go type, nullability included, and on a `@virtual` the `spanner` columns as well, the message naming the first mismatch and the table's key; the two share a `@permissionScope`; the table is served on every outlet the view is served on (fail loud, never a silent degrade); and the view does not name itself. Refused on `@resource` and `@rpc` structs. Several views may name one table. The view's TypeScript metadata carries `rowsOf: Resources.X`, so a page listing the view sends its create, edit, and delete to the table and opens a row on the table's page; a view without the declaration is what it is today, a read-only list. One backing table only: two tables sharing one key (a table and its one-to-one extension) is the door left open and not built, and a parent joined to its children is never one to one with anything and stays a read-only list or an RPC. Examples: [MissionBoard](lodestar/pkg/virtualresources/mission_boards.go), a same-row view over Missions; [SquadronRoster](lodestar/pkg/virtualresources/squadron_rosters.go) and [PilotAssignment](lodestar/pkg/virtualresources/pilot_assignments.go), two association views over SquadronMemberships whose compound key is edited and deleted from the list. [OpenMissionsBySquadron](lodestar/pkg/virtualresources/open_missions_by_squadrons.go) and [FeeByKind](lodestar/pkg/virtualresources/fee_by_kinds.go) declare nothing and stay read-only lists. |
| `@typescript` | the declaration of a type used as a field: a struct, or a named type over another type (`type Position json.RawMessage`) | `Name`, or `Name, from: "module"` | Declares the TypeScript type of a Go type whose shape lives outside Go, once, on the type's declaration, and every generated file that carries the type imports `Name` from `module`, spelled verbatim. A table column, a computed field, or an RPC field typed by it is `Name` in its interface, with display type `object` in the metadata. Without `from:`, `Name` must be a TypeScript built-in (`string`, `number`, `boolean`, `unknown`). A plain struct needs no declaration: its interface is derived from its fields (section 12). A type that writes its own JSON (`MarshalJSON` or `UnmarshalJSON`) must declare, since its fields say nothing about the wire, and is refused without it. A slice comes from the field (`[]Position` is `Position[]`), never from the declaration; the declaration is read wherever the type is declared, in the application or in a dependency. Two declarations importing one `Name` from different modules are refused, as is `from:` on a built-in and a non-built-in without it. Not valid on a `@resource`, `@virtual`, `@computed`, or `@rpc` struct, which are typed field by field. Example: [Position](lodestar/pkg/resources/distress_calls.go). |
| `@attribute` | field of a `@resource` struct | `name[, via: Remote.Segments]` | Declares an attribute binding: the vocabulary name grant conditions reference for this row attribute (ABAC). Bare, the annotated column itself carries the attribute; with `via:`, the binding is a join path leaving through the annotated foreign key — `via:` carries only the remote segments, Go field names on each successive struct, dotted for multi-hop (`via: StationId.Sector`), and every hop must resolve many-to-one through a real foreign key or generation fails. Names follow the condition language's identifier rules (`[A-Za-z_][A-Za-z0-9_]*`); `subject`, `now`, and `new` are reserved; a name is declared once per resource. |
| `@domain` | field of a `@resource` struct (bare form also on a `@virtual` struct) | none, or `via: Remote.Segments` | Declares the structural tenancy binding: how every row of a domain-scoped resource resolves to its tenant. Bare on the tenant-key column itself, or `via:` a foreign-key path to it (same grammar as `@attribute`). **Mandatory on every domain-scoped table-backed or virtual resource** — missing is a generation error, and so is declaring it on a resource that is not domain-scoped: global scope is the explicit opt-out (design plan §06). On a virtual resource only the bare form is valid, naming a column the view's projection carries. A bare `@domain` derives the tenant column's runtime behavior — never stated twice: the column decodes output-only (create and update closed, so the wire cannot express a tenant write or re-tenant a row) and the framework stamps it from the request's domain partition on create, so the checked domain and the written domain are the same value by construction; restating behavior through `conditions` or `default_create_fn` tags is rejected, and Create/Update on the column are ungrantable while reads stay grantable. Deliberately not an `@attribute` — it is consumed by tenancy injection and never referencable from grant conditions. At most one per resource. It also tenant-filters the subject subqueries anchored on the resource (see the subject rows). The `via:` path is resolved through foreign-key metadata, not through the remote resource's own bindings — a domain-scoped parent table does **not** transitively supply tenancy to resources referencing it; each resource declares its own `@domain`. Generation warns at every run for a listed table-backed resource whose lists the schema does not serve under this binding: a bare `@domain` with an `@order` and no index leading with the tenant column and then the order columns, where the warning names the index wanted, or a `via:` path, whose lists scan the whole table (section 9). |
| `@state` | field of a `@resource` struct | `default: <value>` | Marks the resource's state column (ABAC design plan §09). The column must be a foreign key to its state enum table (the ordinary Id/Description convention — the FK identifies the table, nothing is declared on it), and the declared default must be one of that table's values. The marker derives the field's behavior — never stated twice: the field decodes output-only (create and update closed, so the wire cannot express a state write; transitions happen inside RPC bodies), Create/Update on it are ungrantable, reads stay grantable, and the generated create patch applies the declared initial state on insert (never a database DEFAULT). State values change only by migration: a mutation permission registered against the state enum table is a generation error, while Read stays grantable. |
| `@stateRoot` | FK field of a `@resource` struct | `RootStructName` | Declares workflow membership (ABAC design plan §09) on the foreign key anchoring the member — the field is the hop, so only the workflow root's struct name is spelled. Each member declares only its immediate hop; chains compose through other members and must reach the root (a cycle or a hop outside the workflow fails generation), every hop is many-to-one through a real foreign key, and member and root permission scopes must match. The generator synthesizes the uniform `state` attribute binding on the root (a column binding on its `@state` field) and every member (a join path through the chain), so one condition text reads identically across the workflow, and emits the assembled graph as a committed, drift-tested `zz_gen_workflow_<root>.dot` file per stateful root: the whole tree — root and members as solid nodes with one edge per hop labeled with the anchoring FK field, dashed context nodes for every FK reference leaving the tree (the tenant record included), a state cluster with every value and the default marked, one labeled edge per declared `@transition`, and a legend; undeclared state changes stay RPC-body business rules the framework cannot see. The TypeScript resource metadata carries the same facts (root, members with hops, states, default, transitions) as a `Workflows` constant, so a frontend can render the graph itself. A resource belongs to at most one workflow. |
| `@transition` | `@rpc` struct | `RootStructName, from: a[, b…], to: c` | Declares the RPC method as a workflow state transition (ABAC design plan §09): the method moves rows of the named root resource along one edge. The root must carry `@state`, every `from`/`to` value must be a value of its state enum table, method and root permission scopes must match, and the struct's `Execute` must be the transaction form, carrying exactly one `@target` field. The generated handler owns the mechanical frame inside the transaction it already runs: before the body it locates the target row within the tenancy predicate (absent or cross-tenant is NotFound) and verifies the pre-image state is in the `from` set, then evaluates any row-referencing condition the caller's Execute grant carries against the same located row — either refusal is one uniform Forbidden naming the method and the row, so the wire never says whether the state or the condition said no (§12); after the body returns without error it stamps the `to` state as the last mutation. The body never reads or writes the state field — it carries only the edge's business effect. Who may run the method stays its Execute grant (grants-only, §09). The declared edge travels in the generated Collection, draws labeled edges in the workflow DOT file, rides the TypeScript method metadata, and answers `capabilities=Execute` per row. Example: [LaunchMission](lodestar/pkg/rpc/launch_mission.go). |
| `@target` | field of an `@rpc` struct | none, or `RootStructName` | Marks the field carrying the target row's key — exactly one per method, its type matching the target's single-column primary key. With `@transition` it is bare (the declared root is the target); without one, `@target(Root)` names the row resource directly and the method gets the plain located-row form (ABAC design plan §12): the generated handler locates the row inside its transaction (absent or cross-tenant is NotFound) and evaluates any row-referencing condition on the caller's Execute grant against it, with no state check and no stamp. Either way, a targeted method's Execute grants may carry row conditions — `access.MigrateRoles` validates them against the target resource's binding vocabulary — and the method joins the target resource's per-row `capabilities=Execute` answer. Requires the transaction form of `Execute`; method and target permission scopes must match. A domain-scoped target resolves tenancy through its `@domain` binding, either form: a bare tenant column is read off the located row, a join-path binding is verified with one query in the same transaction — absent and cross-tenant rows answer the same NotFound either way. Example: [HailShip](lodestar/pkg/rpc/hail_ship.go). |
| `@answers` | `@rpc` struct | `200, 409` | Declares the statuses the method may answer with; the result type carries `HTTPStatus() int` and chooses one per response. Allowed: `200`, `201`, `202`, `204`, and any 4xx except `401`, `403`, and `404`, which stay the frame's own refusals; at least one must be a 2xx. A 4xx answer is the method's refusal with its typed body: in the transaction form the transaction rolls back first, so nothing the body armed commits. `204` writes no body and requires a pointer result returned nil, or an answerless method, whose only permitted declaration is `@answers(204)`. A result declaring `HTTPStatus()` without `@answers`, or the reverse, is a generation error; an undeclared status at runtime answers 500. The TypeScript client resolves `{ status, result }` for a method with declared statuses and still throws on every undeclared 4xx. Example: [CompleteMission](lodestar/pkg/rpc/complete_mission.go). |
| `@upload` | `@rpc` struct | `max: 5MB` | Declares the method as a multipart upload. Its `Execute` takes `resource.Files` third — `Execute(ctx, txn resource.ReadWriteTransaction, files resource.Files, client *Client)`, the transaction form only, since the transaction is what claims the files — and the declaration and the signature go together (either alone is a generation error). The request is `multipart/form-data`: one part named `request` first, carrying the JSON the method's decoder reads exactly as for a JSON RPC, then one or more parts named `file`. `max` (a byte count or `KB`/`MB`/`GB`, 1024-based) bounds the whole body; over it is a 413 naming the maximum, no `file` part a 400, a body that is not multipart a 415. The frame streams each file to the application's `FileStore` (asserted on the application as `FileStore() resource.FileStore` while any method uploads or any struct declares `@file`) under a key it minted, runs the body with the `Files`, and on any failure before commit deletes the objects it streamed and answers with the failure; after a commit nothing more happens, since the rows the body wrote claim the keys. The body records the keys wherever its schema wants them; reading a file back is the `@file` route (section 13). A dry run streams nothing: the `Files` describe the parts with empty keys. The TypeScript handle gains `upload(body, files)`, which refuses locally over the maximum. Example: [AttachMissionDocument](lodestar/pkg/rpc/attach_mission_document.go). |
| `@file` | field of a `@resource`, `@virtual`, or keyed `@computed` struct (the column holding the store key); or a keyed `@computed` struct | none, `segment`, and on a field `name: Field`, `type: Field` | A row says which stored object is its file, and the generator serves that file under the row's read route: `GET <read route>/content` answers the bytes with their type, name, size, time, and validator, gated by `Read` on the resource and a `Read` grant on `content`, the route's own field, which the Collection registers with no column behind it (`columns=content` on a read stays a 400; a grant naming it is accepted by `access.MigrateRoles`). On a field, the annotation marks the column holding the store key: `@file` bare serves under `content`, `@file(thumbnail)` under its own segment, and `name:` and `type:` name sibling columns carrying the file's name and media type (a struct may carry several, one per segment). The key column goes off the wire in both directions: never returned on read or list, never accepted on create or update, absent from the TypeScript interface and metadata; a `NOT NULL` key means a row is added by the `@upload` method that stores its file, so `Create` is not registered and the patch handlers refuse a create op naming that way in, while a nullable key leaves `Create` ordinary. On a keyed `@computed` struct, `@file` or `@file(segment)` declares a rendered file: the computed package declares `<Name><Segment>(ctx, key…, qSet *resource.QuerySet[Name], client resource.Client, computedClient *Client) (*resource.Content, error)` beside `Read<Name>`, checked at generation as `Read<Name>`'s callers are, and a nil content is 404. Deleting a row that carries a key, or pointing it at another object, releases the old object: the patch machinery records the key on the transaction and the executor deletes it from the client's store (`resource.WithFileStore`) once the commit lands, from every transaction the application runs; one row owns one object, and a row the database deletes by cascade releases nothing (section 13). Refused at generation, naming the struct: a key-less struct, an unknown sibling, two declarations on one segment, a key, name, or type field that is not a `string` or `*string`, the struct-scope form on a table or view, a declaration under a suppressed read route, and a content function that is missing or has another signature. Examples: [MissionDocument.StoreKey](lodestar/pkg/resources/mission_documents.go), a stored file; [ExpenseManifest](lodestar/pkg/computedresources/expense_manifests.go), a rendered one. |
| `@subjectSet` | user-id field of a `@resource` struct | `name, value: Field` | Declares subject-side set vocabulary: `subject.<name>` in grant conditions is the set of `value:` values on this table's rows whose annotated column matches the requesting user (`crew IN subject.crews`). The annotation designates the user-id column — no separate marker — and is repeatable per anchor; `value:` names the sibling Go field the set yields, dotted to continue through foreign-key hops with the same many-to-one validation as `via:`. **Tenancy:** the rendered subject subquery is tenant-filtered by the anchor resource's own `@domain` binding, so a domain-scoped anchor must declare one — generation rejects it otherwise, because without it `subject.<name>` matches the user's rows from every tenant (a membership held at tenant B would satisfy conditions evaluated at tenant A). A global-scoped anchor is the deliberately shared pattern — a certification earned once applies everywhere — and stays unfiltered. Note the anchor's own binding is what counts: tenancy never arrives transitively from a domain-scoped parent table (see `@domain`). **Type:** the set's comparison type is derived from the `value:` column (the terminal of a dotted value) exactly as `@attribute`'s is, and a grant may test only an attribute of the same type for membership in it; `MigrateRoles` refuses the mismatch at deploy. Example: [SquadronMembership](lodestar/pkg/resources/squadron_memberships.go). |
| `@subjectValue` | user-id field of a `@resource` struct | `name, value: Field` | Declares subject-side scalar vocabulary for threshold comparisons (`amount <= subject.approvalLimit`). Same grammar — and the same tenancy rule — as `@subjectSet`, valid only where the annotated user-id column is the whole key of a unique index, the single-column primary key included, so the database enforces exactly one row per user; a column of a composite key or composite unique index does not qualify. **Type:** the value's comparison type is derived from the `value:` column as `@attribute`'s is, and a grant may compare it only against an attribute of the same type (`now` only against a timestamp-typed value); `MigrateRoles` refuses the mismatch at deploy. |
| `@manualAddResource` | `accesstypes.Resource` constant | `permission[, scope]` | Registers the permission on the resource in the generated Collection for a hand-written route with no generated handler. Repeatable. Scope is `global` or `domain`; omitted means the global default. An `@outlet` annotation on the same constant names the outlets the hand-written route is mounted under, so an outlet-filtered TypeScript target (`ForOutlet`) carries the registration only when it names that outlet; omitted means the default outlet. The constant's value is the resource name and must not contain `:` (reserved for access-defined markers like `accesstypes.GlobalResource`); generation rejects it. The registration reaches the TypeScript constants like a generated one: an `Execute` registration joins `Methods`, any other permission joins `Resources`. |
| `@manualAddResourceSet` | `@resource` struct | comma list of `listHandler`, `readHandler`, `patchHandler`, or `allHandlers` | Declares that hand-written handlers register this resource's permission Sets for the given handler types; validated against the set of generated handlers. |
| `@outlet` | `@resource`, `@virtual`, `@computed`, or `@rpc` struct; `accesstypes.Resource` constant carrying `@manualAddResource` | comma list of outlet names | Names the router outlets the struct's routes — or the constant's hand-written route — are registered under. Outlets are independent registration surfaces with their own route prefixes — the generator emits a `Generated<Name>Handlers` interface and `generated<Name>Routes` function per outlet, so the application composes different authentication and middleware around each (a browser app on one, a machine REST API on another). The default outlet is declared by `generation.GenerateRoutes` and carries the reserved name `default`; additional outlets are declared with `generation.WithRouterOutlet(name, routePrefix)`, and referencing an undeclared name is a generation error. An outlet serving browser sessions declares it (`WithRouterOutlet(name, prefix, Auth(pkg, flavor))` under the generated router, `ServesSessions()` under a hand-written one; section 8): the generated route tables register the permission-digest and user-domains routes under its prefix, and only a session-serving outlet may be the target of a `GenerateTypescript` call (`ForOutlet(name)`), which filters every emitted TypeScript file to that outlet's members — a client for a session-less outlet would have no permission channels and fail closed on every page, so it is a generation error. Without the annotation a struct is on the default outlet only; naming outlets replaces that default, so `@outlet(default, automation)` serves both while `@outlet(automation)` serves the automation outlet only. Consolidated resources get one consolidated patch dispatcher per outlet (`PatchResources`, `Patch<Name>Resources`), each bundling exactly that outlet's members. The generated router tests cover every outlet's routes and additionally prove isolation: a route's path under an outlet it is not attached to must 404. Example: [Consignment](lodestar/pkg/resources/consignments.go). |
| `@permissionScope` | `@resource`, `@virtual`, `@computed`, or `@rpc` struct | `global` or `domain` | Sets the permission scope used by all of the resource's registrations. Default: `global`. It also selects the domain the generated handlers evaluate permissions in: global-scoped handlers pass `accesstypes.GlobalDomain`, while domain-scoped handlers read it from a required `/domains/{domain}/` route segment pair between the route prefix and the resource path (pair-style, so domain values can never collide with resource or method route names). Both names are customizable via the `generation.WithDomainRoute` option, e.g. `WithDomainRoute("organizations", "organizationID")` → `/organizations/{organizationID}/`. Domain-scoped handlers validate the URL's domain against the application's `DomainExists` seam (a `Config` sibling of `UserPermissions`) and return 404 for unknown domains before decoding — the application owns its tenant list. With `generation.WithConcealedDomains()` that seam becomes `DomainVisible(ctx, user, domain)` instead: a domain where the requesting user holds zero grants answers identically to a domain that does not exist (404 on routes, 400 in consolidated op paths), so refusals never confirm a tenant; any grant in the domain restores ordinary 403s (`access.Client.UserHasGrants` answers the foothold question from the in-memory snapshot, so the seam stays legal inside the consolidated mutation transaction). Tenant identifiers must be a single URL-safe path segment and must never contain `:` — that character is reserved for access-defined markers (`accesstypes.GlobalDomain` is `access:global`), and the generated guard structurally rejects any `:`-bearing domain value before `DomainExists` is even consulted, so a misconfigured tenant list can never route a permission check into the global partition. In the consolidated patch handler, a domain-scoped resource's operations carry the domain in the path exactly as the URL grammar does (`{"op":"patch","path":"/stations/station-alpha/berths/…"}`); global operations stay domainless, an unknown domain in an operation path is a 400, and cross-domain batches are legal (each operation is checked in its own partition; the batch is one transaction). The tenant-record pattern — a global resource named like the domain segment, so `/api/stations` lists the tenants while `/api/stations/{stationID}/…` serves tenant-scoped routes — is supported with two validated requirements: the resource must have a single primary key (its operations stay at path depth ≤ 2, domain descents at depth ≥ 3), and its read-route parameter must equal the domain route parameter. Example: [Sector](lodestar/pkg/resources/sectors.go). |
| `@page` | `@resource`, `@virtual`, or `@computed` struct | `default: N` and/or `max: M` | Declares the list's page sizes: the page a request without `limit` receives, and the largest page a request may ask for. Both are positive integers; the default never exceeds the maximum, and a maximum alone must be at least the generator-wide default of 50. Undeclared, the list serves pages of 50 with no maximum. A request over the maximum is refused with a 400 naming it, never clamped, and a resource with a maximum refuses `limit=all`. The maximum is also the switch a picker reads: a resource with none is small enough to load whole, read in one request with `limit=all` and sort optional; a resource with a maximum is paged, one server page at a time, so its list needs an order (`@order`, or `sort` on the request) and a picker over it reads the chosen row by key, which is why a bounded picker source may not suppress its read (`@enumerate`). Declaring the maximum is the deliberate choice; nothing decides at runtime from an observed size. Refused on a `@computed` or `@virtual` struct with no `@primarykey`, naming the struct and the two ways out (declare the key, or drop `@page`): without a key the resource is a whole read-only list, with no read route and no paging, so a page size on it is a contradiction. The generated TypeScript descriptor carries both numbers. Example: [Mission](lodestar/pkg/resources/missions.go). |
| `@order` | `@resource`, `@virtual`, or `@computed` struct | comma list of `Field [asc\|desc]` | Declares the order a list takes when the request carries no `sort`, naming Go fields of the struct; the direction defaults to `asc`. The primary key is appended at runtime so the order is total, and a request's `sort` replaces the declared order for that request. The declaration is optional, and every paged request needs an order from somewhere: a resource that declares none serves a request without `sort` only as the whole list (`limit=all`), which is not sorted — its table or view statement carries no `ORDER BY` and the rows arrive in the database's own order, a computed list keeps the order its body yielded — and refuses a bare GET or a `limit` with a 400 naming the resource and the way out (section 4). A nullable column renders as the plain direction and sorts in its database's own `NULL` placement, so an index on the column serves the order: Spanner places `NULL` first ascending and last descending, PostgreSQL last ascending and first descending. A computed resource follows the same placement: its handler sorts and pages the body's rows where the application's database would. On a computed resource only a leaf field may be named (a nested field is opaque). The generated TypeScript descriptor carries the declared order (`order: [{ field, direction }]`, JSON names), so a browser client knows a request without a `sort` is already ordered and pages by cursor; to a resource that declares none it sends a `sort` of its own or asks `limit=all`. On a table-backed resource with a bare `@domain`, generation warns when no index leads with the tenant column and then these columns in this order and direction, naming the index it wants (section 9). Example: [Mission](lodestar/pkg/resources/missions.go). |

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
| `spanner:"ColumnName"` | every field of `@resource`/`@virtual` structs | Maps the field to its Spanner column. Required — a missing tag or unknown column is a generation error, and field nullability must match the column's: a pointer or a Null wrapper on a nullable column, a plain value on a NOT NULL one. A slice-typed field follows the column's nullability, since a Go slice has one form: the Spanner client reads NULL into a nil slice and writes nil as NULL, so `[]byte` types a nullable `BYTES` column and a NOT NULL one alike, as `[]T` does an `ARRAY<T>` (section 12, nullable slices). A pointer to a slice is refused. |
| `conditions:"…"` | resource fields | Comma-separated list of field conditions, see below. Values match exactly (no spaces); a value the generator does not recognize is refused, with the nearest recognized one suggested. |
| `default_create_fn:"pkg.Func"` | resource fields | The generated create path calls the referenced function to populate the field when the request doesn't supply it. A field with a default function is not treated as required. |
| `output_only_update_fn:"pkg.Func"` | resource fields | The generated update path sets the field by calling the referenced function on **every** update; implies output-only. This is the *mechanical enforcement stamp* — a field whose meaning is "this row was updated", like `UpdatedAt`. A timestamp with domain meaning (a "last serviced" written by one business transition) is not an update function: it is an explicit update in the code that owns the business event — see [Ship.LastRefitAt](lodestar/pkg/resources/ships.go). Declaring an update function on any field also gives the resource a generated `New<Resource>Touch(keys…)`: an update carried entirely by the update functions, running the full update pipeline (permission check, stamps, write conditions, change events) with no caller-set fields — the only way to express "bump the row" (an update patch with no fields set is a silent no-op). Example: [Ship.UpdatedAt](lodestar/pkg/resources/ships.go) using `resource.CommitTimestampPtr`. |
| `allow_filter:"true"` | resource fields | Permits `filter` expressions on a field that isn't indexed (indexed fields are filterable automatically). Copied through to the generated request structs. On a table or view the filter must also touch an indexed field, which the database parse enforces: once one index has narrowed the rows a second is rarely used, and indexes are a scarce commodity on Spanner, so `allow_filter` conserves them. On a resource with a bare `@domain` column the tenant predicate is not that indexed field: it is the baseline every unfiltered page pays, and the rule asks the filter to narrow below it. The TypeScript field metadata says so: `filterable: 'withIndexed'` on a table or view field, `'always'` on a computed resource's, whose List function filters in memory. Refused on a list field (a slice, an array, or a named type over one; a byte slice is one value) on every kind of struct, since Spanner has no array equality and a computed resource's evaluator compares single values: `Ship.CargoBays: allow_filter on a list field; a filter compares single values`. |
| `index:"true"` | `@virtual` struct fields only | Declares the field indexed (filterable/sortable). Rejected on table-backed resources, which get index information from the schema. Refused on a list field: no index serves an `ARRAY` column. |
| `uniqueindex:"true"` | `@virtual` struct fields only | As `index`, and marks the index unique. Refused on a list field, as `index` is. |
| `masking:"positional"` | `@resource` and `@virtual` struct fields | The field's masked cells stay hidden on the wire, but a list orders and filters on the real column, so the page comes off the index and a reader can tell where the hidden values fall. Every untagged field conceals: a sort or filter on it runs over `CASE WHEN <condition> THEN column END` (section 4), which hides where the masked values fall and which no index serves, so a page sorted or filtered by it sorts the tenant's whole partition. Declare `positional` on a field whose rank is not sensitive (a deadline; a fee's rank is) and that a list pays for: named in `@order`, indexed, or `allow_filter`. `masking:"concealing"` is accepted and says the default; another value is refused with the nearest one suggested. Refused as a contradiction on a primary key (keys are exempt from masking), on a field no query sorts or filters by with an index behind it (neither indexed, nor `allow_filter`, nor named in `@order`: there is no index to restore and the rank would be disclosed for nothing), and on `@computed` and `@rpc` structs (conditions are refused at decode there, nothing is ever masked). Copied through to the generated list and read request structs and surfaced in the TypeScript field metadata as `masking: 'positional'`. Deploy-time role validation (`access.MigrateRoles`, `access.ValidateRoles`) warns where a role's conditional grant lands on a concealing field the resource orders by or admits as a sort or filter key and the role's other grants leave the `CASE` standing; the warning names the field, the cost, and the three ways out (grant the field unconditionally, tag it positional, accept the cost for a table that never pages at volume). Example: [Mission.Deadline](lodestar/pkg/resources/missions.go). |

Values recognized in a `conditions` tag:

- `immutable` — the client sets the field on create, and it can never change afterward:
  an update touching it is rejected with a 400 (the generator emits `immutable:"true"`
  into the patch request struct — you write the condition, never the emitted tag), and
  the generated Collection never exposes `Update` on the field's tag as grantable.
- `pii` — marks the field as personally identifiable. Emitted as `pii:"true"`, surfaced
  in the TypeScript metadata, and the field is rejected in URL `filter` expressions
  (filter via the POST body instead, which doesn't land in access logs).
- `input_only` — write-only: the client sets the field on create and update, and the
  server never returns it. Read and list structs get `json:"-"`, and a `columns=<field>`
  naming it answers 400. The generated TypeScript row interface omits the property, since
  no list or read response carries it; the metadata entry stays, flagged
  `writeOnly: true`, so a config-driven form still renders the input; and the Create and
  Patch shapes and the field-name constants carry it, since a client writes it and a
  grant check names it. Example:
  [DistressCall.Transcript](lodestar/pkg/resources/distress_calls.go).
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
| `index:"true"` | From the schema's indexes: the field's column leads some index of the table, the primary key and Spanner's foreign-key backing indexes included, or, on a resource with a bare `@domain` column, sits directly after the tenant column in one, since the list binds the tenant by equality and the index then seeks on both. A trailing key column anywhere else and a `STORING` column carry no tag: a filter on either alone would scan the index. On a virtual resource the tag is authored (`index`/`uniqueindex`, section 2). Makes the field filterable; `sort` is not gated on it. |
| `allow_filter:"true"` | Copied from the source struct; makes an unindexed field filterable. |
| `pii:"true"` | From `conditions:"pii"`; the field is rejected in URL filter expressions. |
| `masking:"positional"` | Copied from the source struct; a sort, filter, or cursor on the field runs on the real column while the cell stays masked in the output. Absent on a concealing field. `positional` is the only value written; any other value in a request struct is a startup error (the stale-struct guard). |
| `sqltype:"STRING(64)"` | The column's declared type, verbatim from the schema, on a patch request-struct field whose value the decoder sizes before anything is buffered (section 11): a string-kinded field on `STRING(n)`, `[]byte` on `BYTES(n)`, a decimal on `NUMERIC`, and a slice of one of those, named or not, on the matching `ARRAY<…>`. Absent on `MAX` columns, on keys and output-only fields (hidden from the patch wire), and on every other type. A value the runtime cannot pair with the field's type is a startup error (the stale-struct guard). |
| `nullable:"true"` | On a patch request-struct field typed by a slice whose column allows NULL, and nowhere else: a Go slice has one form, so the decoder cannot read the fact off the field's type as it does off a pointer or a Null wrapper. The decoder accepts a JSON `null` for the field and stores the nil slice, which the Spanner client writes as NULL (section 12, nullable slices); a slice field without the tag refuses `null` with `<field> cannot be null`, since its column is NOT NULL. `true` is the only value written; any other value, or the tag on a field that is not a slice, is a startup error (the stale-struct guard). |

## 4. Reserved query parameters

List/Read requests accept exactly these query parameters — anything else is a 400, and
none of them can be used as field names in filters:

| Parameter | Meaning |
| --- | --- |
| `columns` | Comma-separated JSON field names to return; omitted means all accessible fields. Leaving out a sort field or the primary key changes nothing about paging: the cursor reads those keys from the statement, not from the returned row. |
| `filter` | Filter expression over indexed/`allow_filter` fields, e.g. `name:eq:Vanta`. Operators: `eq`, `ne`, `gt`, `lt`, `gte`, `lte`, `in`, `notin`, `isnull`, `isnotnull`. On POST query routes the filter may be sent in the body as `{"filter": "…"}` instead (required for `pii` fields), but not in both places. The expression is validated when the request is decoded, with the rest of the request's shape: a field that is unknown or not filterable, a malformed condition, or a value that does not fit its field is a 400 before any permission check runs, any query executes, or a computed resource's List function is called. A table filter must also touch at least one indexed field, which the database parse enforces; an indexed field is one whose column leads an index or, on a bare-`@domain` resource, follows the tenant column in one (section 3), and the tenant predicate itself does not count. The generated field metadata states the same eligibility, so a browser draws a filter control only where the server will answer: `filterable: 'always'` on an indexed table or view field and on a computed resource's `allow_filter` field, `'withIndexed'` on a table or view `allow_filter` field, and nothing on a field a filter may not name. |
| `sort` | Comma-separated `field[:direction]` entries, e.g. `name:asc,rank:desc`; direction is `asc` (default) or `desc`. Omitted, the resource's `@order` applies. Every paged request needs one or the other: with neither, a request that is not `limit=all` is refused with a 400 naming the resource and the way out (`<Resource> declares no order; add a sort, or ask limit=all`; on a resource with a maximum, `<Resource> serves at most N rows per page and declares no order; add a sort`), and `limit=all` alone is served unsorted. |
| `limit` | The page size: rows returned per request. Omitted, the resource's declared default applies (`@page`, generator-wide default 50). A value over the resource's declared maximum is refused with a 400 naming the maximum, never clamped; `0` is refused. `limit=all` returns every row with no `Link` header, is permitted only on a resource that declares no maximum, and is the one list request that needs no order (`sort`). A `@computed` or `@virtual` resource with no `@primarykey` is served whole on every request, `limit=all` or none: a numeric `limit` on it is refused with a 400 (`<Resource> declares no primary key, so its list is served whole and does not page; drop the limit, or declare @primarykey to page`). |
| `cursor` | The page position, copied from the `Link` header of the page that issued it (`rel="next"` or `rel="prev"`); never assembled by a client. A sealed token carrying the boundary row's sort values, the direction, and a fingerprint of the resource, scope, filter, sort, and limit — a cursor altered, sealed by another application, or presented with a different query, tenant, or page size is a 400. A cursor is valid for the database that issued it: the seal binds it to the application, and its position is read back in that database's `NULL` placement. A request with a cursor carries the same `sort` and `limit` as the page that issued it. Every paged request carries an order (`@order` or `sort`), so every page has its `Link` relations; a cursor never names a position in an unsorted list, because such a list is served only whole. A cursor on a resource with no `@primarykey` is refused with a 400 naming the missing key: the list has no row identity to anchor a position on, so it is served whole (`limit`). |
| `count` | `count=true` on a first page asks for the total number of rows the same WHERE admits, answered in the `Total-Count` header; later pages carry no count and refuse the parameter. A whole list (`limit=all`, or a key-less resource) answers it too. A request that does not ask pays nothing. |
| `offset` | Removed. A request carrying it is refused with a 400 naming `cursor` as its replacement; pages are positioned by the row the cursor names, not by a count of rows to skip. |
| `capabilities` | Comma-separated write permissions (`Create`, `Update`, `Delete`, `Execute`) to evaluate per row — the §13 capability envelope. Each returned row gains the reserved `zzCapabilities` property: `Update` carries the positive list of editable JSON field names, `Delete` a boolean, `Execute` the positive list of `@target` methods that apply to the row — a declared `@transition` requires the row's pre-image state in its `from` set, a conditional Execute grant ANDs its condition into the same boolean (a plain `@target` method's is the condition alone; unconditional plain methods are structural, no SQL), and the user holds the method's Execute grant — and `Create` the positive list of workflow member resources the user may create beneath the row (§11): the members whose immediate `@stateRoot` hop is this resource, gated by the user's member Create grants, a conditional grant's state terms evaluated against this row's own uniform state binding while terms the parent row cannot answer count potentially-true (an unconditional member grant is structural, no SQL). Advisory hints computed from the same row image and decision instant as the read (conditions render as booleans in the same statement; pure RBAC adds no SQL; a `new.`-referencing term counts potentially-true while the rest of its condition still renders). Enforcement is unchanged. |

A sort or filter runs over the projection the caller can see. A field the caller is
denied cannot be sorted or filtered on: the request is a 403 naming the field, because
the order or the membership of the result would leak its values. A conditionally
granted field can be: the statement orders and filters on `CASE WHEN <condition> THEN
column END`, so a cell the caller may not see is `NULL` for the query — it sorts in the
`NULL` region where the database places it (Spanner first ascending and last descending,
PostgreSQL the reverse, the same as a genuinely `NULL` cell), matches `isnull` and
nothing else, and a cursor positioned on it carries the `NULL` key.
A condition permits only when it is TRUE, and evaluation is three-valued as in SQL: a
comparison against a missing value (a `NULL` column, a `NULL` foreign key or a join path
that reaches no row, a subject value whose requester has no anchor row) is UNKNOWN,
`IN` and `NOT IN` against a missing value are UNKNOWN whether the list is literals or a
subject set, `IS NULL` is TRUE and `IS NOT NULL` FALSE; UNKNOWN refuses exactly as FALSE
does — the row is filtered, the cell masked, the write refused, the capability withheld.
A number literal takes the attribute's storage type: exact against an `INT64` or a
`NUMERIC` column, a double against a `FLOAT64` one. A join-path attribute renders as one
scalar subquery per hop, a subject set as an `EXISTS` guarded by the attribute's nullness,
so the database's own three-valued logic gives these readings; the reference evaluator in
[`conditiontest`](../accesstypes/condition/conditiontest) states them in Go and the
semantic differential in this package's tests compares the rendered SQL against it on the
emulator over random conditions and rows.
When the field's condition covers the whole row predicate (every returned row shows
the field) the raw column is used instead. A grant whose condition implies the field's
counts as covered, under a closed set of same-attribute rules: an equality or `IN` list
whose values sit inside the field's `IN` list, a `!=` or `NOT IN` naming every value the
field's does, and a conjunction one of whose terms does; anything else must spell the
field's condition the same way ([`condition.Implies`](../accesstypes/condition/cover.go)).
A `CASE` in `WHERE` or `ORDER BY` cannot use
an index, so only such a field pays for it, and it pays on every page: the partition
sorts whole ([finding 5](lodestar/perf/REPORT.md)). That is the concealing behavior,
and it is the default for every field. A field declared `masking:"positional"` (section
2) keeps its cell hidden but sorts, filters, and pages on the real column: the index
serves the page, and the field's rank is disclosed. Its cursor still carries the
boundary row's real value: the statement selects it a second time under a reserved
`zzCursor…` column that never reaches the wire, and the token carries it sealed. Every
sort key the request did not select rides the same column — a `columns=` list that
leaves out the sort field or the primary key, or an `@order` field the grid does not
show — so the cursor carries what the statement ordered by (a concealing key's visible
projection, `NULL` where the cell is masked) and the row data stays exactly what was
selected. `index:"true"` stays the declaration of which fields may be filtered. A `@computed` resource evaluates no conditions at read
time, so its sort and filter fields still require an unconditional grant.

A paged list answers with headers beside its JSON array body. `Link` (RFC 8288)
carries a complete URL per relation that exists — `rel="next"` and `rel="prev"`, each
the request's own URL with `cursor` set — so the first page has no `prev` and the last
no `next`, and a client follows the URLs as given. `Total-Count` carries the total when
the first page asked `count=true`. Every paged request carries an order (`@order` or
`sort`; without one it is refused, see `sort`), so a page always has the `Link`
relations that exist. `limit=all` answers with no `Link`: the whole list, unsorted
when neither an `@order` nor a `sort` orders it, a table statement then carrying no
`ORDER BY` and a computed list keeping its body's order, with `Total-Count` when asked. A
`@computed` or `@virtual` resource with no `@primarykey` is that whole list on every
request: it has no read route, no page, and no row identity, and a numeric `limit` or a
`cursor` on it is a 400 naming `@primarykey` as the way to page. A list served cross-origin must
add `Link` and `Total-Count` to the CORS exposed headers, or the browser client cannot
read them.

## 5. The permission endpoints

Every generated router registers two library-owned endpoints on the default outlet
(applications wire nothing):

```
GET /<prefix>/permission-digest              → the session user's global digest
GET /<prefix>/permission-digest?domain={id}  → one tenant partition's digest
GET /<prefix>/user-domains                   → the domains the session user can see
```

An additional outlet that serves sessions (`Auth(...)` under the generated router,
`ServesSessions()` under a hand-written one) gets the same two routes under its own
prefix, served by the same generated handlers behind the outlet's session middleware;
an outlet without the declaration gets neither (the generated router tests prove its
prefix 404s them).

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
  body runs, the body records the minted keys, and the transaction's commit is what
  claims them; on any failure before commit the frame deletes the objects it streamed.
  The body never writes the store. The other direction is the executor's: a row deleted
  or pointed at another object releases the old object, which the executor deletes from
  the client's store once the commit lands (section 13). What neither covers is a crash
  between the stream and the commit, or between the commit and the delete, which leaves
  an object no row claims; the application's sweep removes such objects once they are
  older than its own window, and the window is the application's decision. The sweep is
  the safety net, never the mechanism.

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

## 8. The generated router

`GenerateRoutes` emits the route tables: one `generated<Outlet>Routes` function and
`Generated<Outlet>Handlers` interface per outlet, plus `NewTestRouter`, which serves them
bare for test composition. What sits in front of them in production is the router, and
`generation.GenerateRouter()` emits that too, beside the tables in the same package:
`zz_gen_router.go` and its proof, `zz_gen_router_test.go`. Without the option the
contract is unchanged: generated route tables plus a hand-written router.

**What the program declares.** Two facts per outlet that the generator cannot derive.
The auth behind a session outlet, `generation.Auth(importPath, flavor)`: the auth package
(the package exporting `Name`) and how its people sign in, `Password`, `OIDCGoogle`, or
`OIDCAzure`. A machine outlet declares `generation.APIKey()` instead. And the browser
application an outlet serves, when it serves one: `generation.WebApp(mountPath)`, `"/"`
for the application at the root, `"/portal"` for one under a path. The default outlet
declares them on `GenerateRoutes`, the others on `WithRouterOutlet`. A session `Auth`
makes the outlet serve sessions exactly as `ServesSessions()` does; `ServesSessions()`
stays for applications that keep a hand-written router. Lodestar's program:

```go
generation.GenerateRouter(),
generation.GenerateRoutes("pkg/router", "api",
	generation.Auth("github.com/cccteam/ccc/resource/lodestar/pkg/auth/crew", generation.Password),
	generation.WebApp("/"),
),
generation.WithRouterOutlet("droids", "droids", generation.APIKey()),
generation.WithRouterOutlet("portal", "portal/api",
	generation.Auth("github.com/cccteam/ccc/resource/lodestar/pkg/auth/members", generation.OIDCGoogle),
	generation.WebApp("/portal"),
),
```

Under `GenerateRouter` every outlet says how it authenticates, one way: an outlet with
neither `Auth` nor `APIKey`, or with both, fails generation, as do a machine outlet with
a `WebApp`, two outlets on one mount path, and a browser application mounted under an
API prefix. Without the option the three declarations are refused rather than ignored.

**The generated file** holds four things, in this order. The chain comment opens it as
the package documentation: every outlet's middleware in order, outermost first, one line
per group, each chain followed by what it stands in front of. It is the review surface:
what sits in front of every REST handler, readable without the code beneath it.
`Handlers` is the full surface the router needs: the generated handlers of every outlet;
the default outlet's session handlers embedded (`session.PasswordAuthHandlers` for
Lodestar) and each additional session outlet's through a getter named after it
(`Portal() session.OIDCGoogleHandlers`); `BindAuth(name)` only when more than one session
auth is declared, so a misspelled or removed auth package is a compile error at
`BindAuth(crew.Name)`; `<Outlet>Auth(next)` per API-key outlet; `LoggerMiddleware`,
`SecurityHeaders`, `NoCaching`, and `CompressionMiddleware`; and per `WebApp` a
`DeepLink` and `Assets` pair, prefixed with the outlet's name for an additional outlet
(`PortalDeepLink`, `PortalAssets`). Route-parameter capture (`httpio.WithParams`) is
mounted by the router itself. `Hooks` is a struct, never a map, so an outlet added or
removed surfaces as a compile error at every hook that names it:

```go
type Hooks struct {
	// Outermost runs ahead of the logger on every request: tracing belongs here.
	Outermost []func(http.Handler) http.Handler
	// Root registers routes outside every outlet: health checks, webhooks, scheduler
	// triggers. They sit behind the every-request chain and nothing else.
	Root func(r chi.Router)
	// One field per outlet, named after it. The hook runs inside the outlet's
	// authenticated group (or the API-key group): r already sits behind the outlet's
	// guards, and generated registers the outlet's generated routes. Nil registers them
	// directly.
	Default func(r chi.Router, generated func(chi.Router))
	Portal  func(r chi.Router, generated func(chi.Router))
	Droids  func(r chi.Router, generated func(chi.Router))
}
```

Hooks compose inward only. A hook adds middleware and routes under the guards it is
handed and chooses where inside its own group the generated routes register; it never
sees the outer router, so no generated route can be lifted out from behind session
validation or the XSRF guard. A hook must call `generated` exactly once; the router
refuses one that does not, at construction. `New(h Handlers, hooks Hooks) *chi.Mux` is
written linear and inline, the way a hand router reads: `hooks.Outermost`, the logger,
the security headers, and parameter capture; `hooks.Root`; one group per outlet, top to
bottom; a not-found handler per outlet prefix, so an unknown API path is 404 and never a
browser application's entry document; then the web apps, longer mount paths first so
`"/"` is the catch-all.

A session outlet's group is `BindAuth(<pkg>.Name)` (with two session auths), `NoCaching`,
`CompressionMiddleware`, `StartSession`, `SetXSRFToken`; the flavor's login routes under
the prefix; then the authenticated sub-group, `ValidateSession` and `ValidateXSRFToken`
around the hook and the generated routes. The flavor's routes: Password mounts `POST
user/login`, `GET user/session`, `DELETE user/session`; OIDCGoogle mounts `GET
user/login`, `GET user/callback`, and the two session routes; OIDCAzure adds `GET
user/logout`, the directory's front-channel logout. An API-key outlet's group is
`NoCaching`, `CompressionMiddleware`, `<Outlet>Auth`, then the hook and the routes: no
session handling and no XSRF guard.

**The generated test** drives every generated route through `New` with recording stubs
and asserts the middleware each request passed through, in order, for its outlet; that
each flavor's session routes answer behind the group and before the guards; that under
every prefix an unknown path is 404 and nothing under one outlet's prefix reaches another
outlet's group; that each browser application answers at its mount path through its
deep-link rewrite; and that the hooks sit where the chain comment says. The chain
comment is proven, not stated. `NewTestRouter` and the route tests are unchanged, and the
application's own hooks are the application's suite's to exercise.

**Adopting it.** Lodestar keeps one hand file in its router package, `hooks.go`: an
`AppHandlers` interface (the generated `Handlers` plus its own routes) and an
`AppHooks(h AppHandlers) Hooks` function, and `main` calls `router.New(app,
router.AppHooks(app))`. An application whose router is exactly the base passes
`router.Hooks{}`. The escape hatch stays: an application that needs something the shape
cannot carry removes the option and hand-writes its router on the generated route
tables, losing only the boilerplate and the generated chain test.

**The generated authorization matrix.** `GenerateHandlerTests(dir)` emits, beside the
emulator bootstrap, a test that drives every generated route through `NewTestRouter`:
without the required permission each must fail closed, and with exactly that permission a
list or read must reach data access. The application hand-writes one function,
`newTestHandler`, wiring its App to the scripted grants; Lodestar's is
[test/authz/harness_test.go](lodestar/test/authz/harness_test.go). The scripted grants
are unconditional and the schema is empty, so the matrix pins the endpoint gate alone: a
granted case means the request passed the gate, never that a row is visible, and a
conditional grant would pass the gate exactly as an unconditional one does. What a
condition admits or refuses is proven by the application's integration suites over seeded
rows and the real permission engine provisioned from its role files, as Lodestar's
[test/integration](lodestar/test/integration) suites do.

## 9. Indexes for the shapes the package injects

Tenancy, grant conditions, subject sets and values, the visible projection, and the write
checks all land inside the application's queries. Measured on a real Spanner instance at
half a million rows ([lodestar/perf/REPORT.md](lodestar/perf/REPORT.md)), they need this
much from the schema:

- A resource with a bare `@domain` column that is listed at volume wants an index on the
  tenant key followed by its `@order` columns. Spanner's foreign-key backing index on the
  tenant column alone drives the list, but every page then sorts the whole partition; the
  composite index reads the page's rows and nothing else (52 rows against 20,060 at
  10,000 rows per tenant). Generation warns at every run for a listed resource in this
  shape whose table has no index leading with the tenant column and then the `@order`
  columns, in order and direction, and names the `CREATE INDEX` it wants; the primary
  key is not named because Spanner appends it to every secondary index. A resource
  with no `@order` is silent: its sort-less list is not sorted, and the backing index on
  the tenant column serves it.
- A nullable `@order` column renders as the plain direction with no stated `NULL`
  placement, so the same composite index serves it. Each database keeps its own placement
  (Spanner sorts `NULL` first ascending and last descending, PostgreSQL the reverse) and
  the keyset cursor follows it across the `NULL` boundary; a cursor is read by the database
  that issued it.
- A filter seeks the same way. A filter on a column that leads an index, or that follows
  the tenant column in one on a bare `@domain` resource, reads the index from that
  prefix, and those are the columns the generated list struct tags `index:"true"`
  (section 3); a filter on any other column alone is refused at decode rather than
  scanned, and `allow_filter` admits one beside an indexed field. A global request
  carries no partition predicate, so for it the tenant-second seek is an index scan,
  the same cost class as its ordered list, which already sorts the whole table.
- A resource with join-path tenancy, `@domain(via: ...)`, is scanned whole when listed:
  every row of the table, all tenants, with a lookup per row up the path. No index on the
  child changes that, because no column on the child names the tenant. Join paths suit
  writes and small or parent-scoped tables; a table listed at volume carries its tenant key
  on the row. Generation warns at every run for every listed join-path resource, so the
  application decides knowingly. The warnings are informational, never a refusal, and the
  generator returns them through `Generator.Warnings()`; an application's generate program
  prints them after a successful run, as Lodestar's does.
- Subject-set conditions are key lookups per row against the anchor (its key leads with
  the compared column and the user), so their cost is the partition's size per page, not
  the anchor's. Subject values are one row through the unique index generation requires.
- A sort on a conditionally visible column runs over `CASE WHEN <condition> THEN column
  END`, which no index serves: the page sorts the partition. The `CASE` is dropped when
  the field's condition covers the whole row predicate (a role whose every grant on the
  resource carries the one condition, or a condition that implies it: an equality inside
  the field's `IN` list prunes too), and a field declared `masking:"positional"` never
  renders one; `access.MigrateRoles` warns, per role, where a concealing sort or filter
  key keeps its `CASE`. Lists that page at volume sort on unconditionally visible
  columns, on positional ones, or on columns whose condition the row filter proves.
- Write and insert checks are point lookups at any volume.
- After a bulk load, `ANALYZE`; Spanner otherwise refreshes statistics about every three
  days, and a plan can flip with them.

## 10. Refused commits

Spanner checks foreign keys, interleaved parents, `NOT NULL`, column lengths, primary and
unique keys, and `CHECK` constraints for buffered mutations when the transaction commits,
so a write the schema refuses surfaces from the commit, not from the patch that caused it.
`SpannerClient.ExecuteFunc` answers such a commit with a 4xx and a message the library
composes itself. The translation keys on the gRPC code alone, never on the message text,
which differs between the emulator and the service and is not documented. The message
names only the resources whose patches the transaction buffered, in the order they were
buffered, and never the referencing table, the constraint, the index, the key, the value,
or a count:

| gRPC code | The transaction buffered | Status | Message |
| --- | --- | --- | --- |
| `FailedPrecondition` (a delete of a row other rows still reference through a foreign key without `ON DELETE CASCADE` or as an interleaved parent under `ON DELETE NO ACTION`; a write whose foreign key names a row that does not exist; a required column left empty; a string longer than its column's declared length) | Deletes on one resource | 409 | `Hangars: this record cannot be deleted while other records still reference it.` |
| | Deletes on several resources | 409 | `Hangars, Ships: a record cannot be deleted while other records still reference it.` |
| | Creates or updates only | 409 | `Ships: a referenced record does not exist, or a value is too long for its field.` |
| | Deletes and writes together, or nothing buffered through the transaction wrapper | 409 | `The request could not be applied: a deleted record is still referenced, or a referenced record does not exist.` |
| `AlreadyExists` (a duplicate primary key; a duplicate unique-index value) | Any create | 409 | `Ships: a record with this key or a unique value already exists.` |
| | Updates only | 409 | `Clients: a unique value already exists on another record.` |
| | Nothing buffered | 409 | `The request could not be applied: a record with this key or a unique value already exists.` |
| `OutOfRange` (a violated `CHECK` constraint) | Any write | 400 | `Missions: a value is outside the range the record allows.` |
| | Nothing buffered | 400 | `The request could not be applied: a value is outside the range the record allows.` |
| `NotFound` (an update of a row that does not exist; an interleaved child whose parent does not exist) | Updates only | 404 | `Clients: this record does not exist.` |
| | Any create | 409 | `RefitTasks: a referenced record does not exist.` |
| | Nothing buffered | 409 | `The request could not be applied: a record to update does not exist, or a referenced record does not exist.` |
| anything else (`InvalidArgument`, `Internal`, `Aborted`, …) | | 500 | passes through unchanged |

"Create" is a create or a create-or-update; a touch is an update. Deletes cannot cause any
code but `FailedPrecondition`, so deletes buffered beside writes do not change the other
codes' sentences. A tracked resource's change-event rows do not count among the buffered
patches. An error the transaction function itself returns passes through unchanged, whatever
its code.

What the library does not do: it reads nothing before buffering to name the offending
field. A read inside the transaction does not see the transaction's own buffered writes, so
two creates sharing a value in one batch would pass such a check and fail at commit anyway,
and concurrent requests would race past it; the commit translation is needed regardless.
The application's validators (`@validateCreateType`, `@validateUpdateType`) are where a rule
is answered as 400 naming the field before anything is buffered; the commit translation is
the backstop for what they do not cover. A value the decoder can size never reaches the
commit through the API: a string longer than its `STRING(n)` column, a byte slice longer
than its `BYTES(n)` column, and a decimal beyond `NUMERIC`'s digits answer 400 naming the
field at decode (section 11). The 409 for a too-long string remains for `STRING(MAX)`,
whose ceiling the decoder does not check, and for application code that drives the patch
layer directly.

Two consequences to know:

- A unique index is global across tenants. The 409 for a duplicate unique value confirms
  that the value exists somewhere, in any tenant; that is the schema's choice in declaring
  the index, not the message's, which names no tenant, row, or value.
- `NotFound` is also Spanner's code for an unknown column or table. A deployment whose
  schema disagrees with the generated code answers 404 or 409 with Spanner's text in the
  server log instead of 500. A schema mismatch fails every write that touches the column,
  so it does not hide behind one request.

The tenancy check's 404 for a referenced row outside the request's partition, and the 404
a tenanted or tracked update answers when its row is missing, are unchanged; they run before
the commit, so the same request answers 404 whatever the resource's shape. A request that
deletes children and their parent in one transaction still succeeds in either order,
because nothing is checked before the commit.

## 11. Value limits from the schema

A value the column cannot hold answers 400 naming the field when the request is decoded,
before anything is buffered and before any permission check. The generator carries each
sized column's declared type onto the patch request structs as `sqltype:"…"` (section 3),
the runtime derives the rule from it when the handler's Set is constructed, and the
decoder applies it after the per-field refusals (an unknown field, an immutable field on
update, a null into a field whose column is NOT NULL: a value with no null form, or a slice
without the `nullable:"true"` tag) and before the application validator. A limit is
a fact about the wire value alone, the same class as `cannot be null`; naming it before the
permission check discloses schema shape the metadata already publishes, nothing about rows.

The tag is written exactly where the field's Go type and the column type together have a
rule:

| Go type | Column type | Rule |
| --- | --- | --- |
| `string`, a named string type, `*string`, `spanner.NullString` | `STRING(n)` | at most `n` code points (`utf8.RuneCountInString`); Spanner counts code points, so a combining mark counts and a CJK character counts once |
| `[]byte` | `BYTES(n)` | at most `n` bytes |
| `decimal.Decimal`, `*decimal.Decimal`, `decimal.NullDecimal`, `spanner.NullNumeric` | `NUMERIC` | trailing zeros trimmed, then at most 29 digits before the decimal point and 9 after (GoogleSQL `NUMERIC` is 38 digits of precision with 9 of scale); `1.1234567890` is accepted, `1.1234567891` is not |
| a slice of a checked type, named or not (`[]string`, `type Marks []string`, `[]decimal.Decimal`) | `ARRAY<…>` of the matching type | each element by its rule |

No tag, and no check, on `STRING(MAX)` and `BYTES(MAX)` (Spanner's ceiling of 2,621,440
characters stays a 409 at commit, section 10); on `ccc.UUID` and `ccc.NullUUID`, whose
unmarshal fixes the shape; on `INT64`, `FLOAT64`, `BOOL`, `DATE`, `TIMESTAMP`, and `JSON`
columns; on keys and output-only fields, which the patch wire never carries; on `@virtual`
fields (a view has no schema types; an `@rowsOf` write lands on the table resource, which is
tagged); and on `@computed` and `@rpc` structs, which have no schema. A null in a nullable
wrapper, or in a slice whose column allows NULL, passes: there is nothing to size. Application code that drives `PatchSet.Set`
directly is trusted and keeps the commit backstop.

One message names **every** offending field the request carried, in struct-field order,
joined with `; `, so a form fixes everything in one round trip. Field names are the JSON
names:

- `name is limited to 64 characters`
- `codes: each value is limited to 4 characters`
- `blob is limited to 4 bytes`
- `fee is limited to 29 digits before the decimal point and 9 after`

A `PATCH` checks only the fields it carries. The consolidated handler decodes each
operation through the same function.

The tag follows the Go type and the column alone: a type's name changes nothing, and a
`@typescript` declaration on the type (section 1) changes the interface, never the limit.

The TypeScript field metadata carries the character limit as `maxLength` for the
string-kinded pairs (`STRING(n)`, and per element for `ARRAY<STRING(n)>`) on the fields
whose display type is `string`, `string[]`, or `enumerated` (a picker's key is a string the
column sizes), so a form control refuses before sending; nothing is emitted for bytes or
`NUMERIC`. A field typed by a declaration importing a name (`@typescript(Name, from:
"module")` over a named string or a named slice on a sized column) has display type
`object`: it carries the tag, since the decoder sizes the column's value, and no
`maxLength`, since the interface type is not a string and a form would count the wrong
thing. JavaScript measures a string in UTF-16 units, so a form refuses a little early on
astral characters and never accepts what the server refuses. The framework-neutral client
does not pre-check a string: the server stays the single authority. Example:
[Ship.Registry](lodestar/pkg/resources/ships.go) on `STRING(16)`, and
[Mission.Fee](lodestar/pkg/resources/missions.go) on `NUMERIC`.

## 12. TypeScript types for columns

A field's TypeScript type comes from its Go type alone, and a field whose type reaches
no TypeScript type fails generation naming the resource, the field, the Go type, and the
fix. Nothing falls back to `string`. The same rule types a table column, a view column, a
computed field, and an RPC field, so one Go type is one TypeScript type wherever it
appears.

**How a type resolves.** After aliases are read through (`type NullKindID =
ccc.NullEnum[KindID]` is read as the `NullEnum`), and one pointer is read through (a
pointer column is nullable, as before; a pointer to a slice is refused, below), the
generator tries, in order:

1. the built-in table, by the type's qualified name;
2. a generic row, by the type's origin: `ccc.NullEnum[T]` is `T`'s type;
3. a `@typescript` declaration on the type's declaration (section 1);
4. a basic type, or a named type over one, by the basic type's row (`type KindID string`
   is `string`);
5. a byte slice: an unnamed `[]byte`, or a named slice over `byte` with no JSON methods,
   is the `bytes` leaf (below).

A field that resolves here is a leaf. A field that does not is read once more as a list:
one slice level is stripped from a `[]T`, an array, or a named slice type (`type
Attachments []Attachment` is `Attachment[]`), and the element resolves by the same steps.
An element that is a struct with no declaration is **derived** (below). Anything else is
refused:

- `Ships.Manifest: resources.Manifest has no TypeScript type; declare the type's
  TypeScript form with @typescript(Name, from: "module") on its declaration, or use a
  struct for a derived interface`

Every offending field in a run is reported together.

**The built-in table.** `string`, `bool`, and the numeric types by their names;
`ccc.UUID` and `ccc.NullUUID` (`uuid`, a `string` in the interface); `decimal.Decimal` and
`decimal.NullDecimal` (`number`); `time.Time` (`Date`); `civil.Date` (`civilDate`, a
`Date` in the interface); the Spanner Null wrappers (`spanner.NullString` is `string`,
`NullInt64`, `NullFloat32`, `NullFloat64`, and `NullNumeric` are `number`, `NullBool` is
`boolean`, `NullTime` is `Date`, `NullDate` is `civilDate`); `securehash.Hash` (`string`,
its text form); and `spanner.NullJSON` (`unknown`, a value with no fixed shape, display
type `object`). Nullability keeps coming from the schema, so a nullable `spanner.NullBool`
column renders `nullboolean` exactly as `*bool` does.

**The `database/sql` Null wrappers are refused.** A field typed `sql.NullString`,
`sql.NullInt64`, any of their six siblings, the generic `sql.Null[T]`, or a pointer to
one fails generation on every path (a table or view column, a computed field, an RPC
request or result field). None of them writes its own JSON, so `encoding/json` carries
the wrapper as `{"Int64":7,"Valid":true}` where the interface would promise `number`,
refuses a bare `7` into it, and reads `null` as the zero value silently; the field lies
in both directions. The pointer to the value (`*int64`) carries `null` on the wire and
through the patch decoder, and is what every adopter writes, so the refusal names it.
The rule is keyed on the package and the `Null` prefix, so a wrapper Go adds later is
refused too; the Spanner wrappers, `ccc.NullUUID`, `ccc.NullEnum[T]`, and
`decimal.NullDecimal` keep their rows, since each writes the value or `null` through its
own JSON methods. A standard-library type cannot carry a `@typescript` declaration, so
the message offers none:

- `Ships.Count: sql.NullInt64 has no JSON form of its own (encoding/json writes it as
  {Int64, Valid}); type a nullable column with the pointer *int64`

**Byte slices.** A `[]byte` is one leaf, never a list of numbers, because that is what the
wire carries: `encoding/json` writes a byte slice as a base64 string and a nil one as
`null`, and reads a base64 string back. The field is `string` in the interface and
`bytes` in the metadata, on every path (a table or view column, a computed field, an RPC
request or result field), so a browser knows the value is not text: a grid shows its size
or offers a download rather than the base64, and a form draws no free-text control for it
(a typed word fails the server's base64 decode with a 400). The line is the one the value
limits draw (section 11): an unnamed `[]byte`, a named slice type over `byte` (`type Digest
[]byte`) with no JSON methods, and, on an RPC or computed field, a pointer to either (on a
table or view column a pointer to a slice is refused, below); a `[][]byte` (an
`ARRAY<BYTES>` column) is `string[]` with display type `bytes[]`. Not on it: a byte array (`[N]byte` stays
`number[]`, since `encoding/json` writes an array as an array), a named type carrying
`@typescript` (it keeps what it declares), and one writing its own JSON
(`json.RawMessage` is refused as before, since it writes JSON, not base64). `maxLength`
is never emitted for bytes (section 11); a byte limit can ride the `bytes` type later.

**Nullable slices.** A slice-typed column takes its nullability from the schema, since a
Go slice has one form: the Spanner client reads a NULL `BYTES` or `ARRAY<T>` column into a
nil slice and writes a nil slice as NULL, and `encoding/json` carries nil as `null`. So
`[]byte` types a nullable `BYTES(n)` column and a NOT NULL one alike, `[]int64` an
`ARRAY<INT64>` either way, and the nullability check (section 2) leaves slice fields out.
The generated metadata says `required: false` for the nullable column and `required: true`
for the NOT NULL one without a default; the patch request struct carries `nullable:"true"`
on the nullable one (section 3), so a `null` in a PATCH clears the column, while a `null`
into the NOT NULL one answers `cannot be null` at decode. A pointer to a slice is refused
on a table or view column, naming the plain slice: the client decodes through one pointer
and no more, so `*[]string` fails on the column's first read, NULL or not, and the message
quotes the client's own words for the column's type (a view has no schema type, so there
the quote is left out):

- `Squadrons.Callsigns: *[]string cannot be read by the Spanner client (type **[]string
  cannot be used for decoding ARRAY[STRING]); type the column with the plain slice
  []string, which reads NULL as nil`

Element nullability is not carried: an `ARRAY<INT64>` row holding a NULL element fails at
read into a `[]int64`. Example: [Squadron.Callsigns](lodestar/pkg/resources/squadrons.go),
an `ARRAY<STRING(16)>` column that is NULL until the marshal files the squadron's callsigns
and `[]` for a squadron that flies silent.

**The display-type vocabulary.** Beside its interface type, every field carries a display
type in the generated metadata (`FieldMeta.displayType`, `RPCFieldMeta.displayType`): the
name a browser chooses a control and a cell renderer by. The vocabulary is one closed
set for a table column, a view column, a computed field, and an RPC field, and a leaf's
display type is its own name on every path, so a computed `ccc.UUID` field is `uuid` as a
column of the same type is, and a computed `civil.Date` field `civildate`. The generator
holds the set as typed constants (`resource/generation/displaytype.go`), renders every
display type through one function that refuses anything outside it, and a unit test
asserts that what the three paths emit is exactly this list; `@cccteam/resource` lists
the same members in `ValidDisplayTypes`, spelled the same, so an adopter's build passes
by construction and never checks the generator's output.

| Display type | In the interface | Emitted for |
| --- | --- | --- |
| `string` | `string` | text columns and fields, named string types, `securehash.Hash` |
| `number` | `number` | integers, floats, and decimals |
| `boolean` | `boolean` | a `bool` column that is `NOT NULL`, and every `bool` field on the other paths |
| `nullboolean` | `NullBoolean` | a nullable `BOOL` column (`*bool`, `spanner.NullBool`); the tri-state rule for one column |
| `date` | `Date` | `time.Time`, `spanner.NullTime` |
| `civildate` | `Date` | `civil.Date`, `spanner.NullDate` |
| `uuid` | `string` | `ccc.UUID`, `ccc.NullUUID` |
| `enumerated` | the key's type | a declared or inferred picker (`@enumerate`, a key into a resource or an enumeration table) |
| `object` | the derived or imported interface, or `unknown` | a struct, a `@typescript` type, `spanner.NullJSON` |
| `bytes` | `string` | a byte slice, base64 on the wire |
| `string[]`, `number[]`, `boolean[]`, `date[]`, `civildate[]`, `uuid[]`, `object[]`, `bytes[]` | the element's interface type with `[]` | a slice, an array, or a named slice type of the leaf (an `ARRAY<...>` column); element pointers are read through, so `[]*int64` is `number[]` and a nullable `[]*bool` is `boolean[]` |

Never `nullboolean[]` (the tri-state rule is for one nullable `BOOL` column) and never
`enumerated[]` (a picker stores one key). A list is one value to the query surface:
`allow_filter`, `index`, and `uniqueindex` on a list field are refused at generation
(section 2), and a sort naming one answers 400, since Spanner has no array equality and
cannot index an `ARRAY` column. The Angular library renders the scalars; an array falls
to its text control until it gains a list renderer.

**Derived structs.** A struct a column holds is its own TypeScript interface, derived
from its fields with no annotation and no option, declared in the resource's namespace
(`MissionDocuments.Provenance`) exactly as a computed resource's nested structs are, and
the field is typed `MissionDocuments.Provenance` with display type `object`. The runtime
marshals the column value as declared, so the struct's `json` tags are its wire names:
every field carries one (a missing tag is refused naming the field), `json:"-"` leaves the
field out of the interface, and `omitempty` makes it optional. The walk keeps the wire
rules (exported fields, no embedded fields, no map, interface, array, or recursion), and
the fields inside the struct resolve as leaves by the same steps, so a nested struct is a
second interface in the same namespace and a nested declared type is the same imported
type. Permissions are unchanged: the object is one field, granted, masked, and selected
whole.

A struct that writes its own JSON (`MarshalJSON` or `UnmarshalJSON`) is refused on the
column path unless it declares its type, since its fields do not describe the wire:

- `MissionDocuments.Seal: resources.Seal writes its own JSON (MarshalJSON or
  UnmarshalJSON), so its fields do not describe the wire; add @typescript(...) to its
  declaration`

RPC and computed shapes are unchanged: their mirrors carry generated camel-case tags, and
the mirror is the wire, so a nested struct's own `MarshalJSON` never runs there and
derivation by fields stays consistent. A `@typescript` declaration on a plain struct wins
over derivation, so an application can give a struct a richer TypeScript type when it
wants one.

**Storage.** A struct, a named slice of structs, or a declared type over anything but a
basic type, held by a `JSON` column and implementing no Spanner methods of its own, gets
`EncodeSpanner` (value receiver, `spanner.NullJSON{Value: v, Valid: true}`, so a type's
own `MarshalJSON` is honoured for storage too) and `DecodeSpanner` (pointer receiver,
reading the column's JSON text back, a NULL cell the zero value) generated into
`zz_gen_storage.go` in the package that declares the type. An application writes no
encoder. A type with hand-written Spanner methods keeps them, whatever column it handles;
one method without the other is refused. Such a type on a column that is not `JSON`, on
an unnamed slice (`[]Provenance` has nothing for a method to attach to), or declared in
a package the generator does not write into (only the resources package and the virtual
resources package) is refused naming the fixes:

- `MissionDocuments.Provenance: resources.Provenance is stored by generated JSON methods,
  but column Provenance is STRING(MAX); declare the column JSON, or implement
  EncodeSpanner and DecodeSpanner on the type`

**Imports.** The names every `@typescript` declaration imports are grouped per module
into one import line each, placed after the `@cccteam/resource` import of the file that
carries them (`zz_gen_resources.ts` for table, view, and computed fields;
`zz_gen_methods.ts` for request and result fields; `zz_gen_api.ts` for the key, create,
and patch shapes). The client library exports no application types.

Examples: [MissionDocument.Provenance](lodestar/pkg/resources/mission_documents.go), a
plain struct on a `JSON` column, derived and stored by generated methods;
[DistressCall.Position](lodestar/pkg/resources/distress_calls.go), a GeoJSON `Point`
declared with `@typescript(Point, from: "geojson")`; and
[MissionDocument.Digest](lodestar/pkg/resources/mission_documents.go), the SHA-256 of an
uploaded document on a `BYTES(32)` column, a `string` in both clients' interfaces with
display type `bytes`; and [Ship.CargoBays](lodestar/pkg/resources/ships.go), an
`ARRAY<INT64>` column typed `[]int64`, `number[]` in the console's interface and its
metadata.

## 13. Files

A row says which stored object is its file, and the generator serves that file under
the row's read route. Two halves make a file's life: an `@upload` method (section 7)
stores it and a row records the key the frame minted, and a `@file` declaration serves
it back at `GET <read route>/<segment>`, `content` by default. Nothing is hand-written:
not the permission check, not the not-found answer, not the content type.

**The gate.** Read on the resource and a Read grant on the segment, a field of the
resource with no column behind it. A role that lists documents and reads their rows but
holds no grant on `content` sees the listing and cannot download; the store key is never
the gate, and never on the wire.

**The row.** Located through the resource's own read path with the caller's Read
conditions and tenancy: an absent, cross-tenant, or hidden row is 404, as on the read
route, and a conditional grant on `content` (`client = subject.client`) is the row
condition the statement renders. The columns that deliver the file — the key, and the
`name:` and `type:` columns where declared — are read by the frame for itself, without
field grants. A NULL key is 404 in the row's words, and so is a key the store does not
hold; the key itself is never written into a refusal.

**The response.** `Content-Type` from the type column, then the stored object's type,
then the name's extension, then `application/octet-stream`; `Content-Disposition:
inline; filename="…"` from the name column or the content's name, so an `<img>` or a
link shows the file and an anchor's `download` attribute forces a save;
`Content-Length` and `Last-Modified` when known; `ETag` = the key, quoted, for a stored
file, or the `Tag` a content function sets for a rendered one. When a validator is sent
the frame sends `Cache-Control: private, no-cache` over the outlet's `no-store`, so the
browser may keep the file and asks again with `If-None-Match`, which the frame answers
304 after the gate and the row lookup, before the store is opened. A body that seeks
(a file on disk) goes through `http.ServeContent`, range requests included; any other
body is copied. Refusals are JSON bodies with their status, as on every generated
route. Bytes go through the application: no store is exposed and no link is answered.

**The store.** `resource.FileStore` is the application's object store as the frames
drive it: `Put(ctx, key, contentType, r)` writes an object permanently, `Delete(ctx,
keys)` removes objects, `Open(ctx, key)` reads one back as a `*resource.Content`
(`ErrFileNotFound` when nothing is stored under the key). The application asserts
`FileStore() resource.FileStore` while any struct declares `@upload` or `@file`.
`resource.Content` carries `Name`, `ContentType`, `Size` (-1 unknown), `ModTime` (zero
unknown), `Tag`, and the `Body` the frame closes. Lodestar's store is a directory
confined by `os.Root` ([DirStore](lodestar/pkg/store/dirstore.go)); a bucket store
implements the same three methods.

**Release.** Deleting a row that carries a `@file` key, or pointing it at a new key,
removes the old object from the store once the transaction commits, from every
transaction the application runs: a generated frame, a patch applied on its own,
application code calling `ExecuteFunc`. Nothing is generated into the frames and a body
has nothing to remember. The generator declares the key fields on the resource
(`FileKeys() []accesstypes.Field`, beside `DefaultConfig`), a fact of the schema that no
configuration carries and nothing an application writes changes, and the patch
machinery reads them on the transaction: a delete makes one point read of the row's key columns and records each non-NULL key as
released; an update or insert-or-update that sets a key field reads that field's current
value and records it when the row exists, the value is non-NULL, and it differs from the
new one (a new value of NULL included); an update that leaves the key fields alone reads
nothing, and an insert records nothing. The record lives on the transaction wrapper and
belongs to one attempt, so a retried transaction releases what its committing attempt
recorded. When the commit lands, `ExecuteFunc` calls the store's `Delete` with the
released keys, synchronously, and the request answers after it; a failed delete is
logged naming the keys and the call still succeeds, since the rows are gone. The store
is handed to the database client at construction, `resource.NewSpannerClient(db,
resource.WithFileStore(store))` (the Mock client takes the same option, so a unit test
can assert what a patch released); a client that sees released keys and holds no store
logs them. Any error from the body (`ErrDryRun` included), a commit refusal, or an abort
that ends the transaction releases nothing, because nothing committed. A transaction an
application wraps itself through `NewSpannerReadWriteTransaction` and commits outside
the executor reads what it released with `Released()` and deletes it itself.

**Limitations.**

- One row owns one object. The keys the upload frame mints are UUIDs, so a key is never
  shared by construction, and no claim check runs before the delete: a body that records
  one key on two rows loses the object at the first release. A file several rows share
  is a row of its own, a table with `@file`, that the other rows reference by foreign
  key; the foreign-key refusal protects it while anything references it, and its own
  delete releases the object.
- Rows the database deletes by cascade (`ON DELETE CASCADE`, an interleaved child) never
  pass through the patch machinery; their objects are the sweep's. An application that
  cares deletes the children by patch first.
- A crash between the commit and the delete leaves an object no row claims; the sweep's.
- A reader that located the row before the delete committed may find the object gone;
  the `@file` route answers 404 as for any absent object.

**Rendered files.** A document produced at request time is a computed resource's
content: struct-scope `@file` on a keyed `@computed` struct, and the computed package
declares `<Name><Segment>(ctx, key…, qSet, client, computedClient) (*resource.Content,
error)` beside `Read<Name>`, the QuerySet carrying the checked scope and identity as it
does for `Read<Name>`. The function renders and returns; it never touches the response.
A nil content is 404. Set `Tag` to something that changes when the document does and
the browser revalidates for free.

**The browser client.** The generated descriptor lists a resource's segments
(`files: ['content']`), and its handle gains `fileUrl(key, segment = 'content')` beside
`url(key)`. Nothing fetches: the browser addresses the route itself, in an `<img src>`
or an `<a href>`, and the session cookie rides along.

Examples: [MissionDocument.StoreKey](lodestar/pkg/resources/mission_documents.go), a
stored file with its name and type columns, served to the console's crew and listed but
not served to the client portal; [ExpenseManifest](lodestar/pkg/computedresources/expense_manifests.go),
a rendered `text/csv` manifest of a mission's booked expenses.
