# Changelog

## [0.0.11](https://github.com/cccteam/ccc/compare/middleware/v0.0.10...middleware/v0.0.11) (2026-10-09)


### Code Upgrade

* Go 1.26.9 and x/net v0.60.0 in every module; net/http GO-2026-6612, GO-2026-6613 and GO-2026-6617 fixed ([#879](https://github.com/cccteam/ccc/issues/879)) ([4107fbb](https://github.com/cccteam/ccc/commit/4107fbb68a33305d109a6a8fec23e67986acf046))

## [0.0.10](https://github.com/cccteam/ccc/compare/middleware/v0.0.9...middleware/v0.0.10) (2026-10-07)


### Code Upgrade

* **deps:** every module's Go dependencies move to their latest releases in one change, in place of dependabot's four open pull requests, with the cccteam pins kept at their releases and tracer's semconv import moved to the version the OpenTelemetry SDK's default resource is built on, which its own check requires, while the Google Cloud trace exporter and propagator stay at their current versions, since their later versions carry a deprecation notice that is a change of design for another day ([#837](https://github.com/cccteam/ccc/issues/837)) ([8eda1cf](https://github.com/cccteam/ccc/commit/8eda1cfe6b1b01887ecc826d6bfcb6aece5a011c))

## [0.0.9](https://github.com/cccteam/ccc/compare/middleware/v0.0.8...middleware/v0.0.9) (2026-08-19)


### Bug Fixes

* **deps:** bump Go toolchain to 1.26.6 to resolve stdlib CVEs ([#798](https://github.com/cccteam/ccc/issues/798)) ([6846035](https://github.com/cccteam/ccc/commit/684603579e91ff13394e2820ae9d6810519b4f59))


### Code Upgrade

* **deps:** Bump the go-dependencies group across 1 directory with 3 updates ([#795](https://github.com/cccteam/ccc/issues/795)) ([e8c9b2e](https://github.com/cccteam/ccc/commit/e8c9b2ececafc767a4bac2deb5a7580ed8fe3273))

## [0.0.8](https://github.com/cccteam/ccc/compare/middleware/v0.0.7...middleware/v0.0.8) (2026-07-30)


### Code Upgrade

* **deps:** Bump the go-dependencies group across 1 directory with 3 updates ([#774](https://github.com/cccteam/ccc/issues/774)) ([15fea1c](https://github.com/cccteam/ccc/commit/15fea1c16a9a2c6c7e3dc714010307c4f828fa92))

## [0.0.7](https://github.com/cccteam/ccc/compare/middleware/v0.0.6...middleware/v0.0.7) (2026-07-30)


### Code Upgrade

* **deps:** Bump github.com/cccteam/ccc/tracer ([#757](https://github.com/cccteam/ccc/issues/757)) ([7284824](https://github.com/cccteam/ccc/commit/7284824115450daebb20c02b7bb73c98774e493f))
* go deps ([#787](https://github.com/cccteam/ccc/issues/787)) ([6efebd7](https://github.com/cccteam/ccc/commit/6efebd76f617d5c0ccb61543689b4ea2d1ab8cb8))

## [0.0.6](https://github.com/cccteam/ccc/compare/middleware/v0.0.5...middleware/v0.0.6) (2026-07-09)


### Code Upgrade

* **deps:** Bump the go-dependencies group across 1 directory with 3 updates ([#751](https://github.com/cccteam/ccc/issues/751)) ([4006840](https://github.com/cccteam/ccc/commit/4006840b7055b9101c9ae0778207ef037f59d87e))

## [0.0.5](https://github.com/cccteam/ccc/compare/middleware/v0.0.4...middleware/v0.0.5) (2026-06-05)


### Code Upgrade

* dependencies ([#726](https://github.com/cccteam/ccc/issues/726)) ([9342cdd](https://github.com/cccteam/ccc/commit/9342cdde848c6319adb250c2082f4387cd476d69))
* go 1.26.3 =&gt; 1.26.4 ([#734](https://github.com/cccteam/ccc/issues/734)) ([1b89aa7](https://github.com/cccteam/ccc/commit/1b89aa75106f683ecfce649b55bd5966a3e0bce0))

## [0.0.4](https://github.com/cccteam/ccc/compare/middleware/v0.0.3...middleware/v0.0.4) (2026-05-11)


### Code Upgrade

* **deps:** Bump the go-dependencies group across 1 directory with 2 updates ([#707](https://github.com/cccteam/ccc/issues/707)) ([33bc60d](https://github.com/cccteam/ccc/commit/33bc60d32cd0901c0b51b6fcbd8467baec60a51f))
* go 1.26.2 =&gt; 1.26.3 ([#718](https://github.com/cccteam/ccc/issues/718)) ([5e4aaca](https://github.com/cccteam/ccc/commit/5e4aacac7fee4b2350f369db22758bf1c3c0e691))

## [0.0.3](https://github.com/cccteam/ccc/compare/middleware/v0.0.2...middleware/v0.0.3) (2026-03-27)


### Features

* Allow Host override from environment variable ([#683](https://github.com/cccteam/ccc/issues/683)) ([a55e961](https://github.com/cccteam/ccc/commit/a55e9614d120b8c683675c8608c5ee7852262608))

## [0.0.2](https://github.com/cccteam/ccc/compare/middleware/v0.0.1...middleware/v0.0.2) (2026-03-26)


### Features

* Implement middleware to validate requests coming from Google Services ([#680](https://github.com/cccteam/ccc/issues/680)) ([6b97bb2](https://github.com/cccteam/ccc/commit/6b97bb26dd010310fdbe97a287ec314423c30a9d))
