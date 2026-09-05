// Package router handles wiring up the routes to handlers and the middleware in between.
package router

import (
	"net/http"

	"github.com/cccteam/session"
	"github.com/go-chi/chi/v5"
)

// Handlers is the full handler surface of the served application: the generated API
// handlers for every outlet plus the session, middleware, custom-endpoint, and
// static-asset handlers two browsable applications and a machine channel need.
type Handlers interface {
	GeneratedHandlers
	GeneratedPortalHandlers
	GeneratedDroidsHandlers
	session.PasswordAuthHandlers

	// app middleware
	LoggerMiddleware() func(http.Handler) http.Handler
	SecurityHeaders(next http.Handler) http.Handler
	WithParamsHTTP() func(http.Handler) http.Handler

	// api middleware
	NoCaching(next http.Handler) http.Handler
	CompressionMiddleware() func(http.Handler) http.Handler

	// droids outlet auth: API-key authentication binding the request to the droid
	// service identity, replacing the browser's session and XSRF guards.
	DroidAuth(next http.Handler) http.Handler

	// ShipsLogEntries is the hand-written, sector-scoped list surface over the
	// change-tracking table; its permission is registered through
	// @manualAddResource(List, domain) and checked inside the handler.
	ShipsLogEntries() http.HandlerFunc

	// Impersonate is the hand-written mint route for view-as-user and act-as-role
	// sessions, gated by the manual ViewAsUser and AssumeRole Execute registrations.
	Impersonate() http.HandlerFunc

	// Angular app assets: the crew console at / and the client portal at /client/.
	DeepLink(next http.Handler) http.Handler
	StaticAssets() http.HandlerFunc
	PortalDeepLink(next http.Handler) http.Handler
	PortalStaticAssets() http.HandlerFunc
}

// New wires the full served application: session handling and the demo login around
// the generated API routes (the crew console's outlet at /api and the client portal's
// at /portal, each with its own login and permission routes), the API-key group
// around the droid outlet's routes, and the two Angular applications for everything
// else.
func New(h Handlers) *chi.Mux {
	return newRouter(h,
		func(r chi.Router) { generatedRoutes(r, h) },
		func(r chi.Router) { generatedPortalRoutes(r, h) },
		func(r chi.Router) { generatedDroidsRoutes(r, h) },
	)
}

// newRouter is the composition seam for the router's structure: api registers the
// console's authenticated surface, portalAPI the portal's, each inside its own
// session group, and droidsAPI the machine surface inside the API-key group.
func newRouter(h Handlers, api, portalAPI, droidsAPI func(chi.Router)) *chi.Mux {
	r := chi.NewRouter()

	r.Use(h.LoggerMiddleware())
	r.Use(h.SecurityHeaders)
	r.Use(h.WithParamsHTTP())

	// The crew console's outlet. One PasswordAuth serves both browser outlets; the
	// prefix is what differs, so the same session cookie is honored under either.
	sessionGroup(r, h, "/api", func(r chi.Router) {
		r.Get("/api/sectors/{sectorID}/ships-log-entries", h.DomainGuard()(h.ShipsLogEntries()))
		r.Post("/api/impersonate", h.Impersonate())

		api(r)
	})

	// The client portal's outlet: the same session machinery composed around the
	// portal prefix, serving only the portal's members and its permission routes.
	sessionGroup(r, h, "/portal", portalAPI)

	r.Group(func(r chi.Router) {
		// The droids outlet: machine clients authenticate with an API key, so the
		// group carries no session handling and no XSRF guard.
		r.Use(h.NoCaching)
		r.Use(h.CompressionMiddleware())
		r.Use(h.DroidAuth)

		droidsAPI(r)
	})

	for _, prefix := range []string{"/api/", "/portal/", "/droids/"} {
		r.Route(prefix, func(r chi.Router) {
			r.NotFound(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "Not Found", http.StatusNotFound)
			}))
		})
	}

	r.Route("/client", func(r chi.Router) {
		r.Use(h.PortalDeepLink)

		r.Get("/*", h.PortalStaticAssets())
	})

	r.Route("/", func(r chi.Router) {
		r.Use(h.DeepLink)

		r.Get("/*", h.StaticAssets())
	})

	return r
}

// sessionGroup composes one browser outlet: caching and compression, session start
// and the XSRF token, the login and session routes under the prefix, then the
// authenticated surface behind session and XSRF validation.
func sessionGroup(r chi.Router, h Handlers, prefix string, authenticated func(chi.Router)) {
	r.Group(func(r chi.Router) {
		// Disable all caching of API requests
		r.Use(h.NoCaching)

		// compress api data so large responses are not a problem
		r.Use(h.CompressionMiddleware())

		// Configure global session handling
		r.Use(h.StartSession)

		// Set xsrf token
		r.Use(h.SetXSRFToken)

		// Login validates the demo persona's credentials and starts a session.
		r.Post(prefix+"/user/login", h.Login())

		r.Get(prefix+"/user/session", h.Authenticated())
		r.Delete(prefix+"/user/session", h.Logout())

		r.Group(func(r chi.Router) {
			// all api requests must be authenticated
			r.Use(h.ValidateSession)
			// check xsrf token for all api calls
			r.Use(h.ValidateXSRFToken)

			authenticated(r)
		})
	})
}
