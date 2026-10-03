// Package app contains the application's http handlers. Most handlers are generated; this
// file provides the application plumbing the generated code depends on, plus the
// hand-written served surface (middleware, the machines outlet's API-key
// authentication, and static assets) the generated router.New composes around the generated API
// routes.
package app

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/cccteam/access"
	"github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/outlets/pkg/auth"
	"github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/outlets/pkg/auth/members"
	"github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/outlets/pkg/auth/staff"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/live"
	"github.com/cccteam/httpio"
	"github.com/cccteam/logger"
	"github.com/cccteam/session"
	"github.com/cccteam/session/sessioninfo"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-playground/errors/v5"
	"github.com/go-playground/validator/v10"
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
// reached at (the Firestore emulator in development, Firebase's hosts in production),
// since the browser's feed connects to them directly rather than through the API; and no page may frame the application
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
// engine the handlers check against, the tenant roster, the session manager, the request
// validator, the log exporter, and where the console's built bundle lives. Tenant
// existence is concealed (generation.WithConcealedDomains): the generated DomainGuard
// asks the roster whether the tenant exists and the session's permissions whether the
// caller holds at least one grant in it, in the store of the auth the request came
// through, and answers both "no" alike, so a prober cannot confirm a tenant exists from
// the rejection shape.
type Configurer interface {
	ResourceClient() resource.Client
	// CursorKey seals the cursors every paged list issues: one key per application,
	// derived from the cookie key, so a cursor is never a cookie.
	CursorKey() *resource.CursorKey
	Access() access.Controller
	// TenantRoster is the application's tenant roster: the tenants as the running
	// application knows them, built by the data level with the generated constructor
	// (NewTenantRoster) and kept current on every instance without a restart. The
	// generated DomainGuard asks it whether a domain is a tenant, and a session's tenant
	// list is its Domains filtered by where the principal holds a grant in the store of
	// the auth it came through, since a permission engine holds no tenant list.
	TenantRoster() *resource.TenantRoster
	// Staff returns the auth the console binds to: the staff auth, whose session manager
	// the App composes the console's login and session handlers from.
	Staff() *staff.Auth
	// Members returns the auth the portal outlet binds to: the members auth, whose
	// session manager the App hands the router for the portal's session group.
	Members() *members.Auth
	// Live is the live service the generated handlers subscribe through and publish to
	// (LiveService) and the feature flags follow (Start): the Firestore service over the
	// database or the emulator the data level opened, required in every application. One
	// service serves both browser outlets; the machines outlet refuses a subscribing
	// request.
	Live() live.Service
	// LiveOrigins are the origins the browser reaches the change feed at, which the
	// content security policy names in connect-src: the Firestore emulator in
	// development, Firebase's hosts in production.
	LiveOrigins() []string
	Validator() *validator.Validate
	LogExporter() logger.Exporter
	ConsoleDist() string
	PortalDist() string
	MachinesAPIKey() string
}

// machineUser is the service identity machines-outlet requests act as once the API key
// authenticates: the bootstrap grants it roles like any other user, so the machine
// surface goes through the same fail-closed permission checks as the browser.
const machineUser = "machines"

// App implements the application's http handlers, most of which are generated. It owns no
// router: whoever serves it composes one at the edge (main composes router.New, test
// suites compose router.NewTestRouter).
type App struct {
	// access is the default auth's engine, the staff auth's; engines holds every auth's
	// by name, for requests a session group bound to another auth.
	access  access.Controller
	engines map[string]access.Controller
	*session.PasswordAuth[session.NoCustomData, session.NoCustomData]
	portal         *session.OIDCAzure[session.NoCustomData, session.NoCustomData]
	resourceClient resource.Client
	cursorKey      *resource.CursorKey
	tenants        *resource.TenantRoster
	validate       *validator.Validate
	logExporter    logger.Exporter
	// consoleApp and portalApp serve the two built bundles under /console and /portal: the
	// resource package's served browser app holds the deep-link rewrite, the prefix strip,
	// and the two cache classes a service worker needs (hashed files immutable, everything
	// else revalidated).
	consoleApp     *resource.BrowserApp
	portalApp      *resource.BrowserApp
	machinesAPIKey string
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
		engines:        map[string]access.Controller{},
		resourceClient: cfg.ResourceClient(),
		cursorKey:      cfg.CursorKey(),
		tenants:        cfg.TenantRoster(),
		validate:       cfg.Validator(),
		logExporter:    cfg.LogExporter(),
		consoleApp:     resource.NewBrowserApp(cfg.ConsoleDist(), "/console"),
		portalApp:      resource.NewBrowserApp(cfg.PortalDist(), "/portal"),
		machinesAPIKey: cfg.MachinesAPIKey(),
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
	if staffAuth := cfg.Staff(); staffAuth != nil {
		a.PasswordAuth = staffAuth.Session()
	}
	if membersAuth := cfg.Members(); membersAuth != nil {
		a.portal = membersAuth.Session()
		a.engines[members.Name] = membersAuth.Access()
	}

	return a
}

// BindAuth is the middleware a session group carries to say which auth authenticated its
// requests, so the permission checks and the tenant visibility of everything behind it
// answer from that auth's store. A group that binds nothing gets the default auth.
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
func (a *App) Portal() session.OIDCAzureHandlers {
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

// DeepLink rewrites the console's Angular routes under /console/ to its entry document so
// bookmarked frontend routes load the single-page application: a request whose last
// segment has no extension is served index.html, and a path with an extension is a file
// or a 404.
func (a *App) DeepLink(next http.Handler) http.Handler {
	return a.consoleApp.DeepLink(next)
}

// Assets serves the console's built Angular application: a hashed file is immutable for
// a year, every other file (the entry document, the worker files, the manifest, the icons)
// is revalidated on each use, which is what the service worker needs under any cache.
func (a *App) Assets() http.HandlerFunc {
	return a.consoleApp.Assets()
}

// PortalDeepLink rewrites the portal's Angular routes under /portal/ to its entry document so
// bookmarked frontend routes load the single-page application: a request whose last
// segment has no extension is served index.html, and a path with an extension is a file
// or a 404.
func (a *App) PortalDeepLink(next http.Handler) http.Handler {
	return a.portalApp.DeepLink(next)
}

// PortalAssets serves the portal's built Angular application: a hashed file is immutable for
// a year, every other file (the entry document, the worker files, the manifest, the icons)
// is revalidated on each use, which is what the service worker needs under any cache.
func (a *App) PortalAssets() http.HandlerFunc {
	return a.portalApp.Assets()
}

// MachinesAuth authenticates the machines outlet's clients: the request must carry the
// configured API key as "Authorization: Bearer <key>". A valid key binds the request
// to the machine service identity the way the session middleware binds a browser
// request to its user, so the handlers behind it run the same fail-closed permission
// checks. An empty configured key disables the surface.
func (a *App) MachinesAuth(next http.Handler) http.Handler {
	return httpio.Log(func(w http.ResponseWriter, r *http.Request) error {
		key, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || a.machinesAPIKey == "" ||
			subtle.ConstantTimeCompare([]byte(key), []byte(a.machinesAPIKey)) != 1 {
			return httpio.NewEncoder(w).ClientMessage(r.Context(), httpio.NewUnauthorizedMessage("a valid machines API key is required"))
		}

		ctx := context.WithValue(r.Context(), sessioninfo.CtxSessionInfo, &sessioninfo.SessionData{
			SessionInfo: &sessioninfo.SessionInfo{Username: machineUser},
		})
		next.ServeHTTP(w, r.WithContext(ctx))

		return nil
	})
}

// UserPermissions returns the permission checker for a request, composed from the
// session's principal: the engine of the auth the request came through, bound to the
// user for an ordinary or impersonated-user session, bound to the role for a session
// established as a role, listing the tenants of the application's roster where the
// principal holds a grant in that engine, and attenuated by the session's permission
// mask.
func (a *App) UserPermissions(r *http.Request) resource.UserPermissions {
	engine := a.engine(r.Context())

	return resource.SessionPermissions(r.Context(), engine.ForUser, engine.ForRole, a.tenants.Domains)
}

// TenantRoster returns the application's tenant roster, the generated contract's
// accessor: the generated DomainGuard and the consolidated dispatcher ask it whether a
// domain is a tenant before a tenant-scoped request runs, and the Tenant record's
// generated write paths add and remove tenants in it after their commit.
func (a *App) TenantRoster() *resource.TenantRoster {
	return a.tenants
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
// routes on the console and the portal renew, unsubscribe and mint the browser's
// identity against it; the machines outlet refuses a request carrying X-Subscribe.
// Every application wires one; the generated handlers assume it.
func (a *App) LiveService() live.Service {
	return a.live
}

// Start begins the App's background work and ends it when ctx does: the feature flags
// are followed, so a flip on any instance (signaled through the live service's signals
// document, the features kind) or the library's five-minute backstop reread brings
// this instance's copy current. A copy that could not be read when the App was
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
