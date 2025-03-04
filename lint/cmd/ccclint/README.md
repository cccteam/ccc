# ccclint

`ccclint` is a command-line utility for building and installing a custom `golangci-lint` with the CCC custom linter built into it.

## Features

- Installs a specific version of `golangci-lint`.
- Clones the `ccc` repository and checks out the specified plugin version.
- Builds a custom `golangci-lint` with the custom linter and installs it in your `$GOPATH/bin`.
- Supports verbose output for debugging.

## Installation

To install `ccclint`, run the following command:

```sh
go install github.com/cccteam/ccc/lint/cmd/ccclint@latest
```

## Usage

Run the `ccclint` utility:

```sh
ccclint
```

### Command-Line Flags

- `-g, --golangci-lint-version`: Specify the version of `golangci-lint` to install (default: `v1.64`).
- `-p, --plugin-version`: Specify the version of the `ccc/lint` plugin to use.
- `-v, --verbose`: Enable verbose output.
- `-h, --help`: Print usage information.
- `--version`: Print the version of `ccclint`.

### Example

```sh
ccclint --golangci-lint-version v1.64 --plugin-version v0.0.3 --verbose
```
