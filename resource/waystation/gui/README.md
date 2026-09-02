# Waystation GUI

The Angular frontend for the waystation demo application, built on
[`@cccteam/ccc-lib`](https://www.npmjs.com/package/@cccteam/ccc-lib): the global
resource pages (Catalog Items, Suppliers, Staff Members, Waystations) are
config-driven over the generated metadata in `src/app/core/service/` (`zz_gen_*.ts`,
produced by `go generate ./...` in the module root — never edit by hand).

The waystation-scoped surfaces (Work Orders, Requisitions, Status Board, Incidents,
Logistics) are hand-written components under `src/app/components/waystation/` on the
generated API client: `zz_gen_api.ts` (also generated) plus the framework-neutral
runtime `@cccteam/resource`. `WaystationService.station()` is the client bound to the
selected waystation — its typed handles fill the `{waystationID}` segment, address the
consolidated mutation endpoint, and post to the Execute-gated RPC routes — and the
pages double as the demo of per-station permission partitioning, stateful workflow
transitions, and per-cell masking (a masked cell arrives with its JSON key absent and
renders as an em-dash, never a zero).

The generated files import `@cccteam/resource`; while developing against the local
ccc-lib checkout, `npm run ccclib:local` builds both packages there and attaches them
through yalc (`ccclib:push` rebuilds, `ccclib:restore` returns to the registry pins).

## Development

Serve the API first (see the module README for the emulator + bootstrap + server
steps), then:

```
npm install
npm start
```

`ng serve` proxies `/api` to the Go server on `127.0.0.1:8082` (override with
`WAYSTATION_PORT`, see `proxy.conf.js`).

## Production build

```
npm run build
```

The build lands in `dist/`, which the Go server serves directly (`WAYSTATION_GUI_DIST`
overrides the location).

Sign in as any of the seeded personas — `commander`, `chief-alpha`, `tech-rivera`,
`foreman-okafor`, `procurement-chen`, `auditor-voss`, `quartermaster-idris` — with the
shared password `waystation`.
