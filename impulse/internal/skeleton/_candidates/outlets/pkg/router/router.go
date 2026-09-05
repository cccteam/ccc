// Package router handles wiring up the routes to handlers and the middleware in between.
package router

import (
	"net/http"

	"github.com/cccteam/session"
	"github.com/go-chi/chi/v5"
)

// Handlers is the full handler surface of the served application: the generated API
// handlers for every outlet plus the session, middleware, and static-asset handlers the
// two browser applications need, and the machines outlet's authentication.
type Handlers interface {
	GeneratedHandlers
	GeneratedPortalHandlers
	GeneratedMachinesHandlers
	session.PasswordAuthHandlers

	// app middleware
	LoggerMiddleware() func(http.Handler) http.Handler
	SecurityHeaders(next http.Handler) http.Handler
	WithParamsHTTP() func(http.Handler) http.Handler

	// api middleware
	NoCaching(next http.Handler) http.Handler
	CompressionMiddleware() func(http.Handler) http.Handler

	// machines outlet auth: API-key authentication binding the request to the machine
	// service identity, replacing the browser's session and XSRF guards.
	MachineAuth(next http.Handler) http.Handler

	// Angular app assets: the console at / and the portal at /portal/.
	DeepLink(next http.Handler) http.Handler
	StaticAssets() http.HandlerFunc
	PortalDeepLink(next http.Handler) http.Handler
	PortalAssets() http.HandlerFunc
}

// New wires the full served application: session handling and login around the
// console's generated API routes, the same session handling around the portal outlet's
// routes under its own prefix, the API-key group around the machines outlet's routes,
// and the two Angular applications for everything else.
func New(h Handlers) *chi.Mux {
	return newRouter(h,
		func(r chi.Router) { generatedRoutes(r, h) },
		func(r chi.Router) { generatedPortalRoutes(r, h) },
		func(r chi.Router) { generatedMachinesRoutes(r, h) },
	)
}

// newRouter is the composition seam for the router's structure tests: api registers the
// console's authenticated API surface inside its session group, portalAPI the portal's
// inside its own, and machinesAPI the machine surface inside the API-key group.
func newRouter(h Handlers, api, portalAPI, machinesAPI func(chi.Router)) *chi.Mux {
	r := chi.NewRouter()

	r.Use(h.LoggerMiddleware())
	r.Use(h.SecurityHeaders)
	r.Use(h.WithParamsHTTP())

	// The console: browser sessions under /api.
	sessionGroup(r, h, "/api", api)

	// The portal: the same PasswordAuth composed around the portal prefix, so a portal
	// user signs in at /portal/api/user/login and the portal's generated client reads
	// its digest and user-domains under /portal/api.
	sessionGroup(r, h, "/portal/api", portalAPI)

	r.Group(func(r chi.Router) {
		// The machines outlet: clients authenticate with an API key, so the group
		// carries no session handling and no XSRF guard.
		r.Use(h.NoCaching)
		r.Use(h.CompressionMiddleware())
		r.Use(h.MachineAuth)

		machinesAPI(r)
	})

	for _, prefix := range []string{"/api/", "/portal/api/", "/machines/"} {
		r.Route(prefix, func(r chi.Router) {
			r.NotFound(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "Not Found", http.StatusNotFound)
			}))
		})
	}

	r.Route("/portal", func(r chi.Router) {
		r.Use(h.PortalDeepLink)

		r.Get("/*", h.PortalAssets())
	})

	r.Route("/", func(r chi.Router) {
		r.Use(h.DeepLink)

		r.Get("/*", h.StaticAssets())
	})

	return r
}

// sessionGroup composes one browser-session surface under prefix: the login, session,
// and logout routes, then the authenticated API routes behind session validation and
// the XSRF guard.
func sessionGroup(r chi.Router, h Handlers, prefix string, api func(chi.Router)) {
	r.Group(func(r chi.Router) {
		// Disable all caching of API requests
		r.Use(h.NoCaching)

		// compress api data so large responses are not a problem
		r.Use(h.CompressionMiddleware())

		// Configure global session handling
		r.Use(h.StartSession)

		// Set xsrf token
		r.Use(h.SetXSRFToken)

		// Login validates the credentials and starts a session.
		r.Post(prefix+"/user/login", h.Login())

		r.Get(prefix+"/user/session", h.Authenticated())
		r.Delete(prefix+"/user/session", h.Logout())

		r.Group(func(r chi.Router) {
			// all api requests must be authenticated
			r.Use(h.ValidateSession)
			// check xsrf token for all api calls
			r.Use(h.ValidateXSRFToken)

			api(r)
		})
	})
}
