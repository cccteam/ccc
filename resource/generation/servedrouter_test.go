package generation

import (
	"strconv"
	"strings"
	"testing"

	"cloud.google.com/go/logging"
	"github.com/google/go-cmp/cmp"
)

// Test_routerOutletOptions pins the outlet options' own checks: an import path and a
// known flavor for Auth, a mount path for WebApp, and one declaration of each per outlet.
func Test_routerOutletOptions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		options []OutletOption
		want    routerOutlet
		wantErr string
	}{
		{
			name:    "a session auth serves sessions",
			options: []OutletOption{Auth("example.com/acme/beacon/pkg/auth/staff", Password)},
			want:    routerOutlet{servesSessions: true, auth: &outletAuth{importPath: "example.com/acme/beacon/pkg/auth/staff", flavor: Password}},
		},
		{
			name:    "an API key with a web app is recorded; the contradiction is the generator's to refuse",
			options: []OutletOption{APIKey(), WebApp("/machines")},
			want:    routerOutlet{apiKey: true, webApp: "/machines"},
		},
		{
			name:    "ServesSessions is remembered as declared",
			options: []OutletOption{ServesSessions()},
			want:    routerOutlet{servesSessions: true, declaredSessions: true},
		},
		{name: "an empty import path", options: []OutletOption{Auth("", Password)}, wantErr: "requires an import path"},
		{name: "a slash-wrapped import path", options: []OutletOption{Auth("/pkg/auth/staff/", Password)}, wantErr: "requires an import path"},
		{name: "an unknown flavor", options: []OutletOption{Auth("example.com/pkg/auth/staff", AuthFlavor("ldap"))}, wantErr: "unknown flavor"},
		{name: "two auths", options: []OutletOption{Auth("example.com/pkg/auth/staff", Password), Auth("example.com/pkg/auth/members", OIDCAzure)}, wantErr: "redeclares the outlet's auth"},
		{name: "a relative mount path", options: []OutletOption{WebApp("portal")}, wantErr: "requires a mount path starting with '/'"},
		{name: "a trailing slash", options: []OutletOption{WebApp("/portal/")}, wantErr: "without a trailing '/'"},
		{name: "a wildcard mount path", options: []OutletOption{WebApp("/portal/*")}, wantErr: "requires a mount path"},
		{name: "two web apps", options: []OutletOption{WebApp("/"), WebApp("/portal")}, wantErr: "redeclares the outlet's browser application"},
		{
			name:    "an oldest answered release is remembered as written",
			options: []OutletOption{Auth("example.com/acme/beacon/pkg/auth/staff", Password), OldestAnswered("1.5.0")},
			want:    routerOutlet{servesSessions: true, auth: &outletAuth{importPath: "example.com/acme/beacon/pkg/auth/staff", flavor: Password}, oldestAnswered: "1.5.0", declaredOldest: true},
		},
		{
			name:    "an oldest answered release with a leading v",
			options: []OutletOption{OldestAnswered("v2.0.0-rc1")},
			want:    routerOutlet{oldestAnswered: "v2.0.0-rc1", declaredOldest: true},
		},
		{
			name:    "this release as the oldest answered",
			options: []OutletOption{OldestAnswered(ThisRelease)},
			want:    routerOutlet{oldestAnswered: ThisRelease, declaredOldest: true},
		},
		{name: "an empty oldest answered", options: []OutletOption{OldestAnswered("")}, wantErr: `OldestAnswered("") names no release`},
		{name: "an oldest answered that is not a release", options: []OutletOption{OldestAnswered("dev")}, wantErr: `OldestAnswered("dev") names no release: write a semantic version such as "1.5.0", or ThisRelease`},
		{name: "an oldest answered with four parts", options: []OutletOption{OldestAnswered("1.5.0.2")}, wantErr: "names no release"},
		{name: "two oldest answered releases", options: []OutletOption{OldestAnswered("1.5.0"), OldestAnswered("1.6.0")}, wantErr: `OldestAnswered("1.6.0") redeclares the outlet's oldest answered release ("1.5.0")`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var got routerOutlet
			var err error
			for _, opt := range tt.options {
				if err = opt.applyToOutlet(&got); err != nil {
					break
				}
			}
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("applyToOutlet() error = %v, want containing %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("applyToOutlet() error = %v", err)
			}
			if got.servesSessions != tt.want.servesSessions || got.declaredSessions != tt.want.declaredSessions || got.apiKey != tt.want.apiKey || got.webApp != tt.want.webApp || got.oldestAnswered != tt.want.oldestAnswered || got.declaredOldest != tt.want.declaredOldest {
				t.Errorf("outlet = %+v, want %+v", got, tt.want)
			}
			switch {
			case (got.auth == nil) != (tt.want.auth == nil):
				t.Errorf("auth = %+v, want %+v", got.auth, tt.want.auth)
			case got.auth != nil && *got.auth != *tt.want.auth:
				t.Errorf("auth = %+v, want %+v", *got.auth, *tt.want.auth)
			}
		})
	}
}

// Test_validateRouterConfig pins the agreements between GenerateRouter and the outlet
// declarations: the router options are refused without the switch, every outlet
// authenticates one way with it, the reserved names stay free, the browser
// applications' mount paths are distinct and beside the API prefixes, and an
// application at / is the only one.
func Test_validateRouterConfig(t *testing.T) {
	t.Parallel()

	staff := Auth("example.com/acme/beacon/pkg/auth/staff", Password)
	members := Auth("example.com/acme/beacon/pkg/auth/members", OIDCGoogle)

	tests := []struct {
		name    string
		options []ResourceOption
		wantErr string
	}{
		{
			name:    "a hand router keeps today's declarations",
			options: []ResourceOption{GenerateRoutes("pkg/router", "api"), WithRouterOutlet("portal", "portal/api", ServesSessions())},
		},
		{
			name:    "Auth without the switch",
			options: []ResourceOption{GenerateRoutes("pkg/router", "api", staff)},
			wantErr: `outlet "default" declares Auth, which describes the generated router`,
		},
		{
			name:    "APIKey without the switch",
			options: []ResourceOption{GenerateRoutes("pkg/router", "api"), WithRouterOutlet("machines", "machines", APIKey())},
			wantErr: `outlet "machines" declares APIKey`,
		},
		{
			name:    "WebApp without the switch",
			options: []ResourceOption{GenerateRoutes("pkg/router", "api", WebApp("/"))},
			wantErr: `outlet "default" declares WebApp`,
		},
		{
			name:    "OldestAnswered without the switch",
			options: []ResourceOption{GenerateRoutes("pkg/router", "api", OldestAnswered("1.5.0"))},
			wantErr: `outlet "default" declares OldestAnswered, which describes the generated router`,
		},
		{
			name: "oldest answered releases on the session outlets",
			options: []ResourceOption{
				GenerateRouter(),
				GenerateRoutes("pkg/router", "api", staff, WebApp("/console"), OldestAnswered("1.5.0")),
				WithRouterOutlet("portal", "portal/api", members, WebApp("/portal"), OldestAnswered(ThisRelease)),
				WithRouterOutlet("machines", "machines", APIKey()),
			},
		},
		{
			name:    "an API key with an oldest answered release",
			options: []ResourceOption{GenerateRouter(), GenerateRoutes("pkg/router", "api", staff), WithRouterOutlet("machines", "machines", APIKey(), OldestAnswered("1.5.0"))},
			wantErr: `outlet "machines" declares APIKey and OldestAnswered("1.5.0"): a machine outlet's clients carry no release, so nothing is checked against one; the option belongs on a session outlet`,
		},
		{
			name:    "the switch without routes",
			options: []ResourceOption{GenerateRouter()},
			wantErr: "GenerateRouter requires GenerateRoutes",
		},
		{
			name: "the full shape",
			options: []ResourceOption{
				GenerateRouter(),
				GenerateRoutes("pkg/router", "api", staff, WebApp("/console")),
				WithRouterOutlet("portal", "portal/api", members, WebApp("/portal")),
				WithRouterOutlet("machines", "machines", APIKey()),
			},
		},
		{
			name:    "one browser application at the root alone",
			options: []ResourceOption{GenerateRouter(), GenerateRoutes("pkg/router", "api", staff, WebApp("/")), WithRouterOutlet("portal", "portal/api", members)},
		},
		{
			name:    "a browser application at the root beside a second one",
			options: []ResourceOption{GenerateRouter(), GenerateRoutes("pkg/router", "api", staff, WebApp("/")), WithRouterOutlet("portal", "portal/api", members, WebApp("/portal"))},
			wantErr: `outlet "default" declares WebApp("/") beside outlet "portal"'s WebApp("/portal"): an installed browser application's scope is every URL under its start, so the application at / owns the origin, the one under /portal never gets its own install prompt, and its notifications and links are attributed to the application at /; with two browser applications none is mounted at /, so mount the default outlet's application under a path such as /console`,
		},
		{
			name:    "a second outlet's browser application at the root beside the default outlet's",
			options: []ResourceOption{GenerateRouter(), GenerateRoutes("pkg/router", "api", staff, WebApp("/console")), WithRouterOutlet("portal", "portal/api", members, WebApp("/"))},
			wantErr: `outlet "portal" declares WebApp("/") beside outlet "default"'s WebApp("/console")`,
		},
		{
			name:    "an outlet that says nothing",
			options: []ResourceOption{GenerateRouter(), GenerateRoutes("pkg/router", "api", staff), WithRouterOutlet("portal", "portal/api")},
			wantErr: `outlet "portal" declares neither Auth nor APIKey`,
		},
		{
			name:    "the default outlet that says nothing",
			options: []ResourceOption{GenerateRouter(), GenerateRoutes("pkg/router", "api")},
			wantErr: `outlet "default" declares neither Auth nor APIKey`,
		},
		{
			name:    "both ways at once",
			options: []ResourceOption{GenerateRouter(), GenerateRoutes("pkg/router", "api", staff, APIKey())},
			wantErr: `outlet "default" declares both Auth and APIKey`,
		},
		{
			name:    "an API key that serves sessions",
			options: []ResourceOption{GenerateRouter(), GenerateRoutes("pkg/router", "api", staff), WithRouterOutlet("machines", "machines", APIKey(), ServesSessions())},
			wantErr: `outlet "machines" declares APIKey and ServesSessions`,
		},
		{
			name:    "an API key with a browser application",
			options: []ResourceOption{GenerateRouter(), GenerateRoutes("pkg/router", "api", staff), WithRouterOutlet("machines", "machines", APIKey(), WebApp("/machines"))},
			wantErr: `outlet "machines" declares APIKey and WebApp("/machines")`,
		},
		{
			name:    "an outlet named after a reserved hook",
			options: []ResourceOption{GenerateRouter(), GenerateRoutes("pkg/router", "api", staff), WithRouterOutlet("root", "root", APIKey())},
			wantErr: `outlet "root" would take the Hooks field Root`,
		},
		{
			name:    "an API-key outlet whose middleware would be BindAuth",
			options: []ResourceOption{GenerateRouter(), GenerateRoutes("pkg/router", "api", staff), WithRouterOutlet("bind", "bind", APIKey())},
			wantErr: `outlet "bind" would take the Handlers method BindAuth`,
		},
		{
			name:    "two outlets on one mount path",
			options: []ResourceOption{GenerateRouter(), GenerateRoutes("pkg/router", "api", staff, WebApp("/")), WithRouterOutlet("portal", "portal/api", members, WebApp("/"))},
			wantErr: `outlet "portal" declares WebApp("/"), which outlet "default" already serves`,
		},
		{
			name:    "a browser application under an API prefix",
			options: []ResourceOption{GenerateRouter(), GenerateRoutes("pkg/router", "api", staff, WebApp("/api/console"))},
			wantErr: `WebApp("/api/console"), which sits under outlet "default"'s route prefix /api`,
		},
		{
			name:    "an outlet prefix at the scheduled prefix",
			options: []ResourceOption{GenerateRouter(), GenerateRoutes("pkg/router", "api", staff), WithRouterOutlet("machines", "_scheduled", APIKey())},
			wantErr: `outlet "machines" has the route prefix /_scheduled, under /_scheduled, which the generated router reserves for the scheduled routes (@schedule)`,
		},
		{
			name:    "an outlet prefix under the scheduled prefix",
			options: []ResourceOption{GenerateRouter(), GenerateRoutes("pkg/router", "_scheduled/api", staff)},
			wantErr: `outlet "default" has the route prefix /_scheduled/api, under /_scheduled`,
		},
		{
			name:    "a browser application under the scheduled prefix",
			options: []ResourceOption{GenerateRouter(), GenerateRoutes("pkg/router", "api", staff, WebApp("/_scheduled/console"))},
			wantErr: `outlet "default" declares WebApp("/_scheduled/console"), under /_scheduled, which the generated router reserves for the scheduled routes`,
		},
		{
			name:    "an API-key outlet whose middleware would be SchedulerAuth",
			options: []ResourceOption{GenerateRouter(), GenerateRoutes("pkg/router", "api", staff), WithRouterOutlet("scheduler", "scheduler", APIKey())},
			wantErr: `outlet "scheduler" would take the Handlers method SchedulerAuth, which the generated router reserves for the scheduled routes`,
		},
		{
			name:    "a prefix that only begins with the scheduled prefix's letters is not under it",
			options: []ResourceOption{GenerateRouter(), GenerateRoutes("pkg/router", "_scheduledapi", staff)},
		},
		{
			name:    "an API prefix under a browser application is the ordinary layout",
			options: []ResourceOption{GenerateRouter(), GenerateRoutes("pkg/router", "api", staff), WithRouterOutlet("portal", "portal/api", members, WebApp("/portal"))},
		},
		{
			name:    "OutletRequestLog without the switch",
			options: []ResourceOption{GenerateRoutes("pkg/router", "api", OutletRequestLog(LogOnEvent()))},
			wantErr: `outlet "default" declares OutletRequestLog, which describes the generated router`,
		},
		{
			name:    "OutletTraces without the switch",
			options: []ResourceOption{GenerateRoutes("pkg/router", "api"), WithRouterOutlet("portal", "portal/api", ServesSessions(), OutletTraces(TracesOff()))},
			wantErr: `outlet "portal" declares OutletTraces, which describes the generated router`,
		},
		{
			name:    "WithRequestLog without the switch",
			options: []ResourceOption{GenerateRoutes("pkg/router", "api"), WithRequestLog(LogOnEvent())},
			wantErr: "WithRequestLog(on event) describes the generated router, which builds the request logger with the application default",
		},
		{
			name:    "WithMountedRoutes without the switch",
			options: []ResourceOption{GenerateRoutes("pkg/router", "api"), WithMountedRoutes("/beacons/", LogOnEvent(), TracesOff())},
			wantErr: `WithMountedRoutes("/beacons/") describes the generated router, which builds the request logger and the tracer with the prefix`,
		},
		{
			name: "words at every place",
			options: []ResourceOption{
				GenerateRouter(),
				GenerateRoutes("pkg/router", "api", staff, OutletRequestLog(LogOnEvent()), OutletTraces(TracesCapped(0.5))),
				WithRouterOutlet("machines", "machines", APIKey(), OutletRequestLog(LogNever())),
				WithRequestLog(LogAlways().MinSeverity(logging.Warning)),
				WithMountedRoutes("/beacons/", LogOnEvent(), TracesOff()),
				WithMountedRoutes("/machines/telemetry/", LogNever(), TracesOff()),
			},
		},
		{
			name:    "a hand-mounted prefix under the scheduled prefix",
			options: []ResourceOption{GenerateRouter(), GenerateRoutes("pkg/router", "api", staff), WithMountedRoutes("/_scheduled/prune/", LogOnEvent(), TracesOff())},
			wantErr: `WithMountedRoutes("/_scheduled/prune/") sits under /_scheduled, which the generated router reserves for the scheduled routes (@schedule); a scheduled method declares its word on @schedule`,
		},
		{
			name:    "a hand-mounted prefix at the scheduled prefix",
			options: []ResourceOption{GenerateRouter(), GenerateRoutes("pkg/router", "api", staff), WithMountedRoutes("/_scheduled/", LogOnEvent(), TracesOff())},
			wantErr: `WithMountedRoutes("/_scheduled/") sits under /_scheduled`,
		},
		{
			name:    "a hand-mounted prefix that is an outlet's",
			options: []ResourceOption{GenerateRouter(), GenerateRoutes("pkg/router", "api", staff), WithMountedRoutes("/api/", LogOnEvent(), TracesOff())},
			wantErr: `WithMountedRoutes("/api/") is the default outlet's prefix; declare the outlet's words with OutletRequestLog and OutletTraces`,
		},
		{
			name:    "a hand-mounted prefix beneath an outlet's",
			options: []ResourceOption{GenerateRouter(), GenerateRoutes("pkg/router", "api", staff), WithMountedRoutes("/api/stream/", LogOnEvent(), TracesOff())},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := &resourceGenerator{client: &client{}}
			opts := make([]option, 0, len(tt.options))
			for _, opt := range tt.options {
				opts = append(opts, opt)
			}
			err := resolveOptions(r, opts)
			if err == nil {
				err = r.validateOutletConfig()
			}
			if err == nil {
				err = r.validateRouterConfig()
			}
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

// Test_servedRouterData pins the payload the templates read: BindAuth and the auth
// imports only with two distinct session auths, the default outlet's session handlers
// embedded and an additional outlet's behind a getter, the flavor routes under each
// prefix, the browser applications longest mount first, and the root redirect only
// when applications are served and none is at /.
func Test_servedRouterData(t *testing.T) {
	t.Parallel()

	crew := &outletAuth{importPath: "example.com/acme/beacon/pkg/auth/crew", flavor: Password}
	members := &outletAuth{importPath: "example.com/acme/beacon/pkg/auth/members", flavor: OIDCAzure}

	tests := []struct {
		name       string
		outlets    []routerOutlet
		fileRoutes map[string][]*generatedRoute
		check      func(t *testing.T, data *servedRouterData)
	}{
		{
			name:    "one session auth binds nothing and imports nothing",
			outlets: []routerOutlet{{name: "default", prefix: "api", servesSessions: true, auth: members, webApp: "/"}},
			check: func(t *testing.T, data *servedRouterData) {
				t.Helper()
				if data.MultiAuth || len(data.AuthImports) != 0 {
					t.Errorf("MultiAuth = %v, AuthImports = %v; want neither", data.MultiAuth, data.AuthImports)
				}
				o := data.Outlets[0]
				if o.AuthPackage != "" || o.Getter != "" || o.Receiver != "h" || o.HookField != "Default" || o.SessionHandlers != "session.OIDCAzureHandlers" {
					t.Errorf("default outlet = %+v", o)
				}
				if o.OldestAnswered != "" || o.OldestAnsweredExpr != "" || o.OldestAnsweredNote != "" || o.AnswersSentence != "every release up to the server's own is answered" || len(o.FileRoutes) != 0 {
					t.Errorf("version check of an outlet with no oldest answered = %+v", o)
				}
				if want := (versionProbes{Server: probeServer, InRange: probeFirstRelease, Above: probeAboveServer}); o.Probes != want {
					t.Errorf("probes = %+v, want %+v", o.Probes, want)
				}
				paths := make([]string, 0, len(o.Routes))
				for _, route := range o.Routes {
					paths = append(paths, route.Method+" "+route.Path)
				}
				want := "GET /api/user/login,GET /api/user/callback,GET /api/user/session,DELETE /api/user/session,GET /api/user/logout"
				if got := strings.Join(paths, ","); got != want {
					t.Errorf("routes = %s, want %s", got, want)
				}
				if len(data.WebApps) != 1 || data.WebApps[0].DeepLink != "DeepLink" || data.WebApps[0].Assets != "Assets" || !data.HasRootWebApp {
					t.Errorf("web apps = %+v", data.WebApps)
				}
				if data.RootRedirect != "" || data.RootRedirectOutlet != "" {
					t.Errorf("root redirect = %q to %q; want none with an application at /", data.RootRedirectOutlet, data.RootRedirect)
				}
				if len(data.Flavors) != 1 || data.Flavors[0].StubType != "routerOIDCAzureStub" {
					t.Errorf("flavors = %+v", data.Flavors)
				}
			},
		},
		{
			name: "two session auths and an API key",
			outlets: []routerOutlet{
				{name: "default", prefix: "api", servesSessions: true, auth: crew, webApp: "/console"},
				{name: "droids", prefix: "droids", apiKey: true},
				{name: "portal", prefix: "portal/api", servesSessions: true, auth: members, webApp: "/portal"},
			},
			check: func(t *testing.T, data *servedRouterData) {
				t.Helper()
				if !data.MultiAuth || strings.Join(data.AuthImports, ",") != "example.com/acme/beacon/pkg/auth/crew,example.com/acme/beacon/pkg/auth/members" {
					t.Errorf("MultiAuth = %v, AuthImports = %v", data.MultiAuth, data.AuthImports)
				}
				portal := data.Outlets[2]
				if portal.AuthPackage != "members" || portal.Getter != "Portal" || portal.Receiver != "portalSession" || portal.StubField != "portal" || portal.StubPrefix != "Portal" {
					t.Errorf("portal outlet = %+v", portal)
				}
				droids := data.Outlets[1]
				if !droids.APIKey || droids.AuthMiddleware != "DroidsAuth" || droids.RoutesFunc != "generatedDroidsRoutes" || droids.NotFoundPrefix != "/droids/" {
					t.Errorf("droids outlet = %+v", droids)
				}
				if len(data.ExtraOutlets) != 2 || len(data.SessionOutlets) != 2 || len(data.APIKeyOutlets) != 1 {
					t.Errorf("outlet groups = %d extra, %d session, %d api-key", len(data.ExtraOutlets), len(data.SessionOutlets), len(data.APIKeyOutlets))
				}
				if data.WebApps[0].Mount != "/console" || data.WebApps[0].DeepLink != "DeepLink" || data.WebApps[1].Mount != "/portal" || data.WebApps[1].DeepLink != "PortalDeepLink" || data.HasRootWebApp {
					t.Errorf("web apps = %+v, %+v; want /console first and no root application", data.WebApps[0], data.WebApps[1])
				}
				if data.RootRedirect != "/console/" || data.RootRedirectOutlet != "default" {
					t.Errorf("root redirect = %q to %q; want the default outlet's /console/", data.RootRedirectOutlet, data.RootRedirect)
				}
				if strings.Join(data.NotFoundPrefixes, ",") != "/api/,/droids/,/portal/api/" {
					t.Errorf("not-found prefixes = %v", data.NotFoundPrefixes)
				}
			},
		},
		{
			name: "a default outlet without a browser application redirects the root to the first outlet that serves one",
			outlets: []routerOutlet{
				{name: "default", prefix: "api", servesSessions: true, auth: crew},
				{name: "kiosk", prefix: "kiosk/api", servesSessions: true, auth: crew, webApp: "/kiosk"},
				{name: "portal", prefix: "portal/api", servesSessions: true, auth: members, webApp: "/portal"},
			},
			check: func(t *testing.T, data *servedRouterData) {
				t.Helper()
				if data.RootRedirect != "/kiosk/" || data.RootRedirectOutlet != "kiosk" {
					t.Errorf("root redirect = %q to %q; want the kiosk outlet's /kiosk/", data.RootRedirectOutlet, data.RootRedirect)
				}
			},
		},
		{
			name: "two outlets on one auth stay a single auth",
			outlets: []routerOutlet{
				{name: "default", prefix: "api", servesSessions: true, auth: crew},
				{name: "kiosk", prefix: "kiosk/api", servesSessions: true, auth: crew},
			},
			check: func(t *testing.T, data *servedRouterData) {
				t.Helper()
				if data.MultiAuth {
					t.Error("MultiAuth = true for one auth on two outlets")
				}
				if len(data.Flavors) != 1 {
					t.Errorf("flavors = %+v, want one", data.Flavors)
				}
				if data.RootRedirect != "" || data.RootRedirectOutlet != "" {
					t.Errorf("root redirect = %q to %q; want none without a browser application", data.RootRedirectOutlet, data.RootRedirect)
				}
			},
		},
		{
			name: "the version checks: a release, this release, and the stored-file routes by outlet",
			outlets: []routerOutlet{
				{name: "default", prefix: "api", servesSessions: true, auth: crew, oldestAnswered: "1.5.0", declaredOldest: true},
				{name: "droids", prefix: "droids", apiKey: true},
				{name: "portal", prefix: "portal/api", servesSessions: true, auth: members, oldestAnswered: ThisRelease, declaredOldest: true},
			},
			fileRoutes: map[string][]*generatedRoute{
				"default": {
					{Path: "/api/widgets/{widgetID}/thumbnail", TestURL: "/api/widgets/testWidgetID/thumbnail", HandlerFunc: "WidgetThumbnail", HandlerType: fileHandler},
					{Path: "/api/widgets/{widgetID}/content", TestURL: "/api/widgets/testWidgetID/content", HandlerFunc: "WidgetContent", HandlerType: fileHandler},
				},
				"droids": {{Path: "/droids/beacons/{beaconID}/content", TestURL: "/droids/beacons/testBeaconID/content", HandlerFunc: "BeaconContent", HandlerType: fileHandler}},
			},
			check: func(t *testing.T, data *servedRouterData) {
				t.Helper()
				o := data.Outlets[0]
				if o.OldestAnswered != "1.5.0" || o.OldestAnsweredExpr != `"1.5.0"` || o.OldestAnsweredNote != " (oldest answered 1.5.0)" || o.AnswersSentence != "releases from 1.5.0 up to the server's own are answered" {
					t.Errorf("default outlet's version check = %+v", o)
				}
				if want := (versionProbes{Server: "1.6.0", InRange: "1.5.0", Below: probeBelowAnyRelease, Above: "1.6.1"}); o.Probes != want {
					t.Errorf("default probes = %+v, want %+v", o.Probes, want)
				}
				wantFiles := []servedFileRoute{
					{Pattern: "/api/widgets/{widgetID}/content", TestURL: "/api/widgets/testWidgetID/content", Handler: "WidgetContent"},
					{Pattern: "/api/widgets/{widgetID}/thumbnail", TestURL: "/api/widgets/testWidgetID/thumbnail", Handler: "WidgetThumbnail"},
				}
				if diff := cmp.Diff(wantFiles, o.FileRoutes); diff != "" {
					t.Errorf("default file routes mismatch (-want +got):\n%s", diff)
				}
				if droids := data.Outlets[1]; len(droids.FileRoutes) != 0 || droids.OldestAnsweredExpr != "" {
					t.Errorf("an API-key outlet carries no version check, got %+v", droids)
				}
				portal := data.Outlets[2]
				if portal.OldestAnswered != ThisRelease || portal.OldestAnsweredExpr != "resource.ThisRelease" || portal.OldestAnsweredNote != " (oldest answered this release)" || portal.AnswersSentence != "only the server's own release is answered" || len(portal.FileRoutes) != 0 {
					t.Errorf("portal outlet's version check = %+v", portal)
				}
				if want := (versionProbes{Server: probeServer, InRange: probeServer, Below: probeBeforeServer, Above: probeAboveServer}); portal.Probes != want {
					t.Errorf("portal probes = %+v, want %+v", portal.Probes, want)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := &resourceGenerator{client: &client{}}
			r.router = packageDir("pkg/router")
			r.resource = packageDir("pkg/resources")
			tt.check(t, r.servedRouterData(tt.outlets, nil, tt.fileRoutes, nil))
		})
	}
}

// Test_versionProbesFor pins the releases the generated test sends through a check:
// the server one minor release past the oldest answered, the oldest answered itself in
// range, a release below it unless nothing is below, and one past the server's.
func Test_versionProbesFor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		oldest string
		want   versionProbes
	}{
		{oldest: "", want: versionProbes{Server: probeServer, InRange: probeFirstRelease, Above: probeAboveServer}},
		{oldest: ThisRelease, want: versionProbes{Server: probeServer, InRange: probeServer, Below: probeBeforeServer, Above: probeAboveServer}},
		{oldest: "1.5.0", want: versionProbes{Server: "1.6.0", InRange: "1.5.0", Below: probeBelowAnyRelease, Above: "1.6.1"}},
		{oldest: "v3.12.4", want: versionProbes{Server: "3.13.0", InRange: "v3.12.4", Below: probeBelowAnyRelease, Above: "3.13.1"}},
		{oldest: "2.0.0-rc1", want: versionProbes{Server: "2.1.0", InRange: "2.0.0-rc1", Below: probeBelowAnyRelease, Above: "2.1.1"}},
		{oldest: probeBelowAnyRelease, want: versionProbes{Server: "0.1.0", InRange: probeBelowAnyRelease, Above: "0.1.1"}},
	}
	for _, tt := range tests {
		t.Run(tt.oldest, func(t *testing.T) {
			t.Parallel()

			if got := versionProbesFor(tt.oldest); got != tt.want {
				t.Errorf("versionProbesFor(%q) = %+v, want %+v", tt.oldest, got, tt.want)
			}
		})
	}
}

// Test_servedRouterTemplates pins the rendered shapes: the chain comment opens the file,
// the flavor routes and guards render inline per outlet, BindAuth and the auth imports
// appear only with two session auths, an API-key group carries no session handling, the
// root redirect and its test render only with browser applications and none at /, and
// the test renders a recording stub per flavor in use.
func Test_servedRouterTemplates(t *testing.T) {
	t.Parallel()

	crew := &outletAuth{importPath: "example.com/acme/beacon/pkg/auth/crew", flavor: Password}
	members := &outletAuth{importPath: "example.com/acme/beacon/pkg/auth/members", flavor: OIDCGoogle}

	tests := []struct {
		name                string
		outlets             []routerOutlet
		fileRoutes          map[string][]*generatedRoute
		wantRouter          []string
		wantNotRouter       []string
		wantRouterTest      []string
		wantNotRouterTest   []string
		wantRouterImports   []string
		wantNoRouterImports []string
	}{
		{
			name:    "one Azure outlet with a browser application",
			outlets: []routerOutlet{{name: "default", prefix: "api", servesSessions: true, auth: &outletAuth{importPath: "example.com/acme/beacon/pkg/config", flavor: OIDCAzure}, webApp: "/"}},
			wantRouter: []string{
				"// Package router serves the application.",
				"//\tevery request: tracing, hooks.Outermost, request logging, SecurityHeaders, httpio.WithParams",
				"//\tdefault (/api), Azure directory sessions:",
				"//\t  NoCaching, CompressionMiddleware, StartSession, SetXSRFToken: GET /api/user/login, GET /api/user/callback, GET /api/user/session, DELETE /api/user/session, GET /api/user/logout",
				"//\t  + ValidateSession, ValidateXSRFToken, CheckAPIVersion: hooks.Default, generatedRoutes",
				"\tsession.OIDCAzureHandlers\n",
				"\tServerVersion() string\n",
				"func New(h Handlers, hooks Hooks) *chi.Mux {",
				"\tr := chi.NewRouter()\n\t// The release this server was built from, which every session outlet's version\n\t// check compares a browser application's X-Api-Version against.\n\tserverVersion := h.ServerVersion()\n",
				"\tr.Use(tracer.NewHandler())\n\tr.Use(hooks.Outermost...)\n\tr.Use(logger.NewRequestLogger(h.LogExporter()))\n\tr.Use(h.SecurityHeaders)\n\tr.Use(httpio.WithParams)\n",
				"\tLogExporter() logger.Exporter\n",
				"\t\tbounded.Get(\"/api/user/logout\", h.FrontChannelLogout())\n",
				"\t\t\tr.Use(h.ValidateSession)\n\t\t\tr.Use(h.ValidateXSRFToken)\n\t\t\t// The version check: a browser application sends its release in X-Api-Version,\n\t\t\t// and every release up to the server's own is answered. An application outside that\n\t\t\t// range is refused with 412 naming the server's release, before its body is read;\n\t\t\t// a request without the header, the session routes above are answered at any release.\n\t\t\tr.Use(resource.CheckAPIVersion(resource.APIVersionCheck{\n\t\t\t\tServerVersion: serverVersion,\n\t\t\t}))\n\n\t\t\tregisterGenerated(r, hooks.Default, \"Default\", func(r chi.Router) {\n\t\t\t\tgeneratedRoutes(r, h)\n\t\t\t})",
				"\tfor _, prefix := range []string{\"/api/\"} {",
				"\t// The default outlet's browser application at /, the catch-all.\n\tr.Route(\"/\", func(r chi.Router) {\n\t\tr.Use(h.DeepLink)\n\n\t\tr.Get(\"/*\", h.Assets())\n\t})\n\n\treturn r\n}",
				"\tDefault func(r chi.Router, generated func(chi.Router))\n}",
			},
			wantNotRouter:       []string{"BindAuth", "Generated" + "PortalHandlers", "Auth(next http.Handler)", "http.Redirect", "None is mounted at /", "OldestAnswered:", "Exempt:", "LoggerMiddleware"},
			wantRouterImports:   []string{"github.com/cccteam/ccc/tracer", "github.com/cccteam/logger"},
			wantNoRouterImports: []string{"example.com/acme/beacon/pkg/config"},
			wantRouterTest: []string{
				"var routerRootChain = []string{\"LogExporter\", \"SecurityHeaders\"}",
				"func (s *routerHandlersStub) LogExporter() logger.Exporter {\n\tif s.exporter != nil {\n\t\treturn s.exporter\n\t}\n\n\treturn &routerLogExporterStub{rec: s.rec}\n}",
				"func (s *routerLogExporterStub) Middleware() func(http.Handler) http.Handler {\n\treturn s.rec.Middleware(\"LogExporter\")\n}",
				"type routerOIDCAzureStub struct {\n\tsession.OIDCAzureHandlers",
				"func (s *routerOIDCAzureStub) FrontChannelLogout() http.HandlerFunc {",
				"\t\t{url: \"/api/user/logout\", suffix: \"/user/logout\", method: http.MethodGet, handler: \"FrontChannelLogout\"},",
				"\t\tguards: []string{\"ValidateSession\", \"ValidateXSRFToken\"},",
				"func TestGeneratedRouterWebApps(t *testing.T) {",
				"nothing else, and that the\n// application at / is the catch-all.",
				"\t\t{url: \"/generated-router-page/deep/link\", handler: \"Assets\", deepLink: \"DeepLink\"},",
				"func TestGeneratedRouterAPIVersion(t *testing.T) {",
				"\t\t{\n\t\t\tprefix: \"/api/\", server: \"2.0.0\", inRange: \"0.0.1\", below: \"\", above: \"2.0.1\",\n\t\t},",
				"\tserverVersion string\n\t// exporter is where the request log goes when a test sets one, the console for the\n\t// request log test; nil hands the router the recording stub.\n\texporter logger.Exporter\n}\n\nfunc (s *routerHandlersStub) ServerVersion() string {\n\treturn s.serverVersion\n}",
				"\t\t\tDefault: func(r chi.Router, generated func(chi.Router)) {\n\t\t\t\tr.Use(rec.Middleware(\"DefaultHook\"))\n\t\t\t\tgenerated(r)\n\t\t\t},",
			},
			wantNotRouterTest: []string{"BindAuth", "routerPasswordStub", "TestGeneratedRouterRoot", "files: []routerFileRoute"},
		},
		{
			name: "oldest answered on the default outlet with a stored-file route",
			outlets: []routerOutlet{
				{name: "default", prefix: "api", servesSessions: true, auth: crew, webApp: "/", oldestAnswered: "1.5.0", declaredOldest: true},
			},
			fileRoutes: map[string][]*generatedRoute{
				"default": {{Path: "/api/widgets/{widgetID}/content", TestURL: "/api/widgets/testWidgetID/content", HandlerFunc: "WidgetContent", HandlerType: fileHandler}},
			},
			wantRouter: []string{
				"//\t  + ValidateSession, ValidateXSRFToken, CheckAPIVersion (oldest answered 1.5.0): hooks.Default, generatedRoutes",
				"\t\t\t// The version check: a browser application sends its release in X-Api-Version,\n\t\t\t// and releases from 1.5.0 up to the server's own are answered. An application outside that\n\t\t\t// range is refused with 412 naming the server's release, before its body is read;\n\t\t\t// a request without the header, the session routes above and the stored-file\n\t\t\t// routes are answered at any release.\n\t\t\tr.Use(resource.CheckAPIVersion(resource.APIVersionCheck{\n\t\t\t\tServerVersion:  serverVersion,\n\t\t\t\tOldestAnswered: \"1.5.0\",\n\t\t\t\tExempt: []string{\n\t\t\t\t\t\"/api/widgets/{widgetID}/content\",\n\t\t\t\t},\n\t\t\t}))\n",
			},
			wantRouterImports: []string{"github.com/cccteam/ccc/resource"},
			wantRouterTest: []string{
				"\t\t{\n\t\t\tprefix: \"/api/\", server: \"1.6.0\", inRange: \"1.5.0\", below: \"0.0.0\", above: \"1.6.1\",\n\t\t\tfiles: []routerFileRoute{\n\t\t\t\t{url: \"/api/widgets/testWidgetID/content\", handler: \"WidgetContent\"},\n\t\t\t},\n\t\t},",
				"\t\t\tchecked(\"the oldest answered release\", probe.server, probe.inRange),",
				"\t\t\trefused(\"a release above the server's\", probe.above),",
				"\t\tif probe.below != \"\" {\n\t\t\ttests = append(tests, refused(\"a release below the oldest answered\", probe.below))\n\t\t}",
				"\t\t\t\tif body.read {\n\t\t\t\t\tt.Error(\"the refused request's body was read\")\n\t\t\t\t}",
				"\t\t\tif got := slices.Contains(rr.Header().Values(\"Vary\"), resource.APIVersionHeader); got != tt.varied {",
				"\tt.Run(\"a refused request never reaches the hook\", func(t *testing.T) {",
			},
			wantNotRouterTest: []string{"outlet is not checked"},
		},
		{
			name: "oldest answered on an additional session outlet and this release on another",
			outlets: []routerOutlet{
				{name: "default", prefix: "api", servesSessions: true, auth: crew},
				{name: "droids", prefix: "droids", apiKey: true},
				{name: "portal", prefix: "portal/api", servesSessions: true, auth: members, oldestAnswered: "3.2.0", declaredOldest: true},
				{name: "kiosk", prefix: "kiosk/api", servesSessions: true, auth: crew, oldestAnswered: ThisRelease, declaredOldest: true},
			},
			wantRouter: []string{
				"//\t  + ValidateSession, ValidateXSRFToken, CheckAPIVersion: hooks.Default, generatedRoutes",
				"//\t  + ValidateSession, ValidateXSRFToken, CheckAPIVersion (oldest answered 3.2.0): hooks.Portal, generatedPortalRoutes",
				"//\t  + ValidateSession, ValidateXSRFToken, CheckAPIVersion (oldest answered this release): hooks.Kiosk, generatedKioskRoutes",
				"\t\t\tr.Use(h.ValidateXSRFToken)\n\t\t\t// The version check: a browser application sends its release in X-Api-Version,\n\t\t\t// and every release up to the server's own is answered.",
				"\t\t\tr.Use(portalSession.ValidateXSRFToken)\n\t\t\t// The version check: a browser application sends its release in X-Api-Version,\n\t\t\t// and releases from 3.2.0 up to the server's own are answered.",
				"\t\t\t\tServerVersion:  serverVersion,\n\t\t\t\tOldestAnswered: \"3.2.0\",\n\t\t\t}))",
				"\t\t\tr.Use(kioskSession.ValidateXSRFToken)\n\t\t\t// The version check: a browser application sends its release in X-Api-Version,\n\t\t\t// and only the server's own release is answered.",
				"\t\t\t\tServerVersion:  serverVersion,\n\t\t\t\tOldestAnswered: resource.ThisRelease,\n\t\t\t}))",
				"\t\tr.Use(h.DroidsAuth)\n\n\t\tregisterGenerated(r, hooks.Droids, \"Droids\"",
			},
			wantRouterTest: []string{
				"\t\t{\n\t\t\tprefix: \"/api/\", server: \"2.0.0\", inRange: \"0.0.1\", below: \"\", above: \"2.0.1\",\n\t\t},",
				"\t\t{\n\t\t\tprefix: \"/portal/api/\", server: \"3.3.0\", inRange: \"3.2.0\", below: \"0.0.0\", above: \"3.3.1\",\n\t\t},",
				"\t\t{\n\t\t\tprefix: \"/kiosk/api/\", server: \"2.0.0\", inRange: \"2.0.0\", below: \"1.9.9\", above: \"2.0.1\",\n\t\t},",
				"\tfor _, route := range generatedRouterTests() {\n\t\tif strings.HasPrefix(route.url, \"/droids/\") {\n\t\t\ttests = append(tests, versionCase{name: \"droids outlet is not checked\", server: \"2.0.0\", app: \"99.0.0\", method: route.method, url: route.url, handler: route.handlerFunc})",
			},
		},
		{
			name: "two session auths and an API-key outlet",
			outlets: []routerOutlet{
				{name: "default", prefix: "api", servesSessions: true, auth: crew, webApp: "/console"},
				{name: "droids", prefix: "droids", apiKey: true},
				{name: "portal", prefix: "portal/api", servesSessions: true, auth: members, webApp: "/portal"},
			},
			wantRouter: []string{
				"//\tdefault (/api), password sessions of the crew auth:",
				"//\t  BindAuth(crew.Name), NoCaching, CompressionMiddleware, StartSession, SetXSRFToken: POST /api/user/login, GET /api/user/session, DELETE /api/user/session",
				"//\tdroids (/droids), API key:\n//\t  NoCaching, CompressionMiddleware, DroidsAuth: hooks.Droids, generatedDroidsRoutes",
				"\tGeneratedHandlers\n\tGeneratedDroidsHandlers\n\tGeneratedPortalHandlers\n",
				"\tPortal() session.OIDCGoogleHandlers\n",
				"\tBindAuth(name string) func(http.Handler) http.Handler\n",
				"\tDroidsAuth(next http.Handler) http.Handler\n",
				"\tPortalDeepLink(next http.Handler) http.Handler\n\tPortalAssets() http.HandlerFunc\n",
				"\t\tr.Use(h.BindAuth(crew.Name))\n\t\tr.Use(h.NoCaching)",
				"\t\tr.Use(h.NoCaching)\n\t\tr.Use(h.CompressionMiddleware())\n\t\tr.Use(h.DroidsAuth)\n\n\t\tregisterGenerated(r, hooks.Droids, \"Droids\"",
				"\tportalSession := h.Portal()\n",
				"\t\tr.Use(portalSession.StartSession)",
				"\t\tbounded.Get(\"/portal/api/user/callback\", portalSession.CallbackOIDC())",
				"\tfor _, prefix := range []string{\"/api/\", \"/droids/\", \"/portal/api/\"} {",
				"// applications answer, longer mount paths first: /console (DeepLink, Assets), /portal (PortalDeepLink, PortalAssets).\n// None is mounted at /: the root alone redirects to /console/, the default outlet's application.\npackage router",
				"\t// The default outlet's browser application at /console.\n\tr.Route(\"/console\", func(r chi.Router) {\n\t\tr.Use(h.DeepLink)\n\n\t\tr.Get(\"/*\", h.Assets())\n\t})\n\n\t// The portal outlet's browser application at /portal.\n\tr.Route(\"/portal\", func(r chi.Router) {\n\t\tr.Use(h.PortalDeepLink)\n\n\t\tr.Get(\"/*\", h.PortalAssets())\n\t})\n\n\t// No browser application is mounted at /:",
				"The root alone sends the browser to the default outlet's\n\t// application; every other unmatched path is 404.\n\tr.Get(\"/\", func(w http.ResponseWriter, req *http.Request) {\n\t\thttp.Redirect(w, req, \"/console/\", http.StatusTemporaryRedirect)\n\t})\n\n\treturn r\n}",
				"\tDroids func(r chi.Router, generated func(chi.Router))",
			},
			wantNotRouter:     []string{"h.Portal().StartSession", "droidsSession", "catch-all"},
			wantRouterImports: []string{"example.com/acme/beacon/pkg/auth/crew", "example.com/acme/beacon/pkg/auth/members"},
			wantRouterTest: []string{
				"\t\tgroup:  []string{\"BindAuth(\" + crew.Name + \")\", \"NoCaching\", \"CompressionMiddleware\", \"StartSession\", \"SetXSRFToken\"},",
				"\t\tgroup: []string{\"NoCaching\", \"CompressionMiddleware\", \"DroidsAuth\"},",
				"\t\tgroup:  []string{\"BindAuth(\" + members.Name + \")\", \"NoCaching\", \"CompressionMiddleware\", \"PortalStartSession\", \"PortalSetXSRFToken\"},",
				"type routerPasswordStub struct {\n\tsession.PasswordAuthHandlers",
				"type routerOIDCGoogleStub struct {\n\tsession.OIDCGoogleHandlers",
				"\tportal *routerOIDCGoogleStub\n",
				"\t\tportal:                &routerOIDCGoogleStub{rec: rec, prefix: \"Portal\"},",
				"func (s *routerHandlersStub) DroidsAuth(next http.Handler) http.Handler {",
				"func (s *routerHandlersStub) BindAuth(name string) func(http.Handler) http.Handler {",
				"\t\t\tDroids: func(r chi.Router, generated func(chi.Router)) {",
				"\t\t{url: \"/console/generated-router-page/deep/link\", handler: \"Assets\", deepLink: \"DeepLink\"},",
				"// redirects to the default outlet's application, calling no handler, and that an\n",
				"func TestGeneratedRouterRoot(t *testing.T) {",
				"\t\t{name: \"the root redirects\", url: \"/\", code: http.StatusTemporaryRedirect, location: \"/console/\"},",
				"\t\t{name: \"an unmatched path is not found\", url: \"/generated-router-unmatched\", code: http.StatusNotFound},",
			},
			wantNotRouterTest: []string{"routerOIDCAzureStub", "catch-all"},
		},
		{
			name: "a default outlet without a browser application redirects the root to the first outlet that serves one",
			outlets: []routerOutlet{
				{name: "default", prefix: "api", servesSessions: true, auth: crew},
				{name: "portal", prefix: "portal/api", servesSessions: true, auth: crew, webApp: "/portal"},
			},
			wantRouter: []string{
				"// None is mounted at /: the root alone redirects to /portal/, the portal outlet's application.",
				"The root alone sends the browser to the portal outlet's\n\t// application; every other unmatched path is 404.\n\tr.Get(\"/\", func(w http.ResponseWriter, req *http.Request) {\n\t\thttp.Redirect(w, req, \"/portal/\", http.StatusTemporaryRedirect)\n\t})",
			},
			wantNotRouter: []string{"catch-all", "h.DeepLink", "h.Assets"},
			wantRouterTest: []string{
				"// redirects to the portal outlet's application, calling no handler, and that an\n",
				"\t\t{name: \"the root redirects\", url: \"/\", code: http.StatusTemporaryRedirect, location: \"/portal/\"},",
			},
		},
		{
			name:    "an API-key default outlet alone imports no session package",
			outlets: []routerOutlet{{name: "default", prefix: "api", apiKey: true}},
			wantRouter: []string{
				"//\tdefault (/api), API key:",
				"\tDefaultAuth(next http.Handler) http.Handler\n",
				"\tServerVersion() string\n",
			},
			wantNotRouter: []string{"StartSession", "session.PasswordAuthHandlers", "session.OIDC", "http.Redirect", "r.Use(resource.CheckAPIVersion(", "serverVersion :="},
			// resource stays imported: the body limit wraps hooks.Root's group on every router.
			wantNoRouterImports: []string{"github.com/cccteam/session"},
			wantNotRouterTest:   []string{"session.PasswordAuthHandlers", "session.OIDC", "TestGeneratedRouterWebApps", "func TestGeneratedRouterSessionRoutes", "TestGeneratedRouterRoot", "TestGeneratedRouterAPIVersion", "routerVersionProbe"},
		},
	}
	r := &resourceGenerator{client: &client{}}
	r.router = packageDir("pkg/router")
	r.resource = packageDir("pkg/resources")
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			data := r.servedRouterData(tt.outlets, nil, tt.fileRoutes, nil)

			router := render(t, r, "servedRouterTemplate", servedRouterTemplate, data)
			for _, want := range tt.wantRouter {
				if !strings.Contains(router, want) {
					t.Errorf("router missing %q:\n%s", want, router)
				}
			}
			for _, notWant := range tt.wantNotRouter {
				if strings.Contains(router, notWant) {
					t.Errorf("router must not contain %q:\n%s", notWant, router)
				}
			}
			for _, path := range tt.wantRouterImports {
				if !strings.Contains(router, "\t\""+path+"\"\n") {
					t.Errorf("router must import %q:\n%s", path, router)
				}
			}
			for _, path := range tt.wantNoRouterImports {
				if strings.Contains(router, "\""+path+"\"") {
					t.Errorf("router must not import %q:\n%s", path, router)
				}
			}

			routerTest := render(t, r, "servedRouterTestTemplate", servedRouterTestTemplate, data)
			for _, want := range tt.wantRouterTest {
				if !strings.Contains(routerTest, want) {
					t.Errorf("router test missing %q:\n%s", want, routerTest)
				}
			}
			for _, notWant := range tt.wantNotRouterTest {
				if strings.Contains(routerTest, notWant) {
					t.Errorf("router test must not contain %q:\n%s", notWant, routerTest)
				}
			}
		})
	}
}

// render executes a template and formats the result the way the generator writes it,
// so the import block is pruned and the output is what an application would receive.
func render(t *testing.T, r *resourceGenerator, name, tmpl string, data any) string {
	t.Helper()

	out, err := r.generateTemplateOutput(name, tmpl, data)
	if err != nil {
		t.Fatalf("generateTemplateOutput(%s) error = %v", name, err)
	}
	fileName := generatedGoFileName(servedRouterOutputName)
	if name == "servedRouterTestTemplate" {
		fileName = generatedGoFileName(servedRouterTestOutputName)
	}
	formatted, err := r.formatGoBytes("pkg/router/"+fileName, name, out, data)
	if err != nil {
		t.Fatalf("formatGoBytes(%s) error = %v\n%s", name, err, out)
	}

	return string(formatted)
}

// Test_generateRoutesOutletOptions pins that GenerateRoutes hands its outlet options to the
// default outlet and reports their errors under its own name.
func Test_generateRoutesOutletOptions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		option  ResourceOption
		wantErr []string
		check   func(t *testing.T, r *resourceGenerator)
	}{
		{
			name:   "a session auth and a browser application on the default outlet",
			option: GenerateRoutes("pkg/router", "api", Auth("example.com/acme/beacon/pkg/auth/staff", Password), WebApp("/")),
			check: func(t *testing.T, r *resourceGenerator) {
				t.Helper()
				outlets := r.allOutlets()
				if len(outlets) != 1 || outlets[0].auth == nil || outlets[0].auth.flavor != Password || outlets[0].webApp != "/" || !outlets[0].servesSessions || outlets[0].name != defaultOutletName || outlets[0].prefix != "api" {
					t.Errorf("default outlet = %+v", outlets[0])
				}
			},
		},
		{
			name:    "an option error names the call",
			option:  GenerateRoutes("pkg/router", "api", WebApp("portal")),
			wantErr: []string{`GenerateRoutes("pkg/router", "api")`, `WebApp("portal") requires a mount path`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := &resourceGenerator{client: &client{}}
			err := resolveOptions(r, []option{tt.option})
			if len(tt.wantErr) > 0 {
				for _, want := range tt.wantErr {
					if err == nil || !strings.Contains(err.Error(), want) {
						t.Fatalf("error = %v, want containing %q", err, want)
					}
				}

				return
			}
			if err != nil {
				t.Fatalf("error = %v", err)
			}
			tt.check(t, r)
		})
	}
}

// wordedRouterShape is the router shape the words tests share: an application default
// with a floor, a prefix mounted by hand at the root and one beneath the droids outlet,
// the droids outlet with a word and a setting, the portal outlet with a setting alone,
// a stored-file route with both and a droids route with a sampled word, and a scheduled
// method with a word.
func wordedRouterShape(t *testing.T) (r *resourceGenerator, outlets []routerOutlet, fileRoutes, wordedRoutes map[string][]*generatedRoute) {
	t.Helper()

	crew := &outletAuth{importPath: "example.com/acme/beacon/pkg/auth/crew", flavor: Password}
	members := &outletAuth{importPath: "example.com/acme/beacon/pkg/auth/members", flavor: OIDCGoogle}
	scheduledStruct := fixtureStructs(loadFixture(t, "schedulefixture"))["PruneLogs"]
	r = &resourceGenerator{
		client: &client{
			genRPCMethods:    true,
			scheduledMethods: []*rpcMethodInfo{{Struct: scheduledStruct, Schedule: &rpcSchedule{Cron: "30 3 * * *", Zone: "UTC", RequestLog: LogOnEvent()}}},
		},
		requestLog: LogAlways().MinSeverity(logging.Warning),
		mountedRoutes: []mountedRoutes{
			{prefix: "/droids/telemetry/", requestLog: LogNever(), traces: TracesCapped(0.5)},
			{prefix: "/beacons/", requestLog: LogOnEvent(), traces: TracesOff()},
		},
	}
	r.router = packageDir("pkg/router")
	r.resource = packageDir("pkg/resources")
	outlets = []routerOutlet{
		{name: "default", prefix: "api", servesSessions: true, auth: crew, webApp: "/console"},
		{name: "droids", prefix: "droids", apiKey: true, requestLog: LogOnEvent(), traces: TracesOff()},
		{name: "portal", prefix: "portal/api", servesSessions: true, auth: members, webApp: "/portal", traces: TracesCapped(0.1)},
	}
	content := &generatedRoute{Method: "GET", Path: "/api/widgets/{widgetID}/content", TestURL: "/api/widgets/testWidgetID/content", HandlerFunc: "WidgetContent", HandlerType: fileHandler, RequestLog: LogNever(), Traces: TracesOff()}
	fileRoutes = map[string][]*generatedRoute{"default": {content}}
	wordedRoutes = map[string][]*generatedRoute{
		"default": {content},
		"droids":  {{Method: "POST", Path: "/droids/ingest", TestURL: "/droids/ingest", HandlerFunc: "Ingest", SelfBounded: true, RequestLog: LogSampled(0.5)}},
	}

	return r, outlets, fileRoutes, wordedRoutes
}

// Test_servedRouterData_words pins the words' payload: each outlet's words sentence, a
// route's words under its outlet, the hand-mounted prefixes in prefix order and each
// beneath its outlet or at the root, the surface table in pattern order with every
// trace setting, the worded outlet's not-found handler apart from the rest, the imports
// the words need, and the request log test's cases in the order the test template
// renders them.
func Test_servedRouterData_words(t *testing.T) {
	t.Parallel()

	r, outlets, fileRoutes, wordedRoutes := wordedRouterShape(t)
	data := r.servedRouterData(outlets, nil, fileRoutes, wordedRoutes)

	tests := []struct {
		name  string
		check func(t *testing.T, data *servedRouterData)
	}{
		{
			name: "each outlet's words and its routes' words",
			check: func(t *testing.T, data *servedRouterData) {
				t.Helper()
				words := make([]string, 0, len(data.Outlets))
				for _, o := range data.Outlets {
					words = append(words, o.Name+": "+o.Words)
					for _, route := range o.RouteWords {
						words = append(words, "  "+route.Method+" "+route.Path+": "+route.Words)
					}
				}
				want := "default: |  GET /api/widgets/{widgetID}/content: request log never, traces off|droids: request log on event, traces off|  POST /droids/ingest: request log sampled at 0.5|portal: traces capped at 0.1"
				if got := strings.Join(words, "|"); got != want {
					t.Errorf("words = %s, want %s", got, want)
				}
			},
		},
		{
			name: "the hand-mounted prefixes, beneath their outlets or at the root",
			check: func(t *testing.T, data *servedRouterData) {
				t.Helper()
				want := []servedMountedRoutes{
					{Prefix: "/beacons/", RequestLog: LogOnEvent(), Traces: TracesOff(), Words: "request log on event, traces off"},
					{Prefix: "/droids/telemetry/", RequestLog: LogNever(), Traces: TracesCapped(0.5), Words: "request log never, traces capped at 0.5"},
				}
				if diff := cmp.Diff(want, data.MountedRoutes); diff != "" {
					t.Errorf("MountedRoutes mismatch (-want +got):\n%s", diff)
				}
				if diff := cmp.Diff(want[:1], data.RootMountedRoutes); diff != "" {
					t.Errorf("RootMountedRoutes mismatch (-want +got):\n%s", diff)
				}
				if diff := cmp.Diff(want[1:], data.Outlets[1].MountedBeneath); diff != "" {
					t.Errorf("droids MountedBeneath mismatch (-want +got):\n%s", diff)
				}
				if got := data.Outlets[1].MountedPrefixes(); len(got) != 1 || got[0] != "/droids/telemetry/" {
					t.Errorf("droids MountedPrefixes() = %v, want [/droids/telemetry/]", got)
				}
				if len(data.Outlets[0].MountedBeneath) != 0 || len(data.Outlets[2].MountedBeneath) != 0 {
					t.Errorf("default and portal MountedBeneath = %v, %v; want none", data.Outlets[0].MountedBeneath, data.Outlets[2].MountedBeneath)
				}
			},
		},
		{
			name: "the surface table in pattern order",
			check: func(t *testing.T, data *servedRouterData) {
				t.Helper()
				want := []servedSurface{
					{Pattern: "/api/widgets/{widgetID}/content", Traces: TracesOff()},
					{Pattern: "/beacons/", Traces: TracesOff()},
					{Pattern: "/droids/", Traces: TracesOff()},
					{Pattern: "/droids/telemetry/", Traces: TracesCapped(0.5)},
					{Pattern: "/portal/api/", Traces: TracesCapped(0.1)},
				}
				if diff := cmp.Diff(want, data.Surfaces); diff != "" {
					t.Errorf("Surfaces mismatch (-want +got):\n%s", diff)
				}
			},
		},
		{
			name: "the worded outlet's not-found handler apart from the rest",
			check: func(t *testing.T, data *servedRouterData) {
				t.Helper()
				if diff := cmp.Diff([]string{"/api/", "/portal/api/", "/_scheduled/"}, data.NotFoundPrefixes); diff != "" {
					t.Errorf("NotFoundPrefixes mismatch (-want +got):\n%s", diff)
				}
				if len(data.WordedNotFound) != 1 || data.WordedNotFound[0].Name != "droids" {
					t.Errorf("WordedNotFound = %v, want the droids outlet", data.WordedNotFound)
				}
			},
		},
		{
			name: "the flags the imports and the closing paragraph read",
			check: func(t *testing.T, data *servedRouterData) {
				t.Helper()
				if !data.DeclaresWords || !data.UsesLogging || !data.UsesStrings {
					t.Errorf("DeclaresWords, UsesLogging, UsesStrings = %v, %v, %v; want all true", data.DeclaresWords, data.UsesLogging, data.UsesStrings)
				}
			},
		},
		{
			name: "the request log test's cases",
			check: func(t *testing.T, data *servedRouterData) {
				t.Helper()
				cases := make([]string, 0, len(data.RequestLogCases))
				for _, c := range data.RequestLogCases {
					cases = append(cases, c.Hook+" "+c.Method+" "+c.Path+" "+strconv.Itoa(c.Status)+" "+c.SetWord+" "+c.Want+": "+c.Name)
				}
				want := []string{
					"Root GET /generated-router-probe 200  written: the application default, always, Warning and above, on a quiet request",
					"Root GET /generated-router-failure 404  written: the application default, always, Warning and above, on a failed request",
					"Root GET /generated-router-own-word 200 logger.Never() dropped: a handler that sets its own request's word to never, under the application default, always, Warning and above",
					"Default GET /api/generated-router-probe 200  written: the default outlet, which takes the application default, always, Warning and above",
					"Droids GET /droids/generated-router-probe 200  dropped: the droids outlet's word, on event, on a quiet request",
					"Droids GET /droids/generated-router-failure 404  written: the droids outlet's word, on event, on a failed request",
					"Root GET /beacons/generated-router-probe 200  dropped: the prefix /beacons/ mounted by hand, on event, on a quiet request",
					"Root GET /beacons/generated-router-failure 404  written: the prefix /beacons/ mounted by hand, on event, on a failed request",
					"Droids GET /droids/telemetry/generated-router-probe 200  dropped: the prefix /droids/telemetry/ mounted by hand, never, on a quiet request",
					"Droids GET /droids/telemetry/generated-router-failure 404  dropped: the prefix /droids/telemetry/ mounted by hand, never, on a failed request",
					" GET /api/widgets/testWidgetID/content 200  dropped: the route's own word, never, on GET /api/widgets/{widgetID}/content",
					" POST /droids/ingest 200  : the route's own word, sampled at 0.5, on POST /droids/ingest",
				}
				if diff := cmp.Diff(want, cases); diff != "" {
					t.Errorf("RequestLogCases mismatch (-want +got):\n%s", diff)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tt.check(t, data)
		})
	}
}

// Test_servedRouterData_wordsUndeclared pins the payload with nothing declared: the
// request log test still proves the logger's own word, always, on the root and on the
// first outlet, and a handler's own demotion, and the router imports nothing for words.
func Test_servedRouterData_wordsUndeclared(t *testing.T) {
	t.Parallel()

	crew := &outletAuth{importPath: "example.com/acme/beacon/pkg/auth/crew", flavor: Password}
	r := &resourceGenerator{client: &client{}}
	r.router = packageDir("pkg/router")
	r.resource = packageDir("pkg/resources")
	data := r.servedRouterData([]routerOutlet{{name: "default", prefix: "api", servesSessions: true, auth: crew}, {name: "droids", prefix: "droids", apiKey: true}}, nil, nil, nil)

	if data.DeclaresWords || data.UsesLogging || data.UsesStrings || len(data.Surfaces) != 0 || len(data.MountedRoutes) != 0 || len(data.WordedNotFound) != 0 {
		t.Errorf("payload with nothing declared = %+v", data)
	}
	cases := make([]string, 0, len(data.RequestLogCases))
	for _, c := range data.RequestLogCases {
		cases = append(cases, c.Hook+" "+c.Path+" "+c.Want+": "+c.Name)
	}
	want := []string{
		"Root /generated-router-probe written: the application default, always, since nothing is declared, on a quiet request",
		"Root /generated-router-failure written: the application default, always, since nothing is declared, on a failed request",
		"Root /generated-router-own-word dropped: a handler that sets its own request's word to never, under the application default, always, since nothing is declared",
		"Default /api/generated-router-probe written: the default outlet, which takes the application default, always, since nothing is declared",
	}
	if diff := cmp.Diff(want, cases); diff != "" {
		t.Errorf("RequestLogCases mismatch (-want +got):\n%s", diff)
	}
}

// Test_servedRouterTemplates_words pins what the router and its test render for the
// words: the chain comment naming the word and the setting at each place and closing
// with the rule, the tracer handed the surface table, the request logger handed the
// application default and the prefix table, an outlet's word on its group and on its
// not-found handler, excepting the prefixes mounted by hand beneath it, the helper that
// does so, and the request log test's cases and probes.
func Test_servedRouterTemplates_words(t *testing.T) {
	t.Parallel()

	r, outlets, fileRoutes, wordedRoutes := wordedRouterShape(t)
	alone := []routerOutlet{
		{name: "default", prefix: "api", servesSessions: true, auth: &outletAuth{importPath: "example.com/acme/beacon/pkg/auth/crew", flavor: Password}, requestLog: LogOnEvent()},
	}
	plain := &resourceGenerator{client: &client{}}
	plain.router = packageDir("pkg/router")
	plain.resource = packageDir("pkg/resources")

	tests := []struct {
		name              string
		r                 *resourceGenerator
		outlets           []routerOutlet
		fileRoutes        map[string][]*generatedRoute
		wordedRoutes      map[string][]*generatedRoute
		wantRouter        []string
		wantNotRouter     []string
		wantRouterTest    []string
		wantNotRouterTest []string
		wantRouterImports []string
		wantNoImports     []string
	}{
		{
			name:         "words at every place",
			r:            r,
			outlets:      outlets,
			fileRoutes:   fileRoutes,
			wordedRoutes: wordedRoutes,
			wantRouter: []string{
				"//\tevery request: tracing, hooks.Outermost, request logging (always, Warning and above), SecurityHeaders, httpio.WithParams\n//\t  by hand under /beacons/: request log on event, traces off\n//\tdefault (/api), password sessions of the crew auth:\n",
				"//\t  + ValidateSession, ValidateXSRFToken, CheckAPIVersion: hooks.Default, generatedRoutes\n//\t  GET /api/widgets/{widgetID}/content: request log never, traces off\n//\tdroids (/droids), API key, request log on event, traces off:\n//\t  NoCaching, CompressionMiddleware, DroidsAuth: hooks.Droids, generatedDroidsRoutes\n//\t  by hand under /droids/telemetry/: request log never, traces capped at 0.5\n//\t  POST /droids/ingest: request log sampled at 0.5\n//\tportal (/portal/api), Google directory sessions of the members auth, traces capped at 0.1:\n",
				"//\tscheduled (/_scheduled), Cloud Scheduler's token:\n//\t  NoCaching, CompressionMiddleware, SchedulerAuth: generatedScheduledRoutes\n//\t  POST /_scheduled/prune-logs: request log on event\n//\n",
				"// None is mounted at /: the root alone redirects to /console/, the default outlet's application.\n//\n// The request log word and the trace setting named at each place are the nearest\n// declarations, a route's over its outlet's and an outlet's over the application\n// default; a request under none writes its entry always and its spans follow the front\n// end. The root's request logger decides each entry when the request ends, and a handler\n// may change its own request's word through logger.FromReq(r).SetPolicy.\npackage router\n",
				"\tr.Use(tracer.NewHandler(tracer.Surfaces(map[string]tracer.Traces{\n\t\t\"/api/widgets/{widgetID}/content\": tracer.TracesOff(),\n\t\t\"/beacons/\":                       tracer.TracesOff(),\n\t\t\"/droids/\":                        tracer.TracesOff(),\n\t\t\"/droids/telemetry/\":              tracer.TracesCapped(0.5),\n\t\t\"/portal/api/\":                    tracer.TracesCapped(0.1),\n\t})))\n\tr.Use(hooks.Outermost...)\n",
				"\t// word a request starts with, the application default or the word of the longest\n\t// prefix declared by hand that the path sits under; an outlet's word, a route's own\n\t// and a handler's own request override it on the way down.\n\tr.Use(logger.NewRequestLogger(h.LogExporter(),\n\t\tlogger.DefaultPolicy(logger.Always().MinSeverity(logging.Warning)),\n\t\tlogger.PolicyByPrefix(map[string]logger.Policy{\n\t\t\t\"/beacons/\":          logger.OnEvent(),\n\t\t\t\"/droids/telemetry/\": logger.Never(),\n\t\t}),\n\t))\n\tr.Use(h.SecurityHeaders)\n",
				"\tr.Group(func(r chi.Router) {\n\t\t// Every request under the outlet writes its request log on event, except under\n\t\t// the prefixes declared by hand beneath it, which keep their own word.\n\t\tr.Use(outletRequestLog(logger.OnEvent(), \"/droids/telemetry/\"))\n\t\tr.Use(h.NoCaching)\n\t\tr.Use(h.CompressionMiddleware())\n\t\tr.Use(h.DroidsAuth)\n",
				"\tfor _, prefix := range []string{\"/api/\", \"/portal/api/\", \"/_scheduled/\"} {",
				"\t// The droids outlet's not-found handler carries the outlet's word, so an unknown path\n\t// under its prefix is logged as its requests are.\n\tr.Route(\"/droids/\", func(r chi.Router) {\n\t\t// Every request under the outlet writes its request log on event, except under\n\t\t// the prefixes declared by hand beneath it, which keep their own word.\n\t\tr.Use(outletRequestLog(logger.OnEvent(), \"/droids/telemetry/\"))\n\t\tr.NotFound(func(w http.ResponseWriter, _ *http.Request) {\n",
				"func outletRequestLog(word logger.Policy, mountedPrefixes ...string) func(http.Handler) http.Handler {\n\tsetWord := logger.WithPolicy(word)\n",
			},
			wantNotRouter:     []string{"r.Use(logger.WithPolicy(", "tracer.NewHandler())", "logger.NewRequestLogger(h.LogExporter()))"},
			wantRouterImports: []string{"\t\"strings\"\n", "\t\"cloud.google.com/go/logging\"\n", "\t\"github.com/cccteam/ccc/tracer\"\n", "\t\"github.com/cccteam/logger\"\n"},
			wantRouterTest: []string{
				"\t\tRoot: func(r chi.Router) {\n\t\t\tr.Get(\"/generated-router-probe\", requestLogProbe(200))\n\t\t\tr.Get(\"/generated-router-failure\", requestLogProbe(404))\n\t\t\tr.Get(\"/generated-router-own-word\", requestLogOwnWord(logger.Never()))\n\t\t\tr.Get(\"/beacons/generated-router-probe\", requestLogProbe(200))\n\t\t\tr.Get(\"/beacons/generated-router-failure\", requestLogProbe(404))\n\t\t},\n",
				"\t\tDefault: func(r chi.Router, generated func(chi.Router)) {\n\t\t\tr.Get(\"/api/generated-router-probe\", requestLogProbe(200))\n\t\t\tgenerated(r)\n\t\t},\n",
				"\t\tDroids: func(r chi.Router, generated func(chi.Router)) {\n\t\t\tr.Get(\"/droids/generated-router-probe\", requestLogProbe(200))\n\t\t\tr.Get(\"/droids/generated-router-failure\", requestLogProbe(404))\n\t\t\tr.Get(\"/droids/telemetry/generated-router-probe\", requestLogProbe(200))\n\t\t\tr.Get(\"/droids/telemetry/generated-router-failure\", requestLogProbe(404))\n\t\t\tgenerated(r)\n\t\t},\n",
				"\t\tPortal: func(r chi.Router, generated func(chi.Router)) {\n\t\t\tgenerated(r)\n\t\t},\n",
				"\t\t{name: \"the application default, always, Warning and above, on a failed request\", method: http.MethodGet, url: \"/generated-router-failure\", status: 404, want: \"written\"},\n",
				"\t\t{name: \"a handler that sets its own request's word to never, under the application default, always, Warning and above\", method: http.MethodGet, url: \"/generated-router-own-word\", status: 200, want: \"dropped\"},\n",
				"\t\t{name: \"the droids outlet's word, on event, on a quiet request\", method: http.MethodGet, url: \"/droids/generated-router-probe\", status: 200, want: \"dropped\"},\n",
				"\t\t{name: \"the prefix /droids/telemetry/ mounted by hand, never, on a failed request\", method: http.MethodGet, url: \"/droids/telemetry/generated-router-failure\", status: 404, want: \"dropped\"},\n",
				"\t\t{name: \"the route's own word, never, on GET /api/widgets/{widgetID}/content\", method: http.MethodGet, url: \"/api/widgets/testWidgetID/content\", status: 200, want: \"dropped\"},\n",
				"\t\t{name: \"the route's own word, sampled at 0.5, on POST /droids/ingest\", method: http.MethodPost, url: \"/droids/ingest\", status: 200, want: \"\"},\n",
				"func TestGeneratedRouterRequestLog(t *testing.T) {\n\ttests := []struct {",
				"\t\t\tstub.exporter = logger.NewConsoleExporter().NoColor(true)\n\t\t\trouter := New(stub, requestLogHooks())\n",
			},
		},
		{
			name:    "an outlet's word alone",
			r:       plain,
			outlets: alone,
			wantRouter: []string{
				"//\tevery request: tracing, hooks.Outermost, request logging, SecurityHeaders, httpio.WithParams\n//\tdefault (/api), password sessions, request log on event:\n",
				"\tr.Use(tracer.NewHandler())\n",
				"\tr.Use(logger.NewRequestLogger(h.LogExporter()))\n",
				"\tr.Group(func(r chi.Router) {\n\t\t// Every request under the outlet writes its request log on event.\n\t\tr.Use(logger.WithPolicy(logger.OnEvent()))\n\t\tr.Use(h.NoCaching)\n",
				"\t// The default outlet's not-found handler carries the outlet's word, so an unknown path\n\t// under its prefix is logged as its requests are.\n\tr.Route(\"/api/\", func(r chi.Router) {\n\t\t// Every request under the outlet writes its request log on event.\n\t\tr.Use(logger.WithPolicy(logger.OnEvent()))\n\t\tr.NotFound(",
				"// The request log word and the trace setting named at each place are the nearest\n",
			},
			wantNotRouter: []string{"outletRequestLog", "for _, prefix := range", "tracer.Surfaces", "DefaultPolicy", "PolicyByPrefix"},
			wantNoImports: []string{"\t\"strings\"\n", "\t\"cloud.google.com/go/logging\"\n"},
			wantRouterTest: []string{
				"\t\t{name: \"the application default, always, since nothing is declared, on a quiet request\", method: http.MethodGet, url: \"/generated-router-probe\", status: 200, want: \"written\"},\n",
				"\t\t{name: \"the default outlet's word, on event, on a quiet request\", method: http.MethodGet, url: \"/api/generated-router-probe\", status: 200, want: \"dropped\"},\n",
				"\t\t{name: \"the default outlet's word, on event, on a failed request\", method: http.MethodGet, url: \"/api/generated-router-failure\", status: 404, want: \"written\"},\n",
			},
			wantNotRouterTest: []string{"which takes the application default", "mounted by hand, ", "the route's own word, "},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			data := tt.r.servedRouterData(tt.outlets, nil, tt.fileRoutes, tt.wordedRoutes)
			router := render(t, tt.r, "servedRouterTemplate", servedRouterTemplate, data)
			assertContains(t, "router", router, tt.wantRouter, tt.wantNotRouter)
			assertContains(t, "router imports", router, tt.wantRouterImports, tt.wantNoImports)
			routerTest := render(t, tt.r, "servedRouterTestTemplate", servedRouterTestTemplate, data)
			assertContains(t, "router test", routerTest, tt.wantRouterTest, tt.wantNotRouterTest)
		})
	}
}
