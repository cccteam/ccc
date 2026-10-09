# Changelog

## [0.3.0](https://github.com/cccteam/ccc/compare/impulse/v0.2.1...impulse/v0.3.0) (2026-10-09)


### ⚠ BREAKING CHANGES

* resource's StructDecoder is private; a plain request body decodes with httpio's StructDecoder ([#901](https://github.com/cccteam/ccc/issues/901))

### Features

* impulse owns security-scan.yml, govulncheck and Grype daily over the default branch and the latest release ([#878](https://github.com/cccteam/ccc/issues/878)) ([a9edc10](https://github.com/cccteam/ccc/commit/a9edc1039f697e878a86ed866c3c745032fb8f6c))
* request body limits in one place: the generated router bounds every route it wraps, RPC methods declare their own ([#900](https://github.com/cccteam/ccc/issues/900)) ([53389b9](https://github.com/cccteam/ccc/commit/53389b9f192c8cd155e471d6e77a6eae62c4587e))
* the release pins check refuses a release that pins a sibling at a pseudo-version; the ledger marks a pending step ([#902](https://github.com/cccteam/ccc/issues/902)) ([c43278d](https://github.com/cccteam/ccc/commit/c43278d0c7eb4308a76d1c9c6a3c420bba75e25d))


### Code Refactoring

* resource's StructDecoder is private; a plain request body decodes with httpio's StructDecoder ([#901](https://github.com/cccteam/ccc/issues/901)) ([166c1f6](https://github.com/cccteam/ccc/commit/166c1f66a0884a4cfba7e1d6302dad69806777a2))


### Code Upgrade

* Go 1.26.9 and x/net v0.60.0 in every module; net/http GO-2026-6612, GO-2026-6613 and GO-2026-6617 fixed ([#879](https://github.com/cccteam/ccc/issues/879)) ([4107fbb](https://github.com/cccteam/ccc/commit/4107fbb68a33305d109a6a8fec23e67986acf046))

## [0.2.1](https://github.com/cccteam/ccc/compare/impulse/v0.2.0...impulse/v0.2.1) (2026-10-08)


### Bug Fixes

* the skeleton pins tracer v0.2.0, cloud v0.1.0 and resource v0.12.0, the cloud driver's releases (@cccteam/impulse) ([#872](https://github.com/cccteam/ccc/issues/872)) ([047b46c](https://github.com/cccteam/ccc/commit/047b46c7fda1c03bfd255f2ec5f43c01a611d3d2))

## [0.2.0](https://github.com/cccteam/ccc/compare/impulse/v0.1.1...impulse/v0.2.0) (2026-10-08)


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
* every skeleton wires the tracer provider and the tracing handler (@cccteam/impulse) ([#861](https://github.com/cccteam/ccc/issues/861)) ([78006f9](https://github.com/cccteam/ccc/commit/78006f9273b15efefff4eec625685ca9c5633239))
* impulse upgrade moves the tool pin to the running impulse first, then walks the steps (@cccteam/impulse) ([#858](https://github.com/cccteam/ccc/issues/858)) ([78e2a54](https://github.com/cccteam/ccc/commit/78e2a54fd0a6df202ed6d8f97a6a3b9346fd560d))
* ledger step 2 pins cloud, resource and tracer 0.2, with the cloud-driver recipe (@cccteam/impulse) ([#864](https://github.com/cccteam/ccc/issues/864)) ([406bf2f](https://github.com/cccteam/ccc/commit/406bf2f27be0738147922e64a8452448343fd138))
* LogExporter replaces LoggerMiddleware on the App; the router builds the logger (@cccteam/impulse) ([#864](https://github.com/cccteam/ccc/issues/864)) ([406bf2f](https://github.com/cccteam/ccc/commit/406bf2f27be0738147922e64a8452448343fd138))
* LogExporter() replaces LoggerMiddleware() on the generated Handlers interface (@cccteam/resource) ([#864](https://github.com/cccteam/ccc/issues/864)) ([406bf2f](https://github.com/cccteam/ccc/commit/406bf2f27be0738147922e64a8452448343fd138))
* NewProvider and NewHandler replace the Google-named constructors (@cccteam/tracer) ([#864](https://github.com/cccteam/ccc/issues/864)) ([406bf2f](https://github.com/cccteam/ccc/commit/406bf2f27be0738147922e64a8452448343fd138))
* spans are exported over OTLP to Google Cloud's Telemetry API with default credentials (@cccteam/tracer) ([#864](https://github.com/cccteam/ccc/issues/864)) ([406bf2f](https://github.com/cccteam/ccc/commit/406bf2f27be0738147922e64a8452448343fd138))
* the cloud-driver recipe moves an application's logging and tracing into the driver (@cccteam/impulse) ([#864](https://github.com/cccteam/ccc/issues/864)) ([406bf2f](https://github.com/cccteam/ccc/commit/406bf2f27be0738147922e64a8452448343fd138))
* the generated router installs tracing and the request logger itself (@cccteam/resource) ([#864](https://github.com/cccteam/ccc/issues/864)) ([406bf2f](https://github.com/cccteam/ccc/commit/406bf2f27be0738147922e64a8452448343fd138))
* the ledger's test holds its last step to the skeleton's cccteam pins (@cccteam/impulse) ([#858](https://github.com/cccteam/ccc/issues/858)) ([78e2a54](https://github.com/cccteam/ccc/commit/78e2a54fd0a6df202ed6d8f97a6a3b9346fd560d))
* the provider sets the global propagator, so outgoing requests carry traceparent (@cccteam/tracer) ([#864](https://github.com/cccteam/ccc/issues/864)) ([406bf2f](https://github.com/cccteam/ccc/commit/406bf2f27be0738147922e64a8452448343fd138))
* the upgrade ledger records steps keyed by framework pin sets, not impulse versions (@cccteam/impulse) ([#858](https://github.com/cccteam/ccc/issues/858)) ([78e2a54](https://github.com/cccteam/ccc/commit/78e2a54fd0a6df202ed6d8f97a6a3b9346fd560d))
* W3C trace context propagation, with the legacy X-Cloud-Trace-Context header still read (@cccteam/tracer) ([#864](https://github.com/cccteam/ccc/issues/864)) ([406bf2f](https://github.com/cccteam/ccc/commit/406bf2f27be0738147922e64a8452448343fd138))
* WithClientOptions goes; WithEndpoint and WithInsecure name a collector of your own (@cccteam/tracer) ([#864](https://github.com/cccteam/ccc/issues/864)) ([406bf2f](https://github.com/cccteam/ccc/commit/406bf2f27be0738147922e64a8452448343fd138))
* WithSampling records every span (all) or the edge's choice (edge, the default) (@cccteam/tracer) ([#864](https://github.com/cccteam/ccc/issues/864)) ([406bf2f](https://github.com/cccteam/ccc/commit/406bf2f27be0738147922e64a8452448343fd138))
* WithTokenSource signs the Telemetry API calls with the caller's own token source (@cccteam/tracer) ([#864](https://github.com/cccteam/ccc/issues/864)) ([406bf2f](https://github.com/cccteam/ccc/commit/406bf2f27be0738147922e64a8452448343fd138))


### Bug Fixes

* a step's commit subject names the modules alone when the pins' versions run long (@cccteam/impulse) ([#864](https://github.com/cccteam/ccc/issues/864)) ([406bf2f](https://github.com/cccteam/ccc/commit/406bf2f27be0738147922e64a8452448343fd138))
* impulse upgrade leaves a pin already at or beyond a step's where it is (@cccteam/impulse) ([#858](https://github.com/cccteam/ccc/issues/858)) ([78e2a54](https://github.com/cccteam/ccc/commit/78e2a54fd0a6df202ed6d8f97a6a3b9346fd560d))

## [0.1.1](https://github.com/cccteam/ccc/compare/impulse/v0.1.0...impulse/v0.1.1) (2026-10-08)


### Features

* ci-cache.yml fills the caches after each push to the default or a hotfix branch (@cccteam/impulse) ([#853](https://github.com/cccteam/ccc/issues/853)) ([a67c658](https://github.com/cccteam/ccc/commit/a67c6580afb9dfc025d71f9447116b8ca05b93de))
* impulse check compares ci-cache.yml's branches with origin's default branch (@cccteam/impulse) ([#853](https://github.com/cccteam/ccc/issues/853)) ([a67c658](https://github.com/cccteam/ccc/commit/a67c6580afb9dfc025d71f9447116b8ca05b93de))
* the //impulse:ci line's default-branch names one other than main or master (@cccteam/impulse) ([#853](https://github.com/cccteam/ccc/issues/853)) ([a67c658](https://github.com/cccteam/ccc/commit/a67c6580afb9dfc025d71f9447116b8ca05b93de))
* the //impulse:ci line's large-runner names the jobs on the larger runner (@cccteam/impulse) ([#853](https://github.com/cccteam/ccc/issues/853)) ([a67c658](https://github.com/cccteam/ccc/commit/a67c6580afb9dfc025d71f9447116b8ca05b93de))
* the //impulse:ci line's test-cache=on reuses Go's cached test results (@cccteam/impulse) ([#853](https://github.com/cccteam/ccc/issues/853)) ([a67c658](https://github.com/cccteam/ccc/commit/a67c6580afb9dfc025d71f9447116b8ca05b93de))
* the build and test legs restore the Go module and build caches between runs (@cccteam/impulse) ([#853](https://github.com/cccteam/ccc/issues/853)) ([a67c658](https://github.com/cccteam/ccc/commit/a67c6580afb9dfc025d71f9447116b8ca05b93de))


### Bug Fixes

* the upgrade ledger records impulse v0.1.1, which rewrites the owned workflows (@cccteam/impulse) ([#857](https://github.com/cccteam/ccc/issues/857)) ([ddffdd7](https://github.com/cccteam/ccc/commit/ddffdd772532605cb889d78103c0f5abae248393))

## 0.1.0 (2026-10-06)


### Features

* every auth issues its XSRF token in its own cookie, a refused login arrives as a code the page maps to its own sentence, no page may frame the application, and a Google directory auth reads role groups through the Cloud Identity Groups API with the person's own sign-in token. ([#828](https://github.com/cccteam/ccc/issues/828)) ([37ac74f](https://github.com/cccteam/ccc/commit/37ac74fef0b2e553d72821ecc249f7a393229f95))
* every listed resource in the skeletons declares its @`order` with the index that serves it, since a paged request that names no sort is refused without one. ([#828](https://github.com/cccteam/ccc/issues/828)) ([37ac74f](https://github.com/cccteam/ccc/commit/37ac74fef0b2e553d72821ecc249f7a393229f95))
* every skeleton carries the generated router, the generated authorization matrix, a migrate command with -seed, -version, -force and -force-data, a bootstrap that applies the development seed, a deploy-time role validation test, a warnings test per generate program and the conditions-proven harness. ([#828](https://github.com/cccteam/ccc/issues/828)) ([37ac74f](https://github.com/cccteam/ccc/commit/37ac74fef0b2e553d72821ecc249f7a393229f95))
* every skeleton runs on the framework's server (HTTP/1.1 and HTTP/2 cleartext on one listener), serves maintenance when APP_MAINTENANCE is set, opens the required live service (Firestore in a deployment, the emulator in development) and hands every permission engine the change signal. ([#828](https://github.com/cccteam/ccc/issues/828)) ([37ac74f](https://github.com/cccteam/ccc/commit/37ac74fef0b2e553d72821ecc249f7a393229f95))
* impulse add outlet, add tenancy, add auth (password, oidc-azure, oidc-google or preauth, with the directory or the application owning role membership), add site, add feature and add files lay a step in and hand the rest of the wiring to the agent. ([#828](https://github.com/cccteam/ccc/issues/828)) ([37ac74f](https://github.com/cccteam/ccc/commit/37ac74fef0b2e553d72821ecc249f7a393229f95))
* impulse audit runs every generate program with -audit and prints its warning and audit lines; impulse advise writes a brief asking an agent, with read-only tools, for the judgment the framework's warnings leave to the application. ([#828](https://github.com/cccteam/ccc/issues/828)) ([37ac74f](https://github.com/cccteam/ccc/commit/37ac74fef0b2e553d72821ecc249f7a393229f95))
* impulse check reads the application and reports one line per check (generator program, options, tenancy, outlets, sites, maintenance switch, session tables, auths, change signal, conditions proven, skipauth, emulator version, the ignore files, paging, rpc-execute, pins and the CI workflow), so a hand edit or a drifted shape is found before the pull request. ([#828](https://github.com/cccteam/ccc/issues/828)) ([37ac74f](https://github.com/cccteam/ccc/commit/37ac74fef0b2e553d72821ecc249f7a393229f95))
* impulse handoff hands the failing checks to an agent with a brief that says what each check means, and verifies the guardrails afterwards; every new and add ends in one. ([#828](https://github.com/cccteam/ccc/issues/828)) ([37ac74f](https://github.com/cccteam/ccc/commit/37ac74fef0b2e553d72821ecc249f7a393229f95))
* impulse new creates an application from the embedded skeletons (solo, tenanted, outlets, sites) with its first auth named, composing tenancy, outlets, sites and the file store in one handoff, and pins the tool at the impulse that created it. ([#828](https://github.com/cccteam/ccc/issues/828)) ([37ac74f](https://github.com/cccteam/ccc/commit/37ac74fef0b2e553d72821ecc249f7a393229f95))
* impulse render rewrites the owned files from the code; the ci package renders and owns .github/workflows/ci.yml: the fixed job names a repository rule requires (title, go, web, image, secrets, migrations), the Go checks as eight legs that run at once under the go gate (build, the two test legs, the two lint legs, govulncheck, Semgrep, impulse check), one angular job per browser workspace under the web gate, the test legs and the image build on the larger runner the variable CI_LARGE_RUNNER names, every action pinned by commit and every tool version a constant. ([#828](https://github.com/cccteam/ccc/issues/828)) ([37ac74f](https://github.com/cccteam/ccc/commit/37ac74fef0b2e553d72821ecc249f7a393229f95))
* impulse upgrade walks an application through the impulse releases the ledger records, one commit per release: the release's recipes, the pins moved, the owned files rendered, go generate and impulse check, committed as upgrade:; a failing check stops the walk with the handoff brief written. ([#828](https://github.com/cccteam/ccc/issues/828)) ([37ac74f](https://github.com/cccteam/ccc/commit/37ac74fef0b2e553d72821ecc249f7a393229f95))
* the browser apps install as progressive web apps served through resource.NewBrowserApp, send the release they were built from in X-Api-Version, run component specs with Vitest, and build and test in CI with Bun at the version that wrote the lockfile. ([#828](https://github.com/cccteam/ccc/issues/828)) ([37ac74f](https://github.com/cccteam/ccc/commit/37ac74fef0b2e553d72821ecc249f7a393229f95))
* the deployhook package is the contract for an application's deploy hooks (BeforeMigrate, AfterMigrate, BeforeTraffic and AfterTraffic over typed facts), and the app package is exported so the infrastructure companion reads what an application declares. ([#828](https://github.com/cccteam/ccc/issues/828)) ([37ac74f](https://github.com/cccteam/ccc/commit/37ac74fef0b2e553d72821ecc249f7a393229f95))
* the tenanted skeletons declare the tenant record with @`tenant` and build the generated tenant roster, so a tenant created through the API is served under its segment at once. ([#828](https://github.com/cccteam/ccc/issues/828)) ([37ac74f](https://github.com/cccteam/ccc/commit/37ac74fef0b2e553d72821ecc249f7a393229f95))
