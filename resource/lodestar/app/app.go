// Package app contains Lodestar's http handlers. Most handlers are generated; this file
// provides the application plumbing the generated code depends on, plus the hand-written
// served surface (middleware, the droids outlet's API-key authentication, and the two
// browser applications' static assets) router.New composes around the generated API
// routes.
package app

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strings"
	"time"

	"github.com/cccteam/access"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/lodestar/pkg/auth"
	"github.com/cccteam/ccc/resource/lodestar/pkg/auth/crew"
	"github.com/cccteam/ccc/resource/lodestar/pkg/auth/members"
	"github.com/cccteam/ccc/resource/lodestar/pkg/computedresources"
	"github.com/cccteam/ccc/resource/lodestar/pkg/rpc"
	"github.com/cccteam/ccc/resource/lodestar/pkg/store"
	"github.com/cccteam/httpio"
	"github.com/cccteam/logger"
	"github.com/cccteam/session"
	"github.com/cccteam/session/sessioninfo"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-playground/validator/v10"
	"github.com/jtwatson/spaassets"
)

const (
	// cspPolicy allows the console's own assets plus the Google Fonts hosts
	// index.html links for the Roboto and Material Icons faces.
	cspPolicy = "default-src 'self'; worker-src 'self'; connect-src 'self'; " +
		"style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; " +
		"font-src 'self' https://fonts.gstatic.com data:; img-src 'self' data:"

	hstsPolicy          = "max-age=31536000; includeSubDomains"
	referrerPolicy      = "no-referrer"
	xContentTypeOptions = "nosniff"
	xFrameOptions       = "SAMEORIGIN"
)

// Configurer carries the dependencies for an App: the database client, the permission
// engine the handlers check against, the session manager, the request validator, the
// log exporter, and where the console's built bundle lives.
type Configurer interface {
	ResourceClient() resource.Client
	// CursorKey seals the cursors every paged list issues: one key per application,
	// derived from the cookie key, so a cursor is never a cookie.
	CursorKey() *resource.CursorKey
	Access() access.Controller
	// Crew returns the auth the console binds to: the crew auth, whose session manager
	// the App composes its login and session handlers from.
	Crew() *crew.Auth
	// Members returns the auth the portal outlet binds to: the members auth, whose
	// session manager the App hands the router for the portal's session group; nil
	// where no session group is composed (the test router).
	Members() *members.Auth
	// MembersAccess returns the permission engine a request the portal's session group
	// bound checks against: the members store, or nil where the members population is
	// not served.
	MembersAccess() access.Controller
	Validator() *validator.Validate
	LogExporter() logger.Exporter
	ConsoleDist() string
	PortalDist() string
	// DroidsAPIKey is the bearer key the droids outlet's clients present.
	DroidsAPIKey() string
	// Documents is the store the generated upload frame streams files into (UploadStore)
	// and MissionDocumentContent reads them back from.
	Documents() *store.DirStore
	TenancyConfigurer
}

// operationsClock is the zone the bare word local resolves to in temporal grant
// conditions: the fleet coordinates on headquarters time.
//
// Demonstrates: condition.local-zone.
var operationsClock = func() *time.Location {
	loc, err := time.LoadLocation("America/Denver")
	if err != nil {
		panic(err) // the timezone database is embedded; this cannot fail at runtime
	}

	return loc
}()

// droidUser is the service identity droids-outlet requests act as once the API key
// authenticates: the bootstrap grants it roles like any other user, so the machine
// surface goes through the same fail-closed permission checks as the browser.
//
// Demonstrates: outlet.api-key.
const droidUser = "droid-r7"

// App implements the application's http handlers, most of which are generated. It owns no
// router: whoever serves it composes one at the edge (main composes router.New, test
// suites compose router.NewTestRouter).
type App struct {
	// access is the default auth's engine, the crew auth's; engines holds every auth's by
	// name, for requests a session group bound to another auth.
	access  access.Controller
	engines map[string]access.Controller
	*session.PasswordAuth[session.NoCustomData, session.NoCustomData]
	portal         *session.OIDCGoogle[session.NoCustomData, session.NoCustomData]
	resourceClient resource.Client
	cursorKey      *resource.CursorKey
	validate       *validator.Validate
	logExporter    logger.Exporter
	consoleDist    string
	portalDist     string
	droidsAPIKey   string
	domainVisible  DomainVisibleFunc
	rpcClient      *rpc.Client
	computedClient *computedresources.Client
	documents      *store.DirStore
}

// New constructs an App from its dependencies.
func New(cfg Configurer) *App {
	// The fleet runs one operations clock: the bare word local in a temporal grant
	// condition (timeOfDay(now, local), dayOfWeek(now, local)) resolves to headquarters
	// time, the Dockmaster's day shift.
	resource.SetLocalZone(operationsClock)

	engine := cfg.Access()
	a := &App{
		access:         engine,
		engines:        map[string]access.Controller{},
		resourceClient: cfg.ResourceClient(),
		cursorKey:      cfg.CursorKey(),
		validate:       cfg.Validator(),
		logExporter:    cfg.LogExporter(),
		consoleDist:    cfg.ConsoleDist(),
		portalDist:     cfg.PortalDist(),
		droidsAPIKey:   cfg.DroidsAPIKey(),
		domainVisible:  cfg.DomainVisible,
		rpcClient:      rpc.NewClient(func(role accesstypes.Role) resource.RolePermissions { return engine.ForRole(role) }),
		computedClient: computedresources.NewClient(),
		documents:      cfg.Documents(),
	}
	// The authorization suites bind no auth: they compose the API surface through the
	// test router, and nothing on that path touches the session.
	if crewAuth := cfg.Crew(); crewAuth != nil {
		a.PasswordAuth = crewAuth.Session()
	}
	if membersAuth := cfg.Members(); membersAuth != nil {
		a.portal = membersAuth.Session()
	}
	if membersEngine := cfg.MembersAccess(); membersEngine != nil {
		a.engines[members.Name] = membersEngine
	}

	return a
}

// BindAuth is the middleware a session group carries to say which auth authenticated its
// requests, so the permission checks and the tenant visibility of everything behind it
// answer from that auth's store. A group that binds nothing gets the default auth.
//
// Demonstrates: auth.two-populations.
func (a *App) BindAuth(name string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(auth.Bind(r.Context(), name)))
		})
	}
}

// engine returns the permission engine of the auth the request came through.
func (a *App) engine(ctx context.Context) access.Controller {
	if engine, ok := a.engines[auth.Name(ctx)]; ok {
		return engine
	}

	return a.access
}

// Portal returns the portal outlet's session handlers: the members auth's, so a portal
// session opens nothing on the console and a console session nothing on the portal.
//
// Demonstrates: auth.directory-roles.
func (a *App) Portal() session.OIDCGoogleHandlers {
	if a.portal == nil {
		return nil
	}

	return a.portal
}

// LoggerMiddleware returns a middleware that logs requests.
func (a *App) LoggerMiddleware() func(http.Handler) http.Handler {
	return logger.NewRequestLogger(a.logExporter)
}

// SecurityHeaders is a middleware that sets security-related headers on the response.
func (a *App) SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", cspPolicy)
		w.Header().Set("Strict-Transport-Security", hstsPolicy)
		w.Header().Set("Referrer-Policy", referrerPolicy)
		w.Header().Set("X-Content-Type-Options", xContentTypeOptions)
		w.Header().Set("X-Frame-Options", xFrameOptions)

		next.ServeHTTP(w, r)
	})
}

// NoCaching is a middleware that sets headers to prevent caching of the response.
func (a *App) NoCaching(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache") // For HTTP/1.0 backward compatibility
		w.Header().Set("Expires", "0")       // For proxies

		next.ServeHTTP(w, r)
	})
}

// CompressionMiddleware returns a middleware that compresses http responses.
func (a *App) CompressionMiddleware() func(http.Handler) http.Handler {
	return middleware.Compress(5)
}

// WithParamsHTTP returns the middleware that captures route parameters for httpio.
func (a *App) WithParamsHTTP() func(http.Handler) http.Handler {
	return httpio.WithParams
}

// DeepLink rewrites the console's Angular routes to its entry point so bookmarked
// frontend routes load the single-page application.
func (a *App) DeepLink(next http.Handler) http.Handler {
	return spaassets.DeepLink(next, "/")
}

// StaticAssets serves the console's built Angular application.
func (a *App) StaticAssets() http.HandlerFunc {
	return serveSPA(http.FileServer(http.Dir(a.consoleDist)))
}

// PortalDeepLink rewrites the portal's Angular routes to its entry point under /portal/.
func (a *App) PortalDeepLink(next http.Handler) http.Handler {
	return spaassets.DeepLink(next, "/portal/")
}

// PortalAssets serves the portal's built Angular application from /portal/.
func (a *App) PortalAssets() http.HandlerFunc {
	return serveSPA(http.StripPrefix("/portal", http.FileServer(http.Dir(a.portalDist))))
}

// DroidAuth authenticates the droids outlet's machine clients: the request must carry the
// configured API key as "Authorization: Bearer <key>". A valid key binds the request to
// the droid service identity the way the session middleware binds a browser request to
// its user, so the handlers behind it run the same fail-closed permission checks. An
// empty configured key disables the surface.
//
// Demonstrates: outlet.api-key, outlet.exclusive.
func (a *App) DroidAuth(next http.Handler) http.Handler {
	return httpio.Log(func(w http.ResponseWriter, r *http.Request) error {
		key, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || a.droidsAPIKey == "" ||
			subtle.ConstantTimeCompare([]byte(key), []byte(a.droidsAPIKey)) != 1 {
			return httpio.NewEncoder(w).ClientMessage(r.Context(), httpio.NewUnauthorizedMessage("a valid droid API key is required"))
		}

		ctx := context.WithValue(r.Context(), sessioninfo.CtxSessionInfo, &sessioninfo.SessionData{
			SessionInfo: &sessioninfo.SessionInfo{Username: droidUser},
		})
		next.ServeHTTP(w, r.WithContext(ctx))

		return nil
	})
}

// serveSPA wraps a file server with the caching posture a single-page application
// wants: the entry document is never cached, hashed assets are cached forever.
func serveSPA(assets http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/") || strings.HasSuffix(r.URL.Path, "/index.html") {
			w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
			w.Header().Set("Pragma", "no-cache") // For HTTP/1.0 backward compatibility
			w.Header().Set("Expires", "0")       // For proxies
		} else {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}

		assets.ServeHTTP(w, r)
	}
}

// UserPermissions returns the permission checker for a request, composed from the
// session's principal: the engine of the auth the request came through, bound to the
// user for an ordinary or impersonated-user session, bound to the role for a session
// established as a role, and attenuated by the session's permission mask.
//
// Demonstrates: impersonation.session-permissions.
func (a *App) UserPermissions(r *http.Request) resource.UserPermissions {
	engine := a.engine(r.Context())

	return resource.SessionPermissions(r.Context(), engine.ForUser, engine.ForRole)
}

// Validator returns the request validator the generated decoder constructors draw on.
func (a *App) Validator() resource.ValidatorFunc {
	return a.validate
}

// ResourceClient returns the database client used by the resource layer.
func (a *App) ResourceClient() resource.Client {
	return a.resourceClient
}

// CursorKey returns the key that seals the cursors the generated list handlers issue:
// one key per application, derived from the cookie key, so a cursor carries nothing
// readable and never survives a rotation the sessions do not.
//
// Demonstrates: paging.cursor, paging.sealed-cursor, paging.link-header, paging.total-count, paging.offset-refused, paging.readability-rule, paging.survives-writes, paging.limit-all.
func (a *App) CursorKey() *resource.CursorKey {
	return a.cursorKey
}

// RPCClient returns the dependencies for RPC method implementations: the role checker a
// body composes through caller.As.
func (a *App) RPCClient() *rpc.Client {
	return a.rpcClient
}

// ComputedClient returns the dependencies for computed-resource query logic.
func (a *App) ComputedClient() *computedresources.Client {
	return a.computedClient
}

// UploadStore is the store the generated upload frame streams each file part to before
// an @upload method's body runs, promotes after the transaction commits, and discards
// when it does not.
//
// Demonstrates: rpc.upload-store.
func (a *App) UploadStore() resource.UploadStore {
	return a.documents
}
