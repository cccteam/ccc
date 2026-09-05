# impulse

`impulse` is the command-line tool for Impulse applications: apps built on the cccteam
libraries with `ccc/resource` at the center.

The tool keeps no record of its own. Everything it needs it reads from files the
application already depends on: the generator program, `go.mod`, the browser apps'
`angular.json`, the process files, the test harnesses, and the config struct tags.

## Install

```sh
go install github.com/cccteam/ccc/impulse@latest
```

## impulse check

`check` verifies the agreements between an application's parts and exits non-zero when
any of them is broken. Run it from the application root, or point it at one:

```sh
impulse check
impulse check --app path/to/app --skip-generate
impulse check --only prettier-ignore,env-template --fix
impulse check --list
```

| Check | Verifies |
| --- | --- |
| `generator-program` | Every generator program uses options this release knows, with literal arguments. A program the tool cannot read completely is one it cannot later edit or migrate. |
| `options` | The generator programs declare one coherent option set, and the report states it: layout (flat or multi-site), sites, tenancy (`WithDomainRoute`, `WithConcealedDomains`), outlets, and targets. Handlers come with routes, `ForOutlet` names a declared session-serving outlet, the referenced directories exist, a `//go:generate` directive runs every program, the sites agree on tenancy, and a second site lives under `apps/<site>/`. |
| `tenancy-wired` | A program with `WithDomainRoute` has a migration creating the tenant-record table the segment names, at least one struct annotated `@permissionScope(domain)`, and every `access.MigrateRoles` call outside tests passing domains. A program without it has no tenant-scoped structs and passes no domains. The compiler and the generator hold the rest of the seam. |
| `outlet-wired` | Every outlet a program declares (the default from `GenerateRoutes` and each `WithRouterOutlet`) has its generated routes mounted by a hand-written file in the router package, a session-serving outlet has a `GenerateTypescript` target naming it, and that browser project's development proxy forwards the outlet's prefix. An outlet with no `@outlet` members yet is noted, not failed. |
| `sites-wired` | In the multi-site layout, every site has a main package under `apps/<site>/`, a process in the Procfile or process-compose file running it on its own `PORT`, and every site's router is imported by some package calling `access.MigrateRoles`, so a role migration (the union collection, or one per user pool) reconciles against the site's resources. |
| `emulator-version` | The generator option, the process files' image tags, and the test harnesses name one Spanner emulator version. |
| `prettier-ignore` | Each browser app's `.prettierignore` excludes the generated TypeScript. Prettier reflowing generated files breaks generate idempotence. `--fix` adds the entry. |
| `eslint-ignore` | Each browser app receiving generated TypeScript ignores it in its eslint flat config (`ignores: ['**/zz_gen_*.ts']`) or `.eslintignore`. Generated shapes trip stylistic rules, and the output is not the developer's to change. |
| `package-manager` | Every browser app carries the same kind of lockfile (bun, npm, yarn, or pnpm), and the process files and package scripts invoke that tool and no other. Two tools in one repository means two lockfiles drifting apart. |
| `rpc-execute` | Every generated RPC handler calls the method's `Execute`. A handler the generator could not type-check decodes and returns without running the method. |
| `multi-site` | In a multi-site application, every generator reads the one schema and the shared generator's TypeScript reaches every site's browser app. |
| `env-template` | Every `env` struct tag without a default appears in the development environment template (`.envrc.template`, `.env.template`, or `.env.example`). `--fix` adds the missing lines. |
| `pins` | Framework pins in `go.mod` are released versions. Pseudo-versions and local replaces warn but do not fail. |
| `gowork-off` | `GOWORK=off go build ./...` and `go vet ./...` succeed, so the pins in `go.mod` resolve without the workspace. |
| `regen` | `go generate ./...` reproduces the generated files on disk (content compared before and after, so it holds in untracked trees too). Needs the Spanner emulator and rewrites the working tree; `--skip-generate` leaves it out. |

Statuses: `PASS`, `FAIL`, `WARN` (reported, does not fail the run), `SKIP` (with the
reason).

## impulse render

`render` copies one embedded skeleton into a new or empty directory under the module
path you name, rewriting every import and `go.mod` to it. It is the primitive `new` will
build on and the way the templates are validated: render one, then build, test, and
`impulse check` the result.

```sh
impulse render solo ../beacon --module example.com/acme/beacon
impulse render sites ../harbor --module example.com/acme/harbor --dev-root ~/Development/github.com/cccteam
```

`--dev-root` names a directory of cccteam checkouts laid out by repository
(`<root>/ccc/resource`, `<root>/session`, ...). The rendering then writes a `go.work`
using every framework module the application requires directly that has a checkout there,
so it builds against local framework work instead of the pins; indirect requirements
stay pinned, since a checkout of a library's own dependency can lag what the library needs. That `go.work` is for
development only; never commit it.

## Templates

The application skeletons `new` and `add` will render live under
`internal/skeleton/_candidates/`, one directory per shape (`solo`, `tenanted`, `outlets`,
`sites`), and are embedded in the binary, so a release ships exactly the trees it was
tested with and renders them offline.

While embedded they are not Go modules: each carries its `go.mod` as `go.mod.tmpl`, and
the leading underscore keeps the tree out of `./...` so nothing compiles it in place.
Rendering writes `go.mod` back under the target module path and rewrites every import.
The templates are therefore validated by rendering them and running the rendered
application's build, tests, and `impulse check`, never in place. Their browser workspaces
use bun, with `bun.lock` committed. Two yalc-era settings travel with them until
`@cccteam/resource` is published: an `overrides` entry in package.json that points
ccc-lib's peer dependency on the client at the yalc link, and `peer = false` in
bunfig.toml, because bun would otherwise install a second copy of the client beneath
ccc-lib and TypeScript would see two declarations of every client type. Every peer an
application needs is therefore a direct dependency. Build products
(`node_modules`, `.angular`, `dist`, `.ccc-cache`, `.yalc`, `go.work`) are never embedded;
`internal/skeleton`'s tests enforce that.

## Development

```sh
go test ./...
golangci-lint-v2 run
```

Fixture applications for the tests live under `internal/app/testdata/`. They are
synthetic: a lighthouse-keeping single-site app, a harbor multi-site app, and a generator
program the tool cannot read.
