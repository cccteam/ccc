package app

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// RunsGenerator reports whether a //go:generate directive runs the generator program:
// `go run` of the program's own directory, relative to the directive's file or as an
// import path under the module, or `go run` of a main package in the module that imports
// the program's package. The second is the layout an application takes when tests run
// the declaration in-process (a main package cannot be imported), so the declaration
// lives in a package of its own and the runner beside it calls it; go generate runs the
// program through the runner.
func (a *App) RunsGenerator(g *Generator) (Directive, bool) {
	want := path.Dir(g.File)
	for _, d := range a.GoGenerate {
		target, ok := a.directiveTarget(d)
		if !ok {
			continue
		}
		if target == want || a.mainPackageImports(target, want) {
			return d, true
		}
	}

	return Directive{}, false
}

// directiveTarget resolves the directory a `go run` directive runs, root-relative and
// cleaned: a path relative to the directive's file, or an import path under the module.
// Anything else (another module's program, a bare name go run would not accept, a
// command that is not go run) resolves to nothing.
func (a *App) directiveTarget(d Directive) (string, bool) {
	fields := strings.Fields(d.Command)
	if len(fields) < 3 || fields[0] != "go" || fields[1] != "run" {
		return "", false
	}
	target := fields[2]
	if strings.HasSuffix(target, ".go") {
		target = path.Dir(target)
	}
	switch {
	case target == "." || strings.HasPrefix(target, "./") || strings.HasPrefix(target, "../"):
		target = path.Join(path.Dir(d.File), target)
	case a.GoMod != nil && a.GoMod.Module != nil && strings.HasPrefix(target, a.GoMod.Module.Mod.Path+"/"):
		target = strings.TrimPrefix(target, a.GoMod.Module.Mod.Path+"/")
	default:
		return "", false
	}

	return path.Clean(target), true
}

// ProgramDir is the root-relative directory go run runs the generator program from: the
// target of the //go:generate directive that runs it, when one does, since a program
// declared in a package of its own runs through the main package beside it, and the
// program's own directory otherwise.
func (a *App) ProgramDir(g *Generator) string {
	if d, ok := a.RunsGenerator(g); ok {
		if target, ok := a.directiveTarget(d); ok {
			return target
		}
	}

	return path.Dir(g.File)
}

// readsWarnings reports whether the program reads Warnings(): a call with no arguments
// on any receiver in the program's own directory or in a main package of the module
// that imports the program's package, test files left out.
func (a *App) readsWarnings(g *Generator) bool {
	pkgDir := path.Dir(g.File)
	dirs := []string{pkgDir}
	for _, dir := range a.MainPackages {
		if dir != pkgDir && a.mainPackageImports(dir, pkgDir) {
			dirs = append(dirs, dir)
		}
	}
	for _, dir := range dirs {
		if a.dirCallsWarnings(dir) {
			return true
		}
	}

	return false
}

// dirCallsWarnings reports whether a non-test Go file in the directory calls Warnings()
// on something.
func (a *App) dirCallsWarnings(dir string) bool {
	entries, err := os.ReadDir(a.Abs(dir))
	if err != nil {
		return false
	}
	fset := token.NewFileSet()
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(a.Abs(dir), name), nil, parser.SkipObjectResolution)
		if err != nil {
			continue
		}
		found := false
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) != 0 {
				return !found
			}
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == warningsMethod {
				found = true
			}

			return !found
		})
		if found {
			return true
		}
	}

	return false
}

// warningsMethod is the generator method a program reads after a clean run.
const warningsMethod = "Warnings"

// mainPackageImports reports whether dir holds a main package of the module whose
// non-test Go files import the module package at pkgDir. A file that does not parse
// imports nothing.
func (a *App) mainPackageImports(dir, pkgDir string) bool {
	if a.GoMod == nil || a.GoMod.Module == nil || !slices.Contains(a.MainPackages, dir) {
		return false
	}
	importPath := a.GoMod.Module.Mod.Path
	if pkgDir != "." {
		importPath += "/" + pkgDir
	}

	entries, err := os.ReadDir(a.Abs(dir))
	if err != nil {
		return false
	}
	fset := token.NewFileSet()
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(a.Abs(dir), name), nil, parser.ImportsOnly)
		if err != nil {
			continue
		}
		for _, imp := range f.Imports {
			if p, err := strconv.Unquote(imp.Path.Value); err == nil && p == importPath {
				return true
			}
		}
	}

	return false
}
