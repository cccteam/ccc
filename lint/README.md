# github.com/cccteam/ccc/lint

## Setup

1. Add a `custom` section to the `linters-settings` section of the project's `.golangci.yml` as such:

```yml
linters-settings:
  custom:
    ccclint:
      path: /lint/ccc-lint.so
      description: The description of the linter
      original-url: github.com/cccteam/ccc
```

2. Add `ccclint` to `linters.enable` in the project's `.golangci.yml`.

```yml
linters:
  disable-all: true
  enable:
    - ccclint # Customer linter for CCC projects
```
