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
(cd web && bun install)           # the published @cccteam/resource, @cccteam/resource-firestore and @cccteam/resource-angular
overmind start
```

That starts a fresh Spanner emulator and a fresh Firestore emulator (the live pages'
subscription record and change sets, [below](#live-pages); Firestore needs no schema, so
the bootstrap leaves it alone), bootstraps the database (schema, the demo world, the
personas, the droid account; the roles need no step, since each auth's role file rides in
the binary and the bootstrap only checks the store against it; a database that already
holds data is refused unless the bootstrap runs with `-reset`, which empties the data and
seeds it again without touching the schema), serves the application on :8090, and runs
`ng serve` for both browser apps:
the crew console on :4300 (`/console/api` proxied) and the client portal on :4301
(`/portal/api` proxied). Browse http://127.0.0.1:4300/console/ and sign in as any persona
on the crew manifest; browse http://127.0.0.1:4301/portal/ and sign in through the
simulated directory as `client`. Both Go processes build with the session library's `skipAuth` tag: the portal's
Google directory is simulated from `APP_USERNAME` and `APP_ROLES` in `.envrc`, so no
tenant is contacted and the persona quick-fill still works. `APP_DROIDS_API_KEY` opens the
droid channel.

To work on ccc-lib itself, attach the workspace to the local checkout beside this one
(`(cd web && ./ccclib.sh local)`, which rewrites the pins to `file:.yalc/` specs, never
committed; `./ccclib.sh restore` puts the published versions back). After a change there,
push the rebuilt packages, restart the dev server, and reload the page:

    (cd web && ./ccclib.sh push)
    overmind restart console portal

The dev server does not watch `node_modules`, so the restart is what picks the new build up.
The three packages are bundled with the application code rather than prebundled by Vite (the
`prebundle` exclusion in `angular.json`), so a plain reload shows the new build in any
browser profile: nothing is held behind an immutable URL, and no cache needs clearing.

The served application also serves the built bundles (`bun run build` in `web/`): the
console at `/console/` and the portal at `/portal/`, the bundles' directories overridable
through `APP_CONSOLE_DIST` and `APP_PORTAL_DIST`. Neither application is mounted at `/`:
an installed browser application owns every URL under its start, so two applications on
one origin each sit under their own path, each with its API beneath it (`/console/api`,
`/portal/api`), and the generated router answers the root alone with a temporary redirect
to `/console/`; every other unmatched path is 404. Mission documents land in the directory
`APP_UPLOAD_DIR` names (default `uploads/`, gitignored): the upload frame streams each
file there under a minted key, the transaction's commit claims it, a failure before
commit deletes it, and the generated file route reads it back; `store.DirStore.Sweep` is
the application's safety net for an object no row claims, never the mechanism.

## Live pages

The console's Ships page is live. Sign in as `harbormaster` (Harbormaster Hollis), open
Sector Ops, then Ships, and leave the fleet board on the screen. In a second browser
profile sign in as `engineer` and clear a refit on the hangar deck, or hail a ship as
`pilot`: the harbormaster's board changes without a reload, and so does a ship's own
page when she has one open. Leave the board for the dashboard and come back inside five
minutes: the page is drawn from the browser's own cache and the server sees no request.
Nothing polls and the server holds no connection open. A page that asked to be live is
told about a change through a change set of its user's own in Firestore, and asks for
the page again itself, by the change's timestamp.

To watch it in the emulator: the Firestore emulator answers Firestore's REST API on
`FIRESTORE_EMULATOR_HOST`, and `Authorization: Bearer owner` reads everything, so with
the stack running and `.envrc` loaded

    curl -s -H 'Authorization: Bearer owner' \
      "http://$FIRESTORE_EMULATOR_HOST/v1/projects/$GOOGLE_CLOUD_SPANNER_PROJECT/databases/(default)/documents/subscriptions"

lists every live subscription (`principal`, `tab`, `resource`, `key` for a row or
`domain` for a list, `expiry`), and `.../documents/users/harbormaster/changes` lists the
documents written into Hollis's set: a `row` document for the ship she had open and a
`list` document for the fleet board in Anvil, each with its server timestamp `at`, which
is the version (`_v`) her page asks again by. The pilot watching Bastion's fleet gets no
document for an Anvil refit, and a persona without List on Ships (the cadet) is never
subscribed: the refused request answers 403 as it always did and writes no record. The
walkthrough plays this scenario by curl (its "live pages" section), and
[`live_test.go`](test/integration/live_test.go) pins it over an in-memory live service.

How it is wired:

- The stack. The Procfile starts the Firestore emulator beside the Spanner emulator, from
  the Cloud SDK emulators image the resource package's own tests pin, with the live
  package's security rules (`resource/live/firestore/firestore.rules`) mounted;
  `.envrc.template` sets `FIRESTORE_EMULATOR_HOST`. A fresh emulator per start means an
  empty record. In production `APP_FIRESTORE_DATABASE` names the database,
  `GOOGLE_CLOUD_FIRESTORE_PROJECT` its project (the Spanner project when unset) and
  `APP_FIREBASE_API_KEY` the browser's key; the composite indexes and the time-to-live
  policies the live package's README lists are the infrastructure's to apply.
- The server. `pkg/config/data.go` reads the Firestore settings at the data level and
  constructs the live service (`resource/live/firestore`) once, when a database or the
  emulator is configured; `app.LiveService()` hands it to the generated handlers, which
  register a subscribing request's interest before the query runs, publish a commit's
  touched rows after the commit and before the answer, and send
  `Cache-Control: private, max-age=300` on a list or read answer whose request carried
  `_v`. Both session outlets serve the generated `live/renew`, `live/unsubscribe` and
  `live/token` routes; the droids outlet refuses `X-Subscribe` with a 400 naming the
  header. The content security policy's `connect-src` names the feed's origin beside the
  application, the emulator in development and Firebase's hosts in production, because
  the browser's feed connects there directly ([`live.pages`](app/app.go)).
- The browser. [`ships.config.ts`](web/console/src/app/configs/ships.config.ts) says
  `live: true`, which makes the list page and its row page live and nothing else on them;
  [`app.config.ts`](web/console/src/app/app.config.ts) provides `CHANGE_FEED` with the
  Firestore feed from `@cccteam/resource-firestore`, and the library's `AuthService`
  starts the feed after login with the identity `live/token` answers and stops it at
  logout, unsubscribing everything first. The environments carry nothing
  Firestore-specific: the project, the database, the emulator host and the key arrive in
  the token payload.
- The checks. The walkthrough section above, the integration suite, and a headless check
  driven in Chrome (playwright-core and the system browser from a scratch project outside
  the repository, as the console's other browser proofs are run) that leaves the fleet
  board and comes back with no request to the server, then sees a hail produce a refetch
  carrying the change's timestamp as `_v`.

## Feature flags

The commendations desk is behind a feature flag, and the flag is seeded off. Sign in as
`adjutant` (Adjutant Alba): the server answers the desk's routes with the router's own 404,
the permission digest carries no `Commendations` and no `PilotCards.commendations`, so the
console shows no page and no card field for it, and a request naming the card's field is
refused as an unknown field. Turn the flag on (the header's "Feature flags" link opens the
library's dialog for anyone holding List on `FeatureFlags`; its toggles are live for a
login holding Execute on `SetFeature`, read-only otherwise; the walkthrough does the same
through `POST /console/api/set-feature` with `{"name":"commendations","enabled":true}`)
and the desk exists: its routes answer, the digest carries it, the Commendations item
appears in the console's navigation without a reload, and Pilot Pax's card reads
"Cited: 2" from the field the flag gates. Every running
instance of the application follows the flip within a moment, with no restart and no
release; turn it off again and the desk is gone everywhere. The flag's value lives in the
application's database, so it is per environment by construction, and every flip is
recorded in `FeatureFlagChanges`.

How it is wired:

- The declaration. [`pkg/resources/features.go`](pkg/resources/features.go) declares
  `Commendations resource.Feature = "commendations"`, its doc comment the description an
  administrator reads. `@feature(Commendations)` on the `Commendation` struct
  ([`@feature`](pkg/resources/commendations.go)) gates the desk whole; on
  `PilotCard.Commendations` ([`@feature.field`](pkg/computedresources/pilot_cards.go)) it
  gates the one field. The generator writes `Features()` and `FeatureGates()` into
  `pkg/resources/zz_gen_features.go`, wraps the desk's routes in the application's
  `FeatureGuard`, filters the digest, teaches the decoders the field, and writes
  `test/authz/zz_gen_features_test.go`, every gated route and field driven in both states;
  the TypeScript descriptor and metadata carry `feature: 'commendations'` and the client
  file declares the `Feature` union and constants.
- The table and the seed. Migration `000043` creates the desk's table, which ships whether
  or not the flag is on, with three seeded citations; migration `000042` carries the
  library's `FeatureFlags` and `FeatureFlagChanges` tables. The bootstrap and the deploy's
  migrate step call `deploy.MigrateFeatures`, which writes the declared flag off where the
  table has no row and keeps the state where it has one, so a fresh stack starts with the
  desk dark and a release never flips anything.
- The role and the persona. `FeatureAdministrator` (global: List and Read on
  `FeatureFlags`, Execute on `SetFeature`) is the one role that flips flags, held by
  `adjutant` beside `Adjutant`, the desk's own role (List, Read and Create on
  `Commendations`); no other role gained a flag grant. The crew's `PilotCards` grant carries
  the `commendations` field since the feature shipped: while the flag is off the digest
  hides it, so turning the flag on needs no role change.
- The instances. `app.New` loads the application's copy of the table and `App.Start`
  follows it (`FeatureSet.Follow`): on every signal the live service's application topic
  delivers on `features`, and every five minutes regardless. `SetFeature` writes the row
  and its change record in one transaction, broadcasts on the topic after the commit,
  reloads its own copy and answers the flag as written. With the Firestore emulator
  running, a second server process on another port sees the flip at its next request;
  without one, at the backstop.
- The checks. The walkthrough's "feature flags" section drives the scenario by curl, a
  second server process built from the tree included;
  [`feature_flags_test.go`](test/integration/feature_flags_test.go) pins it over two
  instances, the real engines and the in-memory live service; the generated authorization
  matrix puts every declared flag on in its own test database before it drives a route,
  since a flag that is off answers before the permission gate the matrix pins. In the
  console, `resourceRoutes` reads `feature` from the generated metadata and guards the
  desk's route with `featureMatch`, the header's "Cited" line sits under `*cccFeature`, and
  the header's "Feature flags" link opens the library's dialog; `header.component.spec.ts`
  covers the link, the dialog and the card in both states, and the headless check in the
  walkthrough's feature-flags section drives the flip through the dialog.

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
   the world, checks both auths' policy stores against their role files, and creates the
   personas. Schema changes on a
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

The crew sign in with passwords at `/console/api/user/login`; their roles are the application's
(`pkg/auth/crew/roles.json`, embedded in the binary and validated against the generated
permission collection when the auth opens, so a release whose roles are wrong does not
start; the store holds no row for them, and the bootstrap assigns the memberships from
`cmd/bootstrap/users.json`, a role held in every sector as one membership). The clients
sign in through their company's Google directory at `/portal/api/user/login`; their roles
are the directory's groups (`RoleSync`), reconciled at every login and never assigned in
the application, a global role held in the global partition and a domain role in every
sector; `pkg/auth/members/roles.json` only says what a role may do, in the lowercase names
Google groups carry. The deploy's migrate step and the bootstrap print what each store
holds that the release cannot use. Each auth is a package (`pkg/auth/crew`,
`pkg/auth/members`) with its own session tables, permission store, role file, and XSRF
cookie (`crew-xsrf`, `members-xsrf`), and each outlet binds to one.

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
| `yeoman` | Yeoman Yael | The standing orders, a `@computed` struct with no `@primarykey`: a whole read-only list, served in the book's own order on a bare GET, sorted by section on request, never paged (a `limit` or a `cursor` is refused with a 400 naming the key as the way to page), with no read route and no row identity. Her first screen is the console's Standing Orders page, the library's list over that resource: the whole book as one page with virtual scroll, every line identified by its position, Previous and Next disabled with the count, no View column, no create, and no row route; a `pageSize` or `enableRowExpansion` on its config fails at startup naming the resource. Demonstrates: computed.keyless, list.keyless. |
| `purser` | Purser Priya | The expense manifests, a keyed `@computed` struct whose struct-scope `@file` renders each mission's booked expenses as a `text/csv` sheet on request: `GET .../expense-manifests/{missionId}/content` under her Read grant on `content`, with the sheet's digest as its validator, so a kept copy asks again and hears 304 until an expense is booked; the marshal reads no manifest and the cadet none of it. Demonstrates: @file.rendered. |
| `registrar` | Registrar Rhea | The document register at Anvil: Update on the documents' `title`, Delete on the documents, and Execute on `ReplaceMissionDocument`. A replaced file points the row at the new object and the old one leaves the store once the transaction commits; a deleted document's object goes with the row; a dry run of the replacement, a refusal, and a retitle release nothing. No frame and no body does this: the patch machinery records the released keys on the transaction, and the resource client, constructed over the `DirStore` with `resource.WithFileStore`, deletes them after each commit. Demonstrates: @file.released, @file.replaced. |
| `archivist` | Archivist Ada, all sectors | Terminal-state rows; fee and settlement redacted until completed (two read grants on one resource) and sorted over the visible projection, while the deadline, declared `masking:"positional"`, orders every page on the real column; a fee sort her grid does not display pages on the cursor's copy of the fee; the deploy warns that her fee filter sorts the partition; PII withheld; the domain-scoped ship's log; a briefing that counts her redactions. Demonstrates: cell-masking, paging.masked-sort, paging.unselected-sort-key, masking.positional, warning.concealing-key, pii, @manualAddResource.scope, rpc.armed-read. |
| `assessor` | Assessor Asa | Prices cover before launch: one List grant on Missions, the title unconditionally and the hazard level under `state = 'open'`. Hazard is a named variant of INT64 (`type HazardLevel int64`), concealing and unindexed, so a grid that sorts by hazard without displaying it pages on the cursor's copy of the visible hazard, decoded into the field's own type where the Spanner client refuses a pointer to a pointer to it, and the missions no longer open walk through the NULL region in Spanner's placement. Demonstrates: paging.named-variant-key. |
| `hazards` | Hazard Analyst Hale | A conditional (row-free `now`) grant on a computed resource, the whole board through `limit=all`. Demonstrates: computed.conditional-grant, paging.limit-all, computed.fold. |
| `dock` / `watch` | Dockmaster Dara / Night Watch Nadia | `timeOfDay(now, local)` and the wrap-around `timeOfDay(now, 'America/Denver')` window; `dayOfWeek(now, local) NOT IN ('sat', 'sun')`. At any hour exactly one sees the hangar deck. Demonstrates: condition.time-of-day, condition.day-of-week, condition.local-zone. |
| `harbormaster` | Harbormaster Hollis, Anvil | List and Read on Ships, nothing else of her own: the live fleet board. Her list and the ship she has open are subscribed when they are read, the engineer's refit (or a hail) lands on both without a reload, a board left and reopened inside five minutes is the browser's own, and the pilot watching Bastion's fleet and the cadet, who holds no List on Ships, receive nothing. Demonstrates: live.pages. |
| `adjutant` | Adjutant Alba, headquarters | The commendations desk, the one resource behind a feature flag, and the key that turns it: `FeatureAdministrator` (List and Read on `FeatureFlags`, Execute on `SetFeature`, global) beside `Adjutant` (List, Read and Create on `Commendations`). Off, the desk's routes answer the router's own 404, the digest leaves the desk and the card's `commendations` field out, and the field named in a request is unknown; she turns the flag on through `SetFeature`, the desk answers, Pax's card counts his citations, and a second server process serves the desk at its next request with no restart; off again, both refuse. Demonstrates: @feature, @feature.field. |
| `client` | Client Cleo, portal only | Signs in through her company's directory, whose groups are her roles; the second browser app over the second TypeScript target; `client = subject.client` from the ClientContact anchor; a conditional Execute fired from a portal session; a PII field an external user writes; the portal-only client statement. Demonstrates: auth.directory-roles, auth.skipauth-directory, typescript.second-target, outlet.session, @subjectValue.second-anchor, @manualAddResource.outlet. |
| `droid-r7` | R7, service account, no login | The API-keyed droids outlet: telemetry with no human route, one reading per call, each carrying the firmware's raw frame, a type declared in the droid link's own package whose generated methods the generator writes there (`WithTypes`); releases through the shared method under its own read grant. Demonstrates: outlet.api-key, outlet.exclusive, machine-identity, rpc.row-free, typescript.types-package. |

## Where things live

- `pkg/resources`: every struct and annotation (design plan §5), each with `@page` and
  all but the hull catalog with `@order` ([`order.none`](pkg/resources/ship_classes.go),
  a catalog read whole and unsorted, whose pages need a requested sort,
  [`order.required`](pkg/resources/ship_classes.go)); `pkg/rpc`: the thirteen transitions and the effect methods, including the
  client-form [`rpc.client-form`](pkg/rpc/compile_briefing.go); `pkg/computedresources`
  (the pushdown [`computed.pushdown`](pkg/computedresources/service_ledgers.go), the
  fold, and the key-less standing orders
  [`computed.keyless`](pkg/computedresources/standing_orders.go)) and `pkg/virtualresources`; `pkg/router`: the generated router over three outlets
  (`/console/api`, `/portal/api`, `/droids`; each browser outlet's API under its
  application's mount path) with its chain documented at the top of
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
  [`@file.replaced`](pkg/rpc/replace_mission_document.go)). A `@file` table whose rows
  the database deletes by cascade releases nothing, since its rows never pass through
  the patch machinery, and the generator's audit pass names such tables without saying
  so on every run: `RefitTask.PhotoKey` stores a task photo on a table interleaved in
  Refits `ON DELETE CASCADE`
  ([`audit.cascade-release`](pkg/resources/refit_tasks.go)), so `go run
  ./cmd/generate/resourcegenerator -audit` prints the one `Audit:` line naming it after
  the nine `Warning:` lines, `go generate ./...` prints the nine alone, and
  `cmd/generate/audit_test.go` pins the finding as a typed value.
- `pkg/telemetry`: the droid link's own package, the one package the generator writes
  into without reading a resource from it: `cmd/generate` names it with `WithTypes`, so
  the frame type a `DroidReports` column holds gets its JSON and Spanner methods generated
  beside it ([`typescript.types-package`](pkg/telemetry/telemetry.go)). The briefing
  catalog's `Layout` is the one field typed `json.RawMessage` itself, a computed field
  passed through unmodelled ([`typescript.raw-json`](pkg/computedresources/briefing_templates.go)),
  and `DistressCall.Position` writes no JSON methods of its own any more
  ([`typescript.generated-json-methods`](pkg/resources/distress_calls.go)).
- `pkg/auth/crew` and `pkg/auth/members`: the two populations, each with its
  `roles.json`, every grant in §7 per auth, embedded in the binary;
  `cmd/bootstrap/users.json`: the personas and the droid.
- `schema/migrations` and `schema/devseed`: the schema and the world the suites and the
  demo share; one mission's deadline is written as bootstrap time plus three minutes so the
  overseer's grant flips during a live walkthrough.
- `web/`: one bun workspace, two Angular applications: `console/` (default outlet) and
  `portal/` (portal outlet), each over its own generated TypeScript client; the workspace's
  checks are `bun run build && bun run lint && bun run test`; ccc's CI runs all three in
  its `lodestar-web` job against the published `@cccteam/resource` and
  `@cccteam/resource-angular`, the versions `web/package.json` pins, installed with the
  lockfile frozen; the specs run on Angular's unit-test builder with
  Vitest under jsdom, no browser, the wiring the
  Impulse skeleton carries (the console's dashboard, login, header, top bar, footer, and
  shell, the portal's login and tracker, over a scripted client from
  `@cccteam/resource-angular/testing`). The console's
  [`star-chart`](web/console/src/app/components/sector/sector.service.ts) carries the one
  labeled bypass of the digest-first rule. Two consumers page on the server: the
  hand-written flight deck, and the library's config-driven list on the
  [Missions page](web/console/src/app/configs/missions.config.ts)
  ([`list.server-paged`](web/console/src/app/configs/missions.config.ts)), which asks for
  one page of the mission board at the descriptor's size, turns it by the server's cursors,
  and draws a filter control only on the columns the generated metadata marks filterable
  ([`metadata.filterable`](pkg/resources/missions.go)). The
  [Standing Orders page](web/console/src/app/configs/standingOrders.config.ts) is the same
  list over a key-less resource ([`list.keyless`](web/console/src/app/configs/standingOrders.config.ts)):
  the whole book as one page, rows identified by position, no row route, and a config
  asking for a page size or a row expansion refused when the page is built. Four pages
  carry the field shapes a form never types and the write-only field: the
  [Documents page](web/console/src/app/configs/missionDocuments.config.ts) shows a
  document's digest as a size in its cell and as a download on its row page
  ([`field.bytes`](web/console/src/app/configs/missionDocuments.config.ts)) and its
  provenance as JSON, the [Calls page](web/console/src/app/configs/distressCalls.config.ts)
  shows a call's position as JSON ([`field.object`](web/console/src/app/configs/distressCalls.config.ts))
  and draws nothing for its write-only transcript in view mode and a blank input in its
  create form and in its edit form, whose value travels only when typed; the edit form has
  the input because the row's Update envelope names every field the caller may write,
  projected or not, so the marshal's covers the transcript no read returns
  ([`field.write-only`](web/console/src/app/configs/distressCalls.config.ts)), and the
  Ships page's cargo bays and the Squadrons page's callsigns are joined in a cell and chips
  on the row page, read-only in edit mode ([`field.array`](web/console/src/app/configs/ships.config.ts)). The
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
  world through the real engines (each conditional grant names its case through
  `provesGrant`, so `impulse check`'s conditions-proven check finds a grant no suite
  proves, and `grants_test.go` proves the deploy path serves every unconditional grant),
  including [`paging.nullable-sort`](test/integration/paging_test.go),
  [`rpc.armed-read`](test/integration/rpc_forms_test.go),
  [`impersonation.revoke`](test/integration/impersonation_ops_test.go), and the parity
  world provisioned from the shipped role files. The served suites need the simulated
  directory: `go test -tags skipAuth ./...`, as the CI stub and the Procfile do.
- `demonstrations_test.go` and `DEMONSTRATIONS.md`: the demonstration index, and
  `walkthrough.sh`, the proof by hand. Demonstrates: demonstration-index, walkthrough.
- `walkthrough.sh`: every persona's proof by curl against a freshly bootstrapped or reset stack, including the
  droid channel, the portal through the simulated directory, the dry runs, the watch desk,
  both impersonation moments, the feature flag's flip with a second server process built
  from the tree on `LODESTAR_SECOND_PORT` (default 8091), and the three-minute wait for
  the overdue flip (`LODESTAR_SKIP_FLIP=1` to skip). Export the `.envrc` variables before
  running it.

## Regen discipline

`go generate ./...` from the module root is idempotent from a clean tree; the `zz_gen_*`
output (Go, two TypeScript targets, two workflow DOT graphs, the bindings DOT graph) is the drift baseline, pinned
by `cmd/generate/generate_test.go` as a content snapshot. After changing schema,
annotations, or generator config: regenerate, `go test -tags skipAuth ./...`, and keep the
diff. The generate program prints the schema warnings after every run, one `Warning:`
line each; `go run ./cmd/generate/resourcegenerator -audit` runs the same generation and
also prints the audit pass's findings, one `Audit:` line each, the advisory findings
a plain `go generate` stays silent on. `impulse check` from the module root verifies the
agreements between the parts the tool laid in.
