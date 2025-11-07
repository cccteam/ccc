# github.com/cccteam/ccc/lint

This package provides custom linting rules for CCC projects. The `install-ccc-linter` utility builds and installs a custom `golangci-lint` binary with CCC's custom linters integrated.

## Prerequisites

- Make sure golangci-lint is installed, but VS Code should already have this taken care of

## Setup

1. Install the install-ccc-linter utility.

```sh
go install github.com/cccteam/ccc/lint/cmd/install-ccc-linter@latest
```

2. Make sure your `$GOPATH/bin` is included in your `$PATH`.

3. Run the install-ccc-linter utility:

```sh
install-ccc-linter
```

You can also specify specific versions if needed:

```sh
# Specify plugin version
install-ccc-linter --plugin-version v0.0.3

# Specify both plugin and golangci-lint versions
install-ccc-linter --plugin-version v0.0.3 --golangci-lint-version v2.5.0
```

4. Add a `custom` section to the `linters-settings` section of the project's `.golangci.yml` as shown below.

```yml
linters:
  ...
  settings:
    ...
    custom:
      ccclint:
        type: module
        description: CCC custom linter
```

5. Update your project to use `golang-ci.yml@v5.13.0` or greater and make sure golangci-lint-version is 'v2.4' or later. You can also set the ccclint-version.

```yml
golang-ci:
  uses: cccteam/github-workflows/.github/workflows/golang-ci.yml@v5.13.0
  with:
    build-tags: '["", "dev"]'
    golangci-lint-version: 'v2.4'
    ccclint-version: 'v0.0.3'
```

6. Ensure VSCode is configured to use the linter `golangci-lint-v2`.

7. Update your project's readme with the steps to install the lint plugin:
   Note: This should only need to be done at most every time you change your go version.

```sh
go install github.com/cccteam/ccc/lint/cmd/install-ccc-linter@latest
install-ccc-linter
```
