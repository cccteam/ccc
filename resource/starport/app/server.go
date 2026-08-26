package app

import (
	"context"
	"net/http"
	"strings"

	"github.com/cccteam/access"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/starport/pkg/router"
	"github.com/cccteam/ccc/resource/starport/pkg/rpc"
	"github.com/cccteam/ccc/resource/starport/pkg/stations"
	"github.com/cccteam/httpio"
	"github.com/cccteam/logger"
	"github.com/cccteam/session"
	"github.com/cccteam/session/sessioninfo"
	"github.com/go-chi/chi/v5"
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

var _ http.Handler = &Server{}

// ServerConfigurer carries the dependencies for a served Server.
type ServerConfigurer interface {
	ResourceClient() resource.Client
	RPCClient() *rpc.Client
	Access() access.Controller
	Session() *session.PasswordAuth[session.NoCustomData, session.NoCustomData]
	Validator() *validator.Validate
	GuiDist() string
}

// Server is the served starport application: the API handlers of App wrapped with the
// session, middleware, and static-asset plumbing of a full web application. The
// integration tests keep exercising the bare App; the Server is the production
// surface main.go starts.
type Server struct {
	*App
	*session.PasswordAuth[session.NoCustomData, session.NoCustomData]
	access  access.Controller
	guiDist string
	router  *chi.Mux
}

// NewServer constructs the served application from its dependencies.
func NewServer(cfg ServerConfigurer) *Server {
	s := &Server{
		App: New(Config{
			ResourceClient:  cfg.ResourceClient(),
			RPCClient:       cfg.RPCClient(),
			UserPermissions: NewAccessUserPermissions(cfg.Access()),
			DomainExists: func(_ context.Context, domain accesstypes.Domain) (bool, error) {
				return stations.Exists(domain), nil
			},
			Validator: cfg.Validator(),
		}),
		PasswordAuth: cfg.Session(),
		access:       cfg.Access(),
		guiDist:      cfg.GuiDist(),
	}

	s.router = router.NewServer(s)

	return s
}

// ServeHTTP implements the http.Handler interface.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}

// LoggerMiddleware returns a middleware that logs requests.
func (s *Server) LoggerMiddleware() func(http.Handler) http.Handler {
	return logger.NewRequestLogger(logger.NewConsoleExporter())
}

// SecurityHeaders is a middleware that sets security-related headers on the response.
func (s *Server) SecurityHeaders(next http.Handler) http.Handler {
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
func (s *Server) NoCaching(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache") // For HTTP/1.0 backward compatibility
		w.Header().Set("Expires", "0")       // For proxies

		next.ServeHTTP(w, r)
	})
}

// CompressionMiddleware returns a middleware that compresses http responses.
func (s *Server) CompressionMiddleware() func(http.Handler) http.Handler {
	return middleware.Compress(5)
}

// WithParamsHTTP returns the middleware that captures route parameters for httpio.
func (s *Server) WithParamsHTTP() func(http.Handler) http.Handler {
	return httpio.WithParams
}

// DeepLink rewrites Angular routes to the application entry point so bookmarked
// frontend routes load the single-page application.
func (s *Server) DeepLink(next http.Handler) http.Handler {
	return spaassets.DeepLink(next, "/")
}

// Assets serves the built Angular application.
func (s *Server) Assets() http.HandlerFunc {
	assets := http.FileServer(http.Dir(s.guiDist))

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
func (s *Server) SessionData() http.HandlerFunc {
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
		ctx, err := s.PasswordAuth.API().ValidateSession(r.Context())
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
		collection, err := s.access.UserManager().UserPermissions(ctx, user, scopes...)
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

// Stations lists the demo stations: the permission domains the station-scoped
// resources and RPC methods are served under.
func (s *Server) Stations() http.HandlerFunc {
	type response struct {
		Stations []accesstypes.Domain `json:"stations"`
	}

	return httpio.Log(func(w http.ResponseWriter, _ *http.Request) error {
		return httpio.NewEncoder(w).Ok(response{Stations: stations.Domains()})
	})
}
