package transition

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/impulse/app"
	"github.com/cccteam/ccc/impulse/internal/check"
)

// Outlet adds a router outlet to a flat application: a second URL space on the same
// host, either a browser surface bound to an auth (a session outlet, which gets its own
// generated client and browser project) or a machine surface behind an authentication the
// application defines (an API-key outlet).
type Outlet struct {
	// Name is the outlet's lowerCamelCase name, as WithRouterOutlet and @outlet use it.
	Name string
	// Prefix is the outlet's URL prefix without slashes at either end: portal/api.
	Prefix string
	// Sessions marks a session outlet.
	Sessions bool
	// Auth names the auth package a session outlet binds to, pkg/auth/<Auth>. Empty binds
	// the outlet to the auth the default outlet declares: the application's only auth when
	// impulse new composes the outlet.
	Auth string
}

// ReferenceCandidate is the embedded skeleton with both outlet kinds wired.
const ReferenceCandidate = "outlets"

var outletNameRE = regexp.MustCompile(`^[a-z][A-Za-z0-9]*$`)

// Command is the impulse command line for the transition.
func (o Outlet) Command() string {
	kind := "--api-key"
	if o.Sessions {
		kind = "--auth " + o.Auth
	}

	return fmt.Sprintf("impulse add outlet %s --prefix %s %s", o.Name, o.Prefix, kind)
}

// Validate checks the outlet against the application before anything is changed.
func (o Outlet) Validate(a *app.App) error {
	if !outletNameRE.MatchString(o.Name) || o.Name == defaultOutletName {
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
		return errors.New("adding an outlet to an application in the sites layout is not supported yet")
	}
	site := &p.Sites[0]
	g := site.Generator
	if g.RoutesDir() == "" {
		return errors.Newf("%s: an outlet needs GenerateRoutes", g.File)
	}
	for i := range site.Outlets {
		existing := &site.Outlets[i]
		if existing.Name == o.Name {
			return errors.Newf("%s: outlet %s is already declared", existing.Pos, o.Name)
		}
	}
	if o.Sessions && defaultTarget(g) == nil {
		return errors.Newf("%s: a session outlet's browser project is copied from the default outlet's, but no GenerateTypescript target without ForOutlet names one", g.File)
	}
	if o.Sessions {
		if _, _, err := o.binding(a, site); err != nil {
			return err
		}
	}

	return nil
}

// binding resolves the auth a session outlet binds to: the package --auth names, in the
// flavor its constructor declares, or the auth the default outlet declares when none is
// named. Under a hand-written router that declares no Auth there is nothing to bind to in
// the program, and both results are empty.
func (o Outlet) binding(a *app.App, site *app.Site) (name string, auth *app.OutletAuth, err error) {
	if o.Auth == "" {
		if site.Default.Auth == nil {
			if site.GeneratedRouter {
				return "", nil, errors.Newf("%s: the default outlet declares no Auth to bind the %s outlet to; under GenerateRouter the console's GenerateRoutes carries Auth(<package>, <flavor>), or --auth names the auth", site.Generator.File, o.Name)
			}

			return "", nil, nil
		}

		return authName(a, site.Default.Auth.ImportPath), site.Default.Auth, nil
	}
	for i := range a.AuthPackages {
		p := &a.AuthPackages[i]
		if p.Name != o.Auth {
			continue
		}
		flavor := ""
		for j := range a.Auths {
			if path.Dir(a.Auths[j].File) == p.Dir {
				flavor = a.Auths[j].Flavor

				break
			}
		}
		ident := app.FlavorIdent(flavor)
		if ident == "" {
			return "", nil, errors.Newf("%s: the %s auth constructs a %s authenticator, which the generated router does not compose; an outlet binds to a password, oidc-azure, or oidc-google auth", p.Dir, o.Auth, flavor)
		}

		return p.Name, &app.OutletAuth{ImportPath: p.Path, Flavor: ident}, nil
	}

	return "", nil, errors.Newf("no auth package pkg/auth/%s to bind the %s outlet to; the auths are %s (impulse add auth adds one)", o.Auth, o.Name, authNames(a))
}

// authName is the name of the auth package at an import path: the package's directory
// name, as the auths are named.
func authName(a *app.App, importPath string) string {
	for i := range a.AuthPackages {
		if a.AuthPackages[i].Path == importPath {
			return a.AuthPackages[i].Name
		}
	}

	return path.Base(importPath)
}

// authNames lists the application's auths, or says there are none.
func authNames(a *app.App) string {
	if len(a.AuthPackages) == 0 {
		return "none"
	}
	names := make([]string, 0, len(a.AuthPackages))
	for i := range a.AuthPackages {
		names = append(names, a.AuthPackages[i].Name)
	}

	return strings.Join(names, ", ")
}

// Apply makes the deterministic half of the transition: the generator program gains the
// outlet and, for a session outlet, a generated client; the browser project is copied
// from the console's with its prefix, base path, and ports rewritten and registered in
// the workspace and the Procfile; a console that was alone at the root moves under its
// own path, since no browser application is mounted at / beside another; and go generate
// emits the outlet's routes, handlers, and client. The router, the served assets, the
// configuration, the members, and the tests are the agent's.
func (o Outlet) Apply(ctx context.Context, a *app.App, exec check.Execer) (*Change, error) {
	if err := o.Validate(a); err != nil {
		return nil, err
	}
	site := &a.Profile().Sites[0]
	g := site.Generator
	var (
		name string
		auth *app.OutletAuth
	)
	if o.Sessions {
		var err error
		if name, auth, err = o.binding(a, site); err != nil {
			return nil, err
		}
		o.Auth = name
	}
	ch := &Change{Command: o.Command()}

	if err := o.editProgram(a, site, auth, ch); err != nil {
		return nil, err
	}
	if o.Sessions {
		consoleAuth := ""
		if site.Default.Auth != nil {
			consoleAuth = authName(a, site.Default.Auth.ImportPath)
		}
		if err := o.cloneProject(a, g, consoleMount(site), consoleAuth, ch); err != nil {
			return nil, err
		}
		if err := o.moveConsole(a, site, ch); err != nil {
			return nil, err
		}
	}

	generate(ctx, a, exec, ch, "go generate ./... failed, so the outlet's routes, handlers, and client are not generated yet; fix the cause and run it:", fmt.Sprintf("ran go generate ./..., which emitted the %s outlet's routes and handlers%s", o.Name, o.clientNote()))

	return ch, nil
}

func (o Outlet) clientNote() string {
	if o.Sessions {
		return " and its browser client"
	}

	return ""
}

// editProgram adds WithRouterOutlet after GenerateRoutes and, for a session outlet, a
// GenerateTypescript target for the outlet copied from the default outlet's. Under the
// generated router the declaration says how the outlet authenticates: a session outlet
// binds to its auth (the one --auth names, else the default outlet's) and serves its
// browser application at /<name>, an API-key outlet declares APIKey; under a hand-written
// router a session outlet declares ServesSessions and the brief names the auth.
func (o Outlet) editProgram(a *app.App, site *app.Site, auth *app.OutletAuth, ch *Change) error {
	g := site.Generator
	src, mode, err := readFile(a, g.File)
	if err != nil {
		return err
	}
	option := fmt.Sprintf("generation.WithRouterOutlet(%q, %q)", o.Name, o.Prefix)
	summary := option
	switch {
	case site.GeneratedRouter && o.Sessions:
		authText := fmt.Sprintf("generation.Auth(%q, generation.%s)", auth.ImportPath, auth.Flavor)
		webApp := fmt.Sprintf("generation.WebApp(%q)", "/"+o.Name)
		option = fmt.Sprintf("generation.WithRouterOutlet(%q, %q,\n\t\t\t%s,\n\t\t\t%s,\n\t\t)", o.Name, o.Prefix, authText, webApp)
		summary = fmt.Sprintf("generation.WithRouterOutlet(%q, %q, %s, %s)", o.Name, o.Prefix, authText, webApp)
	case site.GeneratedRouter:
		option = fmt.Sprintf("generation.WithRouterOutlet(%q, %q, generation.APIKey())", o.Name, o.Prefix)
		summary = option
	case o.Sessions:
		option = fmt.Sprintf("generation.WithRouterOutlet(%q, %q, generation.ServesSessions())", o.Name, o.Prefix)
		summary = option
	}
	edited, err := app.InsertOptions(g.File, src, "GenerateRoutes", []string{option})
	if err != nil {
		return err
	}
	bound := ""
	if o.Sessions && !site.GeneratedRouter && o.Auth != "" {
		bound = fmt.Sprintf("; the outlet binds to the %s auth, whose session group the hand-written router composes", o.Auth)
	}
	ch.didf("%s: added %s%s", g.File, strings.TrimPrefix(summary, "generation."), bound)

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
var skippedNames = map[string]bool{"node_modules": true, "dist": true, ".angular": true, ".yalc": true}

// cloneProject copies the default outlet's browser project to the outlet's, rewrites the
// API prefix, base path (from the console's mount path, mount), and output paths in the
// copy (and the XSRF cookie it names, when the outlet binds to an auth other than the
// console's, consoleAuth), and registers the project in angular.json, the package
// scripts, and the Procfile. Each registration it cannot make is recorded as skipped.
func (o Outlet) cloneProject(a *app.App, g *app.Generator, mount, consoleAuth string, ch *Change) error {
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
	if routes, ok := g.Option("GenerateRoutes"); ok && len(routes.Args) >= 2 {
		oldPrefix = routes.Args[1].Str
	}
	rewrites := append(moveRewrites(oldPrefix, mount, o.Prefix, "/"+o.Name), rewrite{"out-tsc/" + oldRoot, "out-tsc/" + o.Name})
	rebound := o.Auth != "" && consoleAuth != "" && o.Auth != consoleAuth
	if rebound {
		// The copy echoes the console auth's XSRF cookie; the outlet's auth issues its own.
		rewrites = append(rewrites, rewrite{"cookieName: '" + consoleAuth + "-xsrf'", "cookieName: '" + o.Auth + "-xsrf'"})
	}
	if err := copyProject(a.Abs(path.Join(w.Dir, oldRoot)), a.Abs(path.Join(w.Dir, newRoot)), rewrites); err != nil {
		return err
	}
	// The generator opens the client directory before it writes, and the copy left it
	// out when it held only generated files.
	if err := os.MkdirAll(a.Abs(o.clientDir(a, target.Dir)), 0o755); err != nil {
		return errors.Wrap(err, "os.MkdirAll()")
	}
	ch.didf("copied the %s browser project to %s/%s, rewriting its API prefix (/%s to /%s), base path (/%s/), and output paths; its titles still say %s", oldRoot, w.Dir, newRoot, oldPrefix, o.Prefix, o.Name, oldRoot)
	if rebound {
		ch.didf("%s/%s: its HttpClient names the %s auth's XSRF cookie (%s-xsrf) in place of the %s auth's", w.Dir, newRoot, o.Auth, o.Auth, consoleAuth)
	}

	port := 0
	for i := range projects {
		port = max(port, projects[i].DevPort)
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

// moveRewrites are the textual substitutions that move a browser project from one API
// prefix and mount path to another: the proxy and client prefix, the base element, the
// web app manifest's id (the mount path, which identifies the installed application),
// and the base URL, which an application at the root writes empty in development.
func moveRewrites(oldPrefix, oldMount, newPrefix, newMount string) []rewrite {
	oldBase, newBase := basePath(oldMount), basePath(newMount)
	rewrites := []rewrite{
		{"'/" + oldPrefix + "/'", "'/" + newPrefix + "/'"},
		{"'/" + oldPrefix + "'", "'/" + newPrefix + "'"},
		{`<base href="` + oldBase + `" />`, `<base href="` + newBase + `" />`},
		{`"id": "` + oldBase + `"`, `"id": "` + newBase + `"`},
		{"baseUrl: '" + oldBase + "'", "baseUrl: '" + newBase + "'"},
	}
	if oldBase == "/" {
		rewrites = append(rewrites, rewrite{"baseUrl: ''", "baseUrl: '" + newBase + "'"})
	}

	return rewrites
}

type rewrite struct{ old, new string }

// copyProject copies a project directory, skipping build products and generated files,
// applying the rewrites to text files. Both trees are opened as roots, so the copy stays
// inside them however the project is laid out. With dst equal to src the project is
// rewritten in place, and a file the rewrites leave as it was is not written.
func copyProject(src, dst string, rewrites []rewrite) error {
	inPlace := src == dst
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
		out := data
		if utf8.Valid(data) {
			text := string(data)
			for _, r := range rewrites {
				text = strings.ReplaceAll(text, r.old, r.new)
			}
			out = []byte(text)
		}
		if inPlace && bytes.Equal(out, data) {
			return nil
		}
		// The mode carries over, so a script such as ccclib.sh stays executable.
		info, err := d.Info()
		if err != nil {
			return errors.Wrap(err, "fs.DirEntry.Info()")
		}
		if err := to.WriteFile(filepath.FromSlash(p), out, info.Mode().Perm()); err != nil {
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
	var extended []string
	for _, script := range workspaceScripts {
		edited, ok := extendScript(text, script, from, o.Name)
		if ok {
			text = edited
			extended = append(extended, script)
		}
	}
	if err := os.WriteFile(a.Abs(rel), []byte(text), mode); err != nil {
		ch.skipf("%s: not written (%v)", rel, err)

		return
	}
	if len(extended) == 0 {
		ch.didf("%s: added start:%s for the %s project", rel, o.Name, o.Name)

		return
	}
	ch.didf("%s: added start:%s and extended %s to the %s project", rel, o.Name, joinAnd(extended), o.Name)
}

// workspaceScripts are the package scripts that run once per project and grow a segment
// per outlet: ng build, ng lint, and ng test, each project's segment carrying the flags
// the first project's does (ng test console --watch=false, so bun run test stays the
// single-run form).
var workspaceScripts = []string{"build", "lint", "test"}

// scriptFlags are the flags a project's segment of a workspace script carries: bare
// flags (--watch=false) and a flag with a quoted value, which package.json spells with
// escaped quotes (--define \"APP_VERSION='${VERSION:-dev}'\").
const scriptFlags = `((?: --?[^\s"&\\]+(?: \\"(?:[^"\\]|\\.)*\\")?)*)`

// scriptValue matches one script's JSON string value, escaped quotes included.
const scriptValue = `((?:[^"\\]|\\.)*)`

// extendScript appends the project's segment to the named workspace script after the
// from project's, with the same flags, and reports whether the script had a segment to
// follow.
func extendScript(text, script, from, project string) (string, bool) {
	valueRE := regexp.MustCompile(`"` + script + `":\s*"` + scriptValue + `"`)
	vm := valueRE.FindStringSubmatchIndex(text)
	if vm == nil {
		return text, false
	}
	segmentRE := regexp.MustCompile(`\bng ` + script + ` ` + regexp.QuoteMeta(from) + scriptFlags)
	sm := segmentRE.FindStringSubmatch(text[vm[2]:vm[3]])
	if sm == nil {
		return text, false
	}

	return text[:vm[3]] + " && ng " + script + " " + project + sm[1] + text[vm[3]:], true
}

// joinAnd writes a list the way a sentence does: "build", "build and lint", "build,
// lint, and test".
func joinAnd(words []string) string {
	switch len(words) {
	case 0:
		return ""
	case 1:
		return words[0]
	case 2:
		return words[0] + " and " + words[1]
	default:
		return strings.Join(words[:len(words)-1], ", ") + ", and " + words[len(words)-1]
	}
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

// consoleName is the name the default outlet's browser application takes when no
// workspace project names it: the skeletons' project.
const consoleName = "console"

// apiSegment is the last segment of a browser outlet's API prefix in the one shape every
// browser outlet takes: its API under its application's mount path, <mount>/api.
const apiSegment = "api"

// consoleMount is the mount path of the default outlet's browser application: its WebApp,
// or the root under a hand-written router, which declares none.
func consoleMount(site *app.Site) string {
	if site.Default.WebApp == "" {
		return "/"
	}

	return site.Default.WebApp
}

// moveConsole takes the default outlet's browser application off the root when this
// outlet brings a second one: the generated router refuses an application at / beside
// another, since an installed application's scope is every URL under its start, so one
// at / would own the origin and the other would never get its own install prompt. The
// application moves to /<project>, named after its Angular project (console in the
// skeletons), and its API prefix to <project>/api: in the generator program
// (GenerateRoutes and its WebApp), the workspace (the project's baseHref and servePath),
// and the project's files (the proxy, the environments, the base element). The
// regenerated router then answers the root alone with a redirect to the application. The
// Go side and the prose that still name the old prefix are listed for the agent, each by
// file and line.
func (o Outlet) moveConsole(a *app.App, site *app.Site, ch *Change) error {
	if !site.GeneratedRouter || site.Default.WebApp != "/" {
		return nil
	}
	g := site.Generator
	oldPrefix := site.Default.Prefix
	project, w, found, err := consoleProject(a, g)
	if err != nil {
		return err
	}
	name := consoleName
	if found {
		name = project.Name
	}
	mount, prefix := "/"+name, name+"/"+apiSegment

	src, mode, err := readFile(a, g.File)
	if err != nil {
		return err
	}
	edited, err := app.MoveDefaultOutlet(g.File, src, prefix, mount)
	if err != nil {
		return err
	}
	if err := os.WriteFile(a.Abs(g.File), edited, mode); err != nil {
		return errors.Wrap(err, "os.WriteFile()")
	}
	ch.didf("%s: with two browser applications none is mounted at /, so the default outlet's application moves to %s and its API to /%s: GenerateRoutes(%q, %q, ..., WebApp(%q)) in place of %q and WebApp(\"/\"); the regenerated router answers the root alone with a redirect to %s/", g.File, mount, prefix, g.RoutesDir(), prefix, mount, oldPrefix, mount)

	if !found {
		ch.skipf("no browser project holds the default outlet's client, so nothing on the browser side moved; serve the application under %s/ with its API at /%s", mount, prefix)
	} else if err := o.mountProject(a, w.Dir, &project, oldPrefix, prefix, mount, ch); err != nil {
		return err
	}

	return listOldPrefix(a, oldPrefix, prefix, mount, ch)
}

// consoleProject finds the browser project holding the default outlet's generated client:
// the project of the workspace whose directory contains the default GenerateTypescript
// target's. It reports false when no target, workspace, or project does.
func consoleProject(a *app.App, g *app.Generator) (project app.AngularProject, w app.WebApp, found bool, err error) {
	target := defaultTarget(g)
	if target == nil {
		return app.AngularProject{}, app.WebApp{}, false, nil
	}
	w, ok := a.WebAppFor(target.Dir)
	if !ok {
		return app.AngularProject{}, app.WebApp{}, false, nil
	}
	projects, err := a.ReadAngular(w.Dir)
	if err != nil {
		return app.AngularProject{}, app.WebApp{}, false, err
	}
	rel := strings.TrimPrefix(strings.TrimPrefix(target.Dir, w.Dir), "/")
	project, ok = app.ProjectFor(projects, rel)
	if !ok || project.Root == "" {
		return app.AngularProject{}, app.WebApp{}, false, nil
	}

	return project, w, true, nil
}

// mountProject moves the default outlet's browser project from the root under the mount
// path: its angular.json entry (baseHref and servePath) and its files (the proxy, the
// environments, the base element), rewritten in place.
func (o Outlet) mountProject(a *app.App, webDir string, project *app.AngularProject, oldPrefix, prefix, mount string, ch *Change) error {
	rel := path.Join(webDir, "angular.json")
	data, mode, err := readFile(a, rel)
	if err != nil {
		return err
	}
	out, err := mountAngularProject(data, project.Name, mount)
	if err != nil {
		ch.skipf("%s: the %s project could not be moved under %s (%v); set its baseHref to %s/ and its servePath to %s by hand", rel, project.Name, mount, err, mount, mount)
	} else if err := os.WriteFile(a.Abs(rel), out, mode); err != nil {
		return errors.Wrap(err, "os.WriteFile()")
	}
	dir := a.Abs(path.Join(webDir, project.Root))
	if err := copyProject(dir, dir, moveRewrites(oldPrefix, "/", prefix, mount)); err != nil {
		return err
	}
	ch.didf("%s/%s: the %s project serves under %s/ with its API at /%s (angular.json baseHref and servePath, the proxy, the environments, the base element)", webDir, project.Root, project.Name, mount, prefix)

	return nil
}

// mention is one file's lines that name something the move left for the agent.
type mention struct {
	file  string
	lines []int
}

// mentions collects line numbers per file and renders them sorted by file, one line each.
type mentions map[string][]int

func (m mentions) add(file string, line int) {
	m[file] = append(m[file], line)
}

func (m mentions) sorted() []mention {
	files := make([]string, 0, len(m))
	for f := range m {
		files = append(files, f)
	}
	sort.Strings(files)
	out := make([]mention, 0, len(files))
	for _, f := range files {
		out = append(out, mention{file: f, lines: m[f]})
	}

	return out
}

// lineList renders line numbers as "12" or "12, 40".
func lineList(lines []int) string {
	parts := make([]string, 0, len(lines))
	for _, l := range lines {
		parts = append(parts, strconv.Itoa(l))
	}

	return strings.Join(parts, ", ")
}

// rootMountRE matches the App serving the console's bundle at the root: the resource
// package's served browser app built with "/" as its mount path
// (resource.NewBrowserApp(dir, "/")), or the older spaassets.DeepLink call rewriting to
// "/"; loginURLRE captures the login page a data level's construction names.
var (
	rootMountRE = regexp.MustCompile(`(?:DeepLink|NewBrowserApp)\(.*"/"\)`)
	loginURLRE  = regexp.MustCompile(`LoginURL:\s*"(/[^"]*)"`)
)

// oldPrefixScan collects what still names the console's old API prefix or its old place
// after the move, for the brief: the App serving the console's bundle at the root (the
// served browser app's mount path, which its DeepLink and Assets pair delegate to), a
// LoginURL naming a page outside the mount path,
// the hand-written Go naming the old prefix as a string literal (routes, hooks, tests),
// and the README, process files, and environment template naming it.
type oldPrefixScan struct {
	// literal is the old prefix as a Go string literal opens it.
	literal string
	// proseRE matches the old prefix as a path in prose, not inside a longer path.
	proseRE *regexp.Regexp
	mount   string

	literals, deepLinks, prose mentions
	// loginURLs maps file:line to the login page it names.
	loginURLs map[string]string
}

func newOldPrefixScan(oldPrefix, mount string) *oldPrefixScan {
	return &oldPrefixScan{
		literal:   `"/` + oldPrefix,
		proseRE:   regexp.MustCompile(`(^|[^A-Za-z0-9./_-])/` + regexp.QuoteMeta(oldPrefix) + `([^A-Za-z0-9_-]|$)`),
		mount:     mount,
		literals:  mentions{},
		deepLinks: mentions{},
		prose:     mentions{},
		loginURLs: map[string]string{},
	}
}

// file reads one file's lines: hand-written Go for the deep link, the login page, and the
// literal; prose for the prefix as a path.
func (sc *oldPrefixScan) file(rel string, data []byte, isGo, isProse bool) {
	n := 0
	for line := range strings.Lines(string(data)) {
		n++
		switch {
		case isGo && rootMountRE.MatchString(line):
			sc.deepLinks.add(rel, n)
		case isGo && strings.Contains(line, sc.literal):
			sc.literals.add(rel, n)
		case isProse && sc.proseRE.MatchString(line):
			sc.prose.add(rel, n)
		}
		if m := loginURLRE.FindStringSubmatch(line); isGo && m != nil && !strings.HasPrefix(m[1], sc.mount+"/") {
			sc.loginURLs[rel+":"+strconv.Itoa(n)] = m[1]
		}
	}
}

// report records every mention as the agent's, deep links first, then login pages, Go
// literals, and prose, each file on one line.
func (sc *oldPrefixScan) report(ch *Change, oldPrefix, prefix string) {
	for _, m := range sc.deepLinks.sorted() {
		ch.skipf("%s:%s: serves the console's bundle at \"/\"; it moves to %q: resource.NewBrowserApp(cfg.ConsoleDist(), %q) serves it under the mount path, as a second browser application's does (a spaassets.DeepLink and Assets pair becomes that one type)", m.file, lineList(m.lines), sc.mount+"/", sc.mount)
	}
	keys := make([]string, 0, len(sc.loginURLs))
	for k := range sc.loginURLs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		ch.skipf("%s: LoginURL %q is the console's login page, which is now under %s/: %q", k, sc.loginURLs[k], sc.mount, sc.mount+sc.loginURLs[k])
	}
	for _, m := range sc.literals.sorted() {
		ch.skipf("%s:%s: names %q, the console's old prefix, in a string literal; the console's API is now /%s", m.file, lineList(m.lines), "/"+oldPrefix, prefix)
	}
	for _, m := range sc.prose.sorted() {
		ch.skipf("%s:%s: names /%s, the console's old prefix; the console's API is now /%s and its pages are under %s/", m.file, lineList(m.lines), oldPrefix, prefix, sc.mount)
	}
}

// listOldPrefix walks the application for what still names the console's old prefix or
// its old place after the move and records each as the agent's. Generated files, build
// products, and the repository's own directory are not read; the tree is opened as a
// root, so the walk stays inside it.
func listOldPrefix(a *app.App, oldPrefix, prefix, mount string, ch *Change) error {
	root, err := os.OpenRoot(a.Root)
	if err != nil {
		return errors.Wrap(err, "os.OpenRoot()")
	}
	defer root.Close()

	scan := newOldPrefixScan(oldPrefix, mount)
	err = fs.WalkDir(root.FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return errors.Wrap(err, "fs.WalkDir()")
		}
		if d.IsDir() {
			if p != "." && (skippedNames[d.Name()] || d.Name() == ".git") {
				return fs.SkipDir
			}

			return nil
		}
		name := d.Name()
		isGo := strings.HasSuffix(name, ".go") && !strings.HasPrefix(name, "zz_gen_")
		isProse := name == readmeFile || processFileRE.MatchString(name) || (a.EnvTemplate != "" && p == a.EnvTemplate)
		if !isGo && !isProse {
			return nil
		}
		data, err := root.ReadFile(filepath.FromSlash(p))
		if err != nil {
			return errors.Wrap(err, "os.Root.ReadFile()")
		}
		scan.file(p, data, isGo, isProse)

		return nil
	})
	if err != nil {
		return errors.Wrap(err, "fs.WalkDir()")
	}
	scan.report(ch, oldPrefix, prefix)

	return nil
}

// processFileRE matches the development process files, whose comments name the proxied
// prefix.
var processFileRE = regexp.MustCompile(`^(Procfile.*|process-compose.*\.ya?ml)$`)

// Meaning explains the outlet in this framework and names the wiring left to do.
func (o Outlet) Meaning() string {
	pascal := strings.ToUpper(o.Name[:1]) + o.Name[1:]
	upper := strings.ToUpper(o.Name)
	var b strings.Builder
	fmt.Fprintf(&b, "A router outlet is a second URL space on the same host. Structs annotated `@outlet(%s)` are served under `/%s` by the generated `generated%sRoutes`; naming only the new outlet takes a struct off the default outlet, and `@outlet(default, %s)` keeps it on both. The generated router (`GenerateRouter`) mounts the outlet from its declaration: its group, its not-found handler, and, for a session outlet, its login routes and its browser application, with the chain documented at the top of `zz_gen_router.go` and proven by `zz_gen_router_test.go`; an application that kept a hand-written router composes the group there instead. The generated tests prove the outlets' URL spaces are disjoint.\n\n", o.Name, o.Prefix, pascal, o.Name)
	if o.Sessions {
		auth := "the auth the console uses"
		if o.Auth != "" {
			auth = "the " + o.Auth + " auth"
		}
		fmt.Fprintf(&b, "This is a session outlet: a browser surface bound to %s (the program declares it with that auth's `Auth` and `WebApp(\"/%s\")`), so its people sign in under `/%s/user/login` and the generated router serves its browser application from `/%s/`. Give the App what the generated `Handlers` now requires: `%s()` returning the auth's session handlers (an auth's embedded session manager satisfies its handler interface, so the method returns it), and the `%sDeepLink` and `%sAssets` pair serving a dist directory from configuration (`APP_%s_DIST` defaulting to `web/dist/%s`), like the console's. A route of the outlet's own goes in the `%s` field of the application's `Hooks`, inside the outlet's guards. Decide which resources the %s outlet serves and annotate them; then run `go generate ./...`. Extend the integration tests: sign in under `/%s/user/login` and read `user-domains` and the permission digest there, and show a resource that is not a member answers not found under the prefix. If the outlet's audience is another population, add an auth for it (`impulse add auth`), point the outlet's `Auth` at it, and add its development login to the bootstrap identities.\n\n", auth, o.Name, o.Prefix, o.Name, pascal, pascal, pascal, upper, o.Name, pascal, o.Name, o.Prefix)
		fmt.Fprintf(&b, "The outlet serves the live routes (`/%s/live/renew`, `/%s/live/unsubscribe`, `/%s/live/token`) through the App's one `LiveService`, the live service the data level opens over the application's Firestore database, so it needs no live wiring of its own; its browser application's list and record pages opt in through the client packages' live option as the console's do.\n\n", o.Prefix, o.Prefix, o.Prefix)
		b.WriteString("Every browser outlet takes one shape: its API under its application's mount path (`/" + o.Name + "/api` under `/" + o.Name + "`), so the application's scope covers its own API, login, and callback routes; `impulse check` warns where an outlet's API sits elsewhere. With two browser applications none is mounted at `/`: an installed application's scope is every URL under its start, so one at `/` would own the origin and the other would never get its own install prompt, and the generator refuses the shape. A console that was alone at `/` therefore moved to `/console` with its API at `/console/api` (the generator program, the project's `baseHref` and `servePath`, its proxy, environments, and base element), and the regenerated router answers the root alone with a redirect to `/console/`. The App's `DeepLink` and `Assets` pair follows (the resource package's served browser app built with the mount path, `resource.NewBrowserApp(cfg.ConsoleDist(), \"/console\")`, which strips the mount prefix and rewrites the application's routes to its entry document, as the portal's reads in the reference), as do a `LoginURL` naming the console's login page, the hand-written routes, hooks, and tests that name `/api`, and the README and Procfile lines; the brief lists each by file and line.\n")

		return b.String()
	}
	fmt.Fprintf(&b, "This is an API-key outlet: a machine surface with no browser, declared with `APIKey()`, so the generated router composes its group with no session handling and no XSRF guard: `NoCaching`, `CompressionMiddleware`, then `%sAuth`. Give the App that middleware: it requires `Authorization: Bearer <key>`, compares it in constant time against a configured key (`APP_%s_API_KEY`; when unset, generate an ephemeral key at startup so the surface stays closed), and binds the request to a service identity in the session context the way the session middleware binds a browser to its user, so the handlers behind it run the same permission checks. Seed that service identity's roles in the bootstrap identities. Decide which resources the %s outlet serves and annotate them; then run `go generate ./...`. Extend the integration tests: the right key answers, a wrong key and no key answer unauthorized, and a resource that is not a member answers not found under the prefix.\n", pascal, upper, o.Name)

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
