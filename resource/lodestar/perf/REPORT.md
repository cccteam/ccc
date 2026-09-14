# Query plans for the injected shapes on a real Spanner instance

Measured 2026-09-11 against a regional-us-central1 instance at 100 processing units,
database `lodestar`, optimizer version 9, fresh statistics (`ANALYZE`) after every load.
The shapes are the statements the resource package renders into the application's
queries: structural tenancy, grant conditions over subject sets and subject values, the
visible projection, and the write checks. Their texts are in `shapes/`, the per-capture
numbers in `results/`, the procedure in `README.md`.

## The world at each volume

| | seed | medium | large |
| --- | --- | --- | --- |
| Sectors | 3 | 23 | 53 |
| Missions, total | 30 | 52,530 | 510,030 |
| Missions in Anvil | 30 | 2,530 | 10,030 |
| Ships, total (Anvil) | 12 (12) | 4,212 (212) | 20,412 (412) |
| Sorties, total | 12 | 10,512 | 102,012 |
| Squadron memberships, total | 8 | 3,032 | 18,368 |

Every list is read as the Anvil persona sees it: Anvil holds a fiftieth of the large world
and a twentieth of the medium one.

## What each shape scanned, and how long it took

Rows scanned and elapsed time as Spanner reports them in PROFILE mode. The last column is
the large world after two indexes were added for the experiment in the findings:
`Missions(SectorId, Deadline)` and `Ships(HangarId, Name)`.

| shape | seed | medium | large | large, indexed |
| --- | --- | --- | --- | --- |
| s1 Missions, bare tenancy, default page | 62 / 14 ms | 5,062 / 47 ms | 20,060 / 68 ms | **52 / 17 ms** |
| s1b Missions, tenancy + hazard filter, max page | 62 / 12 ms | 5,062 / 20 ms | 20,060 / 89 ms | **1,240 / 89 ms** |
| s1c Consignments, keyset cursor page | 24 / 16 ms | 24 / 13 ms | 14 / 13 ms | 14 / 51 ms |
| s2 Ships, join-path tenancy through Hangars | 22 / 13 ms | 4,422 / 30 ms | 20,822 / 108 ms | 20,822 / 96 ms |
| s3 SquadronMemberships, two hops | 22 / 18 ms | 6,214 / 35 ms | 82 / 23 ms | 82 / 20 ms |
| s3b Sorties, join-path through the mission | 24 / 20 ms | 11,024 / 59 ms | **104,024 / 1.27 s** | 104,024 / 847 ms |
| s4 Missions, subject set | 83 / 49 ms | 5,767 / 177 ms | 20,138 / 220 ms | 20,138 / 163 ms |
| s4b Missions, subject set + capability booleans | 104 / 97 ms | 5,845 / 93 ms | **512,886 / 1.72 s** | 20,216 / 169 ms |
| s5 Missions, subject value | 69 / 51 ms | 5,444 / 38 ms | 20,088 / 82 ms | 20,088 / 114 ms |
| s6 Missions, sort on a conditionally visible column | 62 / 15 ms | 5,062 / 34 ms | 20,060 / 73 ms | 20,060 / 55 ms |
| s6b Missions, masked fee sort | 62 / 114 ms | 5,062 / 29 ms | 20,060 / 61 ms | 20,060 / 62 ms |
| s7 write check, one group, by primary key | 1 / 19 ms | 1 / 10 ms | 4 / 10 ms | 4 / 15 ms |
| s7b insert check, condition + two-hop tenancy | 3 / 20 ms | 3 / 17 ms | 4 / 33 ms | 4 / 26 ms |

Elapsed times are single runs on a small instance and vary by tens of milliseconds
between runs; the scanned-row counts are exact and are what the plans decide.

## Findings

**1. A bare tenant column is indexed for free, and sorted for every page.** Spanner keeps
a backing index for every foreign-key column, and `Missions.SectorId` references
`Sectors`, so the list drives from that index: 10,030 index rows for Anvil, one lookup
each into the base table, then a sort for the page's order. Cost grows with the partition,
not the table. Adding `Missions(SectorId, Deadline)`, the tenant key followed by the
resource's `@order` columns, turns the default page into 26 index rows and 26 lookups, and
the filtered page into a scan that stops as soon as it has its rows. This is the index a
listed, tenant-scoped resource wants.

**2. Join-path tenancy lists scan the whole table.** `Ships` reaches its sector through
`Hangars`, so the list is a full scan of `Ships`, all sectors, with a hangar lookup per row
(20,412 rows for a page of 11). `Sorties` reaches its sector through `Missions`: 102,012
rows and up to 1.3 seconds for a page of 11. The `Ships(HangarId, Name)` index changed
nothing; the optimizer still drove from the full scan. Cost grows with the table, not the
partition, and no index on the child fixes it, because no column on the child names the
tenant. For a table listed at volume, the tenant key belongs on the row (`@domain` on a
column, plus the index above). Join-path tenancy stays right for small tables, for
children whose lists are always under a parent, and for writes, whose checks are point
lookups (finding 6).

**3. Subject sets evaluate the whole partition per page.** `assignedSquadron IN
subject.squadrons` renders as a correlated EXISTS over `SquadronMemberships`; each check is
a key lookup (the anchor's primary key is `(SquadronId, UserId)`), so the per-row cost is
small, but the list evaluates it for every row of the partition before sorting: 20,138 rows
and 160 to 220 ms at 10,030. The composite index did not shorten it; the filter is not one
the index scan can stop on. Applications should expect subject-set lists to cost in
proportion to the tenant's partition, and keep partitions the size a page's latency
allows.

**4. Capability booleans can push the optimizer off the tenant index.** The same list with
the per-row capability array (an EXISTS per transition in the select list) made the
optimizer choose a full scan of `Missions`, 512,886 rows and 1.7 seconds, where the list
without the array stayed on the partition. With `Missions(SectorId, Deadline)` present it
returned to the partition (20,216 rows, 169 ms). A cost-model choice, so it can go either
way as statistics change; the composite index makes the right choice cheap enough to be
the one taken.

**5. The visible projection costs the index.** `ORDER BY CASE WHEN <condition> THEN
Deadline END` cannot use an index on `Deadline`. With the composite index, the plain list
read 52 rows; the same list sorted on the conditionally visible column read 20,060 and
sorted them, 55 to 73 ms against 17. That is the price of the visible-projection rule
whenever the sort column is conditionally granted, and it is paid per page. When these
plans were captured the renderer also sorted on an `IS NULL` key ahead of every nullable
column (added because the emulator refuses `NULLS FIRST` and `NULLS LAST`), a second
expression an index cannot serve. That key is gone: a nullable column now renders as the
plain direction and sorts in Spanner's own `NULL` placement (first ascending, last
descending), so the composite index of finding 1 serves a nullable `@order` column too.
The shapes in `shapes/` carry the current text; s1c with `Consignments(SectorId,
ReleasedAt DESC)` present has not yet been re-measured.

**6. Write and insert checks are constant.** One to four rows scanned at every volume, 10
to 30 ms dominated by the round trip. The check-SELECT locates its row by primary key and
every EXISTS inside it is a point lookup. Tenancy proofs on insert are the same shape.

**7. Two-hop lists can stop early when an index carries the order.** The membership list
read `SquadronMembershipsByUserId`, which is already in `ORDER BY UserId` order, and
stopped after 82 rows once 26 of Anvil's had passed the two-hop check. The cost of that
strategy is the page size divided by the tenant's share of the index's order; Anvil's
synthetic users sort first, so 82 flatters it. It is the same mechanism finding 1's
composite index gives on purpose.

**8. Statistics matter, and they are not automatic after a load.** Spanner builds
statistics about every three days; the medium and large plans differ (the membership list
flipped strategy between them) and an `ANALYZE` after each load is what let the optimizer
see the rows. An application whose volume grows in a burst runs on stale statistics until
the next package.

## Guidance, by binding kind

- **Bare `@domain` on a listed resource:** an index on `(tenant key, @order columns)`. The
  foreign-key backing index alone means a sort of the partition per page.
- **Join-path `@domain(via: ...)`:** fine for writes and for small or parent-scoped
  tables. A table listed at volume needs the tenant key on the row; no index on the child
  substitutes.
- **`@subjectSet` anchors:** the anchor's key must lead with the compared column and the
  user column, which generation's requirement that the anchor be keyed or indexed by user
  already gives; the list still costs the partition per page.
- **`@subjectValue` anchors:** the unique index generation requires is all the scalar
  subquery needs; one row.
- **Conditionally visible sort columns:** every page sorts the partition; no index helps.
  Prefer unconditionally visible sort columns on lists that page at volume.
- **Capability booleans on large lists:** keep the composite tenant index in place so the
  optimizer has a cheap plan to prefer.
- **After a bulk load:** `ANALYZE`.

## Caveats

The statements carry literals where the application binds parameters; Spanner plans
parameterized statements the same way except where a literal's value changes an
estimate. Three shapes (Ships, SquadronMemberships, Sorties) are written by hand in the
renderer's form because the walkthrough does not list them; the visible-projection shape
is trimmed to the columns that decide its plan. The synthetic world is uniform, which
real data is not, and the instance is the smallest Spanner sells.
