// Package app contains Lodestar's http handlers. Most handlers are generated; this file
// provides the application plumbing the generated code depends on, plus the hand-written
// served surface (middleware, the droids outlet's API-key authentication, and the two
// browser applications' static assets) the generated router.New composes around the
// generated API routes, with the console's and the portal's own routes mounted through
// router.AppHooks.
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
	"github.com/cccteam/ccc/resource/jobs"
	"github.com/cccteam/ccc/resource/live"
	"github.com/cccteam/ccc/resource/lodestar/pkg/auth"
	"github.com/cccteam/ccc/resource/lodestar/pkg/auth/crew"
	"github.com/cccteam/ccc/resource/lodestar/pkg/auth/members"
	"github.com/cccteam/ccc/resource/lodestar/pkg/computedresources"
	"github.com/cccteam/ccc/resource/lodestar/pkg/resources"
	"github.com/cccteam/ccc/resource/lodestar/pkg/rpc"
	"github.com/cccteam/ccc/resource/scheduled"
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
// since the browser's feed connects to them directly rather than through the API; and
// no page may frame the application
// (frame-ancestors 'none'; X-Frame-Options DENY says the same to browsers that predate
// it), so its pages cannot be overlaid or clickjacked.
//
// Demonstrates: live.pages.
func cspPolicy(liveOrigins []string) string {
	connect := strings.Join(append([]string{"'self'"}, liveOrigins...), " ")

	return "default-src 'self'; worker-src 'self'; connect-src " + connect + "; " +
		"style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; " +
		"font-src 'self' https://fonts.gstatic.com data:; img-src 'self' data:; " +
		"frame-ancestors 'none'"
}

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
	// AppVersion is the release this build was made from (APP_VERSION, dev where no
	// release built it): what the generated router checks a browser application's
	// X-Api-Version against.
	AppVersion() string
	ConsoleDist() string
	PortalDist() string
	// DroidsAPIKey is the bearer key the droids outlet's clients present.
	DroidsAPIKey() string
	// Live is the live service the generated handlers subscribe through and publish to
	// (LiveService), the feature flags follow and the permission engines signal
	// through: the Firestore service, or the in-memory fake in a test harness; every
	// application wires one.
	Live() live.Service
	// LiveOrigins are the origins the browser reaches the change feed at, which the
	// content security policy names in connect-src: the Firestore emulator in
	// development, Firebase's hosts in production.
	LiveOrigins() []string
	// UserManagement is the crew engine's user-management handlers (access.Handlers),
	// which the console's role-membership routes delegate to behind the App's own
	// permission checks; nil where those routes are never mounted (the test router's
	// suites), since mounting them draws on it.
	UserManagement() access.Handlers
	// Scheduler is the guard the scheduled routes sit behind (resource/scheduled): the
	// invoker identity the stack names in APP_SCHEDULER_INVOKER, whose Google-signed
	// tokens alone are admitted. A guard with no invoker, or none at all, refuses every
	// scheduled call.
	Scheduler() *scheduled.Guard
	// Jobs starts the application's job process (resource/jobs): an execution of the
	// Cloud Run job deployed with this revision, named from the template job the stack
	// sets in APP_JOBS_TEMPLATE and the version the image bakes in.
	// The scheduled CleanUpFiles method starts the orphaned-file cleanup through it, so
	// the service starts its job and the cleanup never runs inside a request. Where no
	// job is configured (development, a pull-request stack) every start is refused.
	Jobs() jobs.Starter
	// TenantRoster is the application's tenant roster: the Sectors table's keys, built by
	// the generated NewSectorRoster over the resource client and started by the
	// configuration, so it is loaded before the App is built and keeps up through the
	// live service's tenants signal. The generated DomainGuard asks it before every
	// sector-scoped request runs, the generated Sector write paths add and remove sectors
	// in it after their commit, and a session's sector list is this roster filtered by
	// where the session holds a grant. A test harness builds one with Add alone.
	TenantRoster() *resource.TenantRoster
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
	version        string
	// consoleApp and portalApp serve the two built browser applications
	// (resource.BrowserApp): the console under /console and the portal under /portal, each
	// from the directory the configuration names, with the resource package's cache rule
	// and deep-link rewrite. The generated router's asset and deep-link handlers below
	// delegate to them.
	consoleApp     *resource.BrowserApp
	portalApp      *resource.BrowserApp
	droidsAPIKey   string
	scheduler      *scheduled.Guard
	tenants        *resource.TenantRoster
	rpcClient      *rpc.Client
	computedClient *computedresources.Client
	live           live.Service
	management     access.Handlers
	// features is the application's copy of its feature flags, read through the
	// configuration's database client when the App is built; featuresErr is why it
	// could not be, which Start reports.
	features    *resource.FeatureSet
	featuresErr error
	csp         string
}

// New constructs an App from its dependencies.
func New(cfg Configurer) *App {
	// The fleet runs one operations clock: the bare word local in a temporal grant
	// condition (timeOfDay(now, local), dayOfWeek(now, local)) resolves to headquarters
	// time, the Dockmaster's day shift.
	resource.SetLocalZone(operationsClock)

	engine := cfg.Access()
	// The file stores are the resource client's: the generated frames read each off it,
	// and the RPC client takes the Documents store, which its document methods read
	// back from.
	resourceClient := cfg.ResourceClient()
	a := &App{
		access:         engine,
		engines:        map[string]access.Controller{},
		resourceClient: resourceClient,
		cursorKey:      cfg.CursorKey(),
		validate:       cfg.Validator(),
		logExporter:    cfg.LogExporter(),
		version:        cfg.AppVersion(),
		consoleApp:     resource.NewBrowserApp(cfg.ConsoleDist(), "/console"),
		portalApp:      resource.NewBrowserApp(cfg.PortalDist(), "/portal"),
		droidsAPIKey:   cfg.DroidsAPIKey(),
		scheduler:      cfg.Scheduler(),
		tenants:        cfg.TenantRoster(),
		rpcClient:      rpc.NewClient(func(role accesstypes.Role) resource.RolePermissions { return engine.ForRole(role) }, resourceClient.FileStore(resource.StoreNameFor[resources.Documents]()), cfg.Jobs()),
		computedClient: computedresources.NewClient(),
		live:           cfg.Live(),
		management:     cfg.UserManagement(),
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

// ServerVersion is the release this server was built from, the configuration's
// APP_VERSION: what the generated router's session outlets check a browser
// application's X-Api-Version against. A development build reports dev, which checks
// nothing.
func (a *App) ServerVersion() string {
	return a.version
}

// SecurityHeaders is a middleware that sets security-related headers on the response,
// the content security policy among them (cspPolicy), whose connect-src names the live
// change feed's origins beside the application.
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

// DeepLink rewrites the console's Angular routes to its entry document under /console/, so
// a bookmarked or reloaded route loads the single-page application: a request whose last
// segment has no extension, matrix parameters removed, is served /console/index.html, and
// a path with an extension is a file or a 404 (resource.BrowserApp).
func (a *App) DeepLink(next http.Handler) http.Handler {
	return a.consoleApp.DeepLink(next)
}

// Assets serves the console's built Angular application under /console/ with the resource
// package's cache rule: a file whose name carries the build hash answers public,
// max-age=31536000, immutable, and every other file (the entry document, ngsw.json, the
// worker script, the web manifest, the favicon, the icons) answers no-cache with a strong
// ETag, so the service worker and any cache between keep a build's hashed files for good
// and revalidate the rest, an unchanged file answering 304; the web manifest answers its
// own media type, and a missing file, a path outside the mount or a directory is 404.
//
// Demonstrates: webapp.cache-rule.
func (a *App) Assets() http.HandlerFunc {
	return a.consoleApp.Assets()
}

// PortalDeepLink rewrites the portal's Angular routes to its entry document under /portal/,
// as DeepLink does for the console.
func (a *App) PortalDeepLink(next http.Handler) http.Handler {
	return a.portalApp.DeepLink(next)
}

// PortalAssets serves the portal's built Angular application under /portal/ with the same
// cache rule as Assets.
//
// Demonstrates: webapp.cache-rule.
func (a *App) PortalAssets() http.HandlerFunc {
	return a.portalApp.Assets()
}

// DroidsAuth authenticates the droids outlet's machine clients: the request must carry
// the configured API key as "Authorization: Bearer <key>". A valid key binds the request
// to the droid service identity the way the session middleware binds a browser request
// to its user, so the handlers behind it run the same fail-closed permission checks. An
// empty configured key disables the surface.
//
// Demonstrates: outlet.api-key, outlet.exclusive.
func (a *App) DroidsAuth(next http.Handler) http.Handler {
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

// SchedulerAuth admits Cloud Scheduler's calls to the scheduled routes: a token Google
// signed for the route's URL whose verified email is the invoker identity the stack names
// in APP_SCHEDULER_INVOKER. Any other call answers 401, its reason logged, and with no
// invoker configured (development, a pull-request stack) every call does.
//
// Demonstrates: @schedule.
func (a *App) SchedulerAuth(next http.Handler) http.Handler {
	return a.scheduler.Middleware(next)
}

// UserPermissions returns the permission checker for a request, composed from the
// session's principal: the engine of the auth the request came through, bound to the
// user for an ordinary or impersonated-user session, bound to the role for a session
// established as a role, and attenuated by the session's permission mask. The sectors
// the session lists are the tenant roster filtered by where the checker holds a grant,
// so the star chart and the sector guard can never disagree, and a sector charted a
// moment ago lights for everyone whose role is held in every sector.
//
// Demonstrates: impersonation.session-permissions.
func (a *App) UserPermissions(r *http.Request) resource.UserPermissions {
	engine := a.engine(r.Context())

	return resource.SessionPermissions(r.Context(), engine.ForUser, engine.ForRole, a.tenants.Domains)
}

// TenantRoster is the application's tenant roster (resource.TenantRoster): the sectors
// every instance serves, which the generated DomainGuard and the consolidated dispatcher
// ask before a sector-scoped request runs (Has: no read, no wait) and the generated
// Sector write paths add to and remove from after their commit. The configuration builds
// it with the generated NewSectorRoster and starts it over the live service's signals,
// so a sector charted on any instance is served here at the next request.
//
// Demonstrates: tenancy.run-time-tenant.
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

// CursorKey returns the key that seals the cursors the generated list handlers issue:
// one key per application, derived from the cookie key, so a cursor carries nothing
// readable and never survives a rotation the sessions do not.
//
// Demonstrates: paging.cursor, paging.sealed-cursor, paging.link-header, paging.total-count, paging.offset-refused, paging.readability-rule, paging.survives-writes, paging.limit-all, paging.unselected-sort-key.
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

// LiveService is the live service the generated handlers draw on (resource/live): the
// list and read handlers register a subscribing request's interest in it before the
// query, the mutations publish their committed rows through it, and the live routes
// renew, unsubscribe and mint the browser's identity against it. Every application
// wires one; it is also the channel the feature flags follow and the permission engines
// signal through.
func (a *App) LiveService() live.Service {
	return a.live
}

// Start begins the App's background work and ends it when ctx does: the feature flags
// are followed, so a flip on any instance (signaled on the features kind of the live
// service's signals document) or the library's backstop reread brings this instance's
// copy current. A copy that could not be read when the App was built is reported here, as
// the start-up failure it is: the schema is behind the release.
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
