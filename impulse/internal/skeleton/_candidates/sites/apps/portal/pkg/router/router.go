// Package router handles wiring up the routes to handlers and the middleware in between.
package router

import (
	"net/http"

	"github.com/cccteam/session"
	"github.com/go-chi/chi/v5"
)

// Handlers is the full handler surface of the served application: the generated API
// handlers plus the session, middleware, and static-asset handlers the site's browser
// application needs.
type Handlers interface {
	GeneratedHandlers
	session.PasswordAuthHandlers

	// app middleware
	LoggerMiddleware() func(http.Handler) http.Handler
	SecurityHeaders(next http.Handler) http.Handler
	WithParamsHTTP() func(http.Handler) http.Handler

	// api middleware
	NoCaching(next http.Handler) http.Handler
	CompressionMiddleware() func(http.Handler) http.Handler

	// Angular app assets
	DeepLink(next http.Handler) http.Handler
	StaticAssets() http.HandlerFunc
}

// New wires the full served application: session handling and login around the
// generated API routes, and the console's Angular application for everything else.
func New(h Handlers) *chi.Mux {
	return newRouter(h, func(r chi.Router) { generatedRoutes(r, h) })
}

// newRouter is the composition seam for the router's structure tests: api registers the
// authenticated API surface inside the session group.
func newRouter(h Handlers, api func(chi.Router)) *chi.Mux {
	r := chi.NewRouter()

	r.Use(h.LoggerMiddleware())
	r.Use(h.SecurityHeaders)
	r.Use(h.WithParamsHTTP())

	// The console: browser sessions under /api.
	sessionGroup(r, h, "/api", api)

	r.Route("/api/", func(r chi.Router) {
		r.NotFound(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "Not Found", http.StatusNotFound)
		}))
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
