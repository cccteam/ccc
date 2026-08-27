// Package app contains the http handlers for the starport. Most handlers are generated;
// this file provides the application plumbing the generated code depends on, plus the
// handwritten served surface (session, middleware, custom endpoints, static assets)
// router.New composes around the generated API routes.
package app

import (
	"context"
	"net/http"
	"strings"

	"github.com/cccteam/access"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/starport/pkg/rpc"
	"github.com/cccteam/ccc/resource/starport/pkg/stations"
	"github.com/cccteam/httpio"
	"github.com/cccteam/logger"
	"github.com/cccteam/session"
	"github.com/cccteam/session/sessioninfo"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-playground/errors/v5"
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
// handlers check against, and DomainExists the application's tenancy source for the
// generated handlers' unknown-domain 404 guard — both sit on the Configurer so test
// suites can script permissions and tenancy per request.
type Configurer interface {
	ResourceClient() resource.Client
	RPCClient() *rpc.Client
	Access() access.Controller
	Session() *session.PasswordAuth[session.NoCustomData, session.NoCustomData]
	Validator() *validator.Validate
	GuiDist() string
	DomainExists(ctx context.Context, domain accesstypes.Domain) (bool, error)
}

// App implements the http handlers for the starport, most of which are generated. It
// owns no router: whoever serves it composes one at the edge (main composes
// router.New, test suites compose router.NewTestRouter).
type App struct {
	access access.Controller
	*session.PasswordAuth[session.NoCustomData, session.NoCustomData]
	resourceClient  resource.Client
	rpcClient       *rpc.Client
	userPermissions func(*http.Request) resource.UserPermissions
	domainExists    func(ctx context.Context, domain accesstypes.Domain) (bool, error)
	guiDist         string
	validate        *validator.Validate
}

// New constructs an App from its dependencies.
func New(cfg Configurer) *App {
	return &App{
		access:          cfg.Access(),
		PasswordAuth:    cfg.Session(),
		resourceClient:  cfg.ResourceClient(),
		rpcClient:       cfg.RPCClient(),
		userPermissions: NewAccessUserPermissions(cfg.Access()),
		domainExists:    cfg.DomainExists,
		guiDist:         cfg.GuiDist(),
		validate:        cfg.Validator(),
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

// Assets serves the built Angular application.
func (a *App) Assets() http.HandlerFunc {
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

// SessionData reports the session user's permission collection across the global
// scope and every station. The wire shape is structural, mirroring
// accesstypes.Scope: the global partition is its own key, never a magic entry in
// the domain map. Like the session library's Authenticated handler, an
// unauthenticated session gets an empty collection rather than an error: the
// frontend probes this endpoint before login.
func (a *App) SessionData() http.HandlerFunc {
	type resourcePermissions map[accesstypes.Resource]map[accesstypes.Permission]bool

	type permissions struct {
		Global  resourcePermissions                        `json:"global"`
		Domains map[accesstypes.Domain]resourcePermissions `json:"domains"`
	}

	type response struct {
		Permissions permissions `json:"permissions"`
	}

	permissionSet := func(scopePerms accesstypes.UserScopePermissions) resourcePermissions {
		set := make(resourcePermissions, len(scopePerms.Resources))
		for res, perms := range scopePerms.Resources {
			resSet := make(map[accesstypes.Permission]bool, len(perms))
			for _, perm := range perms {
				resSet[perm] = true
			}
			set[res] = resSet
		}

		return set
	}

	return httpio.Log(func(w http.ResponseWriter, r *http.Request) error {
		ctx, err := a.PasswordAuth.API().ValidateSession(r.Context())
		if err != nil {
			if httpio.HasUnauthorized(err) {
				return httpio.NewEncoder(w).Ok(response{})
			}

			return httpio.NewEncoder(w).ClientMessage(r.Context(), err)
		}
		user := accesstypes.User(sessioninfo.FromCtx(ctx).Username)

		scopes := []accesstypes.Scope{accesstypes.GlobalScope()}
		for _, domain := range stations.Domains() {
			scopes = append(scopes, accesstypes.DomainScope(domain))
		}
		collection, err := a.access.UserManager().UserPermissions(ctx, user, scopes...)
		if err != nil {
			return httpio.NewEncoder(w).ClientMessage(ctx, errors.Wrap(err, "access.UserManager.UserPermissions()"))
		}

		perms := permissions{Domains: make(map[accesstypes.Domain]resourcePermissions, len(collection))}
		for scope, scopePerms := range collection {
			if scope.IsGlobal() {
				perms.Global = permissionSet(scopePerms)

				continue
			}
			domain, _ := scope.Domain()
			perms.Domains[domain] = permissionSet(scopePerms)
		}

		return httpio.NewEncoder(w).Ok(response{Permissions: perms})
	})
}

// StationDirectory lists the demo stations: the permission domains the station-scoped
// resources and RPC methods are served under. It is the frontend's station picker
// source; the generated Stations resource handler (permission-checked rows) owns
// /api/stations itself.
func (a *App) StationDirectory() http.HandlerFunc {
	type response struct {
		Stations []accesstypes.Domain `json:"stations"`
	}

	return httpio.Log(func(w http.ResponseWriter, _ *http.Request) error {
		return httpio.NewEncoder(w).Ok(response{Stations: stations.Domains()})
	})
}

// UserPermissions returns the permission checker for a request.
func (a *App) UserPermissions(r *http.Request) resource.UserPermissions {
	return a.userPermissions(r)
}

// Validator returns the request validator the generated decoder constructors draw on.
func (a *App) Validator() resource.ValidatorFunc {
	return a.validate
}

// DomainExists reports whether the application recognizes the domain; the generated
// DomainGuard middleware 404s unknown domains before domain-scoped handlers run.
func (a *App) DomainExists(ctx context.Context, domain accesstypes.Domain) (bool, error) {
	return a.domainExists(ctx, domain)
}

// ResourceClient returns the database client used by the resource layer.
func (a *App) ResourceClient() resource.Client {
	return a.resourceClient
}

// RPCClient returns the dependencies for RPC method implementations.
func (a *App) RPCClient() *rpc.Client {
	return a.rpcClient
}

// handleError allows the application to translate resource errors per resource type
// before they are encoded to the client. The starport performs no translation.
func handleError[T any](err error) error {
	return err
}
