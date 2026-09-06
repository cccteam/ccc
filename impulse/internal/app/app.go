// Package app discovers the shape of an Impulse application from its code.
//
// There is no manifest: everything the tool needs is read from files that are already
// load-bearing for the application itself — the generator program, go.mod, the browser
// apps' angular.json, the Procfile, the test harnesses, and the config struct tags.
package app

import (
	"os"
	"path/filepath"

	"github.com/go-playground/errors/v5"
	"golang.org/x/mod/modfile"
)

// App is the discovered shape of one Impulse application rooted at a Go module.
type App struct {
	// Root is the absolute path of the application root (the directory holding go.mod).
	Root string
	// GoMod is the parsed root go.mod.
	GoMod *modfile.File
	// Generators are the resource generator programs found in the tree, one per site
	// plus the shared generator in a multi-site application.
	Generators []*Generator
	// WebApps are the browser applications: every directory holding an angular.json.
	WebApps []WebApp
	// EmulatorImages are the Spanner emulator image tags named by process files
	// (Procfile, process-compose.yaml).
	EmulatorImages []EmulatorRef
	// EmulatorHarnesses are the Spanner emulator versions requested by test harnesses
	// through initiator.NewSpannerContainer.
	EmulatorHarnesses []EmulatorRef
	// EnvTags are the env struct tags declared by the application's Go code.
	EnvTags []EnvTag
	// EnvTemplate is the root-relative path of the development environment template
	// (.envrc.template or similar), or empty when the application has none.
	EnvTemplate string
	// DomainResources are the structs the application's own code annotates
	// @permissionScope(domain): the tenant-scoped resources.
	DomainResources []DomainResource
	// RoleMigrations are the calls to access.MigrateRoles outside tests: where the
	// application provisions its roles.
	RoleMigrations []RoleMigration
	// OutletMembers are the structs the application's own code annotates @outlet: the
	// resources attached to router outlets other than, or in addition to, the default.
	OutletMembers []OutletMember
	// Auths are the session authentication constructions outside tests: the user pools'
	// login flavors and the tables they read.
	Auths []Auth
	// GoGenerate are the //go:generate directives in the tree.
	GoGenerate []Directive
	// MainPackages are the root-relative directories holding a package main.
	MainPackages []string
	// AuthPackages are the auths: the packages under an auth directory constructing a
	// session authenticator, with every reference to them, sorted by name.
	AuthPackages []AuthPackage

	// goFiles are the non-test Go files scanned, for the passes that follow the walk.
	goFiles []string
	// roleWrappers are the application's own functions that pass their variadic domains
	// through to access.MigrateRoles; their callers are role migrations too.
	roleWrappers []roleWrapper
}

// GoFiles lists the non-test Go files of the application, root-relative.
func (a *App) GoFiles() []string {
	return a.goFiles
}

// roleWrapper is an application function wrapping access.MigrateRoles.
type roleWrapper struct {
	// Pkg is the wrapper's package import path.
	Pkg string
	// Func is the wrapper's name.
	Func string
	// Fixed is how many parameters precede the variadic domains.
	Fixed int
}

// Auth is one construction of a session authenticator: session.NewPasswordAuth,
// NewOIDCAzure, NewOIDCGoogle, or NewPreauth.
type Auth struct {
	File string
	Line int
	// Authority is who owns role membership for an OIDC flavor: AuthorityDirectory when
	// the constructor's slot is RoleSync, AuthorityApplication for DisableRoleSync, and
	// empty for the other flavors (the application's by nature) or a slot not read.
	Authority string
	// Flavor is the login flavor: password, oidc-azure, oidc-google, or preauth.
	Flavor string
	// SessionTable is the sessions table the authenticator reads: WithSessionTableName or
	// the library default.
	SessionTable string
	// UserTable is the users table: WithUserTableName or the default for password auth;
	// for the OIDC flavors, the anchor table (WithOIDCUserTableName or the default) only
	// when the storage enables it with sessionstorage.WithOIDCUsers; empty for preauth.
	UserTable string
	// ExtraTables are the custom session and user data tables the storage attaches
	// (sessionstorage.NewSpannerCustomSessionData and NewSpannerCustomUserData), when
	// their names are literals.
	ExtraTables []string
	// CookieName is the WithCookieName argument, or empty for the library default.
	CookieName string
	// XSRFCookieName is the WithXSRFCookieName argument, or empty for the library default.
	XSRFCookieName string
	// Impersonation reports a storage constructed with sessionstorage.WithImpersonation.
	Impersonation bool
	// ImpersonationTable is the impersonation table's literal name, or empty when the
	// name is not a literal in the call (the library default is then assumed).
	ImpersonationTable string
	// OptionsForwarded reports a variadic pass-through (opts...) in the call: options
	// the callers add are not visible here, so the tables are the defaults as far as
	// this construction shows.
	OptionsForwarded bool
}

// Directive is one //go:generate directive.
type Directive struct {
	File string
	Line int
	// Command is the directive's command line.
	Command string
}

// OutletMember is one struct annotated @outlet(...).
type OutletMember struct {
	File string
	Line int
	Name string
	// Outlets are the outlet names the annotation lists.
	Outlets []string
}

// DomainResource is one struct annotated @permissionScope(domain).
type DomainResource struct {
	File string
	Line int
	Name string
}

// RoleMigration is one call to access.MigrateRoles, or to an application wrapper that
// passes its own variadic domains through to it.
type RoleMigration struct {
	File string
	Line int
	// Domains counts the domain arguments after the callee's fixed ones.
	Domains int
	// Spread reports a trailing slice argument (domains...), which may be empty at run
	// time: an untenanted application's wrapper passes its own empty variadic through.
	Spread bool
	// Via is the wrapper as the caller writes it (deploy.MigrateRoles), or empty for a
	// direct access.MigrateRoles call.
	Via string
}

// WithDomains reports whether the call can provision roles into tenants.
func (m RoleMigration) WithDomains() bool { return m.Domains > 0 || m.Spread }

// Callee is the function called, as written.
func (m RoleMigration) Callee() string {
	if m.Via != "" {
		return m.Via
	}

	return "access.MigrateRoles"
}

// WebApp is one browser application.
type WebApp struct {
	// Dir is the root-relative directory holding angular.json.
	Dir string
}

// EmulatorRef is one place a Spanner emulator version is named.
type EmulatorRef struct {
	File    string
	Line    int
	Version string
}

// EnvTag is one env struct tag declared in the application's Go code.
type EnvTag struct {
	File       string
	Line       int
	Name       string
	Required   bool
	HasDefault bool
}

// Discover reads the application rooted at dir. dir must hold a go.mod.
func Discover(dir string) (*App, error) {
	root, err := filepath.Abs(dir)
	if err != nil {
		return nil, errors.Wrap(err, "filepath.Abs()")
	}

	modPath := filepath.Join(root, "go.mod")
	modData, err := os.ReadFile(modPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, errors.Newf("no go.mod at %s: run from the application root or pass --app", root)
		}

		return nil, errors.Wrap(err, "os.ReadFile()")
	}

	goMod, err := modfile.Parse(modPath, modData, nil)
	if err != nil {
		return nil, errors.Wrap(err, "modfile.Parse()")
	}

	a := &App{Root: root, GoMod: goMod}
	if err := a.scan(); err != nil {
		return nil, err
	}

	for _, name := range envTemplateNames {
		if _, err := os.Stat(filepath.Join(root, name)); err == nil {
			a.EnvTemplate = name

			break
		}
	}

	return a, nil
}

// envTemplateNames are the development environment templates recognized at the
// application root, in order of preference.
var envTemplateNames = []string{".envrc.template", ".env.template", ".env.example"}

// SiteGenerators returns the generators that emit handlers: one per site.
func (a *App) SiteGenerators() []*Generator {
	var sites []*Generator
	for _, g := range a.Generators {
		if g.HandlersDir() != "" {
			sites = append(sites, g)
		}
	}

	return sites
}

// SharedGenerators returns the generators that emit no handlers: the shared resource
// generators of a multi-site application.
func (a *App) SharedGenerators() []*Generator {
	var shared []*Generator
	for _, g := range a.Generators {
		if g.HandlersDir() == "" {
			shared = append(shared, g)
		}
	}

	return shared
}

// WebAppFor returns the browser application whose directory contains the root-relative
// path, or false when no browser application contains it.
func (a *App) WebAppFor(rel string) (WebApp, bool) {
	rel = filepath.ToSlash(filepath.Clean(rel))
	best, found := WebApp{}, false
	for _, w := range a.WebApps {
		if !within(w.Dir, rel) {
			continue
		}
		if !found || len(w.Dir) > len(best.Dir) {
			best, found = w, true
		}
	}

	return best, found
}

// within reports whether rel (a slash-separated root-relative path) is dir or lies under it.
func within(dir, rel string) bool {
	if dir == "." {
		return true
	}

	return rel == dir || len(rel) > len(dir) && rel[:len(dir)] == dir && rel[len(dir)] == '/'
}

// Rel returns the root-relative, slash-separated form of an absolute path under Root.
func (a *App) Rel(abs string) string {
	rel, err := filepath.Rel(a.Root, abs)
	if err != nil {
		return filepath.ToSlash(abs)
	}

	return filepath.ToSlash(rel)
}

// Abs returns the absolute path of a root-relative path.
func (a *App) Abs(rel string) string {
	return filepath.Join(a.Root, filepath.FromSlash(rel))
}
