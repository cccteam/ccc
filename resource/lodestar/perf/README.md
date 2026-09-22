# perf: plans and statistics on a real instance

The emulator returns no query plans and does not promise the service's execution plans,
so the shapes the resource package injects into the application's queries (tenancy, grant
conditions, subject sets and values, the visible projection, the write checks) are
measured here against a real Cloud Spanner instance, the one the README's "Running against
a real Spanner instance" procedure targets. Every script reads that target from the same
environment variables the application does.

- `shapes/*.sql`: one statement per shape, the renderer's own text with its parameters
  bound to literals from the seed (`'anvil'`, `'lead'`, the enumeration ids). The first
  comment line of each file says what the shape is and where its text came from.
- `plans.sh <output-dir> [shape...]`: runs every shape in PROFILE mode and saves Spanner's
  plan with per-node execution statistics, one file per shape.
- `summarize.sh <output-dir>...`: one line per shape per capture: elapsed, CPU, rows
  returned, rows scanned, what the plan scans, how it sorts and joins.
- `catalogue.sh [10MINUTE|HOUR]`: every statement the application issued in the latest
  complete window, from `SPANNER_SYS.QUERY_STATS_TOP_*`, with executions, rows scanned and
  returned, and latency. Run it after a walkthrough for the statement catalogue.
- `../cmd/volume -scale medium|large`: synthetic rows over the seeded world, batched insert
  mutations in foreign-key order, no DDL, so the same shapes can be read at volume. A
  bootstrap `-reset` removes them.

The measured procedure: bootstrap, `plans.sh` at seed volume, `cmd/volume -scale medium`,
`ANALYZE` (`gcloud spanner databases ddl update <db> --ddl='ANALYZE'`, so the optimizer's
statistics see the new rows), `plans.sh` again; then a bootstrap `-reset` (the presets
share their identifiers, so one scale must go before the next comes), `cmd/volume -scale
large`, `ANALYZE`, `plans.sh`; then `-reset` to leave the seed. The findings and the
indexing guidance they support are in `REPORT.md`.
