package check

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/cccteam/ccc/impulse/internal/app"
	"github.com/go-playground/errors/v5"
)

// resourceStyles verifies that every browser app depending on @cccteam/resource-angular
// imports the library's stylesheet from one of the global stylesheets its angular.json
// build names. The library's form and list components request classes from that sheet by
// name (the field grid, the read-only and edit-mode treatments, the sticky list header),
// so an app without the import renders them unstyled and stacks its section labels in a
// corner of the page.
type resourceStyles struct{}

func (resourceStyles) Name() string { return "resource-styles" }

func (resourceStyles) Describe() string {
	return "each browser app depending on @cccteam/resource-angular imports its stylesheet"
}

const (
	stylesPackage = "@cccteam/resource-angular"
	stylesImport  = "@use '" + stylesPackage + "/styles';"
)

// stylesImported matches the sass statements that load the library stylesheet.
var stylesImported = regexp.MustCompile(`@(?:use|import|forward)\s+['"]` + regexp.QuoteMeta(stylesPackage) + `/styles['"]`)

func (c resourceStyles) Run(_ context.Context, env *Env) Result {
	checked, missing, err := c.scan(env.App)
	if err != nil {
		return fail(c.Name(), err.Error())
	}
	if checked == 0 {
		return skip(c.Name(), "no browser app depends on "+stylesPackage)
	}
	if len(missing) == 0 {
		return pass(c.Name(), fmt.Sprintf("%d browser project(s) import the %s stylesheet", checked, stylesPackage))
	}
	details := make([]string, 0, len(missing)*2)
	sheets := map[string]bool{}
	for _, m := range missing {
		details = append(details, fmt.Sprintf("%s: project %s imports no %s/styles (add to %s: %s)", m.webDir, m.project, stylesPackage, m.sheet, stylesImport))
		sheets[m.sheet] = true
	}
	if !env.Fix {
		return fail(c.Name(), fmt.Sprintf("%d browser project(s) do not import the %s stylesheet (--fix adds the @use)", len(missing), stylesPackage), details...)
	}
	fixed := make([]string, 0, len(sheets))
	for sheet := range sheets {
		fixed = append(fixed, sheet)
	}
	sort.Strings(fixed)
	for _, sheet := range fixed {
		if err := prependLines(env.App.Abs(sheet), "// The @cccteam/resource-angular components' cross-cutting classes and variables.", stylesImport); err != nil {
			return fail(c.Name(), err.Error())
		}
		details = append(details, "fixed: "+sheet)
	}

	return passWithDetails(c.Name(), fmt.Sprintf("%d stylesheet(s) updated", len(fixed)), details...)
}

// missingImport is one browser project whose global stylesheets lack the import; sheet
// is the first of them, where --fix adds it.
type missingImport struct {
	webDir, project, sheet string
}

// scan visits every browser app that depends on the library and returns how many of its
// projects own a global stylesheet, and which of those import none of the library's.
func (resourceStyles) scan(a *app.App) (int, []missingImport, error) {
	checked := 0
	var missing []missingImport
	for _, w := range a.WebApps {
		depends, err := dependsOn(a.Abs(path.Join(w.Dir, "package.json")), stylesPackage)
		if err != nil {
			return 0, nil, err
		}
		if !depends {
			continue
		}
		projects, err := buildStyles(a.Abs(path.Join(w.Dir, "angular.json")))
		if err != nil {
			return 0, nil, err
		}
		names := make([]string, 0, len(projects))
		for name := range projects {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			local, imported, err := importedBy(a, w.Dir, projects[name])
			if err != nil {
				return 0, nil, err
			}
			if len(local) == 0 {
				continue // no global stylesheet of its own: nothing to import from
			}
			checked++
			if !imported {
				missing = append(missing, missingImport{webDir: w.Dir, project: name, sheet: path.Join(w.Dir, local[0])})
			}
		}
	}

	return checked, missing, nil
}

// importedBy returns the entries that are files inside the web app (package paths are
// not) and whether any of them loads the library stylesheet.
func importedBy(a *app.App, webDir string, entries []string) (local []string, imported bool, err error) {
	for _, entry := range entries {
		abs := a.Abs(path.Join(webDir, entry))
		data, err := os.ReadFile(abs)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, false, errors.Wrap(err, "os.ReadFile()")
		}
		local = append(local, entry)
		if stylesImported.Match(data) {
			imported = true
		}
	}

	return local, imported, nil
}

// dependsOn reports whether the package.json at abs names the package among its
// dependencies or devDependencies; an absent manifest depends on nothing.
func dependsOn(abs, pkg string) (bool, error) {
	data, err := os.ReadFile(abs)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, errors.Wrap(err, "os.ReadFile()")
	}
	var manifest struct {
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return false, errors.Wrapf(err, "json.Unmarshal(): %s", abs)
	}
	_, dep := manifest.Dependencies[pkg]
	_, dev := manifest.DevDependencies[pkg]

	return dep || dev, nil
}

// buildStyles returns, per project in the angular.json at abs, the global stylesheets
// its build target names (string entries and {input} objects alike), relative to the
// workspace directory. Projects without a build target contribute nothing.
func buildStyles(abs string) (map[string][]string, error) {
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, errors.Wrap(err, "os.ReadFile()")
	}
	var workspace struct {
		Projects map[string]struct {
			Architect map[string]struct {
				Options struct {
					Styles []json.RawMessage `json:"styles"`
				} `json:"options"`
			} `json:"architect"`
		} `json:"projects"`
	}
	if err := json.Unmarshal(data, &workspace); err != nil {
		return nil, errors.Wrapf(err, "json.Unmarshal(): %s", abs)
	}
	styles := map[string][]string{}
	for name, project := range workspace.Projects {
		build, ok := project.Architect["build"]
		if !ok {
			continue
		}
		for _, raw := range build.Options.Styles {
			var entry string
			if err := json.Unmarshal(raw, &entry); err != nil {
				var object struct {
					Input string `json:"input"`
				}
				if err := json.Unmarshal(raw, &object); err != nil {
					return nil, errors.Wrapf(err, "json.Unmarshal(): %s styles entry", abs)
				}
				entry = object.Input
			}
			if entry != "" {
				styles[name] = append(styles[name], entry)
			}
		}
	}

	return styles, nil
}

// prependLines writes a comment and lines at the top of the file, before its current
// content; a sass @use must precede every other rule.
func prependLines(abs, comment string, lines ...string) error {
	existing, err := os.ReadFile(abs)
	if err != nil {
		return errors.Wrap(err, "os.ReadFile()")
	}
	var b strings.Builder
	if comment != "" {
		b.WriteString(comment + "\n")
	}
	for _, l := range lines {
		b.WriteString(l + "\n")
	}
	if len(existing) > 0 {
		b.WriteString("\n")
		b.Write(existing)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return errors.Wrap(err, "os.Stat()")
	}
	if err := os.WriteFile(abs, []byte(b.String()), info.Mode().Perm()); err != nil {
		return errors.Wrap(err, "os.WriteFile()")
	}

	return nil
}
