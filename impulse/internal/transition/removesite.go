package transition

import (
	"context"
	"fmt"
	"go/format"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/impulse/internal/app"
	"github.com/cccteam/ccc/impulse/internal/check"
)

// RemoveSite removes a site from a multi-site application: its tree under apps/<name>/,
// its generator program and directive, its TypeScript target in the shared generator,
// its router collection in the union, and its processes. The remaining sites stay where
// they are: an application left with one site is a multi-site application of one, and
// nothing moves back to the root.
type RemoveSite struct {
	// Name is the site's name and directory under apps/.
	Name string
}

// Command is the impulse command line for the transition.
func (r RemoveSite) Command() string {
	return "impulse remove site " + r.Name
}

// Validate checks the site against the application before anything is changed.
func (r RemoveSite) Validate(a *app.App) error {
	if !siteNameRE.MatchString(r.Name) {
		return errors.Newf("site name %q: name the site in lowercase letters and digits, as its directory under %s/ is", r.Name, sitesDir)
	}
	p := a.Profile()
	if p.Layout != app.LayoutSites {
		return errors.Newf("the application is flat: its one site is the application, so there is no site to remove")
	}
	site := r.site(p)
	if site == nil {
		return errors.Newf("no site named %s (the sites are %s)", r.Name, siteNames(p))
	}
	if len(p.Sites) == 1 {
		return errors.Newf("%s is the application's only site; an application is at least one site", r.Name)
	}
	if !strings.HasPrefix(site.Dir, sitesDir+"/") {
		return errors.Newf("%s: the site's directory is not under %s/, so the tool cannot tell what to delete", site.Dir, sitesDir)
	}

	return nil
}

// site finds the named site in the profile, or nil.
func (r RemoveSite) site(p app.Profile) *app.Site {
	for i := range p.Sites {
		if p.Sites[i].Name == r.Name {
			return &p.Sites[i]
		}
	}

	return nil
}

// Apply makes the deterministic half: the site's tree, generator, directive, shared
// target, union element, and processes go, and go generate runs. The integration suite,
// the deployment configuration outside the repository, and the tables only the site
// declared are the agent's.
func (r RemoveSite) Apply(ctx context.Context, a *app.App, exec check.Execer) (*Change, error) {
	if err := r.Validate(a); err != nil {
		return nil, err
	}
	ch := &Change{Command: r.Command()}
	p := a.Profile()
	site := r.site(p)
	g := site.Generator
	modulePath := a.GoMod.Module.Mod.Path

	declarations := resourceFiles(a, g.ResourcePackageDir)
	if err := removeTree(a, site.Dir); err != nil {
		return nil, err
	}
	ch.didf("deleted %s: the %s site's main package, handlers, router, resources, authorization suite, and browser workspace", site.Dir, r.Name)
	if err := r.removeGenerator(a, g, ch); err != nil {
		return nil, err
	}
	for _, shared := range p.Shared {
		if err := r.removeSharedTarget(a, shared, site.Dir, ch); err != nil {
			return nil, err
		}
	}
	if err := r.leaveUnion(a, modulePath, site.Dir, ch); err != nil {
		return nil, err
	}
	r.removeProcesses(a, site.Dir, ch)
	if err := r.noteImports(a, modulePath, site.Dir, ch); err != nil {
		return nil, err
	}

	if out, err := exec.Run(ctx, a.Root, nil, "go", "generate", "./..."); err != nil {
		ch.skipf("go generate ./... failed; fix the cause and run it:\n%s", strings.TrimSpace(string(out)))
	} else {
		ch.didf("ran go generate ./...: every remaining site's generated code at its place, without the %s site's", r.Name)
	}
	remaining := make([]string, 0, len(p.Sites)-1)
	for i := range p.Sites {
		if p.Sites[i].Name != r.Name {
			remaining = append(remaining, p.Sites[i].Name)
		}
	}
	ch.skipf("test/integration served the %s site beside the others; take its server, its logins, and the cross-site assertions that named it out of the harness", r.Name)
	ch.skipf("deployment configuration outside the repository (Cloud Build path filters, Cloud Run source directories, CI paths) still builds %s and serves a host for the %s site; retire them", site.Dir, r.Name)
	if len(declarations) > 0 {
		ch.skipf("the %s site's resource declarations went with it (%s); a table only they declared is still in the schema: declare the resource in the site that serves it now, or write a migration dropping the table", r.Name, strings.Join(declarations, ", "))
	}
	if len(remaining) == 1 {
		ch.didf("the %s site is the one left; the application stays multi-site, with the site under %s/%s and the shared packages at the root", remaining[0], sitesDir, remaining[0])
	}

	return ch, nil
}

// resourceFiles lists the hand-written Go files of a resource package, root-relative.
func resourceFiles(a *app.App, dir string) []string {
	entries, err := os.ReadDir(a.Abs(dir))
	if err != nil {
		return nil
	}
	var files []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || strings.HasPrefix(name, "zz_gen_") {
			continue
		}
		files = append(files, path.Join(dir, name))
	}

	return files
}

// removeGenerator deletes the site's generator program directory and the directive that
// ran it.
func (r RemoveSite) removeGenerator(a *app.App, g *app.Generator, ch *Change) error {
	genDir := path.Dir(g.File)
	if err := removeTree(a, genDir); err != nil {
		return err
	}
	ch.didf("deleted %s: the %s site's generator", genDir, r.Name)
	needle := "./" + path.Base(genDir)
	for _, d := range a.GoGenerate {
		if !directiveRuns(d.Command, needle) {
			continue
		}
		data, mode, err := readFile(a, d.File)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue // it was in the deleted tree
			}

			return err
		}
		var out strings.Builder
		for line := range strings.Lines(string(data)) {
			if strings.HasPrefix(strings.TrimSpace(line), "//go:generate") && directiveRuns(line, needle) {
				continue
			}
			out.WriteString(line)
		}
		if err := os.WriteFile(a.Abs(d.File), []byte(out.String()), mode); err != nil {
			return errors.Wrap(err, "os.WriteFile()")
		}
		ch.didf("%s: no longer runs %s", d.File, needle)
	}

	return nil
}

// directiveRuns reports whether a directive's command line runs the generator directory,
// as a whole word.
func directiveRuns(command, dir string) bool {
	return regexp.MustCompile(`(^|\s)` + regexp.QuoteMeta(dir) + `(\s|$)`).MatchString(command)
}

// removeSharedTarget takes the site's TypeScript target out of a shared generator.
func (r RemoveSite) removeSharedTarget(a *app.App, shared *app.Generator, siteDir string, ch *Change) error {
	src, mode, err := readFile(a, shared.File)
	if err != nil {
		return err
	}
	edited := src
	removed := 0
	for _, t := range shared.TypescriptTargets() {
		if !strings.HasPrefix(t.Dir, siteDir+"/") {
			continue
		}
		var n int
		edited, n, err = app.RemoveOptions(shared.File, edited, "GenerateTypescript", t.Dir)
		if err != nil {
			return err
		}
		removed += n
	}
	if removed == 0 {
		return nil
	}
	if err := os.WriteFile(a.Abs(shared.File), edited, mode); err != nil {
		return errors.Wrap(err, "os.WriteFile()")
	}
	ch.didf("%s: no longer emits the shared TypeScript into the %s site", shared.File, r.Name)

	return nil
}

// leaveUnion takes the site's router collection out of the union: its aliased import and
// its element of the access.UnionCollection call.
func (r RemoveSite) leaveUnion(a *app.App, modulePath, siteDir string, ch *Change) error {
	rel, data, mode, err := findFileCalling(a, unionNeedle)
	if err != nil {
		return err
	}
	if rel == "" {
		ch.skipf("no package calls access.UnionCollection; take the %s site's router collection out of the roles' registry wherever it is reconciled", r.Name)

		return nil
	}
	text := string(data)
	importRE := regexp.MustCompile(`(?m)^\t(\w+) ` + regexp.QuoteMeta(fmt.Sprintf("%q", modulePath+"/"+path.Join(siteDir, "pkg/router"))) + `\n`)
	m := importRE.FindStringSubmatch(text)
	if m == nil {
		ch.skipf("%s: does not import %s/%s/pkg/router under an alias, so the %s site's collection was not taken out of the union; remove it by hand", rel, modulePath, siteDir, r.Name)

		return nil
	}
	alias := m[1]
	text = importRE.ReplaceAllString(text, "")
	element := alias + ".Collection()"
	text, ok := removeUnionElement(text, element)
	if !ok {
		ch.skipf("%s: the access.UnionCollection call has no %s element in the shape the tool edits; remove the %s site's collection by hand", rel, element, r.Name)
	}
	formatted, err := format.Source([]byte(text))
	if err != nil {
		return errors.Wrap(err, "format.Source()")
	}
	if err := os.WriteFile(a.Abs(rel), formatted, mode); err != nil {
		return errors.Wrap(err, "os.WriteFile()")
	}
	ch.didf("%s: the %s site's router collection is out of the union", rel, r.Name)

	return nil
}

// removeProcesses drops the Procfile processes that ran the site: its server and its
// browser application.
func (r RemoveSite) removeProcesses(a *app.App, siteDir string, ch *Change) {
	data, mode, err := readFile(a, "Procfile")
	if err != nil {
		ch.skipf("Procfile: not read (%v); remove the processes running the %s site", err, r.Name)

		return
	}
	mention := regexp.MustCompile(`(\./|\bcd )` + regexp.QuoteMeta(siteDir) + `([^A-Za-z0-9_-]|$)`)
	var out strings.Builder
	var removed []string
	for line := range strings.Lines(string(data)) {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "#") && mention.MatchString(trimmed) {
			name, _, _ := strings.Cut(trimmed, ":")
			removed = append(removed, strings.TrimSpace(name))

			continue
		}
		out.WriteString(line)
	}
	if len(removed) == 0 {
		ch.skipf("Procfile: no process runs %s; remove the %s site's processes if they exist under another shape, and the comments that describe them", siteDir, r.Name)

		return
	}
	if err := os.WriteFile(a.Abs("Procfile"), []byte(out.String()), mode); err != nil {
		ch.skipf("Procfile: not written (%v)", err)

		return
	}
	ch.didf("Procfile: removed the %s process(es); the comments that describe them are still there", strings.Join(removed, " and "))
}

// noteImports records the Go files, tests included, that still import the site's
// packages: the integration harness, most often, whose unwiring is the agent's.
func (r RemoveSite) noteImports(a *app.App, modulePath, siteDir string, ch *Change) error {
	needle := `"` + modulePath + "/" + siteDir + "/"
	root, err := os.OpenRoot(a.Root)
	if err != nil {
		return errors.Wrap(err, "os.OpenRoot()")
	}
	defer root.Close()
	var files []string
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
		data, err := root.ReadFile(filepath.FromSlash(p))
		if err != nil {
			return errors.Wrap(err, "os.Root.ReadFile()")
		}
		if strings.Contains(string(data), needle) {
			files = append(files, p)
		}

		return nil
	})
	if err != nil {
		return errors.Wrap(err, "fs.WalkDir()")
	}
	if len(files) > 0 {
		ch.skipf("these files still import the %s site's packages, so the tree does not build until they are unwired: %s", r.Name, strings.Join(files, ", "))
	}

	return nil
}

// Meaning explains the removal in this framework and names the unwiring left to do.
func (r RemoveSite) Meaning() string {
	var b strings.Builder
	fmt.Fprintf(&b, "A site is a stand-alone application on a host of its own under `apps/<site>/`, served by a process of its own and covered by the role migration through the union of every site's permission collection. The %s site's tree, generator, directive, shared TypeScript target, union element, and processes are gone; the remaining sites stay where they are. An application left with one site is a multi-site application of one: the site stays under `apps/`, the shared generator keeps emitting into it, and the union has one element. Nothing moves back to the root.\n\n", r.Name)
	b.WriteString("Left to unwire:\n\n")
	items := []string{
		fmt.Sprintf("The integration suite: `test/integration` served the %s site beside the others over the shared database; take its server, its logins, and the cross-site assertions that named it out of the harness, and any other file that still imports `apps/%s/...` (the brief lists them).", r.Name, r.Name),
		fmt.Sprintf("The tables only the %s site declared: its resource declarations went with its tree, but the schema still holds their tables. For each, declare the resource in the site that serves it now, or write a migration dropping the table.", r.Name),
		fmt.Sprintf("The deployment outside the repository: the build (Cloud Build path filters, Cloud Run source directories, CI paths) and the host that served the %s site.", r.Name),
		fmt.Sprintf("The words: the README and the Procfile comments that describe the %s site, and its people's roles in the roles files when no remaining site's permissions need them.", r.Name),
	}
	for i, item := range items {
		fmt.Fprintf(&b, "%d. %s\n", i+1, item)
	}
	b.WriteString("\nData consequence: none from the removal itself. The sites shared one schema, one policy store per auth, and one role configuration, so the remaining sites' data and logins are untouched; a table only the removed site declared stays until a migration drops it.\n")

	return b.String()
}
