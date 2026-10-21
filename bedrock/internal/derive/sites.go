// sites.go reads the served sites: one in the flat layout, one per directory under apps/
// in the sites layout, each a process of the image the stack deploys as a Cloud Run
// service of its own, with its outlets, its router's release file, its hostname label
// and, in the sites layout, the browser bundle the stack points it at.

package derive

import (
	"path"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/impulse/app"
)

// Site is one served site: a main package the image builds into a binary of its own and
// the stack runs as a Cloud Run service per region. A solo application has one, named
// app as the process; a sites-layout application has one per directory under apps/,
// each named after its directory (console, portal), and every per-site resource of the
// stack carries the name as its second segment.
type Site struct {
	// Name is the site's name: the directory under apps/ in the sites layout, the
	// module path's last element in the flat layout. In the sites layout it names the
	// process, the binary (/<name>) and the per-site resources.
	Name string
	// Dir is the root-relative directory of the site's main package: apps/<name>, or "."
	// in the flat layout.
	Dir string
	// Label is the site's hostname label under the apps domain: the one the placement
	// declares for it (Placement.Hostnames), else the default, the code alone in the
	// flat layout and <code>-<name> in the sites layout.
	Label string
	// Process is the site as a deployed process: its levels and whether it speaks h2c.
	Process
	// Outlets are the site's router outlets, the default first: the URL spaces its
	// routes carrying JSON input live under.
	Outlets []Outlet
	// SignInRoutes are the site's sign-in routes, the ones under its session outlets
	// that take a credential by POST (signin.go), in outlet order: each session
	// outlet's password login and its pending sign-in's completion routes.
	SignInRoutes []SignInRoute
	// RouterDir is the root-relative directory of the site's generated router package,
	// where the release file is; empty when its generator declares none.
	RouterDir string
	// Scheduled, FileRoutes and Surfaces are the site's, read from its release file;
	// RequestLog the exclusion derived from its surfaces, nil when none excludes.
	Scheduled  []ScheduledRoute
	FileRoutes []FileRoute
	Surfaces   []Surface
	RequestLog *RequestLogExclusion
	// Environments are the placement's environments with the hostnames the site serves
	// in each, the canonical first.
	Environments []Environment
	// Bundle is the site-level variable the stack sets per service to the directory the
	// site serves its built browser bundle from in the sites layout (RoleSiteBundle).
	// Nil in the flat layout, where the image sets the bundle variables itself, and for
	// a site without a browser workspace; the directories below are empty with it.
	Bundle *Variable
	// Workspace is the root-relative browser workspace under the site's directory
	// (apps/console/web), the directory holding angular.json and package.json: where
	// the image installs and builds.
	Workspace string
	// BundleDir is the root-relative directory the site serves its bundle from, which
	// the stack sets Bundle to under the image's working directory
	// (apps/console/web/dist/console): the one the site's process line in the Procfile
	// sets (ProcfileBundleDir), else the one the workspace's build writes (OutputDir).
	// Always under Workspace, since the image copies it out of the workspace's build.
	BundleDir string
	// ProcfileBundleDir is the directory the site's process line in the Procfile sets
	// Bundle to for development, as written; empty when no line sets it.
	ProcfileBundleDir string
	// OutputDir is the root-relative directory the workspace's build writes the site's
	// bundle to, read from angular.json's outputPath (bundleOutput); empty when
	// angular.json names no single application project.
	OutputDir string
}

// MultiSite reports a sites-layout application with more than one site: every per-site
// resource carries the site's name, and the pipeline deploys each site's service.
func (m *Model) MultiSite() bool {
	return len(m.Sites) > 1
}

// SiteNamed finds a site by name, or nil.
func (m *Model) SiteNamed(name string) *Site {
	for i := range m.Sites {
		if m.Sites[i].Name == name {
			return &m.Sites[i]
		}
	}

	return nil
}

// RouterDirs lists the sites' generated router package directories, in site order,
// a site without one left out: where the release files are.
func (m *Model) RouterDirs() []string {
	var dirs []string
	for i := range m.Sites {
		if m.Sites[i].RouterDir != "" {
			dirs = append(dirs, m.Sites[i].RouterDir)
		}
	}

	return dirs
}

// SiteNames lists the sites' names in order.
func (m *Model) SiteNames() []string {
	names := make([]string, 0, len(m.Sites))
	for i := range m.Sites {
		names = append(names, m.Sites[i].Name)
	}

	return names
}

// SiteBundleVariable is the site-level variable a sites-layout application reads its
// built browser bundle's directory from, required and without a default, since each
// site's differs: the Procfile sets it per process in development and the stack per
// service (RoleSiteBundle). The flat layout names its bundles with a default the image
// sets instead (BundleRE).
const SiteBundleVariable = "APP_DIST"

// SiteNamePattern is the rule for a site's name in the sites layout: lowercase letters
// and digits starting with a letter, the segment every per-site resource name and the
// default hostname label carry. The length comes from the tightest name the segment
// enters, the Cloud Run service's (SiteNameBudget).
const SiteNamePattern = `^[a-z][a-z0-9]*$`

var siteNameRE = regexp.MustCompile(SiteNamePattern)

// cloudRunNameLimit is the most characters a Cloud Run service name takes.
const cloudRunNameLimit = 49

// SiteNameBudget is the most characters a site's name may take in this placement: what
// Cloud Run's limit leaves after the service name's other segments at their longest,
// <prefix>-<env>-<region code>-<code>-<site>-app.
func (p *Placement) SiteNameBudget() int {
	env, region := 0, 0
	for _, e := range p.Environments {
		env = max(env, len(e))
	}
	for _, r := range p.Regions {
		region = max(region, len(r.Code))
	}

	return cloudRunNameLimit - (len(p.Prefix) + 1 + env + 1 + region + 1 + len(p.Code) + 1 + 1 + len("app"))
}

// ValidateSiteName holds a site's name to the rule and the placement's budget.
func (p *Placement) ValidateSiteName(name string) error {
	if !siteNameRE.MatchString(name) {
		return errors.Newf("site name %q is not lowercase letters and digits starting with a letter", name)
	}
	if budget := p.SiteNameBudget(); len(name) > budget {
		return errors.Newf("site name %q is %d characters, and %d is the most a site of %s may take: the service name <prefix>-<env>-<region>-<code>-<site>-app fits Cloud Run's %d with %d left for the site", name, len(name), budget, p.Code, cloudRunNameLimit, budget)
	}

	return nil
}

// sites reads every site: its main package, its outlets, its label and its router
// directory, in the generators' order. The placement's hostnames may name no site the
// application lacks. In the sites layout each site's name is held to the rule and the
// budget, and each site's bundle variable is settled (siteBundles).
func (m *Model) sites(a *app.App, cfg *config, profile *app.Profile) error {
	if len(profile.Sites) == 0 {
		return errors.New("the application has no site: no generator emits handlers")
	}
	names := make([]string, 0, len(profile.Sites))
	for i := range profile.Sites {
		names = append(names, profile.Sites[i].Name)
	}
	for site := range m.Placement.Hostnames {
		if !slices.Contains(names, site) {
			return errors.Newf("placement.json's hostnames names %q, which is not a site of the application: its site%s %s", site, verb(len(names)), joinSites(names))
		}
	}
	multi := len(profile.Sites) > 1
	for i := range profile.Sites {
		s := &profile.Sites[i]
		if !slices.Contains(a.MainPackages, s.Dir) {
			return errors.Newf("no main package at %s, the %s site's directory", s.Dir, s.Name)
		}
		if multi {
			if err := m.Placement.ValidateSiteName(s.Name); err != nil {
				return errors.Wrapf(err, "%s", s.Dir)
			}
		}
		name := siteProcess
		if multi {
			name = s.Name
		}
		process, err := m.process(a, cfg, name, s.Dir)
		if err != nil {
			return err
		}
		site := Site{Name: s.Name, Dir: s.Dir, Label: m.Placement.Label(s.Name, s.Dir), Process: process}
		outlets := s.AllOutlets()
		for i := range outlets {
			site.Outlets = append(site.Outlets, Outlet{Name: outlets[i].Name, Prefix: outlets[i].Prefix})
			site.SignInRoutes = append(site.SignInRoutes, signInRoutes(s.Name, &outlets[i])...)
		}
		if s.Generator != nil {
			site.RouterDir = s.Generator.RoutesDir()
		}
		site.Environments = m.Placement.environments(s.Name, site.Label)
		m.Sites = append(m.Sites, site)
	}
	m.Label = m.Sites[0].Label
	m.RouterDir = m.Sites[0].RouterDir
	m.Outlets = m.allOutlets()
	m.SignInRoutes = m.allSignInRoutes()
	m.Environments = m.allEnvironments()
	if multi {
		return m.siteBundles(a)
	}

	return nil
}

// allOutlets is the union of the sites' outlets, the first site's first, an outlet
// prefix listed once: what Cloud Armor's rule sets are scoped to, since a rule is
// matched by path on every host alike.
func (m *Model) allOutlets() []Outlet {
	var outlets []Outlet
	for i := range m.Sites {
		for _, o := range m.Sites[i].Outlets {
			if !slices.ContainsFunc(outlets, func(have Outlet) bool { return have.Name == o.Name && have.Prefix == o.Prefix }) {
				outlets = append(outlets, o)
			}
		}
	}

	return outlets
}

// allEnvironments is each environment with every site's hostnames, the first site's
// first and each site's canonical first: the hostnames the stack serves there.
func (m *Model) allEnvironments() []Environment {
	envs := make([]Environment, 0, len(m.Placement.Environments))
	for i, env := range m.Placement.Environments {
		e := Environment{Name: env}
		for j := range m.Sites {
			e.Hostnames = append(e.Hostnames, m.Sites[j].Environments[i].Hostnames...)
		}
		envs = append(envs, e)
	}

	return envs
}

// siteBundles settles each site's browser bundle in the sites layout: the site-level
// variable SiteBundleVariable, which the stack sets per service to the directory the
// site serves its bundle from, built in the browser workspace under the site's directory
// (apps/<site>/web) and copied out of it by the image. The variable takes the role; a
// site without a workspace serves no bundle, which the variable's requirement refuses at
// start, so it is refused here; the directory itself is the Procfile's, else the
// build's (bundleDir).
func (m *Model) siteBundles(a *app.App) error {
	v := m.variableNamed(SiteBundleVariable)
	if v == nil {
		return nil
	}
	if v.Level != LevelSite {
		return errors.Newf("%s:%d: %s (%s) is declared at the %s level; the sites layout reads each site's bundle directory from it at the site level, where the stack sets it per service", v.File, v.Line, v.Name, v.Declaration(), v.Level)
	}
	if v.HasDefault {
		return errors.Newf("%s:%d: %s (%s) has a default (%s); in the sites layout each site's bundle differs, so the variable is required with no default, set per process by the Procfile and per service by the stack", v.File, v.Line, v.Name, v.Declaration(), v.Default)
	}
	v.Role = RoleSiteBundle
	processes, err := readProcfile(a)
	if err != nil {
		return err
	}
	for i := range m.Sites {
		s := &m.Sites[i]
		workspace, err := siteWorkspace(a, s, v)
		if err != nil {
			return err
		}
		s.Bundle, s.Workspace = v, workspace
		if err := s.bundleDir(a, processes); err != nil {
			return err
		}
	}

	return nil
}

// siteWorkspace is the one browser workspace under the site's directory, the one whose
// bundle the site serves: none and several are refused.
func siteWorkspace(a *app.App, s *Site, v *Variable) (string, error) {
	var dirs []string
	for _, w := range a.WebApps {
		if strings.HasPrefix(w.Dir, s.Dir+"/") {
			dirs = append(dirs, w.Dir)
		}
	}
	sort.Strings(dirs)
	switch len(dirs) {
	case 1:
		return dirs[0], nil
	case 0:
		return "", errors.Newf("the %s site (%s) has no browser workspace (no angular.json under %s), and the site level requires %s (%s), the bundle the site serves: add the workspace, or make the variable optional", s.Name, s.Dir, s.Dir, v.Name, v.Declaration())
	default:
		return "", errors.Newf("the %s site (%s) has %d browser workspaces (%s), and %s (%s) names one bundle: a site serves one workspace's", s.Name, s.Dir, len(dirs), strings.Join(dirs, ", "), v.Name, v.Declaration())
	}
}

// bundleDir settles the directory the site serves its bundle from: the one its process
// line in the Procfile sets the bundle variable to, the value development runs with,
// else the one the workspace's build writes (bundleOutput), and with neither nothing
// points the service anywhere, so the site is refused. The Procfile's directory is held
// under the workspace, since the image copies it out of the workspace's build stage;
// whether it is the directory the build writes is bedrock check's finding, not a refusal
// here. The build's directory is read either way, for the check.
func (s *Site) bundleDir(a *app.App, processes procfile) error {
	output, err := bundleOutput(a, s.Workspace)
	if err != nil {
		return err
	}
	s.OutputDir = output
	s.ProcfileBundleDir = processes.env(s.Process.Name, s.Bundle.Name)
	switch {
	case s.ProcfileBundleDir != "":
		dir := path.Clean(s.ProcfileBundleDir)
		if !strings.HasPrefix(dir, s.Workspace+"/") {
			return errors.Newf("%s: the %s process sets %s=%s, which is not under the %s site's browser workspace %s, where the image builds the bundle: the stack would point the site's service at a directory the image does not hold; set it to the directory under %s the build writes (%s)", ProcfileName, s.Process.Name, s.Bundle.Name, s.ProcfileBundleDir, s.Name, s.Workspace, s.Workspace, s.outputOrDist())
		}
		s.BundleDir = dir
	case s.OutputDir != "":
		s.BundleDir = s.OutputDir
	default:
		return errors.Newf("the %s site (%s) names no bundle directory: its %s process line in the %s sets no %s, and %s/%s has no application project to read the build's output directory from; set %s=<directory> on the process line, the directory under %s the build writes", s.Name, s.Dir, s.Process.Name, ProcfileName, s.Bundle.Name, s.Workspace, angularJSONName, s.Bundle.Name, s.Workspace)
	}

	return nil
}

// outputOrDist names the directory the build writes, or the workspace's dist when
// angular.json does not say.
func (s *Site) outputOrDist() string {
	if s.OutputDir != "" {
		return s.OutputDir
	}

	return path.Join(s.Workspace, angularDist)
}

// plural is the s of a count other than one.
func plural(n int) string {
	if n == 1 {
		return ""
	}

	return "s"
}

// verb is "is" for one and "s are" for several, after "site".
func verb(n int) string {
	if n == 1 {
		return " is"
	}

	return "s are"
}

// joinSites spells the site names: one, or the several with "and".
func joinSites(names []string) string {
	if len(names) <= 1 {
		return strings.Join(names, "")
	}

	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}
