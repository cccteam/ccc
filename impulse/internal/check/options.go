package check

import (
	"context"
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/cccteam/ccc/impulse/internal/app"
)

// options verifies that the generator programs declare one coherent option set and
// reports the set in force: layout, sites, tenancy, outlets, and targets. Every other
// obligation check and every transition reads the same profile, so this is where a
// program that contradicts itself, or the tree it writes into, is caught first.
type options struct{}

func (options) Name() string { return "options" }

func (options) Describe() string {
	return "the generator programs declare one coherent option set (layout, tenancy, outlets, targets)"
}

// singleOptions may be passed once per program; a repeat means the later call silently
// overrides the earlier.
var singleOptions = []string{
	"GenerateHandlers", "GenerateRoutes", "GenerateHandlerTests", "WithDomainRoute", "WithConcealedDomains",
	"WithRPC", "ApplicationName", "WithSpannerEmulatorVersion", "WithConsolidatedHandlers",
}

func (c options) Run(_ context.Context, env *Env) Result {
	a := env.App
	if len(a.Generators) == 0 {
		return skip(c.Name(), "no generator program")
	}
	for _, g := range a.Generators {
		if len(g.Problems) > 0 {
			return skip(c.Name(), "a generator program could not be read completely (see generator-program)")
		}
	}

	p := a.Profile()
	var details []string
	for _, g := range a.Generators {
		details = append(details, c.programFindings(a, g)...)
	}
	for i := range p.Sites {
		details = append(details, c.siteFindings(&p.Sites[i])...)
	}
	details = append(details, c.layoutFindings(p)...)
	details = append(details, c.tenancyFindings(p)...)

	if len(details) > 0 {
		return fail(c.Name(), fmt.Sprintf("%d option set problem(s)", len(details)), details...)
	}

	return passWithDetails(c.Name(), profileSummary(p), profileDetails(p)...)
}

// programFindings checks one program against itself and the tree: repeated
// single-valued options, referenced directories that do not exist, and local packages
// outside the module or without a directory.
func (options) programFindings(a *app.App, g *app.Generator) []string {
	var details []string
	for _, name := range singleOptions {
		if calls := g.OptionsNamed(name); len(calls) > 1 {
			details = append(details, fmt.Sprintf("%s: %s passed %d times; the last call silently wins", calls[1].Pos, name, len(calls)))
		}
	}

	dirs := []struct{ label, dir string }{
		{"resource package", g.ResourcePackageDir},
		{"GenerateHandlers", g.HandlersDir()},
		{"GenerateRoutes", g.RoutesDir()},
		{"WithRPC", g.RPCDir()},
	}
	for _, d := range dirs {
		if d.dir == "" {
			continue
		}
		if info, err := os.Stat(a.Abs(d.dir)); err != nil || !info.IsDir() {
			details = append(details, fmt.Sprintf("%s: %s names %s, which is not a directory in the tree", g.File, d.label, d.dir))
		}
	}

	modulePath := ""
	if a.GoMod != nil && a.GoMod.Module != nil {
		modulePath = a.GoMod.Module.Mod.Path
	}
	for _, pkg := range g.LocalPackages {
		if modulePath == "" || (pkg != modulePath && !strings.HasPrefix(pkg, modulePath+"/")) {
			continue // a third-party field-type package
		}
		dir := strings.TrimPrefix(strings.TrimPrefix(pkg, modulePath), "/")
		if dir == "" {
			dir = "."
		}
		if info, err := os.Stat(a.Abs(dir)); err != nil || !info.IsDir() {
			details = append(details, fmt.Sprintf("%s: local package %s has no directory %s in the module", g.File, pkg, dir))
		}
	}

	seen := map[string]string{}
	for _, t := range g.TypescriptTargets() {
		if first, ok := seen[t.Dir]; ok {
			details = append(details, fmt.Sprintf("%s: GenerateTypescript target %s repeats the target at %s", t.Pos, t.Dir, first))

			continue
		}
		seen[t.Dir] = t.Pos
	}

	return details
}

// siteFindings checks the agreements inside one site's program: handlers and routes come
// together, outlets need routes and distinct names, concealment needs tenancy, and every
// ForOutlet names a declared, session-serving outlet.
func (options) siteFindings(s *app.Site) []string {
	g := s.Generator
	var details []string
	if g.RoutesDir() == "" {
		details = append(details, fmt.Sprintf("%s: GenerateHandlers without GenerateRoutes; nothing serves the handlers", g.File))
		if len(s.Outlets) > 0 {
			details = append(details, fmt.Sprintf("%s: WithRouterOutlet requires GenerateRoutes", s.Outlets[0].Pos))
		}
	}
	if s.ConcealedDomains && !s.Tenanted() {
		details = append(details, fmt.Sprintf("%s: WithConcealedDomains without WithDomainRoute; there are no domains to conceal", g.File))
	}

	outlets := map[string]app.Outlet{}
	for _, o := range s.Outlets {
		if first, ok := outlets[o.Name]; ok {
			details = append(details, fmt.Sprintf("%s: outlet %q is declared again (first at %s)", o.Pos, o.Name, first.Pos))

			continue
		}
		outlets[o.Name] = o
	}
	for _, t := range g.TypescriptTargets() {
		if t.Outlet == "" {
			continue
		}
		o, ok := outlets[t.Outlet]
		switch {
		case !ok:
			details = append(details, fmt.Sprintf("%s: ForOutlet(%q) names an outlet the program does not declare", t.Pos, t.Outlet))
		case !o.ServesSessions:
			details = append(details, fmt.Sprintf("%s: ForOutlet(%q) names an outlet without ServesSessions; a browser app cannot bootstrap there", t.Pos, t.Outlet))
		}
	}

	return details
}

// layoutFindings checks the sites against the layout: one site at the root, or every
// site under apps/<site>/ with a distinct name.
func (options) layoutFindings(p app.Profile) []string {
	var details []string
	if p.Layout == app.LayoutFlat && len(p.Sites) > 1 {
		files := make([]string, 0, len(p.Sites))
		for _, s := range p.Sites {
			files = append(files, s.Generator.File)
		}

		return []string{fmt.Sprintf("%d sites share the flat layout (%s); a second site belongs under apps/<site>/", len(p.Sites), strings.Join(files, ", "))}
	}
	if p.Layout == app.LayoutFlat {
		return nil
	}
	byName := map[string]string{}
	for _, s := range p.Sites {
		if s.Dir == "." {
			details = append(details, fmt.Sprintf("%s: site packages are not under one apps/<site>/ directory (resources %s, handlers %s, routes %s)", s.Generator.File, s.Generator.ResourcePackageDir, s.Generator.HandlersDir(), s.Generator.RoutesDir()))

			continue
		}
		if first, ok := byName[s.Name]; ok {
			details = append(details, fmt.Sprintf("%s: site %s is also generated by %s", s.Generator.File, s.Name, first))

			continue
		}
		byName[s.Name] = s.Generator.File
	}

	return details
}

// tenancyFindings checks that the sites agree on tenancy: one schema means one tenancy
// model, so every site declares the same domain route and concealment.
func (options) tenancyFindings(p app.Profile) []string {
	if len(p.Sites) < 2 {
		return nil
	}
	first := &p.Sites[0]
	var details []string
	for i := range p.Sites[1:] {
		s := &p.Sites[i+1]
		if s.DomainRoute != first.DomainRoute || s.ConcealedDomains != first.ConcealedDomains {
			details = append(details, fmt.Sprintf("sites disagree on tenancy: %s is %s but %s is %s", first.Name, tenancy(first), s.Name, tenancy(s)))
		}
	}

	return details
}

// tenancy renders a site's tenancy declaration.
func tenancy(s *app.Site) string {
	if !s.Tenanted() {
		return "not tenanted"
	}
	if s.ConcealedDomains {
		return fmt.Sprintf("tenanted on %q (concealed)", s.DomainRoute)
	}

	return fmt.Sprintf("tenanted on %q", s.DomainRoute)
}

// profileSummary is the one-line option set: layout, sites, tenancy, outlets.
func profileSummary(p app.Profile) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s layout, %d site(s)", p.Layout, len(p.Sites))
	if p.Layout == app.LayoutSites {
		fmt.Fprintf(&b, " (%s)", strings.Join(p.SiteNames(), ", "))
		if len(p.Shared) > 0 {
			fmt.Fprintf(&b, " + %d shared", len(p.Shared))
		}
	}
	switch {
	case len(p.Sites) == 0:
		b.WriteString("; no site generator")
	default:
		b.WriteString("; " + tenancy(&p.Sites[0]))
	}
	var outlets []string
	for _, s := range p.Sites {
		for _, o := range s.Outlets {
			name := o.Name
			if o.ServesSessions {
				name += " (sessions)"
			}
			outlets = append(outlets, name)
		}
	}
	if len(outlets) == 0 {
		b.WriteString("; no outlets")
	} else {
		b.WriteString("; outlets " + strings.Join(outlets, ", "))
	}

	return b.String()
}

// profileDetails lists each generator with where it writes.
func profileDetails(p app.Profile) []string {
	details := make([]string, 0, len(p.Sites)+len(p.Shared))
	for _, s := range p.Sites {
		g := s.Generator
		parts := []string{"resources " + g.ResourcePackageDir, "handlers " + g.HandlersDir()}
		if routes, ok := g.Option("GenerateRoutes"); ok && len(routes.Args) == 2 {
			parts = append(parts, fmt.Sprintf("routes %s under /%s", g.RoutesDir(), path.Clean(routes.Args[1].Str)))
		}
		if tests := g.HandlerTestsDir(); tests != "" {
			parts = append(parts, "tests "+tests)
		}
		if rpc := g.RPCDir(); rpc != "" {
			parts = append(parts, "rpc "+rpc)
		}
		parts = append(parts, typescriptSummary(g)...)
		details = append(details, fmt.Sprintf("%s (%s): %s", s.Name, g.File, strings.Join(parts, ", ")))
	}
	for _, g := range p.Shared {
		parts := append([]string{"resources " + g.ResourcePackageDir}, typescriptSummary(g)...)
		details = append(details, fmt.Sprintf("shared (%s): %s", g.File, strings.Join(parts, ", ")))
	}

	return details
}

func typescriptSummary(g *app.Generator) []string {
	var parts []string
	for _, t := range g.TypescriptTargets() {
		if t.Outlet != "" {
			parts = append(parts, fmt.Sprintf("typescript %s (outlet %s)", t.Dir, t.Outlet))

			continue
		}
		parts = append(parts, "typescript "+t.Dir)
	}

	return parts
}
