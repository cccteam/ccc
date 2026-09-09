// Package app contains the application's http handlers. Most handlers are generated; this
// file provides the application plumbing the generated code depends on, plus the
// hand-written served surface (middleware and static assets) router.New composes
// around the generated API routes.
package app

import (
	"context"
	"net/http"
	"strings"

	"github.com/cccteam/access"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/tenanted/pkg/auth/staff"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/httpio"
	"github.com/cccteam/logger"
	"github.com/cccteam/session"
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
// engine the handlers check against, the tenancy seam, the session manager, the request
// validator, the log exporter, and where the console's built bundle lives. Tenant
// existence is concealed (generation.WithConcealedDomains): DomainVisible answers
// whether the tenant exists AND the caller holds at least one grant in it, so a prober
// cannot confirm a tenant exists from the rejection shape.
type Configurer interface {
	ResourceClient() resource.Client
	// CursorKey seals the cursors every paged list issues: one key per application,
	// derived from the cookie key, so a cursor is never a cookie.
	CursorKey() *resource.CursorKey
	Access() access.Controller
	DomainVisible(ctx context.Context, user accesstypes.User, domain accesstypes.Domain) (bool, error)
	// Staff returns the auth this surface binds to: the staff auth, whose session manager
	// the App composes its login and session handlers from.
	Staff() *staff.Auth
	Validator() *validator.Validate
	LogExporter() logger.Exporter
	ConsoleDist() string
}

// App implements the application's http handlers, most of which are generated. It owns no
// router: whoever serves it composes one at the edge (main composes router.New, test
// suites compose router.NewTestRouter).
type App struct {
	access access.Controller
	*session.PasswordAuth[session.NoCustomData, session.NoCustomData]
	resourceClient resource.Client
	cursorKey      *resource.CursorKey
	domainVisible  func(ctx context.Context, user accesstypes.User, domain accesstypes.Domain) (bool, error)
	validate       *validator.Validate
	logExporter    logger.Exporter
	consoleDist    string
}

// New constructs an App from its dependencies.
func New(cfg Configurer) *App {
	a := &App{
		access:         cfg.Access(),
		resourceClient: cfg.ResourceClient(),
		cursorKey:      cfg.CursorKey(),
		domainVisible:  cfg.DomainVisible,
		validate:       cfg.Validator(),
		logExporter:    cfg.LogExporter(),
		consoleDist:    cfg.ConsoleDist(),
	}
	// The authorization suites bind no auth: they compose the API surface through the
	// test router, and nothing on that path touches the session.
	if auth := cfg.Staff(); auth != nil {
		a.PasswordAuth = auth.Session()
	}

	return a
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
// session's principal: the access engine bound to the user for an ordinary or
// impersonated-user session, bound to the role for a session established as a role,
// and attenuated by the session's permission mask.
func (a *App) UserPermissions(r *http.Request) resource.UserPermissions {
	return resource.SessionPermissions(r.Context(), a.access.ForUser, a.access.ForRole)
}

// DomainVisible reports whether the tenant exists and the user holds at least one grant
// in it; the generated DomainGuard middleware and the consolidated dispatcher answer
// "no" with the same not-found an unknown tenant gets.
func (a *App) DomainVisible(ctx context.Context, user accesstypes.User, domain accesstypes.Domain) (bool, error) {
	return a.domainVisible(ctx, user, domain)
}

// Validator returns the request validator the generated decoder constructors draw on.
func (a *App) Validator() resource.ValidatorFunc {
	return a.validate
}

// ResourceClient returns the database client used by the resource layer.
func (a *App) ResourceClient() resource.Client {
	return a.resourceClient
}

// CursorKey returns the key that seals the cursors the generated list handlers issue.
func (a *App) CursorKey() *resource.CursorKey {
	return a.cursorKey
}
