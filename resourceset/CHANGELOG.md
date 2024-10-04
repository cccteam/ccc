# Changelog

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
