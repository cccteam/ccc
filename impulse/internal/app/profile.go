package app

import (
	"path"
	"sort"
	"strings"
)

// Layout is how an application's sites are laid out in the tree.
type Layout int

// The layouts. An application starts flat and is promoted to LayoutSites when it grows a
// second site: the site's packages move under apps/<site>/ and the shared packages stay
// at the root.
const (
	// LayoutFlat has one site at the application root: app/, pkg/, web/.
	LayoutFlat Layout = iota
	// LayoutSites has every site under apps/<site>/, with the shared packages at the root.
	LayoutSites
)

func (l Layout) String() string {
	switch l {
	case LayoutFlat:
		return "flat"
	case LayoutSites:
		return "multi-site"
	default:
		return "unknown"
	}
}

// sitesDir is the directory the sites of a LayoutSites application live under.
const sitesDir = "apps"

// Profile is the option set in force across an application: the axes of the composed
// skeleton — layout, sites, tenancy, outlets — as the generator programs declare them.
// It is read, never recorded: a transition that adds an option is done when the profile
// shows it.
type Profile struct {
	Layout Layout
	// Sites are the generators that emit handlers, one per site, in generator order.
	Sites []Site
	// Shared are the generators that emit no handlers: the shared resource generators of
	// a multi-site application.
	Shared []*Generator
}

// Site is one site: the generator program that emits its handlers and what that program
// declares about it.
type Site struct {
	// Name is the site's directory name under apps/ in the multi-site layout, or the
	// module path's last element in the flat layout.
	Name string
	// Dir is the root-relative directory holding the site's packages: apps/<site> in the
	// multi-site layout, "." in the flat layout.
	Dir       string
	Generator *Generator
	// DomainRoute is the WithDomainRoute segment, or empty when the site is not tenanted.
	DomainRoute string
	// ConcealedDomains reports WithConcealedDomains.
	ConcealedDomains bool
	// Outlets are the WithRouterOutlet declarations, in order. The default outlet
	// GenerateRoutes declares is not listed.
	Outlets []Outlet
}

// Outlet is one WithRouterOutlet declaration.
type Outlet struct {
	Name   string
	Prefix string
	// ServesSessions reports the ServesSessions outlet option.
	ServesSessions bool
	// Pos is the position of the WithRouterOutlet call.
	Pos string
}

// Tenanted reports whether the site declares a domain route.
func (s *Site) Tenanted() bool { return s.DomainRoute != "" }

// Profile reads the option set in force from the generator programs.
func (a *App) Profile() Profile {
	p := Profile{Shared: a.SharedGenerators()}
	for _, g := range a.SiteGenerators() {
		p.Sites = append(p.Sites, a.site(g))
	}
	for i := range p.Sites {
		if p.Sites[i].Dir != "." {
			p.Layout = LayoutSites

			break
		}
	}

	return p
}

// site reads one site generator.
func (a *App) site(g *Generator) Site {
	s := Site{Generator: g, Dir: siteDir(g), Name: a.moduleName()}
	if s.Dir != "." {
		s.Name = path.Base(s.Dir)
	}
	if c, ok := g.Option("WithDomainRoute"); ok && len(c.Args) > 0 && c.Args[0].Kind == ArgString {
		s.DomainRoute = c.Args[0].Str
	}
	_, s.ConcealedDomains = g.Option("WithConcealedDomains")
	for _, c := range g.OptionsNamed("WithRouterOutlet") {
		if len(c.Args) < 2 || c.Args[0].Kind != ArgString || c.Args[1].Kind != ArgString {
			continue
		}
		o := Outlet{Name: c.Args[0].Str, Prefix: c.Args[1].Str, Pos: c.Pos}
		for _, arg := range c.Args[2:] {
			if arg.Kind == ArgCall && arg.Call.Name == optServesSessions {
				o.ServesSessions = true
			}
		}
		s.Outlets = append(s.Outlets, o)
	}

	return s
}

// moduleName is the module path's last element, or empty without a module directive.
func (a *App) moduleName() string {
	if a.GoMod == nil || a.GoMod.Module == nil {
		return ""
	}

	return path.Base(a.GoMod.Module.Mod.Path)
}

// siteDir derives a site's directory from where its generator writes: apps/<site> when
// the resource package, the handlers, and the routes all live under one such directory,
// "." otherwise.
func siteDir(g *Generator) string {
	dirs := []string{g.ResourcePackageDir, g.HandlersDir()}
	if routes := g.RoutesDir(); routes != "" {
		dirs = append(dirs, routes)
	}
	site := ""
	for _, d := range dirs {
		parts := strings.Split(d, "/")
		if len(parts) < 3 || parts[0] != sitesDir {
			return "."
		}
		candidate := path.Join(parts[0], parts[1])
		if site != "" && candidate != site {
			return "."
		}
		site = candidate
	}

	return site
}

// SiteNames lists the sites' names in order.
func (p Profile) SiteNames() []string {
	names := make([]string, 0, len(p.Sites))
	for _, s := range p.Sites {
		names = append(names, s.Name)
	}

	return names
}

// OutletNames lists every outlet name declared by any site, sorted and without repeats.
func (p Profile) OutletNames() []string {
	seen := map[string]bool{}
	var names []string
	for _, s := range p.Sites {
		for _, o := range s.Outlets {
			if !seen[o.Name] {
				seen[o.Name] = true
				names = append(names, o.Name)
			}
		}
	}
	sort.Strings(names)

	return names
}
