// Package router handles wiring up the routes to handlers and the middleware in between.
package router

import (
	"net/http"

	"github.com/cccteam/ccc/resource/lodestar/pkg/auth/crew"
	"github.com/cccteam/ccc/resource/lodestar/pkg/auth/members"
	"github.com/cccteam/session"
	"github.com/go-chi/chi/v5"
)

// Handlers is the full handler surface of the served application: the generated API
// handlers for every outlet plus the session, middleware, and static-asset handlers the
// two browser applications need, and the droids outlet's authentication.
type Handlers interface {
	GeneratedHandlers
	GeneratedPortalHandlers
	GeneratedDroidsHandlers
	// The console's session handlers: the crew auth's.
	session.PasswordAuthHandlers
	// Portal returns the portal outlet's session handlers: the members auth's, whose
	// people sign in through their company's Google directory.
	Portal() session.OIDCGoogleHandlers
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

	// droids outlet auth: API-key authentication binding the request to the droid
	// service identity, replacing the browser's session and XSRF guards.
	DroidAuth(next http.Handler) http.Handler

	// The console's hand-written routes: the sector-scoped ship's log over the
	// change-tracking table (registered through @manualAddResource(List, domain)), the
	// mission document download, the impersonation mint route and the watch desk over
	// the live impersonated sessions. EndImpersonation and EnforceReadOnlyMask are the
	// session library's own, through PasswordAuthHandlers.
	ShipsLogEntries() http.HandlerFunc
	MissionDocumentContent() http.HandlerFunc
	Impersonate() http.HandlerFunc
	ActiveImpersonations() http.HandlerFunc
	RevokeImpersonation() http.HandlerFunc
	EndImpersonation() http.HandlerFunc
	EnforceReadOnlyMask(next http.Handler) http.Handler

	// The portal's hand-written route: a company's statement over the change log
	// (registered through @manualAddResource(List, domain) with @outlet(portal)).
	ClientStatements() http.HandlerFunc

	// Angular app assets: the console at / and the portal at /portal/.
	DeepLink(next http.Handler) http.Handler
	StaticAssets() http.HandlerFunc
	PortalDeepLink(next http.Handler) http.Handler
	PortalAssets() http.HandlerFunc
}

// MissionDocumentContentRoute serves a mission document's bytes; the document listing
// is the generated mission-documents route beside it.
const MissionDocumentContentRoute = "/api/sectors/{sectorID}/mission-documents/{missionDocumentID}/content"

// ImpersonationRoute names one live impersonated session on the watch desk.
const ImpersonationRoute = "/api/impersonations/{impersonationID}"

// ClientStatementsRoute is the portal's statement route under the sector segment.
const ClientStatementsRoute = "/portal/api/sectors/{sectorID}/client-statements"

// New wires the full served application: the crew auth's session handling and login
// around the console's generated API routes, a second session group around the portal
// outlet's routes under its own prefix, the API-key group around the droids outlet's
// routes, and the two Angular applications for everything else.
func New(h Handlers) *chi.Mux {
	return newRouter(h,
		func(r chi.Router) { generatedRoutes(r, h) },
		func(r chi.Router) { generatedPortalRoutes(r, h) },
		func(r chi.Router) { generatedDroidsRoutes(r, h) },
	)
}

// newRouter is the composition seam for the router's structure: api registers the
// console's authenticated API surface inside its session group, portalAPI the portal's
// inside its own, and droidsAPI the machine surface inside the API-key group.
func newRouter(h Handlers, api, portalAPI, droidsAPI func(chi.Router)) *chi.Mux {
	r := chi.NewRouter()

	r.Use(h.LoggerMiddleware())
	r.Use(h.SecurityHeaders)
	r.Use(h.WithParamsHTTP())

	// The console: the crew auth's browser sessions under /api, with the read-only
	// backstop mounted after session validation: a view-as session that somehow issues
	// a write is refused at the door before any handler runs.
	sessionGroup(r, h, crew.Name, "/api", func(r chi.Router) {
		// Ending a view stays reachable from a read-only session: it ends the session,
		// it writes nothing the mask protects, so it is mounted before the backstop.
		r.Post("/api/impersonate/end", h.EndImpersonation())

		r.Group(func(r chi.Router) {
			r.Use(h.EnforceReadOnlyMask)

			r.Get("/api/sectors/{sectorID}/ships-log-entries", h.DomainGuard()(h.ShipsLogEntries()))
			r.Get(MissionDocumentContentRoute, h.DomainGuard()(h.MissionDocumentContent()))
			r.Post("/api/impersonate", h.Impersonate())
			r.Get("/api/impersonations", h.ActiveImpersonations())
			r.Delete(ImpersonationRoute, h.RevokeImpersonation())

			api(r)
		})
	})

	// The portal: the members auth composed around the portal prefix, so a client signs
	// in through the directory at /portal/api/user/login and the portal's generated
	// client reads its digest and user-domains under /portal/api. Its session is its
	// own: a client's cookie opens nothing under /api.
	oidcGroup(r, h, h.Portal(), members.Name, "/portal/api", func(r chi.Router) {
		r.Get(ClientStatementsRoute, h.DomainGuard()(h.ClientStatements()))

		portalAPI(r)
	})

	r.Group(func(r chi.Router) {
		// The droids outlet: machine clients authenticate with an API key, so the
		// group carries no session handling and no XSRF guard.
		r.Use(h.NoCaching)
		r.Use(h.CompressionMiddleware())
		r.Use(h.DroidAuth)

		droidsAPI(r)
	})

	for _, prefix := range []string{"/api/", "/portal/api/", "/droids/"} {
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

// sessionGroup composes one browser-session surface under prefix for the crew auth, the
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
// sign in through a directory: the login redirect, the directory's callback, the session
// and logout routes, then the authenticated API routes behind session validation and the
// XSRF guard. Google has no directory-initiated logout, so there is no front-channel
// route. Every request in the group is bound to the named auth.
//
// Demonstrates: auth.two-populations, outlet.session, outlet.session, outlet.api-key, outlet.isolation, hand-written-route, impersonation.read-only-backstop, impersonation.end, consolidation.batch.
func oidcGroup(r chi.Router, h Handlers, s session.OIDCGoogleHandlers, name, prefix string, api func(chi.Router)) {
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
