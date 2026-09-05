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
| `emulator-version` | The generator option, the process files' image tags, and the test harnesses name one Spanner emulator version. |
| `prettier-ignore` | Each browser app's `.prettierignore` excludes the generated TypeScript. Prettier reflowing generated files breaks generate idempotence. `--fix` adds the entry. |
| `rpc-execute` | Every generated RPC handler calls the method's `Execute`. A handler the generator could not type-check decodes and returns without running the method. |
| `multi-site` | In a multi-site application, every generator reads the one schema and the shared generator's TypeScript reaches every site's browser app. |
| `env-template` | Every `env` struct tag without a default appears in the development environment template (`.envrc.template`, `.env.template`, or `.env.example`). `--fix` adds the missing lines. |
| `pins` | Framework pins in `go.mod` are released versions. Pseudo-versions and local replaces warn but do not fail. |
| `gowork-off` | `GOWORK=off go build ./...` and `go vet ./...` succeed, so the pins in `go.mod` resolve without the workspace. |
| `regen` | `go generate ./...` reproduces the generated files on disk (content compared before and after, so it holds in untracked trees too). Needs the Spanner emulator and rewrites the working tree; `--skip-generate` leaves it out. |

Statuses: `PASS`, `FAIL`, `WARN` (reported, does not fail the run), `SKIP` (with the
reason).

## Templates

The application skeletons `new` and `add` will render live under
`internal/skeleton/_candidates/`, one directory per shape (`solo`, `tenanted`, `outlets`,
`sites`), and are embedded in the binary, so a release ships exactly the trees it was
tested with and renders them offline.

While embedded they are not Go modules: each carries its `go.mod` as `go.mod.tmpl`, and
the leading underscore keeps the tree out of `./...` so nothing compiles it in place.
Rendering writes `go.mod` back under the target module path and rewrites every import.
The templates are therefore validated by rendering them and running the rendered
application's build, tests, and `impulse check`, never in place. Build products
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
