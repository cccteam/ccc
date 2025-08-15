# Changelog

## [0.2.14](https://github.com/cccteam/ccc/compare/resource/v0.2.13...resource/v0.2.14) (2025-08-15)


### Bug Fixes

* Add missing default clause in the Consolidated Patch Handler ([#410](https://github.com/cccteam/ccc/issues/410)) ([be3979c](https://github.com/cccteam/ccc/commit/be3979cd374b7aa60ba77cf3eecd2acb89549775))

## [0.2.13](https://github.com/cccteam/ccc/compare/resource/v0.2.12...resource/v0.2.13) (2025-08-13)


### Features

* Add generation cache ([#407](https://github.com/cccteam/ccc/issues/407)) ([1be7c18](https://github.com/cccteam/ccc/commit/1be7c18691b18f3bb8c08fc76259360806797600))

## [0.2.12](https://github.com/cccteam/ccc/compare/resource/v0.2.11...resource/v0.2.12) (2025-08-08)


### Features

* Rpc type generation [#403](https://github.com/cccteam/ccc/issues/403) ([70424fb](https://github.com/cccteam/ccc/commit/70424fb984d918368a253a618339b863fca04417))

## [0.2.11](https://github.com/cccteam/ccc/compare/resource/v0.2.10...resource/v0.2.11) (2025-08-07)


### Bug Fixes

* Fix bug in template for Patch handlers with compound primary keys ([#401](https://github.com/cccteam/ccc/issues/401)) ([fe4ae06](https://github.com/cccteam/ccc/commit/fe4ae06bcdcc42b82c871a100560565766f9dfd9))

## [0.2.10](https://github.com/cccteam/ccc/compare/resource/v0.2.9...resource/v0.2.10) (2025-08-06)


### Bug Fixes

* Fix resourceMeta() function to satisfy typescript ([#399](https://github.com/cccteam/ccc/issues/399)) ([73103e5](https://github.com/cccteam/ccc/commit/73103e55485e989f8d3af8bffb3176c7c73185cc))

## [0.2.9](https://github.com/cccteam/ccc/compare/resource/v0.2.8...resource/v0.2.9) (2025-08-06)


### Features

* rpc method typescript metadata ([#396](https://github.com/cccteam/ccc/issues/396)) ([e6a7aa5](https://github.com/cccteam/ccc/commit/e6a7aa586a1da509fb40cf372a0faaea173a17dc))


### Bug Fixes

* Revert breaking changes to the public interface (397) ([c45b0be](https://github.com/cccteam/ccc/commit/c45b0be637cf0e00a8ca44f87f4ad4ece11b753c))

## [0.2.8](https://github.com/cccteam/ccc/compare/resource/v0.2.7...resource/v0.2.8) (2025-08-05)


### Bug Fixes

* Fix bug causing unstable KEY_ORDINAL_POSITION ([#394](https://github.com/cccteam/ccc/issues/394)) ([7eeecde](https://github.com/cccteam/ccc/commit/7eeecdeb8ac21a2575463aa85cbeea71869d835c))

## [0.2.7](https://github.com/cccteam/ccc/compare/resource/v0.2.6...resource/v0.2.7) (2025-08-04)


### Features

* Add limit parameter to query decoder ([#389](https://github.com/cccteam/ccc/issues/389)) ([6b31061](https://github.com/cccteam/ccc/commit/6b31061981e812bf49415e8ed16e0b3155266c81))
* Add support for Sorting and Limiting Query Clauses ([#393](https://github.com/cccteam/ccc/issues/393)) ([56d9cfd](https://github.com/cccteam/ccc/commit/56d9cfd96933a8f0afdd0e1197a6fa78daa2d729))


### Code Upgrade

* **deps:** Bump github.com/cccteam/session ([#391](https://github.com/cccteam/ccc/issues/391)) ([aa84ed6](https://github.com/cccteam/ccc/commit/aa84ed6a6bca6eef0dd730f4e8d0aa66a5a6bc01))

## [0.2.6](https://github.com/cccteam/ccc/compare/resource/v0.2.5...resource/v0.2.6) (2025-07-31)


### Code Refactoring

* consolidated handlers generation ([#382](https://github.com/cccteam/ccc/issues/382)) ([70fb1a8](https://github.com/cccteam/ccc/commit/70fb1a846b5636a6bcb530b8413351e3324e6f11))

## [0.2.5](https://github.com/cccteam/ccc/compare/resource/v0.2.4...resource/v0.2.5) (2025-07-31)


### Bug Fixes

* Handle named types where underlying type is supported by spanner ([#385](https://github.com/cccteam/ccc/issues/385)) ([0dca4bd](https://github.com/cccteam/ccc/commit/0dca4bdf4c708e6e681038bdc9ee21f0f2b39188))

## [0.2.4](https://github.com/cccteam/ccc/compare/resource/v0.2.3...resource/v0.2.4) (2025-07-29)


### Bug Fixes

* Export RemoveGeneratedFiles() for external usage ([#383](https://github.com/cccteam/ccc/issues/383)) ([1cae856](https://github.com/cccteam/ccc/commit/1cae85636c3966744eb22cb4132f4c1225f6fb75))
* Fix identifier for mostly numeric descriptions ([#378](https://github.com/cccteam/ccc/issues/378)) ([05c5d73](https://github.com/cccteam/ccc/commit/05c5d737368bd3615ad23d7308bfcde5313641f0))
* Spelling ([#380](https://github.com/cccteam/ccc/issues/380)) ([2209b08](https://github.com/cccteam/ccc/commit/2209b08c3527e38983e29e3f90d2dcba6e50d828))

## [0.2.3](https://github.com/cccteam/ccc/compare/resource/v0.2.2...resource/v0.2.3) (2025-07-22)


### Features

* Add support for prefix matching when decoding operations ([#373](https://github.com/cccteam/ccc/issues/373)) ([9dddb3a](https://github.com/cccteam/ccc/commit/9dddb3a071639b20b3135e7360e4a2a265abab8b))
* enum generation ([#377](https://github.com/cccteam/ccc/issues/377)) ([6d00e59](https://github.com/cccteam/ccc/commit/6d00e59c20e9070f0cdcb033d7df35c75141e6ad))


### Bug Fixes

* Cleanup after Jules ([#369](https://github.com/cccteam/ccc/issues/369)) ([6fe0bac](https://github.com/cccteam/ccc/commit/6fe0bacdd07cd559a8365b5b9d63257c161540e2))
* Support `add` and `patch` operations that use Path parameters and have no Value parameters  ([#372](https://github.com/cccteam/ccc/issues/372)) ([bfb4b05](https://github.com/cccteam/ccc/commit/bfb4b053d9b113abf03b55c06fc69521b82d144e))

## [0.2.2](https://github.com/cccteam/ccc/compare/resource/v0.2.1...resource/v0.2.2) (2025-07-16)


### Bug Fixes

* use underlying type for null wrapper types in clause generation ([#364](https://github.com/cccteam/ccc/issues/364)) ([a98dec0](https://github.com/cccteam/ccc/commit/a98dec00c371b9314ca82a5028eb5def15e4bb20))


### Code Upgrade

* **deps:** Bump golang.org/x/tools ([#361](https://github.com/cccteam/ccc/issues/361)) ([a44935a](https://github.com/cccteam/ccc/commit/a44935ab72fa7dd06cafa75899e95021b6a6c7de))
* **deps:** Bump the go-dependencies group across 1 directory with 2 updates ([#368](https://github.com/cccteam/ccc/issues/368)) ([7333932](https://github.com/cccteam/ccc/commit/73339325d3897e6a3c5d115011f3b616a96cd0b5))

## [0.2.1](https://github.com/cccteam/ccc/compare/resource/v0.2.0...resource/v0.2.1) (2025-07-10)


### Bug Fixes

* Fix bug for Named types typescript type ([#360](https://github.com/cccteam/ccc/issues/360)) ([304bdd3](https://github.com/cccteam/ccc/commit/304bdd3bc8a447290773a40708aee4dd3c414c4e))
* Remove pointers from input fields for clause comparisons ([#358](https://github.com/cccteam/ccc/issues/358)) ([6e7887b](https://github.com/cccteam/ccc/commit/6e7887b83b06abf79c334933824176d27ac0ba07))

## [0.2.0](https://github.com/cccteam/ccc/compare/resource/v0.1.19...resource/v0.2.0) (2025-07-09)


### ⚠ BREAKING CHANGES

* Changed signature of `NewResourceGenerator()` (357)

### Features

* Changed signature of `NewResourceGenerator()` (357) ([c1c8013](https://github.com/cccteam/ccc/commit/c1c80132790d8301a321b7ebda24619e53889e6f))
* Take in local packages instead of hard coding (357) ([c1c8013](https://github.com/cccteam/ccc/commit/c1c80132790d8301a321b7ebda24619e53889e6f))


### Bug Fixes

* Return empty array instead of nil when no records found (357) ([c1c8013](https://github.com/cccteam/ccc/commit/c1c80132790d8301a321b7ebda24619e53889e6f))


### Code Upgrade

* **deps:** Bump github.com/go-chi/chi/v5 in /resource ([#351](https://github.com/cccteam/ccc/issues/351)) ([90c29d3](https://github.com/cccteam/ccc/commit/90c29d387967460f0a27e7a05e187dd663d1d07d))
* **deps:** Bump the go-dependencies group across 1 directory with 5 updates ([#356](https://github.com/cccteam/ccc/issues/356)) ([4f8e37f](https://github.com/cccteam/ccc/commit/4f8e37fd7ce6b5e9ce6d852abe56ac837f28d071))

## [0.1.19](https://github.com/cccteam/ccc/compare/resource/v0.1.18...resource/v0.1.19) (2025-07-02)


### Features

* Add support for tokenlist search indexes on virtual resources (views) ([#352](https://github.com/cccteam/ccc/issues/352)) ([713a839](https://github.com/cccteam/ccc/commit/713a839d4c20149719f202aae1a7368ae953ea82))

## [0.1.18](https://github.com/cccteam/ccc/compare/resource/v0.1.17...resource/v0.1.18) (2025-06-19)


### Features

* Implement DefaultString() values ([#347](https://github.com/cccteam/ccc/issues/347)) ([1478241](https://github.com/cccteam/ccc/commit/1478241b72ebff3b935a54e3c8d4b5d6dc0a0a0d))


### Code Refactoring

* Cleanup Option processing ([#345](https://github.com/cccteam/ccc/issues/345)) ([e985297](https://github.com/cccteam/ccc/commit/e9852974e5647bc37a29fb86b0f74378eb9fcb1b))

## [0.1.17](https://github.com/cccteam/ccc/compare/resource/v0.1.16...resource/v0.1.17) (2025-06-18)


### Features

* New Option to set Spanner Emulator Version ([#343](https://github.com/cccteam/ccc/issues/343)) ([6eae667](https://github.com/cccteam/ccc/commit/6eae66781f42e2f1b509ffa76d84f0e114b70ef0))

## [0.1.16](https://github.com/cccteam/ccc/compare/resource/v0.1.15...resource/v0.1.16) (2025-06-18)


### Features

* Add a default function for booleans ([#341](https://github.com/cccteam/ccc/issues/341)) ([72debe6](https://github.com/cccteam/ccc/commit/72debe6593af47b5c8d3d0a32cccc1717e47744f))

## [0.1.15](https://github.com/cccteam/ccc/compare/resource/v0.1.14...resource/v0.1.15) (2025-06-18)


### Features

* Implement allow_filter struct tag to support non-indexed columns in filters ([#338](https://github.com/cccteam/ccc/issues/338)) ([ef0f5c0](https://github.com/cccteam/ccc/commit/ef0f5c0cb13f3cdd053bfd12c63777bc29a6e950))

## [0.1.14](https://github.com/cccteam/ccc/compare/resource/v0.1.13...resource/v0.1.14) (2025-06-17)


### Features

* comment parsing ([#306](https://github.com/cccteam/ccc/issues/306)) ([59bc9ae](https://github.com/cccteam/ccc/commit/59bc9ae7350a41632f3ebb7fe18debc567dc83a7))

## [0.1.13](https://github.com/cccteam/ccc/compare/resource/v0.1.12...resource/v0.1.13) (2025-06-17)


### Features

* Add support for 'sort' query parameter ([#328](https://github.com/cccteam/ccc/issues/328)) ([d110d6f](https://github.com/cccteam/ccc/commit/d110d6f1dde3a1b28b5683d7fa44aac89a2ed1c2))
* Add tests ([#329](https://github.com/cccteam/ccc/issues/329)) ([cb6647f](https://github.com/cccteam/ccc/commit/cb6647f1c64fd6fa83dbebb905b0445a81ea1321))
* support query builder filtering on virtual resources ([#333](https://github.com/cccteam/ccc/issues/333)) ([578ade6](https://github.com/cccteam/ccc/commit/578ade6eae28117e4fb61437a8f42ec7c27121de))


### Bug Fixes

* Cleanup error messages to return client message ([#334](https://github.com/cccteam/ccc/issues/334)) ([3a32195](https://github.com/cccteam/ccc/commit/3a321958806a692549d75e936b6e27e91e8ea013))


### Code Refactoring

* Remove support for legacy query filters by column ([#326](https://github.com/cccteam/ccc/issues/326)) ([138014c](https://github.com/cccteam/ccc/commit/138014c8f8fa0f2ae1fa8989787715cb31d71e49))


### Code Upgrade

* **deps:** Bump the go-dependencies group ([#316](https://github.com/cccteam/ccc/issues/316)) ([8fba3e5](https://github.com/cccteam/ccc/commit/8fba3e5b019ecfe9617b2edc4cc42ce247d23ddc))

## [0.1.12](https://github.com/cccteam/ccc/compare/resource/v0.1.11...resource/v0.1.12) (2025-06-12)


### Features

* Added support for filtering on boolean columns ([#307](https://github.com/cccteam/ccc/issues/307)) ([873baeb](https://github.com/cccteam/ccc/commit/873baeb85fb8e14d175e0f960214a3929cb5eb05))
* Cleanup walk interface to return params ([#319](https://github.com/cccteam/ccc/issues/319)) ([458154f](https://github.com/cccteam/ccc/commit/458154f02d1ab0cc05b723981e24ef33acce6b2f))
* Implement filter query param ([#322](https://github.com/cccteam/ccc/issues/322)) ([222f389](https://github.com/cccteam/ccc/commit/222f389e03be3f2779539b30fbfbf8d4fe619686))
* Support NULL checks ([#308](https://github.com/cccteam/ccc/issues/308)) ([3ef7b17](https://github.com/cccteam/ccc/commit/3ef7b175a7909192c11f9a1148f56efbfddc4b71))


### Bug Fixes

* Enclose index filter fragments in parentheses for multiple terms ([#315](https://github.com/cccteam/ccc/issues/315)) ([afb9ea8](https://github.com/cccteam/ccc/commit/afb9ea8fd62cf64b86ee31a257b2ac76bf960a9f))
* Fix formatting bug with parens for IN/NOT IN clauses ([#320](https://github.com/cccteam/ccc/issues/320)) ([aa2fe0f](https://github.com/cccteam/ccc/commit/aa2fe0f83a554cd0e6e58e8acaea7b8c29add0b7))
* Fix PostgresStmt() method to return resolvedWhereClause like the Spanner variant ([#324](https://github.com/cccteam/ccc/issues/324)) ([5f2664e](https://github.com/cccteam/ccc/commit/5f2664e104e525f840105c1c4b32ed1534222160))
* Return correct typescript data type for uuid and date arrays ([#321](https://github.com/cccteam/ccc/issues/321)) ([438463a](https://github.com/cccteam/ccc/commit/438463a52e3d003adf08e57641f094df383575dd))


### Code Refactoring

* Remove duplicate SQL Generator code ([#323](https://github.com/cccteam/ccc/issues/323)) ([cdfa5e4](https://github.com/cccteam/ccc/commit/cdfa5e426dc7d36c47437f61bac06c55a8b8b98e))


### Code Cleanup

* Cleanup the where clause generation ([#299](https://github.com/cccteam/ccc/issues/299)) ([cb805d4](https://github.com/cccteam/ccc/commit/cb805d4faef6dacd62ffa90ac92faba0a2bda10a))

## [0.1.11](https://github.com/cccteam/ccc/compare/resource/v0.1.10...resource/v0.1.11) (2025-05-14)


### Bug Fixes

* Resolve parameters from where clause in error messages ([#300](https://github.com/cccteam/ccc/issues/300)) ([713e041](https://github.com/cccteam/ccc/commit/713e0412311958ae9d79f30eacabd1b35da5bae7))
* Use unqualified type for local types in the resourceFileTemplate ([#301](https://github.com/cccteam/ccc/issues/301)) ([242813e](https://github.com/cccteam/ccc/commit/242813e49bab70c28f478c83d4b414a7a8a16cce))

## [0.1.10](https://github.com/cccteam/ccc/compare/resource/v0.1.9...resource/v0.1.10) (2025-05-12)


### Features

* Implement default update function support ([#295](https://github.com/cccteam/ccc/issues/295)) ([ff4be47](https://github.com/cccteam/ccc/commit/ff4be4784c039dc2df12ea30d4a0a53e6745807d))

## [0.1.9](https://github.com/cccteam/ccc/compare/resource/v0.1.8...resource/v0.1.9) (2025-05-11)


### Bug Fixes

* Simple date typescript type ([#293](https://github.com/cccteam/ccc/issues/293)) ([ed662ca](https://github.com/cccteam/ccc/commit/ed662ca29687532870c4d702d2a297381ad56a3f))

## [0.1.8](https://github.com/cccteam/ccc/compare/resource/v0.1.7...resource/v0.1.8) (2025-05-09)


### Code Refactoring

* Refactor handler types for better code readability ([#289](https://github.com/cccteam/ccc/issues/289)) ([5357052](https://github.com/cccteam/ccc/commit/5357052237b6a5fa63ccea224c9ee4713c9191ae))

## [0.1.7](https://github.com/cccteam/ccc/compare/resource/v0.1.6...resource/v0.1.7) (2025-05-09)


### Bug Fixes

* Exclude resources with no handlers from typescript resource generation ([#287](https://github.com/cccteam/ccc/issues/287)) ([5e137e9](https://github.com/cccteam/ccc/commit/5e137e934deb15445207fb80b1196341a6541515))

## [0.1.6](https://github.com/cccteam/ccc/compare/resource/v0.1.5...resource/v0.1.6) (2025-05-08)


### Bug Fixes

* Ensure old values for inserts are set to nil in data change tracking ([#284](https://github.com/cccteam/ccc/issues/284)) ([72ab7d0](https://github.com/cccteam/ccc/commit/72ab7d0c05256c3c6eb185e55169eb8dc58c4ae2))
* Fix not found error condition for filters and where clauses ([#284](https://github.com/cccteam/ccc/issues/284)) ([72ab7d0](https://github.com/cccteam/ccc/commit/72ab7d0c05256c3c6eb185e55169eb8dc58c4ae2))


### Code Refactoring

* Remove unused closure ([#286](https://github.com/cccteam/ccc/issues/286)) ([8f4e075](https://github.com/cccteam/ccc/commit/8f4e07539490e26a2ef2d638d4086be1ff9a1001))

## [0.1.5](https://github.com/cccteam/ccc/compare/resource/v0.1.4...resource/v0.1.5) (2025-05-08)


### Features

* Support funcs for setting resource field defaults ([#268](https://github.com/cccteam/ccc/issues/268)) ([af6b4ab](https://github.com/cccteam/ccc/commit/af6b4abb622a4ba0c1419a56433d90c7e70575ab))

## [0.1.4](https://github.com/cccteam/ccc/compare/resource/v0.1.3...resource/v0.1.4) (2025-05-08)


### Features

* Implement InputOnly and OutputOnly field options ([#280](https://github.com/cccteam/ccc/issues/280)) ([4f670c3](https://github.com/cccteam/ccc/commit/4f670c3f3fbe68aa2d87b65a84e02878f3f8a9b8))


### Bug Fixes

* Allow setting immutable fields on Create ([#277](https://github.com/cccteam/ccc/issues/277)) ([f730f7b](https://github.com/cccteam/ccc/commit/f730f7b3d0fbd6c4ecdec075c4eca2809a42c302))

## [0.1.3](https://github.com/cccteam/ccc/compare/resource/v0.1.2...resource/v0.1.3) (2025-05-07)


### Bug Fixes

* Output the correct type name for local types in resourceFileTemplate ([#274](https://github.com/cccteam/ccc/issues/274)) ([c0c44c0](https://github.com/cccteam/ccc/commit/c0c44c036fb946471b9a89ba652b29680cc7d7fb))

## [0.1.2](https://github.com/cccteam/ccc/compare/resource/v0.1.1...resource/v0.1.2) (2025-05-06)


### Bug Fixes

* Correct where clause generation ([#275](https://github.com/cccteam/ccc/issues/275)) ([8902974](https://github.com/cccteam/ccc/commit/890297492a226da5aab99b43f255ad5030d1881f))

## [0.1.1](https://github.com/cccteam/ccc/compare/resource/v0.1.0...resource/v0.1.1) (2025-05-02)


### Features

* Add rpc method config generation ([#270](https://github.com/cccteam/ccc/issues/270)) ([d2f66c8](https://github.com/cccteam/ccc/commit/d2f66c824e3bcf889e808879f41ab7494d4805c8))

## [0.1.0](https://github.com/cccteam/ccc/compare/resource/v0.0.27...resource/v0.1.0) (2025-04-30)


### ⚠ BREAKING CHANGES

* Renamed `StructDecoder.DecodeWithoutPermissions()` -> `StructDecoder.Decode()`

### Features

* Add generation of typescript methods metadata ([#265](https://github.com/cccteam/ccc/issues/265)) ([5b25170](https://github.com/cccteam/ccc/commit/5b25170baeee4387f90f1937617490429fc09f7a))


### Bug Fixes

* Fix bug in StructDecoder permission checking with new RPCDecoder ([f0a1ff9](https://github.com/cccteam/ccc/commit/f0a1ff9b7083fa9a44017e51153c47729e70f430))
* Renamed `StructDecoder.DecodeWithoutPermissions()` -&gt; `StructDecoder.Decode()` ([f0a1ff9](https://github.com/cccteam/ccc/commit/f0a1ff9b7083fa9a44017e51153c47729e70f430))

## [0.0.27](https://github.com/cccteam/ccc/compare/resource/v0.0.26...resource/v0.0.27) (2025-04-24)


### Bug Fixes

* Check that the primary key is a generated UUID (vs just a UUID) ([#262](https://github.com/cccteam/ccc/issues/262)) ([78308f1](https://github.com/cccteam/ccc/commit/78308f1486ecc70ad404d2ee9cd720a4b5738b0f))

## [0.0.26](https://github.com/cccteam/ccc/compare/resource/v0.0.25...resource/v0.0.26) (2025-04-22)


### Features

* Support singular primary keys with names other than 'Id' ([#259](https://github.com/cccteam/ccc/issues/259)) ([733ebb0](https://github.com/cccteam/ccc/commit/733ebb02a25c4b24b109a9745163ec624178cdec))


### Bug Fixes

* add google's civil.date type to typescript overrides ([#257](https://github.com/cccteam/ccc/issues/257)) ([f4ee8f4](https://github.com/cccteam/ccc/commit/f4ee8f44d6bd18d9ed771acf80eb4c9e4f68ceac))
* run go mod tidy ([#260](https://github.com/cccteam/ccc/issues/260)) ([0381744](https://github.com/cccteam/ccc/commit/038174498e26525defc01a3ebada433c72b7f802))
* update resource & app handler templates to import google's civil package instead of ccc's ([#255](https://github.com/cccteam/ccc/issues/255)) ([fb54aef](https://github.com/cccteam/ccc/commit/fb54aefb0c3c87e5c5c2482e10d59ec626c9359f))

## [0.0.25](https://github.com/cccteam/ccc/compare/resource/v0.0.24...resource/v0.0.25) (2025-04-16)


### Features

* Do not label resource field as required when default defined in the DB ([#254](https://github.com/cccteam/ccc/issues/254)) ([8f11c65](https://github.com/cccteam/ccc/commit/8f11c6523b7fba6122f293b44734742245e08913))

## [0.0.24](https://github.com/cccteam/ccc/compare/resource/v0.0.23...resource/v0.0.24) (2025-04-11)


### Features

* update typescript template ([95dcc52](https://github.com/cccteam/ccc/commit/95dcc5242870ed956cfc9141a587ca591da8fcb4))

## [0.0.23](https://github.com/cccteam/ccc/compare/resource/v0.0.22...resource/v0.0.23) (2025-04-08)


### Code Refactoring

* typescript namespacing ([#248](https://github.com/cccteam/ccc/issues/248)) ([8187146](https://github.com/cccteam/ccc/commit/81871463b5b7a2ae4e0b244e119472ba73d79b5c))

## [0.0.22](https://github.com/cccteam/ccc/compare/resource/v0.0.21...resource/v0.0.22) (2025-04-05)


### Bug Fixes

* Fix auto commit bug ([#245](https://github.com/cccteam/ccc/issues/245)) ([459c358](https://github.com/cccteam/ccc/commit/459c358f41e4a9d3b625e5beb06a0e33c0a2cb37))


### Code Refactoring

* Refactor Add Column methods into a separate type ([#246](https://github.com/cccteam/ccc/issues/246)) ([632c1e3](https://github.com/cccteam/ccc/commit/632c1e3e0e3e0ee64a4de8e70cc043a3dfb6b16a))

## [0.0.21](https://github.com/cccteam/ccc/compare/resource/v0.0.20...resource/v0.0.21) (2025-04-04)


### Features

* Implement Commit Buffer ([#243](https://github.com/cccteam/ccc/issues/243)) ([f9e26fd](https://github.com/cccteam/ccc/commit/f9e26fdf93aad2e882f757411b82fecd85a4df4f))

## [0.0.20](https://github.com/cccteam/ccc/compare/resource/v0.0.19...resource/v0.0.20) (2025-04-03)


### Bug Fixes

* correct resource tag assignment in templates ([#241](https://github.com/cccteam/ccc/issues/241)) ([7a610e2](https://github.com/cccteam/ccc/commit/7a610e2518eff63d8647bd345db0c1ff8bc95686))

## [0.0.19](https://github.com/cccteam/ccc/compare/resource/v0.0.18...resource/v0.0.19) (2025-03-31)


### Features

* generate query clause ([#239](https://github.com/cccteam/ccc/issues/239)) ([0813f26](https://github.com/cccteam/ccc/commit/0813f2628fdd6177a8050be88b799f36feebe988))
* query builder ([#235](https://github.com/cccteam/ccc/issues/235)) ([884fc64](https://github.com/cccteam/ccc/commit/884fc64caf22f8279dbf2bf1c7d4d903363a0015))

## [0.0.18](https://github.com/cccteam/ccc/compare/resource/v0.0.17...resource/v0.0.18) (2025-03-28)


### Features

* Support for iter.Seq2 ([#236](https://github.com/cccteam/ccc/issues/236)) ([25c8d90](https://github.com/cccteam/ccc/commit/25c8d9051fca233c6d92733edc886316b4effdfe))

## [0.0.17](https://github.com/cccteam/ccc/compare/resource/v0.0.16...resource/v0.0.17) (2025-03-20)


### Features

* Stateful resources phase 1 - Move permission evaluation to QuerySet ([#230](https://github.com/cccteam/ccc/issues/230)) ([d418330](https://github.com/cccteam/ccc/commit/d418330f6f9be9f728958fe3a6c48fa0220ab860))


### Code Refactoring

* separate parser package ([#232](https://github.com/cccteam/ccc/issues/232)) ([6d856f1](https://github.com/cccteam/ccc/commit/6d856f147cff43951c15e30be4a774704b904f84))

## [0.0.16](https://github.com/cccteam/ccc/compare/resource/v0.0.15...resource/v0.0.16) (2025-03-18)


### Features

* use parsed index tags when generating handlers for views ([#228](https://github.com/cccteam/ccc/issues/228)) ([dca43fd](https://github.com/cccteam/ccc/commit/dca43fd2cf35a5eb7c9afcdc0132bca04c8272c2))


### Bug Fixes

* Support multi field filters ([#227](https://github.com/cccteam/ccc/issues/227)) ([04b9cf6](https://github.com/cccteam/ccc/commit/04b9cf6677b45c6ac0c159be3a9af72db456bb51))

## [0.0.15](https://github.com/cccteam/ccc/compare/resource/v0.0.14...resource/v0.0.15) (2025-03-07)


### Features

* RPC endpoint generation ([#223](https://github.com/cccteam/ccc/issues/223)) ([3c6b527](https://github.com/cccteam/ccc/commit/3c6b5278ff765617999038fc868ea855196b0c4f))


### Code Upgrade

* Upgrade to use omitzero from go 1.24 ([#218](https://github.com/cccteam/ccc/issues/218)) ([244851a](https://github.com/cccteam/ccc/commit/244851aa50b36b42179e2f5606a05fedfc34c431))

## [0.0.14](https://github.com/cccteam/ccc/compare/resource/v0.0.13...resource/v0.0.14) (2025-02-22)


### Features

* generalized the search functionality to allow filtering of any kind ([#197](https://github.com/cccteam/ccc/issues/197)) ([0b59a5e](https://github.com/cccteam/ccc/commit/0b59a5e088ce87783f1964b0c0ec939697f6a725))


### Bug Fixes

* Add missing fix ([#217](https://github.com/cccteam/ccc/issues/217)) ([e06f64a](https://github.com/cccteam/ccc/commit/e06f64ac1af402f993a99e23075c469c680392cb))


### Code Refactoring

* split resource and typescript generators ([#214](https://github.com/cccteam/ccc/issues/214)) ([85d281b](https://github.com/cccteam/ccc/commit/85d281b3b5632f2b603b78e3a416982ce41d413e))

## [0.0.13](https://github.com/cccteam/ccc/compare/resource/v0.0.12...resource/v0.0.13) (2025-02-19)


### Features

* Added columns query param to filter struct fields ([#188](https://github.com/cccteam/ccc/issues/188)) ([b31ef18](https://github.com/cccteam/ccc/commit/b31ef18188e329a154a4e04e1145b8a4c32778ed))
* Added generation for consolidated handler for many resources ([#203](https://github.com/cccteam/ccc/issues/203)) ([83376d1](https://github.com/cccteam/ccc/commit/83376d1b1614ac01e706b8c4c152bcfc1debab5d))
* Added generation for routes and router tests for plugin into existing backend structure ([#195](https://github.com/cccteam/ccc/issues/195)) ([b9cf171](https://github.com/cccteam/ccc/commit/b9cf1717f48875108fbf4afc1837b579b7a7d0a9))
* consolidate-parsing ([#196](https://github.com/cccteam/ccc/issues/196)) ([54a5779](https://github.com/cccteam/ccc/commit/54a57792324ff128f6624d0606c31b1d6743f0f0))
* make resource generation independent of typescript generation ([#211](https://github.com/cccteam/ccc/issues/211)) ([268fe41](https://github.com/cccteam/ccc/commit/268fe4164496cce2cc8fd48b363d2298ef63372e))
* move pkg info and directory change into generator ([#208](https://github.com/cccteam/ccc/issues/208)) ([48fd62a](https://github.com/cccteam/ccc/commit/48fd62a4d4288e0f7c9408bd8881e1f1f8fda70b))
* text search support ([#169](https://github.com/cccteam/ccc/issues/169)) ([5c2ab80](https://github.com/cccteam/ccc/commit/5c2ab8037ba978169f5db0439d74a859d441670c))


### Bug Fixes

* Bake imports into handler template ([#207](https://github.com/cccteam/ccc/issues/207)) ([6e46537](https://github.com/cccteam/ccc/commit/6e46537f273d0d809f1ddcd393afec74d1055f16))

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
