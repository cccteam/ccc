package generation

import (
	"regexp"
	"strings"
	"testing"
)

// fileTemplates returns every file-level template by name: templates whose
// output is written as a complete generated file.
func fileTemplates() map[string]string {
	return map[string]string{
		"resourcesInterfaceTemplate":      resourcesInterfaceTemplate,
		"resourceFileTemplate":            resourceFileTemplate,
		"handlerHeaderTemplate":           handlerHeaderTemplate,
		"consolidatedPatchTemplate":       consolidatedPatchTemplate,
		"resourceEnumsTemplate":           resourceEnumsTemplate,
		"typescriptConstantsTemplate":     typescriptConstantsTemplate,
		"typescriptResourcesTemplate":     typescriptResourcesTemplate,
		"typescriptMethodsTemplate":       typescriptMethodsTemplate,
		"typescriptEnumsTemplate":         typescriptEnumsTemplate,
		"collectionTemplate":              collectionTemplate,
		"routesTemplate":                  routesTemplate,
		"routerTestTemplate":              routerTestTemplate,
		"rpcFileTemplate":                 rpcFileTemplate,
		"rpcHandlerTemplate":              rpcHandlerTemplate,
		"rpcInterfacesTemplate":           rpcInterfacesTemplate,
		"computedResourceHandlerTemplate": computedResourceHandlerTemplate,
		"domainGuardTemplate":             domainGuardTemplate,
		"decodersTemplate":                decodersTemplate,
		"appContractTemplate":             appContractTemplate,
		"handlerTestsMainTemplate":        handlerTestsMainTemplate,
		"authzTestTemplate":               authzTestTemplate,
	}
}

// Test_decodersTemplate_gating pins that each generated decoder constructor is emitted
// only when a generated handler calls it, and that every emitted constructor delegates
// to the library's Must* implementation under the generated closed unions.
func Test_decodersTemplate_gating(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		data            decodersFileData
		wantContains    []string
		wantNotContains []string
	}{
		{
			name: "all features emit all constructors",
			data: decodersFileData{
				Package:                 "app",
				ApplicationName:         "App",
				ReceiverName:            "a",
				RPCPackage:              "rpc",
				RouterPackage:           "router",
				HasQueryDecoder:         true,
				HasComputedQueryDecoder: true,
				HasPatchDecoder:         true,
				HasRPCDecoder:           true,
			},
			wantContains: []string{
				"func NewQueryDecoder[Resource Resourcer, Request any](permissions ...accesstypes.Permission) *resource.QueryDecoder[Resource, Request] {",
				"resource.MustNewQueryDecoder[Resource, Request](router.Collection(), permissions...)",
				"func NewComputedQueryDecoder[Resource Resourcer, Request any](permissions ...accesstypes.Permission) *resource.ComputedQueryDecoder[Resource, Request] {",
				"resource.MustNewComputedQueryDecoder[Resource, Request](permissions...)",
				"func NewDecoder[Resource Resourcer, Request any](a *App, permissions ...accesstypes.Permission) *resource.Decoder[Resource, Request] {",
				"resource.MustNewDecoder[Resource, Request](a, router.Collection(), permissions...)",
				"func NewRPCDecoder[Method rpc.Method, Request any](a *App, perm accesstypes.Permission) *resource.RPCDecoder[Request] {",
				"resource.MustNewRPCDecoder[Request](a, method.Method(), perm)",
			},
		},
		{
			name: "query-only emits no computed, patch, or RPC constructor",
			data: decodersFileData{
				Package:         "app",
				ApplicationName: "App",
				ReceiverName:    "a",
				HasQueryDecoder: true,
			},
			wantContains:    []string{"func NewQueryDecoder["},
			wantNotContains: []string{"func NewComputedQueryDecoder[", "func NewDecoder[", "func NewRPCDecoder["},
		},
		{
			name: "computed-only emits only the computed constructor",
			data: decodersFileData{
				Package:                 "app",
				ApplicationName:         "App",
				ReceiverName:            "a",
				HasComputedQueryDecoder: true,
			},
			wantContains:    []string{"func NewComputedQueryDecoder["},
			wantNotContains: []string{"func NewQueryDecoder[", "func NewDecoder[", "func NewRPCDecoder["},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c := &client{}
			out, err := c.generateTemplateOutput("decodersTemplate", decodersTemplate, tt.data)
			if err != nil {
				t.Fatalf("generateTemplateOutput() error = %v", err)
			}

			for _, want := range tt.wantContains {
				if !strings.Contains(string(out), want) {
					t.Errorf("decodersTemplate output missing %q:\n%s", want, out)
				}
			}
			for _, notWant := range tt.wantNotContains {
				if strings.Contains(string(out), notWant) {
					t.Errorf("decodersTemplate output must not contain %q:\n%s", notWant, out)
				}
			}
		})
	}
}

// Test_computedResourceHandlerTemplate_decoder pins the enforcement seam of the
// generated computed handlers: they must construct the eager-checking
// ComputedQueryDecoder, never the deferred QueryDecoder — computed resources execute
// application code, so a deferred check is never discharged.
func Test_computedResourceHandlerTemplate_decoder(t *testing.T) {
	t.Parallel()

	if want := "NewComputedQueryDecoder["; !strings.Contains(computedResourceHandlerTemplate, want) {
		t.Errorf("computedResourceHandlerTemplate missing %q", want)
	}
	if notWant := " NewQueryDecoder["; strings.Contains(computedResourceHandlerTemplate, notWant) {
		t.Errorf("computedResourceHandlerTemplate must not construct the deferred QueryDecoder: found %q", notWant)
	}
}

// Test_appContractTemplate_gating pins the generated app contract: the resource block
// is unconditional; every other block is emitted only while its feature generates a
// caller. Methods with generator-known signatures are asserted through interfaces;
// RPCClient and ComputedClient (application-owned return types) through method
// expressions that assert existence only.
func Test_appContractTemplate_gating(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		data            appContractData
		wantContains    []string
		wantNotContains []string
	}{
		{
			name: "all features emit all contract blocks",
			data: appContractData{
				Package:         "app",
				ApplicationName: "App",
				HasValidator:    true,
				HasDomainScoped: true,
				HasRPC:          true,
				HasComputed:     true,
			},
			wantContains: []string{
				"UserPermissions(r *http.Request) resource.UserPermissions",
				"ResourceClient() resource.Client",
				"var _ resourceApp = (*App)(nil)",
				"Validator() resource.ValidatorFunc",
				"var _ validatorApp = (*App)(nil)",
				"DomainExists(ctx context.Context, domain accesstypes.Domain) (bool, error)",
				"DomainGuard() func(http.HandlerFunc) http.HandlerFunc",
				"var _ domainScopedApp = (*App)(nil)",
				"var _ = (*App).RPCClient",
				"var _ = (*App).ComputedClient",
			},
		},
		{
			name: "concealed domains swap the contract method to DomainVisible",
			data: appContractData{
				Package:          "app",
				ApplicationName:  "App",
				HasDomainScoped:  true,
				ConcealedDomains: true,
			},
			wantContains: []string{
				"DomainVisible(ctx context.Context, user accesstypes.User, domain accesstypes.Domain) (bool, error)",
				"DomainGuard() func(http.HandlerFunc) http.HandlerFunc",
				"var _ domainScopedApp = (*App)(nil)",
			},
			wantNotContains: []string{"DomainExists"},
		},
		{
			name: "query-only app asserts the resource surface alone",
			data: appContractData{
				Package:         "app",
				ApplicationName: "App",
			},
			wantContains: []string{"var _ resourceApp = (*App)(nil)"},
			wantNotContains: []string{
				"validatorApp",
				"domainScopedApp",
				"RPCClient",
				"ComputedClient",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c := &client{}
			out, err := c.generateTemplateOutput("appContractTemplate", appContractTemplate, tt.data)
			if err != nil {
				t.Fatalf("generateTemplateOutput() error = %v", err)
			}

			for _, want := range tt.wantContains {
				if !strings.Contains(string(out), want) {
					t.Errorf("appContractTemplate output missing %q:\n%s", want, out)
				}
			}
			for _, notWant := range tt.wantNotContains {
				if strings.Contains(string(out), notWant) {
					t.Errorf("appContractTemplate output must not contain %q:\n%s", notWant, out)
				}
			}
		})
	}
}

// Test_authzTestTemplate pins the generated authorization matrix: query cases render a
// denied row expecting 403 and a granted row (exactly the required permission)
// expecting 200/404; mutation cases (DeniedOnly) render the denied row alone, carrying
// their minimal body — all driven through the hand-written newTestHandler hook.
func Test_authzTestTemplate(t *testing.T) {
	t.Parallel()

	c := &client{}
	out, err := c.generateTemplateOutput("authzTestTemplate", authzTestTemplate, authzTestData{
		Package: "handlertests",
		Cases: []authzCase{
			{Name: "Widgets", Method: "http.MethodGet", URL: "/api/widgets", Permission: "List"},
			{Name: "Widget", Method: "http.MethodGet", URL: "/api/widgets/1", Permission: "Read"},
			{Name: "PatchWidgets delete", Method: "http.MethodPatch", URL: "/api/widgets", Body: `[{"op":"remove","path":"/1"}]`, DeniedOnly: true},
			{Name: "LaunchWidget", Method: "http.MethodPost", URL: "/api/launch-widget", Body: "{}", DeniedOnly: true},
		},
	})
	if err != nil {
		t.Fatalf("generateTemplateOutput() error = %v", err)
	}

	for _, want := range []string{
		`name:         "Widgets denied",`,
		`wantStatuses: []int{http.StatusForbidden},`,
		`grants:       grants{accesstypes.List: true},`,
		`grants:       grants{accesstypes.Read: true},`,
		`target:       "/api/widgets/1",`,
		`wantStatuses: []int{http.StatusOK, http.StatusNotFound},`,
		`name:         "PatchWidgets delete denied",`,
		"body:         `[{\"op\":\"remove\",\"path\":\"/1\"}]`,",
		`name:         "LaunchWidget denied",`,
		"h := newTestHandler(t, db, tt.grants)",
	} {
		if !strings.Contains(string(out), want) {
			t.Errorf("authzTestTemplate output missing %q:\n%s", want, out)
		}
	}

	// DeniedOnly cases must not render a granted row.
	for _, notWant := range []string{
		`"PatchWidgets delete granted"`,
		`"LaunchWidget granted"`,
	} {
		if strings.Contains(string(out), notWant) {
			t.Errorf("authzTestTemplate output must not contain %q:\n%s", notWant, out)
		}
	}
}

// Test_routesTemplate_testRouter pins the generated test-composition seam: the routes
// file must expose NewTestRouter — the bare generated route table plus the
// route-parameter middleware the handlers require — so test suites compose the API
// surface through a generated constructor instead of handwritten test-only routers.
func Test_routesTemplate_testRouter(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"func NewTestRouter(h GeneratedHandlers) *chi.Mux {",
		"r.Use(httpio.WithParams)",
		"generatedRoutes(r, h)",
	} {
		if !strings.Contains(routesTemplate, want) {
			t.Errorf("routesTemplate missing %q", want)
		}
	}
}

// Test_routerTestTemplate_selfContained pins that the generated router test carries no
// dependency on app-maintained artifacts: the handlers stub, call recorder, and
// TestGeneratedRoutes are all emitted by the template itself, the stub's DomainGuard
// pass-through appears exactly when domain-scoped routes exist, and nothing references
// a mockgen package or an app-named interface.
func Test_routerTestTemplate_selfContained(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		data            routerFileData
		wantContains    []string
		wantNotContains []string
	}{
		{
			name: "domain-scoped consolidated app emits guard pass-through and dispatch test",
			data: routerFileData{
				Package: "router",
				RoutesMap: map[string][]*generatedRoute{
					"Widget": {{Method: "GET", Path: "/api/widgets", HandlerFunc: "Widgets"}},
				},
				HasDomainScopedRoutes:  true,
				StubDomainGuard:        true,
				HasConsolidatedHandler: true,
			},
			wantContains: []string{
				"func (rec *generatedCallRecorder) RecordMiddlewareCall(name string) func(next http.Handler) http.Handler {",
				"func (rec *generatedCallRecorder) MiddlewareCount() int {",
				"func (rec *generatedCallRecorder) MiddlewareCallCount(name string) int {",
				"func newGeneratedHandlersStub(record func(handlerName string) http.HandlerFunc) *generatedHandlersStub {",
				"func (s *generatedHandlersStub) DomainGuard() func(http.HandlerFunc) http.HandlerFunc {",
				"return func(next http.HandlerFunc) http.HandlerFunc { return next }",
				"func (s *generatedHandlersStub) Widgets() http.HandlerFunc {",
				`return s.record("Widgets")`,
				"func (s *generatedHandlersStub) PatchResources() http.HandlerFunc {",
				"func TestGeneratedRoutes(t *testing.T) {",
				"router := NewTestRouter(newGeneratedHandlersStub(rec.RecordHandlerCall))",
			},
			wantNotContains: []string{"mock_router", "MockHandlers"},
		},
		{
			name: "without domain-scoped routes or a consolidated handler the stub stays minimal",
			data: routerFileData{
				Package: "router",
				RoutesMap: map[string][]*generatedRoute{
					"Widget": {{Method: "GET", Path: "/api/widgets", HandlerFunc: "Widgets"}},
				},
			},
			wantContains:    []string{"func (s *generatedHandlersStub) Widgets() http.HandlerFunc {"},
			wantNotContains: []string{"DomainGuard", "PatchResources", "mock_router"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c := &client{}
			out, err := c.generateTemplateOutput("routerTestTemplate", routerTestTemplate, tt.data)
			if err != nil {
				t.Fatalf("generateTemplateOutput() error = %v", err)
			}

			for _, want := range tt.wantContains {
				if !strings.Contains(string(out), want) {
					t.Errorf("routerTestTemplate output missing %q:\n%s", want, out)
				}
			}
			for _, notWant := range tt.wantNotContains {
				if strings.Contains(string(out), notWant) {
					t.Errorf("routerTestTemplate output must not contain %q:\n%s", notWant, out)
				}
			}
		})
	}
}

// Test_routesTemplate_outlets pins the per-outlet rendering: each extra outlet gets
// its own suffixed interface and registration function, the test router becomes the
// all-outlet composition over the AllGeneratedHandlers union, and none of it renders
// without extra outlets.
func Test_routesTemplate_outlets(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		data            routerFileData
		wantContains    []string
		wantNotContains []string
	}{
		{
			name: "extra outlet renders suffixed surface and all-outlet test router",
			data: routerFileData{
				Package: "router",
				RoutesMap: map[string][]*generatedRoute{
					"Widget": {{Method: "GET", Path: "/api/widgets", HandlerFunc: "Widgets", HandlerType: ListHandler}},
				},
				ExtraOutlets: []*outletRouteData{{
					Name:   "automation",
					Suffix: "Automation",
					RoutesMap: map[string][]*generatedRoute{
						"Widget": {{Method: "GET", Path: "/automation/widgets", HandlerFunc: "Widgets", HandlerType: ListHandler}},
						"Gadget": {{Method: "PATCH", Path: "/automation/stations/{stationID}/gadgets", HandlerFunc: "PatchGadgets", DomainScoped: true}},
					},
					HasDomainScopedRoutes:   true,
					HasConsolidatedHandler:  true,
					ConsolidatedHandlerFunc: "PatchAutomationResources",
					ConsolidatedPath:        "/automation/resources",
				}},
			},
			wantContains: []string{
				"type GeneratedAutomationHandlers interface {",
				"func generatedAutomationRoutes(r chi.Router, h GeneratedAutomationHandlers) {",
				`r.Get("/automation/widgets", widgetsHandler)`,
				`r.Patch("/automation/stations/{stationID}/gadgets", domainGuard(h.PatchGadgets()))`,
				`r.Patch("/automation/resources", h.PatchAutomationResources())`,
				"PatchAutomationResources() http.HandlerFunc",
				"type AllGeneratedHandlers interface {",
				"GeneratedAutomationHandlers",
				"func NewTestRouter(h AllGeneratedHandlers) *chi.Mux {",
				"generatedAutomationRoutes(r, h)",
			},
			wantNotContains: []string{
				"func NewTestRouter(h GeneratedHandlers) *chi.Mux {",
				// A session-less outlet acquires no permission routes and no
				// PermissionDigest/UserDomains requirement of its own.
				`r.Get("/automation/permission-digest"`,
				`r.Get("/automation/user-domains"`,
			},
		},
		{
			name: "a session-serving outlet registers the permission routes under its prefix",
			data: routerFileData{
				Package: "router",
				RoutesMap: map[string][]*generatedRoute{
					"Widget": {{Method: "GET", Path: "/api/widgets", HandlerFunc: "Widgets", HandlerType: ListHandler}},
				},
				ExtraOutlets: []*outletRouteData{{
					Name:           "portal",
					Suffix:         "Portal",
					Prefix:         "portal",
					ServesSessions: true,
					RoutesMap: map[string][]*generatedRoute{
						"Widget": {{Method: "GET", Path: "/portal/widgets", HandlerFunc: "Widgets", HandlerType: ListHandler}},
					},
				}},
			},
			wantContains: []string{
				"type GeneratedPortalHandlers interface {",
				`r.Get("/portal/permission-digest", h.PermissionDigest())`,
				`r.Get("/portal/user-domains", h.UserDomains())`,
			},
		},
		{
			name: "no extra outlets renders the single-outlet file",
			data: routerFileData{
				Package: "router",
				RoutesMap: map[string][]*generatedRoute{
					"Widget": {{Method: "GET", Path: "/api/widgets", HandlerFunc: "Widgets", HandlerType: ListHandler}},
				},
			},
			wantContains: []string{
				"func NewTestRouter(h GeneratedHandlers) *chi.Mux {",
			},
			wantNotContains: []string{"AllGeneratedHandlers", "generatedAutomationRoutes"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c := &client{}
			out, err := c.generateTemplateOutput("routesTemplate", routesTemplate, tt.data)
			if err != nil {
				t.Fatalf("generateTemplateOutput() error = %v", err)
			}

			for _, want := range tt.wantContains {
				if !strings.Contains(string(out), want) {
					t.Errorf("routesTemplate output missing %q:\n%s", want, out)
				}
			}
			for _, notWant := range tt.wantNotContains {
				if strings.Contains(string(out), notWant) {
					t.Errorf("routesTemplate output must not contain %q:\n%s", notWant, out)
				}
			}
		})
	}
}

// Test_permissionsTemplate_sessionOutlets pins the handlers' doc comments: with only
// the default outlet the wording is unchanged; with an additional session-serving
// outlet the comments say the routes also ride that outlet's prefix.
func Test_permissionsTemplate_sessionOutlets(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		data            permissionsData
		wantContains    []string
		wantNotContains []string
	}{
		{
			name: "default outlet only keeps today's wording",
			data: permissionsData{Package: "app", ApplicationName: "App", ReceiverName: "a", RoutePrefix: "api"},
			wantContains: []string{
				"registers it at GET /api/permission-digest.",
				"GET /api/user-domains.",
			},
			wantNotContains: []string{"additional session-serving"},
		},
		{
			name: "extra session outlets extend the comments",
			data: permissionsData{Package: "app", ApplicationName: "App", ReceiverName: "a", RoutePrefix: "api", HasExtraSessionOutlets: true},
			wantContains: []string{
				"registers it at GET /api/permission-digest and,",
				"for each additional session-serving outlet (ServesSessions), under that outlet's prefix.",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c := &client{}
			out, err := c.generateTemplateOutput("permissionsTemplate", permissionsTemplate, tt.data)
			if err != nil {
				t.Fatalf("generateTemplateOutput() error = %v", err)
			}

			for _, want := range tt.wantContains {
				if !strings.Contains(string(out), want) {
					t.Errorf("permissionsTemplate output missing %q:\n%s", want, out)
				}
			}
			for _, notWant := range tt.wantNotContains {
				if strings.Contains(string(out), notWant) {
					t.Errorf("permissionsTemplate output must not contain %q:\n%s", notWant, out)
				}
			}
		})
	}
}

// Test_routesTemplate_sessionOutletHandlers pins that a session-serving outlet's
// interface requires the permission handlers itself: the methods appear once in the
// default GeneratedHandlers interface and once in the outlet's.
func Test_routesTemplate_sessionOutletHandlers(t *testing.T) {
	t.Parallel()

	data := routerFileData{
		Package:   "router",
		RoutesMap: map[string][]*generatedRoute{},
		ExtraOutlets: []*outletRouteData{{
			Name:           "portal",
			Suffix:         "Portal",
			Prefix:         "portal",
			ServesSessions: true,
			RoutesMap:      map[string][]*generatedRoute{},
		}},
	}

	c := &client{}
	out, err := c.generateTemplateOutput("routesTemplate", routesTemplate, data)
	if err != nil {
		t.Fatalf("generateTemplateOutput() error = %v", err)
	}

	for _, method := range []string{"PermissionDigest() http.HandlerFunc", "UserDomains() http.HandlerFunc"} {
		if got := strings.Count(string(out), method); got != 2 {
			t.Errorf("%q appears %d times, want 2 (default interface and portal interface):\n%s", method, got, out)
		}
	}
}

// Test_routerTestTemplate_outletIsolation pins the outlet dimension of the generated
// router tests: extra-outlet consolidated dispatch cases, stub methods for handlers
// served only under extra outlets, and the 404 isolation test — none of which render
// without extra outlets.
func Test_routerTestTemplate_outletIsolation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		data            routerFileData
		wantContains    []string
		wantNotContains []string
	}{
		{
			name: "extra outlets render isolation test, extra stubs, and consolidated dispatch case",
			data: routerFileData{
				Package: "router",
				RoutesMap: map[string][]*generatedRoute{
					"Widget": {{Method: "GET", Path: "/api/widgets", HandlerFunc: "Widgets", HandlerType: ListHandler}},
				},
				ExtraOutlets: []*outletRouteData{
					{
						Name:                    "automation",
						Suffix:                  "Automation",
						Prefix:                  "automation",
						RoutesMap:               map[string][]*generatedRoute{},
						HasConsolidatedHandler:  true,
						ConsolidatedHandlerFunc: "PatchAutomationResources",
						ConsolidatedPath:        "/automation/resources",
					},
					{
						Name:           "portal",
						Suffix:         "Portal",
						Prefix:         "portal",
						ServesSessions: true,
						RoutesMap:      map[string][]*generatedRoute{},
					},
				},
				ExtraStubHandlerFuncs: []string{"PatchAutomationResources"},
				NegativeRouterTests: []negativeRouterTest{
					{Method: "http.MethodGet", URL: "/automation/widgets"},
				},
			},
			wantContains: []string{
				"func TestGeneratedRouteOutletIsolation(t *testing.T) {",
				`{url: "/automation/widgets", method: http.MethodGet},`,
				"if got := rr.Code; got != http.StatusNotFound {",
				"func (s *generatedHandlersStub) PatchAutomationResources() http.HandlerFunc {",
				`url: "/automation/resources", method: http.MethodPatch,`,
				`handlerFunc: "PatchAutomationResources",`,
				// The session-serving outlet's permission routes get dispatch cases;
				// the session-less outlet's do not.
				`url: "/portal/permission-digest", method: http.MethodGet,`,
				`url: "/portal/user-domains", method: http.MethodGet,`,
			},
			wantNotContains: []string{
				`url: "/automation/permission-digest"`,
				`url: "/automation/user-domains"`,
			},
		},
		{
			name: "no extra outlets renders no isolation test",
			data: routerFileData{
				Package: "router",
				RoutesMap: map[string][]*generatedRoute{
					"Widget": {{Method: "GET", Path: "/api/widgets", HandlerFunc: "Widgets", HandlerType: ListHandler}},
				},
			},
			wantNotContains: []string{"TestGeneratedRouteOutletIsolation"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c := &client{}
			out, err := c.generateTemplateOutput("routerTestTemplate", routerTestTemplate, tt.data)
			if err != nil {
				t.Fatalf("generateTemplateOutput() error = %v", err)
			}

			for _, want := range tt.wantContains {
				if !strings.Contains(string(out), want) {
					t.Errorf("routerTestTemplate output missing %q:\n%s", want, out)
				}
			}
			for _, notWant := range tt.wantNotContains {
				if strings.Contains(string(out), notWant) {
					t.Errorf("routerTestTemplate output must not contain %q:\n%s", notWant, out)
				}
			}
		})
	}
}

// Test_consolidatedPatchTemplate_handlerName pins that the consolidated dispatcher's
// method name comes from the template data, so each outlet's dispatcher gets its own
// name instead of a hard-coded PatchResources.
func Test_consolidatedPatchTemplate_handlerName(t *testing.T) {
	t.Parallel()

	if !strings.Contains(consolidatedPatchTemplate, "{{ .HandlerName }}() http.HandlerFunc") {
		t.Error("consolidatedPatchTemplate must take the dispatcher method name from .HandlerName")
	}
	if strings.Contains(consolidatedPatchTemplate, ") PatchResources() http.HandlerFunc") {
		t.Error("consolidatedPatchTemplate must not hard-code the PatchResources method name")
	}
}

func Test_fileTemplates_generationHeader(t *testing.T) {
	t.Parallel()

	for name, tmpl := range fileTemplates() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if !strings.HasPrefix(tmpl, generationHeader+"\n") {
				t.Errorf("%s must start with generationHeader %q so removeGeneratedFileByHeaderComment can clean up its output; got %q", name, generationHeader, firstLine(tmpl))
			}
		})
	}
}

func firstLine(s string) string {
	if before, _, ok := strings.Cut(s, "\n"); ok {
		return before
	}

	return s
}

// declaredTemplateImports returns the import paths declared in a template's
// import block (both the parenthesized and single-import forms).
func declaredTemplateImports(tmpl string) []string {
	templateImportPath := regexp.MustCompile(`(?m)^\t(?:[\w.]+ )?"([^"]+)"$|^import (?:[\w.]+ )?"([^"]+)"$`)

	var paths []string
	for _, match := range templateImportPath.FindAllStringSubmatch(tmpl, -1) {
		if match[1] != "" {
			paths = append(paths, match[1])
		} else {
			paths = append(paths, match[2])
		}
	}

	return paths
}

// Test_stdlibImports_doesNotShadowTemplateImports pins the importFixer's stdlib
// seed exclusion rule: a qualifier that any template resolves to a third-party
// package (errors -> go-playground/errors, cmp -> go-cmp/cmp) must never appear
// in the seed. Otherwise a template that references the qualifier without
// declaring the third-party import would silently get the stdlib package
// instead of falling back to goimports resolution.
func Test_stdlibImports_doesNotShadowTemplateImports(t *testing.T) {
	t.Parallel()

	for name, tmpl := range fileTemplates() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			seed := stdlibImports()
			for _, path := range declaredTemplateImports(tmpl) {
				if root, _, _ := strings.Cut(path, "/"); !strings.Contains(root, ".") {
					continue // standard library path: cannot shadow itself
				}

				qualifier := assumedPackageName(path)
				if seedPath, ok := seed[qualifier]; ok {
					t.Errorf("stdlibImports() maps %q to %q, shadowing %q declared by %s: remove the seed entry — the seed must not contain qualifiers that generated code resolves to third-party packages", qualifier, seedPath, path, name)
				}
			}
		})
	}
}
