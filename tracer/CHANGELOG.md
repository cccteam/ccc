# Changelog

## [0.2.0](https://github.com/cccteam/ccc/compare/tracer/v0.1.8...tracer/v0.2.0) (2026-10-08)


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

## [0.1.8](https://github.com/cccteam/ccc/compare/tracer/v0.1.7...tracer/v0.1.8) (2026-10-07)


### Code Upgrade

* **deps:** every module's Go dependencies move to their latest releases in one change, in place of dependabot's four open pull requests, with the cccteam pins kept at their releases and tracer's semconv import moved to the version the OpenTelemetry SDK's default resource is built on, which its own check requires, while the Google Cloud trace exporter and propagator stay at their current versions, since their later versions carry a deprecation notice that is a change of design for another day ([#837](https://github.com/cccteam/ccc/issues/837)) ([8eda1cf](https://github.com/cccteam/ccc/commit/8eda1cfe6b1b01887ecc826d6bfcb6aece5a011c))

## [0.1.7](https://github.com/cccteam/ccc/compare/tracer/v0.1.6...tracer/v0.1.7) (2026-08-19)


### Bug Fixes

* **deps:** bump Go toolchain to 1.26.6 to resolve stdlib CVEs ([#798](https://github.com/cccteam/ccc/issues/798)) ([6846035](https://github.com/cccteam/ccc/commit/684603579e91ff13394e2820ae9d6810519b4f59))

## [0.1.6](https://github.com/cccteam/ccc/compare/tracer/v0.1.5...tracer/v0.1.6) (2026-07-30)


### Code Upgrade

* go deps ([#787](https://github.com/cccteam/ccc/issues/787)) ([6efebd7](https://github.com/cccteam/ccc/commit/6efebd76f617d5c0ccb61543689b4ea2d1ab8cb8))

## [0.1.5](https://github.com/cccteam/ccc/compare/tracer/v0.1.4...tracer/v0.1.5) (2026-07-14)


### Features

* Add support for Client Options ([#746](https://github.com/cccteam/ccc/issues/746)) ([631c75f](https://github.com/cccteam/ccc/commit/631c75f689d4bf2b497839875676f36920f35f7d))


### Bug Fixes

* **deps:** update tracer gRPC dependency ([#761](https://github.com/cccteam/ccc/issues/761)) ([82216d1](https://github.com/cccteam/ccc/commit/82216d16e23d47fd926015932acef8c019739d03))

## [0.1.4](https://github.com/cccteam/ccc/compare/tracer/v0.1.3...tracer/v0.1.4) (2026-07-09)


### Bug Fixes

* address tracer and cache go vulns ([#754](https://github.com/cccteam/ccc/issues/754)) ([bdcbf1b](https://github.com/cccteam/ccc/commit/bdcbf1b2f37908c97514751c1e04b1ff43abc4ad))


### Dependencies

* Fix all go vulns ([#738](https://github.com/cccteam/ccc/issues/738)) ([ec6c058](https://github.com/cccteam/ccc/commit/ec6c058631faaa3f3c9f8ed093f6fab7d9be0168))


### Code Upgrade

* **deps:** Bump go.opentelemetry.io/otel/sdk from 1.41.0 to 1.43.0 in /tracer ([#740](https://github.com/cccteam/ccc/issues/740)) ([92ff5de](https://github.com/cccteam/ccc/commit/92ff5de5d50c0fad3712653ff23ee947cce76415))

## [0.1.3](https://github.com/cccteam/ccc/compare/tracer/v0.1.2...tracer/v0.1.3) (2026-06-05)


### Code Upgrade

* go 1.26.3 =&gt; 1.26.4 ([#734](https://github.com/cccteam/ccc/issues/734)) ([1b89aa7](https://github.com/cccteam/ccc/commit/1b89aa75106f683ecfce649b55bd5966a3e0bce0))

## [0.1.2](https://github.com/cccteam/ccc/compare/tracer/v0.1.1...tracer/v0.1.2) (2026-05-11)


### Code Upgrade

* **deps:** Bump google.golang.org/grpc in /tracer ([#710](https://github.com/cccteam/ccc/issues/710)) ([b14c69c](https://github.com/cccteam/ccc/commit/b14c69c535f8ae81ee15eaea73b20e9b28b32860))
* go 1.26.2 =&gt; 1.26.3 ([#718](https://github.com/cccteam/ccc/issues/718)) ([5e4aaca](https://github.com/cccteam/ccc/commit/5e4aacac7fee4b2350f369db22758bf1c3c0e691))
* Upgrade/remove ptr for new ([#663](https://github.com/cccteam/ccc/issues/663)) ([7dfd20c](https://github.com/cccteam/ccc/commit/7dfd20cd5ba0ecd8fe320b005f5f042c014037bc))

## [0.1.1](https://github.com/cccteam/ccc/compare/tracer/v0.1.0...tracer/v0.1.1) (2026-03-05)


### Code Upgrade

* Upgrade resource & tracer go deps to address GO-2026-4559 ([#657](https://github.com/cccteam/ccc/issues/657)) ([2a8e00a](https://github.com/cccteam/ccc/commit/2a8e00aeb466f0378d82e7244dbb662a6aad1cd2))

## [0.1.0](https://github.com/cccteam/ccc/compare/tracer/v0.0.4...tracer/v0.1.0) (2026-02-23)


### ⚠ BREAKING CHANGES

* Change return type for NewGoogleCloudTracerProvider() to hide otel package ([#649](https://github.com/cccteam/ccc/issues/649))

### Bug Fixes

* Add missing unit test for `traceResource()` ([#631](https://github.com/cccteam/ccc/issues/631)) ([d2d0616](https://github.com/cccteam/ccc/commit/d2d0616e1c467ef61bb7aa9bd3ac99c6e27f8e29))
* Fix Deprecation comment for `tracer.Start()` ([#631](https://github.com/cccteam/ccc/issues/631)) ([d2d0616](https://github.com/cccteam/ccc/commit/d2d0616e1c467ef61bb7aa9bd3ac99c6e27f8e29))


### Code Refactoring

* Change return type for NewGoogleCloudTracerProvider() to hide otel package ([#649](https://github.com/cccteam/ccc/issues/649)) ([56c6839](https://github.com/cccteam/ccc/commit/56c68397fd4913c15188c84609fbeceed4416d04))
* Implement build tags to switch exporter between dev and prod ([#649](https://github.com/cccteam/ccc/issues/649)) ([56c6839](https://github.com/cccteam/ccc/commit/56c68397fd4913c15188c84609fbeceed4416d04))


### Code Upgrade

* **deps:** Bump the go-dependencies group with 3 updates ([#631](https://github.com/cccteam/ccc/issues/631)) ([d2d0616](https://github.com/cccteam/ccc/commit/d2d0616e1c467ef61bb7aa9bd3ac99c6e27f8e29))
* Upgrade dependencies ([#649](https://github.com/cccteam/ccc/issues/649)) ([56c6839](https://github.com/cccteam/ccc/commit/56c68397fd4913c15188c84609fbeceed4416d04))

## [0.0.4](https://github.com/cccteam/ccc/compare/tracer/v0.0.3...tracer/v0.0.4) (2026-02-10)


### Code Upgrade

* go 1.25.6 =&gt; go 1.25.7 ([#633](https://github.com/cccteam/ccc/issues/633)) ([27ea695](https://github.com/cccteam/ccc/commit/27ea695642760b8dbccfe4ef47529f4200fefe66))

## [0.0.3](https://github.com/cccteam/ccc/compare/tracer/v0.0.2...tracer/v0.0.3) (2026-01-30)


### Code Upgrade

* Update go version (to 1.25.6) and deps ([#622](https://github.com/cccteam/ccc/issues/622)) ([b921e92](https://github.com/cccteam/ccc/commit/b921e929a22c03f6cd8beae197d4d6d9ae7f37d6))

## [0.0.2](https://github.com/cccteam/ccc/compare/tracer/v0.0.1...tracer/v0.0.2) (2026-01-28)


### Features

* New tracer package for OpenTelemetry tracing ([#617](https://github.com/cccteam/ccc/issues/617)) ([8a3f027](https://github.com/cccteam/ccc/commit/8a3f027fcf537d2d9a37d63c86f86aebb00b16cb))
