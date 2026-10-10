# Changelog

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
