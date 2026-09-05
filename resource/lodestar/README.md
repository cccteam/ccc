# lodestar

The full-capability demo application for `github.com/cccteam/ccc/resource`: the
operations console of a fictional frontier rescue and salvage service — distress calls
and salvage jobs, the crews who fly them, the hangars that keep the ships airworthy,
and the droids that report on both. Built from the Lodestar design plan so that every
annotation, every handler shape, and the whole ABAC runtime is something a human can log
into and watch working, and so that the state of a mission is something you can watch
move. It is the generator's committed regression baseline and the product-shaped tour in
one.

Everything here is synthetic. The demo personas' plaintext passwords are committed
deliberately; the application only ever runs against a local Spanner emulator.

## Running it

With [overmind](https://github.com/DarthSim/overmind) and podman installed:

```
overmind start
```

That starts a fresh emulator, bootstraps it (schema, roles, personas, demo data — the
bootstrap requires a fresh emulator and is not idempotent; provisioning the roles takes
about a minute), serves the app on :8083, and runs `ng serve` for both browser apps:
the crew console on :4300 (`/api` proxied) and the client portal on :4301 (`/portal`
proxied). Browse http://127.0.0.1:4300 and sign in as any persona on the crew manifest;
browse http://127.0.0.1:4301/client/ as `client`. Without overmind, run the Procfile's commands
yourself in that order. Set `LODESTAR_DROID_API_KEY` before starting the server to open
the droid channel.

The served app on :8083 also serves the built bundles (`npm run build` in `web/`): the
console at `/` and the portal at `/client/`, paths overridable via
`LODESTAR_CONSOLE_DIST` and `LODESTAR_PORTAL_DIST`.

## The world

Three **sectors** are the tenants: **Anvil** is home (nearly everyone holds a role there,
and the hangars are there), **Bastion** is where a role stops at a border, and **Cinder**
is the dark sector only headquarters, the archivist, and the hazard analyst can see.
Clients post **missions** with a hazard class, a fee, and a deadline. **Squadrons**
(grouped into **wings**) claim them and fly **sorties** with **expenses**. **Ships** in
**hangars** go through **refits** bay by bay. **Droids** post telemetry over their own
channel and release **consignments** held in bond.

## The personas

All passwords are `lodestar`; the login is the job word. The login page is the crew
manifest: pick a card, sign in, switch — never more than two clicks.

| Login | Who | What their view proves |
| --- | --- | --- |
| `governor` | Governor Greer, headquarters | Every global role and Sector Marshal in all three sectors — the pruned pure-RBAC baseline. May view as anyone and act as a role. |
| `marshal` | Marshal Maren, Anvil | Full sector authority at Anvil, nothing at Bastion or Cinder (the fail-closed border); every transition including Scrap; the row-free `now` condition on IssueBulletin. |
| `cadet` | Cadet Cass | `hazard IN (1, 2)`; the flight deck with only Claim lit; the two-input distress-call form (create-form narrowing). |
| `pilot` | Pilot Pax, clearance 3 | `hazard <= subject.clearance AND (requiredCert IS NULL OR requiredCert IN subject.certifications)`; `hangarZone != 'quarantine'` on ships and on HailShip. |
| `veteran` | Veteran Vela | `NOT (hazard IN (1, 2) OR fee < 5000)`. |
| `lead` | Flight Lead Lior, Hammer | `assignedSquadron IN subject.squadrons OR bookedBy = subject`; launch, hold, resume, complete, fail; sorties only while underway. |
| `dispatcher` | Dispatcher Dunn | `state NOT IN (...)`; two Update grants on one resource: `new.assignedSquadron IN subject.squadrons` and `new.deadline >= deadline` — the write-grouping demo. |
| `overseer` | Overseer Orla | `deadline < now` as a right-side operand; the reassign that unlocks by the clock (the seeded three-minute mission). |
| `booking` | Booking Agent Bex | `new.fee <= subject.feeLimit` on create and inside an Update; `fee > 10000 OR bookedBy = subject`; delete by base decision; Stand Down from three sources. |
| `wingco` | Wing Commander Wilde, Forge Wing | `wing IN subject.wings` — a subject set with a dotted value path; `hazard >= 4`. |
| `engineer` | Engineer Ezra | The refit workflow with its failed-test loop; `inspectedAt IS NOT NULL`; interleaved compound-key tasks; the Hail touch. |
| `quartermaster` | Quartermaster Quill | `state = 'underway'` evaluated two hops deep on SortieExpenses. |
| `supercargo` | Supercargo Sol | `releasedAt IS NULL` on Update and on ReleaseConsignment (shared with the droid); `expiresOn < '2026-09-01'`; `allow_filter` on Mass. |
| `archivist` | Archivist Ada, all sectors | Terminal-state rows; fee and settlement redacted until completed (two read grants on one resource); PII withheld; the domain-scoped ship's log. |
| `hazards` | Hazard Analyst Hale | A conditional (row-free `now`) grant on a computed resource. |
| `dock` / `watch` | Dockmaster Dara / Night Watch Nadia | `timeOfDay(now, local)` and the wrap-around `timeOfDay(now, 'America/Denver')` window; `dayOfWeek(now, local) NOT IN ('sat', 'sun')`. At any hour exactly one sees the hangar deck. |
| `client` | Client Cleo, portal only | The second browser app over the second TypeScript target; `client = subject.client` from the ClientContact anchor; a conditional Execute fired from a portal session; a PII field an external user writes. |
| `droid-r7` | R7, service account, no login | The API-keyed droids outlet: telemetry with no human route, releases through the shared method. |

## Where things live

- `pkg/resources` — every struct and annotation (design plan §5); `pkg/rpc` — the
  thirteen transitions and four effect methods; `pkg/computedresources` and
  `pkg/virtualresources`; `pkg/router` — the router over three outlets (`/api`,
  `/portal`, `/droids`); `app/` — wiring, middleware, the ship's-log route, and the
  impersonation mint route.
- `cmd/bootstrap/demo_access.json` — every grant in §7, provisioned by
  `access.MigrateRoles` and frozen by the bootstrap-parity suites.
- `schema/migrations` and `schema/demoseed` — the schema and the world the suites and the
  demo share; one mission's deadline is written as bootstrap time plus three minutes so
  the overseer's grant flips during a live walkthrough.
- `web/` — one Angular workspace, two applications: `console/` (default outlet) and
  `portal/` (portal outlet), each over its own generated TypeScript client.
- `test/authz` — the generated authorization matrix; `test/integration` — the suites
  (§9): language, chain, multi-from, graph, grouping, clock, computed-conditional,
  execute-conditions, subject-anchors, portal, temporal, impersonation, and the ported
  waystation suites.
- `walkthrough.sh` — every persona's proof by curl against a fresh stack, including the
  droid channel, the portal, both impersonation moments, and the three-minute wait for
  the overdue flip (`LODESTAR_SKIP_FLIP=1` to skip).

## Regen discipline

`go generate ./...` from the module root is idempotent from a clean tree; the
`zz_gen_*` output (Go, two TypeScript targets, two workflow DOT graphs) is the drift
baseline, pinned by `cmd/generate/generate_test.go` as a content snapshot. After changing
schema, annotations, or generator config: regenerate, `go test ./...`, and keep the diff.
