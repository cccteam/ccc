package check

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/impulse/internal/app"
)

// outletWired verifies that every router outlet a program declares is wired through the
// application: the hand-written router mounts the outlet's generated routes (nothing
// else makes it call generated<Outlet>Routes), a session-serving outlet has a browser
// client generated for it, and that client's development proxy forwards the outlet's
// prefix. The generator holds the rest: an @outlet naming an undeclared outlet fails
// generation, and the outlets' URL spaces are proven disjoint by the generated tests.
type outletWired struct{}

func (outletWired) Name() string { return "outlet-wired" }

func (outletWired) Describe() string {
	return "every declared outlet is mounted by the router, and a session outlet has a browser client whose proxy reaches it"
}

// defaultOutlet is the reserved name of the outlet GenerateRoutes declares.
const defaultOutlet = "default"

// outletSurface is one outlet as the check sees it: the profile's declaration plus the
// generated function that mounts its routes.
type outletSurface struct {
	app.Outlet
	// Mount is the generated function the router must call.
	Mount string
	// Default marks the outlet GenerateRoutes declares.
	Default bool
}

func (c outletWired) Run(_ context.Context, env *Env) Result {
	a := env.App
	p := a.Profile()
	if len(p.Sites) == 0 {
		return skip(c.Name(), "no site generator")
	}

	var details, notes []string
	mounted := 0
	for i := range p.Sites {
		site := &p.Sites[i]
		g := site.Generator
		routesDir := g.RoutesDir()
		if routesDir == "" {
			continue // the options check reports handlers without routes
		}
		called, err := calledFunctions(a, routesDir)
		if err != nil {
			return fail(c.Name(), err.Error())
		}

		for _, o := range outletSurfaces(site) {
			o := &o
			if !called[o.Mount] {
				details = append(details, fmt.Sprintf("%s: no file in %s calls %s; the %s outlet's routes are not mounted", g.File, routesDir, o.Mount, o.Name))

				continue
			}
			mounted++
			if !o.Default && len(membersOf(a, o.Name)) == 0 {
				notes = append(notes, fmt.Sprintf("outlet %s has no @outlet(%s) members yet", o.Name, o.Name))
			}
			if !o.ServesSessions {
				continue
			}
			targets := targetsFor(g, o)
			if len(targets) == 0 && !o.Default {
				details = append(details, fmt.Sprintf("%s: outlet %s serves sessions, but no GenerateTypescript target names it (ForOutlet); no browser app can bootstrap there", o.Pos, o.Name))
			}
			for _, t := range targets {
				finding, err := proxyFinding(a, t, o)
				if err != nil {
					return fail(c.Name(), err.Error())
				}
				if finding != "" {
					details = append(details, finding)
				}
			}
		}
	}

	if len(details) > 0 {
		return fail(c.Name(), fmt.Sprintf("%d outlet wiring problem(s)", len(details)), append(details, notes...)...)
	}

	return passWithDetails(c.Name(), fmt.Sprintf("%d outlet(s) mounted: %s", mounted, strings.Join(outletList(p), "; ")), notes...)
}

// outletSurfaces lists a site's outlets with their generated mount functions: the
// default outlet first, then the declarations in order.
func outletSurfaces(site *app.Site) []outletSurface {
	prefix := ""
	if routes, ok := site.Generator.Option("GenerateRoutes"); ok && len(routes.Args) == 2 && routes.Args[1].Kind == app.ArgString {
		prefix = routes.Args[1].Str
	}
	surfaces := make([]outletSurface, 0, 1+len(site.Outlets))
	surfaces = append(surfaces, outletSurface{
		Outlet:  app.Outlet{Name: defaultOutlet, Prefix: prefix, ServesSessions: true},
		Mount:   "generatedRoutes",
		Default: true,
	})
	for _, o := range site.Outlets {
		surfaces = append(surfaces, outletSurface{Outlet: o, Mount: "generated" + pascal(o.Name) + "Routes"})
	}

	return surfaces
}

// pascal upper-cases the first letter of a lowerCamelCase outlet name, as the generator
// does for its identifiers.
func pascal(name string) string {
	if name == "" {
		return ""
	}

	return strings.ToUpper(name[:1]) + name[1:]
}

// membersOf lists the structs annotated onto the outlet.
func membersOf(a *app.App, outlet string) []app.OutletMember {
	var members []app.OutletMember
	for _, m := range a.OutletMembers {
		for _, name := range m.Outlets {
			if name == outlet {
				members = append(members, m)

				break
			}
		}
	}

	return members
}

// targetsFor lists the GenerateTypescript targets addressed to the outlet: those
// naming it with ForOutlet, or those naming no outlet for the default.
func targetsFor(g *app.Generator, o *outletSurface) []app.TSTarget {
	var targets []app.TSTarget
	for _, t := range g.TypescriptTargets() {
		if (o.Default && t.Outlet == "") || (!o.Default && t.Outlet == o.Name) {
			targets = append(targets, t)
		}
	}

	return targets
}

// proxyFinding checks that the browser project receiving a target forwards the outlet's
// prefix in its development proxy. A target outside a browser app, a project without a
// proxy configuration, or a missing proxy file is nothing to report here: prettier-ignore
// owns the first, and the other two mean the app is served some other way.
func proxyFinding(a *app.App, t app.TSTarget, o *outletSurface) (string, error) {
	w, ok := a.WebAppFor(t.Dir)
	if !ok {
		return "", nil
	}
	projects, err := a.ReadAngular(w.Dir)
	if err != nil {
		return "", err
	}
	rel := strings.TrimPrefix(strings.TrimPrefix(t.Dir, w.Dir), "/")
	project, ok := app.ProjectFor(projects, rel)
	if !ok || project.ProxyConfig == "" {
		return "", nil
	}
	proxyRel := path.Join(w.Dir, project.ProxyConfig)
	data, err := os.ReadFile(a.Abs(proxyRel))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", errors.Wrap(err, "os.ReadFile()")
	}
	if o.Prefix == "" || strings.Contains(string(data), "/"+o.Prefix) {
		return "", nil
	}

	return fmt.Sprintf("%s does not forward /%s, the %s outlet's prefix, so ng serve for %s cannot reach it", proxyRel, o.Prefix, o.Name, project.Name), nil
}

// outletList renders each site's outlets for the summary.
func outletList(p app.Profile) []string {
	var parts []string
	for i := range p.Sites {
		site := &p.Sites[i]
		var outlets []string
		for _, o := range outletSurfaces(site) {
			desc := fmt.Sprintf("%s (/%s", o.Name, o.Prefix)
			if o.ServesSessions && !o.Default {
				desc += ", sessions"
			}
			outlets = append(outlets, desc+")")
		}
		if p.Layout == app.LayoutSites {
			parts = append(parts, site.Name+": "+strings.Join(outlets, ", "))

			continue
		}
		parts = append(parts, strings.Join(outlets, ", "))
	}

	return parts
}

// calledFunctions returns the names of the package-level functions the hand-written Go
// files of a root-relative directory call.
func calledFunctions(a *app.App, dir string) (map[string]bool, error) {
	entries, err := os.ReadDir(a.Abs(dir))
	if errors.Is(err, os.ErrNotExist) {
		return map[string]bool{}, nil // the options check reports the missing directory
	}
	if err != nil {
		return nil, errors.Wrap(err, "os.ReadDir()")
	}
	called := map[string]bool{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || strings.HasPrefix(name, "zz_gen_") {
			continue
		}
		rel := path.Join(dir, name)
		src, err := os.ReadFile(a.Abs(rel))
		if err != nil {
			return nil, errors.Wrap(err, "os.ReadFile()")
		}
		f, err := parser.ParseFile(token.NewFileSet(), rel, src, parser.SkipObjectResolution)
		if err != nil {
			return nil, errors.Wrap(err, "parser.ParseFile()")
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok {
				if id, ok := call.Fun.(*ast.Ident); ok {
					called[id.Name] = true
				}
			}

			return true
		})
	}

	return called, nil
}
