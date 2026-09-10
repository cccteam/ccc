package generation

import (
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/go-playground/errors/v5"
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
// rejected, and the browser applications' mount paths are distinct and outside every
// outlet's prefix.
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
			default:
				continue
			}

			return errors.Newf("outlet %q declares %s, which describes the generated router: declare GenerateRouter, or drop it and compose the outlet in a hand-written router (ServesSessions marks a session outlet there)", o.name, declared)
		}

		return nil
	}

	mounts := make(map[string]string, len(outlets))
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
		}
		if field := caser.ToPascal(o.name); slices.Contains(reservedHookFields, field) {
			return errors.Newf("outlet %q would take the Hooks field %s, which the generated router reserves; choose another name", o.name, field)
		}
		if o.apiKey && caser.ToPascal(o.name)+"Auth" == bindAuthMethod {
			return errors.Newf("outlet %q would take the Handlers method BindAuth, which the generated router reserves; choose another name", o.name)
		}
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
// beside the route tables.
func (r *resourceGenerator) runServedRouterGeneration(outlets []routerOutlet, negativeTests []negativeRouterTest) error {
	begin := time.Now()
	data := r.servedRouterData(outlets, negativeTests)

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

	return nil
}

// servedRouterData builds the template payload from the validated outlet declarations.
func (r *resourceGenerator) servedRouterData(outlets []routerOutlet, negativeTests []negativeRouterTest) *servedRouterData {
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
	for _, flavor := range []AuthFlavor{Password, OIDCGoogle, OIDCAzure} {
		if f, ok := flavors[flavor]; ok {
			data.Flavors = append(data.Flavors, f)
		}
	}
	data.HandlersSummary = handlersSummary(data)

	return data
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
//	  + ValidateSession, ValidateXSRFToken: hooks.{{ .HookField }}, {{ .RoutesFunc }}
{{- end }}
{{- end }}
//
// hooks.Root's routes sit behind the every-request chain alone. Under an outlet's prefix
// nothing else answers: an unknown path is 404.{{ if .WebApps }} Outside every prefix the browser
// applications answer, longer mount paths first:{{ range $i, $w := .WebApps }}{{ if $i }},{{ end }} {{ $w.Mount }} ({{ $w.DeepLink }}, {{ $w.Assets }}){{ end }}.{{ end }}
package {{ .Package }}

import (
	"fmt"
	"net/http"

{{- range .AuthImports }}
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
	r := chi.NewRouter()

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
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

{{- range .AuthImports }}
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

	router := New(newRouterHandlersStub(rec), hooks)
	req := httptest.NewRequestWithContext(t.Context(), method, url, http.NoBody)
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
}

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
