# Changelog

## [0.0.12](https://github.com/cccteam/ccc/compare/resource/v0.0.11...resource/v0.0.12) (2025-02-11)


### Features

* generate enumerated resource fields in typescript metadata ([#186](https://github.com/cccteam/ccc/issues/186)) ([d8097d9](https://github.com/cccteam/ccc/commit/d8097d90a212a50bf17fafd065e2b6a188215742))
* primary key and ordinal position fields in typescript metadata ([#192](https://github.com/cccteam/ccc/issues/192)) ([f3dd430](https://github.com/cccteam/ccc/commit/f3dd430ad0cee7b18fc5660bd031344e165d2646))


### Bug Fixes

* Add options to resource.Operations() to better define path requirements ([#193](https://github.com/cccteam/ccc/issues/193)) ([f02b1bf](https://github.com/cccteam/ccc/commit/f02b1bf6eced75f7226af23285576d919df70667))
* Changed generated suffix to prefix for all generated files ([#187](https://github.com/cccteam/ccc/issues/187)) ([71f06d6](https://github.com/cccteam/ccc/commit/71f06d6b90b98122dbf6bee2098db9837d012184))
* Fix bugs around querying PrimaryKeys ([#191](https://github.com/cccteam/ccc/issues/191)) ([74f203a](https://github.com/cccteam/ccc/commit/74f203ad957d8fa7f85846696a194198890bd7c8))

## [0.0.11](https://github.com/cccteam/ccc/compare/resource/v0.0.10...resource/v0.0.11) (2025-02-05)


### Features

* Added index tags to generated handlers ([#185](https://github.com/cccteam/ccc/issues/185)) ([a00f0d8](https://github.com/cccteam/ccc/commit/a00f0d8474a5ce257ee071d3c541fe055389014b))
* Added Link type for virtual resources ([#174](https://github.com/cccteam/ccc/issues/174)) ([90a3043](https://github.com/cccteam/ccc/commit/90a3043894686c0257092508121c8bfa16c185c8))


### Code Refactoring

* Parse resource file & AST one time ([#177](https://github.com/cccteam/ccc/issues/177)) ([cfec4cb](https://github.com/cccteam/ccc/commit/cfec4cb7d13fdda7c6de9ad98f6f608d3dc744a1))


### Code Upgrade

* ccc and sub repos GO version to `1.23.6` and all dependencies except CCC authored packages ([#178](https://github.com/cccteam/ccc/issues/178)) ([117a49d](https://github.com/cccteam/ccc/commit/117a49d3740b461d1b295047cdeaf85b4cacb53f))

## [0.0.10](https://github.com/cccteam/ccc/compare/resource/v0.0.9...resource/v0.0.10) (2025-02-04)


### Features

* typescript generation augmented ([#175](https://github.com/cccteam/ccc/issues/175)) ([d2f1ccd](https://github.com/cccteam/ccc/commit/d2f1ccd27a92d8f3e503b27c8dbb3179dbcbfb7d))
* Virtual resource generation and resource interface formatting ([#172](https://github.com/cccteam/ccc/issues/172)) ([a3e6747](https://github.com/cccteam/ccc/commit/a3e6747886a67f5bafe2f4540fc67a860bb50f1b))

## [0.0.9](https://github.com/cccteam/ccc/compare/resource/v0.0.8...resource/v0.0.9) (2025-01-29)


### Bug Fixes

* Fixed compound table and pluralization issues ([#168](https://github.com/cccteam/ccc/issues/168)) ([7091a1c](https://github.com/cccteam/ccc/commit/7091a1c97aab69238776d04677b846e2e0ebf670))

## [0.0.8](https://github.com/cccteam/ccc/compare/resource/v0.0.7...resource/v0.0.8) (2025-01-15)


### Bug Fixes

* Remove Key from the Set method name. ([#166](https://github.com/cccteam/ccc/issues/166)) ([af8ce9e](https://github.com/cccteam/ccc/commit/af8ce9e15b825136cbb19ad9efdac835902256df))

## [0.0.7](https://github.com/cccteam/ccc/compare/resource/v0.0.6...resource/v0.0.7) (2025-01-15)


### Bug Fixes

* Use NullJSON type for ChangeSet [#164](https://github.com/cccteam/ccc/issues/164) ([8f1da9b](https://github.com/cccteam/ccc/commit/8f1da9ba0be87ecb535d76f5b68453344a8250be))

## [0.0.6](https://github.com/cccteam/ccc/compare/resource/v0.0.5...resource/v0.0.6) (2025-01-15)


### Features

* Added resource generation code to new generation package ([#161](https://github.com/cccteam/ccc/issues/161)) ([2505d96](https://github.com/cccteam/ccc/commit/2505d96dfe43157574a5055d3f609c6aa9586b72))

## [0.0.5](https://github.com/cccteam/ccc/compare/resource/v0.0.4...resource/v0.0.5) (2025-01-02)


### Bug Fixes

* Allow duplicate registration of permission and resource ([#158](https://github.com/cccteam/ccc/issues/158)) ([04fad82](https://github.com/cccteam/ccc/commit/04fad825c160b10d5e8de1789d168f12faec4c72))

## [0.0.4](https://github.com/cccteam/ccc/compare/resource/v0.0.3...resource/v0.0.4) (2024-12-18)


### Bug Fixes

* Fix use before initialization bug ([#156](https://github.com/cccteam/ccc/issues/156)) ([e062401](https://github.com/cccteam/ccc/commit/e062401abef7eccd728c82f8f094caf4b35046db))

## [0.0.3](https://github.com/cccteam/ccc/compare/resource/v0.0.2...resource/v0.0.3) (2024-12-18)


### Code Refactoring

* QuerySet and PatchSet ([#154](https://github.com/cccteam/ccc/issues/154)) ([7a30fb8](https://github.com/cccteam/ccc/commit/7a30fb88e79480eac38ef7761187a2b2c9218327))

## [0.0.2](https://github.com/cccteam/ccc/compare/resource/v0.0.1...resource/v0.0.2) (2024-12-05)


### Features

* Implement QuerySet ([#146](https://github.com/cccteam/ccc/issues/146)) ([8e71fe8](https://github.com/cccteam/ccc/commit/8e71fe80d044b2c16089b0e40ddf63734aa2f027))
* Merge queryset, resourceset, patchset, resourcestore into a single resource package ([#146](https://github.com/cccteam/ccc/issues/146)) ([8e71fe8](https://github.com/cccteam/ccc/commit/8e71fe80d044b2c16089b0e40ddf63734aa2f027))

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
