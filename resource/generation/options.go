package generation

import (
	"fmt"
	"maps"
	"path"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"time"

	"cloud.google.com/go/civil"
	"cloud.google.com/go/spanner"
	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/securehash"
	"github.com/ettle/strcase"
	"github.com/go-playground/errors/v5"
	"github.com/shopspring/decimal"
	"golang.org/x/mod/semver"
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
// route — list and read on every outlet, domain-scoped or global, and every mutation
// arm — through the generated test router. Without the required permission the
// pipeline must fail closed with 403 (404 or 400 under concealed domains); with
// exactly that permission a list or read must reach data access (200, or 404 on the
// empty schema). Mutation arms are denied-only. The target package hand-writes exactly
// one function the generated suite calls:
//
//	newTestHandler(t *testing.T, db *initiator.SpannerDB, g grants) http.Handler
//
// where the application constructs its App around the test database with the scripted
// grants and composes it through router.NewTestRouter. The scripted grants are
// unconditional, so the matrix pins the endpoint gate alone: a granted case proves the
// request passed the gate and reached data access, never that a row is visible.
// Conditional grants are proven by the application's own integration suites over
// seeded rows and the real permission engine. Requires GenerateHandlers and
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
// the auth its browser sessions come from, or Tokens makes it a machine surface carrying
// that auth's bearer tokens, and WebApp names the browser application it serves.
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
// and inline with the middleware chain documented at the top of the file;
// zz_gen_router_test.go proves that chain by driving every route through New; and
// zz_gen_release.json (resource.ReleaseFileName) names every outlet and each session
// outlet's oldest answered release, for a deploy reading the checkout
// (resource.ReadReleaseFile).
//
// Every outlet then declares how it authenticates, Auth or Tokens, a session outlet
// that serves a browser application declares WebApp, and a machine outlet that serves
// the programs the release ships declares ShipsProgram. Requires GenerateRoutes. Without
// the option the contract is unchanged, generated route tables plus a hand-written
// router, and Auth, Tokens, WebApp, and ShipsProgram are not accepted.
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
	// auth is the auth the generated router binds the outlet's browser sessions to
	// (Auth); nil for a machine outlet and for an outlet under a hand-written router.
	auth *outletAuth
	// webApp is the mount path of the browser application the outlet serves (WebApp),
	// empty when it serves none.
	webApp string
	// oldestAnswered is the oldest release of the browser application the outlet
	// still answers (OldestAnswered): a release, ThisRelease, or empty for every
	// release that sends the header. declaredOldest, below, records the declaration,
	// which contradicts Tokens and describes the generated router.
	oldestAnswered string
	// servesSessions declares the outlet a browser-session surface: the generated
	// router registers the permission-digest, user-domains and live routes under its
	// prefix. True for the default outlet unless it declares Tokens; opt-in via
	// ServesSessions() or a session Auth for additional outlets. A machine outlet
	// never serves sessions: its generated routes are the resource routes its tokens
	// open and the device flow's endpoints, nothing else.
	servesSessions bool
	// apiKey marks a machine outlet for the generated router (Tokens, or the retired
	// APIKey): no session handling and no XSRF guard, the token check in front.
	// tokenAuth is the import path of the auth package whose Auth issues the outlet's
	// tokens (Tokens); empty under the retired APIKey, which generation refuses.
	apiKey    bool
	tokenAuth string
	// declaredSessions records an explicit ServesSessions(), which contradicts Tokens.
	declaredSessions bool
	declaredOldest   bool
	// requestLog is the outlet's request log word (OutletRequestLog), which the
	// generated router sets on every request in the outlet's group; traces is its trace
	// setting (OutletTraces), which the router's surface table carries. Either is
	// undeclared when the outlet takes the application's.
	requestLog RequestLog
	traces     Traces
	// manages are the import paths of the other Auths whose user-management surfaces
	// this outlet serves beside its own (Manages), in declaration order.
	manages []string
	// programs are the programs a machine outlet ships to their installed copies
	// (ShipsProgram), in declaration order; a session outlet ships none, which
	// generation refuses.
	programs []shippedProgram
}

// shippedProgram is one ShipsProgram declaration: the main package's root-relative
// directory, the program's name (the directory's base), and the platforms the release
// compiles it for.
type shippedProgram struct {
	dir       string
	name      string
	platforms []string
}

// outletAuth is one Auth declaration: the auth package a session outlet binds to and the
// sign-in methods registered on its Auth, in declaration order.
type outletAuth struct {
	importPath string
	methods    []AuthMethod
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
// PermissionDigest and UserDomains handlers serve them. The default outlet serves
// sessions unless it declares Tokens; an outlet without the declaration gets no
// permission routes, and a GenerateTypescript target may only name a session-serving
// outlet (ForOutlet).
// Under GenerateRouter a session Auth declares the same, so ServesSessions is for
// applications that keep a hand-written router.
// The generator then does not know the outlet's auth, so the outlet's routes function
// and NewTestRouter take the auth's name, which keys each person's live pages.
func ServesSessions() OutletOption {
	return outletOption(func(o *routerOutlet) error {
		o.servesSessions = true
		o.declaredSessions = true

		return nil
	})
}

// authMethodKind is a sign-in method the session library offers: the package the
// application registers it from.
type authMethodKind string

// The sign-in methods the generated router mounts.
const (
	// methodPassword is username and password sign-in (session.PasswordSignIn): a
	// form posting to user/password/login.
	methodPassword authMethodKind = "password"
	// methodGoogle is Google Workspace sign-in (google.SignIn): user/<name>/login
	// sends the browser to the directory and user/<name>/callback receives it back.
	methodGoogle authMethodKind = "google"
	// methodAzure is Microsoft Entra ID sign-in (azure.SignIn): Google's routes plus
	// user/<name>/logout, the directory's front-channel logout.
	methodAzure authMethodKind = "azure"
	// methodWorkOS is WorkOS single sign-on (workos.SignIn): Google's routes.
	methodWorkOS authMethodKind = "workos"
)

// passwordMethodName is the name the session library registers the password method
// under; no external registration may take it.
const passwordMethodName = "password"

// AuthMethod is one sign-in method registered on a session outlet's Auth, as the
// generated router mounts it: the method, the name it is registered under, and the
// label the login page shows for it. Build one with PasswordMethod, GoogleMethod,
// AzureMethod or WorkOSMethod, and reword its label with Labeled.
type AuthMethod struct {
	kind  authMethodKind
	name  string
	label string
	// invited marks a registration admitting invited users only (InvitedUsers), and
	// syncsRoles one that writes its users' roles from the directory (SyncsRoles); both
	// shape the Auth's user-management surface. openSignUp marks a registration
	// admitting anyone (OpenSignUp): what a tenant record's self-service creation
	// shape needs on the outlet serving it.
	invited    bool
	syncsRoles bool
	openSignUp bool
	// usernameIsEmail marks the password method declared with the username as the
	// person's email address (UsernameIsEmail).
	usernameIsEmail bool
}

// PasswordMethod is the username and password method, registered as "password" with
// the label "Password". It takes UsernameIsEmail, which makes the username the
// person's email address, and OpenSignUp, which offers sign-up to anyone with an
// address.
func PasswordMethod() AuthMethod {
	return AuthMethod{kind: methodPassword, name: passwordMethodName, label: "Password"}
}

// GoogleMethod is the Google Workspace method registered under name, with the label
// "Google".
func GoogleMethod(name string) AuthMethod {
	return AuthMethod{kind: methodGoogle, name: name, label: "Google"}
}

// AzureMethod is the Microsoft Entra ID method registered under name, with the label
// "Microsoft".
func AzureMethod(name string) AuthMethod {
	return AuthMethod{kind: methodAzure, name: name, label: "Microsoft"}
}

// WorkOSMethod is the WorkOS single sign-on method registered under name, with the
// label "Single sign-on".
func WorkOSMethod(name string) AuthMethod {
	return AuthMethod{kind: methodWorkOS, name: name, label: "Single sign-on"}
}

// Labeled gives the method the label the login page shows for it, in the
// application's words ("Company directory").
func (m AuthMethod) Labeled(label string) AuthMethod {
	m.label = label

	return m
}

// InvitedUsers declares that the registration admits invited users only (the session
// library's invited-users account rule): a person signs in through the directory once
// an operator has created their account, so the Auth's user-management surface creates
// users for it, without a password. Not for the password method, whose users are
// always created here.
func (m AuthMethod) InvitedUsers() AuthMethod {
	m.invited = true

	return m
}

// SyncsRoles declares that the registration writes its users' roles from the
// directory at every sign-in (the method's RoleSync): the Auth's user-management
// surface shows every user's seats and never writes them, since a seat made by hand
// would not survive the next sign-in. Such a registration stands alone on its Auth
// (validateAuthMethods refuses another method beside it): the directory is then the
// authority for everyone the Auth holds. Not for the password method.
func (m AuthMethod) SyncsRoles() AuthMethod {
	m.syncsRoles = true

	return m
}

// OpenSignUp declares that the registration admits anyone the directory verifies (the
// session library's open sign-up account rule, signin.OpenSignUp on the registration's
// Accounts): a stranger signs in and becomes a user holding nothing. A tenant record
// declaring self-service creation (@tenant(creation: self-service) or both) needs one
// such registration on the outlet serving it, since the person creates their tenant
// after signing in. On the password method it offers sign-up to anyone with an email
// address (the session library's session.OpenSignUp): the login page starts a sign-up
// from a mailed link, and the router mounts the sign-up routes. Not beside
// InvitedUsers, which admits the opposite.
func (m AuthMethod) OpenSignUp() AuthMethod {
	m.openSignUp = true

	return m
}

// UsernameIsEmail declares that the password method's username is the person's email
// address (the session library's session.UsernameIsEmail): the users table derives the
// address from the username, the login page asks for the address, a sign-up takes no
// username, and the user-management surface takes no address beside the username and
// refuses a rename. For the password method alone.
func (m AuthMethod) UsernameIsEmail() AuthMethod {
	m.usernameIsEmail = true

	return m
}

// Name is the name the method is registered under on the Auth.
func (m AuthMethod) Name() string {
	return m.name
}

// Label is what the login page shows for the method.
func (m AuthMethod) Label() string {
	return m.label
}

// String spells the method the way the generator program declares it.
func (m AuthMethod) String() string {
	switch m.kind {
	case methodPassword:
		spelled := "PasswordMethod()"
		if m.usernameIsEmail {
			spelled += ".UsernameIsEmail()"
		}
		if m.openSignUp {
			spelled += ".OpenSignUp()"
		}

		return spelled
	case methodGoogle, methodAzure, methodWorkOS:
		spelled := fmt.Sprintf("%sMethod(%q)", caser.ToPascal(string(m.kind)), m.name)
		if m.invited {
			spelled += ".InvitedUsers()"
		}
		if m.syncsRoles {
			spelled += ".SyncsRoles()"
		}
		if m.openSignUp {
			spelled += ".OpenSignUp()"
		}

		return spelled
	default:
		return string(m.kind)
	}
}

// accessor is the Pascal-cased registration name the generated Handlers interface
// prefixes the method's handlers with: PasswordLogin, GoogleCallback.
func (m AuthMethod) accessor() string {
	return caser.ToPascal(m.name)
}

// registrationName is the shape of a registration's name: a path segment of lowercase
// letters, digits and hyphens, starting with a letter, which is also what the session
// library names registrations (azure, lakeside-entra).
var registrationName = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// validateAuthMethods checks an outlet's methods: at least one, each with a well-formed
// name no other method takes, the password method's name the library's, and a label.
func validateAuthMethods(importPath string, methods []AuthMethod) error {
	if len(methods) == 0 {
		return errors.Newf("Auth(%q) declares no sign-in method: name at least one, such as PasswordMethod() or GoogleMethod(\"google\")", importPath)
	}
	seen := make(map[string]bool, len(methods))
	for _, m := range methods {
		switch {
		case m.kind == "":
			return errors.Newf("Auth(%q) is given a zero AuthMethod: build each with PasswordMethod, GoogleMethod, AzureMethod or WorkOSMethod", importPath)
		case !registrationName.MatchString(m.name):
			return errors.Newf("Auth(%q, %s) names the registration %q; a registration name is lowercase letters, digits and hyphens, starting with a letter", importPath, m, m.name)
		case m.kind != methodPassword && m.name == passwordMethodName:
			return errors.Newf("Auth(%q, %s) names an external registration %q, the name the library keeps for the password method", importPath, m, m.name)
		case seen[m.name]:
			return errors.Newf("Auth(%q, %s) registers %q twice: each registration has its own name", importPath, m, m.name)
		case m.label == "":
			return errors.Newf("Auth(%q, %s) has an empty label: Labeled takes the words the login page shows", importPath, m)
		case m.kind == methodPassword && (m.invited || m.syncsRoles):
			return errors.Newf("Auth(%q, %s) declares InvitedUsers or SyncsRoles on the password method: they describe a registration's directory", importPath, m)
		case m.kind != methodPassword && m.usernameIsEmail:
			return errors.Newf("Auth(%q, %s) declares UsernameIsEmail on a registration: it describes the password method's username", importPath, m)
		case m.invited && m.openSignUp:
			return errors.Newf("Auth(%q, %s) declares both InvitedUsers and OpenSignUp: a registration admits invited users only or anyone, not both", importPath, m)
		}
		seen[m.name] = true
	}
	for _, m := range methods {
		if m.syncsRoles && len(methods) > 1 {
			return errors.Newf("Auth(%q, %s) declares SyncsRoles beside %d other method(s): a registration whose directory writes the roles stands alone on its Auth, since the directory is then the authority for everyone the Auth holds; give the registration an Auth of its own, or drop SyncsRoles so the application assigns the roles", importPath, m, len(methods)-1)
		}
	}

	return nil
}

// Auth declares the auth a session outlet binds to, for the generated router
// (GenerateRouter). importPath is the auth package: the package exporting Name whose
// session surface serves the outlet (the default outlet's is embedded in Handlers, an
// additional outlet's comes from a getter named after it), and methods are the sign-in
// methods registered on its Auth, which decide the login routes the router mounts
// under the outlet's prefix and what the outlet's login page is offered. The default
// outlet declares it on GenerateRoutes, an additional outlet on WithRouterOutlet. An
// Auth makes the outlet serve sessions exactly as ServesSessions does.
//
// When more than one session auth is declared the generated router binds every request
// in the outlet's group to its auth, BindAuth(<pkg>.Name), so a misspelled or removed
// auth package is a compile error; with one session auth the router does not import
// the package. The route tables import it whatever the count: the outlet's routes bind
// its Name for the live pages (live.Subscribing(<pkg>.Name)), which key each person's
// subscriptions and change set by it.
func Auth(importPath string, methods ...AuthMethod) OutletOption {
	return outletOption(func(o *routerOutlet) error {
		if importPath == "" || strings.ContainsAny(importPath, " \t\n\"") || strings.Trim(importPath, "/") != importPath {
			return errors.Newf("Auth(%q) requires an import path: the auth package, such as \"github.com/acme/beacon/pkg/auth/staff\"", importPath)
		}
		if err := validateAuthMethods(importPath, methods); err != nil {
			return err
		}
		if o.auth != nil {
			return errors.Newf("Auth(%q) redeclares the outlet's auth (%q): an outlet binds to one auth", importPath, o.auth.importPath)
		}
		o.auth = &outletAuth{importPath: importPath, methods: slices.Clone(methods)}
		o.servesSessions = true

		return nil
	})
}

// Manages declares that the outlet also serves the user-management surface of another
// Auth, named by its import path: the staff console managing the members a portal
// outlet signs in. The managed Auth must be declared with Auth on some outlet. The
// managed Auth's resources mount under this outlet's prefix exactly as under their own,
// served through the managed Auth's stores and checked in the caller's engine against
// the caller's role file; the collection is shared, so either role file may grant them,
// and an application that wants only staff to manage members leaves the resources out
// of the members' file.
func Manages(importPath string) OutletOption {
	return outletOption(func(o *routerOutlet) error {
		if importPath == "" || strings.ContainsAny(importPath, " \t\n\"") || strings.Trim(importPath, "/") != importPath {
			return errors.Newf("Manages(%q) requires an import path: the managed auth package, such as \"github.com/acme/beacon/pkg/auth/members\"", importPath)
		}
		if slices.Contains(o.manages, importPath) {
			return errors.Newf("Manages(%q) is declared twice on the outlet", importPath)
		}
		o.manages = append(o.manages, importPath)

		return nil
	})
}

// Tokens declares a machine outlet for the generated router (GenerateRouter): a URL
// space a program calls with a bearer token, never a session cookie. importPath names
// the auth package whose Auth issues the outlet's tokens, the same Auth a session
// outlet of the application declares with Auth; its store holds the tokens, and a
// signed-in person of that outlet issues an application token for a program of their
// own, or approves a command-line client's device authorization, on its token page.
//
// The generated router composes the outlet's group with no session handling and no
// XSRF guard: NoCaching, CompressionMiddleware and ReleaseHeader, the device flow's two
// public endpoints where a client obtains its tokens (POST <prefix>/oauth/device/authorize
// and POST <prefix>/oauth/token, the issuing auth's session.DeviceHandlers), then the
// outlet's token check in front of the outlet's routes: the ValidateToken of the
// machine outlet the application registers on the issuing Auth
// (session.RegisterMachineOutlet(auth, name)) and hands the router through the outlet's
// getter on Handlers (<Outlet>() for an additional outlet, embedded for the default).
// The check binds each request to the person the token acts for the way the session
// middleware binds a browser request to its user, so the handlers behind it run the
// same permission checks. The issuing outlet's session surface gains the token page's
// handlers, mounted in its validated group under <prefix>/user/tokens and
// <prefix>/user/device.
//
// A machine outlet serves no sessions: the generated router registers no
// permission-digest, user-domains or live routes under its prefix, so its generated
// routes are the resource routes its tokens authorize and nothing else, and a request
// carrying X-Subscribe is refused naming the header. An outlet declaring Tokens of an
// auth no session outlet declares is refused at generation.
func Tokens(importPath string) OutletOption {
	return outletOption(func(o *routerOutlet) error {
		if importPath == "" || strings.ContainsAny(importPath, " \t\n\"") || strings.Trim(importPath, "/") != importPath {
			return errors.Newf("Tokens(%q) requires an import path: the auth package whose Auth issues the outlet's tokens, such as \"github.com/acme/beacon/pkg/auth/staff\"", importPath)
		}
		if o.tokenAuth != "" {
			return errors.Newf("Tokens(%q) redeclares the outlet's issuing auth (%q): a machine outlet takes one auth's tokens", importPath, o.tokenAuth)
		}
		o.apiKey = true
		o.tokenAuth = importPath
		o.servesSessions = false

		return nil
	})
}

// APIKey declared a machine outlet behind an API key the application checked in a
// hand-written <Outlet>Auth middleware. The bearer tokens the session library issues
// replaced it: a machine outlet declares Tokens(importPath), naming the auth whose
// tokens it takes, and the generated router carries the token check. The option still
// compiles, so a generator program that declares it is told at generation, naming the
// outlet, rather than failing to build.
//
// Deprecated: declare Tokens(importPath).
func APIKey() OutletOption {
	return outletOption(func(o *routerOutlet) error {
		o.apiKey = true
		o.servesSessions = false

		return nil
	})
}

// ShipsProgram declares that a machine outlet serves a program the release ships to its
// installed copies, which fetch their next release there with the token they already
// carry. dir is the root-relative directory of the program's main package, "cmd/corral",
// and the program's name is the directory's base, corral. The version build compiles
// the program for the platforms the declaration names (Platforms) and copies each file
// into the image beside the server, and the generated router serves them under the
// outlet's prefix, behind its token check, at
// GET <prefix>/programs/<name>/<release>/<os>/<arch> with the file's checksum
// (resource/shipped). The release file beside the router lists the programs, so the
// build reads what to compile from the checkout.
//
// The directory is a clean relative path: no leading "/" or "./" and no "..". At least
// one platform is declared, each <os>/<arch> in lowercase letters and digits, such as
// linux/amd64. A directory declared twice on the outlet is refused. The option belongs
// on a machine outlet (Tokens); a session outlet serves no programs, which generation
// refuses.
func ShipsProgram(dir string, options ...ProgramOption) OutletOption {
	return outletOption(func(o *routerOutlet) error {
		if err := validateProgramDir(dir); err != nil {
			return err
		}
		program := shippedProgram{dir: dir, name: path.Base(dir)}
		for _, opt := range options {
			if err := opt.applyToProgram(&program); err != nil {
				return errors.Wrapf(err, "ShipsProgram(%q)", dir)
			}
		}
		if len(program.platforms) == 0 {
			return errors.Newf("ShipsProgram(%q) declares no platform: name the platforms the release compiles it for, such as Platforms(\"linux/amd64\")", dir)
		}
		for _, declared := range o.programs {
			if declared.dir == dir {
				return errors.Newf("ShipsProgram(%q) is declared twice on the outlet", dir)
			}
		}
		o.programs = append(o.programs, program)

		return nil
	})
}

// validateProgramDir refuses a directory that is not a clean relative path: empty, with
// a leading "/" or "./", with a ".." segment, with a space or a quote, or ending in a
// separator.
func validateProgramDir(dir string) error {
	switch {
	case dir == "", strings.ContainsAny(dir, " \t\n\"\\"), strings.HasPrefix(dir, "/"), strings.HasPrefix(dir, "./"), path.Clean(dir) != dir, dir == ".":
		return errors.Newf("ShipsProgram(%q) requires the program's directory as a clean relative path, such as \"cmd/corral\"", dir)
	case dir == ".." || strings.HasPrefix(dir, "../") || strings.Contains(dir, "/../") || strings.HasSuffix(dir, "/.."):
		return errors.Newf("ShipsProgram(%q) requires a directory inside the module: no \"..\"", dir)
	}

	return nil
}

// ProgramOption refines one ShipsProgram declaration.
type ProgramOption interface {
	applyToProgram(*shippedProgram) error
}

// programOption adapts a function to ProgramOption.
type programOption func(*shippedProgram) error

func (f programOption) applyToProgram(p *shippedProgram) error {
	return f(p)
}

// platformPattern is the shape of a platform, <os>/<arch> in lowercase letters and
// digits, as Go names them (GOOS/GOARCH).
var platformPattern = regexp.MustCompile(`^[a-z0-9]+/[a-z0-9]+$`)

// Platforms declares the platforms the release compiles a shipped program for, each
// <os>/<arch> as Go names them: "linux/amd64", "linux/arm64". The version build
// compiles one file per platform and the outlet serves each to the copies running on
// it. At least one is named, each in lowercase letters and digits with one slash, none
// twice, and a program declares them once.
func Platforms(platforms ...string) ProgramOption {
	return programOption(func(p *shippedProgram) error {
		if len(platforms) == 0 {
			return errors.New("Platforms() names no platform: write each as <os>/<arch>, such as \"linux/amd64\"")
		}
		if p.platforms != nil {
			return errors.Newf("Platforms(%q) redeclares the program's platforms (%q): a program declares them once", platforms, p.platforms)
		}
		for i, platform := range platforms {
			if !platformPattern.MatchString(platform) {
				return errors.Newf("Platforms(%q) names %q, which is not <os>/<arch> in lowercase letters and digits, such as \"linux/amd64\"", platforms, platform)
			}
			if slices.Contains(platforms[:i], platform) {
				return errors.Newf("Platforms(%q) names %q twice", platforms, platform)
			}
		}
		p.platforms = slices.Clone(platforms)

		return nil
	})
}

// WebApp declares the browser application a session outlet serves, for the generated
// router (GenerateRouter): mountPath is where it is mounted ("/" for the application
// at the root, "/console" for one under a path), and the application supplies its
// DeepLink and Assets handlers, prefixed with the outlet's name for an additional
// outlet (PortalDeepLink, PortalAssets). The router mounts every web app after the
// outlets, longer paths first; an outlet without the declaration mounts no assets.
//
// An application at "/" must be the only one, and is then the catch-all. An installed
// browser application's scope is every URL under its start, so beside a second
// application one at / would own the origin and the other would never install on its
// own: with several applications none is mounted at /, generation refuses WebApp("/"),
// and the router answers the root alone with a temporary redirect to the default
// outlet's application (or the first declared outlet's that serves one).
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

// ThisRelease names the server's own release as an outlet's oldest answered: only a
// browser application built from the same release is answered. A generator program
// writes it for a release that changes the API inside a maintenance window.
const ThisRelease = resource.ThisRelease

// OldestAnswered declares the oldest release of the browser application a session
// outlet still answers, for the generated router (GenerateRouter). A browser
// application sends the release it was built from in X-Api-Version with every request,
// and the server answers releases from this one up to its own, the configuration's
// APP_VERSION the application reports through Handlers.ServerVersion; an application
// outside that range is refused with 412 naming the server's release, before its body
// is read, and picks up the current build. release is a semantic version such as
// "1.5.0", or ThisRelease for a release that changes the API inside a maintenance
// window, when only the server's own release is answered. The default outlet declares
// it on GenerateRoutes, an additional outlet on WithRouterOutlet; without it every
// release that sends the header is answered. A machine outlet (Tokens) carries no
// release and refuses the option. The declaration is also written to the release file
// beside the router (resource.ReleaseFileName), which a deploy reads from the checkout
// to tell whether the release needs a maintenance window.
func OldestAnswered(release string) OutletOption {
	return outletOption(func(o *routerOutlet) error {
		if release != ThisRelease && !isRelease(release) {
			return errors.Newf("OldestAnswered(%q) names no release: write a semantic version such as \"1.5.0\", or ThisRelease", release)
		}
		if o.declaredOldest {
			return errors.Newf("OldestAnswered(%q) redeclares the outlet's oldest answered release (%q): an outlet answers from one release", release, o.oldestAnswered)
		}
		o.oldestAnswered = release
		o.declaredOldest = true

		return nil
	})
}

// OutletRequestLog declares what a request under the outlet writes to the request log,
// for the generated router (GenerateRouter): LogAlways, LogOnEvent, LogSampled or LogNever,
// with a MinSeverity floor when wanted. The router sets the word on every request in the
// outlet's group, the session routes included, ahead of the outlet's hook and its
// generated routes, so it overrides the application default (WithRequestLog) and a route's
// own word (@rpc(log:), @file(log:)) overrides it in turn: the nearest declaration wins.
// A hand-mounted prefix declared beneath the outlet (WithMountedRoutes) keeps its own
// word. The default outlet declares it on GenerateRoutes, an additional outlet on
// WithRouterOutlet; without it the outlet takes the application's word. The release file
// lists the outlet's prefix with the word, so the stack renders the cloud's request-log
// exclusion from it.
func OutletRequestLog(log RequestLog) OutletOption {
	return outletOption(func(o *routerOutlet) error {
		if !log.Declared() {
			return errors.New("OutletRequestLog(RequestLog{}) declares no word; the words are LogAlways(), LogOnEvent(), LogSampled(fraction) and LogNever()")
		}
		if err := log.validate(); err != nil {
			return errors.Wrap(err, "OutletRequestLog()")
		}
		if o.requestLog.Declared() {
			return errors.Newf("OutletRequestLog(%s) redeclares the outlet's request log word (%s): an outlet declares one", log, o.requestLog)
		}
		o.requestLog = log

		return nil
	})
}

// OutletTraces declares how the spans of requests under the outlet are sampled, for the
// generated router (GenerateRouter): TracesFollowFrontEnd, TracesCapped(rate) or TracesOff.
// The router hands the outlet's prefix with the setting to the tracer's surface table
// (tracer.Surfaces), and the sampler applies it as each request's span starts, since a
// span's sampled flag is fixed then; a route's own setting (@rpc(trace:), @file(trace:))
// is the longer match and wins. The default outlet declares it on GenerateRoutes, an
// additional outlet on WithRouterOutlet; without it the outlet's spans follow the front
// end. The release file lists the outlet's prefix with the setting.
func OutletTraces(traces Traces) OutletOption {
	return outletOption(func(o *routerOutlet) error {
		if !traces.Declared() {
			return errors.New("OutletTraces(Traces{}) declares no setting; the settings are TracesFollowFrontEnd(), TracesCapped(rate) and TracesOff()")
		}
		if err := traces.validate(); err != nil {
			return errors.Wrap(err, "OutletTraces()")
		}
		if o.traces.Declared() {
			return errors.Newf("OutletTraces(%s) redeclares the outlet's trace setting (%s): an outlet declares one", traces, o.traces)
		}
		o.traces = traces

		return nil
	})
}

// isRelease reports whether version is a semantic version, with or without the
// leading v, as the server's check reads one.
func isRelease(version string) bool {
	if version == "" {
		return false
	}
	if !strings.HasPrefix(version, "v") {
		version = "v" + version
	}

	return semver.IsValid(version)
}

// outletNamePattern constrains outlet names to lowerCamelCase identifiers so the
// Pascal-cased generated identifiers are unambiguous.
var outletNamePattern = regexp.MustCompile(`^[a-z][a-zA-Z0-9]*$`)

// The route segment pair domain-scoped resources are served under,
// /{prefix}/{segment}/{param}/..., derives from the tenant record (@tenant) once the
// package is parsed (resolveTenantRecord): the segment is the record's route name, the
// parameter its key's route parameter. These defaults stand only while nothing is
// domain-scoped, where no route reads them. Generated code references the parameter
// name as the router package's Domain const value.
const (
	defaultDomainRouteSegment = "domains"
	defaultDomainRouteParam   = "domain"
)

// defaultApplicationName is the generated application struct's name when the
// ApplicationName option is not used.
const defaultApplicationName = "App"

// WithConcealedDomains makes "unauthorized" indistinguishable from "nonexistent" on
// every surface that names a domain (ABAC design plan §06, the existence oracle): after
// the tenant roster and the session's reach (UserPermissions.Reaches, which answers a
// tenant that does not admit the session as an unknown domain in every mode), the
// generated DomainGuard and the consolidated dispatcher ask the caller's foothold in
// the domain (HasGrants) and answer a caller without one exactly as an unknown domain
// is answered. A caller with any foothold in the domain still receives ordinary 403s
// for the specific permissions they lack. Off by default: most
// applications' tenant lists are not secret, and the distinct errors are better DX; opt
// in when tenant existence is itself sensitive (e.g. a client list).
func WithConcealedDomains() ResourceOption {
	return resourceOption(func(r *resourceGenerator) error {
		r.concealedDomains = true

		return nil
	})
}

// DefaultBodyLimit is the request body limit the generated router applies when the
// application sets none (WithBodyLimit): 4 MiB.
const DefaultBodyLimit int64 = 4 << 20

// WithBodyLimit sets the request body limit the generated router applies, in bytes. One
// number bounds every resource route, the consolidated patch route, the session routes
// and the other generated JSON routes, and every RPC method that declares no maximum of
// its own (@rpc(max: 8MB)); an upload is bounded by its @upload(max:) and a live route by
// its own constant, each applied where the body is read. A body over its limit answers
// 413 naming the limit. DefaultBodyLimit when the option is absent.
func WithBodyLimit(limit int64) ResourceOption {
	return resourceOption(func(r *resourceGenerator) error {
		if limit <= 0 {
			return errors.Newf("WithBodyLimit(%d): the limit must be positive", limit)
		}
		r.bodyLimit = limit

		return nil
	})
}

// WithRequestLog sets what a request writes to the request log when nothing nearer
// declares otherwise, the application default, for the generated router
// (GenerateRouter): LogAlways, LogOnEvent, LogSampled(fraction) or LogNever, with a
// MinSeverity floor when wanted. The router builds the request logger with it
// (logger.DefaultPolicy), so every request starts with the word; an outlet's word
// (OutletRequestLog), a hand-mounted prefix's (WithMountedRoutes) and a route's own
// (@rpc(log:), @file(log:), @schedule(log:)) override it where they are declared, and a
// handler may change its own request through logger.FromReq(r).SetPolicy. Without the
// option every request's entry is written, as today. The release file lists the default
// as the surface at /, so the stack renders the cloud's request-log exclusion from it.
func WithRequestLog(log RequestLog) ResourceOption {
	return resourceOption(func(r *resourceGenerator) error {
		if !log.Declared() {
			return errors.New("WithRequestLog(RequestLog{}) declares no word; the words are LogAlways(), LogOnEvent(), LogSampled(fraction) and LogNever()")
		}
		if err := log.validate(); err != nil {
			return errors.Wrap(err, "WithRequestLog()")
		}
		if r.requestLog.Declared() {
			return errors.Newf("WithRequestLog(%s) redeclares the application's request log word (%s): declare it once", log, r.requestLog)
		}
		r.requestLog = log

		return nil
	})
}

// WithMountedRoutes declares the words of routes the application mounts by hand, under a
// path prefix, for the generated router (GenerateRouter): the routes hooks.Root registers
// outside every outlet, or an outlet's hook inside its group. The generator never sees
// those routes, since the application registers them at run time, so the declaration is
// how the router, the tracer and the release file learn them from one place: the router
// builds the request logger with the prefix (logger.PolicyByPrefix), so a request under it
// starts with the word, the longest declared prefix first, ahead of the application
// default and kept under an outlet's word; the tracer's surface table carries the prefix
// with its trace setting; and the release file lists it, so the stack renders the
// cloud's request-log exclusion from it. The author's own logger.WithPolicy in the hook
// stays available for a decision only the handler can make.
//
// prefix is the path as mounted, starting with /, matched as a prefix of the request's
// path: write it with its trailing slash, /beacons/, to mean everything under it. Both
// words are declared; LogAlways() and TracesFollowFrontEnd() name today's behavior. The
// option may be passed once per prefix.
func WithMountedRoutes(prefix string, log RequestLog, traces Traces) ResourceOption {
	return resourceOption(func(r *resourceGenerator) error {
		switch {
		case prefix == "/":
			return errors.New(`WithMountedRoutes("/") declares the whole application; WithRequestLog declares the application default`)
		case !strings.HasPrefix(prefix, "/") || strings.ContainsAny(prefix, "{}* \t\n\""):
			return errors.Newf("WithMountedRoutes(%q) requires a path prefix starting with '/', such as \"/beacons/\"", prefix)
		case !log.Declared():
			return errors.Newf("WithMountedRoutes(%q) declares no request log word; the words are LogAlways(), LogOnEvent(), LogSampled(fraction) and LogNever(), and LogAlways() is today's behavior", prefix)
		case !traces.Declared():
			return errors.Newf("WithMountedRoutes(%q) declares no trace setting; the settings are TracesFollowFrontEnd(), TracesCapped(rate) and TracesOff(), and TracesFollowFrontEnd() is today's behavior", prefix)
		}
		if err := log.validate(); err != nil {
			return errors.Wrapf(err, "WithMountedRoutes(%q)", prefix)
		}
		if err := traces.validate(); err != nil {
			return errors.Wrapf(err, "WithMountedRoutes(%q)", prefix)
		}
		for _, mounted := range r.mountedRoutes {
			if mounted.prefix == prefix {
				return errors.Newf("WithMountedRoutes(%q) is declared twice: declare each prefix once", prefix)
			}
		}
		r.mountedRoutes = append(r.mountedRoutes, mountedRoutes{prefix: prefix, requestLog: log, traces: traces})

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
// GenerateEnums, and ForOutlet. Everything else — package locations
// (WithVirtualResources, WithComputedResources, WithRPC), the Spanner emulator version,
// plural overrides, and consolidated handlers — is a ResourceOption inherited from the
// enclosing NewResourceGenerator options, so nesting one here fails to compile.
// GeneratePermissions and GenerateMetadata render from the permission collection, so
// they additionally require GenerateRoutes or manual declarations (@manualAddResource,
// @manualAddResourceSet, WithManualResources); enum output reads only the schema and
// carries no such requirement.
//
// A field's TypeScript type comes from its Go type alone: the generator's built-in
// table for the basic and library types, a derived interface for a struct, and the
// @typescript declaration on a type whose shape lives outside Go (README, "TypeScript
// types for columns"). There is no per-target type option.
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

// WithTypes names an application package the generator may write the generated method
// pairs into: the JSON pair (MarshalJSON and UnmarshalJSON) of a type declared over a
// type with JSON methods, and the Spanner pair (EncodeSpanner and DecodeSpanner) of a
// type a JSON column holds, each into its own file (zz_gen_json.go, zz_gen_storage.go)
// in that package. Without it the writable set is the resources package and the
// virtual, computed, and RPC packages when configured, and a type declared elsewhere is
// refused naming this option among its fixes. It is for a shared package whose types the
// resources package uses: an external system's client package whose model type a column
// holds, which a move into the resources package would misplace and a wrapper would
// make a conversion at every boundary, or a package the resources package already
// imports, which can never receive its types back without an import cycle. Nothing else
// is generated into the package and its structs do not become resources. The package's
// name is expected to be the same as its directory name; the option may be given more
// than once, and to one generator run when a module has several.
func WithTypes(typesPackageDir string) ResourceOption {
	return Option(func(g any) error {
		switch t := g.(type) {
		case *resourceGenerator:
		case *typescriptGenerator: // no-op
		case *client:
			t.types = append(t.types, packageDir(typesPackageDir))
			t.loadPackages = append(t.loadPackages, typesPackageDir)
		default:
			panic(fmt.Sprintf("unexpected generator type in WithTypes(): %T", t))
		}

		return nil
	})
}

// WithImports names import paths the generated code may reference beyond what the
// generator derives from the packages it loads and writes (the resources, RPC, virtual,
// computed and WithTypes packages by their type-checked paths; the handlers and routes
// packages by the module path plus their directory). Each path's package name is
// assumed from the path. It is the escape hatch: every use of it is a generator gap to
// file as a bug against the generator, since a qualifier the derived set does not
// resolve fails the run naming the file, the template and the qualifier.
// Standard-library paths are ignored, as before: goimports resolves them natively.
func WithImports(paths ...string) ResourceOption {
	return Option(func(g any) error {
		switch t := g.(type) {
		case *resourceGenerator:
		case *typescriptGenerator: // no-op
		case *client:
			t.imports = append(t.imports, paths...)
		default:
			panic(fmt.Sprintf("unexpected generator type in WithImports(): %T", t))
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
		// The target's options are checked now, so a bad one fails construction.
		if _, err := target.resolve(); err != nil {
			return err
		}
	}

	return nil
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
	numberTSType    = "number"
	uuidTSType      = "uuid"
	dateTSType      = "Date"
	civilDateTSType = "civilDate"
	// bytesTSType is the display type of a byte slice: a string in the interface, since
	// encoding/json carries a []byte as a base64 string and a nil one as null.
	bytesTSType = "bytes"
	// nullBooleanTSType is the wire type of a nullable boolean, a value type the
	// generated TypeScript imports from @cccteam/resource.
	nullBooleanTSType = "NullBoolean"
)

// defaultTypescriptOverrides is the generator's built-in TypeScript type table, keyed
// by qualified Go type name: the basic types, the library types a column or a wire
// field may carry (UUIDs, decimals, times, dates), the Spanner Null wrappers
// (nullability comes from the schema, so a nullable spanner.NullBool column renders
// nullboolean exactly as *bool does), securehash.Hash as its text form, and
// spanner.NullJSON and json.RawMessage as unknown, a value with no fixed shape (the two
// share one wire form, any JSON value or null, and neither can carry a @typescript
// declaration). ccc.NullEnum[T] resolves to its type argument's row (leaf.go).
// Everything else is a struct the generator derives an interface for, a type carrying a
// @typescript declaration, or a refusal.
func defaultTypescriptOverrides() map[string]string {
	return map[string]string{
		reflect.TypeFor[ccc.UUID]().String():            uuidTSType,
		reflect.TypeFor[ccc.NullUUID]().String():        uuidTSType,
		reflect.TypeFor[decimal.Decimal]().String():     numberTSType,
		reflect.TypeFor[decimal.NullDecimal]().String(): numberTSType,
		reflect.TypeFor[time.Time]().String():           dateTSType,
		reflect.TypeFor[civil.Date]().String():          civilDateTSType,
		reflect.TypeFor[spanner.NullString]().String():  stringTSType,
		reflect.TypeFor[spanner.NullInt64]().String():   numberTSType,
		reflect.TypeFor[spanner.NullFloat32]().String(): numberTSType,
		reflect.TypeFor[spanner.NullFloat64]().String(): numberTSType,
		reflect.TypeFor[spanner.NullNumeric]().String(): numberTSType,
		reflect.TypeFor[spanner.NullBool]().String():    booleanStr,
		reflect.TypeFor[spanner.NullTime]().String():    dateTSType,
		reflect.TypeFor[spanner.NullDate]().String():    civilDateTSType,
		reflect.TypeFor[spanner.NullJSON]().String():    unknownTSType,
		// json.RawMessage under both of its names: the named type before Go 1.27, and
		// jsontext.Value, which it is an alias of from Go 1.27 on and which aliases read
		// through resolve to (reflect spells it so too).
		jsonRawMessageName:                          unknownTSType,
		jsontextValueName:                           unknownTSType,
		reflect.TypeFor[securehash.Hash]().String(): stringTSType,
		boolGoType:                                  booleanStr,
		stringGoType:                                stringTSType,
		intGoType:                                   numberTSType,
		int8GoType:                                  numberTSType,
		int16GoType:                                 numberTSType,
		int32GoType:                                 numberTSType,
		int64GoType:                                 numberTSType,
		uintGoType:                                  numberTSType,
		uint8GoType:                                 numberTSType,
		uint16GoType:                                numberTSType,
		uint32GoType:                                numberTSType,
		uint64GoType:                                numberTSType,
		uintptrGoType:                               numberTSType,
		float32GoType:                               numberTSType,
		float64GoType:                               numberTSType,
		complex64GoType:                             numberTSType,
		complex128GoType:                            numberTSType,
	}
}
