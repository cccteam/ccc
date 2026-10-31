# Changelog

## [0.4.4](https://github.com/cccteam/ccc/compare/bedrock/v0.4.3...bedrock/v0.4.4) (2026-10-31)


### Features

* **bedrock:** the placement's persona block names the persona provider; the stack sets the issuer and derives the client id and the callback in the first environment and its pull requests; the pipeline registers each deployment with the provider after traffic and removes a torn-down or closed pull request's (deploy persona register, deploy persona remove) ([#1252](https://github.com/cccteam/ccc/issues/1252)) ([62dbffb](https://github.com/cccteam/ccc/commit/62dbffbe04950e2a3dcd70117cc333f290aab128))
* **impulse:** impulse add method persona writes the persona method, its cast and the cast's seeding; the persona-cast check holds the cast to its shape and its roles to the role file; the persona package carries the cast contract and the provider's registration client ([#1252](https://github.com/cccteam/ccc/issues/1252)) ([62dbffb](https://github.com/cccteam/ccc/commit/62dbffbe04950e2a3dcd70117cc333f290aab128))
* **resource:** the generator declares a persona method (PersonaMethod) that the served router mounts and the methods route offers only where the auth registers it, and Lodestar demonstrates the persona sign-in of the test environments on the console's crew auth, with its cast seeded at start ([#1252](https://github.com/cccteam/ccc/issues/1252)) ([62dbffb](https://github.com/cccteam/ccc/commit/62dbffbe04950e2a3dcd70117cc333f290aab128))


### Bug Fixes

* **bedrock:** a pull-request build registers its persona deployment from its own stack's outputs, so a pull request that introduces or changes the method registers before any release carries it to the environment's triggers ([#1252](https://github.com/cccteam/ccc/issues/1252)) ([62dbffb](https://github.com/cccteam/ccc/commit/62dbffbe04950e2a3dcd70117cc333f290aab128))
* **bedrock:** a pull-request stack with two regions applies: the revision templates output groups the services by name, since a pull request's service carries one name in every region, instead of failing on the duplicate key ([#1252](https://github.com/cccteam/ccc/issues/1252)) ([62dbffb](https://github.com/cccteam/ccc/commit/62dbffbe04950e2a3dcd70117cc333f290aab128))
* **bedrock:** the organization's environment layer grants each deploy identity the right to create its own OpenID Connect tokens, which Cloud Build's metadata server requires before it issues the identity token a persona registration proves itself with ([#1252](https://github.com/cccteam/ccc/issues/1252)) ([62dbffb](https://github.com/cccteam/ccc/commit/62dbffbe04950e2a3dcd70117cc333f290aab128))
* **impulse:** impulse new writes the application's name into the auth packages' Application constant, so the mail an auth sends names the application and not the skeleton's candidate ([#1252](https://github.com/cccteam/ccc/issues/1252)) ([62dbffb](https://github.com/cccteam/ccc/commit/62dbffbe04950e2a3dcd70117cc333f290aab128))


### Dependencies

* **bedrock:** bedrock pins impulse v0.6.13, the release of the persona method and cast it reads, in place of the pushed commit ([#1257](https://github.com/cccteam/ccc/issues/1257)) ([03f8d93](https://github.com/cccteam/ccc/commit/03f8d935a19d1aabd303fe756c6eee480837ac70))

## [0.4.3](https://github.com/cccteam/ccc/compare/bedrock/v0.4.2...bedrock/v0.4.3) (2026-10-29)


### Features

* **bedrock:** the operations workflow's `release` action runs the application's release trigger again on the default branch's head from GitHub, for the release a merged release pull request is owed when the platform ended the push's build before release-please ran; `bedrock release` dispatches it, and the operations identity acts as the release identity in the first environment ([#1235](https://github.com/cccteam/ccc/issues/1235)) ([1586c38](https://github.com/cccteam/ccc/commit/1586c38597c7788a3be2ebea2734cd18790066ac))

## [0.4.2](https://github.com/cccteam/ccc/compare/bedrock/v0.4.1...bedrock/v0.4.2) (2026-10-29)


### Bug Fixes

* **bedrock:** a release whose only change to the environment's stack is an output value (a setting the stack passes through to the revision templates or the substitutions) applies the stack instead of answering that it matches the code, the plan lists the changed outputs, and the applied line counts them ([#1219](https://github.com/cccteam/ccc/issues/1219)) ([7dfa50b](https://github.com/cccteam/ccc/commit/7dfa50bf5d88eb743677f58e183b900b7579ada7))

## [0.4.1](https://github.com/cccteam/ccc/compare/bedrock/v0.4.0...bedrock/v0.4.1) (2026-10-29)


### Bug Fixes

* **bedrock:** a Microsoft registration's tenant shape is read from the slot function impulse add method writes and from nowhere else, a registration built any other way is refused with its file and line, and the stack README's hand step names the supported account type from the code instead of saying it could not read it ([#1213](https://github.com/cccteam/ccc/issues/1213)) ([c6ace93](https://github.com/cccteam/ccc/commit/c6ace93fbeab7fff86f6325988c6a5207db6e469))

## [0.4.0](https://github.com/cccteam/ccc/compare/bedrock/v0.3.24...bedrock/v0.4.0) (2026-10-28)


### ⚠ BREAKING CHANGES

* **resource:** a roles file names every role in the one grammar (letters and digits starting with a capital letter, the scope never part of the name) and gives every role a description, one or two plain sentences saying what it is for, or the permission engine refuses it at start; each <Name>Roles table gains a Description STRING(MAX) NOT NULL column; UserSeats.Roles returns []access.RoleInfo and MemUserStore.DeclareRoles takes access.RoleInfo; the resource module requires github.com/cccteam/access.

### Features

* **impulse:** the skeletons name roles for their job (Administrator, TenantAdministrator, MachineOperator) and describe every role, the roles resources serve each role's description, and impulse check gains roles-file, which reads every role file the way the permission engine does ([#1198](https://github.com/cccteam/ccc/issues/1198)) ([c949146](https://github.com/cccteam/ccc/commit/c9491467fa98fb76abc8974a1b5fd8533f30b2bd))
* **resource:** every role carries a description and the user-management surface serves it: the roles rows and the generated client gain description, the seat store lists each role with what it is for, and the generator detects an enumeration table by its Id and Description columns together ([#1198](https://github.com/cccteam/ccc/issues/1198)) ([c949146](https://github.com/cccteam/ccc/commit/c9491467fa98fb76abc8974a1b5fd8533f30b2bd))

## [0.3.24](https://github.com/cccteam/ccc/compare/bedrock/v0.3.23...bedrock/v0.3.24) (2026-10-28)


### Bug Fixes

* **bedrock:** the stack declares the administered tenants and Workspaces variables its locals hand to the processes, so a stack with a directory registration plans again on 0.3.23's successor, and the render tests hold every referenced variable to a declaration ([#1197](https://github.com/cccteam/ccc/issues/1197)) ([0774118](https://github.com/cccteam/ccc/commit/07741180122523155d74c535ca95b7acea9bc786))

## [0.3.23](https://github.com/cccteam/ccc/compare/bedrock/v0.3.22...bedrock/v0.3.23) (2026-10-28)


### Features

* **bedrock:** the administered tenants and Workspaces of a directory registration are placement variables ([3d45773](https://github.com/cccteam/ccc/commit/3d45773884f4d35a42db39312c7a451bf5fc9086))
* **impulse:** every generated auth requires the sign-in code unless a remembered browser or an administered directory stands in for it, a directory method declares the organizations the deployment administers (_ADMINISTERED_TENANTS, _ADMINISTERED_WORKSPACES), the session migration creates the sign-in codes and trusted devices tables, and the pins move to session v0.13.14 ([3d45773](https://github.com/cccteam/ccc/commit/3d45773884f4d35a42db39312c7a451bf5fc9086))
* **resource:** every session carries a second factor: the generated router serves the sign-in code routes, the user surface resets a person's remembered browsers (Reset&lt;…&gt;UserFactors), and Lodestar demonstrates the code, the attested directory and the remembered browser ([3d45773](https://github.com/cccteam/ccc/commit/3d45773884f4d35a42db39312c7a451bf5fc9086))


### Bug Fixes

* a declared shipped program's main package may carry its own tagged settings, which bedrock's derivation leaves out of the application's configuration, and impulse refuses any environment read outside the config package except inside such a package ([#1195](https://github.com/cccteam/ccc/issues/1195)) ([f2ac8e0](https://github.com/cccteam/ccc/commit/f2ac8e029cd1c8cf1587a02679fff1ff8bbc5492))

## [0.3.22](https://github.com/cccteam/ccc/compare/bedrock/v0.3.21...bedrock/v0.3.22) (2026-10-28)


### Features

* an application ships a command program with its release: ShipsProgram on a machine outlet names its main package and platforms, the image build compiles it, the outlet serves it behind its token check, and an installed copy upgrades itself from the outlet through resource/shipped ([#1184](https://github.com/cccteam/ccc/issues/1184)) ([5b89382](https://github.com/cccteam/ccc/commit/5b893827e026be12938639afe369e93a58239cba))

## [0.3.21](https://github.com/cccteam/ccc/compare/bedrock/v0.3.20...bedrock/v0.3.21) (2026-10-28)


### Bug Fixes

* **bedrock:** the deploy steps retry Cloud Run's and the registry's transient answers (5xx, timeouts) with doubling waits before failing the release, and the image build is run once more when a base image's pull fails transiently ([#1179](https://github.com/cccteam/ccc/issues/1179)) ([bd3ab05](https://github.com/cccteam/ccc/commit/bd3ab059085a8ec70a0208829e2c53be858ebda5))

## [0.3.20](https://github.com/cccteam/ccc/compare/bedrock/v0.3.19...bedrock/v0.3.20) (2026-10-27)


### Bug Fixes

* **bedrock:** restore and the other operation commands say when GitHub does not list the operations workflow, and org register prints the apply action in every case, never a trigger to run by hand ([41f03d4](https://github.com/cccteam/ccc/commit/41f03d4cd903a6702c396811509141b5e8a771d6))
* **bedrock:** the operations workflow lists itself on the push that adds or changes its file, so an operation can start from a repository whose first push did not list it ([41f03d4](https://github.com/cccteam/ccc/commit/41f03d4cd903a6702c396811509141b5e8a771d6))

## [0.3.19](https://github.com/cccteam/ccc/compare/bedrock/v0.3.18...bedrock/v0.3.19) (2026-10-27)


### Bug Fixes

* **bedrock:** the stack README's hand step for a Microsoft registration names the supported account type the registration's tenant shape needs, and the consent another tenant gives a multi-tenant one ([#1168](https://github.com/cccteam/ccc/issues/1168)) ([0448049](https://github.com/cccteam/ccc/commit/0448049627c8c1190a5195720ca64db16877b633))
* **impulse:** the application reader records a Microsoft registration's tenant shape (one, listed, any, personal) from its configuration ([#1168](https://github.com/cccteam/ccc/issues/1168)) ([0448049](https://github.com/cccteam/ccc/commit/0448049627c8c1190a5195720ca64db16877b633))

## [0.3.18](https://github.com/cccteam/ccc/compare/bedrock/v0.3.17...bedrock/v0.3.18) (2026-10-27)


### Features

* **bedrock:** every environment protects its data stores from a destroy, and a release retires a secret or a file store one release before removing it ([#1160](https://github.com/cccteam/ccc/issues/1160)) ([87f9b8e](https://github.com/cccteam/ccc/commit/87f9b8e42eb98f8cd6fb21998eb40981b9e1f011))
* **bedrock:** the pipeline builds every revision from the release's own stack, and a rollback applies no stack ([#1160](https://github.com/cccteam/ccc/issues/1160)) ([87f9b8e](https://github.com/cccteam/ccc/commit/87f9b8e42eb98f8cd6fb21998eb40981b9e1f011))
* **impulse:** the handoff brief tells the agent that anything the running release uses is retired in one release and removed in the next ([#1160](https://github.com/cccteam/ccc/issues/1160)) ([87f9b8e](https://github.com/cccteam/ccc/commit/87f9b8e42eb98f8cd6fb21998eb40981b9e1f011))


### Bug Fixes

* **bedrock:** the memberships on the database and a file store's bucket policy follow the database's and the bucket's ids, so a change to their attributes no longer recreates the grants ([#1160](https://github.com/cccteam/ccc/issues/1160)) ([87f9b8e](https://github.com/cccteam/ccc/commit/87f9b8e42eb98f8cd6fb21998eb40981b9e1f011))

## [0.3.17](https://github.com/cccteam/ccc/compare/bedrock/v0.3.16...bedrock/v0.3.17) (2026-10-24)


### Bug Fixes

* the Google hand steps say the Cloud Identity API must be enabled in the project that owns the OAuth client ([#1121](https://github.com/cccteam/ccc/issues/1121)) ([2596921](https://github.com/cccteam/ccc/commit/2596921c7f86b5a2ea6d0dfffe07e24891c6445d))

## [0.3.16](https://github.com/cccteam/ccc/compare/bedrock/v0.3.15...bedrock/v0.3.16) (2026-10-24)


### Bug Fixes

* the deployer's plan comment separates the environments with a blank line and sets the unpinned-secret notice as its own paragraph ([#1114](https://github.com/cccteam/ccc/issues/1114)) ([3a8ae2e](https://github.com/cccteam/ccc/commit/3a8ae2e9deed9b0c74ab158ced160a0a34b30c76))

## [0.3.15](https://github.com/cccteam/ccc/compare/bedrock/v0.3.14...bedrock/v0.3.15) (2026-10-24)


### Features

* the stack carries an optional From address for the auths' mail, so a no-reply alias of the mailbox account is the sender ([#1109](https://github.com/cccteam/ccc/issues/1109)) ([4c7ad78](https://github.com/cccteam/ccc/commit/4c7ad78ada3142c3725603ec6468a08eb260fbac))

## [0.3.14](https://github.com/cccteam/ccc/compare/bedrock/v0.3.13...bedrock/v0.3.14) (2026-10-23)


### Bug Fixes

* the application identity may revoke a browser's live identity on sign-out: the stack grants it Firebase Authentication Admin ([#1106](https://github.com/cccteam/ccc/issues/1106)) ([84dc01f](https://github.com/cccteam/ccc/commit/84dc01f0447cbb0f69d66917698d350b2b2412f7))

## [0.3.13](https://github.com/cccteam/ccc/compare/bedrock/v0.3.12...bedrock/v0.3.13) (2026-10-23)


### Bug Fixes

* a retired application's plan passes in the other environments, and the retire's journal line claims no final backup of an earlier destroy ([#1097](https://github.com/cccteam/ccc/issues/1097)) ([5a3f66a](https://github.com/cccteam/ccc/commit/5a3f66a42ec1aa421228614f9933f1f62e23271a))

## [0.3.12](https://github.com/cccteam/ccc/compare/bedrock/v0.3.11...bedrock/v0.3.12) (2026-10-23)


### Features

* bedrock org retire takes an application out of the organization, and bedrock destroy takes a final backup first ([#1090](https://github.com/cccteam/ccc/issues/1090)) ([00bc3d0](https://github.com/cccteam/ccc/commit/00bc3d047032211965d029cd47f74f10ae7d016c))

## [0.3.11](https://github.com/cccteam/ccc/compare/bedrock/v0.3.10...bedrock/v0.3.11) (2026-10-23)


### Features

* invitations, email addresses and sign-up from mailed links across resource, Lodestar, impulse and bedrock ([#1082](https://github.com/cccteam/ccc/issues/1082)) ([c841798](https://github.com/cccteam/ccc/commit/c841798d7c1f40aacea46dccf25f7a3fc1c2754d))


### Bug Fixes

* bedrock's Cloud Build client sends a read or a token mint again after a 502, 503 or 504, as the GitHub client does ([#1074](https://github.com/cccteam/ccc/issues/1074)) ([9c340ef](https://github.com/cccteam/ccc/commit/9c340ef49a988ca2d4d6e6c68242370c1266e9be))

## [0.3.10](https://github.com/cccteam/ccc/compare/bedrock/v0.3.9...bedrock/v0.3.10) (2026-10-23)


### Code Upgrade

* **deps:** Go 1.27.3; the Go image moves to go1.27.3, so built images carry crypto/x509 GO-2026-6809's fix ([#1059](https://github.com/cccteam/ccc/issues/1059)) ([24d17c1](https://github.com/cccteam/ccc/commit/24d17c14405fd51e065a2ee7af018b637ed58ec4))

## [0.3.9](https://github.com/cccteam/ccc/compare/bedrock/v0.3.8...bedrock/v0.3.9) (2026-10-22)


### Bug Fixes

* a database without a schema migrations table yet reads as no version, so a pull request's fresh database migrates ([#1056](https://github.com/cccteam/ccc/issues/1056)) ([4fbab03](https://github.com/cccteam/ccc/commit/4fbab03dbcd05f27ab6a5355c7396e914dcf0759))

## [0.3.8](https://github.com/cccteam/ccc/compare/bedrock/v0.3.7...bedrock/v0.3.8) (2026-10-22)


### Bug Fixes

* a pull request's stack plan and apply wait for the state lock instead of failing when another build holds it ([#1054](https://github.com/cccteam/ccc/issues/1054)) ([60a3264](https://github.com/cccteam/ccc/commit/60a3264c52fd2136fb381f6420c5fdd4b0e86593))
* the pull-request stack's stored outputs carry the migrate command's secrets, so the preview build's migrations run ([#1053](https://github.com/cccteam/ccc/issues/1053)) ([f4a0db1](https://github.com/cccteam/ccc/commit/f4a0db1c81f21256b704669171f47c90733c16d1))

## [0.3.7](https://github.com/cccteam/ccc/compare/bedrock/v0.3.6...bedrock/v0.3.7) (2026-10-22)


### Bug Fixes

* a pull request's services carry its name, and the plan guard passes a creation named only at the apply ([#1051](https://github.com/cccteam/ccc/issues/1051)) ([18b20ca](https://github.com/cccteam/ccc/commit/18b20ca883ce9953ed10ed9569f480d0869256a3))

## [0.3.6](https://github.com/cccteam/ccc/compare/bedrock/v0.3.5...bedrock/v0.3.6) (2026-10-22)


### Bug Fixes

* the pull-request stack job reads Spanner, the state and the generated pins with its own credentials when it runs as the apply identity ([#1049](https://github.com/cccteam/ccc/issues/1049)) ([945ca35](https://github.com/cccteam/ccc/commit/945ca35b444479dbdfb953bced4c88ed73a85f9e))

## [0.3.5](https://github.com/cccteam/ccc/compare/bedrock/v0.3.4...bedrock/v0.3.5) (2026-10-22)


### Bug Fixes

* a Cloud Armor rule's description fits Google's 256-character limit, so a stack with the sign-in throttles applies ([#1042](https://github.com/cccteam/ccc/issues/1042)) ([e904570](https://github.com/cccteam/ccc/commit/e904570d502b5c708b8623159df468656b39b662))
* bedrock counts a Spanner instance's databases against its size before a registration or a stack's plan adds one ([#1048](https://github.com/cccteam/ccc/issues/1048)) ([a1b4f00](https://github.com/cccteam/ccc/commit/a1b4f0091f029db012b6010d3a7f534f935eb5fc))
* the apply identity reads the records bucket, which the pull request's stack job lists before its plan ([#1039](https://github.com/cccteam/ccc/issues/1039)) ([9434124](https://github.com/cccteam/ccc/commit/94341246c650cc133d56f755ccd33e09b329c6b6))
* the check trigger refuses a Cloud Armor entry removed in one step, reading the base branch through GitHub ([#1044](https://github.com/cccteam/ccc/issues/1044)) ([5012302](https://github.com/cccteam/ccc/commit/50123027c86306adb1f08058cd0f0c59ab1d0ebc))
* the GitHub client retries a stalled or gateway-failed read; one slow answer no longer fails the release trigger ([#1041](https://github.com/cccteam/ccc/issues/1041)) ([29c6096](https://github.com/cccteam/ccc/commit/29c6096e92595015e6cb71614369ade33d5325ae))

## [0.3.4](https://github.com/cccteam/ccc/compare/bedrock/v0.3.3...bedrock/v0.3.4) (2026-10-21)


### Features

* a consolidation passes the migrations job and the guard, which reads every environment's migration version ([#987](https://github.com/cccteam/ccc/issues/987)) ([289e80c](https://github.com/cccteam/ccc/commit/289e80c743a255529a17edf027963fabd4b5a6e9))
* a site serves on a vanity domain delegated whole to the network project, verified by bedrock domain check ([#1002](https://github.com/cccteam/ccc/issues/1002)) ([ec872be](https://github.com/cccteam/ccc/commit/ec872bee6a86ea6fa9514cc4cb4037cf16983691))
* bedrock carries every Auth's registrations, and the application code parts from the repository name ([#981](https://github.com/cccteam/ccc/issues/981)) ([a5f247a](https://github.com/cccteam/ccc/commit/a5f247af65b9c4fa0c75f124da9dac0ffd7d2b9a))
* bedrock renders a sites-layout application's stack with a service and a backend per site ([#1000](https://github.com/cccteam/ccc/issues/1000)) ([39134e6](https://github.com/cccteam/ccc/commit/39134e62cb656f3b604d083c08f1b14f38f74b4c))
* bedrock upgrade applies 0-bootstrap when the pin changes kind; the release shape's fetch refuses a commit pin ([#1013](https://github.com/cccteam/ccc/issues/1013)) ([2039435](https://github.com/cccteam/ccc/commit/2039435ba2bcce8954c3a1eff43e59cd2b7c8662))
* each site declares its hostname label in the placement; the organization records the hostnames its stack serves and refuses a collision ([#998](https://github.com/cccteam/ccc/issues/998)) ([2f6728b](https://github.com/cccteam/ccc/commit/2f6728b6415a7ac5dbdb636512ec28f6ecb09ac0))
* the Cloud Armor policy throttles each outlet's sign-in routes by client address, at a threshold tfvars sets ([#1031](https://github.com/cccteam/ccc/issues/1031)) ([f831635](https://github.com/cccteam/ccc/commit/f8316353af6ecef32fa4bb08a6b2be0241dc3d52))
* the migrate command seeds each auth's first administrator while its users table is empty ([#982](https://github.com/cccteam/ccc/issues/982)) ([7e82fc3](https://github.com/cccteam/ccc/commit/7e82fc3a44983e18adede47e63ef0c63ab505bd1))


### Bug Fixes

* a multi-site service passes its binary as args, not command, so the placeholder image starts on the first apply ([#1008](https://github.com/cccteam/ccc/issues/1008)) ([aba21eb](https://github.com/cccteam/ccc/commit/aba21eb85b5deb02aac145fa4fa3f047e48104a4))
* a sites-layout service serves its bundle from the directory the Procfile names, with the project segment ([#1014](https://github.com/cccteam/ccc/issues/1014)) ([3622145](https://github.com/cccteam/ccc/commit/3622145306a0acb7ce485b5d8110764650f2c727))
* bedrock check refuses a multi-site image that keeps an ENTRYPOINT; render notes the lines a kept Dockerfile needs ([#1009](https://github.com/cccteam/ccc/issues/1009)) ([f928eb0](https://github.com/cccteam/ccc/commit/f928eb0d250311db9c4bd4b96e1260bcac994abd))
* bedrock migration consolidate says the pull-request job and the guard accept a consolidation by its shape ([#1031](https://github.com/cccteam/ccc/issues/1031)) ([f831635](https://github.com/cccteam/ccc/commit/f8316353af6ecef32fa4bb08a6b2be0241dc3d52))
* bedrock migration renumber leaves a consolidation at the committed sequence's highest index alone ([#1031](https://github.com/cccteam/ccc/issues/1031)) ([f831635](https://github.com/cccteam/ccc/commit/f8316353af6ecef32fa4bb08a6b2be0241dc3d52))
* bedrock org register --next prints the first stack apply that works, read from GitHub's workflow list ([#993](https://github.com/cccteam/ccc/issues/993)) ([a0bfd35](https://github.com/cccteam/ccc/commit/a0bfd3521b6df3aff5cabf6507f8327bbea6aabf))
* bedrock pins impulse at its v0.5.0 release, ahead of the bedrock release ([#1036](https://github.com/cccteam/ccc/issues/1036)) ([dba9b4f](https://github.com/cccteam/ccc/commit/dba9b4f6d56dd72cead7ed3899e9ee07cf2f68e6))
* org register --next at the grants step says the first push comes first while the repository is empty ([#1007](https://github.com/cccteam/ccc/issues/1007)) ([2370ee5](https://github.com/cccteam/ccc/commit/2370ee5d37db8b9fa1e3b90e3c2540501b75180c))
* the consolidation guard reads a 403 on the generation after the live database as the end of the generations ([#1031](https://github.com/cccteam/ccc/issues/1031)) ([f831635](https://github.com/cccteam/ccc/commit/f8316353af6ecef32fa4bb08a6b2be0241dc3d52))
* the deploy identity writes traces: the migrate command runs on the build worker with the application's tracer ([#991](https://github.com/cccteam/ccc/issues/991)) ([91ca4aa](https://github.com/cccteam/ccc/commit/91ca4aad274783ca9fd5ad74831a01748fe0861b))
* the migrate command reads the first administrator's generated password, which the deploy identity alone may access ([#984](https://github.com/cccteam/ccc/issues/984)) ([dae45e8](https://github.com/cccteam/ccc/commit/dae45e8c259fca244335b659d4ec517b0f92bc23))
* the stack's plan untaints a Cloud Run service or job whose create failed, so the apply updates it in place ([#1011](https://github.com/cccteam/ccc/issues/1011)) ([ec7a052](https://github.com/cccteam/ccc/commit/ec7a052f65bd966208b34575eeb153e6c4fd56b0))


### Code Upgrade

* **deps:** Go 1.26.10; crypto/x509 GO-2026-6809 fixed ([#1018](https://github.com/cccteam/ccc/issues/1018)) ([25dc507](https://github.com/cccteam/ccc/commit/25dc507e6226ed8d39fd49960a87ec04d8a04f5a))

## [0.3.3](https://github.com/cccteam/ccc/compare/bedrock/v0.3.2...bedrock/v0.3.3) (2026-10-17)


### Features

* bedrock migration consolidate folds the schema sequence into one file at the latest index, the up files in order and the down files in reverse ([#972](https://github.com/cccteam/ccc/issues/972)) ([64ae779](https://github.com/cccteam/ccc/commit/64ae779134b61b3acb48328b56b2eaa4485b7d58))
* bedrock patch names the hotfix path what it is, with patch status, the placement's maintenance window and the merge-back guard ([#969](https://github.com/cccteam/ccc/issues/969)) ([fbcdfb3](https://github.com/cccteam/ccc/commit/fbcdfb3f1aec48ba85bae026c2cf1c5e7e6d1587))
* the operations workflow's teardown and destroy actions take an environment down through the stack trigger as the apply identity ([#970](https://github.com/cccteam/ccc/issues/970)) ([45a082a](https://github.com/cccteam/ccc/commit/45a082afb2e77ab2b5ebe8389facaf1535af1215))


### Bug Fixes

* a destroyed pull-request stack's state object is deleted, and a state that holds nothing is skipped before any output is read ([#968](https://github.com/cccteam/ccc/issues/968)) ([5ee5868](https://github.com/cccteam/ccc/commit/5ee5868ce4ed40ee53c68045453e91ba45666479))
* the CI migrations job and bedrock render read the whole migrations directory by the sequence rule ([#963](https://github.com/cccteam/ccc/issues/963)) ([a60cc3c](https://github.com/cccteam/ccc/commit/a60cc3c100ebbd7378b6261822c0e57cf6b11b7d))
* the freeze on committed migrations starts at the first release; before it the renumber packs the whole sequence ([#967](https://github.com/cccteam/ccc/issues/967)) ([7fb8356](https://github.com/cccteam/ccc/commit/7fb8356273f69d500f2d9fdbbc027c6793f7568c))
* the organization triggers' steps follow the pin kind, so a release pin renders no build step and pulls no Go image ([#962](https://github.com/cccteam/ccc/issues/962)) ([ab01fda](https://github.com/cccteam/ccc/commit/ab01fda0a36aaba271f67037a6746607ee30ddfa))

## [0.3.2](https://github.com/cccteam/ccc/compare/bedrock/v0.3.1...bedrock/v0.3.2) (2026-10-15)


### Bug Fixes

* the records buckets and the file stores keep Cloud Storage's default grants to the project's basic roles ([#960](https://github.com/cccteam/ccc/issues/960)) ([d7f041b](https://github.com/cccteam/ccc/commit/d7f041bc2f6e3a842269e647c8479b888977d9ea))

## [0.3.1](https://github.com/cccteam/ccc/compare/bedrock/v0.3.0...bedrock/v0.3.1) (2026-10-15)


### Features

* 2-env's stack trigger applies an application's stack for the first time, started by the operations workflow ([#947](https://github.com/cccteam/ccc/issues/947)) ([2bff55a](https://github.com/cccteam/ccc/commit/2bff55ab43e7d73c014f6820d4dc855b2c2a8255))
* an application's repository requires the stack's check trigger once the stack has had its first apply ([#948](https://github.com/cccteam/ccc/issues/948)) ([f00bf79](https://github.com/cccteam/ccc/commit/f00bf7994b24f459b8bcfec8f683e0b6b9ffbb56))
* bedrock org destroy deletes the organization's projects by hand, 1-org then 0-bootstrap on local state ([#941](https://github.com/cccteam/ccc/issues/941)) ([b55495f](https://github.com/cccteam/ccc/commit/b55495fb799cbf642755d985ec713b34f655a608))
* bedrock org teardown &lt;app&gt; and bedrock org serve take one application out of service and back ([#957](https://github.com/cccteam/ccc/issues/957)) ([eb748aa](https://github.com/cccteam/ccc/commit/eb748aa357c97997e2188ee0279131c41fff453f))
* bedrock teardown and destroy take an environment's stack down by hand, and bedrock org teardown takes the organization's applications out of service ([#944](https://github.com/cccteam/ccc/issues/944)) ([725f3b8](https://github.com/cccteam/ccc/commit/725f3b8137c0709d76b497d33fc49a97e2369d92))
* the placement records how far an application's registration has gone, so every pull request of it passes the check ([#946](https://github.com/cccteam/ccc/issues/946)) ([746915d](https://github.com/cccteam/ccc/commit/746915d1f092e7ad01871b71a4f776367ee592cc))
* the stack trigger's apply brings a torn-down stack back, with the generated secrets' pins derived from their containers ([#952](https://github.com/cccteam/ccc/issues/952)) ([599b0bd](https://github.com/cccteam/ccc/commit/599b0bd82174b623cf2beb2d313f611b12e9f10f))


### Bug Fixes

* 2-env applies before 2-net and the environment layer identity may add Firebase to its project ([#942](https://github.com/cccteam/ccc/issues/942)) ([8a73acf](https://github.com/cccteam/ccc/commit/8a73acfb55fc658267a708941ebaebcbee19f273))
* the apply run initializes every layer before applying any, so a layer may read one that has never applied ([#945](https://github.com/cccteam/ccc/issues/945)) ([5b411e6](https://github.com/cccteam/ccc/commit/5b411e6230497ad3ce9b7fddf6c880afc74b2a46))
* the operations workflow's migrations job leaves the rotate action alone, as it leaves the other actions ([#951](https://github.com/cccteam/ccc/issues/951)) ([68fb54b](https://github.com/cccteam/ccc/commit/68fb54b7a31f2393042ceeec46640f2981fd71b4))
* the plan trigger passes a layer that has never applied as planned after its first apply instead of failing its init ([#943](https://github.com/cccteam/ccc/issues/943)) ([3a3a5c6](https://github.com/cccteam/ccc/commit/3a3a5c6955f50c04738df948e21b4fb9a7bb247a))
* the release trigger's step hands release-please its token as --token, the one place release-please reads it ([#950](https://github.com/cccteam/ccc/issues/950)) ([24513ff](https://github.com/cccteam/ccc/commit/24513ff20fa16e839e3f337098212cc0238c4e62))

## [0.3.0](https://github.com/cccteam/ccc/compare/bedrock/v0.2.2...bedrock/v0.3.0) (2026-10-14)


### ⚠ BREAKING CHANGES

* **bedrock:** an organization and its applications move by the hand steps in bedrock/README.md, "Upgrading to the Cloud Build triggers": the Cloud Build token and the deployer GitHub App's key move to the boot project and placement.json, 2-env's four GitHub values retire, 0-bootstrap is applied once by hand, and every application's stack is applied once in tst by hand so its triggers exist.

### Features

* **bedrock:** a release whose code declares a secret with no pinned version in an environment after the first is refused at that environment's plan; a pull request's plan summary says it ([#930](https://github.com/cccteam/ccc/issues/930)) ([2ecb1f3](https://github.com/cccteam/ccc/commit/2ecb1f32581f3ee643c67553f81f6c97b9b7b2f3))
* **bedrock:** the number of approving reviews a merge needs is a placement setting, one by default, on every application repository and the organization's own, whose default branch also requires the plan trigger's check ([#930](https://github.com/cccteam/ccc/issues/930)) ([2ecb1f3](https://github.com/cccteam/ccc/commit/2ecb1f32581f3ee643c67553f81f6c97b9b7b2f3))
* **bedrock:** the organization's layers and an application's pull-request stack are planned and applied from Cloud Build triggers whose steps the render holds, the check of every owned file first; the layers and infrastructure workflows and the federation pool retire; each application's required check is its stack's check trigger ([#930](https://github.com/cccteam/ccc/issues/930)) ([2ecb1f3](https://github.com/cccteam/ccc/commit/2ecb1f32581f3ee643c67553f81f6c97b9b7b2f3))
* release-please runs in Cloud Build as a release identity; generated secrets are made and pinned by the pipeline ([#937](https://github.com/cccteam/ccc/issues/937)) ([5ba7a19](https://github.com/cccteam/ccc/commit/5ba7a196f41586c7d2067c6d8a62be1878478a9a))
* stack and organization ids in the names Google holds after a delete and in the deployment records' paths ([#935](https://github.com/cccteam/ccc/issues/935)) ([ff29eee](https://github.com/cccteam/ccc/commit/ff29eee202a5acdb3d718a82df77a5cee8530389))


### Bug Fixes

* bedrock carries no migration blocks or retired-file removals; an earlier shape is re-created, not migrated ([#933](https://github.com/cccteam/ccc/issues/933)) ([77d1f62](https://github.com/cccteam/ccc/commit/77d1f62c429a040c6f80ce6619c35a99147801fd))
* **bedrock:** the bootstrap README's alternative to the Cloud Build browser step is a classic personal access token with the repo, read:user and read:org scopes ([#930](https://github.com/cccteam/ccc/issues/930)) ([2ecb1f3](https://github.com/cccteam/ccc/commit/2ecb1f32581f3ee643c67553f81f6c97b9b7b2f3))
* the application plan identity reads the request-log exclusion; a failed plan's comment carries its errors; no empty scaling block ([#934](https://github.com/cccteam/ccc/issues/934)) ([ad818a1](https://github.com/cccteam/ccc/commit/ad818a1b115699018d13040ebb384a370256053c))
* the Cloud Build triggers' first lab runs: runner reads, markers by name, boot plan identities, plan comments ([537c142](https://github.com/cccteam/ccc/commit/537c14247c9ac2f5c943a1103b69654ec130781d))

## [0.2.2](https://github.com/cccteam/ccc/compare/bedrock/v0.2.1...bedrock/v0.2.2) (2026-10-10)


### Bug Fixes

* **bedrock:** the OIDC callback is found on the generated router's bounded group as well as on the router itself, so an application at resource v0.13.0 or later renders ([#928](https://github.com/cccteam/ccc/issues/928)) ([4fae365](https://github.com/cccteam/ccc/commit/4fae365ae78eb158e3cb2ffb6fe41116e026f2d4))

## [0.2.1](https://github.com/cccteam/ccc/compare/bedrock/v0.2.0...bedrock/v0.2.1) (2026-10-10)


### Bug Fixes

* **bedrock:** a new application's image builds with the Chainguard Go image at go1.27.2 ([#925](https://github.com/cccteam/ccc/issues/925)) ([3c2f4b6](https://github.com/cccteam/ccc/commit/3c2f4b6e9c47f71628e1ef7b4340c7e7cb95c7cf))
* **bedrock:** the driver-declaration refusals name impulse upgrade as the remedy for an application behind the provider-drivers step, and the hand edit by its alias ([#925](https://github.com/cccteam/ccc/issues/925)) ([3c2f4b6](https://github.com/cccteam/ccc/commit/3c2f4b6e9c47f71628e1ef7b4340c7e7cb95c7cf))
* **impulse:** impulse upgrade's help and README state the upgrade order: impulse first, then bedrock, both in one change ([#925](https://github.com/cccteam/ccc/issues/925)) ([3c2f4b6](https://github.com/cccteam/ccc/commit/3c2f4b6e9c47f71628e1ef7b4340c7e7cb95c7cf))

## [0.2.0](https://github.com/cccteam/ccc/compare/bedrock/v0.1.4...bedrock/v0.2.0) (2026-10-10)


### ⚠ BREAKING CHANGES

* **tracer:** tracer.NewHandler takes options (tracer.NewHandler(opts ...HandlerOption)); call sites with no arguments are unchanged, any other call moves to the options.
* **resource:** jobs.NewCloudRun, jobs.CloudRun, jobs.FromEnvironment, jobs.JobOf and jobs.VersionKey leave resource/jobs for resource/jobs/cloudrun (cloudrun.Open, Driver.Start, JobOf, VersionKey); an application opens the job starter through the driver.

### Features

* **bedrock:** the stack derives its variables from the database, job and live drivers' declarations as well as the cloud driver's, reads the job template variable's name off the job driver's declaration, and refuses a job process without the driver's settings embedded or the settings embedded without a job process ([#915](https://github.com/cccteam/ccc/issues/915)) ([a81e76b](https://github.com/cccteam/ccc/commit/a81e76b28272e243a13bd8f742621312d3067d60))
* **bedrock:** the stack renders a project-level log exclusion per application service and environment from the release file's surfaces, so the infrastructure's request-log entries follow the words the code declares ([#916](https://github.com/cccteam/ccc/issues/916)) ([c1a7104](https://github.com/cccteam/ccc/commit/c1a71042afb90bfb812176e21b4b97455c1b440f))
* **impulse:** the provider-drivers recipe moves an application's configuration to the drivers under their neutral aliases and renames its wrappers for the kinds (DatabaseSettings, LiveSettings), the ledger takes the step, and the framework declarations list the four drivers' settings ([#915](https://github.com/cccteam/ccc/issues/915)) ([a81e76b](https://github.com/cccteam/ccc/commit/a81e76b28272e243a13bd8f742621312d3067d60))
* **impulse:** the skeleton candidates and the ledger take the request log step: logger v0.1.30, the tracer's options and the generated declarations ([#916](https://github.com/cccteam/ccc/issues/916)) ([c1a7104](https://github.com/cccteam/ccc/commit/c1a71042afb90bfb812176e21b4b97455c1b440f))
* **resource:** per-surface request log and trace declarations in the generator: WithRequestLog, WithMountedRoutes, OutletRequestLog, OutletTraces and the [@rpc](https://github.com/rpc), [@file](https://github.com/file) and [@schedule](https://github.com/schedule) log and trace words; the generated router hands the request logger and the tracer their prefix tables and the release file lists the surfaces ([#916](https://github.com/cccteam/ccc/issues/916)) ([c1a7104](https://github.com/cccteam/ccc/commit/c1a71042afb90bfb812176e21b4b97455c1b440f))
* **resource:** the database, the live change feed and the Cloud Run job starter open through provider drivers with Settings, Open and Close: resource/database/spanner, resource/database/postgres, resource/live/firestore and resource/jobs/cloudrun, each with a declaration package the tools read; the application binds the drivers under neutral aliases (database, liveservice, jobstarter, cloud) so a provider swap is the import line, and the live driver resolves its emulator project itself ([#915](https://github.com/cccteam/ccc/issues/915)) ([a81e76b](https://github.com/cccteam/ccc/commit/a81e76b28272e243a13bd8f742621312d3067d60))
* **tracer:** the handler takes options, and Surfaces hands it a prefix table that decides each request's sampling at span start: follow the front end, capped at a rate by trace id, or off ([#916](https://github.com/cccteam/ccc/issues/916)) ([c1a7104](https://github.com/cccteam/ccc/commit/c1a71042afb90bfb812176e21b4b97455c1b440f))


### Bug Fixes

* **bedrock:** the module pins resource v0.14.0 ([#923](https://github.com/cccteam/ccc/issues/923)) ([737c6ff](https://github.com/cccteam/ccc/commit/737c6ff23f8f2cec017e3aa29d0e4e794de7163d))
* **impulse:** the skeleton and the ledger pin tracer v0.3.0 and resource v0.14.0, the request log step's releases ([#923](https://github.com/cccteam/ccc/issues/923)) ([737c6ff](https://github.com/cccteam/ccc/commit/737c6ff23f8f2cec017e3aa29d0e4e794de7163d))

## [0.1.4](https://github.com/cccteam/ccc/compare/bedrock/v0.1.3...bedrock/v0.1.4) (2026-10-09)


### Features

* cloud/gcp exports its settings declaration; impulse's env-template check and bedrock's derivation read it ([#906](https://github.com/cccteam/ccc/issues/906)) ([d0b9f3e](https://github.com/cccteam/ccc/commit/d0b9f3ee63f58520481877765d664f4fe3bd9602))


### Bug Fixes

* the settings declaration moves to cloud/gcp/declaration, apart from the driver, so impulse links no cloud client ([#910](https://github.com/cccteam/ccc/issues/910)) ([8b3748a](https://github.com/cccteam/ccc/commit/8b3748a1b889a1650bc13dc43f90af2e62880761))

## [0.1.3](https://github.com/cccteam/ccc/compare/bedrock/v0.1.2...bedrock/v0.1.3) (2026-10-09)


### Code Upgrade

* Go 1.26.9 and x/net v0.60.0 in every module; net/http GO-2026-6612, GO-2026-6613 and GO-2026-6617 fixed ([#879](https://github.com/cccteam/ccc/issues/879)) ([4107fbb](https://github.com/cccteam/ccc/commit/4107fbb68a33305d109a6a8fec23e67986acf046))

## [0.1.2](https://github.com/cccteam/ccc/compare/bedrock/v0.1.1...bedrock/v0.1.2) (2026-10-08)


### Features

* 1-org enables telemetry.googleapis.com in the application projects (@cccteam/bedrock) ([#859](https://github.com/cccteam/ccc/issues/859)) ([c544506](https://github.com/cccteam/ccc/commit/c5445064c47d43a76fd29e711690a107f5c16312))
* derive expands an embedded framework settings struct into its variables (@cccteam/bedrock) ([#859](https://github.com/cccteam/ccc/issues/859)) ([c544506](https://github.com/cccteam/ccc/commit/c5445064c47d43a76fd29e711690a107f5c16312))
* the site and jobs identities hold telemetry.tracesWriter beside cloudtrace.agent (@cccteam/bedrock) ([#859](https://github.com/cccteam/ccc/issues/859)) ([c544506](https://github.com/cccteam/ccc/commit/c5445064c47d43a76fd29e711690a107f5c16312))
* the stack sets APP_TRACE_SAMPLING: all in pull-request stacks and tst, edge beyond (@cccteam/bedrock) ([#859](https://github.com/cccteam/ccc/issues/859)) ([c544506](https://github.com/cccteam/ccc/commit/c5445064c47d43a76fd29e711690a107f5c16312))

## [0.1.1](https://github.com/cccteam/ccc/compare/bedrock/v0.1.0...bedrock/v0.1.1) (2026-10-08)


### Features

* 1-org creates the GitHub-hosted runner and its runner group for the applications (@cccteam/bedrock) ([#854](https://github.com/cccteam/ccc/issues/854)) ([e4d1fd4](https://github.com/cccteam/ccc/commit/e4d1fd4a7a8859fb9f764d9b61fa079084a96522))
* the placement sizes the larger CI runner (ciLargeRunnerSize, ciLargeRunnerMaximum) (@cccteam/bedrock) ([#854](https://github.com/cccteam/ccc/issues/854)) ([e4d1fd4](https://github.com/cccteam/ccc/commit/e4d1fd4a7a8859fb9f764d9b61fa079084a96522))

## 0.1.0 (2026-10-06)


### Features

* a pull-request stack renders with short names, its own database and identities and the first environment's secrets, and an hourly sweep destroys closed pull requests' environments. ([#829](https://github.com/cccteam/ccc/issues/829)) ([21d3430](https://github.com/cccteam/ccc/commit/21d343010a5dec58bd6528a1f99d9eb43869ad2f))
* a release build applies each environment's stack before it deploys, a pull-request build plans every environment's stack as a read-only plan identity and posts the summaries on the pull request, and a placement change promotes as a release. ([#829](https://github.com/cccteam/ccc/issues/829)) ([21d3430](https://github.com/cccteam/ccc/commit/21d343010a5dec58bd6528a1f99d9eb43869ad2f))
* bedrock check refuses a Cloud Armor policy removed in one step (an environment on at the default branch and absent in the tree), since the entry goes to off first and is removed by a later apply; the infrastructure workflow fetches the default branch before the check. ([#829](https://github.com/cccteam/ccc/issues/829)) ([21d3430](https://github.com/cccteam/ccc/commit/21d343010a5dec58bd6528a1f99d9eb43869ad2f))
* bedrock domain add registers the apps domain and bedrock domain check resolves the live delegation for every domain shape and prints the missing records as the provider's form takes them; the core projects carry a lien. ([#829](https://github.com/cccteam/ccc/issues/829)) ([21d3430](https://github.com/cccteam/ccc/commit/21d343010a5dec58bd6528a1f99d9eb43869ad2f))
* bedrock is distributed as a GitHub Release with checksums; the placement pins bedrockVersion and bedrockSha256 (or a pushed commit's pseudo-version), the pipeline and the infrastructure workflow download and verify the pinned binary, and bedrock upgrade moves the pin. ([#829](https://github.com/cccteam/ccc/issues/829)) ([21d3430](https://github.com/cccteam/ccc/commit/21d343010a5dec58bd6528a1f99d9eb43869ad2f))
* bedrock maintenance off &lt;env&gt; takes an application out of the maintenance a failed run left on, moving traffic back to the revision the maintenance revision displaced, resuming the task queue and saying what the application comes back to (its database ready, or still being restored), while bedrock rerun finishes the run instead. ([#829](https://github.com/cccteam/ccc/issues/829)) ([21d3430](https://github.com/cccteam/ccc/commit/21d343010a5dec58bd6528a1f99d9eb43869ad2f))
* bedrock migration renumber moves a branch's own migrations to follow the default branch with no gap, and bedrock migration version, rerun and force dispatch the operations workflow to report or force the database's version below production. ([#829](https://github.com/cccteam/ccc/issues/829)) ([21d3430](https://github.com/cccteam/ccc/commit/21d343010a5dec58bd6528a1f99d9eb43869ad2f))
* bedrock org new renders the organization foundation from its placement, org render and org check keep it, org register adds an application and writes its first placement.json, org preflight checks the bootstrap roles before the seed, and the hand steps a new organization needs are printed and documented, each opening with where it happens. ([#829](https://github.com/cccteam/ccc/issues/829)) ([21d3430](https://github.com/cccteam/ccc/commit/21d343010a5dec58bd6528a1f99d9eb43869ad2f))
* bedrock render writes an application's stack under infrastructure/ from the code (through impulse's reader) and placement.json, and bedrock check compares every owned file with the committed one and refuses what the code cannot deploy. ([#829](https://github.com/cccteam/ccc/issues/829)) ([21d3430](https://github.com/cccteam/ccc/commit/21d343010a5dec58bd6528a1f99d9eb43869ad2f))
* bedrock rollback &lt;env&gt; --reason [--to] returns an environment to an earlier release with nothing of the database (the earlier release's build again, no migration run, the record listing the migrations the database holds), and bedrock restore &lt;env&gt; --reason --before &lt;release&gt; | --at &lt;moment&gt; | --backup &lt;name&gt; [--of &lt;database&gt;] returns the database in a run of its own, production included, into a new generation after a forensic backup of the live one, so a bad release is answered by the code rollback first and the restore second; bedrock backups &lt;env&gt; lists the runs that went live with their backups and the generations; a restore to a moment whose data holds a migration above the release is refused before anything runs; bedrock restore &lt;env&gt; below production replaces the database (empty, or from production's backup) and deploys the release. ([#829](https://github.com/cccteam/ccc/issues/829)) ([21d3430](https://github.com/cccteam/ccc/commit/21d343010a5dec58bd6528a1f99d9eb43869ad2f))
* bedrock secret add stores a value as a new version of the container the stack names and secret pin moves the version the release reads; build secrets are declared per environment; check lists every secret that tracks latest and refuses a required build-secret mount an environment does not declare. ([#829](https://github.com/cccteam/ccc/issues/829)) ([21d3430](https://github.com/cccteam/ccc/commit/21d343010a5dec58bd6528a1f99d9eb43869ad2f))
* every identity holds the least it needs: the deploy identity a custom build-reader role, each plan identity a custom reader role, and the apply identity's Spanner and storage grants bounded to its own database and buckets. ([#829](https://github.com/cccteam/ccc/issues/829)) ([21d3430](https://github.com/cccteam/ccc/commit/21d343010a5dec58bd6528a1f99d9eb43869ad2f))
* hotfix lines: bedrock hotfix start opens hotfix/&lt;major&gt;.&lt;minor&gt;.x at a released patch, bedrock hotfix merge brings a released hotfix to the default branch through a merge-back branch and a squash pull request, and a hotfix passes a database check in every environment. ([#829](https://github.com/cccteam/ccc/issues/829)) ([21d3430](https://github.com/cccteam/ccc/commit/21d343010a5dec58bd6528a1f99d9eb43869ad2f))
* maintenance windows: a breaking release (one whose outlets no longer answer the environment's release) waits inside the build for the environment's window and deploys behind the maintenance revision, which pauses and purges the task queue and lets requests in flight finish. ([#829](https://github.com/cccteam/ccc/issues/829)) ([21d3430](https://github.com/cccteam/ccc/commit/21d343010a5dec58bd6528a1f99d9eb43869ad2f))
* migrations run inline on the build worker as the deploy identity from the release's own migrate command; the guard refuses a migrations directory that is not one contiguous sequence, a committed migration that changed, and an index the default branch has taken. ([#829](https://github.com/cccteam/ccc/issues/829)) ([21d3430](https://github.com/cccteam/ccc/commit/21d343010a5dec58bd6528a1f99d9eb43869ad2f))
* the image build reads and writes a layer cache in the registry holding the dependency downloads alone, reads build secrets through Secret Manager into BuildKit mounts, passes values the stack makes as declared build arguments, and stamps the release into the binary. ([#829](https://github.com/cccteam/ccc/issues/829)) ([21d3430](https://github.com/cccteam/ccc/commit/21d343010a5dec58bd6528a1f99d9eb43869ad2f))
* the organization placement's ciLargeRunner is set by 1-org as the organization's Actions variable CI_LARGE_RUNNER, the larger runner every application's CI runs its test legs and image build on. ([#829](https://github.com/cccteam/ccc/issues/829)) ([21d3430](https://github.com/cccteam/ccc/commit/21d343010a5dec58bd6528a1f99d9eb43869ad2f))
* the organization's own OpenTofu runs through a rendered GitHub workflow: a pull request plans each changed layer as its plan identity, a merge applies them in layer order, every run signs in with no key through workload identity, and the repository's rulesets require bedrock check and the CI's fixed jobs. ([#829](https://github.com/cccteam/ccc/issues/829)) ([21d3430](https://github.com/cccteam/ccc/commit/21d343010a5dec58bd6528a1f99d9eb43869ad2f))
* the pipeline is cloudbuild.yaml rendered by bedrock, every step but the image build a bedrock deploy command run by the pinned bedrock: resolve, guard the migrations, plan and apply the stack, check and validate the release, build the image, create the jobs, run the migrations, deploy without traffic, shift traffic, and write the deployment record. ([#829](https://github.com/cccteam/ccc/issues/829)) ([21d3430](https://github.com/cccteam/ccc/commit/21d343010a5dec58bd6528a1f99d9eb43869ad2f))
* the placement records what the code cannot know and validates it: the prefix, environments, regions and domains, the bedrock pin, the build machine, instance bounds, Cloud Armor, build arguments, Spanner retention, release backups, the seed list, the approval environments and the maintenance windows. ([#829](https://github.com/cccteam/ccc/issues/829)) ([21d3430](https://github.com/cccteam/ccc/commit/21d343010a5dec58bd6528a1f99d9eb43869ad2f))
* the pull-request build is the developer's preview on /gcbrun and not a required check: a pull request merges on bedrock check and the CI jobs, release-please's pull request merges with no build, and the first environment's tag build is a release's first build. ([#829](https://github.com/cccteam/ccc/issues/829)) ([21d3430](https://github.com/cccteam/ccc/commit/21d343010a5dec58bd6528a1f99d9eb43869ad2f))
* the stack derives from the code: the Cloud Run services (their port named h2c when the site runs the framework's server), the Spanner database with its backup schedules and retention, a secret container for every field tagged secret, the backend services, the version, pull-request and rollback triggers, the records bucket and the file stores. ([#829](https://github.com/cccteam/ccc/issues/829)) ([21d3430](https://github.com/cccteam/ccc/commit/21d343010a5dec58bd6528a1f99d9eb43869ad2f))
