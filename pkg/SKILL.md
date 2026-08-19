---
name: pkg
description: Use the github.com/cccteam/ccc/pkg package when a devtool or code generator needs to discover the enclosing Go module — its module path and the directory containing go.mod — from the current working directory. Reach for it whenever code must chdir to the module root, resolve the module path at runtime, or locate go.mod by walking up parent directories.
---

# pkg

A single-purpose helper: answer "what Go module am I running inside, and where is
its root?" It walks up from the current working directory until it finds a
`go.mod`, then reads the `module` directive. Its in-repo consumer is the
`resource/generation` code generator, which uses it to chdir to the module root
before generating files.

## API

```go
type Information struct {
    AbsolutePath string // directory containing the found go.mod
    PackageName  string // the module path from the module directive
}

func Info() (*Information, error)
```

Naming trap: `PackageName` holds the **module path** (e.g.
`github.com/cccteam/ccc/resource`), not a Go package name.

## Usage

```go
pkgInfo, err := pkg.Info()
if err != nil {
    return errors.Wrap(err, "pkg.Info()")
}
if err := os.Chdir(pkgInfo.AbsolutePath); err != nil {
    return errors.Wrap(err, "os.Chdir()")
}
```

## Behavior & gotchas

- Resolution is relative to `os.Getwd()` at call time, **not** the caller's
  source location. In this monorepo it finds the *nearest* enclosing module —
  running from `cache/` yields `github.com/cccteam/ccc/cache`, not the root.
- The go.mod scan is naive (line-prefix match on `module`), not
  `golang.org/x/mod/modfile` — fine for standard files, but an indented directive
  would be missed.
- The upward walk terminates at literal `/`, so treat it as Unix-only.
- Errors are plain messages with no sentinels: "reached root and did not find
  go.mod", "failed to find module directive in go.mod", etc.
