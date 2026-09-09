package transition

import (
	"context"
	"encoding/json"
	"fmt"
	"go/format"
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

	"github.com/cccteam/ccc/impulse/internal/app"
	"github.com/cccteam/ccc/impulse/internal/check"
	"github.com/cccteam/ccc/impulse/internal/names"
)

// Site adds a site: a stand-alone application on a host of its own, with its own main
// package, handlers, router, resources, authorization suite, and browser workspace under
// apps/<name>/, served by a process of its own and covered by the role migration through
// the union of every site's permission collection. On a flat application the first site
// added promotes the layout: the existing site moves under apps/<First>/ (its import paths
// change), the generator program becomes that site's, a shared generator is laid in for
// what every site's browser application needs in the same shape, the configuration's
// served level becomes the site level, and the deployment's collection becomes the union.
// Everything existing belongs to the first site; the shared package starts empty.
type Site struct {
	// Name is the new site's lowercase name and directory under apps/.
	Name string
	// First is the name the existing site takes under apps/ when the application is
	// still flat. Required then, since the name is the site's directory for good;
	// refused once the application has sites.
	First string
}

// The sites directory and the shared resource package of a multi-site application.
const (
	sitesDir       = "apps"
	sharedPackage  = "pkg/sharedresources"
	generateDir    = "cmd/generate"
	sharedGenName  = "sharedgenerator"
	sharedTSSubdir = "shared"
	// SitesReference is the candidate a site transition hands the agent as its model.
	SitesReference = "sites"
)

var siteNameRE = regexp.MustCompile(`^[a-z][a-z0-9]*$`)

// Command is the impulse command line for the transition.
func (s Site) Command() string {
	cmd := "impulse add site " + s.Name
	if s.First != "" {
		cmd += " --first " + s.First
	}

	return cmd
}

// Validate checks the transition against the application before anything is changed.
func (s Site) Validate(a *app.App) error {
	if !siteNameRE.MatchString(s.Name) {
		return errors.Newf("site name %q: name the site in lowercase letters and digits, such as portal", s.Name)
	}
	p := a.Profile()
	if len(p.Sites) == 0 {
		return errors.New("no site generator: a site is a generator program that emits handlers, and the application has none")
	}
	for i := range p.Sites {
		if p.Sites[i].Name == s.Name {
			return errors.Newf("%s: the %s site already exists", p.Sites[i].Dir, s.Name)
		}
	}
	if _, err := os.Stat(a.Abs(path.Join(sitesDir, s.Name))); err == nil {
		return errors.Newf("%s/%s already exists", sitesDir, s.Name)
	}
	switch p.Layout {
	case app.LayoutFlat:
		if len(p.Sites) != 1 {
			return errors.Newf("%d site generators in a flat layout; promotion moves one site", len(p.Sites))
		}
		if s.First == "" {
			return errors.New("--first is required on a flat application: the existing site moves under apps/<first>/ and the name is its directory for good, so it is asked rather than defaulted (the console it serves is a natural answer)")
		}
		if !siteNameRE.MatchString(s.First) {
			return errors.Newf("first site name %q: name it in lowercase letters and digits", s.First)
		}
		if s.First == s.Name {
			return errors.Newf("--first %s names the new site too; the existing site and the new one are two sites", s.First)
		}
		if _, err := os.Stat(a.Abs(sitesDir)); err == nil {
			return errors.Newf("%s/ already exists on a flat application; promotion creates it", sitesDir)
		}
		if err := s.promotable(a, &p.Sites[0]); err != nil {
			return err
		}
	case app.LayoutSites:
		if s.First != "" {
			return errors.Newf("--first is for promotion, and the application already has sites (%s); the new site is copied from %s", siteNames(p), p.Sites[0].Name)
		}
	}

	return nil
}

// promotable checks that the flat site's parts are where the move expects them: under
// the root, not already under apps/.
func (Site) promotable(a *app.App, site *app.Site) error {
	g := site.Generator
	for _, dir := range []string{g.ResourcePackageDir, g.HandlersDir(), g.RoutesDir()} {
		if dir == "" || dir == "." || strings.HasPrefix(dir, sitesDir+"/") {
			return errors.Newf("%s: the generator's directories (%q, %q, %q) are not root-relative packages a promotion can move", g.File, g.ResourcePackageDir, g.HandlersDir(), g.RoutesDir())
		}
		if _, err := os.Stat(a.Abs(dir)); err != nil {
			return errors.Newf("%s: %s does not exist, so the site cannot be moved", g.File, dir)
		}
	}

	return nil
}

func siteNames(p app.Profile) string {
	list := make([]string, len(p.Sites))
	for i := range p.Sites {
		list[i] = p.Sites[i].Name
	}

	return strings.Join(list, ", ")
}

// Apply makes the deterministic half: the promotion when the application is flat, then
// the new site beside the first.
func (s Site) Apply(ctx context.Context, a *app.App, exec check.Execer) (*Change, error) {
	if err := s.Validate(a); err != nil {
		return nil, err
	}
	ch := &Change{Command: s.Command()}
	p := a.Profile()
	first := p.Sites[0]
	if p.Layout == app.LayoutFlat {
		if err := s.promote(a, &first, ch); err != nil {
			return nil, err
		}
		// The tree moved, so the application is read again before what reads it.
		moved, err := rediscover(a)
		if err != nil {
			return nil, err
		}
		p = moved.Profile()
		if len(p.Sites) != 1 || p.Layout != app.LayoutSites {
			return nil, errors.Newf("after the promotion the profile reads %d site(s) in the %s layout; the move did not land where the generator reads", len(p.Sites), p.Layout)
		}
		if err := s.union(moved, moved.GoMod.Module.Mod.Path, p.Sites[0].Name, ch); err != nil {
			return nil, err
		}
		if err := s.sharedGenerator(moved, ch); err != nil {
			return nil, err
		}
		a, err = rediscover(moved)
		if err != nil {
			return nil, err
		}
		first = a.Profile().Sites[0]
	}
	if err := s.copySite(a, &first, ch); err != nil {
		return nil, err
	}
	if out, err := exec.Run(ctx, a.Root, nil, "go", "generate", "./..."); err != nil {
		ch.skipf("go generate ./... failed; fix the cause and run it:\n%s", strings.TrimSpace(string(out)))
	} else {
		ch.didf("ran go generate ./...: the %s site's generated code, and every site's at its place", s.Name)
	}
	ch.skipf("test/integration serves the %s site only; serve the %s site beside it over the same database, as the reference's harness does, and prove that a login on one site opens the other only where the auth is shared", first.Name, s.Name)
	ch.skipf("deployment configuration outside the repository (Cloud Build path filters, Cloud Run source directories, CI paths) now has %s/%s to build and a host for the %s site to serve", sitesDir, s.Name, s.Name)

	return ch, nil
}

// rediscover reads the application again after its tree changed.
func rediscover(a *app.App) (*app.App, error) {
	fresh, err := app.Discover(a.Root)
	if err != nil {
		return nil, errors.Wrap(err, "app.Discover()")
	}

	return fresh, nil
}

// promote moves the flat site under apps/<First>/: its packages, main package, and
// browser workspace, with every import following; the generator program under the site's
// name; the served level as the site level; and the processes and environment template.
func (s Site) promote(a *app.App, site *app.Site, ch *Change) error {
	g := site.Generator
	base := path.Join(sitesDir, s.First)
	modulePath := a.GoMod.Module.Mod.Path

	// The site's packages, its main package, and its browser workspace move.
	dirs := []string{g.ResourcePackageDir, g.HandlersDir(), g.RoutesDir()}
	if tests := g.HandlerTestsDir(); tests != "" {
		dirs = append(dirs, tests)
	}
	webDir := ""
	if target := defaultTarget(g); target != nil {
		if w, ok := a.WebAppFor(target.Dir); ok && w.Dir != "." {
			webDir = w.Dir
			dirs = append(dirs, w.Dir)
		}
	}
	sort.Strings(dirs)
	moved := []string{}
	for _, dir := range dirs {
		if err := moveTree(a, dir, path.Join(base, dir)); err != nil {
			return err
		}
		moved = append(moved, dir)
	}
	mains, err := rootMainFiles(a)
	if err != nil {
		return err
	}
	for _, f := range mains {
		if err := moveTree(a, f, path.Join(base, f)); err != nil {
			return err
		}
	}
	if len(mains) == 0 {
		ch.skipf("no main package at the root to move to %s; give the %s site its main.go there", base, s.First)
	}
	ch.didf("%s: the %s site, moved from the root: %s%s", base, s.First, strings.Join(moved, ", "), mainsNote(mains))

	// Every import of a moved package follows it.
	rewrites := []rewrite{}
	for _, dir := range dirs {
		if dir == webDir {
			continue
		}
		rewrites = append(rewrites,
			rewrite{`"` + modulePath + "/" + dir + `"`, `"` + modulePath + "/" + path.Join(base, dir) + `"`},
			rewrite{`"` + modulePath + "/" + dir + "/", `"` + modulePath + "/" + path.Join(base, dir) + "/"},
		)
	}
	rewritten, err := rewriteGoFiles(a, rewrites)
	if err != nil {
		return err
	}
	ch.didf("%d Go file(s): imports of the moved packages now name %s/%s/...", rewritten, modulePath, base)
	if webDir != "" {
		if err := s.rewriteWebBoundary(a, modulePath, webDir, base); err != nil {
			return err
		}
		s.nameWorkspace(a, path.Join(base, webDir), ch)
	}

	// The generator program becomes the site's.
	if err := s.moveGenerator(a, g, base, dirs, ch); err != nil {
		return err
	}
	// The served level becomes the site level.
	s.siteLevel(a, ch)
	// The development processes and the environment template follow.
	s.promoteProcfile(a, base, webDir, ch)
	s.promoteEnvTemplate(a, ch)

	return nil
}

func mainsNote(mains []string) string {
	if len(mains) == 0 {
		return ""
	}

	return ", " + strings.Join(mains, ", ")
}

// moveTree moves a file or directory to a new root-relative path, creating the parents.
func moveTree(a *app.App, from, to string) error {
	if err := os.MkdirAll(filepath.Dir(a.Abs(to)), 0o755); err != nil {
		return errors.Wrap(err, "os.MkdirAll()")
	}
	if err := os.Rename(a.Abs(from), a.Abs(to)); err != nil {
		return errors.Wrapf(err, "os.Rename(%s, %s)", from, to)
	}

	return nil
}

// rootMainFiles lists the non-test Go files of the root's main package, when the root is
// one: what serves the flat site.
func rootMainFiles(a *app.App) ([]string, error) {
	entries, err := os.ReadDir(a.Root)
	if err != nil {
		return nil, errors.Wrap(err, "os.ReadDir()")
	}
	var files []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") {
			continue
		}
		data, err := os.ReadFile(a.Abs(name))
		if err != nil {
			return nil, errors.Wrap(err, "os.ReadFile()")
		}
		if pkg, err := app.PackageName(name, data); err == nil && pkg == "main" {
			files = append(files, name)
		}
	}

	return files, nil
}

// rewriteGoFiles applies the textual rewrites to every Go file of the application, and
// returns how many changed.
func rewriteGoFiles(a *app.App, rewrites []rewrite) (int, error) {
	root, err := os.OpenRoot(a.Root)
	if err != nil {
		return 0, errors.Wrap(err, "os.OpenRoot()")
	}
	defer root.Close()
	changed := 0
	err = fs.WalkDir(root.FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return errors.Wrap(err, "fs.WalkDir()")
		}
		if d.IsDir() {
			if p != "." && skippedNames[d.Name()] {
				return fs.SkipDir
			}

			return nil
		}
		if !strings.HasSuffix(d.Name(), ".go") {
			return nil
		}
		name := filepath.FromSlash(p)
		data, err := root.ReadFile(name)
		if err != nil {
			return errors.Wrap(err, "os.Root.ReadFile()")
		}
		text := string(data)
		edited := text
		for _, r := range rewrites {
			edited = strings.ReplaceAll(edited, r.old, r.new)
		}
		if edited == text {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return errors.Wrap(err, "fs.DirEntry.Info()")
		}
		if err := root.WriteFile(name, []byte(edited), info.Mode()); err != nil {
			return errors.Wrap(err, "os.Root.WriteFile()")
		}
		changed++

		return nil
	})
	if err != nil {
		return 0, errors.Wrap(err, "fs.WalkDir()")
	}

	return changed, nil
}

// exists reports whether a root-relative path is there.
func exists(a *app.App, rel string) bool {
	_, err := os.Stat(a.Abs(rel))

	return err == nil
}

// rewriteWebBoundary renames the browser workspace's module boundary (its go.mod, there
// only to keep Go tooling out of node_modules) after the move.
func (Site) rewriteWebBoundary(a *app.App, modulePath, webDir, base string) error {
	rel := path.Join(base, webDir, "go.mod")
	if !exists(a, rel) {
		return nil // no boundary module: nothing to rename
	}
	data, mode, err := readFile(a, rel)
	if err != nil {
		return err
	}
	text := strings.Replace(string(data), "module "+modulePath+"/"+webDir, "module "+modulePath+"/"+path.Join(base, webDir), 1)
	if err := os.WriteFile(a.Abs(rel), []byte(text), mode); err != nil {
		return errors.Wrap(err, "os.WriteFile()")
	}

	return nil
}

// moveGenerator makes the flat generator program the first site's: its directory takes
// the site's name, its string arguments follow the moved directories, and the directive
// runs it under the new name.
func (s Site) moveGenerator(a *app.App, g *app.Generator, base string, dirs []string, ch *Change) error {
	oldDir := path.Dir(g.File)
	newDir := path.Join(path.Dir(oldDir), s.First+"generator")
	if oldDir != newDir {
		if _, err := os.Stat(a.Abs(newDir)); err == nil {
			return errors.Newf("%s already exists; the %s site's generator would move there", newDir, s.First)
		}
		if err := moveTree(a, oldDir, newDir); err != nil {
			return err
		}
	}
	file := path.Join(newDir, path.Base(g.File))
	data, mode, err := readFile(a, file)
	if err != nil {
		return err
	}
	text := string(data)
	for _, dir := range dirs {
		text = strings.ReplaceAll(text, `"`+dir+`"`, `"`+path.Join(base, dir)+`"`)
		text = strings.ReplaceAll(text, `"`+dir+`/`, `"`+path.Join(base, dir)+`/`)
	}
	if err := os.WriteFile(a.Abs(file), []byte(text), mode); err != nil {
		return errors.Wrap(err, "os.WriteFile()")
	}
	ch.didf("%s: the %s site's generator (was %s), reading and writing under %s", file, s.First, g.File, base)

	// The directive follows the directory.
	if oldDir != newDir {
		for _, d := range a.GoGenerate {
			if !strings.Contains(d.Command, path.Base(oldDir)) {
				continue
			}
			src, mode, err := readFile(a, d.File)
			if err != nil {
				return err
			}
			edited := strings.ReplaceAll(string(src), "./"+path.Base(oldDir), "./"+path.Base(newDir))
			if err := os.WriteFile(a.Abs(d.File), []byte(edited), mode); err != nil {
				return errors.Wrap(err, "os.WriteFile()")
			}
			ch.didf("%s: runs ./%s", d.File, path.Base(newDir))
		}
	}

	return nil
}

// The served level's names in the base and in the multi-site shape: the level is declared
// once and every site's process supplies its own values, so nothing in it names one site.
var siteLevelRewrites = []rewrite{
	{"ServerConfiguration", "SiteConfiguration"},
	{"serverConfig", "siteConfig"},
	{"ConsoleDist", "Dist"},
	{"consoleDist", "dist"},
	{`env:"APP_CONSOLE_DIST,default=web/dist/console"`, `env:"APP_DIST,required"`},
}

// siteLevel turns the served level into the site level across the Go files, and renames
// its file when it is the base's.
func (Site) siteLevel(a *app.App, ch *Change) {
	changed, err := rewriteGoFiles(a, siteLevelRewrites)
	if err != nil {
		ch.skipf("the served level could not be rewritten as the site level (%v); make ServerConfiguration the SiteConfiguration reading PORT and APP_DIST per site", err)

		return
	}
	if _, err := os.Stat(a.Abs("pkg/config/server.go")); err == nil {
		if err := moveTree(a, "pkg/config/server.go", "pkg/config/site.go"); err == nil {
			ch.didf("pkg/config/site.go (was server.go): the site level, SiteConfiguration reading PORT and APP_DIST per site process; %d file(s) follow the rename", changed)

			return
		}
	}
	if changed > 0 {
		ch.didf("%d file(s): the served level is the site level, SiteConfiguration reading PORT and APP_DIST per site process", changed)
	} else {
		ch.skipf("no file declares the base's ServerConfiguration; make the served level a site level reading PORT and APP_DIST per site process")
	}
}

var (
	// goRunRootRE is the flat Procfile's serve command.
	goRunRootRE = regexp.MustCompile(`(\bgo run(?: -[^ ]+)* )\.(['"\s]|$)`)
	// envPortRE is the environment template's PORT export.
	envPortRE = regexp.MustCompile(`(?m)^export PORT=(\d+)\n`)
	// portOnLineRE finds a PORT assignment on a process line.
	portOnLineRE = regexp.MustCompile(`\bPORT=(\d+)`)
	// cdWebRE is a process line's change into the browser workspace.
	cdWebRE = regexp.MustCompile(`\bcd ([A-Za-z0-9_./-]+) && `)
	// serverProcessRE is the base's serve process name.
	serverProcessRE = regexp.MustCompile(`(?m)^server:`)
	// distAssignRE and startScriptRE are what a copied process line carries of the first site.
	distAssignRE  = regexp.MustCompile(`APP_DIST=[^ ]+`)
	startScriptRE = regexp.MustCompile(`start:\w+`)
	// siteRouterImportRE is an aliased site router import in the deploy package.
	siteRouterImportRE = regexp.MustCompile(`(?m)^\t\w+router "[^"]+"\n`)
)

// promoteProcfile makes the flat serve process the first site's: it runs apps/<first>
// with the site's PORT, APP_DIST, and APP_SERVICE_NAME inline, and the browser process
// changes into the moved workspace. Returns the first site's port.
func (s Site) promoteProcfile(a *app.App, base, webDir string, ch *Change) {
	data, mode, err := readFile(a, "Procfile")
	if err != nil {
		ch.skipf("Procfile: not read (%v); run the %s site with go run ./%s and its PORT, APP_DIST, and APP_SERVICE_NAME", err, s.First, base)

		return
	}
	port := s.templatePort(a)
	text := string(data)
	dist := path.Join(base, "dist")
	if webDir != "" {
		dist = path.Join(base, webDir, "dist", s.webProject(a, base, webDir))
	}
	edited := goRunRootRE.ReplaceAllString(text, fmt.Sprintf("APP_SERVICE_NAME=%s PORT=%d APP_DIST=%s ${1}./%s${2}", s.First, port, dist, base))
	edited = serverProcessRE.ReplaceAllString(edited, s.First+":")
	if webDir != "" {
		edited = cdWebRE.ReplaceAllStringFunc(edited, func(m string) string {
			sm := cdWebRE.FindStringSubmatch(m)
			if path.Clean(sm[1]) != webDir {
				return m
			}

			return fmt.Sprintf("cd %s && ", path.Join(base, webDir))
		})
		edited = strings.ReplaceAll(edited, "&& bun run start:", fmt.Sprintf("&& PORT=%d bun run start:", port))
		edited = strings.ReplaceAll(edited, "/dev/tcp/127.0.0.1/${PORT})", fmt.Sprintf("/dev/tcp/127.0.0.1/%d)", port))
	}
	if edited == text {
		ch.skipf("Procfile: no `go run .` to make the %s site's process; run go run ./%s with PORT=%d, APP_DIST=%s, and APP_SERVICE_NAME=%s", s.First, base, port, dist, s.First)

		return
	}
	if err := os.WriteFile(a.Abs("Procfile"), []byte(edited), mode); err != nil {
		ch.skipf("Procfile: not written (%v)", err)

		return
	}
	ch.didf("Procfile: the %s process runs ./%s with PORT=%d, APP_DIST=%s, and APP_SERVICE_NAME=%s inline; the browser process changes into %s", s.First, base, port, dist, s.First, path.Join(base, webDir))
}

// templatePort reads the flat PORT from the environment template, or the base's default.
func (Site) templatePort(a *app.App) int {
	if a.EnvTemplate == "" {
		return 8080
	}
	data, _, err := readFile(a, a.EnvTemplate)
	if err != nil {
		return 8080
	}
	if m := envPortRE.FindSubmatch(data); m != nil {
		if p, err := strconv.Atoi(string(m[1])); err == nil {
			return p
		}
	}

	return 8080
}

// webProject is the name of the site's browser project: the one its generator writes into.
func (Site) webProject(a *app.App, base, webDir string) string {
	if webDir == "" {
		return ""
	}
	projects, err := a.ReadAngular(path.Join(base, webDir))
	if err != nil || len(projects) == 0 {
		return ""
	}

	return projects[0].Name
}

// promoteEnvTemplate moves PORT and the bundle directory out of the shared template: they
// differ per site, so each site's process sets them.
func (s Site) promoteEnvTemplate(a *app.App, ch *Change) {
	if a.EnvTemplate == "" {
		return
	}
	data, mode, err := readFile(a, a.EnvTemplate)
	if err != nil {
		return
	}
	text := string(data)
	edited := envPortRE.ReplaceAllString(text, "# PORT and APP_DIST differ per site, so the Procfile sets them per process.\n# export PORT=\n")
	edited = regexp.MustCompile(`(?m)^# export APP_CONSOLE_DIST=.*\n`).ReplaceAllString(edited, "# export APP_DIST=\n")
	edited = strings.ReplaceAll(edited, "# --- server: the served application ---", "# --- site: one served site; per process, set by the Procfile ---")
	if edited == text {
		return
	}
	if err := os.WriteFile(a.Abs(a.EnvTemplate), []byte(edited), mode); err != nil {
		ch.skipf("%s: not written (%v)", a.EnvTemplate, err)

		return
	}
	ch.didf("%s: PORT and APP_DIST are per site now, set by the Procfile per process", a.EnvTemplate)
}

// unionAnchorRE is the base deploy package's use of the one router's collection.
var unionAnchorRE = regexp.MustCompile(`\brouter\.Collection\(\)`)

// unionNeedle is the call every multi-site deploy package makes: the union of the
// sites' router collections, with one element per site.
const unionNeedle = "access.UnionCollection("

// errorsImport is the error package the generated Collection() wraps with.
const errorsImport = "github.com/go-playground/errors/v5"

// union makes the deployment's collection the union of every named site's router
// collection: the aliased import, Collection() over access.UnionCollection, and the role
// migration reading Collection() before it reconciles.
func (s Site) union(a *app.App, modulePath, first string, ch *Change) error {
	rel, data, mode, err := findFileCalling(a, "access.MigrateRoles")
	if err != nil {
		return err
	}
	if rel == "" || !unionAnchorRE.Match(data) {
		ch.skipf("no package calling access.MigrateRoles passes router.Collection(), so the collection was not made the union; reconcile the roles against access.UnionCollection over every site's router collection, as the reference's pkg/deploy does")

		return nil
	}
	text := string(data)
	firstImport := `"` + modulePath + "/" + path.Join(sitesDir, first, "pkg/router") + `"`
	if !strings.Contains(text, firstImport) {
		ch.skipf("%s: does not import %s, so the collection was not made the union; reconcile the roles against access.UnionCollection over every site's router collection", rel, firstImport)

		return nil
	}
	alias := first + "router"
	text = strings.Replace(text, "\t"+firstImport, "\t"+alias+" "+firstImport, 1)
	text = readCollectionBefore(text, unionAnchorRE)
	text, imported := ensureImport(text, errorsImport)
	text = strings.TrimRight(text, "\n") + fmt.Sprintf(`

// Collection is the application's whole permission registry: the union of every site's
// generated collection. The sites share one policy store, so the roles are reconciled
// against everything any site registers; a resource several sites serve is declared
// identically in each and appears once, and access.UnionCollection refuses sites that
// disagree on one.
func Collection() (access.PermissionCollection, error) {
	collection, err := access.UnionCollection(%s.Collection())
	if err != nil {
		return nil, errors.Wrap(err, "access.UnionCollection()")
	}

	return collection, nil
}
`, alias)
	formatted, err := format.Source([]byte(text))
	if err != nil {
		return errors.Wrap(err, "format.Source()")
	}
	if err := os.WriteFile(a.Abs(rel), formatted, mode); err != nil {
		return errors.Wrap(err, "os.WriteFile()")
	}
	if !imported {
		ch.skipf("%s: has no import block to add %q to; import it for Collection()", rel, errorsImport)
	}
	ch.didf("%s: the roles are reconciled against Collection(), access.UnionCollection over every site's router collection (the %s site's to start)", rel, first)

	return nil
}

// readCollectionBefore rewrites the statement using the anchored expression to read
// Collection() first: the statement's line is preceded by the read and its error
// check, at the same indentation, and the expression becomes the read's result.
func readCollectionBefore(text string, anchor *regexp.Regexp) string {
	loc := anchor.FindStringIndex(text)
	if loc == nil {
		return text
	}
	lineStart := strings.LastIndex(text[:loc[0]], "\n") + 1
	indent := text[lineStart : lineStart+len(text[lineStart:])-len(strings.TrimLeft(text[lineStart:], "\t "))]
	read := indent + "collection, err := Collection()\n" +
		indent + "if err != nil {\n" +
		indent + "\treturn err\n" +
		indent + "}\n\n"
	rest := anchor.ReplaceAllString(text[lineStart:], "collection")

	return text[:lineStart] + read + rest
}

// ensureImport adds importPath to the file's first import block when no import of it is
// there; ok is false when the file has no import block to add to.
func ensureImport(text, importPath string) (edited string, ok bool) {
	quoted := fmt.Sprintf("%q", importPath)
	if strings.Contains(text, quoted) {
		return text, true
	}
	i := strings.Index(text, "import (\n")
	if i < 0 {
		return text, false
	}
	at := i + len("import (\n")

	return text[:at] + "\t" + quoted + "\n" + text[at:], true
}

// unionArguments locates the arguments of the access.UnionCollection call in text: the
// indexes just inside its parentheses.
func unionArguments(text string) (start, end int, ok bool) {
	i := strings.Index(text, unionNeedle)
	if i < 0 {
		return 0, 0, false
	}
	start = i + len(unionNeedle)
	depth := 1
	for j := start; j < len(text); j++ {
		switch text[j] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return start, j, true
			}
		}
	}

	return 0, 0, false
}

// addUnionElement appends element to the access.UnionCollection call's arguments.
func addUnionElement(text, element string) (edited string, ok bool) {
	start, end, ok := unionArguments(text)
	if !ok {
		return text, false
	}
	arguments := strings.TrimSpace(text[start:end])
	if arguments != "" {
		arguments += ", "
	}

	return text[:start] + arguments + element + text[end:], true
}

// removeUnionElement takes element out of the access.UnionCollection call's arguments;
// ok is false when the call or the element is not there.
func removeUnionElement(text, element string) (edited string, ok bool) {
	start, end, ok := unionArguments(text)
	if !ok {
		return text, false
	}
	var kept []string
	found := false
	for _, argument := range strings.Split(text[start:end], ",") {
		switch argument = strings.TrimSpace(argument); argument {
		case "":
		case element:
			found = true
		default:
			kept = append(kept, argument)
		}
	}
	if !found {
		return text, false
	}

	return text[:start] + strings.Join(kept, ", ") + text[end:], true
}

// extendUnion adds a site's router collection to an existing union.
func (s Site) extendUnion(a *app.App, modulePath string, ch *Change) error {
	rel, data, mode, err := findFileCalling(a, unionNeedle)
	if err != nil {
		return err
	}
	if rel == "" {
		ch.skipf("no package calls access.UnionCollection, so the %s site's router collection is not in the roles' registry; add %s/%s/pkg/router's Collection() to the union", s.Name, sitesDir, s.Name)

		return nil
	}
	text := string(data)
	alias := s.Name + "router"
	importLine := fmt.Sprintf("\t%s %q\n", alias, modulePath+"/"+path.Join(sitesDir, s.Name, "pkg/router"))
	all := siteRouterImportRE.FindAllStringIndex(text, -1)
	if all == nil {
		ch.skipf("%s: no aliased site router import to add %s beside; import %s/%s/pkg/router and add its Collection() to the union", rel, alias, sitesDir, s.Name)

		return nil
	}
	last := all[len(all)-1]
	text = text[:last[1]] + importLine + text[last[1]:]
	text, ok := addUnionElement(text, alias+".Collection()")
	if !ok {
		ch.skipf("%s: the access.UnionCollection call is not in the shape the tool edits; add %s.Collection() to it by hand", rel, alias)

		return nil
	}
	formatted, err := format.Source([]byte(text))
	if err != nil {
		return errors.Wrap(err, "format.Source()")
	}
	if err := os.WriteFile(a.Abs(rel), formatted, mode); err != nil {
		return errors.Wrap(err, "os.WriteFile()")
	}
	ch.didf("%s: the %s site's router collection joins the union", rel, s.Name)

	return nil
}

// findFileCalling finds the first non-test, non-generated Go file whose text contains
// needle, with its contents and mode.
func findFileCalling(a *app.App, needle string) (rel string, data []byte, mode os.FileMode, err error) {
	for _, f := range a.GoFiles() {
		if strings.HasSuffix(f, "_test.go") || strings.HasPrefix(path.Base(f), "zz_gen_") {
			continue
		}
		if _, err := os.Stat(a.Abs(f)); err != nil {
			continue // moved since the scan
		}
		d, m, err := readFile(a, f)
		if err != nil {
			return "", nil, 0, err
		}
		if strings.Contains(string(d), needle) {
			return f, d, m, nil
		}
	}

	return "", nil, 0, nil
}

// sharedGenerator lays in the shared resource package and its generator: it reads
// pkg/sharedresources and emits its TypeScript into every site's browser application, and
// the directive runs it after the sites'.
func (s Site) sharedGenerator(a *app.App, ch *Change) error {
	modulePath := a.GoMod.Module.Mod.Path
	if err := writeNew(a, path.Join(sharedPackage, "sharedresources.go"), `// Package sharedresources holds what every site's browser application needs in the
// same shape: the enumerations. The shared generator reads this package and emits its
// TypeScript into each site's web application; no handlers or routes are generated
// from it, since each site serves its own resources from its own package. It starts
// empty: a type both sites' pages spell the same way moves here.
package sharedresources
`); err != nil {
		return err
	}
	migrations := "file://schema/migrations"
	if dir := appMigrationsDir(a); dir != "" {
		migrations = fileScheme + dir
	}
	p := a.Profile()
	var targets strings.Builder
	var sites []string
	for i := range p.Sites {
		target := defaultTarget(p.Sites[i].Generator)
		if target == nil {
			continue
		}
		dir := sharedTargetOf(target.Dir)
		if err := os.MkdirAll(a.Abs(dir), 0o755); err != nil {
			return errors.Wrap(err, "os.MkdirAll()")
		}
		fmt.Fprintf(&targets, "\t\tgeneration.GenerateTypescript(%q,\n\t\t\tgeneration.GenerateEnums(),\n\t\t),\n", dir)
		sites = append(sites, p.Sites[i].Name)
	}
	emulator := ""
	if len(p.Sites) > 0 && p.Sites[0].Generator.EmulatorVersion() != "" {
		emulator = fmt.Sprintf("\t\tgeneration.WithSpannerEmulatorVersion(%q),\n", p.Sites[0].Generator.EmulatorVersion())
	}
	program := fmt.Sprintf(`// Package main implements the shared generator: it reads %[1]s and emits
// its TypeScript into every site's web application, so the sites agree on the shared
// vocabulary. It generates no handlers or routes: each site serves its own resources.
package main

import (
	"context"
	"log"

	"github.com/cccteam/ccc/resource/generation"
	"github.com/go-playground/errors/v5"
)

func main() {
	if err := run(context.Background()); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context) error {
	generator, err := generation.NewResourceGenerator(
		ctx,
		%[1]q,
		[]string{%[2]q},
		[]string{
			%[3]q,
		},
%[4]s		// One TypeScript target per site: adding a site means adding its target here, and
		// impulse check fails the build if a site is missed.
%[5]s	)
	if err != nil {
		return errors.Wrap(err, "generation.NewResourceGenerator()")
	}
	defer generator.Close()

	if err := generator.Generate(); err != nil {
		return errors.Wrap(err, "generation.Generator.Generate()")
	}

	return nil
}
`, sharedPackage, migrations, modulePath+"/"+sharedPackage, emulator, targets.String())
	file := path.Join(generateDir, sharedGenName, "main.go")
	if err := writeNew(a, file, program); err != nil {
		return err
	}
	if err := s.addDirective(a, sharedGenName, ch); err != nil {
		return err
	}
	ch.didf("%s: the shared generator over %s (empty to start), emitting TypeScript into the %s site's browser application", file, sharedPackage, strings.Join(sites, " and "))

	return nil
}

// sharedTargetOf is where the shared generator writes a site's TypeScript: beside the
// site's own generated client, under shared/.
func sharedTargetOf(clientDir string) string {
	return path.Join(clientDir, sharedTSSubdir)
}

// addDirective appends a go:generate directive for a generator directory to the file that
// runs the others.
func (s Site) addDirective(a *app.App, genDir string, ch *Change) error {
	if len(a.GoGenerate) == 0 {
		ch.skipf("no //go:generate directive runs the generators; run ./%s from %s", genDir, generateDir)

		return nil
	}
	rel := a.GoGenerate[0].File
	data, mode, err := readFile(a, rel)
	if err != nil {
		return err
	}
	line := "//go:generate go run ./" + genDir
	text := string(data)
	if strings.Contains(text, line) {
		return nil
	}
	// The shared generator runs last; a site's generator goes before it.
	shared := "//go:generate go run ./" + sharedGenName
	if genDir != sharedGenName && strings.Contains(text, shared) {
		text = strings.Replace(text, shared, line+"\n"+shared, 1)
	} else {
		text = strings.TrimRight(text, "\n") + "\n" + line + "\n"
	}
	if err := os.WriteFile(a.Abs(rel), []byte(text), mode); err != nil {
		return errors.Wrap(err, "os.WriteFile()")
	}
	ch.didf("%s: runs ./%s", rel, genDir)

	return nil
}

// copySite lays in the new site beside the first: its hand-written Go files with the
// imports renamed, an empty resources package, its browser workspace, its generator and
// directive, its processes, and its place in the shared generator and the union.
func (s Site) copySite(a *app.App, first *app.Site, ch *Change) error {
	modulePath := a.GoMod.Module.Mod.Path
	from, to := first.Dir, path.Join(sitesDir, s.Name)
	g := first.Generator
	rewrites := []rewrite{
		{`"` + modulePath + "/" + from + "/", `"` + modulePath + "/" + to + "/"},
		{"the " + first.Name + " site", "the " + s.Name + " site"},
	}
	// Hand-written Go files of the site's packages, the resources kept back: every
	// resource belongs to the first site until someone moves it.
	for _, dir := range []string{g.HandlersDir(), g.RoutesDir(), g.HandlerTestsDir(), from} {
		if dir == "" {
			continue
		}
		if _, err := copyGoFiles(a, dir, strings.Replace(dir, from, to, 1), rewrites); err != nil {
			return err
		}
	}
	resourcesDir := strings.Replace(g.ResourcePackageDir, from, to, 1)
	if err := writeNew(a, path.Join(resourcesDir, "resources.go"), fmt.Sprintf(`// Package resources provides the resource types for the %s site. Each struct annotated
// with @resource describes one table of the schema; the generator derives the handlers,
// routes, permission collection, and TypeScript client from them. The site starts with
// none: a resource it serves is declared here (and identically in any other site that
// serves it).
package resources

import "github.com/cccteam/ccc/resource"

func defaultConfig() resource.Config {
	return resource.Config{
		TrackChanges: false,
	}
}
`, s.Name)); err != nil {
		return err
	}
	ch.didf("%s: the %s site, its main package, handlers, router, and authorization harness copied from the %s site with the imports renamed, and an empty resources package (every resource stays the %s site's until it is moved or declared in both)", to, s.Name, first.Name, first.Name)

	// The browser workspace.
	webDir, project, port := "", "", 0
	if target := defaultTarget(g); target != nil {
		if w, ok := a.WebAppFor(target.Dir); ok {
			webDir = w.Dir
			project, port = s.copyWorkspace(a, w.Dir, strings.Replace(w.Dir, from, to, 1), ch)
		}
	}
	// The generator.
	if err := s.copyGenerator(a, g, from, to, webDir, project, ch); err != nil {
		return err
	}
	// The processes.
	s.addProcesses(a, first, from, to, webDir, project, port, ch)
	// The shared generator's target and the union, when the application had sites already.
	if shared := a.SharedGenerators(); len(shared) > 0 {
		if err := s.addSharedTarget(a, shared[0], s.newClientDir(a, g, from, to, webDir, project), ch); err != nil {
			return err
		}
		if err := s.extendUnion(a, modulePath, ch); err != nil {
			return err
		}
	}

	return nil
}

// newClientDir is where the new site's generator writes its TypeScript client: the first
// site's client directory carried over, with the browser project renamed.
func (Site) newClientDir(a *app.App, g *app.Generator, from, to, webDir, project string) string {
	t := defaultTarget(g)
	if t == nil {
		return ""
	}
	dir := strings.Replace(t.Dir, from, to, 1)
	if webDir == "" || project == "" {
		return dir
	}
	if olds, err := a.ReadAngular(webDir); err == nil && len(olds) > 0 && olds[0].Root != project {
		newWeb := strings.Replace(webDir, from, to, 1)
		dir = strings.Replace(dir, path.Join(newWeb, olds[0].Root)+"/", path.Join(newWeb, project)+"/", 1)
	}

	return dir
}

// addSharedTarget gives the shared generator a TypeScript target in the new site's
// browser application, and creates the directory the generator opens before it writes.
func (s Site) addSharedTarget(a *app.App, shared *app.Generator, clientDir string, ch *Change) error {
	if clientDir == "" {
		ch.skipf("%s: the %s site has no TypeScript client directory to emit the shared TypeScript into; add its target", shared.File, s.Name)

		return nil
	}
	target := sharedTargetOf(clientDir)
	src, mode, err := readFile(a, shared.File)
	if err != nil {
		return err
	}
	if !strings.Contains(string(src), strconv.Quote(target)) {
		option := fmt.Sprintf("generation.GenerateTypescript(%q, generation.GenerateEnums())", target)
		edited, err := app.InsertOptions(shared.File, src, "GenerateTypescript", []string{option})
		switch {
		case err != nil:
			ch.skipf("%s: the %s site's TypeScript target was not added (%v); add %s", shared.File, s.Name, err, option)
		default:
			if err := os.WriteFile(a.Abs(shared.File), edited, mode); err != nil {
				return errors.Wrap(err, "os.WriteFile()")
			}
			ch.didf("%s: emits the shared TypeScript into the %s site too (%s)", shared.File, s.Name, target)
		}
	}
	if err := os.MkdirAll(a.Abs(target), 0o755); err != nil {
		return errors.Wrap(err, "os.MkdirAll()")
	}

	return nil
}

// copyGoFiles copies the hand-written, non-test-generated Go files of one directory to
// another with the rewrites applied, and returns how many.
func copyGoFiles(a *app.App, from, to string, rewrites []rewrite) (int, error) {
	entries, err := os.ReadDir(a.Abs(from))
	if err != nil {
		return 0, errors.Wrap(err, "os.ReadDir()")
	}
	n := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasPrefix(name, "zz_gen_") {
			continue
		}
		data, err := os.ReadFile(a.Abs(path.Join(from, name)))
		if err != nil {
			return 0, errors.Wrap(err, "os.ReadFile()")
		}
		text := string(data)
		for _, r := range rewrites {
			text = strings.ReplaceAll(text, r.old, r.new)
		}
		if err := writeNew(a, path.Join(to, name), text); err != nil {
			return 0, err
		}
		n++
	}

	return n, nil
}

// copyWorkspace copies the first site's browser workspace to the new site's, renaming
// its project after the site: the project directory, and the project's name where the
// workspace configuration spells it. Returns the project's new name and dev port.
func (s Site) copyWorkspace(a *app.App, from, to string, ch *Change) (project string, port int) {
	projects, err := a.ReadAngular(from)
	if err != nil || len(projects) == 0 {
		ch.skipf("%s: no Angular project to copy for the %s site (%v); create its browser workspace at %s", from, s.Name, err, to)

		return "", 0
	}
	old := projects[0]
	port = 0
	for _, w := range a.WebApps {
		ps, err := a.ReadAngular(w.Dir)
		if err != nil {
			continue
		}
		for _, p := range ps {
			port = max(port, p.DevPort)
		}
	}
	if port > 0 {
		port++
	}
	if err := copyProject(a.Abs(from), a.Abs(to), nil); err != nil {
		ch.skipf("%s: not copied to %s (%v); create the %s site's browser workspace", from, to, err, s.Name)

		return "", 0
	}
	// The project takes the site's name: its directory, and its name in the workspace
	// configuration (not in the sources, where the word may mean something else).
	project = old.Name
	if old.Name != s.Name && old.Root != "" && old.Root != "." && s.renameProject(a, to, old, port) {
		project = s.Name
	}
	ch.didf("%s: the %s site's browser workspace, a copy of %s with its project named %s on port %d; its titles and API prefix still say what the %s site's do", to, s.Name, from, project, port, old.Name)

	return project, port
}

// nameWorkspace gives the promoted site's browser workspace the site's name the way the
// sites skeleton spells it: <app>-web becomes <app>-<site>-web in package.json and bun.lock,
// so the second site's copy takes its own name from it. A workspace named some other way,
// or already for the site, keeps its name.
func (s Site) nameWorkspace(a *app.App, webDir string, ch *Change) {
	pkg, _, err := readFile(a, path.Join(webDir, "package.json"))
	if err != nil {
		return
	}
	from := workspaceName(pkg)
	stem, ok := strings.CutSuffix(from, "-web")
	if !ok || strings.HasSuffix(stem, "-"+s.First) {
		return
	}
	to := stem + "-" + s.First + "-web"
	renamed := renameWorkspaceFiles(a, webDir, from, to, ch)
	if len(renamed) > 0 {
		ch.didf("%s: the workspace is named %s (was %s)", strings.Join(renamed, ", "), to, from)
	}
}

// renameWorkspaceFiles renames the workspace in package.json and bun.lock, returning the
// files it changed.
func renameWorkspaceFiles(a *app.App, webDir, from, to string, ch *Change) []string {
	var renamed []string
	for _, name := range []string{"package.json", "bun.lock"} {
		rel := path.Join(webDir, name)
		data, mode, err := readFile(a, rel)
		if err != nil {
			continue
		}
		edited := names.RenameWorkspace(string(data), from, to)
		if edited == string(data) {
			continue
		}
		if err := os.WriteFile(a.Abs(rel), []byte(edited), mode); err != nil {
			ch.skipf("%s: not written (%v); name the workspace %s", rel, err, to)

			continue
		}
		renamed = append(renamed, rel)
	}

	return renamed
}

// workspaceName reads the name field of a package.json, or "" when it has none.
func workspaceName(pkg []byte) string {
	var manifest struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(pkg, &manifest); err != nil {
		return ""
	}

	return manifest.Name
}

// renameProject moves the copied project's directory to the site's name and renames the
// project where the workspace configuration spells it, with the next dev port in
// angular.json, and reports whether the move succeeded. The lockfile spells the workspace
// name too, and only there does the word mean the workspace (a dependency named like the
// site keeps its name), so it follows package.json's name rather than the word.
func (s Site) renameProject(a *app.App, to string, old app.AngularProject, port int) bool {
	if err := moveTree(a, path.Join(to, old.Root), path.Join(to, s.Name)); err != nil {
		return false
	}
	pkgBefore, _, _ := readFile(a, path.Join(to, "package.json"))
	word := regexp.MustCompile(`\b` + regexp.QuoteMeta(old.Name) + `\b`)
	for _, name := range []string{"angular.json", "package.json", "tsconfig.json", "eslint.config.js", ".prettierignore"} {
		rel := path.Join(to, name)
		data, mode, err := readFile(a, rel)
		if err != nil {
			continue
		}
		edited := word.ReplaceAllString(string(data), s.Name)
		if name == "angular.json" && old.DevPort > 0 && port > 0 {
			edited = strings.Replace(edited, fmt.Sprintf(`"port": %d`, old.DevPort), fmt.Sprintf(`"port": %d`, port), 1)
		}
		if edited != string(data) {
			_ = os.WriteFile(a.Abs(rel), []byte(edited), mode)
		}
	}
	pkgAfter, _, err := readFile(a, path.Join(to, "package.json"))
	if err != nil {
		return true
	}
	if wsFrom, wsTo := workspaceName(pkgBefore), workspaceName(pkgAfter); wsFrom != "" && wsFrom != wsTo {
		if lock, mode, err := readFile(a, path.Join(to, "bun.lock")); err == nil {
			_ = os.WriteFile(a.Abs(path.Join(to, "bun.lock")), []byte(names.RenameWorkspace(string(lock), wsFrom, wsTo)), mode)
		}
	}

	return true
}

// copyGenerator writes the new site's generator program as a copy of the first site's
// with the site's directories, and runs it from the directive.
func (s Site) copyGenerator(a *app.App, g *app.Generator, from, to, webDir, project string, ch *Change) error {
	data, _, err := readFile(a, g.File)
	if err != nil {
		return err
	}
	text := strings.ReplaceAll(string(data), `"`+from+"/", `"`+to+"/")
	text = strings.ReplaceAll(text, `"`+from+`"`, `"`+to+`"`)
	modulePath := a.GoMod.Module.Mod.Path
	text = strings.ReplaceAll(text, `"`+modulePath+"/"+from+"/", `"`+modulePath+"/"+to+"/")
	if webDir != "" && project != "" {
		oldProjects, err := a.ReadAngular(webDir)
		if err == nil && len(oldProjects) > 0 && oldProjects[0].Root != "" && oldProjects[0].Root != project {
			newWeb := strings.Replace(webDir, from, to, 1)
			text = strings.ReplaceAll(text, `"`+path.Join(newWeb, oldProjects[0].Root)+"/", `"`+path.Join(newWeb, project)+"/")
		}
	}
	text = strings.ReplaceAll(text, "the "+path.Base(from)+" site", "the "+s.Name+" site")
	genDir := s.Name + "generator"
	file := path.Join(path.Dir(path.Dir(g.File)), genDir, path.Base(g.File))
	if err := writeNew(a, file, text); err != nil {
		return err
	}
	if err := s.addDirective(a, genDir, ch); err != nil {
		return err
	}
	ch.didf("%s: the %s site's generator, a copy of %s reading and writing under %s", file, s.Name, g.File, to)

	return nil
}

// addProcesses adds the new site's serve process and browser process to the Procfile,
// copied from the first site's with the site's port and directories: it waits for the
// first site (so the bootstrap is done) instead of running the bootstrap itself.
func (s Site) addProcesses(a *app.App, first *app.Site, from, to, webDir, project string, webPort int, ch *Change) {
	data, mode, err := readFile(a, "Procfile")
	if err != nil {
		ch.skipf("Procfile: not read (%v); add a process running go run ./%s with its own PORT, APP_DIST, and APP_SERVICE_NAME", err, to)

		return
	}
	lines, err := processLinesOf(string(data))
	if err != nil {
		ch.skipf("Procfile: %v", err)

		return
	}
	serve, web, firstPort, port := firstSiteProcesses(lines, from, webDir)
	if serve == "" {
		ch.skipf("Procfile: no process runs go run ./%s to copy; add one running go run ./%s with its own PORT, APP_DIST, and APP_SERVICE_NAME", from, to)

		return
	}
	newServe := strings.Replace(serve, "./"+from, "./"+to, 1)
	newServe = strings.Replace(newServe, "APP_SERVICE_NAME="+first.Name, "APP_SERVICE_NAME="+s.Name, 1)
	newServe = portOnLineRE.ReplaceAllString(newServe, fmt.Sprintf("PORT=%d", port))
	newServe = distAssignRE.ReplaceAllString(newServe, "APP_DIST="+path.Join(to, strings.TrimPrefix(webDir, from+"/"), "dist", project))
	newServe = strings.Replace(newServe, "go run ./cmd/bootstrap && ", "", 1)
	newServe = strings.Replace(newServe, "${SPANNER_EMULATOR_PORT}", strconv.Itoa(firstPort), 1)
	name, _, _ := strings.Cut(newServe, ":")
	newServe = s.Name + strings.TrimPrefix(newServe, strings.TrimSpace(name))
	added := []string{newServe}
	if web != "" {
		added = append(added, s.webProcess(web, webDir, from, to, project, firstPort, port))
	}
	text := strings.TrimRight(string(data), "\n") + "\n" + strings.Join(added, "\n") + "\n"
	if err := os.WriteFile(a.Abs("Procfile"), []byte(text), mode); err != nil {
		ch.skipf("Procfile: not written (%v)", err)

		return
	}
	ch.didf("Procfile: the %s process serves ./%s on PORT=%d after the %s site is up, and %s-web serves its browser application on %d", s.Name, to, port, first.Name, s.Name, webPort)
}

// firstSiteProcesses finds the first site's serve and browser process lines, its port,
// and the next port no process uses.
func firstSiteProcesses(lines []string, from, webDir string) (serve, web string, firstPort, next int) {
	used := map[int]bool{}
	for _, l := range lines {
		if m := portOnLineRE.FindStringSubmatch(l); m != nil {
			p, _ := strconv.Atoi(m[1])
			used[p] = true
		}
	}
	for _, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), "#") {
			continue
		}
		if strings.Contains(l, "go run ./"+from+"'") || strings.Contains(l, "go run ./"+from+" ") || strings.HasSuffix(strings.TrimSpace(l), "go run ./"+from) {
			serve = l
			if m := portOnLineRE.FindStringSubmatch(l); m != nil {
				firstPort, _ = strconv.Atoi(m[1])
			}
		}
		if webDir != "" && strings.Contains(l, "cd "+webDir+" ") {
			web = l
		}
	}
	next = firstPort + 1
	for used[next] {
		next++
	}

	return serve, web, firstPort, next
}

// webProcess is the new site's browser process line, copied from the first site's.
func (s Site) webProcess(web, webDir, from, to, project string, firstPort, port int) string {
	newWeb := strings.Replace(web, "cd "+webDir+" ", "cd "+strings.Replace(webDir, from, to, 1)+" ", 1)
	newWeb = portOnLineRE.ReplaceAllString(newWeb, fmt.Sprintf("PORT=%d", port))
	newWeb = strings.Replace(newWeb, "/"+strconv.Itoa(firstPort)+")", "/"+strconv.Itoa(port)+")", 1)
	if project != "" {
		newWeb = startScriptRE.ReplaceAllString(newWeb, "start:"+project)
	}
	name, _, _ := strings.Cut(newWeb, ":")

	return s.Name + "-web" + strings.TrimPrefix(newWeb, strings.TrimSpace(name))
}

// processLinesOf splits a process file into lines, refusing binary content.
func processLinesOf(text string) ([]string, error) {
	if !utf8.ValidString(text) {
		return nil, errors.New("not a text file")
	}

	return strings.Split(text, "\n"), nil
}

// Meaning explains a site in this framework and names the wiring left to do.
func (s Site) Meaning() string {
	var b strings.Builder
	b.WriteString("A site is a stand-alone application on a host of its own: its own main package, handlers, router, resources, authorization suite, and browser workspace under `apps/<site>/`, served by a process of its own (its own `PORT`, `APP_DIST`, and `APP_SERVICE_NAME` over the shared data level) and covered by the role migration through the union of every site's permission collection. Multi-site is several hosts from one repository: the sites share the schema, the configuration levels, the auths, and `pkg/sharedresources`, whose TypeScript the shared generator emits into every site's browser application so they agree on the shared vocabulary.\n\n")
	if s.First != "" {
		fmt.Fprintf(&b, "The application was flat, so adding the %s site promoted the layout: the existing site moved under `apps/%s/` (every import of its packages changed), its generator became `cmd/generate/%sgenerator`, the served level became the site level (`SiteConfiguration` reading `PORT` and `APP_DIST` per site process), the deployment's collection became the union, and the shared generator was laid in over an empty `pkg/sharedresources`. Everything existing belongs to the %s site.\n\n", s.Name, s.First, s.First, s.First)
	}
	b.WriteString("Left to wire:\n\n")
	items := []string{
		fmt.Sprintf("Give the %s site its resources. Declare what it serves in `apps/%s/pkg/resources` (a resource another site serves too is declared identically in both; a type every site's pages spell the same way moves to `pkg/sharedresources`), and regenerate.", s.Name, s.Name),
		fmt.Sprintf("Serve it in the integration suite: `test/integration` serves the first site only; serve the %s site beside it over the same database, as the reference's harness does, and prove that a login on one site opens the other only where the auth is shared.", s.Name),
		fmt.Sprintf("Make the %s site's browser application its own: its titles, its pages, and the API prefix it proxies still say what the first site's do.", s.Name),
		fmt.Sprintf("Register it outside the repository: the deployment's build (Cloud Build path filters, Cloud Run source directories, CI paths) has `apps/%s` to build and a host for the %s site to serve%s.", s.Name, s.Name, s.deployNote()),
	}
	for i, item := range items {
		fmt.Fprintf(&b, "%d. %s\n", i+1, item)
	}
	b.WriteString("\nData consequence: none in the database. The sites share one schema, one policy store per auth, and one role configuration; a login is one identity across the sites an auth serves.\n")

	return b.String()
}

func (s Site) deployNote() string {
	if s.First == "" {
		return ""
	}

	return fmt.Sprintf(", and what built the root now builds `apps/%s`", s.First)
}
