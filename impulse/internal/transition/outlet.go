package transition

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/impulse/internal/app"
	"github.com/cccteam/ccc/impulse/internal/check"
)

// Outlet adds a router outlet to a flat application: a second URL space on the same
// host, either a browser surface behind the same session handling as the console (a
// session outlet, which gets its own generated client and browser project) or a machine
// surface behind an authentication the application defines (an API-key outlet).
type Outlet struct {
	// Name is the outlet's lowerCamelCase name, as WithRouterOutlet and @outlet use it.
	Name string
	// Prefix is the outlet's URL prefix without slashes at either end: portal/api.
	Prefix string
	// Sessions marks a session outlet.
	Sessions bool
}

// ReferenceCandidate is the embedded skeleton with both outlet kinds wired.
const ReferenceCandidate = "outlets"

var outletNameRE = regexp.MustCompile(`^[a-z][A-Za-z0-9]*$`)

// Command is the impulse command line for the transition.
func (o Outlet) Command() string {
	kind := "--api-key"
	if o.Sessions {
		kind = "--sessions"
	}

	return fmt.Sprintf("impulse add outlet %s --prefix %s %s", o.Name, o.Prefix, kind)
}

// Validate checks the outlet against the application before anything is changed.
func (o Outlet) Validate(a *app.App) error {
	if !outletNameRE.MatchString(o.Name) || o.Name == "default" {
		return errors.Newf("outlet name %q: use a lowerCamelCase identifier other than default", o.Name)
	}
	if o.Prefix == "" || strings.HasPrefix(o.Prefix, "/") || strings.HasSuffix(o.Prefix, "/") {
		return errors.Newf("outlet prefix %q: name the URL prefix without slashes at either end, such as portal/api", o.Prefix)
	}
	p := a.Profile()
	switch {
	case len(p.Sites) == 0:
		return errors.New("no generator program emits handlers; the application has no site to add an outlet to")
	case len(p.Sites) > 1:
		return errors.New("adding an outlet to a multi-site application is not supported yet")
	}
	site := &p.Sites[0]
	g := site.Generator
	if g.RoutesDir() == "" {
		return errors.Newf("%s: an outlet needs GenerateRoutes", g.File)
	}
	for _, existing := range site.Outlets {
		if existing.Name == o.Name {
			return errors.Newf("%s: outlet %s is already declared", existing.Pos, o.Name)
		}
	}
	if o.Sessions && defaultTarget(g) == nil {
		return errors.Newf("%s: a session outlet's browser project is copied from the default outlet's, but no GenerateTypescript target without ForOutlet names one", g.File)
	}

	return nil
}

// Apply makes the deterministic half of the transition: the generator program gains the
// outlet and, for a session outlet, a generated client; the browser project is copied
// from the console's with its prefix, base path, and ports rewritten and registered in
// the workspace and the Procfile; and go generate emits the outlet's routes, handlers,
// and client. The router, the served assets, the configuration, the members, and the
// tests are the agent's.
func (o Outlet) Apply(ctx context.Context, a *app.App, exec check.Execer) (*Change, error) {
	if err := o.Validate(a); err != nil {
		return nil, err
	}
	ch := &Change{Command: o.Command()}
	site := &a.Profile().Sites[0]
	g := site.Generator

	if err := o.editProgram(a, g, ch); err != nil {
		return nil, err
	}
	if o.Sessions {
		if err := o.cloneProject(a, g, ch); err != nil {
			return nil, err
		}
	}

	if out, err := exec.Run(ctx, a.Root, nil, "go", "generate", "./..."); err != nil {
		ch.skipf("go generate ./... failed, so the outlet's routes, handlers, and client are not generated yet; fix the cause and run it:\n%s", strings.TrimSpace(string(out)))
	} else {
		ch.didf("ran go generate ./..., which emitted the %s outlet's routes and handlers%s", o.Name, o.clientNote())
	}

	return ch, nil
}

func (o Outlet) clientNote() string {
	if o.Sessions {
		return " and its browser client"
	}

	return ""
}

// editProgram adds WithRouterOutlet after GenerateRoutes and, for a session outlet, a
// GenerateTypescript target for the outlet copied from the default outlet's.
func (o Outlet) editProgram(a *app.App, g *app.Generator, ch *Change) error {
	src, mode, err := readFile(a, g.File)
	if err != nil {
		return err
	}
	option := fmt.Sprintf("generation.WithRouterOutlet(%q, %q)", o.Name, o.Prefix)
	if o.Sessions {
		option = fmt.Sprintf("generation.WithRouterOutlet(%q, %q, generation.ServesSessions())", o.Name, o.Prefix)
	}
	edited, err := app.InsertOptions(g.File, src, "GenerateRoutes", []string{option})
	if err != nil {
		return err
	}
	ch.didf("%s: added %s", g.File, strings.TrimPrefix(option, "generation."))

	if o.Sessions {
		target := defaultTarget(g)
		text, err := app.OptionText(g.File, src, "GenerateTypescript", target.Dir)
		if err != nil {
			return err
		}
		newDir := o.clientDir(a, target.Dir)
		clone := cloneTypescriptCall(text, target.Dir, newDir, o.Name)
		edited, err = app.InsertOptions(g.File, edited, "GenerateTypescript", []string{clone})
		if err != nil {
			return err
		}
		ch.didf("%s: added GenerateTypescript(%q, ForOutlet(%q), ...) with the default target's options", g.File, newDir, o.Name)
	}

	if err := os.WriteFile(a.Abs(g.File), edited, mode); err != nil {
		return errors.Wrap(err, "os.WriteFile()")
	}

	return nil
}

// cloneTypescriptCall rewrites a GenerateTypescript call's text to another directory
// with ForOutlet as its first option.
func cloneTypescriptCall(text, oldDir, newDir, outlet string) string {
	quoted := fmt.Sprintf("%q", oldDir)
	head, rest, ok := strings.Cut(text, quoted)
	if !ok {
		return fmt.Sprintf("generation.GenerateTypescript(%q, generation.ForOutlet(%q))", newDir, outlet)
	}
	forOutlet := fmt.Sprintf("generation.ForOutlet(%q)", outlet)
	if strings.HasPrefix(strings.TrimSpace(rest), ",") {
		return head + fmt.Sprintf("%q,\n\t\t\t%s", newDir, forOutlet) + rest
	}

	return head + fmt.Sprintf("%q, %s", newDir, forOutlet) + rest
}

// clientDir is where the outlet's generated client goes: the default target's path with
// the browser project's directory replaced by the outlet's.
func (o Outlet) clientDir(a *app.App, defaultDir string) string {
	w, ok := a.WebAppFor(defaultDir)
	if !ok {
		return path.Join(path.Dir(path.Dir(defaultDir)), o.Name, path.Base(defaultDir))
	}
	rel := strings.TrimPrefix(strings.TrimPrefix(defaultDir, w.Dir), "/")
	_, inProject, _ := strings.Cut(rel, "/")
	if projects, err := a.ReadAngular(w.Dir); err == nil {
		if p, ok := app.ProjectFor(projects, rel); ok && p.Root != "" {
			inProject = strings.TrimPrefix(strings.TrimPrefix(rel, p.Root), "/")
		}
	}

	return path.Join(w.Dir, o.Name, inProject)
}

// defaultTarget is the GenerateTypescript target of the default outlet, or nil.
func defaultTarget(g *app.Generator) *app.TSTarget {
	for _, t := range g.TypescriptTargets() {
		if t.Outlet == "" {
			return &t
		}
	}

	return nil
}

// skippedNames are the directories a project copy leaves out: build products and the
// generated client, which go generate writes for the new target.
var skippedNames = map[string]bool{"node_modules": true, "dist": true, ".angular": true}

// cloneProject copies the default outlet's browser project to the outlet's, rewrites the
// API prefix, base path, and output paths in the copy, and registers the project in
// angular.json, the package scripts, and the Procfile. Each registration it cannot make
// is recorded as skipped.
func (o Outlet) cloneProject(a *app.App, g *app.Generator, ch *Change) error {
	target := defaultTarget(g)
	w, ok := a.WebAppFor(target.Dir)
	if !ok {
		ch.skipf("no browser workspace (angular.json) holds %s, so no browser project was copied for %s; create one", target.Dir, o.Name)

		return nil
	}
	projects, err := a.ReadAngular(w.Dir)
	if err != nil {
		return err
	}
	rel := strings.TrimPrefix(strings.TrimPrefix(target.Dir, w.Dir), "/")
	project, ok := app.ProjectFor(projects, rel)
	if !ok || project.Root == "" {
		ch.skipf("%s/angular.json has no project rooted at a directory holding %s, so no browser project was copied for %s; create one", w.Dir, rel, o.Name)

		return nil
	}
	oldRoot, newRoot := project.Root, o.Name
	if _, err := os.Stat(a.Abs(path.Join(w.Dir, newRoot))); err == nil {
		return errors.Newf("%s/%s already exists; the %s browser project would be copied there", w.Dir, newRoot, o.Name)
	}
	oldPrefix := ""
	if routes, ok := g.Option("GenerateRoutes"); ok && len(routes.Args) == 2 {
		oldPrefix = routes.Args[1].Str
	}
	rewrites := o.rewrites(oldPrefix, oldRoot)
	if err := copyProject(a.Abs(path.Join(w.Dir, oldRoot)), a.Abs(path.Join(w.Dir, newRoot)), rewrites); err != nil {
		return err
	}
	// The generator opens the client directory before it writes, and the copy left it
	// out when it held only generated files.
	if err := os.MkdirAll(a.Abs(o.clientDir(a, target.Dir)), 0o755); err != nil {
		return errors.Wrap(err, "os.MkdirAll()")
	}
	ch.didf("copied the %s browser project to %s/%s, rewriting its API prefix (/%s to /%s), base path (/%s/), and output paths; its titles still say %s", oldRoot, w.Dir, newRoot, oldPrefix, o.Prefix, o.Name, oldRoot)

	port := 0
	for _, p := range projects {
		port = max(port, p.DevPort)
	}
	if port > 0 {
		port++
	}
	if err := o.registerProject(a, w.Dir, project.Name, port, ch); err != nil {
		return err
	}
	o.registerScripts(a, w.Dir, project.Name, ch)
	o.registerProcess(a, project.Name, ch)

	return nil
}

// rewrites are the textual substitutions a copied project needs: the proxy and client
// prefix, the base path, and the compiler output directory.
func (o Outlet) rewrites(oldPrefix, oldRoot string) []rewrite {
	base := "/" + o.Name + "/"

	return []rewrite{
		{"'/" + oldPrefix + "/'", "'/" + o.Prefix + "/'"},
		{"'/" + oldPrefix + "'", "'/" + o.Prefix + "'"},
		{`<base href="/" />`, `<base href="` + base + `" />`},
		{"baseUrl: ''", "baseUrl: '" + base + "'"},
		{"baseUrl: '/'", "baseUrl: '" + base + "'"},
		{"out-tsc/" + oldRoot, "out-tsc/" + o.Name},
	}
}

type rewrite struct{ old, new string }

// copyProject copies a project directory, skipping build products and generated files,
// applying the rewrites to text files. Both trees are opened as roots, so the copy stays
// inside them however the project is laid out.
func copyProject(src, dst string, rewrites []rewrite) error {
	from, err := os.OpenRoot(src)
	if err != nil {
		return errors.Wrap(err, "os.OpenRoot()")
	}
	defer from.Close()
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return errors.Wrap(err, "os.MkdirAll()")
	}
	to, err := os.OpenRoot(dst)
	if err != nil {
		return errors.Wrap(err, "os.OpenRoot()")
	}
	defer to.Close()

	err = fs.WalkDir(from.FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return errors.Wrap(err, "fs.WalkDir()")
		}
		if d.IsDir() {
			if skippedNames[d.Name()] {
				return fs.SkipDir
			}
			if p != "." {
				if err := to.MkdirAll(filepath.FromSlash(p), 0o755); err != nil {
					return errors.Wrap(err, "os.Root.MkdirAll()")
				}
			}

			return nil
		}
		if strings.HasPrefix(d.Name(), "zz_gen_") {
			return nil
		}
		data, err := from.ReadFile(filepath.FromSlash(p))
		if err != nil {
			return errors.Wrap(err, "os.Root.ReadFile()")
		}
		if utf8.Valid(data) {
			text := string(data)
			for _, r := range rewrites {
				text = strings.ReplaceAll(text, r.old, r.new)
			}
			data = []byte(text)
		}
		if err := to.WriteFile(filepath.FromSlash(p), data, 0o644); err != nil {
			return errors.Wrap(err, "os.Root.WriteFile()")
		}

		return nil
	})
	if err != nil {
		return errors.Wrap(err, "fs.WalkDir()")
	}

	return nil
}

// registerProject adds the outlet's project to angular.json as a copy of the default
// project's entry, serving under the outlet's base path on the next free port.
func (o Outlet) registerProject(a *app.App, webDir, from string, port int, ch *Change) error {
	rel := path.Join(webDir, "angular.json")
	data, mode, err := readFile(a, rel)
	if err != nil {
		return err
	}
	out, err := cloneAngularProject(data, from, o.Name, port)
	if err != nil {
		ch.skipf("%s: the %s project could not be copied to %s (%v); add the project by hand, serving under /%s with proxyConfig %s/proxy.conf.js", rel, from, o.Name, err, o.Name, o.Name)

		return nil
	}
	if err := os.WriteFile(a.Abs(rel), out, mode); err != nil {
		return errors.Wrap(err, "os.WriteFile()")
	}
	ch.didf("%s: added the %s project as a copy of %s, serving under /%s on port %d", rel, o.Name, from, o.Name, port)

	return nil
}

// registerScripts adds a start script for the outlet and extends the build and lint
// scripts, following the default project's lines.
func (o Outlet) registerScripts(a *app.App, webDir, from string, ch *Change) {
	rel := path.Join(webDir, "package.json")
	data, mode, err := readFile(a, rel)
	if err != nil {
		ch.skipf("%s: not read (%v); add start:%s, build, and lint scripts for the %s project", rel, err, o.Name, o.Name)

		return
	}
	text := string(data)
	startRE := regexp.MustCompile(`(?m)^([ \t]*)"start:` + regexp.QuoteMeta(from) + `":\s*"ng serve ` + regexp.QuoteMeta(from) + `([^"]*)",?\n`)
	m := startRE.FindStringSubmatchIndex(text)
	if m == nil {
		ch.skipf("%s: no \"start:%s\" script to copy; add start:%s, build, and lint scripts for the %s project", rel, from, o.Name, o.Name)

		return
	}
	indent, flags := text[m[2]:m[3]], text[m[4]:m[5]]
	added := fmt.Sprintf("%s\"start:%s\": \"ng serve %s%s\",\n", indent, o.Name, o.Name, flags)
	text = text[:m[1]] + added + text[m[1]:]
	for _, script := range []string{"build", "lint"} {
		re := regexp.MustCompile(`"` + script + `":\s*"(ng ` + script + ` [^"]*)"`)
		if sm := re.FindStringSubmatchIndex(text); sm != nil {
			value := text[sm[2]:sm[3]]
			text = text[:sm[2]] + value + " && ng " + script + " " + o.Name + text[sm[3]:]
		}
	}
	if err := os.WriteFile(a.Abs(rel), []byte(text), mode); err != nil {
		ch.skipf("%s: not written (%v)", rel, err)

		return
	}
	ch.didf("%s: added start:%s and extended build and lint to the %s project", rel, o.Name, o.Name)
}

// registerProcess adds a Procfile process for the outlet's dev server, copied from the
// default project's.
func (o Outlet) registerProcess(a *app.App, from string, ch *Change) {
	data, mode, err := readFile(a, "Procfile")
	if err != nil {
		ch.skipf("Procfile: not read (%v); add a process running the %s dev server", err, o.Name)

		return
	}
	var out strings.Builder
	added := false
	for line := range strings.Lines(string(data)) {
		out.WriteString(line)
		trimmed := strings.TrimSpace(line)
		if added || strings.HasPrefix(trimmed, "#") || !strings.Contains(trimmed, "start:"+from) {
			continue
		}
		name, command, ok := strings.Cut(trimmed, ":")
		if !ok || strings.TrimSpace(name) != from {
			continue
		}
		process := o.Name + ":" + strings.ReplaceAll(command, "start:"+from, "start:"+o.Name)
		if !strings.HasSuffix(line, "\n") {
			out.WriteString("\n")
		}
		out.WriteString(process + "\n")
		added = true
	}
	if !added {
		ch.skipf("Procfile: no %s process running start:%s to copy; add a process running the %s dev server", from, from, o.Name)

		return
	}
	if err := os.WriteFile(a.Abs("Procfile"), []byte(out.String()), mode); err != nil {
		ch.skipf("Procfile: not written (%v)", err)

		return
	}
	ch.didf("Procfile: added the %s process, a copy of %s running start:%s", o.Name, from, o.Name)
}

// Meaning explains the outlet in this framework and names the wiring left to do.
func (o Outlet) Meaning() string {
	pascal := strings.ToUpper(o.Name[:1]) + o.Name[1:]
	upper := strings.ToUpper(o.Name)
	var b strings.Builder
	fmt.Fprintf(&b, "A router outlet is a second URL space on the same host. Structs annotated `@outlet(%s)` are served under `/%s` by the generated `generated%sRoutes`, which the hand-written router must mount; naming only the new outlet takes a struct off the default outlet, and `@outlet(default, %s)` keeps it on both. The generated tests prove the outlets' URL spaces are disjoint.\n\n", o.Name, o.Prefix, pascal, o.Name)
	if o.Sessions {
		fmt.Fprintf(&b, "This is a session outlet: a browser surface behind the same session handling as the console. In the router, compose the session group around `generated%sRoutes` under `/%s` the way the console's is composed under its prefix, give the prefix its own not-found handler, and serve the %s browser application from `/%s/` (a dist directory from configuration, `APP_%s_DIST` defaulting to `web/dist/%s`, with a deep-link and assets handler pair in the app package like the console's). Decide which resources the %s outlet serves and annotate them; then run `go generate ./...`. Extend the integration tests: sign in under `/%s/user/login` and read `user-domains` and the permission digest there, and show a resource that is not a member answers not found under the prefix. If the outlet's audience is not the console's, add its development login to the bootstrap identities.\n", pascal, o.Prefix, o.Name, o.Name, upper, o.Name, o.Name, o.Prefix)

		return b.String()
	}
	fmt.Fprintf(&b, "This is an API-key outlet: a machine surface with no browser. In the router, compose a group around `generated%sRoutes` under `/%s` with no session handling and no XSRF guard, and an authentication middleware in the app package that requires `Authorization: Bearer <key>`, compares it in constant time against a configured key (`APP_%s_API_KEY`; when unset, generate an ephemeral key at startup so the surface stays closed), and binds the request to a service identity in the session context the way the session middleware binds a browser to its user, so the handlers behind it run the same permission checks. Seed that service identity's roles in the bootstrap identities. Decide which resources the %s outlet serves and annotate them; then run `go generate ./...`. Extend the integration tests: the right key answers, a wrong key and no key answer unauthorized, and a resource that is not a member answers not found under the prefix.\n", pascal, o.Prefix, upper, o.Name)

	return b.String()
}

// readFile reads a root-relative file with its mode, so an edit keeps it.
func readFile(a *app.App, rel string) (data []byte, mode os.FileMode, err error) {
	abs := a.Abs(rel)
	info, err := os.Stat(abs)
	if err != nil {
		return nil, 0, errors.Wrap(err, "os.Stat()")
	}
	data, err = os.ReadFile(abs)
	if err != nil {
		return nil, 0, errors.Wrap(err, "os.ReadFile()")
	}

	return data, info.Mode().Perm(), nil
}
