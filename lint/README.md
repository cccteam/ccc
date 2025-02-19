# github.com/cccteam/ccc/lint

## Setup

1. Make sure your project has `/tmp` in the `.gitignore` file.

2. Add a `custom` section to the `linters-settings` section of the project's `.golangci.yml` as shown below. Replace `v0.0.3` with the version of the linter you want to use.

```yml
linters-settings:
  custom:
    ccclint:
      path: tmp/ccc-lint/v0.0.3/ccc-lint.so
      original-url: github.com/cccteam/ccc
```

3. Install the ccclint utility.

```sh
go install github.com/cccteam/ccc/lint/cmd/ccclint@latest
```

4. Run the ccclint utility from the root directory of your project. If your `$GOPATH/bin` is set up in your `$PATH`, you can now just simply run it:

```sh
ccclint
```

5. Update your project to use `golang-ci.yml@v5.4.0` or greater. Also make sure golangci-lint-version is 'v1.64' or later.

```yml
golang-ci:
  needs: env-setup
  uses: cccteam/github-workflows/.github/workflows/golang-ci.yml@v5.4.0
  with:
    build-tags: '["", "dev"]'
    golangci-lint-version: 'v1.64'
```

6. Update your project's readme with the steps to install the lint plugin:

```
go install github.com/cccteam/ccc/lint/cmd/ccclint@latest
ccclint
```
