// Package app contains the http handlers for the waystation. Most handlers are
// generated; this file provides the application plumbing the generated code depends on,
// plus the handwritten served surface (session, middleware, custom endpoints, static
// assets) router.New composes around the generated API routes.
package app

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/cccteam/access"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/waystation/pkg/computedresources"
	"github.com/cccteam/ccc/resource/waystation/pkg/rpc"
	"github.com/cccteam/httpio"
	"github.com/cccteam/logger"
	"github.com/cccteam/session"
	"github.com/cccteam/session/sessioninfo"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-playground/validator/v10"
	"github.com/jtwatson/spaassets"
)

const (
	// cspPolicy allows the Angular application's own assets plus the Google Fonts
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
// source — the waystation derives both from its tenant table, and test suites script
// them per request through this seam. Waystation existence is concealed
// (generation.WithConcealedDomains): DomainVisible answers whether the station exists
// AND the caller holds at least one grant in it, so a prober cannot confirm a station
// exists from the rejection shape.
type Configurer interface {
	ResourceClient() resource.Client
	RPCClient() *rpc.Client
	Access() access.Controller
	Session() *session.PasswordAuth[session.NoCustomData, session.NoCustomData]
	Validator() *validator.Validate
	GuiDist() string
	DomainVisible(ctx context.Context, user accesstypes.User, domain accesstypes.Domain) (bool, error)
	Domains(ctx context.Context) ([]accesstypes.Domain, error)
	AutomationAPIKey() string
}

// automationUser is the service identity automation-outlet requests act as once the
// API key authenticates: the bootstrap grants it roles like any other user, so the
// machine surface goes through the same fail-closed permission checks as the browser.
const automationUser = "automation"

// App implements the http handlers for the waystation, most of which are generated. It
// owns no router: whoever serves it composes one at the edge (main composes
// router.New, test suites compose router.NewTestRouter).
type App struct {
	access access.Controller
	*session.PasswordAuth[session.NoCustomData, session.NoCustomData]
	resourceClient   resource.Client
	rpcClient        *rpc.Client
	computedClient   *computedresources.Client
	domainVisible    func(ctx context.Context, user accesstypes.User, domain accesstypes.Domain) (bool, error)
	domains          func(ctx context.Context) ([]accesstypes.Domain, error)
	guiDist          string
	validate         *validator.Validate
	automationAPIKey string
}

// New constructs an App from its dependencies.
func New(cfg Configurer) *App {
	return &App{
		access:           cfg.Access(),
		PasswordAuth:     cfg.Session(),
		resourceClient:   cfg.ResourceClient(),
		rpcClient:        cfg.RPCClient(),
		computedClient:   computedresources.NewClient(),
		domainVisible:    cfg.DomainVisible,
		domains:          cfg.Domains,
		guiDist:          cfg.GuiDist(),
		validate:         cfg.Validator(),
		automationAPIKey: cfg.AutomationAPIKey(),
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

// DeepLink rewrites Angular routes to the application entry point so bookmarked
// frontend routes load the single-page application.
func (a *App) DeepLink(next http.Handler) http.Handler {
	return spaassets.DeepLink(next, "/")
}

// StaticAssets serves the built Angular application. (Named to stay clear of the
// generated Assets resource list handler.)
func (a *App) StaticAssets() http.HandlerFunc {
	assets := http.FileServer(http.Dir(a.guiDist))

	return func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/") || r.URL.Path == "/index.html" {
			w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
			w.Header().Set("Pragma", "no-cache") // For HTTP/1.0 backward compatibility
			w.Header().Set("Expires", "0")       // For proxies
		} else {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}

		assets.ServeHTTP(w, r)
	}
}

// AutomationAuth authenticates the automation outlet's machine clients: the request
// must carry the configured API key as "Authorization: Bearer <key>". A valid key
// binds the request to the automation service identity the way the session middleware
// binds a browser request to its user, so the handlers behind it run the same
// fail-closed permission checks. An empty configured key disables the surface.
func (a *App) AutomationAuth(next http.Handler) http.Handler {
	return httpio.Log(func(w http.ResponseWriter, r *http.Request) error {
		key, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || a.automationAPIKey == "" ||
			subtle.ConstantTimeCompare([]byte(key), []byte(a.automationAPIKey)) != 1 {
			return httpio.NewEncoder(w).ClientMessage(r.Context(), httpio.NewUnauthorizedMessage("a valid automation API key is required"))
		}

		ctx := context.WithValue(r.Context(), sessioninfo.CtxSessionInfo, &sessioninfo.SessionData{
			SessionInfo: &sessioninfo.SessionInfo{Username: automationUser},
		})
		next.ServeHTTP(w, r.WithContext(ctx))

		return nil
	})
}

// UserPermissions returns the permission checker for a request: the access engine
// bound to the session's user (established by session middleware; the request panics
// without one, like the generated mutation handlers). Test suites script permissions
// by supplying a fake access.Controller through the Configurer's Access seam.
func (a *App) UserPermissions(r *http.Request) resource.UserPermissions {
	return a.access.ForUser(accesstypes.User(sessioninfo.FromRequest(r).Username))
}

// Validator returns the request validator the generated decoder constructors draw on.
func (a *App) Validator() resource.ValidatorFunc {
	return a.validate
}

// DomainVisible reports whether the domain exists and the user holds at least one
// grant in it; the generated DomainGuard middleware and the consolidated dispatcher
// answer "no" with the same not-found an unknown domain gets, concealing station
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
