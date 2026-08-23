# Starport GUI

The Angular frontend for the starport demo application, built on
[`@cccteam/ccc-lib`](https://www.npmjs.com/package/@cccteam/ccc-lib): the resource
pages (Ships, Docking Bays, Crew Members, Cargo Manifests, Supply Crates) are
config-driven over the generated metadata in `src/app/core/service/` (`zz_gen_*.ts`,
produced by `go generate ./...` in the module root — never edit by hand).

The station-scoped surfaces (Berths, Authorize Docking) are hand-written components
under `src/app/components/stations/`: the generated TypeScript metadata does not carry
domain routes yet, so those pages address `/api/stations/{stationID}/...` directly and
double as the demo of per-station permission partitioning (the demo user is authorized
in station-alpha only).

## Development

Serve the API first (see the module README for the emulator + bootstrap + server
steps), then:

```
npm install
npm start
```

`ng serve` proxies `/api` to the Go server on `127.0.0.1:8080` (override with
`STARPORT_PORT`, see `proxy.conf.js`).

## Production build

```
npm run build
```

The build lands in `dist/`, which the Go server serves directly (`STARPORT_GUI_DIST`
overrides the location).

Sign in with the seeded demo credentials: `demo` / `starport`.
