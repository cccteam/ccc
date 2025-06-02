# Changelog

## [0.3.0](https://github.com/cccteam/ccc/compare/v0.2.9...v0.3.0) (2025-06-02)


### ⚠ BREAKING CHANGES

* Support atomic operations across create update delete ([#120](https://github.com/cccteam/ccc/issues/120))

### Features

* add immutable permission ([#149](https://github.com/cccteam/ccc/issues/149)) ([560b53f](https://github.com/cccteam/ccc/commit/560b53f4aa0a06b6400e779cd944000550edbdf1))
* add lint package ([#200](https://github.com/cccteam/ccc/issues/200)) ([8250164](https://github.com/cccteam/ccc/commit/82501647152168866470b0d7617b4092d9043e2e))
* Add support for keys ([#109](https://github.com/cccteam/ccc/issues/109)) ([8f23951](https://github.com/cccteam/ccc/commit/8f239515236c088f3e848a8db6e061fd7fe49eff))
* Add support to fetch a value from the patchset ([#109](https://github.com/cccteam/ccc/issues/109)) ([8f23951](https://github.com/cccteam/ccc/commit/8f239515236c088f3e848a8db6e061fd7fe49eff))
* Implement QuerySet ([#146](https://github.com/cccteam/ccc/issues/146)) ([8e71fe8](https://github.com/cccteam/ccc/commit/8e71fe80d044b2c16089b0e40ddf63734aa2f027))
* Merge queryset, resourceset, patchset, resourcestore into a single resource package ([#146](https://github.com/cccteam/ccc/issues/146)) ([8e71fe8](https://github.com/cccteam/ccc/commit/8e71fe80d044b2c16089b0e40ddf63734aa2f027))
* Move base resouce permission checking into columnset ([#132](https://github.com/cccteam/ccc/issues/132)) ([f76879d](https://github.com/cccteam/ccc/commit/f76879d09ff489b64e5290f9d55b278cc01d7b5c))
* New BaseResource() method ([#111](https://github.com/cccteam/ccc/issues/111)) ([694ef45](https://github.com/cccteam/ccc/commit/694ef454390be2cbb8223a53f7fccd8eeb7904ff))
* Package Information ([#202](https://github.com/cccteam/ccc/issues/202)) ([fcb19aa](https://github.com/cccteam/ccc/commit/fcb19aa1b96230899a231e256bdf3472f9886a32))
* Support atomic operations across create update delete ([#120](https://github.com/cccteam/ccc/issues/120)) ([9f15fce](https://github.com/cccteam/ccc/commit/9f15fce5c8022ca5c25b86dee12be0326212cc75))
* Support for iter.Seq2 ([#236](https://github.com/cccteam/ccc/issues/236)) ([25c8d90](https://github.com/cccteam/ccc/commit/25c8d9051fca233c6d92733edc886316b4effdfe))
* text search support ([#169](https://github.com/cccteam/ccc/issues/169)) ([5c2ab80](https://github.com/cccteam/ccc/commit/5c2ab8037ba978169f5db0439d74a859d441670c))


### Bug Fixes

* Fix import for unit tests ([#115](https://github.com/cccteam/ccc/issues/115)) ([4f0da34](https://github.com/cccteam/ccc/commit/4f0da34c25bc2346e94c54d5ddbfe74ac068be01))
* Fix unstable ordering of composite primary keys ([#136](https://github.com/cccteam/ccc/issues/136)) ([8a37c94](https://github.com/cccteam/ccc/commit/8a37c9408d76dbe571474e6b51874a2c5ac78933))
* Implement stable ordering of data fields ([#141](https://github.com/cccteam/ccc/issues/141)) ([128edea](https://github.com/cccteam/ccc/commit/128edeae4608f82b3e6765b7c79fb9de741d489a))
* Return error for condition where user does not have permission for any column ([#113](https://github.com/cccteam/ccc/issues/113)) ([c501924](https://github.com/cccteam/ccc/commit/c5019244871bb407d755d4eab3634258260610a1))


### Code Refactoring

* change format of generated typescript from resource store ([#119](https://github.com/cccteam/ccc/issues/119)) ([bd90eaa](https://github.com/cccteam/ccc/commit/bd90eaa76014a92679ac1c87aa9c614346563800))
* replace ccc-types import with ccc-lib ([#147](https://github.com/cccteam/ccc/issues/147)) ([7e5c631](https://github.com/cccteam/ccc/commit/7e5c631f18ebfb1d08ed9c996d29a65051ac9a37))
* Typescript generation whitespace fix ([#142](https://github.com/cccteam/ccc/issues/142)) ([76031de](https://github.com/cccteam/ccc/commit/76031de18e64fb5606c6e441bcd627b7dcc5c39f))


### Code Upgrade

* ccc and sub repos GO version to `1.23.6` and all dependencies except CCC authored packages ([#178](https://github.com/cccteam/ccc/issues/178)) ([117a49d](https://github.com/cccteam/ccc/commit/117a49d3740b461d1b295047cdeaf85b4cacb53f))
* **deps:** Bump github.com/google/go-cmp in the go-dependencies group ([#221](https://github.com/cccteam/ccc/issues/221)) ([2dbbff6](https://github.com/cccteam/ccc/commit/2dbbff605ff8575402bf5992e128edb7f597a17e))
* Upgrade dependencies ([#135](https://github.com/cccteam/ccc/issues/135)) ([7901e64](https://github.com/cccteam/ccc/commit/7901e64376e6f8437af357ed9606429b7187ae95))
* Upgrade go dependencies ([#125](https://github.com/cccteam/ccc/issues/125)) ([bc379ee](https://github.com/cccteam/ccc/commit/bc379eefa9ec295092ff2ae15fc5bd7729d0084c))
* Upgrade go dependencies ([#126](https://github.com/cccteam/ccc/issues/126)) ([64192ed](https://github.com/cccteam/ccc/commit/64192ed95dace976dbb9088b167144455047c078))
* Upgrade go dependencies ([#127](https://github.com/cccteam/ccc/issues/127)) ([9fae5f2](https://github.com/cccteam/ccc/commit/9fae5f2a049a8b4a6f73bb55b171c9ef8578af08))
* Upgrade go dependencies ([#128](https://github.com/cccteam/ccc/issues/128)) ([045f94a](https://github.com/cccteam/ccc/commit/045f94a28f9dae9c2157fbbacfec73a904903d75))

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
