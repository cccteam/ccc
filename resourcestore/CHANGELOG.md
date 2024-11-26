# Changelog

## [0.5.3](https://github.com/cccteam/ccc/compare/resourcestore/v0.5.2...resourcestore/v0.5.3) (2024-11-26)


### Code Refactoring

* replace ccc-types import with ccc-lib ([#147](https://github.com/cccteam/ccc/issues/147)) ([7e5c631](https://github.com/cccteam/ccc/commit/7e5c631f18ebfb1d08ed9c996d29a65051ac9a37))

## [0.5.2](https://github.com/cccteam/ccc/compare/resourcestore/v0.5.1...resourcestore/v0.5.2) (2024-11-23)


### Code Refactoring

* Typescript generation whitespace fix ([#142](https://github.com/cccteam/ccc/issues/142)) ([76031de](https://github.com/cccteam/ccc/commit/76031de18e64fb5606c6e441bcd627b7dcc5c39f))

## [0.5.1](https://github.com/cccteam/ccc/compare/resourcestore/v0.5.0...resourcestore/v0.5.1) (2024-11-16)


### Code Refactoring

* change format of generated typescript from resource store ([#119](https://github.com/cccteam/ccc/issues/119)) ([bd90eaa](https://github.com/cccteam/ccc/commit/bd90eaa76014a92679ac1c87aa9c614346563800))

## [0.5.0](https://github.com/cccteam/ccc/compare/resourcestore/v0.4.2...resourcestore/v0.5.0) (2024-11-09)


### ⚠ BREAKING CHANGES

* Support atomic operations across create update delete ([#120](https://github.com/cccteam/ccc/issues/120))

### Features

* Support atomic operations across create update delete ([#120](https://github.com/cccteam/ccc/issues/120)) ([9f15fce](https://github.com/cccteam/ccc/commit/9f15fce5c8022ca5c25b86dee12be0326212cc75))


### Code Upgrade

* Upgrade go dependencies ([#127](https://github.com/cccteam/ccc/issues/127)) ([9fae5f2](https://github.com/cccteam/ccc/commit/9fae5f2a049a8b4a6f73bb55b171c9ef8578af08))

## [0.4.2](https://github.com/cccteam/ccc/compare/resourcestore/v0.4.1...resourcestore/v0.4.2) (2024-10-21)


### Code Upgrade

* Upgrade go dependencies ([#101](https://github.com/cccteam/ccc/issues/101)) ([18e36a0](https://github.com/cccteam/ccc/commit/18e36a0e5ee749a0d5daaf91466bb02e899da2aa))

## [0.4.1](https://github.com/cccteam/ccc/compare/resourcestore/v0.4.0...resourcestore/v0.4.1) (2024-10-11)


### Bug Fixes

* modify go build tags ([#91](https://github.com/cccteam/ccc/issues/91)) ([ef42102](https://github.com/cccteam/ccc/commit/ef42102c8b6c8e4a00b4fba6baf8699f130996ca))

## [0.4.0](https://github.com/cccteam/ccc/compare/resourcestore/v0.3.1...resourcestore/v0.4.0) (2024-10-11)


### ⚠ BREAKING CHANGES

* Removed method `GenerateTypeScriptEnums()` ([#89](https://github.com/cccteam/ccc/issues/89))

### Features

* Add method `GenerateTypeScript()` to generate typescript code with resource to permission mapping ([#89](https://github.com/cccteam/ccc/issues/89)) ([ccef2a2](https://github.com/cccteam/ccc/commit/ccef2a2d970298a85525a6709d8e49a018c4a5bd))


### Code Refactoring

* Removed method `GenerateTypeScriptEnums()` ([#89](https://github.com/cccteam/ccc/issues/89)) ([ccef2a2](https://github.com/cccteam/ccc/commit/ccef2a2d970298a85525a6709d8e49a018c4a5bd))

## [0.3.1](https://github.com/cccteam/ccc/compare/resourcestore/v0.3.0...resourcestore/v0.3.1) (2024-10-07)


### Features

* Stablize sort order of generated enums ([#83](https://github.com/cccteam/ccc/issues/83)) ([7629738](https://github.com/cccteam/ccc/commit/7629738a4d118059390e0206a5b1f9ae674ac516))


### Bug Fixes

* Fix import version of resoucetypes ([#83](https://github.com/cccteam/ccc/issues/83)) ([7629738](https://github.com/cccteam/ccc/commit/7629738a4d118059390e0206a5b1f9ae674ac516))

## [0.3.0](https://github.com/cccteam/ccc/compare/resourcestore/v0.2.1...resourcestore/v0.3.0) (2024-10-04)


### ⚠ BREAKING CHANGES

* Change AddResourceFields() method to AddResourceTags() ([#75](https://github.com/cccteam/ccc/issues/75))

### Code Refactoring

* Change AddResourceFields() method to AddResourceTags() ([#75](https://github.com/cccteam/ccc/issues/75)) ([cb8ee7a](https://github.com/cccteam/ccc/commit/cb8ee7a7824d942fea27320abe8933cd29182134))

## [0.2.1](https://github.com/cccteam/ccc/compare/resourcestore/v0.2.0...resourcestore/v0.2.1) (2024-10-04)


### Features

* Added `Store.List()` and `Store.Scope()` methods ([#65](https://github.com/cccteam/ccc/issues/65)) ([ddd9b6c](https://github.com/cccteam/ccc/commit/ddd9b6c578b8527ff32fc477219b50d0b89501c5))

## [0.2.0](https://github.com/cccteam/ccc/compare/resourcestore/v0.1.1...resourcestore/v0.2.0) (2024-10-02)


### ⚠ BREAKING CHANGES

* Rename TypeScriptPermissions to GenerateTypeScriptEnums ([#61](https://github.com/cccteam/ccc/issues/61))

### Code Refactoring

* Rename TypeScriptPermissions to GenerateTypeScriptEnums ([#61](https://github.com/cccteam/ccc/issues/61)) ([d889459](https://github.com/cccteam/ccc/commit/d889459ff64b6a517573f2a24da4ca1328e0a204))

## [0.1.1](https://github.com/cccteam/ccc/compare/resourcestore/v0.1.0...resourcestore/v0.1.1) (2024-10-02)


### Features

* Added functionality to generate enums for typescript ([#59](https://github.com/cccteam/ccc/issues/59)) ([60029f5](https://github.com/cccteam/ccc/commit/60029f5b46671516a41ee0491f10c711650de7c2))

## [0.1.0](https://github.com/cccteam/ccc/compare/resourcestore-v0.0.1...resourcestore/v0.1.0) (2024-10-01)


### ⚠ BREAKING CHANGES

* Implement permission resolution and resource registration. ([#56](https://github.com/cccteam/ccc/issues/56))

### Features

* Implement permission resolution and resource registration. ([#56](https://github.com/cccteam/ccc/issues/56)) ([0e9003d](https://github.com/cccteam/ccc/commit/0e9003d620b4e0e9a456ba76f9a82fa4cd247d0d))
* Move package to a new location with independent versioning ([#41](https://github.com/cccteam/ccc/issues/41)) ([0f0e563](https://github.com/cccteam/ccc/commit/0f0e5637c1e71efb95e4bc81ab8995ab44036fe7))
