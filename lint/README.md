# github.com/cccteam/ccc/lint

## Setup

1. Install the ccclint utility.

```sh
go install github.com/cccteam/ccc/lint/cmd/ccclint@latest
```

2. Make sure your `$GOPATH/bin` is included in your `$PATH`.

3. Run the ccclint utility:

```sh
ccclint
```

4. Add a `custom` section to the `linters-settings` section of the project's `.golangci.yml` as shown below.

```yml
linters-settings:
  ...

  custom:
    ccclint:
      type: 'module'
      description: CCC custom linter
```

5. Update your project to use `golang-ci.yml@v5.4.0` or greater and make sure golangci-lint-version is 'v1.64' or later. You can also set the ccclint-version.

```yml
golang-ci:
  needs: env-setup
  uses: cccteam/github-workflows/.github/workflows/golang-ci.yml@v5.4.0
  with:
    build-tags: '["", "dev"]'
    golangci-lint-version: 'v1.64'
    ccclint-version: v0.0.3
```

6. Update your project's readme with the steps to install the lint plugin:
   Note: This should only need to be done at most every time you change your go version.

```sh
go install github.com/cccteam/ccc/lint/cmd/ccclint@latest
ccclint
```
