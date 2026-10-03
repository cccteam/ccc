package generation

import (
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-playground/errors/v5"
	"golang.org/x/mod/semver"
)

// Names the flavor table and the generated code share.
const (
	userSessionSuffix    = "/user/session"
	passwordHandlersType = "session.PasswordAuthHandlers"
	azureStubType        = "routerOIDCAzureStub"
	bindAuthMethod       = "BindAuth"
)

// flavorRoute is one route a session flavor mounts under its outlet's prefix.
type flavorRoute struct {
	// Method is the HTTP method name (http.MethodPost).
	Method string
	// Suffix is the path under the outlet prefix.
	Suffix string
	// Handler is the session handler method serving it.
	Handler string
}

// authFlavorSpec is what the generated router knows about one auth flavor: the session
// library's handler interface, how the chain comment names it, the routes it mounts, and
// the recording stub the generated test builds for it.
type authFlavorSpec struct {
	handlers string
	label    string
	stub     string
	routes   []flavorRoute
}

// oidcRoutes are the routes every directory flavor mounts: the login redirect, the
// directory's callback, and the session routes.
var oidcRoutes = []flavorRoute{
	{Method: http.MethodGet, Suffix: "/user/login", Handler: "Login"},
	{Method: http.MethodGet, Suffix: "/user/callback", Handler: "CallbackOIDC"},
	{Method: http.MethodGet, Suffix: userSessionSuffix, Handler: "Authenticated"},
	{Method: http.MethodDelete, Suffix: userSessionSuffix, Handler: "Logout"},
}

// authFlavors is the flavor table, keyed by the option constants.
var authFlavors = map[AuthFlavor]authFlavorSpec{
	Password: {
		handlers: passwordHandlersType,
		label:    "password sessions",
		stub:     "routerPasswordStub",
		routes: []flavorRoute{
			{Method: http.MethodPost, Suffix: "/user/login", Handler: "Login"},
			{Method: http.MethodGet, Suffix: userSessionSuffix, Handler: "Authenticated"},
			{Method: http.MethodDelete, Suffix: userSessionSuffix, Handler: "Logout"},
		},
	},
	OIDCGoogle: {
		handlers: "session.OIDCGoogleHandlers",
		label:    "Google directory sessions",
		stub:     "routerOIDCGoogleStub",
		routes:   oidcRoutes,
	},
	OIDCAzure: {
		handlers: "session.OIDCAzureHandlers",
		label:    "Azure directory sessions",
		stub:     azureStubType,
		routes: append(slices.Clone(oidcRoutes),
			flavorRoute{Method: http.MethodGet, Suffix: "/user/logout", Handler: "FrontChannelLogout"},
		),
	},
}

// Hook fields the generated Hooks struct carries beside the per-outlet fields; an outlet
// whose Pascal-cased name would take one of them cannot be declared.
var reservedHookFields = []string{"Outermost", "Root"}

// validateRouterConfig checks the outlet declarations against GenerateRouter: without
// the option the router-describing outlet options are refused (they would be silently
// ignored), and with it every outlet says how it authenticates, the contradictions are
// rejected, and the browser applications' mount paths are distinct, outside every
// outlet's prefix, and at / only when there is one browser application.
func (r *resourceGenerator) validateRouterConfig() error {
	outlets := r.allOutlets()
	if !r.genRouter {
		for _, o := range outlets {
			var declared string
			switch {
			case o.auth != nil:
				declared = "Auth"
			case o.apiKey:
				declared = "APIKey"
			case o.webApp != "":
				declared = "WebApp"
			case o.declaredOldest:
				declared = "OldestAnswered"
			default:
				continue
			}

			return errors.Newf("outlet %q declares %s, which describes the generated router: declare GenerateRouter, or drop it and compose the outlet in a hand-written router (ServesSessions marks a session outlet there)", o.name, declared)
		}

		return nil
	}

	for _, o := range outlets {
		switch {
		case o.auth != nil && o.apiKey:
			return errors.Newf("outlet %q declares both Auth and APIKey: an outlet is a browser surface behind one auth or a machine surface behind an API key", o.name)
		case o.auth == nil && !o.apiKey:
			return errors.Newf("outlet %q declares neither Auth nor APIKey: under GenerateRouter every outlet says how it authenticates", o.name)
		case o.apiKey && o.declaredSessions:
			return errors.Newf("outlet %q declares APIKey and ServesSessions: an API-key outlet serves no browser sessions", o.name)
		case o.apiKey && o.webApp != "":
			return errors.Newf("outlet %q declares APIKey and WebApp(%q): a machine outlet serves no browser application", o.name, o.webApp)
		case o.apiKey && o.declaredOldest:
			return errors.Newf("outlet %q declares APIKey and OldestAnswered(%q): a machine outlet's clients carry no release, so nothing is checked against one; the option belongs on a session outlet", o.name, o.oldestAnswered)
		}
		if field := caser.ToPascal(o.name); slices.Contains(reservedHookFields, field) {
			return errors.Newf("outlet %q would take the Hooks field %s, which the generated router reserves; choose another name", o.name, field)
		}
		if o.apiKey && caser.ToPascal(o.name)+"Auth" == bindAuthMethod {
			return errors.Newf("outlet %q would take the Handlers method BindAuth, which the generated router reserves; choose another name", o.name)
		}
	}

	return validateWebAppMounts(outlets)
}

// validateWebAppMounts checks the browser applications' mount paths: every application
// has its own, none sits under an outlet's route prefix, and one at / is the only one.
// An installed browser application's scope is every URL under its start, so with two
// applications one at / would own the origin: the one under a prefix would never get its
// own install prompt, and its notifications and links would be attributed to the
// application at /. With several applications none is mounted at /, and the generated
// router redirects the root to the default outlet's.
func validateWebAppMounts(outlets []routerOutlet) error {
	mounts := make(map[string]string, len(outlets))
	for _, o := range outlets {
		if o.webApp == "" {
			continue
		}
		if prior, ok := mounts[o.webApp]; ok {
			return errors.Newf("outlet %q declares WebApp(%q), which outlet %q already serves: every browser application has its own mount path", o.name, o.webApp, prior)
		}
		mounts[o.webApp] = o.name
		for _, other := range outlets {
			prefixPath := "/" + other.prefix
			if o.webApp == prefixPath || strings.HasPrefix(o.webApp, prefixPath+"/") {
				return errors.Newf("outlet %q declares WebApp(%q), which sits under outlet %q's route prefix /%s: a browser application is mounted beside the API prefixes, never under one", o.name, o.webApp, other.name, other.prefix)
			}
		}
	}

	root, ok := mounts["/"]
	if !ok || len(mounts) == 1 {
		return nil
	}
	for _, o := range outlets {
		if o.webApp == "" || o.webApp == "/" {
			continue
		}

		return errors.Newf(`outlet %q declares WebApp("/") beside outlet %q's WebApp(%q): an installed browser application's scope is every URL under its start, so the application at / owns the origin, the one under %s never gets its own install prompt, and its notifications and links are attributed to the application at /; with two browser applications none is mounted at /, so mount the %s outlet's application under a path such as /console`, root, o.name, o.webApp, o.webApp, root)
	}

	return nil
}

// servedRouterData feeds the served-router templates: the outlets in declaration order
// with their chains, the browser applications longest mount first, and the isolation
// cases the generated test re-proves through New.
type servedRouterData struct {
	Source  string
	Package string
	// AuthImports are the auth packages the file imports for BindAuth(<pkg>.Name),
	// sorted; empty unless more than one session auth is declared.
	AuthImports []string
	// MultiAuth emits BindAuth: more than one distinct session auth is declared.
	MultiAuth bool
	// Outlets is every outlet, the default first.
	Outlets []*servedOutlet
	// ExtraOutlets are the WithRouterOutlet outlets, whose Generated<Suffix>Handlers
	// the Handlers interface embeds beside GeneratedHandlers.
	ExtraOutlets   []*servedOutlet
	SessionOutlets []*servedOutlet
	APIKeyOutlets  []*servedOutlet
	// WebApps are the browser applications, longest mount path first so "/" is last;
	// HasRootWebApp reports one mounted at "/", the catch-all.
	WebApps       []*servedWebApp
	HasRootWebApp bool
	// RootRedirect is where GET / alone redirects when browser applications are served
	// and none is mounted at /: the default outlet's mount path with a trailing slash,
	// or the first declared outlet's that serves one when the default serves none.
	// RootRedirectOutlet names that outlet. Both are empty with a root web app or no
	// web app.
	RootRedirect       string
	RootRedirectOutlet string
	// NotFoundPrefixes are the outlet prefixes as mounted paths ("/api/"), each given
	// a not-found handler so an unknown API path never falls to a browser application.
	NotFoundPrefixes []string
	// Flavors are the distinct session flavors in use, for the test's recording stubs.
	Flavors []*servedFlavor
	// HandlersSummary lists what the Handlers interface carries, for its doc comment.
	HandlersSummary string
	// NegativeRouterTests are the outlet-isolation cases the routes test proves over
	// NewTestRouter, re-proven here through New.
	NegativeRouterTests []negativeRouterTest
	// FileStores are the file stores the generated code reads and writes, which New
	// requires on the resource client before the server starts; StoreImports are the
	// packages the named stores' types are declared in.
	FileStores   []servedFileStore
	StoreImports []string
}

// servedFileStore is one file store as the router requires it at start and the router
// test wires it.
type servedFileStore struct {
	// NameExpr is the store's name as the router spells it: resource.DefaultStore, or
	// resource.StoreNameFor[pkg.T]() for the named store T.
	NameExpr string
	// OptionExpr wires a stub store under that name on the router test's mock client.
	OptionExpr string
}

// servedOutlet is one outlet as the served router composes it.
type servedOutlet struct {
	Name   string
	Suffix string
	Prefix string
	// HookField is the outlet's field on Hooks: Default for the default outlet, the
	// Pascal-cased name otherwise.
	HookField string
	// RoutesFunc is the generated registration function the group mounts.
	RoutesFunc string
	// NotFoundPrefix is the outlet's prefix as a mounted path ("/api/").
	NotFoundPrefix string
	// APIKey marks a machine outlet; AuthMiddleware is then its <Outlet>Auth method.
	APIKey         bool
	AuthMiddleware string
	// The session outlet's flavor, its handler interface, how the chain comment names
	// it, and the package it binds to (empty when the auth is not imported).
	Flavor          AuthFlavor
	FlavorLabel     string
	SessionHandlers string
	AuthPackage     string
	// Getter returns the outlet's session handlers on Handlers for an additional
	// session outlet; the default outlet's are embedded. Receiver is the expression the
	// router reads the session handlers from: h for the default outlet, a local
	// variable for a getter outlet.
	Getter   string
	Receiver string
	// StubType is the recording stub the generated test builds for the flavor, StubField
	// its field on the test's Handlers stub (empty for the embedded default), and
	// StubPrefix the name prefix it records under.
	StubType   string
	StubField  string
	StubPrefix string
	// Routes are the flavor's routes under the prefix.
	Routes []servedRoute
	WebApp *servedWebApp
	// The session outlet's version check. OldestAnswered is the declaration as written
	// ("" for none, ThisRelease, or a release); OldestAnsweredExpr the Go expression
	// the router passes, empty for none; OldestAnsweredNote the chain comment's
	// parenthetical; AnswersSentence the check comment's clause. FileRoutes are the
	// outlet's stored-file routes, answered at any release, and Probes the releases
	// the generated test sends through the check.
	OldestAnswered     string
	OldestAnsweredExpr string
	OldestAnsweredNote string
	AnswersSentence    string
	FileRoutes         []servedFileRoute
	Probes             versionProbes
}

// servedFileRoute is one stored-file route of a session outlet: the pattern the check
// exempts, and the URL and handler the generated test drives it with.
type servedFileRoute struct {
	Pattern string
	TestURL string
	Handler string
}

// versionProbes are the releases the generated test sends through one session outlet's
// check: Server is the release the stub reports; InRange the oldest answered release,
// or the first release where every release is answered; Below a release under the
// oldest answered, empty where every release is answered; Above a release past the
// server's.
type versionProbes struct {
	Server  string
	InRange string
	Below   string
	Above   string
}

// The probes where no release is written: the server the generated test reports when
// every release or the server's own is answered, the release past it, the first
// release, the release before the server's, and the release below any oldest answered.
const (
	probeServer          = "2.0.0"
	probeAboveServer     = "2.0.1"
	probeFirstRelease    = "0.0.1"
	probeBeforeServer    = "1.9.9"
	probeBelowAnyRelease = "0.0.0"
)

// versionProbesFor derives the probes from an outlet's oldest answered declaration.
func versionProbesFor(oldest string) versionProbes {
	switch oldest {
	case "":
		return versionProbes{Server: probeServer, InRange: probeFirstRelease, Above: probeAboveServer}
	case ThisRelease:
		return versionProbes{Server: probeServer, InRange: probeServer, Below: probeBeforeServer, Above: probeAboveServer}
	}

	canonical := oldest
	if !strings.HasPrefix(canonical, "v") {
		canonical = "v" + canonical
	}
	canonical = semver.Canonical(canonical)
	var major, minor int
	if _, err := fmt.Sscanf(semver.MajorMinor(canonical), "v%d.%d", &major, &minor); err != nil {
		panic(fmt.Sprintf("versionProbesFor(%q): %v; the option accepted no release", oldest, err))
	}
	probes := versionProbes{
		Server:  fmt.Sprintf("%d.%d.0", major, minor+1),
		InRange: oldest,
		Above:   fmt.Sprintf("%d.%d.1", major, minor+1),
	}
	if semver.Compare(canonical, "v"+probeBelowAnyRelease) > 0 {
		probes.Below = probeBelowAnyRelease
	}

	return probes
}

// versionCheckOf renders the outlet's declaration for the router: the expression the
// check receives, the chain comment's parenthetical, and the check comment's clause.
func versionCheckOf(oldest string) (expr, note, sentence string) {
	switch oldest {
	case "":
		return "", "", "every release up to the server's own is answered"
	case ThisRelease:
		return "resource.ThisRelease", " (oldest answered this release)", "only the server's own release is answered"
	default:
		return strconv.Quote(oldest), " (oldest answered " + oldest + ")", "releases from " + oldest + " up to the server's own are answered"
	}
}

// servedFileRoutesOf renders an outlet's stored-file routes for the check and the
// test, in path order.
func servedFileRoutesOf(routes []*generatedRoute) []servedFileRoute {
	files := make([]servedFileRoute, 0, len(routes))
	for _, route := range routes {
		files = append(files, servedFileRoute{Pattern: route.Path, TestURL: route.TestURL, Handler: route.HandlerFunc})
	}
	slices.SortFunc(files, func(a, b servedFileRoute) int {
		return strings.Compare(a.Pattern, b.Pattern)
	})

	return files
}

// servedRoute is one session route as mounted.
type servedRoute struct {
	// Method is the HTTP method name; MethodConst its net/http constant expression.
	Method      string
	MethodConst string
	// Path is the mounted path, Suffix the part under the outlet prefix.
	Path    string
	Suffix  string
	Handler string
}

// servedWebApp is one browser application the router serves.
type servedWebApp struct {
	Outlet   string
	Mount    string
	DeepLink string
	Assets   string
}

// servedFlavor is one session flavor in use: the recording stub the generated test
// declares for it, the interface it embeds, and the handlers it overrides.
type servedFlavor struct {
	Flavor   AuthFlavor
	StubType string
	Handlers string
	Routes   []flavorRoute
}

// runServedRouterGeneration renders the served router and its test (GenerateRouter)
// beside the route tables, then the release file naming each outlet's oldest answered
// release. fileRoutes are each outlet's stored-file routes by outlet name, which the
// session outlets' version checks exempt.
func (r *resourceGenerator) runServedRouterGeneration(outlets []routerOutlet, negativeTests []negativeRouterTest, fileRoutes map[string][]*generatedRoute) error {
	begin := time.Now()
	data := r.servedRouterData(outlets, negativeTests, fileRoutes)

	destination := filepath.Join(r.router.Dir(), generatedGoFileName(servedRouterOutputName))
	if err := r.writeFormattedGoFile(destination, "servedRouterTemplate", servedRouterTemplate, data); err != nil {
		return errors.Wrap(err, "writeFormattedGoFile()")
	}
	log.Printf("Generated router file in %s: %s\n", time.Since(begin), destination)

	begin = time.Now()
	testDestination := filepath.Join(r.router.Dir(), generatedGoFileName(servedRouterTestOutputName))
	if err := r.writeFormattedGoFile(testDestination, "servedRouterTestTemplate", servedRouterTestTemplate, data); err != nil {
		return errors.Wrap(err, "writeFormattedGoFile()")
	}
	log.Printf("Generated router test file in %s: %s\n", time.Since(begin), testDestination)

	return r.runReleaseFileGeneration(outlets)
}

// servedRouterData builds the template payload from the validated outlet declarations
// and each outlet's stored-file routes by outlet name.
func (r *resourceGenerator) servedRouterData(outlets []routerOutlet, negativeTests []negativeRouterTest, fileRoutes map[string][]*generatedRoute) *servedRouterData {
	authPaths := make(map[string]struct{})
	for _, o := range outlets {
		if o.auth != nil {
			authPaths[o.auth.importPath] = struct{}{}
		}
	}
	multiAuth := len(authPaths) > 1

	data := &servedRouterData{
		Source:              r.resource.Dir(),
		Package:             r.router.Package(),
		MultiAuth:           multiAuth,
		NegativeRouterTests: negativeTests,
	}
	data.FileStores, data.StoreImports = r.servedFileStores()
	if multiAuth {
		for path := range authPaths {
			data.AuthImports = append(data.AuthImports, path)
		}
		sort.Strings(data.AuthImports)
	}

	flavors := make(map[AuthFlavor]*servedFlavor)
	for i := range outlets {
		o := &outlets[i]
		so := servedOutletOf(o, multiAuth)
		if so.WebApp != nil {
			data.WebApps = append(data.WebApps, so.WebApp)
		}
		switch {
		case so.APIKey:
			data.APIKeyOutlets = append(data.APIKeyOutlets, so)
		case o.auth != nil:
			if _, ok := flavors[o.auth.flavor]; !ok {
				spec := authFlavors[o.auth.flavor]
				flavors[o.auth.flavor] = &servedFlavor{Flavor: o.auth.flavor, StubType: spec.stub, Handlers: spec.handlers, Routes: spec.routes}
			}
			so.FileRoutes = servedFileRoutesOf(fileRoutes[o.name])
			data.SessionOutlets = append(data.SessionOutlets, so)
		}
		data.Outlets = append(data.Outlets, so)
		data.NotFoundPrefixes = append(data.NotFoundPrefixes, so.NotFoundPrefix)
	}
	data.ExtraOutlets = data.Outlets[1:]

	// Longer mount paths first, so "/" is the catch-all; the paths are distinct.
	sort.Slice(data.WebApps, func(i, j int) bool {
		return len(data.WebApps[i].Mount) > len(data.WebApps[j].Mount)
	})
	data.HasRootWebApp = slices.ContainsFunc(data.WebApps, func(w *servedWebApp) bool { return w.Mount == "/" })
	data.RootRedirect, data.RootRedirectOutlet = rootRedirectOf(data)
	for _, flavor := range []AuthFlavor{Password, OIDCGoogle, OIDCAzure} {
		if f, ok := flavors[flavor]; ok {
			data.Flavors = append(data.Flavors, f)
		}
	}
	data.HandlersSummary = handlersSummary(data)

	return data
}

// rootRedirectOf names where the root redirects and whose application it is: with
// browser applications served and none at /, the default outlet's mount path with a
// trailing slash, or the first declared outlet's that serves one. Empty otherwise.
func rootRedirectOf(data *servedRouterData) (target, outlet string) {
	if len(data.WebApps) == 0 || data.HasRootWebApp {
		return "", ""
	}
	for _, o := range data.Outlets {
		if o.WebApp != nil {
			return o.WebApp.Mount + "/", o.Name
		}
	}

	return "", ""
}

// servedOutletOf builds one outlet's payload from its declaration: its names, its
// browser application, and either its API-key middleware or its session flavor with the
// flavor's routes under the prefix.
func servedOutletOf(o *routerOutlet, multiAuth bool) *servedOutlet {
	so := &servedOutlet{
		Name:           o.name,
		Suffix:         o.suffix(),
		Prefix:         o.prefix,
		HookField:      caser.ToPascal(o.name),
		RoutesFunc:     fmt.Sprintf("generated%sRoutes", o.suffix()),
		NotFoundPrefix: "/" + o.prefix + "/",
	}
	if o.webApp != "" {
		so.WebApp = &servedWebApp{
			Outlet:   o.name,
			Mount:    o.webApp,
			DeepLink: o.suffix() + "DeepLink",
			Assets:   o.suffix() + "Assets",
		}
	}
	switch {
	case o.apiKey:
		so.APIKey = true
		so.AuthMiddleware = so.HookField + "Auth"
	case o.auth != nil:
		spec := authFlavors[o.auth.flavor]
		so.Flavor = o.auth.flavor
		so.FlavorLabel = spec.label
		so.SessionHandlers = spec.handlers
		so.StubType = spec.stub
		if multiAuth {
			so.AuthPackage = o.auth.packageName()
		}
		so.Receiver = "h"
		if o.name != defaultOutletName {
			so.Getter = so.HookField
			so.Receiver = caser.ToCamel(o.name) + "Session"
			so.StubField = caser.ToCamel(o.name)
			so.StubPrefix = so.HookField
		}
		so.OldestAnswered = o.oldestAnswered
		so.OldestAnsweredExpr, so.OldestAnsweredNote, so.AnswersSentence = versionCheckOf(o.oldestAnswered)
		so.Probes = versionProbesFor(o.oldestAnswered)
		so.Routes = make([]servedRoute, 0, len(spec.routes))
		for _, route := range spec.routes {
			so.Routes = append(so.Routes, servedRoute{
				Method:      route.Method,
				MethodConst: httpMethodConstant(route.Method),
				Path:        "/" + o.prefix + route.Suffix,
				Suffix:      route.Suffix,
				Handler:     route.Handler,
			})
		}
	}

	return so
}

// handlersSummary lists what the Handlers interface carries, for its doc comment.
func handlersSummary(data *servedRouterData) string {
	parts := []string{"every outlet's generated handlers"}
	if len(data.SessionOutlets) > 0 {
		parts = append(parts, "each session outlet's session handlers")
	}
	parts = append(parts, "the middleware every request and every outlet passes")
	if len(data.APIKeyOutlets) > 0 {
		parts = append(parts, "the API-key outlets' authentication")
	}
	if len(data.WebApps) > 0 {
		parts = append(parts, "the browser applications' handlers")
	}

	return strings.Join(parts[:len(parts)-1], ", ") + ", and " + parts[len(parts)-1]
}

// The served-router templates. servedRouterTemplate renders zz_gen_router.go: the chain
// comment as the package documentation, the Handlers interface, the Hooks struct, and
// New, linear and inline. servedRouterTestTemplate renders zz_gen_router_test.go, which
// proves the chain comment by driving every route through New with recording stubs.
var (
	servedRouterTemplate = `// Code generated by resourcegeneration. DO NOT EDIT.
// Source: {{ .Source }}

// Package {{ .Package }} serves the application. The middleware in front of every route,
// outermost first, one line per group, each chain followed by what it stands in front of:
//
//	every request: hooks.Outermost, LoggerMiddleware, SecurityHeaders, httpio.WithParams
{{- range .Outlets }}
{{- if .APIKey }}
//	{{ .Name }} (/{{ .Prefix }}), API key:
//	  NoCaching, CompressionMiddleware, {{ .AuthMiddleware }}: hooks.{{ .HookField }}, {{ .RoutesFunc }}
{{- else }}
//	{{ .Name }} (/{{ .Prefix }}), {{ .FlavorLabel }}{{ if .AuthPackage }} of the {{ .AuthPackage }} auth{{ end }}:
//	  {{ if .AuthPackage }}BindAuth({{ .AuthPackage }}.Name), {{ end }}NoCaching, CompressionMiddleware, StartSession, SetXSRFToken: {{ range $i, $route := .Routes }}{{ if $i }}, {{ end }}{{ $route.Method }} {{ $route.Path }}{{ end }}
//	  + ValidateSession, ValidateXSRFToken, CheckAPIVersion{{ .OldestAnsweredNote }}: hooks.{{ .HookField }}, {{ .RoutesFunc }}
{{- end }}
{{- end }}
//
// hooks.Root's routes sit behind the every-request chain alone. Under an outlet's prefix
// nothing else answers: an unknown path is 404.{{ if .WebApps }} Outside every prefix the browser
// applications answer, longer mount paths first:{{ range $i, $w := .WebApps }}{{ if $i }},{{ end }} {{ $w.Mount }} ({{ $w.DeepLink }}, {{ $w.Assets }}){{ end }}.{{ end }}
{{- if .RootRedirect }}
// None is mounted at /: the root alone redirects to {{ .RootRedirect }}, the {{ .RootRedirectOutlet }} outlet's application.
{{- end }}
package {{ .Package }}

import (
	"fmt"
	"net/http"

{{- range .AuthImports }}
	"{{ . }}"
{{- end }}
	"github.com/cccteam/ccc/resource"
{{- range .StoreImports }}
	"{{ . }}"
{{- end }}
	"github.com/cccteam/httpio"
	"github.com/cccteam/session"
	"github.com/go-chi/chi/v5"
)

// Handlers is the full surface New composes: {{ .HandlersSummary }}.
type Handlers interface {
	GeneratedHandlers
{{- range .ExtraOutlets }}
	Generated{{ .Suffix }}Handlers
{{- end }}
{{- if .FileStores }}
	// ResourceClient is the client the generated handlers run against. The file
	// stores the generated code reads and writes are wired on it, and New refuses to
	// start without them (resource.RequireFileStores).
	ResourceClient() resource.Client
{{- end }}
{{- range .SessionOutlets }}
{{- if .Getter }}
	// {{ .Getter }} returns the {{ .Name }} outlet's session handlers{{ if .AuthPackage }}: the {{ .AuthPackage }} auth's{{ end }}.
	{{ .Getter }}() {{ .SessionHandlers }}
{{- else }}
	// The default outlet's session handlers{{ if .AuthPackage }}: the {{ .AuthPackage }} auth's{{ end }}.
	{{ .SessionHandlers }}
{{- end }}
{{- end }}
{{- if .MultiAuth }}
	// BindAuth marks a session group's requests as authenticated by the named auth, so
	// the handlers behind it check permissions and tenant visibility in that auth's store.
	BindAuth(name string) func(http.Handler) http.Handler
{{- end }}
{{- range .APIKeyOutlets }}
	// {{ .AuthMiddleware }} authenticates the {{ .Name }} outlet's machine clients, binding each
	// request to a service identity in place of a browser session.
	{{ .AuthMiddleware }}(next http.Handler) http.Handler
{{- end }}
	// ServerVersion is the release this server was built from, the configuration's
	// APP_VERSION: what each session outlet checks a browser application's
	// X-Api-Version against (resource.CheckAPIVersion). A value that is not a release,
	// dev for one, checks nothing.
	ServerVersion() string

	// Every request.
	LoggerMiddleware() func(http.Handler) http.Handler
	SecurityHeaders(next http.Handler) http.Handler

	// Every outlet.
	NoCaching(next http.Handler) http.Handler
	CompressionMiddleware() func(http.Handler) http.Handler
{{- range .WebApps }}

	// The {{ .Outlet }} outlet's browser application at {{ .Mount }}: {{ .DeepLink }} rewrites its routes to
	// the entry document, {{ .Assets }} serves the built bundle.
	{{ .DeepLink }}(next http.Handler) http.Handler
	{{ .Assets }}() http.HandlerFunc
{{- end }}
}

// Hooks are the application's additions to the generated router. They compose inward
// only: a hook adds middleware and routes under the guards it is handed and never sees
// the outer router, so no generated route can be lifted out from behind session
// validation or the XSRF guard. Every field may be nil.
type Hooks struct {
	// Outermost runs ahead of the logger on every request: tracing belongs here.
	Outermost []func(http.Handler) http.Handler
	// Root registers routes outside every outlet: health checks, webhooks, scheduler
	// triggers. They sit behind the every-request chain and nothing else.
	Root func(r chi.Router)
{{- range .Outlets }}
{{- if .APIKey }}
	// {{ .HookField }} runs inside the {{ .Name }} outlet's API-key group: r already sits behind
	// {{ .AuthMiddleware }}, and generated registers the outlet's generated routes. Nil registers
	// them directly.
{{- else }}
	// {{ .HookField }} runs inside the {{ .Name }} outlet's authenticated group: r already sits behind
	// {{ if .AuthPackage }}BindAuth({{ .AuthPackage }}.Name), {{ end }}session validation, and the XSRF guard, and generated registers
	// the outlet's generated routes. Nil registers them directly.
{{- end }}
	{{ .HookField }} func(r chi.Router, generated func(chi.Router))
{{- end }}
}

// New wires the served application: the every-request middleware, hooks.Root, one group
// per outlet with its authentication around its generated routes and the outlet's hook,
// a not-found handler per outlet prefix, and the browser applications.
func New(h Handlers, hooks Hooks) *chi.Mux {
{{- if .FileStores }}
	// Every file store the generated code reads or writes is wired on the resource
	// client, or the server does not start: an unwired store would otherwise surface
	// on the first upload, file request or releasing delete.
	if err := resource.RequireFileStores(h.ResourceClient(){{ range .FileStores }}, {{ .NameExpr }}{{ end }}); err != nil {
		panic(fmt.Sprintf("router.New: %v", err))
	}
{{- end }}
	r := chi.NewRouter()
{{- if .SessionOutlets }}
	// The release this server was built from, which every session outlet's version
	// check compares a browser application's X-Api-Version against.
	serverVersion := h.ServerVersion()
{{- end }}

	// Every request.
	r.Use(hooks.Outermost...)
	r.Use(h.LoggerMiddleware())
	r.Use(h.SecurityHeaders)
	r.Use(httpio.WithParams)

	// The application's routes outside every outlet.
	if hooks.Root != nil {
		r.Group(func(r chi.Router) {
			hooks.Root(r)
		})
	}
{{- range .Outlets }}
{{ if .APIKey }}
	// The {{ .Name }} outlet (/{{ .Prefix }}): machine clients behind an API key, so the group carries
	// no session handling and no XSRF guard.
	r.Group(func(r chi.Router) {
		r.Use(h.NoCaching)
		r.Use(h.CompressionMiddleware())
		r.Use(h.{{ .AuthMiddleware }})

		registerGenerated(r, hooks.{{ .HookField }}, "{{ .HookField }}", func(r chi.Router) {
			{{ .RoutesFunc }}(r, h)
		})
	})
{{- else }}
	// The {{ .Name }} outlet (/{{ .Prefix }}): {{ .FlavorLabel }}{{ if .AuthPackage }} of the {{ .AuthPackage }} auth{{ end }}.
{{- if .Getter }}
	{{ .Receiver }} := h.{{ .Getter }}()
{{- end }}
	r.Group(func(r chi.Router) {
{{- if .AuthPackage }}
		r.Use(h.BindAuth({{ .AuthPackage }}.Name))
{{- end }}
		r.Use(h.NoCaching)
		r.Use(h.CompressionMiddleware())
		r.Use({{ .Receiver }}.StartSession)
		r.Use({{ .Receiver }}.SetXSRFToken)
{{ $outlet := . }}
{{- range .Routes }}
		r.{{ Pascal .Method }}("{{ .Path }}", {{ $outlet.Receiver }}.{{ .Handler }}())
{{- end }}

		r.Group(func(r chi.Router) {
			r.Use({{ .Receiver }}.ValidateSession)
			r.Use({{ .Receiver }}.ValidateXSRFToken)
			// The version check: a browser application sends its release in X-Api-Version,
			// and {{ .AnswersSentence }}. An application outside that
			// range is refused with 412 naming the server's release, before its body is read;
			// a request without the header, the session routes above{{ if .FileRoutes }} and the stored-file
			// routes{{ end }} are answered at any release.
			r.Use(resource.CheckAPIVersion(resource.APIVersionCheck{
				ServerVersion: serverVersion,
{{- if .OldestAnsweredExpr }}
				OldestAnswered: {{ .OldestAnsweredExpr }},
{{- end }}
{{- if .FileRoutes }}
				Exempt: []string{
{{- range .FileRoutes }}
					"{{ .Pattern }}",
{{- end }}
				},
{{- end }}
			}))

			registerGenerated(r, hooks.{{ .HookField }}, "{{ .HookField }}", func(r chi.Router) {
				{{ .RoutesFunc }}(r, h)
			})
		})
	})
{{- end }}
{{- end }}

	// Under an outlet's prefix nothing else answers: an unknown API path is 404, never a
	// browser application's entry document.
	for _, prefix := range []string{ {{- range $i, $p := .NotFoundPrefixes }}{{ if $i }}, {{ end }}"{{ $p }}"{{ end -}} } {
		r.Route(prefix, func(r chi.Router) {
			r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "Not Found", http.StatusNotFound)
			})
		})
	}
{{- range .WebApps }}

	// The {{ .Outlet }} outlet's browser application at {{ .Mount }}{{ if eq .Mount "/" }}, the catch-all{{ end }}.
	r.Route("{{ .Mount }}", func(r chi.Router) {
		r.Use(h.{{ .DeepLink }})

		r.Get("/*", h.{{ .Assets }}())
	})
{{- end }}
{{- if .RootRedirect }}

	// No browser application is mounted at /: an installed application's scope is every
	// URL under its start, so one at / would own the origin and the others would never
	// install on their own. The root alone sends the browser to the {{ .RootRedirectOutlet }} outlet's
	// application; every other unmatched path is 404.
	r.Get("/", func(w http.ResponseWriter, req *http.Request) {
		http.Redirect(w, req, "{{ .RootRedirect }}", http.StatusTemporaryRedirect)
	})
{{- end }}

	return r
}

// registerGenerated registers an outlet's generated routes inside its guarded group:
// through the outlet's hook when the application supplies one, which must call generated
// exactly once, or directly.
func registerGenerated(r chi.Router, hook func(chi.Router, func(chi.Router)), name string, generated func(chi.Router)) {
	if hook == nil {
		generated(r)

		return
	}

	calls := 0
	hook(r, func(r chi.Router) {
		calls++
		generated(r)
	})
	if calls != 1 {
		panic(fmt.Sprintf("router.New: hooks.%s must call generated exactly once to register the outlet's generated routes, called it %d times", name, calls))
	}
}
`

	servedRouterTestTemplate = `// Code generated by resourcegeneration. DO NOT EDIT.
// Source: {{ .Source }}

package {{ .Package }}

import (
{{- if .FileStores }}
	"context"
{{- end }}
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

{{- range .AuthImports }}
	"{{ . }}"
{{- end }}
	"github.com/cccteam/ccc/resource"
{{- range .StoreImports }}
	"{{ . }}"
{{- end }}
	"github.com/cccteam/session"
	"github.com/go-chi/chi/v5"
)

// routerRootChain is the middleware every request passes, in order.
var routerRootChain = []string{"LoggerMiddleware", "SecurityHeaders"}

// routerOutletChain is one outlet's middleware in order: group runs in front of the
// outlet's session routes, guards additionally in front of its generated routes.
type routerOutletChain struct {
	group  []string
	guards []string
}

// routerOutletChains keys each outlet's chain by the prefix its routes sit under.
var routerOutletChains = map[string]routerOutletChain{
{{- range .Outlets }}
{{- if .APIKey }}
	"{{ .NotFoundPrefix }}": {
		group: []string{"NoCaching", "CompressionMiddleware", "{{ .AuthMiddleware }}"},
	},
{{- else }}
	"{{ .NotFoundPrefix }}": {
		group:  []string{ {{- if .AuthPackage }}"BindAuth(" + {{ .AuthPackage }}.Name + ")", {{ end }}"NoCaching", "CompressionMiddleware", "{{ .StubPrefix }}StartSession", "{{ .StubPrefix }}SetXSRFToken"},
		guards: []string{"{{ .StubPrefix }}ValidateSession", "{{ .StubPrefix }}ValidateXSRFToken"},
	},
{{- end }}
{{- end }}
}

// routerChainFor returns the chain of the outlet whose prefix the URL sits under.
func routerChainFor(t *testing.T, url string) routerOutletChain {
	t.Helper()

	for prefix, chain := range routerOutletChains {
		if strings.HasPrefix(url, prefix) {
			return chain
		}
	}
	t.Fatalf("%s sits under no outlet's prefix", url)

	return routerOutletChain{}
}

// routerSessionRoute is one session route: the outlet's login, callback, or session
// handler, which answers behind the outlet's group and before its guards. suffix is the
// path under the outlet's prefix.
type routerSessionRoute struct {
	url     string
	suffix  string
	method  string
	handler string
}

// routerSessionRoutes lists every session outlet's routes.
func routerSessionRoutes() []routerSessionRoute {
	return []routerSessionRoute{
{{- range .SessionOutlets }}
{{- $outlet := . }}
{{- range .Routes }}
		{url: "{{ .Path }}", suffix: "{{ .Suffix }}", method: {{ .MethodConst }}, handler: "{{ $outlet.StubPrefix }}{{ .Handler }}"},
{{- end }}
{{- end }}
	}
}

// serveGeneratedRouter builds the router over recording stubs with the given hooks and
// serves one request through it.
func serveGeneratedRouter(t *testing.T, rec *routerCallRecorder, hooks Hooks, method, url string) *httptest.ResponseRecorder {
	t.Helper()

	return serveGeneratedRouterRequest(t, rec, "", hooks, httptest.NewRequestWithContext(t.Context(), method, url, http.NoBody))
}

// serveGeneratedRouterRequest builds the router over recording stubs whose server
// reports serverVersion, with the given hooks, and serves req through it.
func serveGeneratedRouterRequest(t *testing.T, rec *routerCallRecorder, serverVersion string, hooks Hooks, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()

	stub := newRouterHandlersStub(rec)
	stub.serverVersion = serverVersion
	router := New(stub, hooks)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	return rr
}

// TestGeneratedRouter drives every generated route through New: each request answers
// 200 from exactly its own handler with its route parameters resolved, having passed the
// every-request chain, its outlet's group, and its outlet's guards, in that order and
// nothing else.
func TestGeneratedRouter(t *testing.T) {
	t.Parallel()

	for _, tt := range generatedRouterTests() {
		t.Run(tt.method+"-url"+strings.ReplaceAll(tt.url, "/", "-"), func(t *testing.T) {
			t.Parallel()

			rec := newRouterCallRecorder()
			rr := serveGeneratedRouter(t, rec, Hooks{}, tt.method, tt.url)

			if got := rr.Code; got != http.StatusOK {
				t.Errorf("response.Code = %v, want %v", got, http.StatusOK)
			}
			if cnt := len(rec.handlers); cnt != 1 {
				t.Fatalf("expected 1 handler called, got: %v", rec.handlers)
			}
			if cnt := rec.handlers[tt.handlerFunc]; cnt != 1 {
				t.Fatalf("handler %s, expected 1 call, got: %d", tt.handlerFunc, cnt)
			}
			for key, value := range tt.parameters {
				if got := rec.Parameter(tt.handlerFunc, key); got != value {
					t.Fatalf("%s = %s, expected %s", key, got, value)
				}
			}

			chain := routerChainFor(t, tt.url)
			want := slices.Concat(routerRootChain, chain.group, chain.guards)
			if !slices.Equal(rec.chain, want) {
				t.Errorf("middleware chain = %v, want %v", rec.chain, want)
			}
		})
	}
}

{{ if .SessionOutlets -}}
// TestGeneratedRouterSessionRoutes proves each session outlet's login, callback, and
// session routes answer behind the outlet's group and before its guards: a browser
// reaches them without a validated session.
func TestGeneratedRouterSessionRoutes(t *testing.T) {
	t.Parallel()

	for _, tt := range routerSessionRoutes() {
		t.Run(tt.method+"-url"+strings.ReplaceAll(tt.url, "/", "-"), func(t *testing.T) {
			t.Parallel()

			rec := newRouterCallRecorder()
			rr := serveGeneratedRouter(t, rec, Hooks{}, tt.method, tt.url)

			if got := rr.Code; got != http.StatusOK {
				t.Errorf("response.Code = %v, want %v", got, http.StatusOK)
			}
			if cnt := len(rec.handlers); cnt != 1 {
				t.Fatalf("expected 1 handler called, got: %v", rec.handlers)
			}
			if cnt := rec.handlers[tt.handler]; cnt != 1 {
				t.Fatalf("handler %s, expected 1 call, got: %d", tt.handler, cnt)
			}

			want := slices.Concat(routerRootChain, routerChainFor(t, tt.url).group)
			if !slices.Equal(rec.chain, want) {
				t.Errorf("middleware chain = %v, want %v", rec.chain, want)
			}
		})
	}
}
{{ end }}
// TestGeneratedRouterNotFound proves the prefixes are closed: under every outlet's
// prefix an unknown path is 404 from the prefix's own not-found handler, never a browser
// application's entry document, and a route of one outlet addressed under another's
// prefix reaches no handler and no group, so nothing under one prefix enters another
// outlet's chain.
func TestGeneratedRouterNotFound(t *testing.T) {
	t.Parallel()

	unknown := []string{
{{- range .NotFoundPrefixes }}
		"{{ . }}does-not-exist",
{{- end }}
	}
	for _, url := range unknown {
		t.Run("GET-url"+strings.ReplaceAll(url, "/", "-"), func(t *testing.T) {
			t.Parallel()

			rec := newRouterCallRecorder()
			rr := serveGeneratedRouter(t, rec, Hooks{}, http.MethodGet, url)

			if got := rr.Code; got != http.StatusNotFound {
				t.Errorf("response.Code = %v, want %v", got, http.StatusNotFound)
			}
			if cnt := len(rec.handlers); cnt != 0 {
				t.Fatalf("expected no handler called, got: %v", rec.handlers)
			}
			if !slices.Equal(rec.chain, routerRootChain) {
				t.Errorf("middleware chain = %v, want %v", rec.chain, routerRootChain)
			}
		})
	}

	type foreign struct {
		url    string
		method string
	}
	var foreigns []foreign
{{- if .NegativeRouterTests }}
	foreigns = append(foreigns,
{{- range .NegativeRouterTests }}
		foreign{url: "{{ .URL }}", method: {{ .Method }}},
{{- end }}
	)
{{- end }}
	// Every session route addressed under every other outlet's prefix, unless that
	// outlet mounts the same route itself.
	sessionRoutes := routerSessionRoutes()
	for _, route := range sessionRoutes {
		for prefix := range routerOutletChains {
			if strings.HasPrefix(route.url, prefix) {
				continue
			}
			candidate := foreign{url: strings.TrimSuffix(prefix, "/") + route.suffix, method: route.method}
			if slices.ContainsFunc(sessionRoutes, func(r routerSessionRoute) bool {
				return r.url == candidate.url && r.method == candidate.method
			}) {
				continue
			}
			foreigns = append(foreigns, candidate)
		}
	}
	for _, tt := range foreigns {
		t.Run(tt.method+"-url"+strings.ReplaceAll(tt.url, "/", "-"), func(t *testing.T) {
			t.Parallel()

			rec := newRouterCallRecorder()
			rr := serveGeneratedRouter(t, rec, Hooks{}, tt.method, tt.url)

			if got := rr.Code; got == http.StatusOK {
				t.Errorf("response.Code = %v, want a refusal", got)
			}
			if cnt := len(rec.handlers); cnt != 0 {
				t.Fatalf("expected no handler called, got: %v", rec.handlers)
			}
			if !slices.Equal(rec.chain, routerRootChain) {
				t.Errorf("middleware chain = %v, want %v", rec.chain, routerRootChain)
			}
		})
	}
}
{{ if .WebApps }}
// TestGeneratedRouterWebApps proves each browser application answers at its mount path
// for any path beneath it, through its deep-link rewrite and nothing else{{ if .HasRootWebApp }}, and that the
// application at / is the catch-all{{ end }}.
func TestGeneratedRouterWebApps(t *testing.T) {
	t.Parallel()

	tests := []struct {
		url      string
		handler  string
		deepLink string
	}{
{{- range .WebApps }}
		{url: "{{ if ne .Mount "/" }}{{ .Mount }}{{ end }}/generated-router-page/deep/link", handler: "{{ .Assets }}", deepLink: "{{ .DeepLink }}"},
{{- end }}
	}
	for _, tt := range tests {
		t.Run("GET-url"+strings.ReplaceAll(tt.url, "/", "-"), func(t *testing.T) {
			t.Parallel()

			rec := newRouterCallRecorder()
			rr := serveGeneratedRouter(t, rec, Hooks{}, http.MethodGet, tt.url)

			if got := rr.Code; got != http.StatusOK {
				t.Errorf("response.Code = %v, want %v", got, http.StatusOK)
			}
			if cnt := rec.handlers[tt.handler]; cnt != 1 || len(rec.handlers) != 1 {
				t.Fatalf("handler %s, expected 1 call, got: %v", tt.handler, rec.handlers)
			}

			want := slices.Concat(routerRootChain, []string{tt.deepLink})
			if !slices.Equal(rec.chain, want) {
				t.Errorf("middleware chain = %v, want %v", rec.chain, want)
			}
		})
	}
}
{{ end }}
{{- if .RootRedirect }}
// TestGeneratedRouterRoot proves that with no browser application at / the root alone
// redirects to the {{ .RootRedirectOutlet }} outlet's application, calling no handler, and that an
// unmatched path outside every prefix and every mount is 404: no application answers for
// the whole origin.
func TestGeneratedRouterRoot(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		url      string
		code     int
		location string
	}{
		{name: "the root redirects", url: "/", code: http.StatusTemporaryRedirect, location: "{{ .RootRedirect }}"},
		{name: "an unmatched path is not found", url: "/generated-router-unmatched", code: http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := newRouterCallRecorder()
			rr := serveGeneratedRouter(t, rec, Hooks{}, http.MethodGet, tt.url)

			if got := rr.Code; got != tt.code {
				t.Errorf("response.Code = %v, want %v", got, tt.code)
			}
			if got := rr.Header().Get("Location"); got != tt.location {
				t.Errorf("Location = %q, want %q", got, tt.location)
			}
			if cnt := len(rec.handlers); cnt != 0 {
				t.Fatalf("expected no handler called, got: %v", rec.handlers)
			}
			if !slices.Equal(rec.chain, routerRootChain) {
				t.Errorf("middleware chain = %v, want %v", rec.chain, routerRootChain)
			}
		})
	}
}
{{ end }}
// TestGeneratedRouterHooks proves the seams sit where the chain comment says: Outermost
// runs ahead of the logger, Root's routes sit behind the every-request chain alone, each
// outlet's hook runs inside the outlet's guards with the generated routes registering
// through it, and a hook that skips the generated registration is refused.
func TestGeneratedRouterHooks(t *testing.T) {
	t.Parallel()

	hooksFor := func(rec *routerCallRecorder) Hooks {
		return Hooks{
			Outermost: []func(http.Handler) http.Handler{rec.Middleware("Outermost")},
			Root: func(r chi.Router) {
				r.Get("/generated-router-probe", rec.RecordHandlerCall("RootProbe"))
			},
{{- range .Outlets }}
			{{ .HookField }}: func(r chi.Router, generated func(chi.Router)) {
				r.Use(rec.Middleware("{{ .HookField }}Hook"))
				r.Get("{{ .NotFoundPrefix }}generated-router-probe", rec.RecordHandlerCall("{{ .HookField }}Probe"))
				generated(r)
			},
{{- end }}
		}
	}
	outermost := slices.Concat([]string{"Outermost"}, routerRootChain)

	type hookCase struct {
		name    string
		url     string
		method  string
		handler string
		chain   []string
	}
	tests := []hookCase{
		{name: "a root route", url: "/generated-router-probe", method: http.MethodGet, handler: "RootProbe", chain: outermost},
{{- range .Outlets }}
		{
			name: "the {{ .Name }} outlet's hook", url: "{{ .NotFoundPrefix }}generated-router-probe", method: http.MethodGet, handler: "{{ .HookField }}Probe",
			chain: slices.Concat(outermost, routerOutletChains["{{ .NotFoundPrefix }}"].group, routerOutletChains["{{ .NotFoundPrefix }}"].guards, []string{"{{ .HookField }}Hook"}),
		},
{{- end }}
	}
	// Every generated route registers through its outlet's hook, so it passes the
	// hook's middleware too.
	for _, route := range generatedRouterTests() {
		for prefix, chain := range routerOutletChains {
			if !strings.HasPrefix(route.url, prefix) {
				continue
			}
			hook := prefixHookFields[prefix]
			tests = append(tests, hookCase{
				name: "a generated route registered through " + hook, url: route.url, method: route.method, handler: route.handlerFunc,
				chain: slices.Concat(outermost, chain.group, chain.guards, []string{hook + "Hook"}),
			})

			break
		}
	}
	for _, tt := range tests {
		t.Run(tt.name+" "+tt.method+"-url"+strings.ReplaceAll(tt.url, "/", "-"), func(t *testing.T) {
			t.Parallel()

			rec := newRouterCallRecorder()
			rr := serveGeneratedRouter(t, rec, hooksFor(rec), tt.method, tt.url)

			if got := rr.Code; got != http.StatusOK {
				t.Errorf("response.Code = %v, want %v", got, http.StatusOK)
			}
			if cnt := rec.handlers[tt.handler]; cnt != 1 || len(rec.handlers) != 1 {
				t.Fatalf("handler %s, expected 1 call, got: %v", tt.handler, rec.handlers)
			}
			if !slices.Equal(rec.chain, tt.chain) {
				t.Errorf("middleware chain = %v, want %v", rec.chain, tt.chain)
			}
		})
	}

	t.Run("a hook that skips the generated registration is refused", func(t *testing.T) {
		t.Parallel()

		defer func() {
			msg, _ := recover().(string)
			if !strings.Contains(msg, "hooks.{{ (index .Outlets 0).HookField }} must call generated exactly once") {
				t.Errorf("New() panic = %q, want the hook named", msg)
			}
		}()
		New(newRouterHandlersStub(newRouterCallRecorder()), Hooks{
			{{ (index .Outlets 0).HookField }}: func(chi.Router, func(chi.Router)) {},
		})
		t.Error("New() returned; want a panic")
	})
}

// prefixHookFields names each outlet's hook by the prefix its routes sit under.
var prefixHookFields = map[string]string{
{{- range .Outlets }}
	"{{ .NotFoundPrefix }}": "{{ .HookField }}",
{{- end }}
}
{{ if .SessionOutlets }}
// routerVersionProbe is one session outlet's version check as the generator declared
// it: the prefix its routes sit under, the releases the test sends through it, and the
// stored-file routes answered at any release.
type routerVersionProbe struct {
	prefix string
	// server is the release the stub reports; inRange the oldest answered release, or
	// the first release where every release is answered; below a release under the
	// oldest answered, empty where every release is answered; above a release past the
	// server's.
	server, inRange, below, above string
	files                         []routerFileRoute
}

// routerFileRoute is one stored-file route: its test URL and handler.
type routerFileRoute struct {
	url, handler string
}

// routerVersionProbes lists every session outlet's probe.
func routerVersionProbes() []routerVersionProbe {
	return []routerVersionProbe{
{{- range .SessionOutlets }}
		{
			prefix: "{{ .NotFoundPrefix }}", server: "{{ .Probes.Server }}", inRange: "{{ .Probes.InRange }}", below: "{{ .Probes.Below }}", above: "{{ .Probes.Above }}",
{{- if .FileRoutes }}
			files: []routerFileRoute{
{{- range .FileRoutes }}
				{url: "{{ .TestURL }}", handler: "{{ .Handler }}"},
{{- end }}
			},
{{- end }}
		},
{{- end }}
	}
}

// routerUnreadBody fails a case that reads it: a refused request's body is never read.
type routerUnreadBody struct {
	read bool
}

func (b *routerUnreadBody) Read([]byte) (int, error) {
	b.read = true

	return 0, io.EOF
}

// TestGeneratedRouterAPIVersion proves each session outlet's version check: a request
// whose X-Api-Version is between the outlet's oldest answered release and the server's
// is answered with Vary: X-Api-Version, one below or above is refused with 412 and the
// server's release in X-Api-Version before any handler runs or its body is read, a
// request without the header or with a version that is not a release on either side
// is answered, the session routes and the stored-file routes answer at any release,
// an API-key outlet is never checked, and the check sits behind the outlet's guards
// and ahead of its hook.
func TestGeneratedRouterAPIVersion(t *testing.T) {
	t.Parallel()

	type versionCase struct {
		name    string
		server  string
		app     string
		method  string
		url     string
		handler string
		refused bool
		varied  bool
	}
	var tests []versionCase
	for _, probe := range routerVersionProbes() {
		where := strings.ReplaceAll(probe.prefix, "/", "-")
		digest := probe.prefix + "permission-digest"
		checked := func(name, server, app string) versionCase {
			return versionCase{name: where + name, server: server, app: app, method: http.MethodGet, url: digest, handler: "PermissionDigest", varied: true}
		}
		refused := func(name, app string) versionCase {
			return versionCase{name: where + name, server: probe.server, app: app, method: http.MethodGet, url: digest, refused: true}
		}
		tests = append(tests,
			checked("the oldest answered release", probe.server, probe.inRange),
			checked("the server's release", probe.server, probe.server),
			checked("no header", probe.server, ""),
			checked("an application that is not a release", probe.server, "dev"),
			checked("a server that is not a release", "dev", probe.above),
			refused("a release above the server's", probe.above),
		)
		if probe.below != "" {
			tests = append(tests, refused("a release below the oldest answered", probe.below))
		}
		for _, route := range routerSessionRoutes() {
			if strings.HasPrefix(route.url, probe.prefix) {
				tests = append(tests, versionCase{name: where + "a session route at any release " + route.method + strings.ReplaceAll(route.suffix, "/", "-"), server: probe.server, app: probe.above, method: route.method, url: route.url, handler: route.handler})
			}
		}
		for _, file := range probe.files {
			tests = append(tests, versionCase{name: where + "a stored-file route at any release " + file.handler, server: probe.server, app: probe.above, method: http.MethodGet, url: file.url, handler: file.handler})
		}
	}
{{- range .APIKeyOutlets }}
	for _, route := range generatedRouterTests() {
		if strings.HasPrefix(route.url, "{{ .NotFoundPrefix }}") {
			tests = append(tests, versionCase{name: "{{ .Name }} outlet is not checked", server: "2.0.0", app: "99.0.0", method: route.method, url: route.url, handler: route.handlerFunc})

			break
		}
	}
{{- end }}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := newRouterCallRecorder()
			body := &routerUnreadBody{}
			req := httptest.NewRequestWithContext(t.Context(), tt.method, tt.url, body)
			if tt.app != "" {
				req.Header.Set(resource.APIVersionHeader, tt.app)
			}
			rr := serveGeneratedRouterRequest(t, rec, tt.server, Hooks{}, req)

			if tt.refused {
				if got := rr.Code; got != http.StatusPreconditionFailed {
					t.Fatalf("response.Code = %v, want %v", got, http.StatusPreconditionFailed)
				}
				if got := rr.Header().Get(resource.APIVersionHeader); got != tt.server {
					t.Errorf("%s = %q, want the server's %q", resource.APIVersionHeader, got, tt.server)
				}
				if cnt := len(rec.handlers); cnt != 0 {
					t.Errorf("expected no handler called, got: %v", rec.handlers)
				}
				if body.read {
					t.Error("the refused request's body was read")
				}

				return
			}
			if got := rr.Code; got != http.StatusOK {
				t.Fatalf("response.Code = %v, want %v", got, http.StatusOK)
			}
			if cnt := rec.handlers[tt.handler]; cnt != 1 || len(rec.handlers) != 1 {
				t.Fatalf("handler %s, expected 1 call, got: %v", tt.handler, rec.handlers)
			}
			if got := slices.Contains(rr.Header().Values("Vary"), resource.APIVersionHeader); got != tt.varied {
				t.Errorf("Vary carries %s = %v, want %v", resource.APIVersionHeader, got, tt.varied)
			}
		})
	}

	// The check sits behind the outlet's guards and ahead of its hook: a refused request
	// passes the group and the guards and never reaches the hook's middleware.
	t.Run("a refused request never reaches the hook", func(t *testing.T) {
		t.Parallel()

		probe := routerVersionProbes()[0]
		rec := newRouterCallRecorder()
		hooks := Hooks{
			{{ (index .SessionOutlets 0).HookField }}: func(r chi.Router, generated func(chi.Router)) {
				r.Use(rec.Middleware("{{ (index .SessionOutlets 0).HookField }}Hook"))
				generated(r)
			},
		}
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, probe.prefix+"permission-digest", http.NoBody)
		req.Header.Set(resource.APIVersionHeader, probe.above)
		rr := serveGeneratedRouterRequest(t, rec, probe.server, hooks, req)

		if got := rr.Code; got != http.StatusPreconditionFailed {
			t.Fatalf("response.Code = %v, want %v", got, http.StatusPreconditionFailed)
		}
		chain := routerChainFor(t, probe.prefix+"permission-digest")
		if want := slices.Concat(routerRootChain, chain.group, chain.guards); !slices.Equal(rec.chain, want) {
			t.Errorf("middleware chain = %v, want %v", rec.chain, want)
		}
	})
}
{{ end }}

// routerCallRecorder records the order middleware ran in, beside the handler and
// route-parameter recording the routes test's recorder provides.
type routerCallRecorder struct {
	*generatedCallRecorder
	chain []string
}

func newRouterCallRecorder() *routerCallRecorder {
	return &routerCallRecorder{generatedCallRecorder: newGeneratedCallRecorder()}
}

// Middleware returns middleware that appends its name to the chain when it runs.
func (rec *routerCallRecorder) Middleware(name string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rec.chain = append(rec.chain, name)
			next.ServeHTTP(w, r)
		})
	}
}
{{ range .Flavors }}
// {{ .StubType }} records the {{ .Flavor }} session handlers and middleware of one outlet under
// a name prefix, so a chain says whose session a request passed. The embedded interface
// satisfies the rest of the flavor's surface, which the router never calls.
type {{ .StubType }} struct {
	{{ .Handlers }}
	rec    *routerCallRecorder
	prefix string
}
{{ $stub := .StubType }}
{{- range .Routes }}
func (s *{{ $stub }}) {{ .Handler }}() http.HandlerFunc {
	return s.rec.RecordHandlerCall(s.prefix + "{{ .Handler }}")
}
{{ end }}
func (s *{{ $stub }}) StartSession(next http.Handler) http.Handler {
	return s.rec.Middleware(s.prefix + "StartSession")(next)
}

func (s *{{ $stub }}) SetXSRFToken(next http.Handler) http.Handler {
	return s.rec.Middleware(s.prefix + "SetXSRFToken")(next)
}

func (s *{{ $stub }}) ValidateSession(next http.Handler) http.Handler {
	return s.rec.Middleware(s.prefix + "ValidateSession")(next)
}

func (s *{{ $stub }}) ValidateXSRFToken(next http.Handler) http.Handler {
	return s.rec.Middleware(s.prefix + "ValidateXSRFToken")(next)
}
{{ end }}
// routerHandlersStub satisfies Handlers for the router tests: the generated surface
// through the routes test's stub, the session surfaces through the flavor stubs, and
// every middleware and browser-application handler recording under its own name.
type routerHandlersStub struct {
	*generatedHandlersStub
{{- range .SessionOutlets }}
{{- if not .Getter }}
	*{{ .StubType }}
{{- end }}
{{- end }}
	rec *routerCallRecorder
{{- range .SessionOutlets }}
{{- if .Getter }}
	{{ .StubField }} *{{ .StubType }}
{{- end }}
{{- end }}
	// serverVersion is the release the stub reports; empty, no release, so a case that
	// sets none is never refused.
	serverVersion string
}

func (s *routerHandlersStub) ServerVersion() string {
	return s.serverVersion
}
{{- if .FileStores }}

// ResourceClient answers with a mock client carrying every file store New requires,
// each a stub: the router test never reaches a store.
func (s *routerHandlersStub) ResourceClient() resource.Client {
	return resource.NewMockClient(nil, nil, nil{{ range .FileStores }}, {{ .OptionExpr }}{{ end }})
}

// stubFileStore satisfies resource.FileStore for the router test.
type stubFileStore struct{}

func (stubFileStore) Put(context.Context, string, string, io.Reader) error {
	return nil
}

func (stubFileStore) Delete(context.Context, []string) error {
	return nil
}

func (stubFileStore) Open(context.Context, string) (*resource.Content, error) {
	return nil, nil
}
{{- end }}

func newRouterHandlersStub(rec *routerCallRecorder) *routerHandlersStub {
	return &routerHandlersStub{
		generatedHandlersStub: newGeneratedHandlersStub(rec.RecordHandlerCall),
{{- range .SessionOutlets }}
{{- if not .Getter }}
		{{ .StubType }}: &{{ .StubType }}{rec: rec},
{{- end }}
{{- end }}
		rec: rec,
{{- range .SessionOutlets }}
{{- if .Getter }}
		{{ .StubField }}: &{{ .StubType }}{rec: rec, prefix: "{{ .StubPrefix }}"},
{{- end }}
{{- end }}
	}
}
{{ range .SessionOutlets }}
{{- if .Getter }}
func (s *routerHandlersStub) {{ .Getter }}() {{ .SessionHandlers }} {
	return s.{{ .StubField }}
}
{{ end }}
{{- end }}
{{- if .MultiAuth }}
func (s *routerHandlersStub) BindAuth(name string) func(http.Handler) http.Handler {
	return s.rec.Middleware("BindAuth(" + name + ")")
}
{{ end }}
{{- range .APIKeyOutlets }}
func (s *routerHandlersStub) {{ .AuthMiddleware }}(next http.Handler) http.Handler {
	return s.rec.Middleware("{{ .AuthMiddleware }}")(next)
}
{{ end }}
func (s *routerHandlersStub) LoggerMiddleware() func(http.Handler) http.Handler {
	return s.rec.Middleware("LoggerMiddleware")
}

func (s *routerHandlersStub) SecurityHeaders(next http.Handler) http.Handler {
	return s.rec.Middleware("SecurityHeaders")(next)
}

func (s *routerHandlersStub) NoCaching(next http.Handler) http.Handler {
	return s.rec.Middleware("NoCaching")(next)
}

func (s *routerHandlersStub) CompressionMiddleware() func(http.Handler) http.Handler {
	return s.rec.Middleware("CompressionMiddleware")
}
{{ range .WebApps }}
func (s *routerHandlersStub) {{ .DeepLink }}(next http.Handler) http.Handler {
	return s.rec.Middleware("{{ .DeepLink }}")(next)
}

func (s *routerHandlersStub) {{ .Assets }}() http.HandlerFunc {
	return s.rec.RecordHandlerCall("{{ .Assets }}")
}
{{ end -}}
`
)
