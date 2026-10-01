package check

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/go-playground/errors/v5"
)

// maintenanceSwitch verifies that every site's main checks maintenance.Requested() before
// it builds the site configuration: the deploy pipeline starts a maintenance revision of
// the application's own image, with the maintenance variable set, before a release that
// replaces or interrupts the database, and that revision must open no database client, no
// session store and no secret. A main that builds the configuration first would start
// against the database while the migration runs; the pipeline's probe stops the run, but
// the check finds it before anything deploys.
type maintenanceSwitch struct{}

// maintenanceSwitchName is the check's name.
const maintenanceSwitchName = "maintenance-switch"

// The import path of the maintenance package, the call that reads the switch and the
// call that builds the site configuration.
const (
	maintenancePath   = "github.com/cccteam/ccc/resource/maintenance"
	requestedCall     = "Requested"
	siteConfiguration = "NewSiteConfiguration"
)

func (maintenanceSwitch) Name() string { return maintenanceSwitchName }

func (maintenanceSwitch) Describe() string {
	return "every site's main checks maintenance.Requested() before it builds the site configuration, so a maintenance revision opens no database"
}

func (c maintenanceSwitch) Run(_ context.Context, env *Env) Result {
	a := env.App
	profile := a.Profile()
	if len(profile.Sites) == 0 {
		return skip(c.Name(), "no site: no main to check")
	}
	var problems, unsure []string
	checked := 0
	for i := range profile.Sites {
		site := &profile.Sites[i]
		main, err := mainFunctions(filepath.Join(a.Root, filepath.FromSlash(site.Dir)))
		if err != nil {
			return fail(c.Name(), fmt.Sprintf("site %s: %v", site.Name, err))
		}
		if main == nil {
			continue // sites-wired reports a site with no main package
		}
		checked++
		switch {
		case main.configured == token.NoPos:
			unsure = append(unsure, fmt.Sprintf("%s: no call to config.%s found in the main package; the maintenance switch goes before the site configuration is built, wherever that is", main.file, siteConfiguration))
		case main.requested == token.NoPos:
			problems = append(problems, fmt.Sprintf("%s: the main builds the site configuration without checking maintenance.%s() first; add, before config.%s(ctx): if maintenance.%s() { return maintenance.Serve(ctx) } (import %s)", main.file, requestedCall, siteConfiguration, requestedCall, maintenancePath))
		case main.requested > main.configured:
			problems = append(problems, fmt.Sprintf("%s: maintenance.%s() is checked after config.%s() builds the configuration, so a maintenance revision would open the database; move the check before it", main.file, requestedCall, siteConfiguration))
		}
	}
	if len(problems) > 0 {
		return fail(c.Name(), fmt.Sprintf("%d main(s) without the maintenance switch before the site configuration", len(problems)), append(problems, unsure...)...)
	}
	summary := fmt.Sprintf("%d main(s) check maintenance.%s() before building the site configuration", checked, requestedCall)
	if len(unsure) > 0 {
		return warn(c.Name(), summary+fmt.Sprintf("; %d could not be read for it", len(unsure)), unsure...)
	}

	return pass(c.Name(), summary)
}

// mainCalls is what a site's main package says about the switch: the file holding the
// site configuration call (or the first main file), and the positions of the two calls in
// the function that builds the configuration. A call that is absent is token.NoPos.
type mainCalls struct {
	file       string
	requested  token.Pos
	configured token.Pos
}

// mainFunctions reads the Go files of the directory and, when they form a package main,
// finds the function that calls config.NewSiteConfiguration and, in it, the call to
// maintenance.Requested. Nil when the directory holds no package main.
func mainFunctions(dir string) (*mainCalls, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}

		return nil, errors.Wrap(err, "os.ReadDir()")
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".go") && !strings.HasSuffix(e.Name(), "_test.go") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	fset := token.NewFileSet()
	var found *mainCalls
	for _, name := range names {
		file, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, errors.Wrapf(err, "parsing %s", name)
		}
		if file.Name.Name != "main" {
			continue
		}
		rel := path.Join(filepath.ToSlash(filepath.Base(dir)), name)
		if found == nil {
			found = &mainCalls{file: rel}
		}
		alias := importName(file, maintenancePath)
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			configured := firstCall(fn.Body, "", siteConfiguration)
			if configured == token.NoPos {
				continue
			}
			found.file, found.configured = rel, configured
			if alias != "" {
				found.requested = firstCall(fn.Body, alias, requestedCall)
			}

			return found, nil
		}
	}

	return found, nil
}

// importName is the name the file imports the path under: the alias, else the path's last
// element; empty when the file does not import it.
func importName(file *ast.File, importPath string) string {
	for _, imp := range file.Imports {
		if strings.Trim(imp.Path.Value, `"`) != importPath {
			continue
		}
		if imp.Name != nil {
			return imp.Name.Name
		}

		return path.Base(importPath)
	}

	return ""
}

// firstCall is the position of the first call pkg.sel(...) in the body (any package when
// pkg is empty), or token.NoPos.
func firstCall(body *ast.BlockStmt, pkg, sel string) token.Pos {
	found := token.NoPos
	ast.Inspect(body, func(n ast.Node) bool {
		if found != token.NoPos {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != sel {
			return true
		}
		if ident, ok := selector.X.(*ast.Ident); ok && (pkg == "" || ident.Name == pkg) {
			found = call.Pos()

			return false
		}

		return true
	})

	return found
}
