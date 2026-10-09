# Changelog

## [0.2.1](https://github.com/cccteam/ccc/compare/cloud/v0.2.0...cloud/v0.2.1) (2026-10-09)


### Features

* cloud/gcp exports its settings declaration; impulse's env-template check and bedrock's derivation read it ([#906](https://github.com/cccteam/ccc/issues/906)) ([d0b9f3e](https://github.com/cccteam/ccc/commit/d0b9f3ee63f58520481877765d664f4fe3bd9602))


### Bug Fixes

* the settings declaration moves to cloud/gcp/declaration, apart from the driver, so impulse links no cloud client ([#910](https://github.com/cccteam/ccc/issues/910)) ([8b3748a](https://github.com/cccteam/ccc/commit/8b3748a1b889a1650bc13dc43f90af2e62880761))

## [0.2.0](https://github.com/cccteam/ccc/compare/cloud/v0.1.0...cloud/v0.2.0) (2026-10-09)


### ⚠ BREAKING CHANGES

* resource's StructDecoder is private; a plain request body decodes with httpio's StructDecoder ([#901](https://github.com/cccteam/ccc/issues/901))

### Code Refactoring

* resource's StructDecoder is private; a plain request body decodes with httpio's StructDecoder ([#901](https://github.com/cccteam/ccc/issues/901)) ([166c1f6](https://github.com/cccteam/ccc/commit/166c1f66a0884a4cfba7e1d6302dad69806777a2))


### Code Upgrade

* Go 1.26.9 and x/net v0.60.0 in every module; net/http GO-2026-6612, GO-2026-6613 and GO-2026-6617 fixed ([#879](https://github.com/cccteam/ccc/issues/879)) ([4107fbb](https://github.com/cccteam/ccc/commit/4107fbb68a33305d109a6a8fec23e67986acf046))

## 0.1.0 (2026-10-08)


### ⚠ BREAKING CHANGES

* spans are exported over OTLP to Google Cloud's Telemetry API with default credentials (@cccteam/tracer) ([#864](https://github.com/cccteam/ccc/issues/864))
* WithClientOptions goes; WithEndpoint and WithInsecure name a collector of your own (@cccteam/tracer) ([#864](https://github.com/cccteam/ccc/issues/864))
* the generated router installs tracing and the request logger itself (@cccteam/resource) ([#864](https://github.com/cccteam/ccc/issues/864))
* LogExporter() replaces LoggerMiddleware() on the generated Handlers interface (@cccteam/resource) ([#864](https://github.com/cccteam/ccc/issues/864))
* every skeleton opens the cloud driver for its logs and spans (@cccteam/impulse) ([#864](https://github.com/cccteam/ccc/issues/864))
* LogExporter replaces LoggerMiddleware on the App; the router builds the logger (@cccteam/impulse) ([#864](https://github.com/cccteam/ccc/issues/864))
* NewProvider and NewHandler replace the Google-named constructors (@cccteam/tracer) ([#864](https://github.com/cccteam/ccc/issues/864))

### Features

* cloud/gcp: Settings and Open build the log exporter and the trace provider (@cccteam/cloud) ([#864](https://github.com/cccteam/ccc/issues/864)) ([406bf2f](https://github.com/cccteam/ccc/commit/406bf2f27be0738147922e64a8452448343fd138))
* every skeleton opens the cloud driver for its logs and spans (@cccteam/impulse) ([#864](https://github.com/cccteam/ccc/issues/864)) ([406bf2f](https://github.com/cccteam/ccc/commit/406bf2f27be0738147922e64a8452448343fd138))
* ledger step 2 pins cloud, resource and tracer 0.2, with the cloud-driver recipe (@cccteam/impulse) ([#864](https://github.com/cccteam/ccc/issues/864)) ([406bf2f](https://github.com/cccteam/ccc/commit/406bf2f27be0738147922e64a8452448343fd138))
* LogExporter replaces LoggerMiddleware on the App; the router builds the logger (@cccteam/impulse) ([#864](https://github.com/cccteam/ccc/issues/864)) ([406bf2f](https://github.com/cccteam/ccc/commit/406bf2f27be0738147922e64a8452448343fd138))
* LogExporter() replaces LoggerMiddleware() on the generated Handlers interface (@cccteam/resource) ([#864](https://github.com/cccteam/ccc/issues/864)) ([406bf2f](https://github.com/cccteam/ccc/commit/406bf2f27be0738147922e64a8452448343fd138))
* NewProvider and NewHandler replace the Google-named constructors (@cccteam/tracer) ([#864](https://github.com/cccteam/ccc/issues/864)) ([406bf2f](https://github.com/cccteam/ccc/commit/406bf2f27be0738147922e64a8452448343fd138))
* spans are exported over OTLP to Google Cloud's Telemetry API with default credentials (@cccteam/tracer) ([#864](https://github.com/cccteam/ccc/issues/864)) ([406bf2f](https://github.com/cccteam/ccc/commit/406bf2f27be0738147922e64a8452448343fd138))
* the cloud-driver recipe moves an application's logging and tracing into the driver (@cccteam/impulse) ([#864](https://github.com/cccteam/ccc/issues/864)) ([406bf2f](https://github.com/cccteam/ccc/commit/406bf2f27be0738147922e64a8452448343fd138))
* the generated router installs tracing and the request logger itself (@cccteam/resource) ([#864](https://github.com/cccteam/ccc/issues/864)) ([406bf2f](https://github.com/cccteam/ccc/commit/406bf2f27be0738147922e64a8452448343fd138))
* the provider sets the global propagator, so outgoing requests carry traceparent (@cccteam/tracer) ([#864](https://github.com/cccteam/ccc/issues/864)) ([406bf2f](https://github.com/cccteam/ccc/commit/406bf2f27be0738147922e64a8452448343fd138))
* W3C trace context propagation, with the legacy X-Cloud-Trace-Context header still read (@cccteam/tracer) ([#864](https://github.com/cccteam/ccc/issues/864)) ([406bf2f](https://github.com/cccteam/ccc/commit/406bf2f27be0738147922e64a8452448343fd138))
* WithClientOptions goes; WithEndpoint and WithInsecure name a collector of your own (@cccteam/tracer) ([#864](https://github.com/cccteam/ccc/issues/864)) ([406bf2f](https://github.com/cccteam/ccc/commit/406bf2f27be0738147922e64a8452448343fd138))
* WithSampling records every span (all) or the edge's choice (edge, the default) (@cccteam/tracer) ([#864](https://github.com/cccteam/ccc/issues/864)) ([406bf2f](https://github.com/cccteam/ccc/commit/406bf2f27be0738147922e64a8452448343fd138))
* WithTokenSource signs the Telemetry API calls with the caller's own token source (@cccteam/tracer) ([#864](https://github.com/cccteam/ccc/issues/864)) ([406bf2f](https://github.com/cccteam/ccc/commit/406bf2f27be0738147922e64a8452448343fd138))


### Bug Fixes

* a step's commit subject names the modules alone when the pins' versions run long (@cccteam/impulse) ([#864](https://github.com/cccteam/ccc/issues/864)) ([406bf2f](https://github.com/cccteam/ccc/commit/406bf2f27be0738147922e64a8452448343fd138))
