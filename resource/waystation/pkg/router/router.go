// Package router handles wiring up the routes to handlers and the middleware in between.
package router

import (
	"net/http"

	"github.com/cccteam/session"
	"github.com/go-chi/chi/v5"
)

// Handlers is the full handler surface of the served application: the generated
// API handlers for every outlet plus the session, middleware, custom-endpoint, and
// static-asset handlers a browsable application needs.
type Handlers interface {
	GeneratedHandlers
	GeneratedAutomationHandlers
	session.PasswordAuthHandlers

	// app middleware
	LoggerMiddleware() func(http.Handler) http.Handler
	SecurityHeaders(next http.Handler) http.Handler
	WithParamsHTTP() func(http.Handler) http.Handler

	// api middleware
	NoCaching(next http.Handler) http.Handler
	CompressionMiddleware() func(http.Handler) http.Handler

	// automation outlet auth: API-key authentication binding the request to the
	// automation service identity, replacing the browser's session and XSRF guards.
	AutomationAuth(next http.Handler) http.Handler

	// demo endpoints
	SessionData() http.HandlerFunc

	// AuditTrailEntries is the hand-written list surface over the change-tracking
	// table; its permission is registered through @manualAddResource and checked
	// inside the handler (see app.AuditTrailEntries).
	AuditTrailEntries() http.HandlerFunc

	// Angular app assets
	DeepLink(next http.Handler) http.Handler
	StaticAssets() http.HandlerFunc
}

// New wires the full served application: session handling and the demo login around
// the generated API routes, the API-key group around the automation outlet's routes,
// and the Angular application for everything else.
func New(h Handlers) *chi.Mux {
	return newRouter(h,
		func(r chi.Router) { generatedRoutes(r, h) },
		func(r chi.Router) { generatedAutomationRoutes(r, h) },
	)
}

// newRouter is the composition seam for the router's structure tests: api registers
// the authenticated API surface inside the security middleware group and automationAPI
// the machine surface inside the API-key group, so tests can probe each group's
// enforcement without going through the generated route tables.
func newRouter(h Handlers, api, automationAPI func(chi.Router)) *chi.Mux {
	r := chi.NewRouter()

	r.Use(h.LoggerMiddleware())
	r.Use(h.SecurityHeaders)
	r.Use(h.WithParamsHTTP())

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
		r.Post("/api/user/login", h.Login())

		r.Get("/api/user/session", h.Authenticated())
		// Like the session endpoint, session-data answers gracefully (with an empty
		// permission collection) when the session is not authenticated: the frontend
		// probes it before login.
		r.Get("/api/user/session-data", h.SessionData())
		r.Delete("/api/user/session", h.Logout())

		r.Group(func(r chi.Router) {
			// all api requests must be authenticated
			r.Use(h.ValidateSession)
			// check xsrf token for all api calls
			r.Use(h.ValidateXSRFToken)

			r.Get("/api/audit-trail-entries", h.AuditTrailEntries())

			api(r)
		})
	})

	r.Group(func(r chi.Router) {
		// The automation outlet: machine clients authenticate with an API key, so the
		// group carries no session handling and no XSRF guard.
		r.Use(h.NoCaching)
		r.Use(h.CompressionMiddleware())
		r.Use(h.AutomationAuth)

		automationAPI(r)
	})

	r.Route("/api/", func(r chi.Router) {
		r.NotFound(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "Not Found", http.StatusNotFound)
		}))
	})

	r.Route("/automation/", func(r chi.Router) {
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
