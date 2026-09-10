package generation

import (
	"strings"
	"testing"
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
			if got.servesSessions != tt.want.servesSessions || got.declaredSessions != tt.want.declaredSessions || got.apiKey != tt.want.apiKey || got.webApp != tt.want.webApp {
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
// authenticates one way with it, the reserved names stay free, and the browser
// applications' mount paths are distinct and beside the API prefixes.
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
			name:    "the switch without routes",
			options: []ResourceOption{GenerateRouter()},
			wantErr: "GenerateRouter requires GenerateRoutes",
		},
		{
			name: "the full shape",
			options: []ResourceOption{
				GenerateRouter(),
				GenerateRoutes("pkg/router", "api", staff, WebApp("/")),
				WithRouterOutlet("portal", "portal/api", members, WebApp("/portal")),
				WithRouterOutlet("machines", "machines", APIKey()),
			},
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
			name:    "an API prefix under a browser application is the ordinary layout",
			options: []ResourceOption{GenerateRouter(), GenerateRoutes("pkg/router", "api", staff), WithRouterOutlet("portal", "portal/api", members, WebApp("/portal"))},
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
// prefix, and the browser applications longest mount first.
func Test_servedRouterData(t *testing.T) {
	t.Parallel()

	crew := &outletAuth{importPath: "example.com/acme/beacon/pkg/auth/crew", flavor: Password}
	members := &outletAuth{importPath: "example.com/acme/beacon/pkg/auth/members", flavor: OIDCAzure}

	tests := []struct {
		name    string
		outlets []routerOutlet
		check   func(t *testing.T, data *servedRouterData)
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
				if len(data.Flavors) != 1 || data.Flavors[0].StubType != "routerOIDCAzureStub" {
					t.Errorf("flavors = %+v", data.Flavors)
				}
			},
		},
		{
			name: "two session auths and an API key",
			outlets: []routerOutlet{
				{name: "default", prefix: "api", servesSessions: true, auth: crew, webApp: "/"},
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
				if data.WebApps[0].Mount != "/portal" || data.WebApps[0].DeepLink != "PortalDeepLink" || data.WebApps[1].Mount != "/" {
					t.Errorf("web apps = %+v, %+v; want /portal first", data.WebApps[0], data.WebApps[1])
				}
				if strings.Join(data.NotFoundPrefixes, ",") != "/api/,/droids/,/portal/api/" {
					t.Errorf("not-found prefixes = %v", data.NotFoundPrefixes)
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
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := &resourceGenerator{client: &client{}}
			r.router = packageDir("pkg/router")
			r.resource = packageDir("pkg/resources")
			tt.check(t, r.servedRouterData(tt.outlets, nil))
		})
	}
}

// Test_servedRouterTemplates pins the rendered shapes: the chain comment opens the file,
// the flavor routes and guards render inline per outlet, BindAuth and the auth imports
// appear only with two session auths, an API-key group carries no session handling, and
// the test renders a recording stub per flavor in use. The cases format their output the
// way the generator does, and the aligner behind that is process-global, so they run
// sequentially.
func Test_servedRouterTemplates(t *testing.T) {
	crew := &outletAuth{importPath: "example.com/acme/beacon/pkg/auth/crew", flavor: Password}
	members := &outletAuth{importPath: "example.com/acme/beacon/pkg/auth/members", flavor: OIDCGoogle}

	tests := []struct {
		name                string
		outlets             []routerOutlet
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
				"//\tevery request: hooks.Outermost, LoggerMiddleware, SecurityHeaders, httpio.WithParams",
				"//\tdefault (/api), Azure directory sessions:",
				"//\t  NoCaching, CompressionMiddleware, StartSession, SetXSRFToken: GET /api/user/login, GET /api/user/callback, GET /api/user/session, DELETE /api/user/session, GET /api/user/logout",
				"//\t  + ValidateSession, ValidateXSRFToken: hooks.Default, generatedRoutes",
				"\tsession.OIDCAzureHandlers\n",
				"func New(h Handlers, hooks Hooks) *chi.Mux {",
				"\tr.Use(hooks.Outermost...)\n\tr.Use(h.LoggerMiddleware())\n\tr.Use(h.SecurityHeaders)\n\tr.Use(httpio.WithParams)\n",
				"\t\tr.Get(\"/api/user/logout\", h.FrontChannelLogout())\n",
				"\t\t\tr.Use(h.ValidateSession)\n\t\t\tr.Use(h.ValidateXSRFToken)\n\n\t\t\tregisterGenerated(r, hooks.Default, \"Default\", func(r chi.Router) {\n\t\t\t\tgeneratedRoutes(r, h)\n\t\t\t})",
				"\tfor _, prefix := range []string{\"/api/\"} {",
				"\tr.Route(\"/\", func(r chi.Router) {\n\t\tr.Use(h.DeepLink)\n\n\t\tr.Get(\"/*\", h.Assets())\n\t})",
				"\tDefault func(r chi.Router, generated func(chi.Router))\n}",
			},
			wantNotRouter:       []string{"BindAuth", "Generated" + "PortalHandlers", "Auth(next http.Handler)"},
			wantNoRouterImports: []string{"example.com/acme/beacon/pkg/config"},
			wantRouterTest: []string{
				"type routerOIDCAzureStub struct {\n\tsession.OIDCAzureHandlers",
				"func (s *routerOIDCAzureStub) FrontChannelLogout() http.HandlerFunc {",
				"\t\t{url: \"/api/user/logout\", suffix: \"/user/logout\", method: http.MethodGet, handler: \"FrontChannelLogout\"},",
				"\t\tguards: []string{\"ValidateSession\", \"ValidateXSRFToken\"},",
				"func TestGeneratedRouterWebApps(t *testing.T) {",
				"\t\t{url: \"/generated-router-page/deep/link\", handler: \"Assets\", deepLink: \"DeepLink\"},",
			},
			wantNotRouterTest: []string{"BindAuth", "routerPasswordStub"},
		},
		{
			name: "two session auths and an API-key outlet",
			outlets: []routerOutlet{
				{name: "default", prefix: "api", servesSessions: true, auth: crew, webApp: "/"},
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
				"\t\tr.Get(\"/portal/api/user/callback\", portalSession.CallbackOIDC())",
				"\tfor _, prefix := range []string{\"/api/\", \"/droids/\", \"/portal/api/\"} {",
				"\tr.Route(\"/portal\", func(r chi.Router) {\n\t\tr.Use(h.PortalDeepLink)\n\n\t\tr.Get(\"/*\", h.PortalAssets())\n\t})\n\n\t// The default outlet's browser application at /, the catch-all.\n\tr.Route(\"/\", func(r chi.Router) {",
				"\tDroids func(r chi.Router, generated func(chi.Router))",
			},
			wantNotRouter:     []string{"h.Portal().StartSession", "droidsSession"},
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
			},
			wantNotRouterTest: []string{"routerOIDCAzureStub"},
		},
		{
			name:    "an API-key default outlet alone imports no session package",
			outlets: []routerOutlet{{name: "default", prefix: "api", apiKey: true}},
			wantRouter: []string{
				"//\tdefault (/api), API key:",
				"\tDefaultAuth(next http.Handler) http.Handler\n",
			},
			wantNotRouter:       []string{"StartSession", "session.PasswordAuthHandlers", "session.OIDC"},
			wantNoRouterImports: []string{"github.com/cccteam/session"},
			wantNotRouterTest:   []string{"session.PasswordAuthHandlers", "session.OIDC", "TestGeneratedRouterWebApps", "func TestGeneratedRouterSessionRoutes"},
		},
	}
	r := &resourceGenerator{client: &client{}}
	r.router = packageDir("pkg/router")
	r.resource = packageDir("pkg/resources")
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := r.servedRouterData(tt.outlets, nil)

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
