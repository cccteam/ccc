// Package router handles wiring up the routes to handlers and the middleware in between.
package router

import (
	"net/http"

	"github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/outlets/pkg/auth/members"
	"github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/outlets/pkg/auth/staff"
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
	// The console's session handlers: the staff auth's.
	session.PasswordAuthHandlers
	// Portal returns the portal outlet's session handlers: the members auth's, whose
	// people sign in through a directory.
	Portal() session.OIDCAzureHandlers
	// BindAuth marks a session group's requests as authenticated by the named auth, so
	// the handlers behind it check permissions and tenant visibility in that auth's store.
	BindAuth(name string) func(http.Handler) http.Handler

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

// New wires the full served application: the staff auth's session handling and login
// around the console's generated API routes, the members auth's around the portal
// outlet's routes under its own prefix, the API-key group around the machines outlet's
// routes, and the two Angular applications for everything else.
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

	// The console: the staff auth's browser sessions under /api.
	sessionGroup(r, h, staff.Name, "/api", api)

	// The portal: the members auth composed around the portal prefix, so a member
	// signs in through the directory at /portal/api/user/login and the portal's
	// generated client reads its digest and user-domains under /portal/api. Its
	// session is its own: a member's cookie opens nothing under /api.
	oidcGroup(r, h, h.Portal(), members.Name, "/portal/api", portalAPI)

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

// sessionGroup composes one browser-session surface under prefix for the staff auth, the
// application's password auth: the login, session, and logout routes, then the
// authenticated API routes behind session validation and the XSRF guard. Every request
// in the group is bound to the named auth.
func sessionGroup(r chi.Router, h Handlers, name, prefix string, api func(chi.Router)) {
	r.Group(func(r chi.Router) {
		r.Use(h.BindAuth(name))

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

// oidcGroup composes one browser-session surface under prefix for an auth whose people
// sign in through a directory: the login redirect, the directory's callback, the
// front-channel logout the directory calls when the person signs out elsewhere, the
// session and logout routes, then the authenticated API routes behind session validation
// and the XSRF guard. Every request in the group is bound to the named auth.
func oidcGroup(r chi.Router, h Handlers, s session.OIDCAzureHandlers, name, prefix string, api func(chi.Router)) {
	r.Group(func(r chi.Router) {
		r.Use(h.BindAuth(name))

		// Disable all caching of API requests
		r.Use(h.NoCaching)

		// compress api data so large responses are not a problem
		r.Use(h.CompressionMiddleware())

		// Configure global session handling
		r.Use(s.StartSession)

		// Set xsrf token
		r.Use(s.SetXSRFToken)

		// Login sends the browser to the directory; the directory returns it to the
		// callback, which starts the session and returns the browser to the page it left.
		r.Get(prefix+"/user/login", s.Login())
		r.Get(prefix+"/user/callback", s.CallbackOIDC())
		r.Get(prefix+"/user/logout", s.FrontChannelLogout())

		r.Get(prefix+"/user/session", s.Authenticated())
		r.Delete(prefix+"/user/session", s.Logout())

		r.Group(func(r chi.Router) {
			// all api requests must be authenticated
			r.Use(s.ValidateSession)
			// check xsrf token for all api calls
			r.Use(s.ValidateXSRFToken)

			api(r)
		})
	})
}
