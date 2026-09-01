# waystation

The full-capability demo application for `github.com/cccteam/ccc/resource`: a fictional
transit authority running a ring of orbital waystations, built so that every annotation,
every handler shape, and the entire ABAC runtime is something a human can log into and
watch working. Where starport is the minimal regression baseline, waystation is the
product-shaped tour: the access-control machinery reads as ordinary business rules —
you see the work orders assigned to your teams, you can approve requisitions up to your
limit, costs are hidden until a requisition is approved.

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
| `tech-rivera` | Maintenance technician | `assignedTeam IN subject.teams OR author = subject` — the board shows their teams' orders; teams derive per-station (derived tenancy through the TeamMembership anchor). `zone != 'reactor'` keeps reactor assets out of their asset picker. |
| `foreman-okafor` | Requester | `requestedBy = subject` ownership; lines editable only while `state = 'draft'`; `new.priority <= 3` refuses emergency work orders at create time (an insert-image condition). |
| `procurement-chen` | Approver | Queue is literally the condition: `state = 'submitted' AND totalCost <= subject.approvalLimit`. The RPC body re-verifies the limit for direct over-limit calls. |
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
  pattern: `@state` makes the status column structurally unwritable, transitions happen
  only through Execute-gated RPCs whose bodies check edge legality only, and what each
  role may do in each state is a conditional grant on the uniform `state` binding
  (readable on the root and every `@stateRoot` member). The committed
  `zz_gen_workflow_*.dot` graphs plus the grant matrix are the whole specification;
  there is no imperative permission code anywhere in the app.
- **Structural enforcement** (fail-closed field permissions, outlet exclusivity,
  suppressed routes, domain guard) — `integration/structural_test.go`; Team is the
  deliberately annotation-free resource.
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
- **Generated authorization matrix** — `handlertests/`, unconditional allow/deny per
  endpoint.

## The GUI

`gui/` is Angular + `@cccteam/ccc-lib`. Global resources (catalog, suppliers, staff,
waystations) are config-driven ccc-lib resource pages; every station-scoped surface is
hand-written under `src/app/components/waystation/`, because ccc-lib 0.0.44 uses
`meta.route` verbatim and cannot fill the `{waystationID}` segment of a generated
domain-route template. `WaystationService` holds the selected waystation as shared
state — it is the permission domain of every request, so switching stations re-scopes
what each persona sees. Masked cells arrive as ABSENT JSON keys and render as em-dashes,
never as zero or empty values.

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
   recompute server-side. Try to add a line after submitting: refused, the draft-only
   grant is gone. File a work order at priority 3 (accepted) and priority 5 (refused —
   the insert-image condition).
2. **procurement-chen** — the requisitions page shows only submitted requisitions
   within their limit. Approve the foreman's; the 7120.00 overhaul requisition is not
   approvable (over their 5000 limit, refused by the RPC body even when addressed
   directly).
3. **auditor-voss** — requisition lines show unit costs only on approved requisitions
   (masked cells render as em-dashes); incidents arrive without reporter contact; the
   work-order board shows completed/cancelled only. The audit trail lists the change
   events the other personas just produced.
4. **chief-alpha** — run the full work-order arc: create a draft, add a task, schedule
   it with a team and due date, start it, toggle the task done (state-gated), complete
   it. Deleting the completed order is refused (deletes are draft-only); deleting a
   draft succeeds. Switch the station picker to ws-beta: everything empties.
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

- **Derived tenancy needs the anchor bound.** A `@subjectSet` over a domain-scoped
  anchor (TeamMembership) only partitions per-domain if the anchor resource carries its
  own `@domain` binding; without it the subject set is partition-blind.
- **A field-empty UPDATE silently no-ops** before `output_only_update_fn` runs —
  "touch" semantics require explicitly setting a field (the generated Asset setter
  writes `&spanner.CommitTimestamp`).
- **Tenancy lookups must be cached.** The consolidated handler calls `DomainExists`
  inside the mutation transaction, and the emulator forbids queries inside it — a
  data-driven tenancy seam reads the tenant table at startup, not per check.
- **decimal over NUMERIC needs shopspring master.** The Spanner Encoder/Decoder support
  is merged upstream but in no tagged release; this module requires the master
  pseudo-version directly. The requirement is invisible until runtime-with-data.
- **Computed resources hand-write `Resource()`** (virtuals get it generated), and
  hand-built query paths need explicit `.AddColumns(...All())`.
- **Domain scope partitions permissions, not rows.** An unconditional grant sees every
  station's rows on any station's route — pinned deliberately in the conditions and
  query-parameter suites as the gap the planned tenancy injection (E2) closes.

## Regen discipline

`go generate ./...` from the module root is idempotent from a clean tree; the committed
`zz_gen_*` output is the drift baseline. After changing schema, annotations, or
generator config: regenerate, `go test ./...`, and commit the diff as part of the same
change.
