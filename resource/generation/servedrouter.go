package generation

import (
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/cccteam/ccc/resource/scheduled"
	"github.com/go-playground/errors/v5"
	"golang.org/x/mod/semver"
)

// Names the session surface and the generated code share.
const (
	// The session's own routes under an outlet's prefix: the session (read and ended),
	// the method list the login page renders from, and the pending sign-in.
	userSessionSuffix    = "/user/session"
	userMethodsSuffix    = "/user/methods"
	userPendingSuffix    = "/user/pending"
	userInvitationSuffix = "/user/invitation"
	userEmailSuffix      = "/user/email"
	userSignUpSuffix     = "/user/sign-up"
	// The token page's routes under an issuing session outlet's prefix, and the device
	// flow's public endpoints under a machine outlet's.
	userTokensSuffix  = "/user/tokens"
	userDeviceSuffix  = "/user/device"
	oauthDeviceSuffix = "/oauth/device/authorize"
	oauthTokenSuffix  = "/oauth/token"
	// The pending sign-in handlers on a session outlet's surface (session.PendingHandlers).
	pendingStatusHandler  = "PendingStatus"
	pendingConfirmHandler = "PendingConfirmWithPassword"
	pendingCancelHandler  = "PendingCancel"
	// methodsHandler tells the login page what the outlet offers (session.Auth.Methods).
	methodsHandler = "Methods"
	// The invitation, email address and sign-up handlers on a session outlet's surface
	// (session.InvitationHandlers, EmailHandlers, SignUpHandlers).
	invitationOpenHandler   = "InvitationOpen"
	invitationAcceptHandler = "InvitationAccept"
	invitationSignUpHandler = "InvitationSignUp"
	emailAddHandler         = "EmailAdd"
	emailVerifyHandler      = "EmailVerify"
	signUpStartHandler      = "SignUpStart"
	signUpOpenHandler       = "SignUpOpen"
	signUpCompleteHandler   = "SignUpComplete"
	// The device flow's and the token page's handlers on an issuing session outlet's
	// surface (session.DeviceHandlers, ApplicationTokenHandlers).
	deviceAuthorizeHandler = "DeviceAuthorize"
	deviceTokenHandler     = "DeviceToken"
	deviceLookupHandler    = "DeviceLookup"
	deviceApproveHandler   = "DeviceApprove"
	deviceDenyHandler      = "DeviceDeny"
	tokensListHandler      = "TokensList"
	tokensCreateHandler    = "TokensCreate"
	tokensRevokeHandler    = "TokensRevoke"
	// validateTokenMethod is the one method of a machine outlet's token surface.
	validateTokenMethod = "ValidateToken"
)

// servedMethod is one sign-in method of a session outlet as the router mounts it and
// the login page is told of it.
type servedMethod struct {
	// Name is the registration's name on the Auth, Label what the login page shows,
	// Kind the method behind the registration (password, google, azure, workos).
	Name  string
	Label string
	Kind  string
	// Accessor is the Pascal-cased name the outlet's session surface prefixes the
	// method's handlers with (Password, Google, LakesideEntra).
	Accessor string
	// LoginPath is where the method's login is mounted: what the page is told.
	LoginPath string
	// Handlers are the method's handlers on the session surface, in order:
	// <Accessor>Login, and for a redirect method <Accessor>Callback, and for Entra
	// <Accessor>FrontChannelLogout.
	Handlers []string
	// Description says what the handlers are, for the surface's doc comment.
	Description string
}

// servedPending are the pending sign-in routes of a session outlet: what the login
// page is told beside the methods.
type servedPending struct {
	Status  string
	Confirm string
	Cancel  string
}

// servedInvitation are the invitation routes of a session outlet: where the login page
// opens an invitation it was sent to, where a signed-in person accepts it, and where an
// invited stranger creates an account with a password (empty without a password
// method).
type servedInvitation struct {
	Open   string
	Accept string
	SignUp string
}

// servedEmail are the email address routes of a session outlet: where a signed-in
// person adds an address and where they verify it from the mailed link.
type servedEmail struct {
	Add    string
	Verify string
}

// servedSignUp are the sign-up routes of a session outlet whose password method
// declares open sign-up: start from an address, open the mailed link, complete with a
// password.
type servedSignUp struct {
	Start    string
	Open     string
	Complete string
}

// servedSessionHandler is one handler of the session surface beyond the methods' and
// the pending sign-in's: the invitation, email address and sign-up handlers.
type servedSessionHandler struct {
	Name        string
	Description string
}

// Hook fields the generated Hooks struct carries beside the per-outlet fields; an outlet
// whose Pascal-cased name would take one of them cannot be declared.
var reservedHookFields = []string{"Outermost", rootHookField}

// rootHookField is the Hooks field for the routes outside every outlet.
const rootHookField = "Root"

// validateRouterConfig checks the outlet declarations against GenerateRouter: without
// the option the router-describing outlet options are refused (they would be silently
// ignored), and with it every outlet says how it authenticates, the contradictions are
// rejected, and the browser applications' mount paths are distinct, outside every
// outlet's prefix, and at / only when there is one browser application.
func (r *resourceGenerator) validateRouterConfig() error {
	outlets := r.allOutlets()
	if !r.genRouter {
		return r.validateHandWrittenRouter(outlets)
	}

	sessionAuths := make(map[string]bool)
	for _, o := range outlets {
		if o.auth != nil {
			sessionAuths[o.auth.importPath] = true
		}
	}
	for _, o := range outlets {
		if err := validateOutletAuthentication(o, sessionAuths); err != nil {
			return err
		}
		if err := validateReservedNames(o); err != nil {
			return err
		}
	}
	if err := validateMountedPrefixes(outlets, r.mountedRoutes); err != nil {
		return err
	}

	return validateWebAppMounts(outlets)
}

// validateHandWrittenRouter refuses, under a hand-written router, every declaration that
// describes the generated one: an outlet's authentication, browser application, release
// and words, and the program's request log and mounted prefixes.
func (r *resourceGenerator) validateHandWrittenRouter(outlets []*routerOutlet) error {
	for _, o := range outlets {
		var declared string
		switch {
		case o.auth != nil:
			declared = "Auth"
		case o.tokenAuth != "":
			declared = "Tokens"
		case o.apiKey:
			declared = "APIKey"
		case o.webApp != "":
			declared = "WebApp"
		case o.declaredOldest:
			declared = "OldestAnswered"
		case o.requestLog.Declared():
			declared = "OutletRequestLog"
		case o.traces.Declared():
			declared = "OutletTraces"
		default:
			continue
		}

		return errors.Newf("outlet %q declares %s, which describes the generated router: declare GenerateRouter, or drop it and compose the outlet in a hand-written router (ServesSessions marks a session outlet there)", o.name, declared)
	}
	switch {
	case r.requestLog.Declared():
		return errors.Newf("WithRequestLog(%s) describes the generated router, which builds the request logger with the application default: declare GenerateRouter, or drop it and pass logger.DefaultPolicy to the request logger in a hand-written router", r.requestLog)
	case len(r.mountedRoutes) > 0:
		return errors.Newf("WithMountedRoutes(%q) describes the generated router, which builds the request logger and the tracer with the prefix: declare GenerateRouter, or drop it and pass logger.PolicyByPrefix to the request logger in a hand-written router", r.mountedRoutes[0].prefix)
	}

	return nil
}

// validateOutletAuthentication holds an outlet under the generated router to one way of
// authenticating: a session outlet declares Auth, a machine outlet declares Tokens naming
// an auth a session outlet declares (sessionAuths), and a machine outlet declares nothing
// of a browser surface's.
func validateOutletAuthentication(o *routerOutlet, sessionAuths map[string]bool) error {
	switch {
	case o.auth != nil && o.tokenAuth != "":
		return errors.Newf("outlet %q declares both Auth and Tokens: an outlet is a browser surface behind one auth or a machine surface taking an auth's tokens", o.name)
	case o.auth != nil && o.apiKey:
		return errors.Newf("outlet %q declares both Auth and APIKey: an outlet is a browser surface behind one auth or a machine surface taking an auth's tokens (Tokens)", o.name)
	case o.auth == nil && !o.apiKey:
		return errors.Newf("outlet %q declares neither Auth nor Tokens: under GenerateRouter every outlet says how it authenticates", o.name)
	case o.apiKey && o.tokenAuth == "":
		return errors.Newf("outlet %q declares APIKey, which is retired: a machine outlet declares Tokens(importPath), naming the auth package of a session outlet whose Auth issues its tokens, and the generated router carries the token check", o.name)
	case o.apiKey && !sessionAuths[o.tokenAuth]:
		return errors.Newf("outlet %q declares Tokens(%q), which no session outlet declares with Auth: a machine outlet's tokens are issued by the Auth of a browser outlet of the application, so declare that outlet's auth package", o.name, o.tokenAuth)
	case o.apiKey && o.declaredSessions:
		return errors.Newf("outlet %q declares Tokens and ServesSessions: a machine outlet serves no browser sessions", o.name)
	case o.apiKey && o.webApp != "":
		return errors.Newf("outlet %q declares Tokens and WebApp(%q): a machine outlet serves no browser application", o.name, o.webApp)
	case o.apiKey && o.declaredOldest:
		return errors.Newf("outlet %q declares Tokens and OldestAnswered(%q): a machine outlet's clients carry no release, so nothing is checked against one; the option belongs on a session outlet", o.name, o.oldestAnswered)
	}

	return nil
}

// validateMountedPrefixes checks the hand-mounted prefixes against the router's own
// paths: none sits at or under the scheduled prefix, which the router reserves, and none
// is an outlet's prefix itself, whose words are the outlet's to declare.
func validateMountedPrefixes(outlets []*routerOutlet, mounted []mountedRoutes) error {
	for _, m := range mounted {
		if underScheduledPrefix(strings.TrimSuffix(m.prefix, "/")) {
			return errors.Newf("WithMountedRoutes(%q) sits under %s, which the generated router reserves for the scheduled routes (@%s); a scheduled method declares its word on @%s", m.prefix, scheduled.Prefix, scheduleKeyword, scheduleKeyword)
		}
		for _, o := range outlets {
			if strings.TrimSuffix(m.prefix, "/") == "/"+o.prefix {
				return errors.Newf("WithMountedRoutes(%q) is the %s outlet's prefix; declare the outlet's words with OutletRequestLog and OutletTraces", m.prefix, o.name)
			}
		}
	}

	return nil
}

// validateReservedNames refuses an outlet that would take a name or a path the generated
// router keeps for itself: a Hooks field, a Handlers method, or the scheduled prefix.
func validateReservedNames(o *routerOutlet) error {
	if field := caser.ToPascal(o.name); slices.Contains(reservedHookFields, field) {
		return errors.Newf("outlet %q would take the Hooks field %s, which the generated router reserves; choose another name", o.name, field)
	}
	if underScheduledPrefix("/" + o.prefix) {
		return errors.Newf("outlet %q has the route prefix /%s, under %s, which the generated router reserves for the scheduled routes (@%s); choose another prefix", o.name, o.prefix, scheduled.Prefix, scheduleKeyword)
	}
	if o.webApp != "" && underScheduledPrefix(o.webApp) {
		return errors.Newf("outlet %q declares WebApp(%q), under %s, which the generated router reserves for the scheduled routes (@%s); mount the application elsewhere", o.name, o.webApp, scheduled.Prefix, scheduleKeyword)
	}

	return nil
}

// underScheduledPrefix reports whether a path is the scheduled prefix or sits under it.
func underScheduledPrefix(p string) bool {
	return p == scheduled.Prefix || strings.HasPrefix(p, scheduled.Prefix+"/")
}

// validateWebAppMounts checks the browser applications' mount paths: every application
// has its own, none sits under an outlet's route prefix, and one at / is the only one.
// An installed browser application's scope is every URL under its start, so with two
// applications one at / would own the origin: the one under a prefix would never get its
// own install prompt, and its notifications and links would be attributed to the
// application at /. With several applications none is mounted at /, and the generated
// router redirects the root to the default outlet's.
func validateWebAppMounts(outlets []*routerOutlet) error {
	mounts := make(map[string]string, len(outlets))
	for _, o := range outlets {
		if o.webApp == "" {
			continue
		}
		if prior, ok := mounts[o.webApp]; ok {
			return errors.Newf("outlet %q declares WebApp(%q), which outlet %q already serves: every browser application has its own mount path", o.name, o.webApp, prior)
		}
		mounts[o.webApp] = o.name
		for _, other := range outlets {
			prefixPath := "/" + other.prefix
			if o.webApp == prefixPath || strings.HasPrefix(o.webApp, prefixPath+"/") {
				return errors.Newf("outlet %q declares WebApp(%q), which sits under outlet %q's route prefix /%s: a browser application is mounted beside the API prefixes, never under one", o.name, o.webApp, other.name, other.prefix)
			}
		}
	}

	root, ok := mounts["/"]
	if !ok || len(mounts) == 1 {
		return nil
	}
	for _, o := range outlets {
		if o.webApp == "" || o.webApp == "/" {
			continue
		}

		return errors.Newf(`outlet %q declares WebApp("/") beside outlet %q's WebApp(%q): an installed browser application's scope is every URL under its start, so the application at / owns the origin, the one under %s never gets its own install prompt, and its notifications and links are attributed to the application at /; with two browser applications none is mounted at /, so mount the %s outlet's application under a path such as /console`, root, o.name, o.webApp, o.webApp, root)
	}

	return nil
}

// servedRouterData feeds the served-router templates: the outlets in declaration order
// with their chains, the browser applications longest mount first, and the isolation
// cases the generated test re-proves through New.
type servedRouterData struct {
	Source  string
	Package string
	// AuthImports are the auth packages the file imports for BindAuth(<pkg>.Name),
	// sorted; empty unless more than one session auth is declared.
	AuthImports []string
	// MultiAuth emits BindAuth: more than one distinct session auth is declared.
	MultiAuth bool
	// Outlets is every outlet, the default first.
	Outlets []*servedOutlet
	// ExtraOutlets are the WithRouterOutlet outlets, whose Generated<Suffix>Handlers
	// the Handlers interface embeds beside GeneratedHandlers.
	ExtraOutlets   []*servedOutlet
	SessionOutlets []*servedOutlet
	MachineOutlets []*servedOutlet
	// WebApps are the browser applications, longest mount path first so "/" is last;
	// HasRootWebApp reports one mounted at "/", the catch-all.
	WebApps       []*servedWebApp
	HasRootWebApp bool
	// RootRedirect is where GET / alone redirects when browser applications are served
	// and none is mounted at /: the default outlet's mount path with a trailing slash,
	// or the first declared outlet's that serves one when the default serves none.
	// RootRedirectOutlet names that outlet. Both are empty with a root web app or no
	// web app.
	RootRedirect       string
	RootRedirectOutlet string
	// NotFoundPrefixes are the outlet prefixes as mounted paths ("/api/"), each given
	// a not-found handler so an unknown API path never falls to a browser application.
	NotFoundPrefixes []string
	// HandlersSummary lists what the Handlers interface carries, for its doc comment.
	HandlersSummary string
	// NegativeRouterTests are the outlet-isolation cases the routes test proves over
	// NewTestRouter, re-proven here through New.
	NegativeRouterTests []negativeRouterTest
	// FileStores are the file stores the generated code reads and writes, which New
	// requires on the resource client before the server starts; StoreImports are the
	// packages the named stores' types are declared in.
	FileStores   []servedFileStore
	StoreImports []string
	// ScheduledRoutes are the scheduled methods' routes, mounted under ScheduledPrefix
	// behind SchedulerAuth; none without a scheduled method, and then the router carries
	// no scheduled group.
	ScheduledRoutes []*scheduledRoute
	ScheduledPrefix string
	// RequestLog is the application default (WithRequestLog), which the router builds
	// the request logger with; undeclared leaves the logger's own, every entry written.
	RequestLog RequestLog
	// MountedRoutes are the hand-mounted prefixes (WithMountedRoutes) in prefix order,
	// which the request logger starts a request under with their word and the surface
	// table carries; RootMountedRoutes are the ones under no outlet's prefix, named
	// under the every-request line of the chain comment.
	MountedRoutes     []servedMountedRoutes
	RootMountedRoutes []servedMountedRoutes
	// Surfaces is the tracer's surface table: every outlet, hand-mounted prefix and route
	// with a trace setting, by the path it is mounted at, in path order.
	Surfaces []servedSurface
	// WordedNotFound are the outlets with a request log word, whose not-found handler
	// carries it, so an unknown path under the prefix is logged as the outlet's requests
	// are; NotFoundPrefixes carries the rest.
	WordedNotFound []*servedOutlet
	// DeclaresWords says any surface declares a word or a setting; UsesLogging says a
	// rendered word carries a floor, so the file imports the logging library; UsesStrings
	// says an outlet with a word has hand-mounted prefixes beneath it, so the file carries
	// the helper that keeps their word.
	DeclaresWords bool
	UsesLogging   bool
	UsesStrings   bool
	// RequestLogCases are the cases of the generated request log test: the application
	// default, each outlet's word, each route's own, each hand-mounted prefix, and a
	// handler that sets its own request's word, each on a quiet request and, where a
	// probe can fail, on a failed one.
	RequestLogCases []servedRequestLogCase
}

// servedMountedRoutes is one hand-mounted prefix with its words.
type servedMountedRoutes struct {
	Prefix     string
	RequestLog RequestLog
	Traces     Traces
	// Words says what the prefix declares, for the chain comment.
	Words string
}

// servedSurface is one entry of the tracer's surface table: the path as mounted and
// the setting.
type servedSurface struct {
	Pattern string
	Traces  Traces
}

// servedRouteWords is one route with words of its own, as the chain comment names it.
type servedRouteWords struct {
	Method string
	Path   string
	Words  string
}

// servedRequestLogCase is one case of the generated request log test: a probe the test
// registers through a hook (Hook names Root or the outlet's hook field) or a generated
// route (Hook empty), the status it answers, the word the probe sets on its own request
// when it does, and the decision the console exporter is expected to make: written,
// dropped, or empty when a sampled word leaves a quiet request to the draw.
type servedRequestLogCase struct {
	Name        string
	Hook        string
	Method      string
	MethodConst string
	Path        string
	Status      int
	SetWord     string
	Want        string
}

// The decisions the generated request log test expects.
const (
	decisionWritten = "written"
	decisionDropped = "dropped"
)

// requestLogDecision is the decision the console exporter makes for a request under the
// word that logged no line: written, dropped, or empty when a sampled word draws for a
// quiet request. An undeclared word is the logger's own, always.
func requestLogDecision(word RequestLog, failed bool) string {
	switch word.word {
	case requestLogOnEvent:
		if failed {
			return decisionWritten
		}

		return decisionDropped
	case requestLogSampled:
		if failed {
			return decisionWritten
		}

		return ""
	case requestLogNever:
		return decisionDropped
	case requestLogUndeclared, requestLogAlways:
		return decisionWritten
	default:
		return decisionWritten
	}
}

// servedFileStore is one file store as the router requires it at start and the router
// test wires it.
type servedFileStore struct {
	// NameExpr is the store's name as the router spells it: resource.DefaultStore, or
	// resource.StoreNameFor[pkg.T]() for the named store T.
	NameExpr string
	// OptionExpr wires a stub store under that name on the router test's mock client.
	OptionExpr string
}

// servedOutlet is one outlet as the served router composes it.
type servedOutlet struct {
	Name   string
	Suffix string
	Prefix string
	// HookField is the outlet's field on Hooks: Default for the default outlet, the
	// Pascal-cased name otherwise.
	HookField string
	// RoutesFunc is the generated registration function the group mounts.
	RoutesFunc string
	// NotFoundPrefix is the outlet's prefix as a mounted path ("/api/").
	NotFoundPrefix string
	// Machine marks a machine outlet (Tokens): TokenAuth is the import path of the auth
	// whose Auth issues its tokens, Issuer the session outlet declaring that auth,
	// IssuerExpr the expression the router reads the issuer's session surface from (h,
	// or h.Portal()), IssuerPackage the auth package's name for the prose,
	// TokensInterface the generated interface its token surface satisfies
	// (<HookField>Tokens, one ValidateToken), and DeviceRoutes the device flow's public
	// endpoints under its prefix, served by the issuer's surface.
	Machine         bool
	TokenAuth       string
	Issuer          *servedOutlet
	IssuerExpr      string
	IssuerPackage   string
	TokensInterface string
	DeviceRoutes    []servedRoute
	// IssuesTokens marks a session outlet whose Auth issues tokens for a machine outlet:
	// its surface carries the device flow's and the token page's handlers, the token
	// page's routes sit in its validated group, and ExtraHandlers are the handlers of
	// its surface that serve no route under its own prefix (the device flow's public
	// endpoints, mounted under the machine outlets').
	IssuesTokens  bool
	ExtraHandlers []string
	// The session outlet's auth: how the chain comment names it (AuthLabel), its import
	// path (AuthPath), the generated interface its session surface satisfies
	// (SessionInterface, <HookField>Session), and the package it binds to (empty when
	// the auth is not imported).
	AuthLabel        string
	AuthPath         string
	SessionInterface string
	AuthPackage      string
	// Getter returns the outlet's surface on Handlers for an additional outlet, its
	// session surface or its token surface; the default outlet's is embedded. Receiver
	// is the expression the router reads the surface from: h for the default outlet, a
	// local variable for a getter session outlet, the getter's call for a getter
	// machine outlet.
	Getter   string
	Receiver string
	// StubType is the recording stub the generated test builds for the outlet's
	// surface, StubField its field on the test's Handlers stub (empty for the embedded
	// default), and StubPrefix the name prefix it records under.
	StubType   string
	StubField  string
	StubPrefix string
	// Methods are the sign-in methods registered on the outlet's Auth, in declaration
	// order, and MethodsPath where the login page reads them; Pending are the pending
	// sign-in routes the page is told beside them.
	Methods     []servedMethod
	MethodsPath string
	Pending     servedPending
	// ChangePassword is where the password change is mounted, told to the login page
	// beside the methods; empty without a password method.
	ChangePassword string
	// Invitation, Email and SignUp are the invitation, email address and sign-up
	// routes the login page is told beside the methods: the invitation's sign-up with a
	// password method, SignUp when the password method declares open sign-up (nil
	// otherwise).
	Invitation servedInvitation
	Email      servedEmail
	SignUp     *servedSignUp
	// Routes are the session routes under the prefix, in mount order: OpenRoutes, the
	// ones outside the XSRF guard, GuardedRoutes, the POSTs behind it, then
	// ValidatedRoutes, the ones a signed-in person calls, inside the validated group
	// (the acceptance of an invitation, the address's add and verify).
	Routes          []servedRoute
	OpenRoutes      []servedRoute
	GuardedRoutes   []servedRoute
	ValidatedRoutes []servedRoute
	// SessionHandlers are the invitation, email address and sign-up handlers the
	// outlet's session interface names beyond the methods' and the pending sign-in's.
	SessionHandlers []servedSessionHandler
	WebApp          *servedWebApp
	// The session outlet's version check. OldestAnswered is the declaration as written
	// ("" for none, ThisRelease, or a release); OldestAnsweredExpr the Go expression
	// the router passes, empty for none; OldestAnsweredNote the chain comment's
	// parenthetical; AnswersSentence the check comment's clause. FileRoutes are the
	// outlet's stored-file routes, answered at any release, and Probes the releases
	// the generated test sends through the check.
	OldestAnswered     string
	OldestAnsweredExpr string
	OldestAnsweredNote string
	AnswersSentence    string
	FileRoutes         []servedFileRoute
	Probes             versionProbes
	// RequestLog is the outlet's request log word (OutletRequestLog), set on every
	// request in its group, and Traces its trace setting (OutletTraces), carried by the
	// surface table; Words says both for the chain comment. MountedBeneath are the
	// hand-mounted prefixes under the outlet's prefix, which keep their own word under
	// the outlet's, and RouteWords the outlet's routes with words of their own.
	RequestLog     RequestLog
	Traces         Traces
	Words          string
	MountedBeneath []servedMountedRoutes
	RouteWords     []servedRouteWords
}

// MountedPrefixes lists the prefixes beneath the outlet, for the helper that keeps their
// word.
func (o *servedOutlet) MountedPrefixes() []string {
	prefixes := make([]string, 0, len(o.MountedBeneath))
	for _, m := range o.MountedBeneath {
		prefixes = append(prefixes, m.Prefix)
	}

	return prefixes
}

// servedFileRoute is one stored-file route of a session outlet: the pattern the check
// exempts, and the URL and handler the generated test drives it with.
type servedFileRoute struct {
	Pattern string
	TestURL string
	Handler string
}

// versionProbes are the releases the generated test sends through one session outlet's
// check: Server is the release the stub reports; InRange the oldest answered release,
// or the first release where every release is answered; Below a release under the
// oldest answered, empty where every release is answered; Above a release past the
// server's.
type versionProbes struct {
	Server  string
	InRange string
	Below   string
	Above   string
}

// The probes where no release is written: the server the generated test reports when
// every release or the server's own is answered, the release past it, the first
// release, the release before the server's, and the release below any oldest answered.
const (
	probeServer          = "2.0.0"
	probeAboveServer     = "2.0.1"
	probeFirstRelease    = "0.0.1"
	probeBeforeServer    = "1.9.9"
	probeBelowAnyRelease = "0.0.0"
)

// versionProbesFor derives the probes from an outlet's oldest answered declaration.
func versionProbesFor(oldest string) versionProbes {
	switch oldest {
	case "":
		return versionProbes{Server: probeServer, InRange: probeFirstRelease, Above: probeAboveServer}
	case ThisRelease:
		return versionProbes{Server: probeServer, InRange: probeServer, Below: probeBeforeServer, Above: probeAboveServer}
	}

	canonical := oldest
	if !strings.HasPrefix(canonical, "v") {
		canonical = "v" + canonical
	}
	canonical = semver.Canonical(canonical)
	var major, minor int
	if _, err := fmt.Sscanf(semver.MajorMinor(canonical), "v%d.%d", &major, &minor); err != nil {
		panic(fmt.Sprintf("versionProbesFor(%q): %v; the option accepted no release", oldest, err))
	}
	probes := versionProbes{
		Server:  fmt.Sprintf("%d.%d.0", major, minor+1),
		InRange: oldest,
		Above:   fmt.Sprintf("%d.%d.1", major, minor+1),
	}
	if semver.Compare(canonical, "v"+probeBelowAnyRelease) > 0 {
		probes.Below = probeBelowAnyRelease
	}

	return probes
}

// versionCheckOf renders the outlet's declaration for the router: the expression the
// check receives, the chain comment's parenthetical, and the check comment's clause.
func versionCheckOf(oldest string) (expr, note, sentence string) {
	switch oldest {
	case "":
		return "", "", "every release up to the server's own is answered"
	case ThisRelease:
		return "resource.ThisRelease", " (oldest answered this release)", "only the server's own release is answered"
	default:
		return strconv.Quote(oldest), " (oldest answered " + oldest + ")", "releases from " + oldest + " up to the server's own are answered"
	}
}

// servedFileRoutesOf renders an outlet's stored-file routes for the check and the
// test, in path order.
func servedFileRoutesOf(routes []*generatedRoute) []servedFileRoute {
	files := make([]servedFileRoute, 0, len(routes))
	for _, route := range routes {
		files = append(files, servedFileRoute{Pattern: route.Path, TestURL: route.TestURL, Handler: route.HandlerFunc})
	}
	slices.SortFunc(files, func(a, b servedFileRoute) int {
		return strings.Compare(a.Pattern, b.Pattern)
	})

	return files
}

// servedRoute is one session route as mounted.
type servedRoute struct {
	// Method is the HTTP method name; MethodConst its net/http constant expression.
	Method      string
	MethodConst string
	// Path is the mounted path, Suffix the part under the outlet prefix.
	Path   string
	Suffix string
	// Handler is the session surface's method serving the route (PasswordLogin,
	// Authenticated); XSRF says the route sits behind the outlet's XSRF guard, as the
	// POSTs of a sign-in do; Validated that it sits inside the validated group too, as
	// the routes a signed-in person calls do.
	Handler   string
	XSRF      bool
	Validated bool
}

// servedWebApp is one browser application the router serves.
type servedWebApp struct {
	Outlet   string
	Mount    string
	DeepLink string
	Assets   string
}

// runServedRouterGeneration renders the served router and its test (GenerateRouter)
// beside the route tables, then the release file naming each outlet's oldest answered
// release. fileRoutes are each outlet's stored-file routes by outlet name, which the
// session outlets' version checks exempt; wordedRoutes are each outlet's routes with a
// request log word or a trace setting of their own, which the chain comment names, the
// surface table carries and the release file lists.
func (r *resourceGenerator) runServedRouterGeneration(outlets []*routerOutlet, negativeTests []negativeRouterTest, fileRoutes, wordedRoutes map[string][]*generatedRoute) error {
	begin := time.Now()
	data := r.servedRouterData(outlets, negativeTests, fileRoutes, wordedRoutes)

	destination := filepath.Join(r.router.Dir(), generatedGoFileName(servedRouterOutputName))
	if err := r.writeFormattedGoFile(destination, "servedRouterTemplate", servedRouterTemplate, data); err != nil {
		return errors.Wrap(err, "writeFormattedGoFile()")
	}
	log.Printf("Generated router file in %s: %s\n", time.Since(begin), destination)

	begin = time.Now()
	testDestination := filepath.Join(r.router.Dir(), generatedGoFileName(servedRouterTestOutputName))
	if err := r.writeFormattedGoFile(testDestination, "servedRouterTestTemplate", servedRouterTestTemplate, data); err != nil {
		return errors.Wrap(err, "writeFormattedGoFile()")
	}
	log.Printf("Generated router test file in %s: %s\n", time.Since(begin), testDestination)

	return r.runReleaseFileGeneration(outlets, fileRoutes, wordedRoutes)
}

// servedRouterData builds the template payload from the validated outlet declarations,
// each outlet's stored-file routes and each outlet's worded routes, both by outlet name.
func (r *resourceGenerator) servedRouterData(outlets []*routerOutlet, negativeTests []negativeRouterTest, fileRoutes, wordedRoutes map[string][]*generatedRoute) *servedRouterData {
	authPaths := make(map[string]struct{})
	for _, o := range outlets {
		if o.auth != nil {
			authPaths[o.auth.importPath] = struct{}{}
		}
	}
	multiAuth := len(authPaths) > 1

	data := &servedRouterData{
		Source:              r.resource.Dir(),
		Package:             r.router.Package(),
		MultiAuth:           multiAuth,
		NegativeRouterTests: negativeTests,
		ScheduledRoutes:     r.scheduledRoutes(),
		ScheduledPrefix:     scheduled.Prefix,
		RequestLog:          r.requestLog,
	}
	data.FileStores, data.StoreImports = r.servedFileStores()
	if multiAuth {
		for path := range authPaths {
			data.AuthImports = append(data.AuthImports, path)
		}
		sort.Strings(data.AuthImports)
	}

	for _, o := range outlets {
		so := servedOutletOf(o, multiAuth)
		if so.WebApp != nil {
			data.WebApps = append(data.WebApps, so.WebApp)
		}
		switch {
		case so.Machine:
			data.MachineOutlets = append(data.MachineOutlets, so)
		case o.auth != nil:
			so.FileRoutes = servedFileRoutesOf(fileRoutes[o.name])
			data.SessionOutlets = append(data.SessionOutlets, so)
		}
		servedOutletWords(data, so, wordedRoutes[o.name])
		data.Outlets = append(data.Outlets, so)
	}
	data.ExtraOutlets = data.Outlets[1:]
	linkTokenIssuers(data)
	r.servedMountedRoutes(data)
	slices.SortFunc(data.Surfaces, func(a, b servedSurface) int {
		return strings.Compare(a.Pattern, b.Pattern)
	})
	servedWordFlags(data)
	data.RequestLogCases = append(requestLogCasesOf(data), routeCasesOf(wordedRoutes, data.Outlets)...)
	if len(data.ScheduledRoutes) > 0 {
		// Under the scheduled prefix nothing else answers either: an unknown path is
		// 404, never a browser application's entry document.
		data.NotFoundPrefixes = append(data.NotFoundPrefixes, scheduled.Prefix+"/")
	}

	// Longer mount paths first, so "/" is the catch-all; the paths are distinct.
	sort.Slice(data.WebApps, func(i, j int) bool {
		return len(data.WebApps[i].Mount) > len(data.WebApps[j].Mount)
	})
	data.HasRootWebApp = slices.ContainsFunc(data.WebApps, func(w *servedWebApp) bool { return w.Mount == "/" })
	data.RootRedirect, data.RootRedirectOutlet = rootRedirectOf(data)
	data.HandlersSummary = handlersSummary(data)

	return data
}

// servedOutletWords places an outlet's words: each of its routes with words of its own
// in the chain comment, and in the surface table when the route declares a trace
// setting; the outlet's not-found handler among the worded ones when the outlet declares
// a word, else among the plain prefixes; and the outlet's prefix in the surface table
// when it declares a trace setting.
func servedOutletWords(data *servedRouterData, so *servedOutlet, wordedRoutes []*generatedRoute) {
	for _, route := range wordedRoutes {
		so.RouteWords = append(so.RouteWords, servedRouteWords{Method: route.Method, Path: route.Path, Words: describeWords(route.RequestLog, route.Traces)})
		if route.Traces.Declared() {
			data.Surfaces = append(data.Surfaces, servedSurface{Pattern: route.Path, Traces: route.Traces})
		}
	}
	if so.RequestLog.Declared() {
		data.WordedNotFound = append(data.WordedNotFound, so)
	} else {
		data.NotFoundPrefixes = append(data.NotFoundPrefixes, so.NotFoundPrefix)
	}
	if so.Traces.Declared() {
		data.Surfaces = append(data.Surfaces, servedSurface{Pattern: so.NotFoundPrefix, Traces: so.Traces})
	}
}

// servedWordFlags sets what the templates read off the words as a whole: whether any
// surface declares a word or a setting (the chain comment's closing paragraph), whether
// any word carries a floor (the logging import), and whether any outlet with a word has
// a prefix mounted by hand beneath it (the strings import, for the helper that excepts
// the prefix).
func servedWordFlags(data *servedRouterData) {
	data.DeclaresWords = data.RequestLog.Declared() || len(data.MountedRoutes) > 0 || len(data.Surfaces) > 0 || len(data.WordedNotFound) > 0
	data.UsesLogging = data.RequestLog.HasMinSeverity()
	for _, o := range data.Outlets {
		data.DeclaresWords = data.DeclaresWords || len(o.RouteWords) > 0
		data.UsesLogging = data.UsesLogging || o.RequestLog.HasMinSeverity()
		data.UsesStrings = data.UsesStrings || (o.RequestLog.Declared() && len(o.MountedBeneath) > 0)
	}
	for _, route := range data.ScheduledRoutes {
		data.DeclaresWords = data.DeclaresWords || route.RequestLog.Declared()
	}
	for _, m := range data.MountedRoutes {
		data.UsesLogging = data.UsesLogging || m.RequestLog.HasMinSeverity()
	}
}

// rootRedirectOf names where the root redirects and whose application it is: with
// browser applications served and none at /, the default outlet's mount path with a
// trailing slash, or the first declared outlet's that serves one. Empty otherwise.
func rootRedirectOf(data *servedRouterData) (target, outlet string) {
	if len(data.WebApps) == 0 || data.HasRootWebApp {
		return "", ""
	}
	for _, o := range data.Outlets {
		if o.WebApp != nil {
			return o.WebApp.Mount + "/", o.Name
		}
	}

	return "", ""
}

// servedOutletOf builds one outlet's payload from its declaration: its names, its
// browser application, and either its token check (bound to its issuer by
// linkTokenIssuers) or its session flavor with the flavor's routes under the prefix.
func servedOutletOf(o *routerOutlet, multiAuth bool) *servedOutlet {
	so := &servedOutlet{
		Name:           o.name,
		Suffix:         o.suffix(),
		Prefix:         o.prefix,
		HookField:      caser.ToPascal(o.name),
		RoutesFunc:     fmt.Sprintf("generated%sRoutes", o.suffix()),
		NotFoundPrefix: "/" + o.prefix + "/",
	}
	if o.webApp != "" {
		so.WebApp = &servedWebApp{
			Outlet:   o.name,
			Mount:    o.webApp,
			DeepLink: o.suffix() + "DeepLink",
			Assets:   o.suffix() + "Assets",
		}
	}
	so.RequestLog, so.Traces, so.Words = o.requestLog, o.traces, describeWords(o.requestLog, o.traces)
	switch {
	case o.apiKey:
		so.Machine = true
		so.TokenAuth = o.tokenAuth
		so.IssuerPackage = assumedPackageName(o.tokenAuth)
		so.TokensInterface = so.HookField + "Tokens"
		so.StubType = "router" + so.HookField + "TokensStub"
		so.Receiver = "h"
		if o.name != defaultOutletName {
			so.Getter = so.HookField
			so.Receiver = "h." + so.HookField + "()"
			so.StubField = caser.ToCamel(o.name)
			so.StubPrefix = so.HookField
		}
		prefix := "/" + o.prefix
		so.DeviceRoutes = []servedRoute{
			{Method: http.MethodPost, MethodConst: httpMethodConstant(http.MethodPost), Path: prefix + oauthDeviceSuffix, Suffix: oauthDeviceSuffix, Handler: deviceAuthorizeHandler},
			{Method: http.MethodPost, MethodConst: httpMethodConstant(http.MethodPost), Path: prefix + oauthTokenSuffix, Suffix: oauthTokenSuffix, Handler: deviceTokenHandler},
		}
	case o.auth != nil:
		so.SessionInterface = so.HookField + "Session"
		so.StubType = "router" + so.HookField + "SessionStub"
		so.AuthPath = o.auth.importPath
		if multiAuth {
			so.AuthPackage = o.auth.packageName()
		}
		so.Receiver = "h"
		if o.name != defaultOutletName {
			so.Getter = so.HookField
			so.Receiver = caser.ToCamel(o.name) + "Session"
			so.StubField = caser.ToCamel(o.name)
			so.StubPrefix = so.HookField
		}
		so.OldestAnswered = o.oldestAnswered
		so.OldestAnsweredExpr, so.OldestAnsweredNote, so.AnswersSentence = versionCheckOf(o.oldestAnswered)
		so.Probes = versionProbesFor(o.oldestAnswered)
		servedSessionOf(so, o)
	}

	return so
}

// linkTokenIssuers binds each machine outlet to the session outlet whose Auth issues
// its tokens (the first declaring that auth), names the issuer in the machine outlet's
// chain label, binds the machine outlet to the issuer's auth package where the router
// binds auths, and gives the issuer the token page: its validated routes, and the
// device flow's and the token page's handlers on its surface.
func linkTokenIssuers(data *servedRouterData) {
	for _, m := range data.MachineOutlets {
		var issuer *servedOutlet
		for _, so := range data.SessionOutlets {
			if so.AuthPath == m.TokenAuth {
				issuer = so

				break
			}
		}
		if issuer == nil {
			continue
		}
		m.Issuer = issuer
		m.IssuerExpr = "h"
		if issuer.Getter != "" {
			m.IssuerExpr = "h." + issuer.Getter + "()"
		}
		m.AuthPackage = issuer.AuthPackage
		m.AuthLabel = "bearer tokens of the " + m.IssuerPackage + " auth"
		if issuer.IssuesTokens {
			continue
		}
		issuer.IssuesTokens = true
		prefix := "/" + issuer.Prefix
		validated := func(method, suffix, handler string) servedRoute {
			return servedRoute{Method: method, MethodConst: httpMethodConstant(method), Path: prefix + suffix, Suffix: suffix, Handler: handler, XSRF: true, Validated: true}
		}
		issuer.ValidatedRoutes = append(issuer.ValidatedRoutes,
			validated(http.MethodGet, userTokensSuffix, tokensListHandler),
			validated(http.MethodPost, userTokensSuffix, tokensCreateHandler),
			validated(http.MethodPost, userTokensSuffix+"/revoke", tokensRevokeHandler),
			validated(http.MethodGet, userDeviceSuffix, deviceLookupHandler),
			validated(http.MethodPost, userDeviceSuffix+"/approve", deviceApproveHandler),
			validated(http.MethodPost, userDeviceSuffix+"/deny", deviceDenyHandler),
		)
		issuer.Routes = slices.Concat(issuer.OpenRoutes, issuer.GuardedRoutes, issuer.ValidatedRoutes)
		issuer.ExtraHandlers = []string{deviceAuthorizeHandler, deviceTokenHandler}
		issuer.SessionHandlers = append(issuer.SessionHandlers,
			servedSessionHandler{Name: deviceAuthorizeHandler, Description: "A command-line client's device authorization started (session.DeviceHandlers.Authorize): the client is told the code the person types; mounted under each machine outlet the auth issues tokens for"},
			servedSessionHandler{Name: deviceTokenHandler, Description: "The device flow's token endpoint (session.DeviceHandlers.Token): the client's polls for its pair, and the refresh grant; mounted under each machine outlet the auth issues tokens for"},
			servedSessionHandler{Name: deviceLookupHandler, Description: "The pending device authorization the person's code names, for the approval page (session.DeviceHandlers.Lookup)"},
			servedSessionHandler{Name: deviceApproveHandler, Description: "The device authorization approved by the signed-in person, as themselves (session.DeviceHandlers.Approve)"},
			servedSessionHandler{Name: deviceDenyHandler, Description: "The device authorization denied (session.DeviceHandlers.Deny)"},
			servedSessionHandler{Name: tokensListHandler, Description: "The signed-in person's live tokens (session.ApplicationTokenHandlers.List)"},
			servedSessionHandler{Name: tokensCreateHandler, Description: "An application token issued to the signed-in person for a program of their own, the secret shown once (session.ApplicationTokenHandlers.Create)"},
			servedSessionHandler{Name: tokensRevokeHandler, Description: "A token of the signed-in person's revoked (session.ApplicationTokenHandlers.Revoke)"},
		)
	}
}

// servedSessionOf fills a session outlet's methods and routes from its auth's methods:
// the session's own routes, then each method's login (and callback and front-channel
// logout) under the registration's name, the ones outside the XSRF guard first and the
// POSTs behind it, and the paths the login page is told.
func servedSessionOf(so *servedOutlet, o *routerOutlet) {
	prefix := "/" + o.prefix
	route := func(method, suffix, handler string, xsrf bool) servedRoute {
		return servedRoute{Method: method, MethodConst: httpMethodConstant(method), Path: prefix + suffix, Suffix: suffix, Handler: handler, XSRF: xsrf}
	}
	so.MethodsPath = prefix + userMethodsSuffix
	so.Pending = servedPending{Status: prefix + userPendingSuffix, Confirm: prefix + userPendingSuffix + "/confirm", Cancel: prefix + userPendingSuffix + "/cancel"}
	open := []servedRoute{
		route(http.MethodGet, userMethodsSuffix, methodsHandler, false),
		route(http.MethodGet, userSessionSuffix, "Authenticated", false),
		route(http.MethodDelete, userSessionSuffix, "Logout", false),
		route(http.MethodGet, userPendingSuffix, pendingStatusHandler, false),
	}
	var guarded []servedRoute
	kinds := make([]string, 0, len(o.auth.methods))
	for _, m := range o.auth.methods {
		sm := servedMethod{Name: m.name, Label: m.label, Kind: string(m.kind), Accessor: m.accessor()}
		base := "/user/" + m.name
		switch m.kind {
		case methodPassword:
			sm.LoginPath = prefix + base + "/login"
			sm.Handlers = []string{sm.Accessor + "Login", sm.Accessor + "Change"}
			sm.Description = "The password method's login and password change (session.PasswordMethod); the change validates the session itself, outside ValidateSession"
			guarded = append(guarded,
				route(http.MethodPost, base+"/login", sm.Accessor+"Login", true),
				route(http.MethodPost, base+"/change", sm.Accessor+"Change", true),
			)
			so.ChangePassword = prefix + base + "/change"
		case methodGoogle, methodWorkOS, methodAzure:
			sm.LoginPath = prefix + base + "/login"
			sm.Handlers = []string{sm.Accessor + "Login", sm.Accessor + "Callback"}
			sm.Description = fmt.Sprintf("The %s registration's login and callback (%s.Method)", m.name, m.kind)
			open = append(open,
				route(http.MethodGet, base+"/login", sm.Accessor+"Login", false),
				route(http.MethodGet, base+"/callback", sm.Accessor+"Callback", false),
			)
			if m.kind == methodAzure {
				sm.Handlers = append(sm.Handlers, sm.Accessor+"FrontChannelLogout")
				sm.Description = fmt.Sprintf("The %s registration's login, callback and front-channel logout (azure.Method)", m.name)
				open = append(open, route(http.MethodGet, base+"/logout", sm.Accessor+"FrontChannelLogout", false))
			}
		}
		so.Methods = append(so.Methods, sm)
		kinds = append(kinds, m.name)
	}
	guarded = append(guarded,
		route(http.MethodPost, userPendingSuffix+"/confirm", pendingConfirmHandler, true),
		route(http.MethodPost, userPendingSuffix+"/cancel", pendingCancelHandler, true),
	)
	guarded, validatedRoutes := servedMailRoutesOf(so, o, guarded, route)
	so.OpenRoutes, so.GuardedRoutes, so.ValidatedRoutes = open, guarded, validatedRoutes
	so.Routes = slices.Concat(open, guarded, validatedRoutes)
	so.AuthLabel = "sessions"
	if so.AuthPackage != "" {
		so.AuthLabel += " of the " + so.AuthPackage + " auth"
	}
	so.AuthLabel += " (" + strings.Join(kinds, ", ") + ")"
}

// servedMailRoutesOf adds the invitation, email address and sign-up routes
// (session.Auth.Invitation, Email and SignUp) to the outlet: the login page opens an
// invitation and, with a password method, an invited stranger signs up at it, behind
// the XSRF guard alone; a signed-in person accepts it and adds or verifies an address
// inside the validated group; open sign-up, when the password method declares it,
// starts, opens and completes behind the XSRF guard alone. It answers the guarded
// routes with the mail ones appended, and the validated routes.
func servedMailRoutesOf(so *servedOutlet, o *routerOutlet, guarded []servedRoute, route func(method, suffix, handler string, xsrf bool) servedRoute) (guardedRoutes, validatedRoutes []servedRoute) {
	prefix := "/" + o.prefix
	validated := func(suffix, handler string) servedRoute {
		r := route(http.MethodPost, suffix, handler, true)
		r.Validated = true

		return r
	}
	hasPassword, openSignUp := false, false
	for _, m := range o.auth.methods {
		if m.kind == methodPassword {
			hasPassword, openSignUp = true, m.openSignUp
		}
	}
	so.Invitation = servedInvitation{Open: prefix + userInvitationSuffix, Accept: prefix + userInvitationSuffix + "/accept"}
	so.Email = servedEmail{Add: prefix + userEmailSuffix, Verify: prefix + userEmailSuffix + "/verify"}
	guarded = append(guarded, route(http.MethodPost, userInvitationSuffix, invitationOpenHandler, true))
	so.SessionHandlers = append(so.SessionHandlers,
		servedSessionHandler{Name: invitationOpenHandler, Description: "The invitation the login page was sent to, opened (session.InvitationHandlers.Open): the sign-in that follows carries it"},
		servedSessionHandler{Name: invitationAcceptHandler, Description: "The invitation accepted by a person already signed in (session.InvitationHandlers.Accept)"},
	)
	if hasPassword {
		so.Invitation.SignUp = prefix + userInvitationSuffix + "/sign-up"
		guarded = append(guarded, route(http.MethodPost, userInvitationSuffix+"/sign-up", invitationSignUpHandler, true))
		so.SessionHandlers = append(so.SessionHandlers, servedSessionHandler{Name: invitationSignUpHandler, Description: "An invited person's account, created with a password and signed in (session.InvitationHandlers.SignUp)"})
	}
	so.SessionHandlers = append(so.SessionHandlers,
		servedSessionHandler{Name: emailAddHandler, Description: "A signed-in person's email address, added by mailing it a verification link (session.EmailHandlers.Add)"},
		servedSessionHandler{Name: emailVerifyHandler, Description: "The address verified from the mailed link (session.EmailHandlers.Verify)"},
	)
	validatedRoutes = []servedRoute{
		validated(userInvitationSuffix+"/accept", invitationAcceptHandler),
		validated(userEmailSuffix, emailAddHandler),
		validated(userEmailSuffix+"/verify", emailVerifyHandler),
	}
	if openSignUp {
		so.SignUp = &servedSignUp{Start: prefix + userSignUpSuffix, Open: prefix + userSignUpSuffix + "/open", Complete: prefix + userSignUpSuffix + "/complete"}
		guarded = append(guarded,
			route(http.MethodPost, userSignUpSuffix, signUpStartHandler, true),
			route(http.MethodPost, userSignUpSuffix+"/open", signUpOpenHandler, true),
			route(http.MethodPost, userSignUpSuffix+"/complete", signUpCompleteHandler, true),
		)
		so.SessionHandlers = append(so.SessionHandlers,
			servedSessionHandler{Name: signUpStartHandler, Description: "A sign-up started from an address, which is mailed its link (session.SignUpHandlers.Start)"},
			servedSessionHandler{Name: signUpOpenHandler, Description: "The sign-up link opened (session.SignUpHandlers.Open)"},
			servedSessionHandler{Name: signUpCompleteHandler, Description: "The sign-up completed with a password: the account created and signed in (session.SignUpHandlers.Complete)"},
		)
	}

	return guarded, validatedRoutes
}

// handlersSummary lists what the Handlers interface carries, for its doc comment.
func handlersSummary(data *servedRouterData) string {
	parts := []string{"every outlet's generated handlers"}
	if len(data.SessionOutlets) > 0 {
		parts = append(parts, "each session outlet's session handlers")
	}
	parts = append(parts, "the middleware every request and every outlet passes")
	if len(data.MachineOutlets) > 0 {
		parts = append(parts, "the machine outlets' token surfaces")
	}
	if len(data.ScheduledRoutes) > 0 {
		parts = append(parts, "the scheduled methods' handlers and the scheduler's token check")
	}
	if len(data.WebApps) > 0 {
		parts = append(parts, "the browser applications' handlers")
	}

	return strings.Join(parts[:len(parts)-1], ", ") + ", and " + parts[len(parts)-1]
}

// servedMountedRoutes places the hand-mounted prefixes: every one in the request
// logger's prefix table and, with a trace setting, the surface table; each under the
// outlet whose prefix it sits beneath, else under the root.
func (r *resourceGenerator) servedMountedRoutes(data *servedRouterData) {
	mounted := slices.Clone(r.mountedRoutes)
	slices.SortFunc(mounted, func(a, b mountedRoutes) int {
		return strings.Compare(a.prefix, b.prefix)
	})
	for _, m := range mounted {
		served := servedMountedRoutes{Prefix: m.prefix, RequestLog: m.requestLog, Traces: m.traces, Words: describeWords(m.requestLog, m.traces)}
		data.MountedRoutes = append(data.MountedRoutes, served)
		data.Surfaces = append(data.Surfaces, servedSurface{Pattern: m.prefix, Traces: m.traces})
		if outlet := outletBeneath(data.Outlets, m.prefix); outlet != nil {
			outlet.MountedBeneath = append(outlet.MountedBeneath, served)

			continue
		}
		data.RootMountedRoutes = append(data.RootMountedRoutes, served)
	}
}

// outletBeneath returns the outlet whose prefix the hand-mounted prefix sits under, nil
// when it sits under none.
func outletBeneath(outlets []*servedOutlet, prefix string) *servedOutlet {
	for _, o := range outlets {
		if strings.HasPrefix(prefix, o.NotFoundPrefix) {
			return o
		}
	}

	return nil
}

// The probes the generated request log test registers through the hooks, under the
// surface each case names.
const (
	requestLogProbePath   = "generated-router-probe"
	requestLogFailurePath = "generated-router-failure"
	requestLogOwnWordPath = "generated-router-own-word"
)

// requestLogCasesOf builds the generated request log test's cases: the application
// default on a root probe, quiet and failed, and a probe that sets its own request's
// word; the first outlet without a word, which takes the default; each outlet with a
// word, quiet and failed; each hand-mounted prefix, quiet and failed, through the root
// hook or the hook of the outlet it sits beneath; and each generated route with a word
// of its own, quiet.
func requestLogCasesOf(data *servedRouterData) []servedRequestLogCase {
	defaultWord := data.RequestLog
	defaultName := "the application default, " + defaultWord.String()
	if !defaultWord.Declared() {
		defaultName = "the application default, always, since nothing is declared"
	}
	cases := []servedRequestLogCase{
		probeCase(defaultName+", on a quiet request", rootHookField, "/"+requestLogProbePath, http.StatusOK, defaultWord),
		probeCase(defaultName+", on a failed request", rootHookField, "/"+requestLogFailurePath, http.StatusNotFound, defaultWord),
	}
	ownWord := servedRequestLogCase{Name: "a handler that sets its own request's word", Hook: rootHookField, Method: http.MethodGet, MethodConst: httpMethodConstant(http.MethodGet), Path: "/" + requestLogOwnWordPath, Status: http.StatusOK}
	if requestLogDecision(defaultWord, false) == decisionWritten {
		ownWord.Name += " to never, under " + defaultName
		ownWord.SetWord, ownWord.Want = LogNever().Expr(), decisionDropped
	} else {
		ownWord.Name += " to always, under " + defaultName
		ownWord.SetWord, ownWord.Want = LogAlways().Expr(), decisionWritten
	}
	cases = append(cases, ownWord)
	unworded := false
	for _, o := range data.Outlets {
		if !o.RequestLog.Declared() {
			if !unworded {
				cases = append(cases, probeCase("the "+o.Name+" outlet, which takes "+defaultName, o.HookField, o.NotFoundPrefix+requestLogProbePath, http.StatusOK, defaultWord))
				unworded = true
			}

			continue
		}
		name := "the " + o.Name + " outlet's word, " + o.RequestLog.String()
		cases = append(cases,
			probeCase(name+", on a quiet request", o.HookField, o.NotFoundPrefix+requestLogProbePath, http.StatusOK, o.RequestLog),
			probeCase(name+", on a failed request", o.HookField, o.NotFoundPrefix+requestLogFailurePath, http.StatusNotFound, o.RequestLog),
		)
	}
	for _, m := range data.MountedRoutes {
		hook := rootHookField
		if outlet := outletBeneath(data.Outlets, m.Prefix); outlet != nil {
			hook = outlet.HookField
		}
		base := strings.TrimSuffix(m.Prefix, "/") + "/"
		name := "the prefix " + m.Prefix + " mounted by hand, " + m.RequestLog.String()
		cases = append(cases,
			probeCase(name+", on a quiet request", hook, base+requestLogProbePath, http.StatusOK, m.RequestLog),
			probeCase(name+", on a failed request", hook, base+requestLogFailurePath, http.StatusNotFound, m.RequestLog),
		)
	}

	return cases
}

// probeCase is one probe the test registers at path through the hook, answering the
// status, under the word.
func probeCase(name, hook, path string, status int, word RequestLog) servedRequestLogCase {
	return servedRequestLogCase{
		Name:        name,
		Hook:        hook,
		Method:      http.MethodGet,
		MethodConst: httpMethodConstant(http.MethodGet),
		Path:        path,
		Status:      status,
		Want:        requestLogDecision(word, status >= http.StatusBadRequest),
	}
}

// routeCasesOf lists the generated routes with a request log word of their own as cases
// of the generated request log test: each driven at its test URL, quiet, since the
// routes test's stub answers 200.
func routeCasesOf(wordedRoutes map[string][]*generatedRoute, outlets []*servedOutlet) []servedRequestLogCase {
	var cases []servedRequestLogCase
	for _, o := range outlets {
		for _, route := range wordedRoutes[o.Name] {
			if !route.RequestLog.Declared() {
				continue
			}
			cases = append(cases, servedRequestLogCase{
				Name:        "the route's own word, " + route.RequestLog.String() + ", on " + route.Method + " " + route.Path,
				Method:      route.Method,
				MethodConst: httpMethodConstant(route.Method),
				Path:        route.TestURL,
				Status:      http.StatusOK,
				Want:        requestLogDecision(route.RequestLog, false),
			})
		}
	}

	return cases
}

// The served-router templates. servedRouterTemplate renders zz_gen_router.go: the chain
// comment as the package documentation, the Handlers interface, the Hooks struct, and
// New, linear and inline. servedRouterTestTemplate renders zz_gen_router_test.go, which
// proves the chain comment by driving every route through New with recording stubs.
var (
	servedRouterTemplate = `// Code generated by resourcegeneration. DO NOT EDIT.
// Source: {{ .Source }}

// Package {{ .Package }} serves the application. The middleware in front of every route,
// outermost first, one line per group, each chain followed by what it stands in front of:
//
//	every request: tracing, hooks.Outermost, request logging{{ if .RequestLog.Declared }} ({{ .RequestLog }}){{ end }}, SecurityHeaders, httpio.WithParams
{{- range .RootMountedRoutes }}
//	  by hand under {{ .Prefix }}: {{ .Words }}
{{- end }}
{{- range .Outlets }}
{{- if .Machine }}
//	{{ .Name }} (/{{ .Prefix }}), {{ .AuthLabel }}{{ with .Words }}, {{ . }}{{ end }}:
//	  {{ if .AuthPackage }}BindAuth({{ .AuthPackage }}.Name), {{ end }}NoCaching, CompressionMiddleware, ReleaseHeader: {{ range $i, $route := .DeviceRoutes }}{{ if $i }}, {{ end }}{{ $route.Method }} {{ $route.Path }}{{ end }}
//	  + ValidateToken: hooks.{{ .HookField }}, {{ .RoutesFunc }}
{{- else }}
//	{{ .Name }} (/{{ .Prefix }}), {{ .AuthLabel }}{{ with .Words }}, {{ . }}{{ end }}:
//	  {{ if .AuthPackage }}BindAuth({{ .AuthPackage }}.Name), {{ end }}NoCaching, CompressionMiddleware, ReleaseHeader, StartSession, SetXSRFToken: {{ range $i, $route := .OpenRoutes }}{{ if $i }}, {{ end }}{{ $route.Method }} {{ $route.Path }}{{ end }}
//	  + ValidateXSRFToken: {{ range $i, $route := .GuardedRoutes }}{{ if $i }}, {{ end }}{{ $route.Method }} {{ $route.Path }}{{ end }}
//	  + ValidateSession, ValidateXSRFToken: {{ range $i, $route := .ValidatedRoutes }}{{ if $i }}, {{ end }}{{ $route.Method }} {{ $route.Path }}{{ end }}
//	  + ValidateSession, ValidateXSRFToken, CheckAPIVersion{{ .OldestAnsweredNote }}: hooks.{{ .HookField }}, {{ .RoutesFunc }}
{{- end }}
{{- range .MountedBeneath }}
//	  by hand under {{ .Prefix }}: {{ .Words }}
{{- end }}
{{- range .RouteWords }}
//	  {{ .Method }} {{ .Path }}: {{ .Words }}
{{- end }}
{{- end }}
{{- if .ScheduledRoutes }}
//	scheduled ({{ .ScheduledPrefix }}), Cloud Scheduler's token:
//	  NoCaching, CompressionMiddleware, ReleaseHeader, SchedulerAuth: generatedScheduledRoutes
{{- range .ScheduledRoutes }}
{{- if .RequestLog.Declared }}
//	  POST {{ .Path }}: request log {{ .RequestLog }}
{{- end }}
{{- end }}
{{- end }}
//
// hooks.Root's routes sit behind the every-request chain alone. Under an outlet's prefix
// nothing else answers: an unknown path is 404.{{ if .WebApps }} Outside every prefix the browser
// applications answer, longer mount paths first:{{ range $i, $w := .WebApps }}{{ if $i }},{{ end }} {{ $w.Mount }} ({{ $w.DeepLink }}, {{ $w.Assets }}){{ end }}.{{ end }}
{{- if .RootRedirect }}
// None is mounted at /: the root alone redirects to {{ .RootRedirect }}, the {{ .RootRedirectOutlet }} outlet's application.
{{- end }}
{{- if .DeclaresWords }}
//
// The request log word and the trace setting named at each place are the nearest
// declarations, a route's over its outlet's and an outlet's over the application
// default; a request under none writes its entry always and its spans follow the front
// end. The root's request logger decides each entry when the request ends, and a handler
// may change its own request's word through logger.FromReq(r).SetPolicy.
{{- end }}
package {{ .Package }}

import (
	"fmt"
	"net/http"
{{- if .UsesStrings }}
	"strings"
{{- end }}

{{- if .UsesLogging }}
	"cloud.google.com/go/logging"
{{- end }}
{{- range .AuthImports }}
	"{{ . }}"
{{- end }}
	"github.com/cccteam/ccc/resource"
{{- range .StoreImports }}
	"{{ . }}"
{{- end }}
	"github.com/cccteam/ccc/tracer"
	"github.com/cccteam/httpio"
	"github.com/cccteam/logger"
	"github.com/cccteam/session"
	"github.com/go-chi/chi/v5"
)

// Handlers is the full surface New composes: {{ .HandlersSummary }}.
type Handlers interface {
	GeneratedHandlers
{{- range .ExtraOutlets }}
	Generated{{ .Suffix }}Handlers
{{- end }}
{{- if .ScheduledRoutes }}
	GeneratedScheduledHandlers
{{- end }}
{{- if .FileStores }}
	// ResourceClient is the client the generated handlers run against. The file
	// stores the generated code reads and writes are wired on it, and New refuses to
	// start without them (resource.RequireFileStores).
	ResourceClient() resource.Client
{{- end }}
{{- range .SessionOutlets }}
{{- if .Getter }}
	// {{ .Getter }} returns the {{ .Name }} outlet's session surface{{ if .AuthPackage }}: the {{ .AuthPackage }} auth's{{ end }}.
	{{ .Getter }}() {{ .SessionInterface }}
{{- else }}
	// The default outlet's session surface{{ if .AuthPackage }}: the {{ .AuthPackage }} auth's{{ end }}.
	{{ .SessionInterface }}
{{- end }}
{{- end }}
{{- if .MultiAuth }}
	// BindAuth marks a session group's requests as authenticated by the named auth, so
	// the handlers behind it check permissions and tenant visibility in that auth's store.
	BindAuth(name string) func(http.Handler) http.Handler
{{- end }}
{{- range .MachineOutlets }}
{{- if .Getter }}
	// {{ .Getter }} returns the {{ .Name }} outlet's token surface: the machine outlet the application
	// registers on the {{ .IssuerPackage }} auth's Auth (session.RegisterMachineOutlet), whose token
	// check binds each request to the person its bearer token acts for.
	{{ .Getter }}() {{ .TokensInterface }}
{{- else }}
	// The default outlet's token surface: the machine outlet the application registers on
	// the {{ .IssuerPackage }} auth's Auth (session.RegisterMachineOutlet).
	{{ .TokensInterface }}
{{- end }}
{{- end }}
{{- if .ScheduledRoutes }}
	// SchedulerAuth admits Cloud Scheduler's calls to the scheduled routes and refuses
	// every other with 401: a token Google signed for the route's URL whose verified
	// email is the invoker identity (scheduled.Guard's Middleware, built from the
	// environment by scheduled.FromEnvironment).
	SchedulerAuth(next http.Handler) http.Handler
{{- end }}
	// ServerVersion is the release this server was built from, the configuration's
	// APP_VERSION: what every outlet names on each answer (resource.ReleaseHeader) and
	// each session outlet checks a browser application's X-Api-Version against
	// (resource.CheckAPIVersion). A value that is not a release, dev for one, is named
	// as it is and checks nothing.
	ServerVersion() string

	// Every request: where the request log goes; the router builds the request logger from it.
	LogExporter() logger.Exporter
	SecurityHeaders(next http.Handler) http.Handler

	// Every outlet.
	NoCaching(next http.Handler) http.Handler
	CompressionMiddleware() func(http.Handler) http.Handler
{{- range .WebApps }}

	// The {{ .Outlet }} outlet's browser application at {{ .Mount }}: {{ .DeepLink }} rewrites its routes to
	// the entry document, {{ .Assets }} serves the built bundle.
	{{ .DeepLink }}(next http.Handler) http.Handler
	{{ .Assets }}() http.HandlerFunc
{{- end }}
}

{{- range .SessionOutlets }}

// {{ .SessionInterface }} is the {{ .Name }} outlet's session surface: the Auth's middleware and
// handlers, what the login page is told, the pending sign-in handlers, and the handlers
// of the sign-in methods registered on the Auth, each under its registration name.
type {{ .SessionInterface }} interface {
	StartSession(next http.Handler) http.Handler
	SetXSRFToken(next http.Handler) http.Handler
	ValidateSession(next http.Handler) http.Handler
	ValidateXSRFToken(next http.Handler) http.Handler
	// The session's own routes (session.Auth): read and ended.
	Authenticated() http.HandlerFunc
	Logout() http.HandlerFunc
	// Methods tells the login page what the outlet offers (session.Auth.Methods): the
	// router hands it the methods below at the paths it mounts them.
	Methods(list *session.MethodList) http.HandlerFunc
	// The pending sign-in (session.PendingHandlers): a sign-in that waits for a
	// password confirmation or the application's MFA step.
	PendingStatus() http.HandlerFunc
	PendingConfirmWithPassword() http.HandlerFunc
	PendingCancel() http.HandlerFunc
{{- range .SessionHandlers }}
	// {{ .Description }}.
	{{ .Name }}() http.HandlerFunc
{{- end }}
{{- range .Methods }}
	// {{ .Description }}.
{{- range .Handlers }}
	{{ . }}() http.HandlerFunc
{{- end }}
{{- end }}
}
{{- end }}
{{- range .MachineOutlets }}

// {{ .TokensInterface }} is the {{ .Name }} outlet's token surface: the token check of the machine
// outlet the application registers on the {{ .IssuerPackage }} auth's Auth
// (session.RegisterMachineOutlet(auth, "{{ .Name }}")), which a *session.MachineOutlet satisfies.
// A request on the outlet carries a bearer token the auth issued for it, an application
// token or a device grant's access token, and the check binds it to the person the
// token acts for: the same user, principal and evidence a validated session carries.
type {{ .TokensInterface }} interface {
	ValidateToken(next http.Handler) http.Handler
}
{{- end }}

// Hooks are the application's additions to the generated router. They compose inward
// only: a hook adds middleware and routes under the guards it is handed and never sees
// the outer router, so no generated route can be lifted out from behind session
// validation or the XSRF guard. Every field may be nil.
type Hooks struct {
	// Outermost runs after tracing and ahead of the request logger on every request.
	Outermost []func(http.Handler) http.Handler
	// Root registers routes outside every outlet: health checks, webhooks, scheduler
	// triggers. They sit behind the every-request chain and nothing else.
	Root func(r chi.Router)
{{- range .Outlets }}
{{- if .Machine }}
	// {{ .HookField }} runs inside the {{ .Name }} outlet's token group: r already sits behind
	// {{ if .AuthPackage }}BindAuth({{ .AuthPackage }}.Name) and {{ end }}the token check, and generated registers the outlet's generated
	// routes. Nil registers them directly.
{{- else }}
	// {{ .HookField }} runs inside the {{ .Name }} outlet's authenticated group: r already sits behind
	// {{ if .AuthPackage }}BindAuth({{ .AuthPackage }}.Name), {{ end }}session validation, and the XSRF guard, and generated registers
	// the outlet's generated routes. Nil registers them directly.
{{- end }}
	{{ .HookField }} func(r chi.Router, generated func(chi.Router))
{{- end }}
}

// New wires the served application: the every-request middleware, hooks.Root, one group
// per outlet with its authentication around its generated routes and the outlet's hook,
// a not-found handler per outlet prefix, and the browser applications.
func New(h Handlers, hooks Hooks) *chi.Mux {
{{- if .FileStores }}
	// Every file store the generated code reads or writes is wired on the resource
	// client, or the server does not start: an unwired store would otherwise surface
	// on the first upload, file request or releasing delete.
	if err := resource.RequireFileStores(h.ResourceClient(){{ range .FileStores }}, {{ .NameExpr }}{{ end }}); err != nil {
		panic(fmt.Sprintf("router.New: %v", err))
	}
{{- end }}
	r := chi.NewRouter()
{{- if or .Outlets .ScheduledRoutes }}
	// The release this server was built from: every outlet names it on each answer
	// (X-Api-Version), and each session outlet's version check compares a browser
	// application's against it.
	serverVersion := h.ServerVersion()
{{- end }}

	// Every request.
{{- if .Surfaces }}
	// The surfaces with a trace setting of their own, by the path each is mounted at: the
	// sampler applies the longest match as the request's span starts, since a span's
	// sampled flag is fixed then; a request under none follows the front end.
	r.Use(tracer.NewHandler(tracer.Surfaces(map[string]tracer.Traces{
{{- range .Surfaces }}
		"{{ .Pattern }}": {{ .Traces.Expr }},
{{- end }}
	})))
{{- else }}
	r.Use(tracer.NewHandler())
{{- end }}
	r.Use(hooks.Outermost...)
{{- if or .RequestLog.Declared .MountedRoutes }}
{{- if and .RequestLog.Declared .MountedRoutes }}
	// The request logger decides each request's entry when the request ends, under the
	// word a request starts with, the application default or the word of the longest
	// prefix declared by hand that the path sits under; an outlet's word, a route's own
	// and a handler's own request override it on the way down.
{{- else if .RequestLog.Declared }}
	// The request logger decides each request's entry when the request ends, under the
	// word a request starts with, the application default; an outlet's word, a route's
	// own and a handler's own request override it on the way down.
{{- else }}
	// The request logger decides each request's entry when the request ends, under the
	// word a request starts with, the word of the longest prefix declared by hand that
	// the path sits under, else always; an outlet's word, a route's own and a handler's
	// own request override it on the way down.
{{- end }}
	r.Use(logger.NewRequestLogger(h.LogExporter(),
{{- if .RequestLog.Declared }}
		logger.DefaultPolicy({{ .RequestLog.Expr }}),
{{- end }}
{{- if .MountedRoutes }}
		logger.PolicyByPrefix(map[string]logger.Policy{
{{- range .MountedRoutes }}
			"{{ .Prefix }}": {{ .RequestLog.Expr }},
{{- end }}
		}),
{{- end }}
	))
{{- else }}
	r.Use(logger.NewRequestLogger(h.LogExporter()))
{{- end }}
	r.Use(h.SecurityHeaders)
	r.Use(httpio.WithParams)

	// The application's routes outside every outlet.
	if hooks.Root != nil {
		r.Group(func(r chi.Router) {
			// The application's own routes take the body limit the generated routes take.
			r.Use(resource.BodyLimit(BodyLimit))
			hooks.Root(r)
		})
	}
{{- range .Outlets }}
{{ if .Machine }}
	// The {{ .Name }} outlet (/{{ .Prefix }}): machine clients carrying {{ .AuthLabel }}, so the group
	// carries no session handling and no XSRF guard. The device flow's endpoints, where a
	// command-line client obtains its tokens with a person's approval, sit in front of the
	// token check; everything else sits behind it.
	r.Group(func(r chi.Router) {
{{- template "outletWord" . }}
{{- if .AuthPackage }}
		r.Use(h.BindAuth({{ .AuthPackage }}.Name))
{{- end }}
		r.Use(h.NoCaching)
		r.Use(h.CompressionMiddleware())
		r.Use(resource.ReleaseHeader(serverVersion))
{{ $outlet := . }}
		bounded := r.With(resource.BodyLimit(BodyLimit))
{{- range .DeviceRoutes }}
		bounded.{{ Pascal .Method }}("{{ .Path }}", {{ $outlet.IssuerExpr }}.{{ .Handler }}())
{{- end }}

		r.Group(func(r chi.Router) {
			r.Use({{ .Receiver }}.ValidateToken)

			registerGenerated(r, hooks.{{ .HookField }}, "{{ .HookField }}", func(r chi.Router) {
				{{ .RoutesFunc }}(r, h)
			})
		})
	})
{{- else }}
	// The {{ .Name }} outlet (/{{ .Prefix }}): {{ .AuthLabel }}.
{{- if .Getter }}
	{{ .Receiver }} := h.{{ .Getter }}()
{{- end }}
	r.Group(func(r chi.Router) {
{{- template "outletWord" . }}
{{- if .AuthPackage }}
		r.Use(h.BindAuth({{ .AuthPackage }}.Name))
{{- end }}
		r.Use(h.NoCaching)
		r.Use(h.CompressionMiddleware())
		r.Use(resource.ReleaseHeader(serverVersion))
		r.Use({{ .Receiver }}.StartSession)
		r.Use({{ .Receiver }}.SetXSRFToken)
{{ $outlet := . }}
		// The session routes are bounded at BodyLimit, as the generated routes are. The
		// login page reads what the outlet offers from the method list, at the paths
		// mounted below; the sign-in's POSTs sit behind the XSRF guard.
		bounded := r.With(resource.BodyLimit(BodyLimit))
		guarded := bounded.With({{ .Receiver }}.ValidateXSRFToken)
{{- range .Routes }}
{{- if eq .Handler "Methods" }}
		{{ if .XSRF }}guarded{{ else }}bounded{{ end }}.{{ Pascal .Method }}("{{ .Path }}", {{ $outlet.Receiver }}.Methods(&session.MethodList{
			Methods: []session.AdvertisedMethod{
{{- range $outlet.Methods }}
				{Name: "{{ .Name }}", Label: "{{ .Label }}", Path: "{{ .LoginPath }}"},
{{- end }}
			},
			Pending: session.PendingRoutes{Status: "{{ $outlet.Pending.Status }}", Confirm: "{{ $outlet.Pending.Confirm }}", Cancel: "{{ $outlet.Pending.Cancel }}"},
{{- if $outlet.ChangePassword }}
			ChangePassword: "{{ $outlet.ChangePassword }}",
{{- end }}
			Invitation: session.InvitationRoutes{Open: "{{ $outlet.Invitation.Open }}", Accept: "{{ $outlet.Invitation.Accept }}"{{ if $outlet.Invitation.SignUp }}, SignUp: "{{ $outlet.Invitation.SignUp }}"{{ end }}},
			Email: session.EmailRoutes{Add: "{{ $outlet.Email.Add }}", Verify: "{{ $outlet.Email.Verify }}"},
{{- if $outlet.SignUp }}
			SignUp: session.SignUpRoutes{Start: "{{ $outlet.SignUp.Start }}", Open: "{{ $outlet.SignUp.Open }}", Complete: "{{ $outlet.SignUp.Complete }}"},
{{- end }}
		}))
{{- else if not .Validated }}
		{{ if .XSRF }}guarded{{ else }}bounded{{ end }}.{{ Pascal .Method }}("{{ .Path }}", {{ $outlet.Receiver }}.{{ .Handler }}())
{{- end }}
{{- end }}

		r.Group(func(r chi.Router) {
			r.Use({{ .Receiver }}.ValidateSession)
			r.Use({{ .Receiver }}.ValidateXSRFToken)
			// The version check: a browser application sends its release in X-Api-Version,
			// and {{ .AnswersSentence }}. An application outside that
			// range is refused with 412 naming the server's release, before its body is read;
			// a request without the header, the session routes above and below{{ if .FileRoutes }} and the
			// stored-file routes{{ end }} are answered at any release.
			r.Use(resource.CheckAPIVersion(resource.APIVersionCheck{
				ServerVersion: serverVersion,
{{- if .OldestAnsweredExpr }}
				OldestAnswered: {{ .OldestAnsweredExpr }},
{{- end }}
				Exempt: []string{
{{- range .ValidatedRoutes }}
					"{{ .Path }}",
{{- end }}
{{- range .FileRoutes }}
					"{{ .Pattern }}",
{{- end }}
				},
			}))

			// The session routes a signed-in person calls: the acceptance of an invitation
			// the browser carries, and the address they add and verify.
{{- range .ValidatedRoutes }}
			r.With(resource.BodyLimit(BodyLimit)).{{ Pascal .Method }}("{{ .Path }}", {{ $outlet.Receiver }}.{{ .Handler }}())
{{- end }}

			registerGenerated(r, hooks.{{ .HookField }}, "{{ .HookField }}", func(r chi.Router) {
				{{ .RoutesFunc }}(r, h)
			})
		})
	})
{{- end }}
{{- end }}

{{- if .ScheduledRoutes }}

	// The scheduled routes ({{ .ScheduledPrefix }}): Cloud Scheduler's calls, each carrying a token
	// of the invoker identity, so the group carries no session handling, no XSRF guard and
	// no hook: SchedulerAuth is the one way in.
	r.Group(func(r chi.Router) {
		r.Use(h.NoCaching)
		r.Use(h.CompressionMiddleware())
		r.Use(resource.ReleaseHeader(serverVersion))
		r.Use(h.SchedulerAuth)

		generatedScheduledRoutes(r, h)
	})
{{- end }}

	// Under an outlet's prefix nothing else answers: an unknown API path is 404, never a
	// browser application's entry document.
{{- if .NotFoundPrefixes }}
	for _, prefix := range []string{ {{- range $i, $p := .NotFoundPrefixes }}{{ if $i }}, {{ end }}"{{ $p }}"{{ end -}} } {
		r.Route(prefix, func(r chi.Router) {
			r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "Not Found", http.StatusNotFound)
			})
		})
	}
{{- end }}
{{- range .WordedNotFound }}
	// The {{ .Name }} outlet's not-found handler carries the outlet's word, so an unknown path
	// under its prefix is logged as its requests are.
	r.Route("{{ .NotFoundPrefix }}", func(r chi.Router) {
{{- template "outletWord" . }}
		r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "Not Found", http.StatusNotFound)
		})
	})
{{- end }}
{{- range .WebApps }}

	// The {{ .Outlet }} outlet's browser application at {{ .Mount }}{{ if eq .Mount "/" }}, the catch-all{{ end }}.
	r.Route("{{ .Mount }}", func(r chi.Router) {
		r.Use(h.{{ .DeepLink }})

		r.Get("/*", h.{{ .Assets }}())
	})
{{- end }}
{{- if .RootRedirect }}

	// No browser application is mounted at /: an installed application's scope is every
	// URL under its start, so one at / would own the origin and the others would never
	// install on their own. The root alone sends the browser to the {{ .RootRedirectOutlet }} outlet's
	// application; every other unmatched path is 404.
	r.Get("/", func(w http.ResponseWriter, req *http.Request) {
		http.Redirect(w, req, "{{ .RootRedirect }}", http.StatusTemporaryRedirect)
	})
{{- end }}

	return r
}

// registerGenerated registers an outlet's generated routes inside its guarded group:
// through the outlet's hook when the application supplies one, which must call generated
// exactly once, or directly.
func registerGenerated(r chi.Router, hook func(chi.Router, func(chi.Router)), name string, generated func(chi.Router)) {
	if hook == nil {
		generated(r)

		return
	}

	calls := 0
	hook(r, func(r chi.Router) {
		calls++
		generated(r)
	})
	if calls != 1 {
		panic(fmt.Sprintf("router.New: hooks.%s must call generated exactly once to register the outlet's generated routes, called it %d times", name, calls))
	}
}
{{- if .UsesStrings }}

// outletRequestLog sets the outlet's request log word on every request in its group except
// one under a prefix declared by hand beneath the outlet (WithMountedRoutes), which keeps
// the word the root's request logger started it with: the nearest declaration wins.
func outletRequestLog(word logger.Policy, mountedPrefixes ...string) func(http.Handler) http.Handler {
	setWord := logger.WithPolicy(word)

	return func(next http.Handler) http.Handler {
		worded := setWord(next)

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			for _, prefix := range mountedPrefixes {
				if strings.HasPrefix(r.URL.Path, prefix) {
					next.ServeHTTP(w, r)

					return
				}
			}
			worded.ServeHTTP(w, r)
		})
	}
}
{{- end }}
{{- /* outletWord sets an outlet's request log word on its group: with hand-mounted prefixes beneath the outlet, through outletRequestLog, which keeps theirs. */ -}}
{{- define "outletWord" }}
{{- if .RequestLog.Declared }}
{{- if .MountedBeneath }}
		// Every request under the outlet writes its request log {{ .RequestLog }}, except under
		// the prefixes declared by hand beneath it, which keep their own word.
		r.Use(outletRequestLog({{ .RequestLog.Expr }}{{ range .MountedPrefixes }}, "{{ . }}"{{ end }}))
{{- else }}
		// Every request under the outlet writes its request log {{ .RequestLog }}.
		r.Use(logger.WithPolicy({{ .RequestLog.Expr }}))
{{- end }}
{{- end }}
{{- end }}
`

	servedRouterTestTemplate = `// Code generated by resourcegeneration. DO NOT EDIT.
// Source: {{ .Source }}

package {{ .Package }}

import (
	"bytes"
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

{{- range .AuthImports }}
	"{{ . }}"
{{- end }}
	"github.com/cccteam/ccc/resource"
{{- range .StoreImports }}
	"{{ . }}"
{{- end }}
	"github.com/cccteam/logger"
	"github.com/cccteam/session"
	"github.com/go-chi/chi/v5"
)

// routerRootChain is the middleware every request passes, in order.
var routerRootChain = []string{"LogExporter", "SecurityHeaders"}

// routerOutletChain is one outlet's middleware in order: group runs in front of the
// outlet's session routes, or a machine outlet's device-flow endpoints; guards
// additionally in front of its generated routes.
type routerOutletChain struct {
	group  []string
	guards []string
}

// routerOutletChains keys each outlet's chain by the prefix its routes sit under.
var routerOutletChains = map[string]routerOutletChain{
{{- range .Outlets }}
{{- if .Machine }}
	"{{ .NotFoundPrefix }}": {
		group:  []string{ {{- if .AuthPackage }}"BindAuth(" + {{ .AuthPackage }}.Name + ")", {{ end }}"NoCaching", "CompressionMiddleware"},
		guards: []string{"{{ .StubPrefix }}ValidateToken"},
	},
{{- else }}
	"{{ .NotFoundPrefix }}": {
		group:  []string{ {{- if .AuthPackage }}"BindAuth(" + {{ .AuthPackage }}.Name + ")", {{ end }}"NoCaching", "CompressionMiddleware", "{{ .StubPrefix }}StartSession", "{{ .StubPrefix }}SetXSRFToken"},
		guards: []string{"{{ .StubPrefix }}ValidateSession", "{{ .StubPrefix }}ValidateXSRFToken"},
	},
{{- end }}
{{- end }}
}

// routerChainFor returns the chain of the outlet whose prefix the URL sits under.
func routerChainFor(t *testing.T, url string) routerOutletChain {
	t.Helper()

	for prefix, chain := range routerOutletChains {
		if strings.HasPrefix(url, prefix) {
			return chain
		}
	}
	t.Fatalf("%s sits under no outlet's prefix", url)

	return routerOutletChain{}
}

// routerSessionRoute is one session route: the outlet's method list, session, pending
// or sign-in handler, which answers behind the outlet's group and before its session
// guard; xsrf says it sits behind the outlet's XSRF guard, as a sign-in's POSTs do.
// suffix is the path under the outlet's prefix.
type routerSessionRoute struct {
	url       string
	suffix    string
	method    string
	handler   string
	xsrf      bool
	validated bool
}

// routerSessionRoutes lists every session outlet's routes, and the device-flow
// endpoints under each machine outlet's prefix, served by the issuing auth's surface
// in front of the token check.
func routerSessionRoutes() []routerSessionRoute {
	return []routerSessionRoute{
{{- range .SessionOutlets }}
{{- $outlet := . }}
{{- range .Routes }}
		{url: "{{ .Path }}", suffix: "{{ .Suffix }}", method: {{ .MethodConst }}, handler: "{{ $outlet.StubPrefix }}{{ .Handler }}"{{ if .XSRF }}, xsrf: true{{ end }}{{ if .Validated }}, validated: true{{ end }}},
{{- end }}
{{- end }}
{{- range .MachineOutlets }}
{{- $issuer := .Issuer }}
{{- range .DeviceRoutes }}
		{url: "{{ .Path }}", suffix: "{{ .Suffix }}", method: {{ .MethodConst }}, handler: "{{ $issuer.StubPrefix }}{{ .Handler }}"},
{{- end }}
{{- end }}
	}
}

// serveGeneratedRouter builds the router over recording stubs with the given hooks and
// serves one request through it.
func serveGeneratedRouter(t *testing.T, rec *routerCallRecorder, hooks Hooks, method, url string) *httptest.ResponseRecorder {
	t.Helper()

	return serveGeneratedRouterRequest(t, rec, "", hooks, httptest.NewRequestWithContext(t.Context(), method, url, http.NoBody))
}

// serveGeneratedRouterRequest builds the router over recording stubs whose server
// reports serverVersion, with the given hooks, and serves req through it.
func serveGeneratedRouterRequest(t *testing.T, rec *routerCallRecorder, serverVersion string, hooks Hooks, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()

	stub := newRouterHandlersStub(rec)
	stub.serverVersion = serverVersion
	router := New(stub, hooks)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	return rr
}

// TestGeneratedRouter drives every generated route through New: each request answers
// 200 from exactly its own handler with its route parameters resolved, having passed the
// every-request chain, its outlet's group, and its outlet's guards, in that order and
// nothing else.
func TestGeneratedRouter(t *testing.T) {
	t.Parallel()

	for _, tt := range generatedRouterTests() {
		t.Run(tt.method+"-url"+strings.ReplaceAll(tt.url, "/", "-"), func(t *testing.T) {
			t.Parallel()

			rec := newRouterCallRecorder()
			rr := serveGeneratedRouter(t, rec, Hooks{}, tt.method, tt.url)

			if got := rr.Code; got != http.StatusOK {
				t.Errorf("response.Code = %v, want %v", got, http.StatusOK)
			}
			if cnt := len(rec.handlers); cnt != 1 {
				t.Fatalf("expected 1 handler called, got: %v", rec.handlers)
			}
			if cnt := rec.handlers[tt.handlerFunc]; cnt != 1 {
				t.Fatalf("handler %s, expected 1 call, got: %d", tt.handlerFunc, cnt)
			}
			for key, value := range tt.parameters {
				if got := rec.Parameter(tt.handlerFunc, key); got != value {
					t.Fatalf("%s = %s, expected %s", key, got, value)
				}
			}

			chain := routerChainFor(t, tt.url)
			want := slices.Concat(routerRootChain, chain.group, chain.guards)
			if !slices.Equal(rec.chain, want) {
				t.Errorf("middleware chain = %v, want %v", rec.chain, want)
			}
		})
	}
}

{{ if .SessionOutlets -}}
// TestGeneratedRouterSessionRoutes proves each session outlet's method list, session,
// pending and sign-in routes answer behind the outlet's group and before its session
// guard, a sign-in's POSTs behind the XSRF guard alone: a browser reaches them without
// a validated session; the routes a signed-in person calls (an invitation's acceptance,
// the address, the token page) answer behind both guards; and a machine outlet's
// device-flow endpoints answer behind its group and before its token check.
func TestGeneratedRouterSessionRoutes(t *testing.T) {
	t.Parallel()

	for _, tt := range routerSessionRoutes() {
		t.Run(tt.method+"-url"+strings.ReplaceAll(tt.url, "/", "-"), func(t *testing.T) {
			t.Parallel()

			rec := newRouterCallRecorder()
			rr := serveGeneratedRouter(t, rec, Hooks{}, tt.method, tt.url)

			if got := rr.Code; got != http.StatusOK {
				t.Errorf("response.Code = %v, want %v", got, http.StatusOK)
			}
			if cnt := len(rec.handlers); cnt != 1 {
				t.Fatalf("expected 1 handler called, got: %v", rec.handlers)
			}
			if cnt := rec.handlers[tt.handler]; cnt != 1 {
				t.Fatalf("handler %s, expected 1 call, got: %d", tt.handler, cnt)
			}

			chain := routerChainFor(t, tt.url)
			want := slices.Concat(routerRootChain, chain.group)
			switch {
			case tt.validated:
				want = append(want, chain.guards...)
			case tt.xsrf:
				want = append(want, chain.guards[1])
			}
			if !slices.Equal(rec.chain, want) {
				t.Errorf("middleware chain = %v, want %v", rec.chain, want)
			}
		})
	}
}
{{ end }}
// TestGeneratedRouterNotFound proves the prefixes are closed: under every outlet's
// prefix an unknown path is 404 from the prefix's own not-found handler, never a browser
// application's entry document, and a route of one outlet addressed under another's
// prefix reaches no handler and no group, so nothing under one prefix enters another
// outlet's chain.
func TestGeneratedRouterNotFound(t *testing.T) {
	t.Parallel()

	unknown := []string{
{{- range .NotFoundPrefixes }}
		"{{ . }}does-not-exist",
{{- end }}
	}
	for _, url := range unknown {
		t.Run("GET-url"+strings.ReplaceAll(url, "/", "-"), func(t *testing.T) {
			t.Parallel()

			rec := newRouterCallRecorder()
			rr := serveGeneratedRouter(t, rec, Hooks{}, http.MethodGet, url)

			if got := rr.Code; got != http.StatusNotFound {
				t.Errorf("response.Code = %v, want %v", got, http.StatusNotFound)
			}
			if cnt := len(rec.handlers); cnt != 0 {
				t.Fatalf("expected no handler called, got: %v", rec.handlers)
			}
			if !slices.Equal(rec.chain, routerRootChain) {
				t.Errorf("middleware chain = %v, want %v", rec.chain, routerRootChain)
			}
		})
	}

	type foreign struct {
		url    string
		method string
	}
	var foreigns []foreign
{{- if .NegativeRouterTests }}
	foreigns = append(foreigns,
{{- range .NegativeRouterTests }}
		foreign{url: "{{ .URL }}", method: {{ .Method }}},
{{- end }}
	)
{{- end }}
	// Every session route addressed under every other outlet's prefix, unless that
	// outlet mounts the same route itself.
	sessionRoutes := routerSessionRoutes()
	for _, route := range sessionRoutes {
		for prefix := range routerOutletChains {
			if strings.HasPrefix(route.url, prefix) {
				continue
			}
			candidate := foreign{url: strings.TrimSuffix(prefix, "/") + route.suffix, method: route.method}
			if slices.ContainsFunc(sessionRoutes, func(r routerSessionRoute) bool {
				return r.url == candidate.url && r.method == candidate.method
			}) {
				continue
			}
			foreigns = append(foreigns, candidate)
		}
	}
	for _, tt := range foreigns {
		t.Run(tt.method+"-url"+strings.ReplaceAll(tt.url, "/", "-"), func(t *testing.T) {
			t.Parallel()

			rec := newRouterCallRecorder()
			rr := serveGeneratedRouter(t, rec, Hooks{}, tt.method, tt.url)

			if got := rr.Code; got == http.StatusOK {
				t.Errorf("response.Code = %v, want a refusal", got)
			}
			if cnt := len(rec.handlers); cnt != 0 {
				t.Fatalf("expected no handler called, got: %v", rec.handlers)
			}
			if !slices.Equal(rec.chain, routerRootChain) {
				t.Errorf("middleware chain = %v, want %v", rec.chain, routerRootChain)
			}
		})
	}
}
{{ if .WebApps }}
// TestGeneratedRouterWebApps proves each browser application answers at its mount path
// for any path beneath it, through its deep-link rewrite and nothing else{{ if .HasRootWebApp }}, and that the
// application at / is the catch-all{{ end }}.
func TestGeneratedRouterWebApps(t *testing.T) {
	t.Parallel()

	tests := []struct {
		url      string
		handler  string
		deepLink string
	}{
{{- range .WebApps }}
		{url: "{{ if ne .Mount "/" }}{{ .Mount }}{{ end }}/generated-router-page/deep/link", handler: "{{ .Assets }}", deepLink: "{{ .DeepLink }}"},
{{- end }}
	}
	for _, tt := range tests {
		t.Run("GET-url"+strings.ReplaceAll(tt.url, "/", "-"), func(t *testing.T) {
			t.Parallel()

			rec := newRouterCallRecorder()
			rr := serveGeneratedRouter(t, rec, Hooks{}, http.MethodGet, tt.url)

			if got := rr.Code; got != http.StatusOK {
				t.Errorf("response.Code = %v, want %v", got, http.StatusOK)
			}
			if cnt := rec.handlers[tt.handler]; cnt != 1 || len(rec.handlers) != 1 {
				t.Fatalf("handler %s, expected 1 call, got: %v", tt.handler, rec.handlers)
			}

			want := slices.Concat(routerRootChain, []string{tt.deepLink})
			if !slices.Equal(rec.chain, want) {
				t.Errorf("middleware chain = %v, want %v", rec.chain, want)
			}
		})
	}
}
{{ end }}
{{- if .RootRedirect }}
// TestGeneratedRouterRoot proves that with no browser application at / the root alone
// redirects to the {{ .RootRedirectOutlet }} outlet's application, calling no handler, and that an
// unmatched path outside every prefix and every mount is 404: no application answers for
// the whole origin.
func TestGeneratedRouterRoot(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		url      string
		code     int
		location string
	}{
		{name: "the root redirects", url: "/", code: http.StatusTemporaryRedirect, location: "{{ .RootRedirect }}"},
		{name: "an unmatched path is not found", url: "/generated-router-unmatched", code: http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := newRouterCallRecorder()
			rr := serveGeneratedRouter(t, rec, Hooks{}, http.MethodGet, tt.url)

			if got := rr.Code; got != tt.code {
				t.Errorf("response.Code = %v, want %v", got, tt.code)
			}
			if got := rr.Header().Get("Location"); got != tt.location {
				t.Errorf("Location = %q, want %q", got, tt.location)
			}
			if cnt := len(rec.handlers); cnt != 0 {
				t.Fatalf("expected no handler called, got: %v", rec.handlers)
			}
			if !slices.Equal(rec.chain, routerRootChain) {
				t.Errorf("middleware chain = %v, want %v", rec.chain, routerRootChain)
			}
		})
	}
}
{{ end }}
{{- if .ScheduledRoutes }}
// TestGeneratedRouterScheduled proves each scheduled route answers POST under the
// scheduled prefix from exactly its own handler, behind the every-request chain,
// NoCaching, CompressionMiddleware and SchedulerAuth and nothing else, so no session,
// outlet guard or hook stands in front of it, and that another method reaches no handler.
func TestGeneratedRouterScheduled(t *testing.T) {
	t.Parallel()

	tests := []struct {
		url     string
		handler string
	}{
{{- range .ScheduledRoutes }}
		{url: "{{ .Path }}", handler: "{{ .HandlerFunc }}"},
{{- end }}
	}
	for _, tt := range tests {
		t.Run("POST-url"+strings.ReplaceAll(tt.url, "/", "-"), func(t *testing.T) {
			t.Parallel()

			rec := newRouterCallRecorder()
			rr := serveGeneratedRouter(t, rec, Hooks{}, http.MethodPost, tt.url)

			if got := rr.Code; got != http.StatusOK {
				t.Errorf("response.Code = %v, want %v", got, http.StatusOK)
			}
			if cnt := rec.handlers[tt.handler]; cnt != 1 || len(rec.handlers) != 1 {
				t.Fatalf("handler %s, expected 1 call, got: %v", tt.handler, rec.handlers)
			}
			want := slices.Concat(routerRootChain, []string{"NoCaching", "CompressionMiddleware", "SchedulerAuth"})
			if !slices.Equal(rec.chain, want) {
				t.Errorf("middleware chain = %v, want %v", rec.chain, want)
			}
		})
		t.Run("GET-url"+strings.ReplaceAll(tt.url, "/", "-"), func(t *testing.T) {
			t.Parallel()

			rec := newRouterCallRecorder()
			rr := serveGeneratedRouter(t, rec, Hooks{}, http.MethodGet, tt.url)

			if got := rr.Code; got == http.StatusOK {
				t.Errorf("response.Code = %v, want a refusal", got)
			}
			if cnt := len(rec.handlers); cnt != 0 {
				t.Fatalf("expected no handler called, got: %v", rec.handlers)
			}
		})
	}
}
{{ end }}
// TestGeneratedRouterHooks proves the seams sit where the chain comment says: Outermost
// runs ahead of the logger, Root's routes sit behind the every-request chain alone, each
// outlet's hook runs inside the outlet's guards with the generated routes registering
// through it, and a hook that skips the generated registration is refused.
func TestGeneratedRouterHooks(t *testing.T) {
	t.Parallel()

	hooksFor := func(rec *routerCallRecorder) Hooks {
		return Hooks{
			Outermost: []func(http.Handler) http.Handler{rec.Middleware("Outermost")},
			Root: func(r chi.Router) {
				r.Get("/generated-router-probe", rec.RecordHandlerCall("RootProbe"))
			},
{{- range .Outlets }}
			{{ .HookField }}: func(r chi.Router, generated func(chi.Router)) {
				r.Use(rec.Middleware("{{ .HookField }}Hook"))
				r.Get("{{ .NotFoundPrefix }}generated-router-probe", rec.RecordHandlerCall("{{ .HookField }}Probe"))
				generated(r)
			},
{{- end }}
		}
	}
	outermost := slices.Concat([]string{"Outermost"}, routerRootChain)

	type hookCase struct {
		name    string
		url     string
		method  string
		handler string
		chain   []string
	}
	tests := []hookCase{
		{name: "a root route", url: "/generated-router-probe", method: http.MethodGet, handler: "RootProbe", chain: outermost},
{{- range .Outlets }}
		{
			name: "the {{ .Name }} outlet's hook", url: "{{ .NotFoundPrefix }}generated-router-probe", method: http.MethodGet, handler: "{{ .HookField }}Probe",
			chain: slices.Concat(outermost, routerOutletChains["{{ .NotFoundPrefix }}"].group, routerOutletChains["{{ .NotFoundPrefix }}"].guards, []string{"{{ .HookField }}Hook"}),
		},
{{- end }}
	}
	// Every generated route registers through its outlet's hook, so it passes the
	// hook's middleware too.
	for _, route := range generatedRouterTests() {
		for prefix, chain := range routerOutletChains {
			if !strings.HasPrefix(route.url, prefix) {
				continue
			}
			hook := prefixHookFields[prefix]
			tests = append(tests, hookCase{
				name: "a generated route registered through " + hook, url: route.url, method: route.method, handler: route.handlerFunc,
				chain: slices.Concat(outermost, chain.group, chain.guards, []string{hook + "Hook"}),
			})

			break
		}
	}
	for _, tt := range tests {
		t.Run(tt.name+" "+tt.method+"-url"+strings.ReplaceAll(tt.url, "/", "-"), func(t *testing.T) {
			t.Parallel()

			rec := newRouterCallRecorder()
			rr := serveGeneratedRouter(t, rec, hooksFor(rec), tt.method, tt.url)

			if got := rr.Code; got != http.StatusOK {
				t.Errorf("response.Code = %v, want %v", got, http.StatusOK)
			}
			if cnt := rec.handlers[tt.handler]; cnt != 1 || len(rec.handlers) != 1 {
				t.Fatalf("handler %s, expected 1 call, got: %v", tt.handler, rec.handlers)
			}
			if !slices.Equal(rec.chain, tt.chain) {
				t.Errorf("middleware chain = %v, want %v", rec.chain, tt.chain)
			}
		})
	}

	t.Run("a hook that skips the generated registration is refused", func(t *testing.T) {
		t.Parallel()

		defer func() {
			msg, _ := recover().(string)
			if !strings.Contains(msg, "hooks.{{ (index .Outlets 0).HookField }} must call generated exactly once") {
				t.Errorf("New() panic = %q, want the hook named", msg)
			}
		}()
		New(newRouterHandlersStub(newRouterCallRecorder()), Hooks{
			{{ (index .Outlets 0).HookField }}: func(chi.Router, func(chi.Router)) {},
		})
		t.Error("New() returned; want a panic")
	})
}

// prefixHookFields names each outlet's hook by the prefix its routes sit under.
var prefixHookFields = map[string]string{
{{- range .Outlets }}
	"{{ .NotFoundPrefix }}": "{{ .HookField }}",
{{- end }}
}

// requestLogOutput collects what the console exporter writes through the standard
// logger while the request log test runs.
type requestLogOutput struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (o *requestLogOutput) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	return o.buf.Write(p)
}

func (o *requestLogOutput) reset() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.buf.Reset()
}

func (o *requestLogOutput) String() string {
	o.mu.Lock()
	defer o.mu.Unlock()

	return o.buf.String()
}

// requestLogProbe answers the status and logs nothing, so the request's entry is the
// word's decision alone.
func requestLogProbe(status int) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
	}
}

// requestLogOwnWord sets the word on its own request, as a handler that knows its request
// matters, or does not, may, then answers 200.
func requestLogOwnWord(word logger.Policy) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		logger.FromReq(r).SetPolicy(word)
		w.WriteHeader(http.StatusOK)
	}
}

// requestLogHooks registers the probes the request log test drives, each under the
// surface its case names: through hooks.Root outside every outlet, and through each
// outlet's hook inside its group.
func requestLogHooks() Hooks {
	return Hooks{
		Root: func(r chi.Router) {
{{- range .RequestLogCases }}
{{- if eq .Hook "Root" }}
			r.{{ Pascal .Method }}("{{ .Path }}", {{ if .SetWord }}requestLogOwnWord({{ .SetWord }}){{ else }}requestLogProbe({{ .Status }}){{ end }})
{{- end }}
{{- end }}
		},
{{- range $outlet := .Outlets }}
		{{ $outlet.HookField }}: func(r chi.Router, generated func(chi.Router)) {
{{- range $.RequestLogCases }}
{{- if eq .Hook $outlet.HookField }}
			r.{{ Pascal .Method }}("{{ .Path }}", {{ if .SetWord }}requestLogOwnWord({{ .SetWord }}){{ else }}requestLogProbe({{ .Status }}){{ end }})
{{- end }}
{{- end }}
			generated(r)
		},
{{- end }}
	}
}

// TestGeneratedRouterRequestLog proves the request log words land where the chain comment
// says, with the console exporter deciding each request's entry: the application default
// on a root route, quiet and failed; an outlet's word on a route inside its group, quiet
// and failed, and an outlet without one taking the default; a route's own word on the
// generated route; a prefix mounted by hand keeping its word; and a handler setting its
// own request's word. A sampled word leaves a quiet request to the draw, which the test
// does not assert. The test runs on its own, not in parallel: the console exporter writes
// through the standard logger, which the test redirects to read the decisions.
func TestGeneratedRouterRequestLog(t *testing.T) {
	tests := []struct {
		name   string
		method string
		url    string
		status int
		// want is the decision: written, dropped, or empty when the draw decides.
		want string
	}{
{{- range .RequestLogCases }}
		{name: "{{ .Name }}", method: {{ .MethodConst }}, url: "{{ .Path }}", status: {{ .Status }}, want: "{{ .Want }}"},
{{- end }}
	}
	var output requestLogOutput
	previous := log.Writer()
	log.SetOutput(&output)
	t.Cleanup(func() { log.SetOutput(previous) })
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output.reset()
			stub := newRouterHandlersStub(newRouterCallRecorder())
			stub.exporter = logger.NewConsoleExporter().NoColor(true)
			router := New(stub, requestLogHooks())
			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, httptest.NewRequestWithContext(t.Context(), tt.method, tt.url, http.NoBody))

			if got := rr.Code; got != tt.status {
				t.Fatalf("response.Code = %v, want %v", got, tt.status)
			}
			entry := tt.method + " " + tt.url + " " + strconv.Itoa(tt.status)
			written := strings.Contains(output.String(), entry)
			switch {
			case tt.want == "written" && !written:
				t.Errorf("the request's entry %q was not written; the console wrote:\n%s", entry, output.String())
			case tt.want == "dropped" && written:
				t.Errorf("the request's entry %q was written; the console wrote:\n%s", entry, output.String())
			}
		})
	}
}
{{ if .SessionOutlets }}
// routerVersionProbe is one session outlet's version check as the generator declared
// it: the prefix its routes sit under, the releases the test sends through it, and the
// stored-file routes answered at any release.
type routerVersionProbe struct {
	prefix string
	// server is the release the stub reports; inRange the oldest answered release, or
	// the first release where every release is answered; below a release under the
	// oldest answered, empty where every release is answered; above a release past the
	// server's.
	server, inRange, below, above string
	files                         []routerFileRoute
}

// routerFileRoute is one stored-file route: its test URL and handler.
type routerFileRoute struct {
	url, handler string
}

// routerVersionProbes lists every session outlet's probe.
func routerVersionProbes() []routerVersionProbe {
	return []routerVersionProbe{
{{- range .SessionOutlets }}
		{
			prefix: "{{ .NotFoundPrefix }}", server: "{{ .Probes.Server }}", inRange: "{{ .Probes.InRange }}", below: "{{ .Probes.Below }}", above: "{{ .Probes.Above }}",
{{- if .FileRoutes }}
			files: []routerFileRoute{
{{- range .FileRoutes }}
				{url: "{{ .TestURL }}", handler: "{{ .Handler }}"},
{{- end }}
			},
{{- end }}
		},
{{- end }}
	}
}

// routerUnreadBody fails a case that reads it: a refused request's body is never read.
type routerUnreadBody struct {
	read bool
}

func (b *routerUnreadBody) Read([]byte) (int, error) {
	b.read = true

	return 0, io.EOF
}

// TestGeneratedRouterAPIVersion proves each session outlet's version check: a request
// whose X-Api-Version is between the outlet's oldest answered release and the server's
// is answered with Vary: X-Api-Version, one below or above is refused with 412 and the
// server's release in X-Api-Version before any handler runs or its body is read, a
// request without the header or with a version that is not a release on either side
// is answered, the session routes and the stored-file routes answer at any release,
// a machine outlet is never checked, every answer of an outlet names the server's
// release in X-Api-Version, and the check sits behind the outlet's guards and ahead
// of its hook.
func TestGeneratedRouterAPIVersion(t *testing.T) {
	t.Parallel()

	type versionCase struct {
		name    string
		server  string
		app     string
		method  string
		url     string
		handler string
		refused bool
		varied  bool
	}
	var tests []versionCase
	for _, probe := range routerVersionProbes() {
		where := strings.ReplaceAll(probe.prefix, "/", "-")
		digest := probe.prefix + "permission-digest"
		checked := func(name, server, app string) versionCase {
			return versionCase{name: where + name, server: server, app: app, method: http.MethodGet, url: digest, handler: "PermissionDigest", varied: true}
		}
		refused := func(name, app string) versionCase {
			return versionCase{name: where + name, server: probe.server, app: app, method: http.MethodGet, url: digest, refused: true}
		}
		tests = append(tests,
			checked("the oldest answered release", probe.server, probe.inRange),
			checked("the server's release", probe.server, probe.server),
			checked("no header", probe.server, ""),
			checked("an application that is not a release", probe.server, "dev"),
			checked("a server that is not a release", "dev", probe.above),
			refused("a release above the server's", probe.above),
		)
		if probe.below != "" {
			tests = append(tests, refused("a release below the oldest answered", probe.below))
		}
		for _, route := range routerSessionRoutes() {
			if strings.HasPrefix(route.url, probe.prefix) {
				tests = append(tests, versionCase{name: where + "a session route at any release " + route.method + strings.ReplaceAll(route.suffix, "/", "-"), server: probe.server, app: probe.above, method: route.method, url: route.url, handler: route.handler})
			}
		}
		for _, file := range probe.files {
			tests = append(tests, versionCase{name: where + "a stored-file route at any release " + file.handler, server: probe.server, app: probe.above, method: http.MethodGet, url: file.url, handler: file.handler})
		}
	}
{{- range .MachineOutlets }}
	for _, route := range generatedRouterTests() {
		if strings.HasPrefix(route.url, "{{ .NotFoundPrefix }}") {
			tests = append(tests, versionCase{name: "{{ .Name }} outlet is not checked", server: "2.0.0", app: "99.0.0", method: route.method, url: route.url, handler: route.handlerFunc})

			break
		}
	}
{{- end }}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := newRouterCallRecorder()
			body := &routerUnreadBody{}
			req := httptest.NewRequestWithContext(t.Context(), tt.method, tt.url, body)
			if tt.app != "" {
				req.Header.Set(resource.APIVersionHeader, tt.app)
			}
			rr := serveGeneratedRouterRequest(t, rec, tt.server, Hooks{}, req)

			if tt.refused {
				if got := rr.Code; got != http.StatusPreconditionFailed {
					t.Fatalf("response.Code = %v, want %v", got, http.StatusPreconditionFailed)
				}
				if got := rr.Header().Get(resource.APIVersionHeader); got != tt.server {
					t.Errorf("%s = %q, want the server's %q", resource.APIVersionHeader, got, tt.server)
				}
				if cnt := len(rec.handlers); cnt != 0 {
					t.Errorf("expected no handler called, got: %v", rec.handlers)
				}
				if body.read {
					t.Error("the refused request's body was read")
				}

				return
			}
			if got := rr.Code; got != http.StatusOK {
				t.Fatalf("response.Code = %v, want %v", got, http.StatusOK)
			}
			if cnt := rec.handlers[tt.handler]; cnt != 1 || len(rec.handlers) != 1 {
				t.Fatalf("handler %s, expected 1 call, got: %v", tt.handler, rec.handlers)
			}
			if got := slices.Contains(rr.Header().Values("Vary"), resource.APIVersionHeader); got != tt.varied {
				t.Errorf("Vary carries %s = %v, want %v", resource.APIVersionHeader, got, tt.varied)
			}
			if got := rr.Header().Get(resource.APIVersionHeader); got != tt.server {
				t.Errorf("%s = %q, want the server's %q: every answer of an outlet names the release", resource.APIVersionHeader, got, tt.server)
			}
		})
	}

	// The check sits behind the outlet's guards and ahead of its hook: a refused request
	// passes the group and the guards and never reaches the hook's middleware.
	t.Run("a refused request never reaches the hook", func(t *testing.T) {
		t.Parallel()

		probe := routerVersionProbes()[0]
		rec := newRouterCallRecorder()
		hooks := Hooks{
			{{ (index .SessionOutlets 0).HookField }}: func(r chi.Router, generated func(chi.Router)) {
				r.Use(rec.Middleware("{{ (index .SessionOutlets 0).HookField }}Hook"))
				generated(r)
			},
		}
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, probe.prefix+"permission-digest", http.NoBody)
		req.Header.Set(resource.APIVersionHeader, probe.above)
		rr := serveGeneratedRouterRequest(t, rec, probe.server, hooks, req)

		if got := rr.Code; got != http.StatusPreconditionFailed {
			t.Fatalf("response.Code = %v, want %v", got, http.StatusPreconditionFailed)
		}
		chain := routerChainFor(t, probe.prefix+"permission-digest")
		if want := slices.Concat(routerRootChain, chain.group, chain.guards); !slices.Equal(rec.chain, want) {
			t.Errorf("middleware chain = %v, want %v", rec.chain, want)
		}
	})
}
{{ end }}

// routerCallRecorder records the order middleware ran in, beside the handler and
// route-parameter recording the routes test's recorder provides.
type routerCallRecorder struct {
	*generatedCallRecorder
	chain []string
}

func newRouterCallRecorder() *routerCallRecorder {
	return &routerCallRecorder{generatedCallRecorder: newGeneratedCallRecorder()}
}

// Middleware returns middleware that appends its name to the chain when it runs.
func (rec *routerCallRecorder) Middleware(name string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rec.chain = append(rec.chain, name)
			next.ServeHTTP(w, r)
		})
	}
}
{{ range .SessionOutlets }}
// {{ .StubType }} records the {{ .Name }} outlet's session surface under a name prefix, so a
// chain says whose session a request passed.
type {{ .StubType }} struct {
	rec    *routerCallRecorder
	prefix string
}
{{ $stub := .StubType }}
{{- range .Routes }}
{{- if eq .Handler "Methods" }}
func (s *{{ $stub }}) Methods(*session.MethodList) http.HandlerFunc {
	return s.rec.RecordHandlerCall(s.prefix + "Methods")
}
{{ else }}
func (s *{{ $stub }}) {{ .Handler }}() http.HandlerFunc {
	return s.rec.RecordHandlerCall(s.prefix + "{{ .Handler }}")
}
{{ end }}
{{- end }}
{{- range .ExtraHandlers }}
func (s *{{ $stub }}) {{ . }}() http.HandlerFunc {
	return s.rec.RecordHandlerCall(s.prefix + "{{ . }}")
}
{{ end }}
func (s *{{ $stub }}) StartSession(next http.Handler) http.Handler {
	return s.rec.Middleware(s.prefix + "StartSession")(next)
}

func (s *{{ $stub }}) SetXSRFToken(next http.Handler) http.Handler {
	return s.rec.Middleware(s.prefix + "SetXSRFToken")(next)
}

func (s *{{ $stub }}) ValidateSession(next http.Handler) http.Handler {
	return s.rec.Middleware(s.prefix + "ValidateSession")(next)
}

func (s *{{ $stub }}) ValidateXSRFToken(next http.Handler) http.Handler {
	return s.rec.Middleware(s.prefix + "ValidateXSRFToken")(next)
}
{{ end }}
{{- range .MachineOutlets }}
// {{ .StubType }} records the {{ .Name }} outlet's token check under a name prefix, so a chain
// says whose tokens a request passed.
type {{ .StubType }} struct {
	rec    *routerCallRecorder
	prefix string
}

func (s *{{ .StubType }}) ValidateToken(next http.Handler) http.Handler {
	return s.rec.Middleware(s.prefix + "ValidateToken")(next)
}
{{ end }}
// routerHandlersStub satisfies Handlers for the router tests: the generated surface
// through the routes test's stub, the session surfaces through the outlets' session
// stubs, the token surfaces through the machine outlets' token stubs, and every
// middleware and browser-application handler recording under its own name.
type routerHandlersStub struct {
	*generatedHandlersStub
{{- range .SessionOutlets }}
{{- if not .Getter }}
	*{{ .StubType }}
{{- end }}
{{- end }}
{{- range .MachineOutlets }}
{{- if not .Getter }}
	*{{ .StubType }}
{{- end }}
{{- end }}
	rec *routerCallRecorder
{{- range .SessionOutlets }}
{{- if .Getter }}
	{{ .StubField }} *{{ .StubType }}
{{- end }}
{{- end }}
{{- range .MachineOutlets }}
{{- if .Getter }}
	{{ .StubField }} *{{ .StubType }}
{{- end }}
{{- end }}
	// serverVersion is the release the stub reports; empty, no release, so a case that
	// sets none is never refused.
	serverVersion string
	// exporter is where the request log goes when a test sets one, the console for the
	// request log test; nil hands the router the recording stub.
	exporter logger.Exporter
}

func (s *routerHandlersStub) ServerVersion() string {
	return s.serverVersion
}
{{- if .FileStores }}

// ResourceClient answers with a mock client carrying every file store New requires,
// each a stub: the router test never reaches a store.
func (s *routerHandlersStub) ResourceClient() resource.Client {
	return resource.NewMockClient(nil, nil, nil{{ range .FileStores }}, {{ .OptionExpr }}{{ end }})
}

// stubFileStore satisfies resource.FileStore for the router test.
type stubFileStore struct{}

func (stubFileStore) Put(context.Context, string, string, io.Reader) error {
	return nil
}

func (stubFileStore) Delete(context.Context, []string) error {
	return nil
}

func (stubFileStore) Open(context.Context, string) (*resource.Content, error) {
	return nil, nil
}
{{- end }}

func newRouterHandlersStub(rec *routerCallRecorder) *routerHandlersStub {
	return &routerHandlersStub{
		generatedHandlersStub: newGeneratedHandlersStub(rec.RecordHandlerCall),
{{- range .SessionOutlets }}
{{- if not .Getter }}
		{{ .StubType }}: &{{ .StubType }}{rec: rec},
{{- end }}
{{- end }}
{{- range .MachineOutlets }}
{{- if not .Getter }}
		{{ .StubType }}: &{{ .StubType }}{rec: rec},
{{- end }}
{{- end }}
		rec: rec,
{{- range .SessionOutlets }}
{{- if .Getter }}
		{{ .StubField }}: &{{ .StubType }}{rec: rec, prefix: "{{ .StubPrefix }}"},
{{- end }}
{{- end }}
{{- range .MachineOutlets }}
{{- if .Getter }}
		{{ .StubField }}: &{{ .StubType }}{rec: rec, prefix: "{{ .StubPrefix }}"},
{{- end }}
{{- end }}
	}
}
{{ range .SessionOutlets }}
{{- if .Getter }}
func (s *routerHandlersStub) {{ .Getter }}() {{ .SessionInterface }} {
	return s.{{ .StubField }}
}
{{ end }}
{{- end }}
{{- range .MachineOutlets }}
{{- if .Getter }}
func (s *routerHandlersStub) {{ .Getter }}() {{ .TokensInterface }} {
	return s.{{ .StubField }}
}
{{ end }}
{{- end }}
{{- if .MultiAuth }}
func (s *routerHandlersStub) BindAuth(name string) func(http.Handler) http.Handler {
	return s.rec.Middleware("BindAuth(" + name + ")")
}
{{ end }}
{{- if .ScheduledRoutes }}
func (s *routerHandlersStub) SchedulerAuth(next http.Handler) http.Handler {
	return s.rec.Middleware("SchedulerAuth")(next)
}
{{ range .ScheduledRoutes }}
func (s *routerHandlersStub) {{ .HandlerFunc }}() http.HandlerFunc {
	return s.rec.RecordHandlerCall("{{ .HandlerFunc }}")
}
{{ end }}
{{- end }}
func (s *routerHandlersStub) LogExporter() logger.Exporter {
	if s.exporter != nil {
		return s.exporter
	}

	return &routerLogExporterStub{rec: s.rec}
}

// routerLogExporterStub is the exporter the stub hands the router: its middleware
// records under LogExporter, so the request logger's place in the chain is proven; the
// runner and daemon context pass straight through.
type routerLogExporterStub struct {
	rec *routerCallRecorder
}

func (s *routerLogExporterStub) Middleware() func(http.Handler) http.Handler {
	return s.rec.Middleware("LogExporter")
}

func (s *routerLogExporterStub) CliRunner() func(ctx context.Context, command string, f func(context.Context) error) error {
	return func(ctx context.Context, _ string, f func(context.Context) error) error {
		return f(ctx)
	}
}

func (s *routerLogExporterStub) DaemonContext(ctx context.Context) context.Context {
	return ctx
}

func (s *routerHandlersStub) SecurityHeaders(next http.Handler) http.Handler {
	return s.rec.Middleware("SecurityHeaders")(next)
}

func (s *routerHandlersStub) NoCaching(next http.Handler) http.Handler {
	return s.rec.Middleware("NoCaching")(next)
}

func (s *routerHandlersStub) CompressionMiddleware() func(http.Handler) http.Handler {
	return s.rec.Middleware("CompressionMiddleware")
}
{{ range .WebApps }}
func (s *routerHandlersStub) {{ .DeepLink }}(next http.Handler) http.Handler {
	return s.rec.Middleware("{{ .DeepLink }}")(next)
}

func (s *routerHandlersStub) {{ .Assets }}() http.HandlerFunc {
	return s.rec.RecordHandlerCall("{{ .Assets }}")
}
{{ end -}}
`
)
