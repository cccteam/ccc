// Package app discovers the shape of an Impulse application from its code.
//
// There is no manifest: everything the tool needs is read from files that are already
// load-bearing for the application itself — the generator program, go.mod, the browser
// apps' angular.json, the Procfile, the test harnesses, and the config struct tags.
//
// This is the reader other tools import: bedrock derives an application's infrastructure
// from what Discover reports. The dependency points one way: impulse imports nothing from
// those tools, so an application stays deployable by hand with impulse alone.
package app

import (
	"os"
	"path/filepath"
	"strings"

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
	// plus the shared generator in the sites layout.
	Generators []*Generator
	// WebApps are the browser applications: every directory holding an angular.json.
	WebApps []WebApp
	// EmulatorImages are the Spanner emulator image tags named by process files
	// (Procfile, process-compose.yaml).
	EmulatorImages []EmulatorRef
	// EmulatorHarnesses are the Spanner emulator versions requested by test harnesses
	// through initiator.NewSpannerContainer.
	EmulatorHarnesses []EmulatorRef
	// FirestoreEmulatorImages are the Firestore emulator versions named by process files
	// and test harnesses through the Cloud SDK emulators image tag
	// (google-cloud-cli:<version>-emulators), and by test harnesses through
	// initiator.NewFirestoreContainer, which takes the same version: the emulator the live
	// pages run on in development.
	FirestoreEmulatorImages []EmulatorRef
	// EnvTags are the env struct tags declared by the application's Go code.
	EnvTags []EnvTag
	// EnvTemplate is the root-relative path of the development environment template
	// (.envrc.template or similar), or empty when the application has none.
	EnvTemplate string
	// DomainResources are the structs the application's own code annotates
	// @permissionScope(domain): the tenant-scoped resources.
	DomainResources []DomainResource
	// TenantRecords are the structs the application's own code annotates @tenant: each
	// resource package's tenant record, the global resource whose rows are the tenants.
	TenantRecords []TenantRecord
	// RosterConstructions are the constructions of a tenant roster outside tests: the
	// calls to a generated New<Record>Roster constructor, with what each is handed.
	RosterConstructions []RosterConstruction
	// Features are the feature flags the application declares: the resource.Feature
	// constants of its resources packages, in file order.
	Features []FeatureFlag
	// FeatureGates are the @feature annotations in the application's own code: the
	// resources, methods and fields behind a flag.
	FeatureGates []FeatureGate
	// DefaultRoles are the calls to access.WithDefaultRoles outside tests: where a
	// release hands its default roles, an auth package's embedded role file, to the
	// permission engine.
	DefaultRoles []DefaultRoles
	// PolicyChecks are the calls to CheckPolicy outside tests: the deploy step that
	// prints what the store holds that the release cannot use as written.
	PolicyChecks []PolicyCheck
	// RoleValidations are the calls to access.ValidateRoles in the application's tests:
	// where a role file's warnings are pinned in code.
	RoleValidations []RoleValidation
	// GrantProofs are the calls to the harness helper provesGrant in the application's
	// tests: where a test case names the conditional grant it proves.
	GrantProofs []GrantProof
	// OutletMembers are the structs the application's own code annotates @outlet: the
	// resources attached to router outlets other than, or in addition to, the default.
	OutletMembers []OutletMember
	// Auths are the session authentication constructions outside tests: the auths'
	// login flavors and the tables they read.
	Auths []Auth
	// GoGenerate are the //go:generate directives in the tree.
	GoGenerate []Directive
	// CI is the application's //impulse:ci line, or nil without one: what its CI
	// workflow does where the code alone does not decide (which jobs run on the larger
	// runner, whether Go's test cache is used). At most one in the tree.
	CI *CIDirective
	// MainPackages are the root-relative directories holding a package main.
	MainPackages []string
	// AuthPackages are the auths: the packages under an auth directory constructing a
	// session authenticator, with every reference to them, sorted by name.
	AuthPackages []AuthPackage
	// Engines are the permission engine constructions outside tests: every access.New
	// call, with whether it hands the engine a change signal (access.WithChangeSignal).
	Engines []Engine

	// goFiles are the non-test Go files scanned, for the passes that follow the walk.
	goFiles []string
}

// GoFiles lists the non-test Go files of the application, root-relative.
func (a *App) GoFiles() []string {
	return a.goFiles
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
	// ImpersonationTable is the impersonation table's name when the code states it: a
	// literal or a constant of the file, given to NewImpersonationTable inside the storage
	// constructor or assigned to the identifier WithImpersonation takes. Empty otherwise
	// (the library default is then assumed).
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

// CIDirective is the //impulse:ci line: a comment line in any non-test Go file of the
// application, of the form
//
//	//impulse:ci large-runner=go-test,go-test-skipauth,image test-cache=on
//
// with each setting at most once and in any order (a directive in Go's sense, which
// gofmt keeps as written and, at the end of a doc comment, sets off with a blank //
// line). large-runner lists the CI jobs that
// run on the larger runner the variable CI_LARGE_RUNNER names (or none, for no job);
// test-cache is on or off. A setting the line leaves out keeps impulse's default (the
// ci package states them); a line with a setting it does not know, or a second line in
// the tree, is an error at discovery.
type CIDirective struct {
	File string
	Line int
	// LargeRunner lists the jobs the line puts on the larger runner, by job id; empty
	// (and non-nil) for large-runner=none, nil when the line does not set it.
	LargeRunner []string
	// TestCache is the test-cache value, on or off; nil when the line does not set it.
	TestCache *bool
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

// TenantRecord is one struct annotated @tenant: the tenant record, the global resource
// whose rows are the tenants, whose route name is the segment tenant-scoped routes are
// served under, and whose generated constructor builds the application's tenant roster.
type TenantRecord struct {
	File string
	Line int
	Name string
}

// RosterConstruction is one call outside tests to a generated tenant roster constructor
// (New<Record>Roster).
type RosterConstruction struct {
	File string
	Line int
	// Package is the root-relative directory of the package constructing the roster.
	Package string
	// Constructor is the constructor's name (NewTenantRoster for a record named Tenant).
	Constructor string
	// Holder is the name the construction's result is bound to: the variable of a
	// definition or assignment (the field's name when the left side is a selector), or
	// the key of a composite literal element; empty when the result is used some other
	// way, such as returned.
	Holder string
	// Signals reports a resource.WithTenantSignals option among the call's arguments:
	// the roster reloads on the tenants signal, not at its backstop alone.
	Signals bool
}

// DefaultRoles is one call to access.WithDefaultRoles outside tests: a release handing a
// role file to the permission engine.
type DefaultRoles struct {
	File string
	Line int
	// RolesPackage is the import path of the package whose Roles() the call hands over:
	// the file's own package when the argument is Roles() unqualified, the imported
	// package when it is <auth>.Roles(), or empty when the argument reads some other way.
	RolesPackage string
}

// PolicyCheck is one call to CheckPolicy outside tests: the deploy step printing what
// the store holds that the release cannot use as written.
type PolicyCheck struct {
	File string
	Line int
}

// Engine is one construction of a permission engine outside tests: a call to access.New.
type Engine struct {
	File string
	Line int
	// Package is the root-relative directory of the package constructing the engine.
	Package string
	// ChangeSignal reports an access.WithChangeSignal option among the call's arguments:
	// the engine is handed the signal that carries policy changes between the
	// application's instances.
	ChangeSignal bool
	// OptionsForwarded reports a variadic pass-through (opts...) in the call: options the
	// callers add are not visible here.
	OptionsForwarded bool
}

// RoleValidation is one call to access.ValidateRoles in a test file: the role files it
// validates are the auth packages whose Roles() the file calls.
type RoleValidation struct {
	File string
	Line int
	// RolesPackages are the import paths of the packages whose Roles() the file calls,
	// sorted and without repeats; the role files the call validates.
	RolesPackages []string
}

// WebApp is one browser application.
type WebApp struct {
	// Dir is the root-relative directory holding angular.json.
	Dir string
}

// EmulatorRef is one place an emulator version is named.
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
	// Secret reports the field's secret tag, secret:"true": the variable holds a
	// credential that the infrastructure mounts from a secret store.
	Secret bool
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
// generators of an application in the sites layout.
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

// ModuleDir returns the root-relative directory an import path names inside the
// application's module, or false for a path outside it (or when the module path is not
// known).
func (a *App) ModuleDir(importPath string) (string, bool) {
	if a.GoMod == nil || a.GoMod.Module == nil {
		return "", false
	}
	modulePath := a.GoMod.Module.Mod.Path
	switch {
	case importPath == modulePath:
		return ".", true
	case strings.HasPrefix(importPath, modulePath+"/"):
		return strings.TrimPrefix(importPath, modulePath+"/"), true
	default:
		return "", false
	}
}

// Abs returns the absolute path of a root-relative path.
func (a *App) Abs(rel string) string {
	return filepath.Join(a.Root, filepath.FromSlash(rel))
}
