# Changelog

## [0.1.0](https://github.com/cccteam/ccc/compare/cache/v0.0.3...cache/v0.1.0) (2025-09-06)


### ⚠ BREAKING CHANGES

* Switch to cbor will require you to delete the existing cache because cbor can not read gob encoding ([#447](https://github.com/cccteam/ccc/issues/447))

### Features

* Setup code workspace file to handle linting mono-repo ([#441](https://github.com/cccteam/ccc/issues/441)) ([13d81e6](https://github.com/cccteam/ccc/commit/13d81e6ce7dedf538c8e2dff5cbf030d1ef626d1))
* Switch to cbor to allow larger cache sizes ([#447](https://github.com/cccteam/ccc/issues/447)) ([6f91065](https://github.com/cccteam/ccc/commit/6f910659ecbbf221832aa72df3c08beb94b022ba))
* Switch to cbor will require you to delete the existing cache because cbor can not read gob encoding ([#447](https://github.com/cccteam/ccc/issues/447)) ([6f91065](https://github.com/cccteam/ccc/commit/6f910659ecbbf221832aa72df3c08beb94b022ba))


### Bug Fixes

* Remove cache object instead of overwriting to avoid permission issues ([#447](https://github.com/cccteam/ccc/issues/447)) ([6f91065](https://github.com/cccteam/ccc/commit/6f910659ecbbf221832aa72df3c08beb94b022ba))

## [0.0.3](https://github.com/cccteam/ccc/compare/cache/v0.0.2...cache/v0.0.3) (2025-08-23)


### Features

* Add options for managing permissions on cache objects ([#428](https://github.com/cccteam/ccc/issues/428)) ([170c4f7](https://github.com/cccteam/ccc/commit/170c4f7759e4583f31ce7b27a8613dabf3908227))

## [0.0.2](https://github.com/cccteam/ccc/compare/cache/v0.0.1...cache/v0.0.2) (2025-08-18)


### Features

* cache package ([#416](https://github.com/cccteam/ccc/issues/416)) ([c14c820](https://github.com/cccteam/ccc/commit/c14c820cda66e029f60b0dbc875538c4cbc45188))
