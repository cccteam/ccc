# install-ccc-linter

`install-ccc-linter` is a command-line utility for building and installing a custom `golangci-lint-v2` with the CCC custom linter built into it.

## Features

- Generates a custom `.custom-gcl.yml` configuration file from an embedded template (`custom-gcl.yml.tmpl`).
- Builds a custom `golangci-lint-v2` with the custom linter and installs it in your `$GOPATH/bin`.
- Supports specifying both plugin version and golangci-lint version.
- Automatically detects the latest stable golangci-lint version if not specified.
- Supports verbose output for debugging.
- Uses Go's embed feature to include the configuration template at compile time.

## Installation

To install `install-ccc-linter`, run the following command:

```sh
go install github.com/cccteam/ccc/lint/cmd/install-ccc-linter@latest
```

## Usage

Run the `install-ccc-linter` utility:

```sh
install-ccc-linter
```

### Command-Line Flags

- `-p, --plugin-version`: Specify the version of the `ccc/lint` plugin to use.
- `-g, --golangci-lint-version`: Specify the version of golangci-lint to use (default: latest stable).
- `-v, --verbose`: Enable verbose output.
- `-h, --help`: Print usage information.
- `--version`: Print the version of `install-ccc-linter`.

### Examples

```sh
# Install with specific plugin version
install-ccc-linter -p v0.0.3 -v

# Install with specific golangci-lint version
install-ccc-linter -g v2.5.0

# Install with both versions specified
install-ccc-linter -p v0.0.3 -g v2.5.0 -v
```
