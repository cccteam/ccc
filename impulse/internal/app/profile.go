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

// The layouts' names, as the options report and the README spell them.
const (
	layoutFlatName  = "flat"
	layoutSitesName = "sites"
)

func (l Layout) String() string {
	switch l {
	case LayoutFlat:
		return layoutFlatName
	case LayoutSites:
		return layoutSitesName
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
	// an application in the sites layout.
	Shared []*Generator
}

// Site is one site: the generator program that emits its handlers and what that program
// declares about it.
type Site struct {
	// Name is the site's directory name under apps/ in the sites layout, or the
	// module path's last element in the flat layout.
	Name string
	// Dir is the root-relative directory holding the site's packages: apps/<site> in the
	// sites layout, "." in the flat layout.
	Dir       string
	Generator *Generator
	// DomainRoute is the WithDomainRoute segment, or empty when the site is not tenanted.
	DomainRoute string
	// ConcealedDomains reports WithConcealedDomains.
	ConcealedDomains bool
	// GeneratedRouter reports GenerateRouter: the router is generated from the outlet
	// declarations instead of hand-written.
	GeneratedRouter bool
	// Default is the outlet GenerateRoutes declares, with the outlet options it carries;
	// its Name is the reserved default.
	Default Outlet
	// Outlets are the WithRouterOutlet declarations, in order. The default outlet
	// GenerateRoutes declares is Default, not listed here.
	Outlets []Outlet
}

// DefaultOutletName is the reserved name of the outlet GenerateRoutes declares.
const DefaultOutletName = "default"

// Outlet is one outlet declaration: GenerateRoutes for the default outlet, a
// WithRouterOutlet call for an additional one.
type Outlet struct {
	Name   string
	Prefix string
	// ServesSessions reports a session outlet: the ServesSessions option, a session Auth,
	// or the default outlet.
	ServesSessions bool
	// Auth is the outlet's Auth option under the generated router, nil when it declares
	// none.
	Auth *OutletAuth
	// APIKey reports the APIKey option: a machine outlet under the generated router.
	APIKey bool
	// WebApp is the WebApp option's mount path, empty when the outlet serves none.
	WebApp string
	// Pos is the position of the declaring call.
	Pos string
}

// OutletAuth is one Auth option: the auth package the outlet binds to and the flavor its
// people sign in with, as the generation package names it (Password, OIDCGoogle,
// OIDCAzure).
type OutletAuth struct {
	ImportPath string
	Flavor     string
}

// LoginFlavor is the auth's flavor as the auth scan reports it (password, oidc-google,
// oidc-azure).
func (a OutletAuth) LoginFlavor() string { return authFlavorIdents[a.Flavor] }

// Tenanted reports whether the site declares a domain route.
func (s *Site) Tenanted() bool { return s.DomainRoute != "" }

// AllOutlets lists the site's outlets, the default first.
func (s *Site) AllOutlets() []Outlet {
	return append([]Outlet{s.Default}, s.Outlets...)
}

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
	_, s.GeneratedRouter = g.Option(optGenerateRouter)
	if c, ok := g.Option(optGenerateRoutes); ok {
		s.Default = Outlet{Name: DefaultOutletName, ServesSessions: true, Pos: c.Pos}
		if len(c.Args) >= 2 && c.Args[1].Kind == ArgString {
			s.Default.Prefix = c.Args[1].Str
		}
		if len(c.Args) > 2 {
			readOutletOptions(&s.Default, c.Args[2:])
		}
	}
	for _, c := range g.OptionsNamed("WithRouterOutlet") {
		if len(c.Args) < 2 || c.Args[0].Kind != ArgString || c.Args[1].Kind != ArgString {
			continue
		}
		o := Outlet{Name: c.Args[0].Str, Prefix: c.Args[1].Str, Pos: c.Pos}
		readOutletOptions(&o, c.Args[2:])
		s.Outlets = append(s.Outlets, o)
	}

	return s
}

// readOutletOptions applies the outlet options of a declaration to the outlet.
func readOutletOptions(o *Outlet, args []Arg) {
	for _, arg := range args {
		if arg.Kind != ArgCall {
			continue
		}
		c := arg.Call
		switch c.Name {
		case optServesSessions:
			o.ServesSessions = true
		case optAuth:
			if len(c.Args) == 2 && c.Args[0].Kind == ArgString && c.Args[1].Kind == ArgIdent {
				o.Auth = &OutletAuth{ImportPath: c.Args[0].Str, Flavor: c.Args[1].Str}
				o.ServesSessions = true
			}
		case optAPIKey:
			o.APIKey = true
		case optWebApp:
			if len(c.Args) == 1 && c.Args[0].Kind == ArgString {
				o.WebApp = c.Args[0].Str
			}
		}
	}
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
	for i := range p.Sites {
		names = append(names, p.Sites[i].Name)
	}

	return names
}

// OutletNames lists every outlet name declared by any site, sorted and without repeats.
func (p Profile) OutletNames() []string {
	seen := map[string]bool{}
	var names []string
	for i := range p.Sites {
		for _, o := range p.Sites[i].Outlets {
			if !seen[o.Name] {
				seen[o.Name] = true
				names = append(names, o.Name)
			}
		}
	}
	sort.Strings(names)

	return names
}
