// Package app contains the http handlers for the console site. Most handlers are generated; this
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
	"github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/sites/pkg/auth/staff"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/live"
	"github.com/cccteam/logger"
	"github.com/cccteam/session"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-playground/errors/v5"
	"github.com/go-playground/validator/v10"
	"github.com/jtwatson/spaassets"
)

const (
	hstsPolicy          = "max-age=31536000; includeSubDomains"
	referrerPolicy      = "no-referrer"
	xContentTypeOptions = "nosniff"
	xFrameOptions       = "DENY"
)

// cspPolicy writes the content security policy: the console's own assets plus the
// Google Fonts hosts index.html links for the Roboto and Material Icons faces;
// connections to the application itself and to the origins the live change feed is
// reached at (the Firestore emulator in development, Firebase's hosts in production,
// nothing more when no live pages are served), since the browser's feed connects to
// them directly rather than through the API; and no page may frame the application
// (frame-ancestors 'none'; X-Frame-Options DENY says the same to browsers that predate
// it), so its pages cannot be overlaid or clickjacked.
func cspPolicy(liveOrigins []string) string {
	connect := strings.Join(append([]string{"'self'"}, liveOrigins...), " ")

	return "default-src 'self'; worker-src 'self'; connect-src " + connect + "; " +
		"style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; " +
		"font-src 'self' https://fonts.gstatic.com data:; img-src 'self' data:; " +
		"frame-ancestors 'none'"
}

// Configurer carries the dependencies for an App: the database client, the permission
// engine the handlers check against, the tenancy seam, the session manager, the request
// validator, the log exporter, and where the site's built bundle lives. Tenant
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
	// Domains lists the application's tenants, the roster a session's tenant list is
	// filtered from: the permission engine holds no tenant list, so the picker's question
	// is this roster asked, tenant by tenant, whether the session holds a grant there.
	Domains(ctx context.Context) ([]accesstypes.Domain, error)
	// Staff returns the auth this surface binds to: the staff auth, whose session manager
	// the App composes its login and session handlers from.
	Staff() *staff.Auth
	// Live is the live service the generated handlers subscribe through and publish to
	// (LiveService): the Firestore service when a Firestore database or the emulator is
	// configured, nil otherwise, which serves no live pages. The sites share it as they
	// share the data level.
	Live() live.Service
	// LiveOrigins are the origins the browser reaches the change feed at, which the
	// content security policy names in connect-src: the Firestore emulator in
	// development, Firebase's hosts in production, none when no live pages are served.
	LiveOrigins() []string
	Validator() *validator.Validate
	LogExporter() logger.Exporter
	Dist() string
}

// App implements the http handlers for the console site, most of which are generated. It owns no
// router: whoever serves it composes one at the edge (main composes router.New, test
// suites compose router.NewTestRouter).
type App struct {
	access access.Controller
	*session.PasswordAuth[session.NoCustomData, session.NoCustomData]
	resourceClient resource.Client
	cursorKey      *resource.CursorKey
	domainVisible  func(ctx context.Context, user accesstypes.User, domain accesstypes.Domain) (bool, error)
	domains        resource.DomainRoster
	validate       *validator.Validate
	logExporter    logger.Exporter
	dist           string
	live           live.Service
	// features is the application's copy of its feature flags, read through the
	// configuration's database client when the App is built; featuresErr is why it
	// could not be, which Start reports.
	features    *resource.FeatureSet
	featuresErr error
	csp         string
}

// New constructs an App from its dependencies.
func New(cfg Configurer) *App {
	a := &App{
		access:         cfg.Access(),
		resourceClient: cfg.ResourceClient(),
		cursorKey:      cfg.CursorKey(),
		domainVisible:  cfg.DomainVisible,
		domains:        cfg.Domains,
		validate:       cfg.Validator(),
		logExporter:    cfg.LogExporter(),
		dist:           cfg.Dist(),
		live:           cfg.Live(),
		csp:            cspPolicy(cfg.LiveOrigins()),
	}
	// The feature flags: the application's copy of the FeatureFlags table, read as the
	// App is built so the generated handlers answer from it at once; Start keeps it
	// current. A copy that could not be read leaves every gated target off (the set is
	// nil, which fails closed) until Start reports the failure.
	a.features, a.featuresErr = resource.LoadFeatures(context.Background(), a.resourceClient)
	if a.featuresErr != nil {
		a.features = nil
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
		w.Header().Set("Content-Security-Policy", a.csp)
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

// DeepLink rewrites the site's Angular routes to its entry point so bookmarked
// frontend routes load the single-page application.
func (a *App) DeepLink(next http.Handler) http.Handler {
	return spaassets.DeepLink(next, "/")
}

// Assets serves the site's built Angular application.
func (a *App) Assets() http.HandlerFunc {
	return serveSPA(http.FileServer(http.Dir(a.dist)))
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
// listing the tenants of the application's roster where the principal holds a grant,
// and attenuated by the session's permission mask.
func (a *App) UserPermissions(r *http.Request) resource.UserPermissions {
	return resource.SessionPermissions(r.Context(), a.access.ForUser, a.access.ForRole, a.domains)
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

// LiveService is the live service the generated handlers draw on (resource/live): the
// list and read handlers register a subscribing request's interest in it before the
// query, the mutations publish their committed rows through it, and the generated live
// routes renew, unsubscribe and mint the browser's identity against it. Nil when no
// Firestore database and no emulator is configured: the site then serves no live pages
// and refuses a request carrying X-Subscribe.
func (a *App) LiveService() live.Service {
	return a.live
}

// Start begins the App's background work and ends it when ctx does: the feature flags
// are followed, so a flip on any instance (signaled through the live service's
// application topic when one is wired) or the library's five-minute backstop reread
// brings this instance's copy current. A copy that could not be read when the App was
// built is reported here, as the start-up failure it is: the schema is behind the
// release.
func (a *App) Start(ctx context.Context) error {
	if a.featuresErr != nil {
		return errors.Wrap(a.featuresErr, "resource.LoadFeatures()")
	}
	if err := a.features.Follow(ctx, a.live); err != nil {
		return errors.Wrap(err, "resource.FeatureSet.Follow()")
	}

	return nil
}

// FeatureSet is the application's copy of its feature flags: the generated route
// registration answers a gated route 404 while its flag is off, the generated decoders
// answer a gated field as unknown, the permission digest leaves gated targets out, and
// the generated features route lists what is on. The copy is read when the App is
// built and kept current by Start.
func (a *App) FeatureSet() *resource.FeatureSet {
	return a.features
}
