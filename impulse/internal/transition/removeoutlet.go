package transition

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/impulse/internal/app"
	"github.com/cccteam/ccc/impulse/internal/check"
)

// RemoveOutlet removes a router outlet: its declaration and, for a session outlet, its
// generated client and browser project, from every site that declares it. The router
// group that mounted it, the App's handlers for it, its configuration, its tests, and the
// members that were only on it are the agent's.
type RemoveOutlet struct {
	// Name is the outlet's name, as WithRouterOutlet declares it.
	Name string
}

// Command is the impulse command line for the transition.
func (r RemoveOutlet) Command() string {
	return "impulse remove outlet " + r.Name
}

// Validate checks that a site declares the outlet before anything is changed.
func (r RemoveOutlet) Validate(a *app.App) error {
	if !outletNameRE.MatchString(r.Name) || r.Name == defaultOutletName {
		return errors.Newf("outlet name %q: name a declared outlet; the default outlet is GenerateRoutes itself and is not removed", r.Name)
	}
	if len(r.declaring(a)) == 0 {
		return errors.Newf("no generator program declares an outlet named %s (WithRouterOutlet)", r.Name)
	}

	return nil
}

// defaultOutletName is the reserved name of the outlet GenerateRoutes declares.
const defaultOutletName = "default"

// declaring lists the sites whose generator declares the outlet.
func (r RemoveOutlet) declaring(a *app.App) []app.Site {
	var sites []app.Site
	p := a.Profile()
	for i := range p.Sites {
		for _, o := range p.Sites[i].Outlets {
			if o.Name == r.Name {
				sites = append(sites, p.Sites[i])

				break
			}
		}
	}

	return sites
}

// Apply makes the deterministic half: the generator program loses the outlet and its
// client target; the outlet's browser project is deleted and taken out of the workspace,
// the package scripts, and the Procfile; @outlet lists that name the outlet beside others
// drop it; and go generate runs. What names the outlet in the router, the App, the
// configuration, the tests, and the members that were only on it are the agent's.
func (r RemoveOutlet) Apply(ctx context.Context, a *app.App, exec check.Execer) (*Change, error) {
	if err := r.Validate(a); err != nil {
		return nil, err
	}
	ch := &Change{Command: r.Command()}
	for _, site := range r.declaring(a) {
		g := site.Generator
		var targets []app.TSTarget
		for _, t := range g.TypescriptTargets() {
			if t.Outlet == r.Name {
				targets = append(targets, t)
			}
		}
		if err := r.editProgram(a, g, targets, ch); err != nil {
			return nil, err
		}
		for _, t := range targets {
			if err := r.removeProject(a, g, t, ch); err != nil {
				return nil, err
			}
		}
	}
	if err := r.detachMembers(a, ch); err != nil {
		return nil, err
	}
	r.noteEnvironment(a, ch)

	if out, err := exec.Run(ctx, a.Root, nil, "go", "generate", "./..."); err != nil {
		ch.skipf("go generate ./... failed, so the %s outlet's generated routes and handlers are still on disk; fix the cause (a resource still annotated @outlet(%s), most often) and run it:\n%s", r.Name, r.Name, strings.TrimSpace(string(out)))
	} else {
		ch.didf("ran go generate ./..., which regenerated without the %s outlet", r.Name)
	}

	return ch, nil
}

// editProgram removes WithRouterOutlet and every GenerateTypescript target for the outlet
// from the generator program.
func (r RemoveOutlet) editProgram(a *app.App, g *app.Generator, targets []app.TSTarget, ch *Change) error {
	src, mode, err := readFile(a, g.File)
	if err != nil {
		return err
	}
	edited, n, err := app.RemoveOptions(g.File, src, "WithRouterOutlet", r.Name)
	if err != nil {
		return err
	}
	if n == 0 {
		return errors.Newf("%s: WithRouterOutlet(%q, ...) was read but not found to remove", g.File, r.Name)
	}
	ch.didf("%s: removed WithRouterOutlet(%q, ...)", g.File, r.Name)
	for _, t := range targets {
		edited, n, err = app.RemoveOptions(g.File, edited, "GenerateTypescript", t.Dir)
		if err != nil {
			return err
		}
		if n > 0 {
			ch.didf("%s: removed GenerateTypescript(%q, ForOutlet(%q), ...)", g.File, t.Dir, r.Name)
		}
	}
	if err := os.WriteFile(a.Abs(g.File), edited, mode); err != nil {
		return errors.Wrap(err, "os.WriteFile()")
	}

	return nil
}

// removeProject deletes the browser project the outlet's client was generated into and
// takes it out of angular.json, the package scripts, and the Procfile. A client directory
// outside any project loses its generated files only. Each edit that cannot be made is
// recorded as skipped.
func (r RemoveOutlet) removeProject(a *app.App, g *app.Generator, target app.TSTarget, ch *Change) error {
	w, ok := a.WebAppFor(target.Dir)
	if !ok {
		return r.removeGenerated(a, target.Dir, ch)
	}
	projects, err := a.ReadAngular(w.Dir)
	if err != nil {
		return err
	}
	rel := strings.TrimPrefix(strings.TrimPrefix(target.Dir, w.Dir), "/")
	project, ok := app.ProjectFor(projects, rel)
	if !ok || project.Root == "" || project.Root == "." {
		ch.skipf("%s/angular.json has no project rooted at a directory holding %s, so the %s outlet's browser project was not deleted; remove it by hand", w.Dir, rel, r.Name)

		return r.removeGenerated(a, target.Dir, ch)
	}
	if dt := defaultTarget(g); dt != nil {
		if dp, ok := app.ProjectFor(projects, strings.TrimPrefix(strings.TrimPrefix(dt.Dir, w.Dir), "/")); ok && dp.Root == project.Root {
			ch.skipf("%s/angular.json: the %s project holds the default outlet's client too, so it stays; remove what in it was the %s outlet's", w.Dir, project.Name, r.Name)

			return r.removeGenerated(a, target.Dir, ch)
		}
	}
	if err := removeTree(a, path.Join(w.Dir, project.Root)); err != nil {
		return err
	}
	ch.didf("deleted the %s browser project, %s/%s, with the %s outlet's generated client", project.Name, w.Dir, project.Root, r.Name)
	if err := r.unregisterProject(a, w.Dir, project.Name, ch); err != nil {
		return err
	}
	r.unregisterScripts(a, w.Dir, project.Name, ch)
	r.unregisterProcess(a, project.Name, ch)

	return nil
}

// removeGenerated deletes the generated files of a client directory the outlet's target
// wrote, and the directory when nothing else is in it.
func (r RemoveOutlet) removeGenerated(a *app.App, dir string, ch *Change) error {
	entries, err := os.ReadDir(a.Abs(dir))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}

		return errors.Wrap(err, "os.ReadDir()")
	}
	removed := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "zz_gen_") {
			continue
		}
		if err := removeTree(a, path.Join(dir, e.Name())); err != nil {
			return err
		}
		removed++
	}
	if removed == len(entries) {
		if err := removeTree(a, dir); err != nil {
			return err
		}
	}
	if removed > 0 {
		ch.didf("%s: deleted the %d generated file(s) of the %s outlet's client", dir, removed, r.Name)
	}

	return nil
}

// removeTree deletes a root-relative file or directory, staying inside the application.
func removeTree(a *app.App, rel string) error {
	root, err := os.OpenRoot(a.Root)
	if err != nil {
		return errors.Wrap(err, "os.OpenRoot()")
	}
	defer root.Close()
	if err := root.RemoveAll(filepath.FromSlash(rel)); err != nil {
		return errors.Wrapf(err, "os.Root.RemoveAll(%s)", rel)
	}

	return nil
}

// unregisterProject takes the project out of angular.json.
func (r RemoveOutlet) unregisterProject(a *app.App, webDir, project string, ch *Change) error {
	rel := path.Join(webDir, "angular.json")
	data, mode, err := readFile(a, rel)
	if err != nil {
		return err
	}
	out, err := removeAngularProject(data, project)
	if err != nil {
		ch.skipf("%s: the %s project was not removed (%v); remove its entry by hand", rel, project, err)

		return nil
	}
	if err := os.WriteFile(a.Abs(rel), out, mode); err != nil {
		return errors.Wrap(err, "os.WriteFile()")
	}
	ch.didf("%s: removed the %s project", rel, project)

	return nil
}

// unregisterScripts removes the project's start, build, and lint scripts and takes it
// out of the workspace-wide build and lint scripts.
func (r RemoveOutlet) unregisterScripts(a *app.App, webDir, project string, ch *Change) {
	rel := path.Join(webDir, "package.json")
	data, mode, err := readFile(a, rel)
	if err != nil {
		ch.skipf("%s: not read (%v); remove the %s project's scripts", rel, err, project)

		return
	}
	text := string(data)
	edited := regexp.MustCompile(`(?m)^[ \t]*"(start|build|lint):`+regexp.QuoteMeta(project)+`":\s*"[^"]*",?\n`).ReplaceAllString(text, "")
	for _, script := range []string{"build", "lint"} {
		edited = regexp.MustCompile(` && ng `+script+` `+regexp.QuoteMeta(project)+`\b`).ReplaceAllString(edited, "")
		edited = regexp.MustCompile(`\bng `+script+` `+regexp.QuoteMeta(project)+` && `).ReplaceAllString(edited, "")
	}
	// A script removed from the end of the block leaves a comma JSON forbids.
	edited = regexp.MustCompile(`,(\s*\n\s*})`).ReplaceAllString(edited, "$1")
	if edited == text {
		ch.skipf("%s: no scripts name the %s project", rel, project)

		return
	}
	if err := os.WriteFile(a.Abs(rel), []byte(edited), mode); err != nil {
		ch.skipf("%s: not written (%v)", rel, err)

		return
	}
	ch.didf("%s: removed the %s project's scripts and its part of build and lint", rel, project)
}

// unregisterProcess removes the Procfile process running the project's dev server.
func (r RemoveOutlet) unregisterProcess(a *app.App, project string, ch *Change) {
	data, mode, err := readFile(a, "Procfile")
	if err != nil {
		ch.skipf("Procfile: not read (%v); remove the process running the %s dev server", err, project)

		return
	}
	var out strings.Builder
	removed := []string{}
	for line := range strings.Lines(string(data)) {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "#") && strings.Contains(trimmed, "start:"+project) && !strings.Contains(trimmed, "start:"+project+"-") {
			name, _, _ := strings.Cut(trimmed, ":")
			removed = append(removed, strings.TrimSpace(name))

			continue
		}
		out.WriteString(line)
	}
	if len(removed) == 0 {
		ch.skipf("Procfile: no process runs start:%s; remove the %s outlet's process if one exists under another name, and the comments that describe it", project, r.Name)

		return
	}
	if err := os.WriteFile(a.Abs("Procfile"), []byte(out.String()), mode); err != nil {
		ch.skipf("Procfile: not written (%v)", err)

		return
	}
	ch.didf("Procfile: removed the %s process; the comments that describe it are still there", strings.Join(removed, " and "))
}

// outletListRE is an @outlet annotation's list.
var outletListRE = regexp.MustCompile(`@outlet\(([^)]*)\)`)

// detachMembers takes the outlet out of every @outlet list that names it beside other
// outlets, so those structs keep their other outlets. A struct on the outlet alone keeps
// its annotation and is recorded for the agent: whether it moves to the default outlet or
// leaves the application is a decision about who may reach it.
func (r RemoveOutlet) detachMembers(a *app.App, ch *Change) error {
	byFile := map[string][]app.OutletMember{}
	var alone []string
	for _, m := range a.OutletMembers {
		if !slices.Contains(m.Outlets, r.Name) {
			continue
		}
		if len(m.Outlets) == 1 {
			alone = append(alone, fmt.Sprintf("%s:%d %s", m.File, m.Line, m.Name))

			continue
		}
		byFile[m.File] = append(byFile[m.File], m)
	}
	files := make([]string, 0, len(byFile))
	for f := range byFile {
		files = append(files, f)
	}
	sort.Strings(files)
	for _, f := range files {
		data, mode, err := readFile(a, f)
		if err != nil {
			return err
		}
		edited := outletListRE.ReplaceAllStringFunc(string(data), func(m string) string {
			sm := outletListRE.FindStringSubmatch(m)
			var kept []string
			for _, name := range strings.Split(sm[1], ",") {
				if name = strings.TrimSpace(name); name != "" && name != r.Name {
					kept = append(kept, name)
				}
			}
			if len(kept) == 0 {
				return m
			}

			return "@outlet(" + strings.Join(kept, ", ") + ")"
		})
		if err := os.WriteFile(a.Abs(f), []byte(edited), mode); err != nil {
			return errors.Wrap(err, "os.WriteFile()")
		}
		names := make([]string, 0, len(byFile[f]))
		for _, m := range byFile[f] {
			names = append(names, m.Name)
		}
		ch.didf("%s: %s stay on their other outlets; %s is out of their @outlet lists", f, strings.Join(names, ", "), r.Name)
	}
	if len(alone) > 0 {
		ch.skipf("these structs were on the %s outlet alone and keep their @outlet(%s) annotation, which fails generation now; decide for each whether it moves to the default outlet (drop the annotation: the console's people reach it then) or leaves the application with its table, and regenerate:\n  %s", r.Name, r.Name, strings.Join(alone, "\n  "))
	}

	return nil
}

// noteEnvironment records the environment template's lines that name the outlet, for the
// agent: the variables are the application's, so the tool does not guess which are the
// outlet's alone.
func (r RemoveOutlet) noteEnvironment(a *app.App, ch *Change) {
	if a.EnvTemplate == "" {
		return
	}
	data, _, err := readFile(a, a.EnvTemplate)
	if err != nil {
		return
	}
	marker := "APP_" + strings.ToUpper(r.Name) + "_"
	var lines []string
	for line := range strings.Lines(string(data)) {
		if strings.Contains(line, marker) {
			lines = append(lines, strings.TrimSpace(line))
		}
	}
	if len(lines) > 0 {
		ch.skipf("%s: these lines name the %s outlet's configuration; remove them with the configuration fields they fed:\n  %s", a.EnvTemplate, r.Name, strings.Join(lines, "\n  "))
	}
}

// Meaning explains the removal in this framework and names the unwiring left to do.
func (r RemoveOutlet) Meaning() string {
	pascal := strings.ToUpper(r.Name[:1]) + r.Name[1:]
	upper := strings.ToUpper(r.Name)
	var b strings.Builder
	fmt.Fprintf(&b, "A router outlet is a second URL space on the same host, declared by `WithRouterOutlet` and mounted by the hand-written router through the generated `generated%sRoutes`. The declaration is gone, so the generator no longer emits the outlet's routes, handlers, or client, and everything that referred to them has to go with it.\n\n", pascal)
	b.WriteString("Left to unwire:\n\n")
	items := []string{
		fmt.Sprintf("The router: the group that mounted `generated%sRoutes` under the outlet's prefix, the prefix's not-found handler, and, for a session outlet, the routes that served its browser application (deep links and assets under `/%s/`).", pascal, r.Name),
		fmt.Sprintf("The App: the handlers and accessors that were the outlet's (`%s()`, `%sDeepLink`, `%sAssets`, the dist field) and, for an API-key outlet, its authentication middleware and the `Handlers` interface methods only it satisfied.", pascal, pascal, pascal),
		fmt.Sprintf("The configuration: the fields read from `APP_%s_*` (a dist directory, an API key) and their lines in the environment template and the Procfile comments.", upper),
		fmt.Sprintf("The members: a struct still annotated `@outlet(%s)` alone fails generation. For each, decide whether it moves to the default outlet (drop the annotation, and the console's people reach it) or leaves the application with its table (a migration drops the table). Then run `go generate ./...`.", r.Name),
		"The tests: the integration tests that signed in or presented a key under the outlet's prefix, and the bootstrap identities that existed only for it (an API-key outlet's service account).",
		"The auth the outlet was bound to stays, with its tables, store, and roles file, since another surface may bind to it; remove it separately when nothing does.",
	}
	for i, item := range items {
		fmt.Fprintf(&b, "%d. %s\n", i+1, item)
	}
	b.WriteString("\nData consequence: none from the removal itself. A table that only the outlet's resources declared is still in the schema until a migration drops it.\n")

	return b.String()
}
