package router

import (
	"net/http"

	"github.com/cccteam/session"
	"github.com/go-chi/chi/v5"
)

// ServerHandlers is the full handler surface of the served application: the generated
// API handlers plus the session, middleware, custom-endpoint, and static-asset
// handlers a browsable application needs. It lives in its own file so the mock
// generation over router.go keeps covering only the generated handler seam the
// integration tests script.
type ServerHandlers interface {
	Handlers
	session.PasswordAuthHandlers

	// app middleware
	LoggerMiddleware() func(http.Handler) http.Handler
	SecurityHeaders(next http.Handler) http.Handler
	WithParamsHTTP() func(http.Handler) http.Handler

	// api middleware
	NoCaching(next http.Handler) http.Handler
	CompressionMiddleware() func(http.Handler) http.Handler

	// demo endpoints
	SessionData() http.HandlerFunc
	Stations() http.HandlerFunc

	// Angular app assets
	DeepLink(next http.Handler) http.Handler
	Assets() http.HandlerFunc
}

// NewServer wires the full served application: session handling and the demo login
// around the generated API routes, and the Angular application for everything else.
func NewServer(h ServerHandlers) *chi.Mux {
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

		// Login validates the demo user's credentials and starts a session.
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

			r.Get("/api/stations", h.Stations())

			generatedRoutes(r, h)
		})
	})

	r.Route("/api/", func(r chi.Router) {
		r.NotFound(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "Not Found", http.StatusNotFound)
		}))
	})

	r.Route("/", func(r chi.Router) {
		r.Use(h.DeepLink)

		r.Get("/*", h.Assets())
	})

	return r
}
