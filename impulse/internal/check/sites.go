package check

import (
	"context"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/impulse/internal/app"
)

// sitesWired verifies that every site of a multi-site application is served and
// provisioned: a main package under its directory, a process in the process file running
// it on a port of its own, and a role migration whose collection knows the site's router. The multi-site check holds the generators' agreements; this one
// holds what runs them.
type sitesWired struct{}

func (sitesWired) Name() string { return "sites-wired" }

func (sitesWired) Describe() string {
	return "every site has a main package, a process on its own port, and a role migration covering its router"
}

// portAssignRE finds a PORT assignment on a process line.
var portAssignRE = regexp.MustCompile(`\bPORT=(\d+)`)

func (c sitesWired) Run(_ context.Context, env *Env) Result {
	a := env.App
	p := a.Profile()
	if len(p.Sites) == 0 {
		return skip(c.Name(), "no site generator")
	}
	if p.Layout != app.LayoutSites {
		return skip(c.Name(), "flat layout: one site")
	}

	lines, err := processLines(a.Root)
	if err != nil {
		return fail(c.Name(), err.Error())
	}

	var details, served []string
	ports := map[string][]string{} // "file PORT" -> sites
	for i := range p.Sites {
		site := &p.Sites[i]
		if !slices.Contains(a.MainPackages, site.Dir) {
			details = append(details, fmt.Sprintf("site %s has no main package in %s; nothing serves it", site.Name, site.Dir))
		}
		line, ok := serving(lines, site.Dir)
		if !ok {
			details = append(details, fmt.Sprintf("no process file runs site %s (expected a process running go run ./%s)", site.Name, site.Dir))

			continue
		}
		port := ""
		if m := portAssignRE.FindStringSubmatch(line.text); m != nil {
			port = m[1]
			key := line.file + " PORT=" + port
			ports[key] = append(ports[key], site.Name)
		}
		served = append(served, site.Name+" (:"+port+")")
	}
	for _, key := range sortedKeysOf(ports) {
		if sites := ports[key]; len(sites) > 1 {
			file, port, _ := strings.Cut(key, " ")
			details = append(details, fmt.Sprintf("sites %s all listen on %s in %s", strings.Join(sites, " and "), port, file))
		}
	}

	covered, err := c.roleCoverage(a, p)
	if err != nil {
		return fail(c.Name(), err.Error())
	}
	details = append(details, covered...)

	if len(details) > 0 {
		return fail(c.Name(), fmt.Sprintf("%d site wiring problem(s)", len(details)), details...)
	}

	return pass(c.Name(), fmt.Sprintf("%d site(s) wired: %s; every site's router is covered by a role migration", len(p.Sites), strings.Join(served, ", ")))
}

// roleCoverage checks that every site's router package is imported by some package that
// calls access.MigrateRoles: the collection roles are reconciled against must know the
// site's resources, and a router no migrating package imports is in none of them. One
// migration may cover every site (the union collection) or each user pool may run its
// own over the sites it binds; either way every site is covered by one.
func (sitesWired) roleCoverage(a *app.App, p app.Profile) ([]string, error) {
	if len(a.RoleMigrations) == 0 {
		return nil, nil // tenancy-wired reports a tenanted application without one
	}
	modulePath := ""
	if a.GoMod != nil && a.GoMod.Module != nil {
		modulePath = a.GoMod.Module.Mod.Path
	}
	imports := map[string]bool{}
	var dirs []string
	for _, m := range a.RoleMigrations {
		dir := path.Dir(m.File)
		if slices.Contains(dirs, dir) {
			continue
		}
		dirs = append(dirs, dir)
		found, err := packageImports(a, dir)
		if err != nil {
			return nil, err
		}
		for imp := range found {
			imports[imp] = true
		}
	}

	var details []string
	for i := range p.Sites {
		site := &p.Sites[i]
		routes := site.Generator.RoutesDir()
		if routes == "" {
			continue
		}
		want := modulePath + "/" + routes
		if !imports[want] {
			details = append(details, fmt.Sprintf("no role migration covers site %s: %s is not imported by any package calling access.MigrateRoles (%s)", site.Name, want, strings.Join(dirs, ", ")))
		}
	}

	return details, nil
}

// processLine is one line of a process file.
type processLine struct {
	file string
	line int
	text string
}

// processLines reads every line of the process files at the application root.
func processLines(root string) ([]processLine, error) {
	files, err := processFiles(root)
	if err != nil {
		return nil, err
	}
	var lines []processLine
	for _, file := range files {
		data, err := os.ReadFile(filepath.Join(root, file))
		if err != nil {
			return nil, errors.Wrap(err, "os.ReadFile()")
		}
		for i, text := range strings.Split(string(data), "\n") {
			lines = append(lines, processLine{file: file, line: i + 1, text: text})
		}
	}

	return lines, nil
}

// serving finds the process line that runs the site's main package: a go run of the
// site directory, its main.go, or its import path.
func serving(lines []processLine, siteDir string) (processLine, bool) {
	for _, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l.text), "#") {
			continue
		}
		for _, target := range goRunTargets(l.text) {
			if target == siteDir {
				return l, true
			}
		}
	}

	return processLine{}, false
}

// goRunTargets returns the root-relative directories a line's `go run` invocations
// target, for relative targets.
func goRunTargets(text string) []string {
	var targets []string
	fields := strings.Fields(text)
	for i := 0; i+2 < len(fields); i++ {
		if fields[i] != "go" || fields[i+1] != "run" {
			continue
		}
		j := i + 2
		for j < len(fields) && strings.HasPrefix(fields[j], "-") {
			j++ // build flags such as --tags=...
		}
		if j == len(fields) {
			break
		}
		target := strings.Trim(fields[j], `'";&|)`)
		if strings.HasSuffix(target, ".go") {
			target = path.Dir(target)
		}
		if target == "." || strings.HasPrefix(target, "./") || !strings.Contains(target, ".") {
			targets = append(targets, path.Clean(target))
		}
	}

	return targets
}

// packageImports returns the import paths of the non-test Go files in a root-relative
// directory.
func packageImports(a *app.App, dir string) (map[string]bool, error) {
	entries, err := os.ReadDir(a.Abs(dir))
	if err != nil {
		return nil, errors.Wrap(err, "os.ReadDir()")
	}
	imports := map[string]bool{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		rel := path.Join(dir, name)
		src, err := os.ReadFile(a.Abs(rel))
		if err != nil {
			return nil, errors.Wrap(err, "os.ReadFile()")
		}
		f, err := parser.ParseFile(token.NewFileSet(), rel, src, parser.ImportsOnly)
		if err != nil {
			return nil, errors.Wrap(err, "parser.ParseFile()")
		}
		for _, imp := range f.Imports {
			if p, err := strconv.Unquote(imp.Path.Value); err == nil {
				imports[p] = true
			}
		}
	}

	return imports, nil
}

func sortedKeysOf[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)

	return keys
}
