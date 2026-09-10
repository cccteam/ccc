package generation

import (
	"fmt"
	"maps"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"time"

	"cloud.google.com/go/civil"
	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
	"github.com/ettle/strcase"
	"github.com/go-playground/errors/v5"
	"github.com/shopspring/decimal"
)

type (
	resourceOption func(*resourceGenerator) error
	tsOption       func(*typescriptGenerator) error
	// Option is a functional option for configuring a Generator
	Option func(any) error

	option interface {
		isOption()
	}

	// ResourceOption is a functional option for configuring a ResourceGenerator
	ResourceOption interface {
		option
		isResourceOption()
	}

	// TSOption is a functional option for configuring a TypescriptGenerator
	TSOption interface {
		option
		isTypescriptOption()
	}
)

func (resourceOption) isOption()         {}
func (resourceOption) isResourceOption() {}

func (tsOption) isOption()           {}
func (tsOption) isTypescriptOption() {}

func (Option) isOption()           {}
func (Option) isResourceOption()   {}
func (Option) isTypescriptOption() {}

// GenerateHandlers enables generating a handler file for each resource.
// To generate resource handlers in a single file use WithConsolidatedHandlers.
func GenerateHandlers(targetDir string) ResourceOption {
	return resourceOption(func(r *resourceGenerator) error {
		r.genHandlers = true
		r.handler = packageDir(targetDir)

		return nil
	})
}

// GenerateHandlerTests enables generation of the handler test suite in targetDir: the
// Spanner-emulator bootstrap (TestMain + prepareDatabase over the application's
// schema migrations) and the authorization-matrix tests, which drive every generated
// global list/read route through the generated test router — without the required
// permission the pipeline must fail closed with 403; with exactly that permission the
// request must reach data access (200, or 404 on the empty schema). The target
// package hand-writes exactly one function the generated suite calls:
//
//	newTestHandler(t *testing.T, db *initiator.SpannerDB, g grants) http.Handler
//
// where the application constructs its App around the test database with the scripted
// grants and composes it through router.NewTestRouter. Requires GenerateHandlers and
// GenerateRoutes.
func GenerateHandlerTests(targetDir string) ResourceOption {
	return resourceOption(func(r *resourceGenerator) error {
		r.genHandlerTests = true
		r.handlerTests = packageDir(targetDir)

		return nil
	})
}

// ApplicationName sets the name of the application struct.
// The default is "App".
func ApplicationName(name string) ResourceOption {
	return resourceOption(func(r *resourceGenerator) error {
		r.applicationName = name

		return nil
	})
}

// GenerateRoutes enables generating a router file containing routes for all handlers and
// RPC methods, registered under routePrefix on the default outlet. Outlet options refine
// the default outlet's declaration for the generated router (GenerateRouter): Auth names
// the auth its browser sessions come from, or APIKey makes it a machine surface, and
// WebApp names the browser application it serves.
func GenerateRoutes(targetDir, routePrefix string, options ...OutletOption) ResourceOption {
	return resourceOption(func(r *resourceGenerator) error {
		r.genRoutes = true
		r.router = packageDir(targetDir)
		r.routePrefix = routePrefix

		outlet := routerOutlet{name: defaultOutletName, prefix: routePrefix, servesSessions: true}
		for _, opt := range options {
			if err := opt.applyToOutlet(&outlet); err != nil {
				return errors.Wrapf(err, "GenerateRoutes(%q, %q)", targetDir, routePrefix)
			}
		}
		r.defaultOutlet = outlet

		return nil
	})
}

// GenerateRouter emits the application's router beside the route tables GenerateRoutes
// emits, in the same package: zz_gen_router.go holds the Handlers interface (the full
// surface the router needs), the Hooks struct (the application's additions: one field per
// outlet and two for the edges), and New(h Handlers, hooks Hooks) *chi.Mux, written linear
// and inline with the middleware chain documented at the top of the file; and
// zz_gen_router_test.go proves that chain by driving every route through New.
//
// Every outlet then declares how it authenticates, Auth or APIKey, and a session outlet
// that serves a browser application declares WebApp. Requires GenerateRoutes. Without the
// option the contract is unchanged, generated route tables plus a hand-written router,
// and Auth, APIKey, and WebApp are not accepted.
func GenerateRouter() ResourceOption {
	return resourceOption(func(r *resourceGenerator) error {
		r.genRouter = true

		return nil
	})
}

// defaultOutletName is the reserved name of the router outlet GenerateRoutes declares.
// Every resource is on it unless an @outlet annotation says otherwise; the annotation
// references it by this name to combine it with additional outlets.
const defaultOutletName = "default"

// routerOutlet is one declared router outlet: a named registration surface with its
// own route prefix. The default outlet comes from GenerateRoutes; additional outlets
// from WithRouterOutlet.
type routerOutlet struct {
	name   string
	prefix string
	// servesSessions declares the outlet a browser-session surface: the generated
	// router registers the permission-digest and user-domains routes under its
	// prefix. Always true for the default outlet; opt-in via ServesSessions() or a
	// session Auth for additional outlets.
	servesSessions bool
	// auth is the auth the generated router binds the outlet's browser sessions to
	// (Auth); nil for an API-key outlet and for an outlet under a hand-written router.
	auth *outletAuth
	// apiKey marks a machine outlet for the generated router (APIKey): no session
	// handling and no XSRF guard, the application's <Outlet>Auth middleware in front.
	apiKey bool
	// webApp is the mount path of the browser application the outlet serves (WebApp),
	// empty when it serves none.
	webApp string
	// declaredSessions records an explicit ServesSessions(), which contradicts APIKey.
	declaredSessions bool
}

// outletAuth is one Auth declaration: the auth package a session outlet binds to and the
// flavor its people sign in with.
type outletAuth struct {
	importPath string
	flavor     AuthFlavor
}

// packageName is the auth package's name as the generated router imports it, assumed
// from the import path the way goimports does.
func (a outletAuth) packageName() string {
	return assumedPackageName(a.importPath)
}

// suffix returns the outlet's contribution to generated identifiers
// (Generated<suffix>Handlers, generated<suffix>Routes, Patch<suffix>Resources);
// empty for the default outlet, whose identifiers carry no outlet name.
func (o *routerOutlet) suffix() string {
	if o.name == defaultOutletName {
		return ""
	}

	return caser.ToPascal(o.name)
}

// WithRouterOutlet declares an additional router outlet: a second generated
// registration surface (its own Generated<Name>Handlers interface and
// generated<Name>Routes function) served under its own route prefix, so the
// application can compose different authentication and middleware around it.
// Resources, computed resources, and RPC methods join an outlet via the @outlet
// annotation; without the annotation they stay on the default outlet declared by
// GenerateRoutes, which the annotation references by its reserved name "default".
//
// The name must be a lowerCamelCase identifier (it is Pascal-cased into generated
// identifiers), and the route prefix must be a static path segment distinct from —
// and not nested with — every other outlet's prefix, so the outlets' URL spaces
// stay disjoint. The option may be passed once per additional outlet and requires
// GenerateRoutes. Outlet options (ServesSessions) refine the declaration.
func WithRouterOutlet(name, routePrefix string, options ...OutletOption) ResourceOption {
	return resourceOption(func(r *resourceGenerator) error {
		if !outletNamePattern.MatchString(name) {
			return errors.Newf("WithRouterOutlet(%q) requires a lowerCamelCase name matching %s", name, outletNamePattern)
		}
		if name == defaultOutletName {
			return errors.Newf("WithRouterOutlet(%q) redeclares the reserved default outlet; GenerateRoutes declares it", name)
		}
		if routePrefix == "" {
			return errors.Newf("WithRouterOutlet(%q) requires a non-empty route prefix", name)
		}
		if strings.ContainsAny(routePrefix, "{}") || strings.Trim(routePrefix, "/") != routePrefix {
			return errors.Newf("WithRouterOutlet(%q, %q) route prefix must not contain '{', '}', or leading/trailing '/'", name, routePrefix)
		}

		outlet := routerOutlet{name: name, prefix: routePrefix}
		for _, opt := range options {
			if err := opt.applyToOutlet(&outlet); err != nil {
				return errors.Wrapf(err, "WithRouterOutlet(%q, %q)", name, routePrefix)
			}
		}
		r.extraOutlets = append(r.extraOutlets, outlet)

		return nil
	})
}

// OutletOption refines one outlet declaration: the default outlet's on GenerateRoutes,
// an additional outlet's on WithRouterOutlet.
type OutletOption interface {
	applyToOutlet(*routerOutlet) error
}

// outletOption adapts a function to OutletOption.
type outletOption func(*routerOutlet) error

func (f outletOption) applyToOutlet(o *routerOutlet) error { return f(o) }

// ServesSessions declares that the outlet serves browser sessions: the generated
// router registers the permission-digest and user-domains routes under the outlet's
// prefix — behind whatever session middleware the application composes around the
// outlet, exactly like its resource routes — and the application's generated
// PermissionDigest and UserDomains handlers serve them. The default outlet always
// serves sessions; an outlet without the declaration gets no permission routes, and
// a GenerateTypescript target may only name a session-serving outlet (ForOutlet).
// Under GenerateRouter a session Auth declares the same, so ServesSessions is for
// applications that keep a hand-written router.
func ServesSessions() OutletOption {
	return outletOption(func(o *routerOutlet) error {
		o.servesSessions = true
		o.declaredSessions = true

		return nil
	})
}

// AuthFlavor is how a session outlet's people sign in: the session library flavor whose
// handlers the generated router mounts under the outlet's prefix.
type AuthFlavor string

// The auth flavors the generated router composes.
const (
	// Password signs in with a username and password (session.PasswordAuthHandlers):
	// POST user/login, then GET and DELETE user/session.
	Password AuthFlavor = "password"
	// OIDCGoogle signs in through a Google Workspace directory
	// (session.OIDCGoogleHandlers): GET user/login sends the browser to the directory,
	// GET user/callback receives it back, then GET and DELETE user/session.
	OIDCGoogle AuthFlavor = "oidc-google"
	// OIDCAzure signs in through an Azure directory (session.OIDCAzureHandlers):
	// Google's routes plus GET user/logout, the directory's front-channel logout.
	OIDCAzure AuthFlavor = "oidc-azure"
)

// Auth declares the auth a session outlet binds to, for the generated router
// (GenerateRouter). importPath is the auth package: the package exporting Name whose
// session handlers serve the outlet (the default outlet's are embedded in Handlers,
// an additional outlet's come from a getter named after it), and flavor is how its
// people sign in, which decides the login routes the router mounts under the outlet's
// prefix. The default outlet declares it on GenerateRoutes, an additional outlet on
// WithRouterOutlet. A session flavor makes the outlet serve sessions exactly as
// ServesSessions does.
//
// When more than one session auth is declared the generated router binds every request
// in the outlet's group to its auth, BindAuth(<pkg>.Name), so a misspelled or removed
// auth package is a compile error; with one session auth the package is not imported.
func Auth(importPath string, flavor AuthFlavor) OutletOption {
	return outletOption(func(o *routerOutlet) error {
		if importPath == "" || strings.ContainsAny(importPath, " \t\n\"") || strings.Trim(importPath, "/") != importPath {
			return errors.Newf("Auth(%q) requires an import path: the auth package, such as \"github.com/acme/beacon/pkg/auth/staff\"", importPath)
		}
		if _, ok := authFlavors[flavor]; !ok {
			return errors.Newf("Auth(%q, %q) names an unknown flavor; the flavors are Password, OIDCGoogle, and OIDCAzure", importPath, flavor)
		}
		if o.auth != nil {
			return errors.Newf("Auth(%q, %q) redeclares the outlet's auth (%q, %q): an outlet binds to one auth", importPath, flavor, o.auth.importPath, o.auth.flavor)
		}
		o.auth = &outletAuth{importPath: importPath, flavor: flavor}
		o.servesSessions = true

		return nil
	})
}

// APIKey declares a machine outlet for the generated router (GenerateRouter): its group
// carries no session handling and no XSRF guard; NoCaching, CompressionMiddleware, and
// the application's <Outlet>Auth middleware run in front of the outlet's routes, and
// <Outlet>Auth binds each request to a service identity the way the session middleware
// binds a browser request to its user. An API-key outlet serves no sessions.
func APIKey() OutletOption {
	return outletOption(func(o *routerOutlet) error {
		o.apiKey = true

		return nil
	})
}

// WebApp declares the browser application a session outlet serves, for the generated
// router (GenerateRouter): mountPath is where it is mounted ("/" for the application
// at the root, "/portal" for one under a path), and the application supplies its
// DeepLink and Assets handlers, prefixed with the outlet's name for an additional
// outlet (PortalDeepLink, PortalAssets). The router mounts every web app after the
// outlets, longer paths first, so "/" is the catch-all; an outlet without the
// declaration mounts no assets.
func WebApp(mountPath string) OutletOption {
	return outletOption(func(o *routerOutlet) error {
		if mountPath == "" || !strings.HasPrefix(mountPath, "/") || strings.ContainsAny(mountPath, "{}* \t\n\"") || (mountPath != "/" && strings.HasSuffix(mountPath, "/")) {
			return errors.Newf("WebApp(%q) requires a mount path starting with '/' and without a trailing '/', such as \"/\" or \"/portal\"", mountPath)
		}
		if o.webApp != "" {
			return errors.Newf("WebApp(%q) redeclares the outlet's browser application (%q): an outlet serves one", mountPath, o.webApp)
		}
		o.webApp = mountPath

		return nil
	})
}

// outletNamePattern constrains outlet names to lowerCamelCase identifiers so the
// Pascal-cased generated identifiers are unambiguous.
var outletNamePattern = regexp.MustCompile(`^[a-z][a-zA-Z0-9]*$`)

// Default route segment for domain-scoped resources (see WithDomainRoute):
// /{prefix}/{defaultDomainRouteSegment}/{param}/... . The parameter defaults to
// defaultDomainRouteParam and is re-derived after parsing when a resource's route
// name equals the segment (deriveDomainRouteParam). Generated code references the
// parameter name as the router package's Domain const value.
const (
	defaultDomainRouteSegment = "domains"
	defaultDomainRouteParam   = "domain"
)

// defaultApplicationName is the generated application struct's name when the
// ApplicationName option is not used.
const defaultApplicationName = "App"

// WithDomainRoute customizes the static path segment that domain-scoped resources
// (@permissionScope(domain)) are served under: WithDomainRoute("organizations")
// serves domain-scoped resources and RPC methods at
// /{prefix}/organizations/{param}/... . The default segment is "domains".
//
// The route parameter name is derived, never configured. When a resource's route
// name equals the segment (the tenant-record pattern) the parameter must be that
// resource's read-route parameter — chi permits one wildcard name per tree
// position — so it is ToGoCamel(name+pkName). With no matching resource the name
// is a cosmetic pattern label: the default "domain". The generated router const
// is always named Domain; the derived parameter is its value.
func WithDomainRoute(segment string) ResourceOption {
	return resourceOption(func(r *resourceGenerator) error {
		if segment == "" {
			return errors.New("WithDomainRoute() requires a non-empty segment")
		}
		if strings.ContainsAny(segment, "/{}") {
			return errors.Newf("WithDomainRoute(%q) must not contain '/', '{', or '}'", segment)
		}

		r.domainRouteSegment = segment

		return nil
	})
}

// WithConcealedDomains makes "unauthorized" indistinguishable from "nonexistent" on
// every surface that names a domain (ABAC design plan §06, the existence oracle): the
// generated DomainGuard and the consolidated dispatcher consult the application's
// DomainVisible(ctx, user, domain) — the domain exists AND the caller holds at least
// one grant in it — instead of DomainExists, answering the same not-found either way.
// A caller with any foothold in the domain still receives ordinary 403s for the
// specific permissions they lack. Off by default: most applications' tenant lists are
// not secret, and the distinct errors are better DX; opt in when tenant existence is
// itself sensitive (e.g. a client list).
func WithConcealedDomains() ResourceOption {
	return resourceOption(func(r *resourceGenerator) error {
		r.concealedDomains = true

		return nil
	})
}

// GenerateTypescript enables TypeScript generation as part of the resource generator run.
// The permission data is computed statically from the parsed resources, so the run needs
// no compiled application router.
//
// The option may be passed multiple times, once per target directory, each call carrying
// its own TypeScript-specific options — a shared resource package emits its TypeScript
// into every consuming application this way. Target directories must be distinct.
//
// It accepts only TypeScript-specific options: GenerateMetadata, GeneratePermissions,
// GenerateEnums, and WithTypescriptOverrides. Everything else — package locations
// (WithVirtualResources, WithComputedResources, WithRPC), the Spanner emulator version,
// plural overrides, and consolidated handlers — is a ResourceOption inherited from the
// enclosing NewResourceGenerator options, so nesting one here fails to compile.
// GeneratePermissions and GenerateMetadata render from the permission collection, so
// they additionally require GenerateRoutes or manual declarations (@manualAddResource,
// @manualAddResourceSet, WithManualResources); enum output reads only the schema and
// carries no such requirement.
func GenerateTypescript(targetDir string, options ...TSOption) ResourceOption {
	return resourceOption(func(r *resourceGenerator) error {
		r.typescriptTargets = append(r.typescriptTargets, typescriptTarget{destination: targetDir, options: options})

		return nil
	})
}

// typescriptTarget is one recorded GenerateTypescript call: a target directory and the
// TypeScript-specific options that shape what is emitted there.
type typescriptTarget struct {
	destination string
	options     []TSOption
}

// resolve applies the target's options onto a fresh typescriptGenerator, yielding its
// resolved flag set and destination; the caller attaches the shared client and the
// permission collection.
func (target typescriptTarget) resolve() (*typescriptGenerator, error) {
	t := &typescriptGenerator{typescriptDestination: target.destination}

	opts := make([]option, 0, len(target.options))
	for _, opt := range target.options {
		opts = append(opts, opt)
	}
	if err := resolveOptions(t, opts); err != nil {
		return nil, err
	}

	return t, nil
}

// WithManualResources declares permission registrations the generator cannot derive from
// generated handlers: resources registered by hand-written routes (e.g. a Require()
// middleware calling Collection.AddResource). Each declared registration is included in
// the generated permission collection and the generated TypeScript constants.
func WithManualResources(registrations ...ManualRegistration) ResourceOption {
	return resourceOption(func(r *resourceGenerator) error {
		for _, reg := range registrations {
			if reg.Resource == "" {
				return errors.New("manual registration requires a resource name")
			}
			if reg.Permission == accesstypes.NullPermission {
				return errors.Newf("manual registration for resource %q requires a permission", reg.Resource)
			}
		}

		r.manualRegistrations = append(r.manualRegistrations, registrations...)

		return nil
	})
}

// WithTypescriptOverrides sets the Typescript type for a given Go type.
func WithTypescriptOverrides(overrides map[string]string) TSOption {
	return tsOption(func(t *typescriptGenerator) error {
		tempMap := defaultTypescriptOverrides()
		maps.Copy(tempMap, overrides)
		t.typescriptOverrides = tempMap

		return nil
	})
}

// ForOutlet names the router outlet a GenerateTypescript target serves: every file
// the target emits — constants, resource and method metadata, enums, and the client
// descriptor — is filtered to the outlet's members (see WithRouterOutlet and the
// @outlet annotation), so one target never spans two outlets. Without the option a
// target serves the default outlet declared by GenerateRoutes.
//
// The named outlet must be declared, and must serve browser sessions
// (ServesSessions): the generated client reads its permission digest and
// user-domains channels under the outlet's prefix, and a client without those
// channels would fail closed on every page.
func ForOutlet(name string) TSOption {
	return tsOption(func(t *typescriptGenerator) error {
		if name == "" {
			return errors.New("ForOutlet() requires an outlet name")
		}
		t.outletName = name

		return nil
	})
}

// GeneratePermissions enables generating resource and resource-field level permission
// mappings, computed statically from the permission collection (see GenerateTypescript).
func GeneratePermissions() TSOption {
	return tsOption(func(t *typescriptGenerator) error {
		t.genPermission = true

		return nil
	})
}

// GenerateMetadata enables generating information necessary for Typescript configuration of resources.
func GenerateMetadata() TSOption {
	return tsOption(func(t *typescriptGenerator) error {
		t.genMetadata = true

		return nil
	})
}

// GenerateEnums enables generating constants for resources that have been tagged with `@enumerate`
// and have Id and Description values in the schema migrations directory.
func GenerateEnums() TSOption {
	return tsOption(func(t *typescriptGenerator) error {
		t.genEnums = true

		return nil
	})
}

// WithSpannerEmulatorVersion sets the version of the Spanner image pulled from gcr.io
func WithSpannerEmulatorVersion(version string) ResourceOption {
	return Option(func(g any) error {
		switch t := g.(type) {
		case *client:
			t.spannerEmulatorVersion = version
		case *resourceGenerator, *typescriptGenerator: // no-op
		default:
			panic(fmt.Sprintf("unexpected generator type in WithSpannerEmulatorVersion(): %T", t))
		}

		return nil
	})
}

// WithPluralOverrides sets the pluralization for any resource names that are not
// handled correctly by the default pluralization rules.
func WithPluralOverrides(overrides map[string]string) ResourceOption {
	tempMap := maps.Clone(overrides)

	return Option(func(g any) error {
		switch t := g.(type) {
		case *client:
			t.pluralOverrides = tempMap
		case *resourceGenerator, *typescriptGenerator: // no-op
		default:
			panic(fmt.Sprintf("unexpected generator type in WithPluralOverrides(): %T", t))
		}

		return nil
	})
}

// CaserInitialismOverrides sets the initialism for any resources that are not covered by the default initialisms.
func CaserInitialismOverrides(overrides map[string]bool) ResourceOption {
	return Option(func(g any) error {
		switch t := g.(type) {
		case *client:
			caser = strcase.NewCaser(false, overrides, nil)
		case *resourceGenerator, *typescriptGenerator: // no-op
		default:
			panic(fmt.Sprintf("unexpected generator type in CaserInitialismOverrides(): %T", t))
		}

		return nil
	})
}

// WithConsolidatedHandlers enables generating a handler file for all or a list of resources.
func WithConsolidatedHandlers(route string, consolidateAll bool, resources ...string) ResourceOption {
	return Option(func(g any) error {
		if !consolidateAll && len(resources) == 0 {
			return errors.New("at least one resource is required if not consolidating all handlers")
		}

		switch t := g.(type) {
		case *client:
			t.ConsolidatedRoute = route
			t.ConsolidateAll = consolidateAll
			t.ConsolidatedResourceNames = resources
		case *resourceGenerator, *typescriptGenerator: // no-op
		default:
			panic(fmt.Sprintf("unexpected generator type in WithConsolidatedHandlers(): %T", t))
		}

		return nil
	})
}

// WithVirtualResources enables generating resources utilities, routes and handlers for Virtual Resources.
// The package's name is expected to be the same as its directory name.
func WithVirtualResources(virtualResourcesPkgDir string) ResourceOption {
	return Option(func(g any) error {
		switch t := g.(type) {
		case *resourceGenerator:
		case *typescriptGenerator: // no-op
		case *client:
			t.genVirtualResources = true
			t.virtual = packageDir(virtualResourcesPkgDir)
			t.loadPackages = append(t.loadPackages, virtualResourcesPkgDir)
		default:
			panic(fmt.Sprintf("unexpected generator type in WithVirtualResources(): %T", t))
		}

		return nil
	})
}

// WithComputedResources enables generating routes and handlers for Computed Resources.
// The package's name is expected to be the same as its directory name.
func WithComputedResources(compResourcesPkgDir string) ResourceOption {
	return Option(func(g any) error {
		switch t := g.(type) {
		case *resourceGenerator:
		case *typescriptGenerator: // no-op
		case *client:
			t.genComputedResources = true
			t.computed = packageDir(compResourcesPkgDir)
			t.loadPackages = append(t.loadPackages, compResourcesPkgDir)
		default:
			panic(fmt.Sprintf("unexpected generator type in WithComputedResources(): %T", t))
		}

		return nil
	})
}

// WithRPC enables generating RPC method handlers.
// The package's name is expected to be the same as its directory name.
func WithRPC(rpcPackageDir string) ResourceOption {
	return Option(func(g any) error {
		switch t := g.(type) {
		case *resourceGenerator:
		case *typescriptGenerator: // no-op
		case *client:
			t.rpc = packageDir(rpcPackageDir)
			t.genRPCMethods = true
			t.loadPackages = append(t.loadPackages, rpcPackageDir)
		default:
			panic(fmt.Sprintf("unexpected generator type in WithRPC(): %T", t))
		}

		return nil
	})
}

// resolveOptions is called twice, once in the client constructor and once in either the resource or typescript generator's constructor.
// That is why no-op cases are included to prevent falling through to the default panic case.
func resolveOptions(generator any, options []option) error {
	for _, optionFunc := range options {
		if optionFunc != nil {
			switch fn := optionFunc.(type) {
			case resourceOption:
				switch g := generator.(type) {
				case *resourceGenerator:
					if err := fn(g); err != nil {
						return err
					}
				case *client: // no-op
				default:
					panic(fmt.Sprintf("unexpected generator type in resourceOption: %T", g))
				}
			case tsOption:
				switch g := generator.(type) {
				case *typescriptGenerator:
					if err := fn(g); err != nil {
						return err
					}
				case *client: // no-op
				default:
					panic(fmt.Sprintf("unexpected generator type in tsOption: %T", g))
				}
			case Option:
				if err := fn(generator); err != nil {
					return err
				}
			}
		}
	}

	switch g := generator.(type) {
	case *resourceGenerator:
		if err := applyResourceGeneratorDefaults(g); err != nil {
			return err
		}

	case *typescriptGenerator:
		if g.typescriptOverrides == nil {
			g.typescriptOverrides = defaultTypescriptOverrides()
		}
		if g.spannerEmulatorVersion == "" {
			g.spannerEmulatorVersion = "latest"
		}
		if g.outletName == "" {
			g.outletName = defaultOutletName
		}
	case *client: // no-op
	default:
		panic(fmt.Sprintf("unexpected generator type: %T", g))
	}

	return nil
}

// applyResourceGeneratorDefaults fills option defaults after all options have been
// applied, so defaults that depend on other options (e.g. the collection directory
// following the routes directory) see the final configuration.
func applyResourceGeneratorDefaults(g *resourceGenerator) error {
	if g.spannerEmulatorVersion == "" {
		g.spannerEmulatorVersion = "latest"
	}
	if g.applicationName == "" {
		g.applicationName = defaultApplicationName
	}
	g.receiverName = strings.ToLower(string(g.applicationName[0]))
	if g.domainRouteSegment == "" {
		g.domainRouteSegment = defaultDomainRouteSegment
	}
	if g.genHandlerTests && (!g.genHandlers || !g.genRoutes) {
		return errors.New("GenerateHandlerTests requires GenerateHandlers and GenerateRoutes: the generated suite drives the generated handlers through the generated test router")
	}
	if g.genRouter && !g.genRoutes {
		return errors.New("GenerateRouter requires GenerateRoutes: the generated router serves the generated route tables")
	}
	if g.domainRouteParam == "" {
		g.domainRouteParam = defaultDomainRouteParam
	}

	// Each GenerateTypescript call owns one directory; two calls writing the same files
	// to the same place is always a configuration mistake.
	seen := make(map[string]struct{}, len(g.typescriptTargets))
	for _, target := range g.typescriptTargets {
		dir := filepath.Clean(target.destination)
		if _, ok := seen[dir]; ok {
			return errors.Newf("GenerateTypescript(%q) is declared more than once: each call must name a distinct target directory", target.destination)
		}
		seen[dir] = struct{}{}
	}

	mapped, err := mappedTypes(g.typescriptTargets)
	if err != nil {
		return err
	}
	g.mappedTypes = mapped

	return nil
}

// mappedTypes folds the built-in TypeScript type table and every target's overrides
// into the one leaf table the wire walker reads. The Go side of a shape cannot
// depend on which browser app receives it, so two targets mapping one type
// differently is a configuration error.
func mappedTypes(targets []typescriptTarget) (map[string]string, error) {
	mapped := defaultTypescriptOverrides()
	for _, target := range targets {
		t, err := target.resolve()
		if err != nil {
			return nil, err
		}
		for goType, tsType := range t.typescriptOverrides {
			if existing, ok := mapped[goType]; ok && existing != tsType {
				return nil, errors.Newf("GenerateTypescript(%q) maps %s to %s, but another target maps it to %s: every target must agree on a type's mapping", target.destination, goType, tsType, existing)
			}
			mapped[goType] = tsType
		}
	}

	return mapped, nil
}

const (
	stringGoType     = "string"
	boolGoType       = "bool"
	intGoType        = "int"
	int8GoType       = "int8"
	int16GoType      = "int16"
	int32GoType      = "int32"
	int64GoType      = "int64"
	uintGoType       = "uint"
	uint8GoType      = "uint8"
	uint16GoType     = "uint16"
	uint32GoType     = "uint32"
	uint64GoType     = "uint64"
	uintptrGoType    = "uintptr"
	float32GoType    = "float32"
	float64GoType    = "float64"
	complex64GoType  = "complex64"
	complex128GoType = "complex128"
	cccUUIDGoType    = "ccc.UUID"
	civilDateGoType  = "civil.Date"
)

// jsonTrueLiteral is the JSON boolean literal the authorization matrix's synthesized
// update values use.
const jsonTrueLiteral = "true"

// TypeScript type names emitted by the generator.
const (
	stringTSType    = "string"
	linkTSType      = "Link"
	numberTSType    = "number"
	uuidTSType      = "uuid"
	dateTSType      = "Date"
	civilDateTSType = "civilDate"
	// nullBooleanTSType is the wire type of a nullable boolean, a value type the
	// generated TypeScript imports from @cccteam/resource.
	nullBooleanTSType = "NullBoolean"
)

func defaultTypescriptOverrides() map[string]string {
	return map[string]string{
		reflect.TypeFor[ccc.UUID]().String():            uuidTSType,
		reflect.TypeFor[ccc.NullUUID]().String():        uuidTSType,
		reflect.TypeFor[resource.Link]().String():       linkTSType,
		reflect.TypeFor[resource.NullLink]().String():   linkTSType,
		reflect.TypeFor[decimal.Decimal]().String():     numberTSType,
		reflect.TypeFor[decimal.NullDecimal]().String(): numberTSType,
		reflect.TypeFor[time.Time]().String():           dateTSType,
		reflect.TypeFor[civil.Date]().String():          civilDateTSType,
		boolGoType:                                      booleanStr,
		stringGoType:                                    stringTSType,
		intGoType:                                       numberTSType,
		int8GoType:                                      numberTSType,
		int16GoType:                                     numberTSType,
		int32GoType:                                     numberTSType,
		int64GoType:                                     numberTSType,
		uintGoType:                                      numberTSType,
		uint8GoType:                                     numberTSType,
		uint16GoType:                                    numberTSType,
		uint32GoType:                                    numberTSType,
		uint64GoType:                                    numberTSType,
		uintptrGoType:                                   numberTSType,
		float32GoType:                                   numberTSType,
		float64GoType:                                   numberTSType,
		complex64GoType:                                 numberTSType,
		complex128GoType:                                numberTSType,
	}
}
