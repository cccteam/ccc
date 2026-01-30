# Changelog

## [0.2.22](https://github.com/cccteam/ccc/compare/v0.2.21...v0.2.22) (2026-01-30)


### Features

* New tracer package for OpenTelemetry tracing ([#617](https://github.com/cccteam/ccc/issues/617)) ([8a3f027](https://github.com/cccteam/ccc/commit/8a3f027fcf537d2d9a37d63c86f86aebb00b16cb))


### Code Upgrade

* **deps:** Bump the github-actions group with 3 updates ([#603](https://github.com/cccteam/ccc/issues/603)) ([1ea1c89](https://github.com/cccteam/ccc/commit/1ea1c8967495ee5768a91848f7f0c775c9ce8ffe))
* **deps:** Bump the github-actions group with 3 updates ([#616](https://github.com/cccteam/ccc/issues/616)) ([d41f5e6](https://github.com/cccteam/ccc/commit/d41f5e678f92068b40e988077fa78ea2061023ea))
* Update go version (to 1.25.6) and deps ([#622](https://github.com/cccteam/ccc/issues/622)) ([b921e92](https://github.com/cccteam/ccc/commit/b921e929a22c03f6cd8beae197d4d6d9ae7f37d6))

## [0.2.21](https://github.com/cccteam/ccc/compare/v0.2.20...v0.2.21) (2025-12-11)


### Code Upgrade

* **deps:** Bump the go-dependencies group with 3 updates ([#584](https://github.com/cccteam/ccc/issues/584)) ([e29f780](https://github.com/cccteam/ccc/commit/e29f780b979678fd648c8964ea4c7c9e31af05ff))

## [0.2.20](https://github.com/cccteam/ccc/compare/v0.2.19...v0.2.20) (2025-12-08)


### Code Upgrade

* go =&gt; 1.25.5 and dependencies ([#570](https://github.com/cccteam/ccc/issues/570)) ([8476082](https://github.com/cccteam/ccc/commit/8476082d73d3844d454962f9850aec543bff1922))

## [0.2.19](https://github.com/cccteam/ccc/compare/v0.2.18...v0.2.19) (2025-12-08)


### Code Upgrade

* **deps:** Bump the github-actions group with 3 updates ([#567](https://github.com/cccteam/ccc/issues/567)) ([60c02c2](https://github.com/cccteam/ccc/commit/60c02c2175faaf73ac90d181c22954deb48db2e0))

## [0.2.18](https://github.com/cccteam/ccc/compare/v0.2.17...v0.2.18) (2025-12-01)


### Features

* Rename workspace to show which repo the workspace is in ([#560](https://github.com/cccteam/ccc/issues/560)) ([084065d](https://github.com/cccteam/ccc/commit/084065d25c0fbad4a84e890a9aff3650b8a9e57f))

## [0.2.17](https://github.com/cccteam/ccc/compare/v0.2.16...v0.2.17) (2025-11-26)


### Code Upgrade

* CI/CD Workflows ([#551](https://github.com/cccteam/ccc/issues/551)) ([919d233](https://github.com/cccteam/ccc/commit/919d23389933a8b6f9f57f4e53b04e2e3aa420d9))
* **deps:** Bump the github-actions group with 3 updates ([#538](https://github.com/cccteam/ccc/issues/538)) ([0b2da84](https://github.com/cccteam/ccc/commit/0b2da84a9099c857641ed131f80368347a167564))

## [0.2.16](https://github.com/cccteam/ccc/compare/v0.2.15...v0.2.16) (2025-11-21)


### Features

* key hashing for secure storage ([#523](https://github.com/cccteam/ccc/issues/523)) ([663d639](https://github.com/cccteam/ccc/commit/663d6394f790a41be7a0ebef4ae056a2d4b4eac0))
* UpdatePatch.Apply() and UpdatePatch.Buffer() will noop for empty patches ([#530](https://github.com/cccteam/ccc/issues/530)) ([0b58c07](https://github.com/cccteam/ccc/commit/0b58c07bde4fa81a9afce47546dcbd09bb9a2db5))


### Bug Fixes

* Force panic if iterators from BatchIter2 are not used properly ([#543](https://github.com/cccteam/ccc/issues/543)) ([2002d7a](https://github.com/cccteam/ccc/commit/2002d7a81044303840874c980561995db0d0c613))

## [0.2.15](https://github.com/cccteam/ccc/compare/v0.2.14...v0.2.15) (2025-11-11)


### Features

* Implement a StartTrace() function to resolve package and function name. ([#519](https://github.com/cccteam/ccc/issues/519)) ([eb9a318](https://github.com/cccteam/ccc/commit/eb9a3184ffa5f63fbe07af9b4b46e7cef682ac7c))

## [0.2.14](https://github.com/cccteam/ccc/compare/v0.2.13...v0.2.14) (2025-10-31)


### Features

* Implement BatchIter2(), an iterator to break a larger iterator stream into batches ([#504](https://github.com/cccteam/ccc/issues/504)) ([c2f37c4](https://github.com/cccteam/ccc/commit/c2f37c4b352fc3f8653630fded9b5a3b2415a530))
* Implement NextIter(), which will generate a iter.Seq2 from any thing that implements NextIterator ([#506](https://github.com/cccteam/ccc/issues/506)) ([8136761](https://github.com/cccteam/ccc/commit/81367617912967ff45f546bd1a364c4ed3ed537a))
* Implement ReadIter(), which will generate a iter.Seq2 from any thing that implements ReadIterator ([#506](https://github.com/cccteam/ccc/issues/506)) ([8136761](https://github.com/cccteam/ccc/commit/81367617912967ff45f546bd1a364c4ed3ed537a))
* include new cache package in release-please config ([#420](https://github.com/cccteam/ccc/issues/420)) ([0c37a55](https://github.com/cccteam/ccc/commit/0c37a55811d88d8e87120417145f070c7ff24ed6))
* Setup code workspace file to handle linting mono-repo ([#441](https://github.com/cccteam/ccc/issues/441)) ([13d81e6](https://github.com/cccteam/ccc/commit/13d81e6ce7dedf538c8e2dff5cbf030d1ef626d1))


### Bug Fixes

* update cache release please version ([#422](https://github.com/cccteam/ccc/issues/422)) ([ba4467e](https://github.com/cccteam/ccc/commit/ba4467e75dee6396a216c1f48867c4d60864da45))


### Code Refactoring

* Move Release Please from Bot to GitHub Action ([be3979c](https://github.com/cccteam/ccc/commit/be3979cd374b7aa60ba77cf3eecd2acb89549775))


### Code Cleanup

* Fix linter issues ([#449](https://github.com/cccteam/ccc/issues/449)) ([cdbf85a](https://github.com/cccteam/ccc/commit/cdbf85ac3a7695f18d1d76939e23e274309561b6))
* Remove Placeholders and re-consolidate imports ([#486](https://github.com/cccteam/ccc/issues/486)) ([7dd0142](https://github.com/cccteam/ccc/commit/7dd01426aa5ed7104a5f28dabce22293c0f73328))

## [0.2.13](https://github.com/cccteam/ccc/compare/v0.2.12...v0.2.13) (2025-07-21)


### Bug Fixes

* Properly handle nil pointer values in NullEnum.DecodeSpanner() ([#375](https://github.com/cccteam/ccc/issues/375)) ([c1a5e41](https://github.com/cccteam/ccc/commit/c1a5e41e4bf81374837508b03352870b6bbde1ec))

## [0.2.12](https://github.com/cccteam/ccc/compare/v0.2.11...v0.2.12) (2025-07-16)


### Features

* Add a NullEnum type ([#363](https://github.com/cccteam/ccc/issues/363)) ([d037dc2](https://github.com/cccteam/ccc/commit/d037dc28dc976fb1cacaa54a7cbaf844f6c0962c))

## [0.2.11](https://github.com/cccteam/ccc/compare/v0.2.10...v0.2.11) (2025-06-18)


### Code Upgrade

* Update Go version to 1.24.4 to address GO-2025-3750 ([#336](https://github.com/cccteam/ccc/issues/336)) ([62ed5d4](https://github.com/cccteam/ccc/commit/62ed5d4bc52c75565f99ba0fe6b0a5d2b657ca78))

## [0.2.10](https://github.com/cccteam/ccc/compare/v0.2.9...v0.2.10) (2025-06-02)


### Code Upgrade

* ccc and sub repos GO version to `1.23.6` and all dependencies except CCC authored packages ([#178](https://github.com/cccteam/ccc/issues/178)) ([117a49d](https://github.com/cccteam/ccc/commit/117a49d3740b461d1b295047cdeaf85b4cacb53f))
* **deps:** Bump github.com/google/go-cmp in the go-dependencies group ([#221](https://github.com/cccteam/ccc/issues/221)) ([2dbbff6](https://github.com/cccteam/ccc/commit/2dbbff605ff8575402bf5992e128edb7f597a17e))

## [0.2.9](https://github.com/cccteam/ccc/compare/v0.2.8...v0.2.9) (2024-10-21)


### Features

* Add `NewDuration()` and `NewDurationFromString()` constructors ([#104](https://github.com/cccteam/ccc/issues/104)) ([6caff80](https://github.com/cccteam/ccc/commit/6caff805e9540d2b72ef40e4c9a15621e96f1f90))
* Implement `NullDuration` type ([#104](https://github.com/cccteam/ccc/issues/104)) ([6caff80](https://github.com/cccteam/ccc/commit/6caff805e9540d2b72ef40e4c9a15621e96f1f90))

## [0.2.8](https://github.com/cccteam/ccc/compare/v0.2.7...v0.2.8) (2024-10-02)


### Features

* Add new Duration type which supports JSON and Spanner marshaling ([#57](https://github.com/cccteam/ccc/issues/57)) ([1d2db06](https://github.com/cccteam/ccc/commit/1d2db06b145d9ac011c4e45a79620d335f982fe6))

## [0.2.7](https://github.com/cccteam/ccc/compare/v0.2.6...v0.2.7) (2024-09-25)


### Bug Fixes

* Exclude sub-package changes from base package ([#38](https://github.com/cccteam/ccc/issues/38)) ([a9132d1](https://github.com/cccteam/ccc/commit/a9132d17f1ddfb94cb5a3504835d8ee628aff235))

## [0.2.6](https://github.com/cccteam/ccc/compare/v0.2.5...v0.2.6) (2024-09-25)


### Features

* Add license ([#29](https://github.com/cccteam/ccc/issues/29)) ([b33d9be](https://github.com/cccteam/ccc/commit/b33d9be39ed471bf2b8cb6cace9f65fbc432c812))


### Bug Fixes

* Fix release-please config ([#32](https://github.com/cccteam/ccc/issues/32)) ([141cb33](https://github.com/cccteam/ccc/commit/141cb33d307e4190063ffe99ead84bdd0ca0298f))

## [0.2.5](https://github.com/cccteam/ccc/compare/v0.2.4...v0.2.5) (2024-09-24)


### Bug Fixes

* Fix package tag seperator ([#27](https://github.com/cccteam/ccc/issues/27)) ([bc24411](https://github.com/cccteam/ccc/commit/bc24411a37cbe90788ed7eb9688d9ff6132e0370))

## [0.2.4](https://github.com/cccteam/ccc/compare/v0.2.3...v0.2.4) (2024-09-24)


### Features

* Distribute packages versioned separately ([#24](https://github.com/cccteam/ccc/issues/24)) ([aae6b4f](https://github.com/cccteam/ccc/commit/aae6b4f646d7b0b8f4926180f5c90099def694ea))


### Bug Fixes

* Fix bug that prevented mashaling the zero value for ccc.UUID ([#22](https://github.com/cccteam/ccc/issues/22)) ([998a360](https://github.com/cccteam/ccc/commit/998a360131bed098858da1f99e1c76ba64fae022))

## [0.2.3](https://github.com/cccteam/ccc/compare/v0.2.2...v0.2.3) (2024-09-23)


### Features

* Add support for JSON Marchalling ([#20](https://github.com/cccteam/ccc/issues/20)) ([c9eb623](https://github.com/cccteam/ccc/commit/c9eb623ee504536e57bdcab2eea23ab6dd9f19dc))

## [0.2.2](https://github.com/cccteam/ccc/compare/v0.2.1...v0.2.2) (2024-09-17)


### Features

* Initial accesstypes package implementation ([#18](https://github.com/cccteam/ccc/issues/18)) ([791a724](https://github.com/cccteam/ccc/commit/791a7246b73492cbf8fb98c8be97be1153d25ea5))

## [0.2.1](https://github.com/cccteam/ccc/compare/v0.2.0...v0.2.1) (2024-09-06)


### Features

* Add an sns package ([#14](https://github.com/cccteam/ccc/issues/14)) ([52d7864](https://github.com/cccteam/ccc/commit/52d7864df014d23200f7262cbbd7b59be4b567a9))


### Bug Fixes

* Move Must() out of test file so it can be used external to package ([#15](https://github.com/cccteam/ccc/issues/15)) ([7e5f735](https://github.com/cccteam/ccc/commit/7e5f7356e35723da813654dc626516a6003f0c18))

## [0.2.0](https://github.com/cccteam/ccc/compare/v0.1.0...v0.2.0) (2024-08-16)


### ⚠ BREAKING CHANGES

* Removed function `UUIDMustParse()` ([#12](https://github.com/cccteam/ccc/issues/12))

### Features

* Add generic implementation of Must() ([#12](https://github.com/cccteam/ccc/issues/12)) ([29510d5](https://github.com/cccteam/ccc/commit/29510d5740d6dcce32ab39222beb0ed31db805f8))
* Add security scanner and License ([#11](https://github.com/cccteam/ccc/issues/11)) ([960e8f7](https://github.com/cccteam/ccc/commit/960e8f71f1ed31d0f3105d075ef8ba0fd20a01b8))
* Add unit tests ([#9](https://github.com/cccteam/ccc/issues/9)) ([fe68c52](https://github.com/cccteam/ccc/commit/fe68c52af4c1c23d25262a640f67e5c165c3c37e))
* Removed function `UUIDMustParse()` ([#12](https://github.com/cccteam/ccc/issues/12)) ([29510d5](https://github.com/cccteam/ccc/commit/29510d5740d6dcce32ab39222beb0ed31db805f8))

## 0.1.0 (2024-07-25)


### Features

* Add the JSONMap type ([#2](https://github.com/cccteam/ccc/issues/2)) ([75de4c5](https://github.com/cccteam/ccc/commit/75de4c548c033bb3532a32296247b2a9990a5f97))
* Establish baseline repository ([#1](https://github.com/cccteam/ccc/issues/1)) ([83c512e](https://github.com/cccteam/ccc/commit/83c512e6d44836ec805990f99836a31bc087d81c))
* Rename package to ccc ([#5](https://github.com/cccteam/ccc/issues/5)) ([ef027ff](https://github.com/cccteam/ccc/commit/ef027ff01b380815db09d2a7faa53d5a7383a67c))
