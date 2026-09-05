// Package app contains the http handlers for Lodestar. Most handlers are generated;
// this file provides the application plumbing the generated code depends on, plus the
// handwritten served surface (session, middleware, custom endpoints, static assets)
// router.New composes around the generated API routes.
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
	"github.com/cccteam/ccc/resource/lodestar/pkg/computedresources"
	"github.com/cccteam/ccc/resource/lodestar/pkg/rpc"
	"github.com/cccteam/httpio"
	"github.com/cccteam/logger"
	"github.com/cccteam/session"
	"github.com/cccteam/session/sessioninfo"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-playground/validator/v10"
	"github.com/jtwatson/spaassets"
)

const (
	// cspPolicy allows the Angular applications' own assets plus the Google Fonts
	// hosts index.html links for the Roboto and Material Symbols faces.
	cspPolicy = "default-src 'self'; worker-src 'self'; connect-src 'self'; " +
		"style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; " +
		"font-src 'self' https://fonts.gstatic.com data:; img-src 'self' data:"

	hstsPolicy          = "max-age=31536000; includeSubDomains"
	referrerPolicy      = "no-referrer"
	xContentTypeOptions = "nosniff"
	xFrameOptions       = "SAMEORIGIN"
)

// Configurer carries the dependencies for an App. Access is the permission engine the
// handlers check against; DomainVisible and Domains are the application's tenancy
// source — Lodestar derives both from its Sectors table, and test suites script them
// per request through this seam. Sector existence is concealed
// (generation.WithConcealedDomains): DomainVisible answers whether the sector exists
// AND the caller holds at least one grant in it, so a prober cannot confirm a sector
// exists from the rejection shape.
type Configurer interface {
	ResourceClient() resource.Client
	RPCClient() *rpc.Client
	Access() access.Controller
	Session() *session.PasswordAuth[session.NoCustomData, session.NoCustomData]
	Validator() *validator.Validate
	ConsoleDist() string
	PortalDist() string
	DomainVisible(ctx context.Context, user accesstypes.User, domain accesstypes.Domain) (bool, error)
	Domains(ctx context.Context) ([]accesstypes.Domain, error)
	DroidAPIKey() string
}

// droidUser is the service identity droid-outlet requests act as once the API key
// authenticates: the bootstrap grants it roles like any other user, so the machine
// surface goes through the same fail-closed permission checks as the browser.
const droidUser = "droid-r7"

// operationsClock is the zone the bare word local resolves to in temporal grant
// conditions: the fleet coordinates on headquarters time.
var operationsClock = func() *time.Location {
	loc, err := time.LoadLocation("America/Denver")
	if err != nil {
		panic(err) // the timezone database is embedded; this cannot fail at runtime
	}

	return loc
}()

// App implements the http handlers for Lodestar, most of which are generated. It
// owns no router: whoever serves it composes one at the edge (main composes
// router.New, test suites compose router.NewTestRouter).
type App struct {
	access access.Controller
	*session.PasswordAuth[session.NoCustomData, session.NoCustomData]
	resourceClient resource.Client
	rpcClient      *rpc.Client
	computedClient *computedresources.Client
	domainVisible  func(ctx context.Context, user accesstypes.User, domain accesstypes.Domain) (bool, error)
	domains        func(ctx context.Context) ([]accesstypes.Domain, error)
	consoleDist    string
	portalDist     string
	validate       *validator.Validate
	droidAPIKey    string
}

// New constructs an App from its dependencies.
func New(cfg Configurer) *App {
	// The fleet runs one operations clock: the bare word local in a temporal grant
	// condition — timeOfDay(now, local), dayOfWeek(now, local) — resolves to
	// headquarters time (the Dockmaster's day shift).
	resource.SetLocalZone(operationsClock)

	return &App{
		access:         cfg.Access(),
		PasswordAuth:   cfg.Session(),
		resourceClient: cfg.ResourceClient(),
		rpcClient:      cfg.RPCClient(),
		computedClient: computedresources.NewClient(),
		domainVisible:  cfg.DomainVisible,
		domains:        cfg.Domains,
		consoleDist:    cfg.ConsoleDist(),
		portalDist:     cfg.PortalDist(),
		validate:       cfg.Validator(),
		droidAPIKey:    cfg.DroidAPIKey(),
	}
}

// LoggerMiddleware returns a middleware that logs requests.
func (a *App) LoggerMiddleware() func(http.Handler) http.Handler {
	return logger.NewRequestLogger(logger.NewConsoleExporter())
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

// DeepLink rewrites Angular routes to the console's entry point so bookmarked
// frontend routes load the single-page application.
func (a *App) DeepLink(next http.Handler) http.Handler {
	return spaassets.DeepLink(next, "/")
}

// StaticAssets serves the built crew-console Angular application.
func (a *App) StaticAssets() http.HandlerFunc {
	return staticAssets(http.FileServer(http.Dir(a.consoleDist)))
}

// PortalDeepLink rewrites Angular routes to the portal's entry point under /client/.
func (a *App) PortalDeepLink(next http.Handler) http.Handler {
	return spaassets.DeepLink(next, "/client/")
}

// PortalStaticAssets serves the built client-portal Angular application under
// /client/.
func (a *App) PortalStaticAssets() http.HandlerFunc {
	return staticAssets(http.StripPrefix("/client", http.FileServer(http.Dir(a.portalDist))))
}

// staticAssets wraps a file server with the cache policy a single-page application
// wants: never cache the entry point, cache hashed assets forever.
func staticAssets(assets http.Handler) http.HandlerFunc {
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

// DroidAuth authenticates the droid outlet's machine clients: the request must carry
// the configured API key as "Authorization: Bearer <key>". A valid key binds the
// request to the droid service identity the way the session middleware binds a
// browser request to its user, so the handlers behind it run the same fail-closed
// permission checks. An empty configured key disables the surface.
func (a *App) DroidAuth(next http.Handler) http.Handler {
	return httpio.Log(func(w http.ResponseWriter, r *http.Request) error {
		key, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || a.droidAPIKey == "" ||
			subtle.ConstantTimeCompare([]byte(key), []byte(a.droidAPIKey)) != 1 {
			return httpio.NewEncoder(w).ClientMessage(r.Context(), httpio.NewUnauthorizedMessage("a valid droid API key is required"))
		}

		ctx := context.WithValue(r.Context(), sessioninfo.CtxSessionInfo, &sessioninfo.SessionData{
			SessionInfo: &sessioninfo.SessionInfo{Username: droidUser},
		})
		next.ServeHTTP(w, r.WithContext(ctx))

		return nil
	})
}

// UserPermissions returns the permission checker for a request, composed from the
// session (established by session middleware; the request panics without one, like
// the generated mutation handlers): the access engine bound to the session's
// principal — a user for an ordinary or view-as session, a role for an act-as-role
// session — attenuated by the session's permission mask. Test suites script
// permissions by supplying a fake access.Controller through the Configurer's Access
// seam.
func (a *App) UserPermissions(r *http.Request) resource.UserPermissions {
	return resource.SessionPermissions(r.Context(), a.access.ForUser, a.access.ForRole)
}

// Validator returns the request validator the generated decoder constructors draw on.
func (a *App) Validator() resource.ValidatorFunc {
	return a.validate
}

// DomainVisible reports whether the sector exists and the user holds at least one
// grant in it; the generated DomainGuard middleware and the consolidated dispatcher
// answer "no" with the same not-found an unknown sector gets, concealing sector
// existence from callers with no foothold.
func (a *App) DomainVisible(ctx context.Context, user accesstypes.User, domain accesstypes.Domain) (bool, error) {
	return a.domainVisible(ctx, user, domain)
}

// ResourceClient returns the database client used by the resource layer.
func (a *App) ResourceClient() resource.Client {
	return a.resourceClient
}

// RPCClient returns the dependencies for RPC method implementations.
func (a *App) RPCClient() *rpc.Client {
	return a.rpcClient
}

// ComputedClient returns the dependencies for computed-resource query logic.
func (a *App) ComputedClient() *computedresources.Client {
	return a.computedClient
}
