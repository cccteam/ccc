// Package router serves Lodestar: the generated router (zz_gen_router.go, whose package
// comment is the middleware chain in front of every route) with the console's and the
// portal's own routes composed in through AppHooks.
package router

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// AppHandlers is the served application's full surface: the generated router's Handlers
// (every outlet's generated handlers, the two auths' session handlers, the middleware, the
// droids outlet's authentication, and the two browser applications) plus the console's
// and the portal's own routes, which AppHooks mounts inside the outlets' guards.
type AppHandlers interface {
	Handlers

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

	// The portal's hand-written route: a company's statement over the change log
	// (registered through @manualAddResource(List, domain) with @outlet(portal)).
	ClientStatements() http.HandlerFunc
}

// MissionDocumentContentRoute serves a mission document's bytes; the document listing
// is the generated mission-documents route beside it.
const MissionDocumentContentRoute = "/api/sectors/{sectorID}/mission-documents/{missionDocumentID}/content"

// ImpersonationRoute names one live impersonated session on the watch desk.
const ImpersonationRoute = "/api/impersonations/{impersonationID}"

// ClientStatementsRoute is the portal's statement route under the sector segment.
const ClientStatementsRoute = "/portal/api/sectors/{sectorID}/client-statements"

// AppHooks composes the application's own routes into the generated router (New), inside
// the guards each outlet's group already carries: the console's routes behind the crew
// auth's session validation and XSRF guard, the portal's behind the members auth's. The
// generated router owns everything else: the every-request middleware, each outlet's
// session or API-key group, the not-found handlers, and the two browser applications.
//
// Demonstrates: GenerateRouter, auth.two-populations, outlet.session, outlet.api-key, outlet.isolation, hand-written-route, impersonation.read-only-backstop, impersonation.end, consolidation.batch.
func AppHooks(h AppHandlers) Hooks {
	return Hooks{
		// The console: the read-only backstop mounted after session validation, so a
		// view-as session that somehow issues a write is refused at the door before any
		// handler runs; the generated routes register beneath it.
		Default: func(r chi.Router, generated func(chi.Router)) {
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

				generated(r)
			})
		},
		// The portal: the client statement beside the portal outlet's generated routes,
		// behind the members auth. A client's session opens nothing under /api.
		Portal: func(r chi.Router, generated func(chi.Router)) {
			r.Get(ClientStatementsRoute, h.DomainGuard()(h.ClientStatements()))

			generated(r)
		},
	}
}
