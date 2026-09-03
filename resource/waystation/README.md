# waystation

The full-capability demo application for `github.com/cccteam/ccc/resource`: a fictional
transit authority running a ring of orbital waystations, built so that every annotation,
every handler shape, and the entire ABAC runtime is something a human can log into and
watch working. It is the generator's committed regression baseline and the product-shaped
tour in one: the access-control machinery reads as ordinary business rules — you see the
work orders assigned to your teams, you can approve requisitions up to your limit, costs
are hidden until a requisition is approved.

Everything here is synthetic. The demo personas' plaintext passwords are committed
deliberately; the application only ever runs against a local Spanner emulator.

## Running it

With [overmind](https://github.com/DarthSim/overmind) and podman installed:

```
overmind start
```

That starts a fresh emulator, bootstraps it (schema, roles, personas, demo data — the
bootstrap requires a fresh emulator and is not idempotent), serves the app on :8082,
and runs `ng serve` on :4200 with `/api` proxied. Browse http://127.0.0.1:4200 and sign
in as any persona below. Without overmind, run the Procfile's three commands yourself
in that order.

The served app on :8082 also serves the built Angular bundle from `gui/dist`
(`npm run build` in `gui/`, path overridable via `WAYSTATION_GUI_DIST`), so the demo
works without the dev server too.

## The personas

All login passwords are `waystation`.

| Persona | Story | What their login demonstrates |
| --- | --- | --- |
| `commander` | Headquarters | Unconditional global + every-station roles: full fleet dashboard, all costs, all PII, the audit trail. Pins that pruned RBAC output is untouched by the ABAC machinery. |
| `chief-alpha` | Station chief, Alpha | Full domain roles at ws-alpha only; the same pages are empty/refused at ws-beta. Runs the whole work-order lifecycle. |
| `tech-rivera` | Maintenance technician | `assignedTeam IN subject.teams OR author = subject` — the board shows their teams' orders; teams derive per-station (derived tenancy through the TeamMembership anchor). `zone != 'reactor'` keeps reactor assets out of their asset picker. Files incident reports through a two-input form: their partial-width Create grant (`summary`, `severity`) narrows the rendered inputs. |
| `foreman-okafor` | Requester | `requestedBy = subject` ownership; lines editable only while `state = 'draft'`; `new.priority <= 3` refuses emergency work orders at create time (an insert-image condition); `new.priority <= priority` on WorkOrders Update lets them re-prioritize only downward — raising priority is the chief's call (an old-vs-new comparison). |
| `procurement-chen` | Approver | Queue is literally the condition: `state = 'submitted' AND totalCost <= subject.approvalLimit`. The same limit rides the Execute(ApproveRequisition) grant, evaluated against the located row inside the transition frame (§12) — a directly addressed over-limit approval refuses with no code in the RPC body. |
| `auditor-voss` | Compliance | Terminal-state work orders only; `unitCostSnapshot` masked per cell until a requisition is approved (the key is absent, the UI renders an em-dash); incident reporter PII withheld; the audit trail (RecordsAuditor). |
| `quartermaster-idris` | Inventory | Receive gated on `arrivedAt IS NULL` (second receive refused); lot deletes gated on expiry (`expiresOn < '2026-09-01'` — fresh and no-expiry lots refuse). |
| `automation` | Service account, no login | The `/automation` outlet under an API key: posts sensor batches, receives shipments. Sensor readings have no human route at all — the computed status board is the only window onto them. |

## What it covers, and where

- **Conditional grants (ABAC)** live in `cmd/bootstrap/demo_access.json` — provisioned
  by `access.MigrateRoles` at bootstrap, validated against the generated permission
  collection, and frozen by `integration/conditions_test.go`, which provisions the
  SHIPPED config through the REAL engine and pins every persona view. The demo product
  and the regression suite cannot drift apart.
- **Workflows** (work orders, requisitions) are enforced entirely through the stateful
  pattern: `@state` makes the status column structurally unwritable, every state move is
  a declared `@transition` on an Execute-gated RPC — the generated handler locates the
  row within the station, verifies the pre-image state, evaluates any row condition the
  caller's Execute grant carries against the same row (§12: the approver's limit, the
  no-nudging-finished-work rule), and stamps the target state, so the bodies carry only
  business effects — and what each role may do in each state is a conditional grant on
  the uniform `state` binding (readable on the root and every `@stateRoot` member). A
  method that moves no state can still declare its row (`@target(WorkOrder)` on Nudge)
  and gets the same located-row frame minus the stamp. The committed
  `zz_gen_workflow_*.dot` graphs (membership plus the labeled transition edges) and the
  grant matrix are the whole specification; there is no imperative permission or edge
  code anywhere in the app.
- **Old-vs-new comparisons (§05)** — an Update grant may compare the proposed value
  against the row's pre-image: the foreman's `new.priority <= priority` conjunct means
  they may lower a work order's priority but never raise it, judged inside the
  mutation's own check-SELECT; an update that leaves the field untouched degenerates to
  a tautology, so the term only ever blocks what the mutation actually changes —
  `integration/old_vs_new_test.go`.
- **Structural enforcement** (fail-closed field permissions, outlet exclusivity,
  suppressed routes, domain guard) — `integration/structural_test.go`; Team is the
  deliberately minimal resource (no vocabulary beyond the mandatory `@domain`).
- **Reserved query parameters** (filter with indexed/allow_filter gating, PII filter
  placement, sort/limit/offset) — `integration/queryparams_test.go`. Foreign-key
  columns are filterable automatically: Spanner FKs create backing indexes and the
  generator derives filterability from the schema.
- **Consolidated batches** — one `PATCH /api/resources` body is one transaction even
  across waystations, and the wire vocabulary is closed (add/patch/remove; an upsert is
  not expressible, so upsert + conditional grant is unreachable over HTTP — the
  programmatic path is refused fail-closed by the resource package itself) —
  `integration/consolidated_test.go`.
- **Manual resources** — the audit trail. `DataChangeEvents` is library infrastructure
  with no generated handler; `@manualAddResource(List)` on
  `resources.AuditTrailEntries` registers the permission the hand-written
  `app.AuditTrailEntries` route checks — `integration/manual_test.go`.
- **Computed and virtual resources**, change tracking, server-owned fields
  (input_only/output_only/defaults/validators), interleaved client-key creates —
  `integration/computed_virtual_test.go`, `changetracking_test.go`,
  `serverfields_test.go`, `workflow_test.go`.
- **Mechanical stamps and first-class Touch** — WorkOrder.UpdatedAt is the
  `output_only_update_fn` enforcement stamp, and NudgeWorkOrder fires the generated
  `NewWorkOrderTouch`: the full update pipeline with no caller-set fields, so a nudge
  bumps the stamp and lands in the audit trail while no field of the order changes —
  `integration/touch_test.go` (contrast Asset.LastServicedAt, domain data written
  explicitly by the one transition that owns the business event).
- **Generated authorization matrix** — `handlertests/`, unconditional allow/deny per
  endpoint.
- **Permission digest (§13)** — `GET /api/permission-digest[?domain=]`, the
  library-owned endpoint every generated router registers: the session user's
  structural grant enumeration (resource → permission → granted|conditional, absence
  = denied) the frontend renders navigation and forms from.
  `integration/permission_digest_test.go` pins the shipped personas' digests through
  the real engine, including the empty answer for a station without a foothold —
  consistent with the concealed posture.
- **Capability envelope (§13)** — `?capabilities=Update,Delete` on any list or read
  makes each row carry the reserved `zzCapabilities` property: the positive list of
  editable field names and whether delete is live, evaluated against the same row
  image the response shows. `integration/capabilities_test.go` pins the chief's
  unconditional list (pure RBAC, no extra SQL) against the foreman's
  state-conditioned one, row by row.
- **Create-under-parent (§11)** — `capabilities=Create` on a parent's read answers,
  per row, which workflow member resources the user may create beneath it: the
  member Create grant's state condition evaluates against the parent row's own
  uniform state binding (an unconditional grant folds structurally, zero SQL). The
  foreman's `RequisitionLines` Create grant carries `state = 'draft'`, so the
  add-line form renders exactly where a line-create would commit — the UI's last
  hand-copied status check retires. `integration/capabilities_test.go` pins the
  affordance following the parent's state, and the chief's empty answer.
- **Create-form narrowing (§13)** — the digest's field-level Create entries are the
  enumeration a create form renders its inputs from: the technician's
  `IncidentReports` Create grant covers `summary` and `severity` only, so their
  report form is two inputs — the PII contact and raw statement neither render nor
  travel (ReporterContact is nullable so the narrowed create commits).
  `integration/permission_digest_test.go` pins the partial-width enumeration; the
  incidents page and ccc-lib's create component both consume it through
  `grantedFields`.

## The GUI

`gui/` is Angular + `@cccteam/ccc-lib`, over the generated API client. The generator
emits `zz_gen_api.ts` beside the other `zz_gen_*.ts` files: the API's descriptor
(routes, scopes, keys, operations) plus the shapes only the generator can derive —
per-resource `Create`/`Patch` interfaces with server-owned and immutable fields
absent, key tuples in route order, and the `Api` type. The framework-neutral runtime
`@cccteam/resource` (in the ccc-lib repo) interprets it: `createApi` puts global
handles on the client root and station-scoped handles under `api.domain(station)`, so
a station resource cannot be addressed without a station — at compile time. The app
registers the client once (`provideResourceClient` in `app.config.ts`), riding
`HttpClient` so ccc-lib's interceptor keeps applying, and the client's permission
cache is the one ccc-lib's `AuthService`, guard, and directive answer from.

Global resources (catalog, suppliers, staff, waystations) are config-driven ccc-lib
resource pages. Every station-scoped surface is hand-written under
`src/app/components/waystation/` on the client's typed handles: lists are
`stationList((station) => station.workOrders, { sort: ... })`, mutations are
`station.workOrders.create({...})` / `.patch(key, {...})` / `.remove(keyOf(row))`,
transitions are `station.scheduleWorkOrder.execute({...})`, and the audit trail's
manually registered resource comes from the client's `define` escape hatch.
`WaystationService` holds the selected waystation as shared state — it is the
permission domain of every request, so switching stations re-scopes what each persona
sees — and exposes `station()`, the client bound to it. The station picker has no
bespoke endpoint: by default its options are the library's answer to "where do I hold
grants" — the generated `user-domains` endpoint, cached on `AuthService.domains()` —
and the "Show all waystations" toggle widens it to the roster served by the generated,
permission-checked Waystations resource — a demo affordance no real application would
carry, kept as the clickable path to fail-closed refusals. Selecting a station loads
its permission digest into the client, and every hand-written page asks it before
asking the server: `WaystationService.can(permission, resource)` gates each list
request, create form, RPC button, and delete affordance (the client resolves the
scope: a global resource asks the global digest, a station-scoped one the selected
station's), the topbar hides menus the persona cannot open, and a page whose List
grant is absent explains so instead of provoking a 403. Conditional grants render —
the server narrows per row — so a technician still sees the task toggles that the
in-progress condition will refuse on a scheduled order. Masked cells arrive as ABSENT
JSON keys and render as em-dashes, never as zero or empty values.

The same generated client runs outside the browser: `bun run` a script that imports
`createApi` from `zz_gen_api.ts` with `fetchTransport` and a cookie-carrying fetch, and
the whole persona walkthrough drives through typed handles — no Angular anywhere.

`gui/.prettierignore` excludes `zz_gen_*.ts`: prettier reflows generator output and
breaks `go generate` idempotence.

## The persona walkthrough

`./walkthrough.sh` drives the full demo through real persona sessions with curl —
login, XSRF, the exact request shapes the GUI issues — and prints PASS/FAIL per check.
Run it against a freshly bootstrapped stack (`overmind start`, or the Procfile's
spanner + server commands). It is single-shot: several checks move workflow state
(receive a shipment, delete a draft), so rerunning needs a fresh bootstrap.

The same tour in the browser, persona by persona:

1. **foreman-okafor** — create a requisition draft (Needed By is required), add a line
   (the unit cost snapshots from the catalog item), submit it, and watch the total
   recompute server-side. The add-line form disappears with the submit: it renders
   from the row's Create affordance, so the draft-only grant leaving the row takes
   the form with it. File a work order at priority 3 (accepted) and priority 5 (refused —
   the insert-image condition). Nudge the stalled oven order: it jumps to the top of
   the last-activity sort with every field unchanged — the first-class Touch.
2. **procurement-chen** — the requisitions page shows only submitted requisitions
   within their limit. Approve the foreman's; the 7120.00 overhaul requisition is not
   approvable (over their 5000 limit — the grant's condition refuses it even when
   addressed directly, with no check code in the RPC body).
3. **auditor-voss** — requisition lines show unit costs only on approved requisitions
   (masked cells render as em-dashes); incidents arrive without reporter contact; the
   work-order board shows completed/cancelled only. The audit trail lists the change
   events the other personas just produced.
4. **chief-alpha** — run the full work-order arc: create a draft, add a task, schedule
   it with a team and due date, start it, toggle the task done (state-gated), complete
   it. Deleting the completed order is refused (deletes are draft-only); deleting a
   draft succeeds. The station picker offers only ws-alpha — the directory is
   permission-derived; flip "Show all waystations" and select ws-beta to watch every
   list load refuse fail-closed.
5. **quartermaster-idris** — receive the in-transit shipment (a second receive is
   refused: `arrivedAt IS NULL`), delete the expired lot, and watch fresh and
   no-expiry lots refuse deletion. Lots arrive sorted by expiry server-side.
6. **tech-rivera** — the work-order board is their teams' orders, per station; the
   asset list excludes the reactor-zone manifold (two-hop join-path attribute).
7. **commander** — the dashboard's fleet summary (computed, global), any station's
   status board (computed from automation-only sensor data), and the audit trail.
   File an incident: the case number comes back server-generated (`IR-…`), the raw
   statement is stored but never served.

## Authoring notes (learned building this)

- **GUI data loading is declarative — never fetch from an effect.** Each station
  page's lists are `resource()`s whose loaders call the client's typed handles,
  derived from the current-waystation signal and the List grant
  (`WaystationService.stationList`/`globalList`): switching stations refetches them,
  mutations call `.reload()` on the affected lists, and the selected row is a
  `computed` over the live list. The original `effect(() => load())` shape looped
  forever at network speed: ccc-lib's `ApiInterceptor` reads the global loading
  signal on every request (`UiCoreService.beginActivity`), so a fetching effect
  adopts that signal as a dependency and every response's `endActivity` write
  re-runs it. Resource loaders run untracked by design, which rules the loop out
  structurally. The client's permission cache is a plain store; `can()` reads its
  signal mirror (`storeSignal`) first so computeds track digest loads.
- **Derived tenancy needs the anchor bound.** A `@subjectSet` over a domain-scoped
  anchor (TeamMembership) only partitions per-domain if the anchor resource carries its
  own `@domain` binding; without it the subject set is partition-blind.
- **A field-empty UPDATE silently no-ops** before `output_only_update_fn` runs, so a
  bare update patch cannot express a pure "touch". That finding landed as framework
  behavior: declaring an update function now also generates `New<Resource>Touch`,
  which runs the full update pipeline with the update functions supplying the write —
  NudgeWorkOrder fires WorkOrder's. It also surfaced a modeling smell here:
  Asset.LastServicedAt is not an every-update stamp but domain data owned by one
  transition, so it is `conditions:"output_only"` and CompleteWorkOrder writes
  `&spanner.CommitTimestamp` explicitly, while WorkOrder.UpdatedAt is the honest
  every-update `output_only_update_fn` fit.
- **Tenancy lookups must be cached.** The consolidated handler calls `DomainVisible`
  inside the mutation transaction, and the emulator forbids queries inside it — a
  data-driven tenancy seam reads the tenant table at startup, not per check.
  (`access.Client.UserHasGrants`, the other half of visibility, answers from the
  in-memory policy snapshot, so it is transaction-safe by construction.)
- **decimal over NUMERIC needs shopspring master.** The Spanner Encoder/Decoder support
  is merged upstream but in no tagged release; this module requires the master
  pseudo-version directly. The requirement is invisible until runtime-with-data.
- **Computed resources hand-write `Resource()`** (virtuals get it generated), and
  hand-built query paths need explicit `.AddColumns(...All())`.
- **Tenant keys are stamped, never sent.** A bare `@domain` column decodes
  output-only (create and update closed on the wire); the framework stamps it from
  the URL's — or the consolidated op path's — domain on create, so the checked
  domain and the written domain are the same value by construction. Create payloads
  and RoleConfig write grants naming the tenant field are rejected.
- **Rows partition structurally, reads and writes (E2).** Every partitioned query
  carries the tenant predicate in its WHERE (bare column or nested EXISTS through
  the join path) before the read rules run — a cross-station row never renders,
  and a filter that matches one returns an empty list, never a 403. Every
  partitioned mutation locates its row within the same predicate through the
  in-transaction check-SELECT, so a cross-station key is NotFound (never a
  silent commit, never a 403 that confirms existence), and a create's proposed
  parent must land in the route's partition — `integration/tenancy_test.go`.
  The cost model changed knowingly: a partitioned pure-RBAC mutation now pays
  one in-transaction point read.
- **Domains are concealed (E2, opt-in).** The generator runs with
  `generation.WithConcealedDomains()`, so the route guard and the consolidated
  descent ask the app's `DomainVisible(ctx, user, domain)` instead of a bare
  existence check: a real domain where the user holds zero grants answers with
  the same 404 (route) / 400 (op path) as a domain that does not exist — the
  refusal never confirms a tenant. Any foothold at all (even write-only) makes
  the domain visible and restores ordinary 403s.

## Regen discipline

`go generate ./...` from the module root is idempotent from a clean tree; the committed
`zz_gen_*` output is the drift baseline. After changing schema, annotations, or
generator config: regenerate, `go test ./...`, and commit the diff as part of the same
change.
