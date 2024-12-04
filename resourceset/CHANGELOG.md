# Changelog

## [0.4.2](https://github.com/cccteam/ccc/compare/resourceset/v0.4.1...resourceset/v0.4.2) (2024-12-04)


### Features

* add immutable permission ([#149](https://github.com/cccteam/ccc/issues/149)) ([560b53f](https://github.com/cccteam/ccc/commit/560b53f4aa0a06b6400e779cd944000550edbdf1))

## [0.4.1](https://github.com/cccteam/ccc/compare/resourceset/v0.4.0...resourceset/v0.4.1) (2024-11-16)


### Features

* Move base resouce permission checking into columnset ([#132](https://github.com/cccteam/ccc/issues/132)) ([f76879d](https://github.com/cccteam/ccc/commit/f76879d09ff489b64e5290f9d55b278cc01d7b5c))

## [0.4.0](https://github.com/cccteam/ccc/compare/resourceset/v0.3.3...resourceset/v0.4.0) (2024-11-09)


### ⚠ BREAKING CHANGES

* Support atomic operations across create update delete ([#120](https://github.com/cccteam/ccc/issues/120))

### Features

* Support atomic operations across create update delete ([#120](https://github.com/cccteam/ccc/issues/120)) ([9f15fce](https://github.com/cccteam/ccc/commit/9f15fce5c8022ca5c25b86dee12be0326212cc75))


### Bug Fixes

* Fix import for unit tests ([#115](https://github.com/cccteam/ccc/issues/115)) ([4f0da34](https://github.com/cccteam/ccc/commit/4f0da34c25bc2346e94c54d5ddbfe74ac068be01))


### Code Upgrade

* Upgrade go dependencies ([#126](https://github.com/cccteam/ccc/issues/126)) ([64192ed](https://github.com/cccteam/ccc/commit/64192ed95dace976dbb9088b167144455047c078))

## [0.3.3](https://github.com/cccteam/ccc/compare/resourceset/v0.3.2...resourceset/v0.3.3) (2024-10-23)


### Features

* New BaseResource() method ([#111](https://github.com/cccteam/ccc/issues/111)) ([694ef45](https://github.com/cccteam/ccc/commit/694ef454390be2cbb8223a53f7fccd8eeb7904ff))

## [0.3.2](https://github.com/cccteam/ccc/compare/resourceset/v0.3.1...resourceset/v0.3.2) (2024-10-21)


### Code Upgrade

* Upgrade go dependencies ([#103](https://github.com/cccteam/ccc/issues/103)) ([b728acd](https://github.com/cccteam/ccc/commit/b728acd493365623066089277dcf2de1c9da64c2))

## [0.3.1](https://github.com/cccteam/ccc/compare/resourceset/v0.3.0...resourceset/v0.3.1) (2024-10-11)


### Bug Fixes

* modify go build tags ([#91](https://github.com/cccteam/ccc/issues/91)) ([ef42102](https://github.com/cccteam/ccc/commit/ef42102c8b6c8e4a00b4fba6baf8699f130996ca))

## [0.3.0](https://github.com/cccteam/ccc/compare/resourceset/v0.2.0...resourceset/v0.3.0) (2024-10-07)


### ⚠ BREAKING CHANGES

* Upgrade to address breaking changes in accesstypes ([#82](https://github.com/cccteam/ccc/issues/82))

### Bug Fixes

* Upgrade to address breaking changes in accesstypes ([#82](https://github.com/cccteam/ccc/issues/82)) ([900acb7](https://github.com/cccteam/ccc/commit/900acb7298ae2507bcbfa57b58ba2597a41549fe))

## [0.2.0](https://github.com/cccteam/ccc/compare/resourceset/v0.1.2...resourceset/v0.2.0) (2024-10-04)


### ⚠ BREAKING CHANGES

* Changed FieldPermissions() method to TagPermissions() ([#73](https://github.com/cccteam/ccc/issues/73))

### Code Refactoring

* Changed FieldPermissions() method to TagPermissions() ([#73](https://github.com/cccteam/ccc/issues/73)) ([b99c6cf](https://github.com/cccteam/ccc/commit/b99c6cfca0fef3661cc00f6f79a7ebcb8d8458b7))

## [0.1.2](https://github.com/cccteam/ccc/compare/resourceset/v0.1.1...resourceset/v0.1.2) (2024-10-04)


### Features

* Switch to tag based resource field naming ([#66](https://github.com/cccteam/ccc/issues/66)) ([a5ddcb2](https://github.com/cccteam/ccc/commit/a5ddcb2527806e25caf06cc37698825c883dd136))

## [0.1.1](https://github.com/cccteam/ccc/compare/resourceset/v0.1.0...resourceset/v0.1.1) (2024-10-01)


### Bug Fixes

* Update go dependencies ([#50](https://github.com/cccteam/ccc/issues/50)) ([b031a0f](https://github.com/cccteam/ccc/commit/b031a0f22b6e8f2f16ca9e34d68169c4d6b64b56))

## [0.1.0](https://github.com/cccteam/ccc/compare/resourceset/v0.0.2...resourceset/v0.1.0) (2024-10-01)


### ⚠ BREAKING CHANGES

* Change ResourceSet.Contains() to ResourceSet.PermissionRequired()
* Change ResourceSet.Fields() to ResourceSet.FieldPermissions()

### Code Refactoring

* Change ResourceSet.Contains() to ResourceSet.PermissionRequired() ([7412641](https://github.com/cccteam/ccc/commit/74126411074a647d2176ccc1ab1f516991946b3d))
* Change ResourceSet.Fields() to ResourceSet.FieldPermissions() ([7412641](https://github.com/cccteam/ccc/commit/74126411074a647d2176ccc1ab1f516991946b3d))
* Refactor to use new types from accesstypes package ([7412641](https://github.com/cccteam/ccc/commit/74126411074a647d2176ccc1ab1f516991946b3d))

## [0.0.2](https://github.com/cccteam/ccc/compare/resourceset-v0.0.1...resourceset/v0.0.2) (2024-09-25)


### Features

* Move package to a new location with independent versioning ([#41](https://github.com/cccteam/ccc/issues/41)) ([0f0e563](https://github.com/cccteam/ccc/commit/0f0e5637c1e71efb95e4bc81ab8995ab44036fe7))
