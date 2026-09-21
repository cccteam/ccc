# lodestar

The full-capability demonstration application for `github.com/cccteam/ccc/resource`: the
operations console of a fictional frontier rescue and salvage service (distress calls and
salvage jobs, the crews who fly them, the hangars that keep the ships airworthy, and the
droids that report on both), and the client portal its customers use. Built from the
Lodestar design plan so that every annotation, every handler shape, and the whole ABAC
runtime is something a human can log into and watch working, and so that the state of a
mission is something you can watch move. It is the generator's committed regression
baseline and the product-shaped tour in one.

Round 3 (2026-09-09) is a from-scratch rebuild born from `impulse`: the layout, the
configuration levels, the two auth packages, the environment template, the bun workspace,
and the deploy split come from the tool; the plan's domain sits on top. Every struct,
method, test, component, and persona that proves a framework capability says so in a
`Demonstrates:` paragraph, and [DEMONSTRATIONS.md](DEMONSTRATIONS.md) is the generated
table of what proves what. Everything here is synthetic. The crew personas' plaintext
passwords are committed deliberately: the application runs against a local Spanner
emulator by default, or against a private test instance ([below](#running-against-a-real-spanner-instance)),
and is never published.

## Running it

With [overmind](https://github.com/DarthSim/overmind), podman, and bun installed:

```
cp .envrc.template .envrc && direnv allow
(cd web && ./ccclib.sh local)     # once: the local ccc-lib packages, then bun install
overmind start
```

That starts a fresh emulator, bootstraps it (schema, the demo world, both auths' roles,
the personas, the droid account; a database that already holds data is refused unless
the bootstrap runs with `-reset`, which empties the data and seeds it again without
touching the schema), serves the application on :8090, and runs `ng serve` for both browser apps:
the crew console on :4300 (`/api` proxied) and the client portal on :4301 (`/portal/api`
proxied). Browse http://127.0.0.1:4300 and sign in as any persona on the crew manifest;
browse http://127.0.0.1:4301/portal/ and sign in through the simulated directory as
`client`. Both Go processes build with the session library's `skipAuth` tag: the portal's
Google directory is simulated from `APP_USERNAME` and `APP_ROLES` in `.envrc`, so no
tenant is contacted and the persona quick-fill still works. `APP_DROIDS_API_KEY` opens the
droid channel.

The served application also serves the built bundles (`bun run build` in `web/`): the
console at `/` and the portal at `/portal/`, paths overridable through
`APP_CONSOLE_DIST` and `APP_PORTAL_DIST`. Mission documents land in the directory
`APP_UPLOAD_DIR` names (default `uploads/`, gitignored): the upload frame streams each
file there under a minted key, the transaction's commit claims it, a failure before
commit deletes it, and the generated file route reads it back; `store.DirStore.Sweep` is
the application's safety net for an object no row claims, never the mechanism.

## Running against a real Spanner instance

The emulator answers every test, but it returns no query plans, does not promise the
service's execution plans, and has behavior of its own (it refuses `NULLS FIRST` and
`NULLS LAST`; its error texts differ). Once in a while Lodestar runs against a real,
private Cloud Spanner instance to catch emulator-specific behavior and to read real plans.
A real database here is a private test target, not a published application: the committed
personas and their passwords are as valid there as on the emulator, and the login page's
persona cards stay.

The bootstrap picks its target from the environment alone, the way the Spanner client
library does: with `SPANNER_EMULATOR_HOST` set it talks to the emulator, without it to the
project the application credentials reach.

1. Have an instance. The bootstrap creates the database when it is missing but never the
   instance; the credentials need the Cloud Spanner Database Admin role on it.
2. In a shell with `.envrc` loaded: unset `SPANNER_EMULATOR_HOST`, set
   `GOOGLE_APPLICATION_CREDENTIALS` to the credentials file, and point
   `GOOGLE_CLOUD_SPANNER_PROJECT`, `GOOGLE_CLOUD_SPANNER_INSTANCE_ID`, and
   `GOOGLE_CLOUD_SPANNER_DATABASE_NAME` at the instance.
3. `go run -tags skipAuth ./cmd/bootstrap` creates the database, applies the schema, seeds
   the world, provisions both auths' roles, and creates the personas. Schema changes on a
   real instance are slow and counted against its limits (about six minutes for the
   thirty migrations, measured 2026-09-11), so this runs once per database; every later
   session starts with `go run -tags skipAuth ./cmd/bootstrap -reset`, which empties the
   data and seeds it again with no schema change, in about two minutes.
4. `go run -tags skipAuth .` serves the application on `PORT` against the real database,
   and `overmind start -l console,portal` runs the browser apps against it.
5. `./walkthrough.sh` runs every persona's proof; it moves workflow state, so reset before
   running it again. Then use the console and the portal by hand.
6. Reset with the server stopped, and start it again after. A running server's permission
   engine re-reads its store every minute; a reload that lands while the reset has the
   role tables empty answers every domain as unknown until the next reload after the
   reseed, and the reset ends every session anyway.
7. Anything that behaves differently from the emulator is a finding: record it as an
   issue. When done, drop the database or keep it for next time; the instance is the
   running cost.

## The world

Three **sectors** are the tenants: **Anvil** is home (nearly everyone holds a role there,
and the hangars are there), **Bastion** is where a role stops at a border, and **Cinder**
is the dark sector only headquarters, the archivist, and the hazard analyst can see.
Clients post **missions** with a hazard class, a fee, and a deadline. **Squadrons**
(grouped into **wings**) claim them and fly **sorties** with **expenses**. **Ships** in
**hangars** go through **refits** bay by bay. **Droids** post telemetry over their own
channel and release **consignments** held in bond. Every list pages at the size its
`@page` annotation declares, and the seed is deep enough that every list of consequence
has a second page.

## Two populations, two auths

The crew sign in with passwords at `/api/user/login`; their roles are the application's
(`schema/roles/crew.json`, provisioned by `MigrateRoles`, assigned by the bootstrap from
`cmd/bootstrap/users.json`). The clients sign in through their company's Google directory
at `/portal/api/user/login`; their roles are the directory's groups (`RoleSync`),
reconciled at every login, never assigned in the application; `schema/roles/members.json`
only says what a role may do, in the lowercase names Google groups carry. Each auth is a
package (`pkg/auth/crew`, `pkg/auth/members`) with its own session tables, permission
store, and XSRF cookie (`crew-xsrf`, `members-xsrf`), and each outlet binds to one.

## The personas

All crew passwords are `lodestar`; the login is the job word. The login page is the crew
manifest: pick a card, sign in, switch, never more than two clicks.

| Login | Who | What their view proves |
| --- | --- | --- |
| `governor` | Governor Greer, headquarters | Every global role and Sector Marshal in all three sectors: the pruned pure-RBAC baseline. Runs the watch desk over every live impersonated session and can revoke one. Demonstrates: impersonation.active-list, impersonation.revoke, impersonation.act-as-role, computed.pushdown. |
| `marshal` | Marshal Maren, Anvil | Full sector authority at Anvil, nothing at Bastion or Cinder (the fail-closed border); every transition including Scrap; the row-free `now` condition on IssueBulletin; the sector briefing with every fee; attaches mission documents through the `@upload` method and downloads them through the generated file route, whose gate is her Read grant on `content` (the client portal lists documents and holds no such grant). Demonstrates: tenancy.concealed, @transition.multi-from, condition.now, rpc.client-form, @upload, @file.stored, impersonation.view-as. |
| `cadet` | Cadet Cass | `hazard IN (1, 2)`; the flight deck with only Claim lit; the two-input distress-call form (create-form narrowing). Demonstrates: execute-condition, create-form-narrowing, capability-envelope. |
| `pilot` | Pilot Pax, clearance 3 | `hazard <= subject.clearance AND (requiredCert IS NULL OR requiredCert IN subject.certifications)`; `hangarZone != 'quarantine'` on ships and on HailShip, the touch that answers No Content. Demonstrates: @subjectValue, @subjectSet.global, @attribute.nullable-fk, @attribute.join-path, touch, @answers.no-content. |
| `veteran` | Veteran Vela | `NOT (hazard IN (1, 2) OR fee < 5000)`. Demonstrates: condition.prefix-not. |
| `lead` | Flight Lead Lior, Hammer | `assignedSquadron IN subject.squadrons OR bookedBy = subject`; launch, hold, resume, complete, fail; sorties only while underway. Holds Execute on HoldMission but no Update on the notes, so the armed hold refuses inside the transaction in the grant's words, and the dry run of every lit edge says so before anything is touched. CompleteMission posts the settlement through the Paymaster role's checker (`caller.As`) and chooses its status (`@answers(200, 409)`). Demonstrates: @subjectSet.domain, condition.subject-scalar, rpc.armed-write, rpc.dry-run, rpc.as-role, @answers, @transition.loop, create-under-parent. |
| `dispatcher` | Dispatcher Dunn | `state NOT IN (...)`; two Update grants on one resource: `new.assignedSquadron IN subject.squadrons` and `new.deadline >= deadline`, the write-grouping demo; compiles the briefing without the hazard board. Demonstrates: condition.not-in, write-grouping, condition.old-vs-new, rpc.decision-as-data. |
| `overseer` | Overseer Orla | `deadline < now` as a right-side operand; the reassign that unlocks by the clock (the seeded three-minute mission). Demonstrates: condition.now, @attribute.timestamp. |
| `booking` | Booking Agent Bex | `new.fee <= subject.feeLimit` on create and inside an Update; `fee > 10000 OR bookedBy = subject`; delete by base decision; Stand Down from three sources. Demonstrates: @subjectValue.two-per-anchor, @attribute.decimal, @transition.multi-from. |
| `wingco` | Wing Commander Wilde, Forge Wing | `wing IN subject.wings`, a subject set with a dotted value path; `hazard >= 4`. Demonstrates: @subjectSet.dotted-value. |
| `engineer` | Engineer Ezra | The refit workflow with its failed-test loop on a join-path root; `inspectedAt IS NOT NULL`; interleaved compound-key tasks; the Hail touch. Demonstrates: @transition.join-path-root, @transition.loop, interleaved-table, compound-key, client-supplied-key, transition-owned-timestamp, @defaultsUpdateType, @validateUpdateType. |
| `quartermaster` | Quartermaster Quill | `state = 'underway'` evaluated two hops deep on SortieExpenses. Demonstrates: @stateRoot.two-hop. |
| `supercargo` | Supercargo Sol | `releasedAt IS NULL` on Update and on ReleaseConsignment (shared with the droid, which reads the manifest armed and answers a receipt); `expiresOn < '2026-09-01'`; `allow_filter` on Mass; the hold walked by release date across the NULL boundary. Demonstrates: @attribute.date, allow_filter, paging.nullable-sort, rpc.armed-read, rpc.typed-result, outlet.shared. |
| `salvor` | Salvor Sable | `insured IS NULL OR insured = true` on Clients, the salvage desk's one grant: the seed leaves Halvard covered, Meridian and Bastion Relay undecided, and Vellum refused, so she lists the first three and never Vellum, disagreeing with the trusted view in both directions; `= true` alone, or `!= false`, would drop the undecided outfits (the semantic differential proves that half on the same shape). Demonstrates: @attribute.nullable-bool. |
| `yeoman` | Yeoman Yael | The standing orders, a `@computed` struct with no `@primarykey`: a whole read-only list, served in the book's own order on a bare GET, sorted by section on request, never paged (a `limit` or a `cursor` is refused with a 400 naming the key as the way to page), with no read route and no row identity. Demonstrates: computed.keyless. |
| `purser` | Purser Priya | The expense manifests, a keyed `@computed` struct whose struct-scope `@file` renders each mission's booked expenses as a `text/csv` sheet on request: `GET .../expense-manifests/{missionId}/content` under her Read grant on `content`, with the sheet's digest as its validator, so a kept copy asks again and hears 304 until an expense is booked; the marshal reads no manifest and the cadet none of it. Demonstrates: @file.rendered. |
| `registrar` | Registrar Rhea | The document register at Anvil: Update on the documents' `title`, Delete on the documents, and Execute on `ReplaceMissionDocument`. A replaced file points the row at the new object and the old one leaves the store once the transaction commits; a deleted document's object goes with the row; a dry run of the replacement, a refusal, and a retitle release nothing. No frame and no body does this: the patch machinery records the released keys on the transaction, and the resource client, constructed over the `DirStore` with `resource.WithFileStore`, deletes them after each commit. Demonstrates: @file.released, @file.replaced. |
| `archivist` | Archivist Ada, all sectors | Terminal-state rows; fee and settlement redacted until completed (two read grants on one resource) and sorted over the visible projection, while the deadline, declared `masking:"positional"`, orders every page on the real column; a fee sort her grid does not display pages on the cursor's copy of the fee; the deploy warns that her fee filter sorts the partition; PII withheld; the domain-scoped ship's log; a briefing that counts her redactions. Demonstrates: cell-masking, paging.masked-sort, paging.unselected-sort-key, masking.positional, warning.concealing-key, pii, @manualAddResource.scope, rpc.armed-read. |
| `assessor` | Assessor Asa | Prices cover before launch: one List grant on Missions, the title unconditionally and the hazard level under `state = 'open'`. Hazard is a named variant of INT64 (`type HazardLevel int64`), concealing and unindexed, so a grid that sorts by hazard without displaying it pages on the cursor's copy of the visible hazard, decoded into the field's own type where the Spanner client refuses a pointer to a pointer to it, and the missions no longer open walk through the NULL region in Spanner's placement. Demonstrates: paging.named-variant-key. |
| `hazards` | Hazard Analyst Hale | A conditional (row-free `now`) grant on a computed resource, the whole board through `limit=all`. Demonstrates: computed.conditional-grant, paging.limit-all, computed.fold. |
| `dock` / `watch` | Dockmaster Dara / Night Watch Nadia | `timeOfDay(now, local)` and the wrap-around `timeOfDay(now, 'America/Denver')` window; `dayOfWeek(now, local) NOT IN ('sat', 'sun')`. At any hour exactly one sees the hangar deck. Demonstrates: condition.time-of-day, condition.day-of-week, condition.local-zone. |
| `client` | Client Cleo, portal only | Signs in through her company's directory, whose groups are her roles; the second browser app over the second TypeScript target; `client = subject.client` from the ClientContact anchor; a conditional Execute fired from a portal session; a PII field an external user writes; the portal-only client statement. Demonstrates: auth.directory-roles, auth.skipauth-directory, typescript.second-target, outlet.session, @subjectValue.second-anchor, @manualAddResource.outlet. |
| `droid-r7` | R7, service account, no login | The API-keyed droids outlet: telemetry with no human route, one reading per call, releases through the shared method under its own read grant. Demonstrates: outlet.api-key, outlet.exclusive, machine-identity, rpc.row-free. |

## Where things live

- `pkg/resources`: every struct and annotation (design plan §5), each with `@page` and
  all but the hull catalog with `@order` ([`order.none`](pkg/resources/ship_classes.go),
  a catalog read whole and unsorted, whose pages need a requested sort,
  [`order.required`](pkg/resources/ship_classes.go)); `pkg/rpc`: the thirteen transitions and the effect methods, including the
  client-form [`rpc.client-form`](pkg/rpc/compile_briefing.go); `pkg/computedresources`
  (the pushdown [`computed.pushdown`](pkg/computedresources/service_ledgers.go), the
  fold, and the key-less standing orders
  [`computed.keyless`](pkg/computedresources/standing_orders.go)) and `pkg/virtualresources`; `pkg/router`: the generated router over three outlets
  (`/api`, `/portal/api`, `/droids`) with its chain documented at the top of
  `zz_gen_router.go`, and `hooks.go`, the console's and the portal's own routes composed
  into it; `app/`: wiring, middleware, the ship's log, the client statement, the
  impersonation mint route, and the watch desk. The mission document download is
  generated from `MissionDocument.StoreKey`'s `@file`
  ([`@file.stored`](pkg/resources/mission_documents.go)), and the purser's expense
  manifest is a computed resource whose struct-scope `@file` renders a CSV sheet on
  request ([`@file.rendered`](pkg/computedresources/expense_manifests.go)). The
  registrar's `ReplaceMissionDocument` points a document at a new file, and the
  resource client, constructed over the `DirStore` in `pkg/config/data.go`
  (`resource.WithFileStore`), deletes the object a committed transaction released,
  the replaced file's and the deleted document's alike
  ([`@file.released`](pkg/resources/mission_documents.go),
  [`@file.replaced`](pkg/rpc/replace_mission_document.go)).
- `pkg/auth/crew` and `pkg/auth/members`: the two populations; `schema/roles/*.json`:
  every grant in §7 per auth; `cmd/bootstrap/users.json`: the personas and the droid.
- `schema/migrations` and `schema/devseed`: the schema and the world the suites and the
  demo share; one mission's deadline is written as bootstrap time plus three minutes so the
  overseer's grant flips during a live walkthrough.
- `web/`: one bun workspace, two Angular applications: `console/` (default outlet) and
  `portal/` (portal outlet), each over its own generated TypeScript client. The console's
  [`star-chart`](web/console/src/app/components/sector/sector.service.ts) carries the one
  labeled bypass of the digest-first rule. Two consumers page on the server: the
  hand-written flight deck, and the library's config-driven list on the
  [Missions page](web/console/src/app/configs/missions.config.ts)
  ([`list.server-paged`](web/console/src/app/configs/missions.config.ts)), which asks for
  one page of the mission board at the descriptor's size, turns it by the server's cursors,
  and draws a filter control only on the columns the generated metadata marks filterable
  ([`metadata.filterable`](pkg/resources/missions.go)). The
  [Ships page](web/console/src/app/configs/ships.config.ts) puts the two picker read
  modes side by side, each decided by the maximum page size the generated descriptor
  carries: [`picker.paged`](web/console/src/app/configs/ships.config.ts) over the
  hangars, which declare one (one server page at a time, Previous and Next inside the
  panel, the chosen hangar read by key), and
  [`picker.whole`](web/console/src/app/configs/ships.config.ts) over the hull catalog,
  which declares none (read whole with `limit=all`); its Hangar column resolves each
  page's hangars with one `in` request over the page's keys
  ([`column.referenced-in`](web/console/src/app/configs/ships.config.ts)) where its
  Class column maps the catalog read whole once. Lodestar is also the browser library's
  application: `@cccteam/resource` and `@cccteam/resource-angular` live in
  [cccteam/ccc-lib](https://github.com/cccteam/ccc-lib), hold no application of their own,
  and are proven here through the yalc loop (`web/ccclib.sh local|push`, "Running it"). The
  console carries the library surface no generated page reaches: the idle session
  configured from the build's environment, with the stay-logged-in action in the header
  ([`idle.configured`](web/console/src/app/app.config.ts)); the Squadrons page's channel
  card and its "Other squadrons in this sector"
  ([`config.component`](web/console/src/app/configs/squadrons.config.ts),
  [`config.array`](web/console/src/app/configs/squadrons.config.ts)); the leave-page
  confirmation every config-driven route carries
  ([`form.leave-page`](web/console/src/app/app.routes.ts)); and the client's judgment
  rendered by the adapter, with no HTTP interceptor of the console's own: a 401
  mid-session returns the browser to the login page with the attempted URL kept
  ([`client.login-redirect`](web/console/src/app/app.config.ts)), and an `ApiError` nobody
  caught raises one global notice in the server's words while the flight deck's in-place
  refusals raise none
  ([`client.uncaught-notice`](web/console/src/app/components/sector/flight-deck/flight-deck.component.ts)).
- `test/authz`: the generated authorization matrix, which pins the endpoint gate with
  unconditional grants over the empty schema and so never meets a condition;
  `test/integration`: the suites (§9), where every condition is proven over the seeded
  world through the real engines, including [`paging.nullable-sort`](test/integration/paging_test.go),
  [`rpc.armed-read`](test/integration/rpc_forms_test.go),
  [`impersonation.revoke`](test/integration/impersonation_ops_test.go), and the parity
  world provisioned from the shipped role files. The served suites need the simulated
  directory: `go test -tags skipAuth ./...`, as the CI stub and the Procfile do.
- `demonstrations_test.go` and `DEMONSTRATIONS.md`: the demonstration index, and
  `walkthrough.sh`, the proof by hand. Demonstrates: demonstration-index, walkthrough.
- `walkthrough.sh`: every persona's proof by curl against a freshly bootstrapped or reset stack, including the
  droid channel, the portal through the simulated directory, the dry runs, the watch desk,
  both impersonation moments, and the three-minute wait for the overdue flip
  (`LODESTAR_SKIP_FLIP=1` to skip). Export the `.envrc` variables before running it.

## Regen discipline

`go generate ./...` from the module root is idempotent from a clean tree; the `zz_gen_*`
output (Go, two TypeScript targets, two workflow DOT graphs) is the drift baseline, pinned
by `cmd/generate/generate_test.go` as a content snapshot. After changing schema,
annotations, or generator config: regenerate, `go test -tags skipAuth ./...`, and keep the
diff. `impulse check` from the module root verifies the agreements between the parts the
tool laid in.
