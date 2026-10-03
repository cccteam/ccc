package generation

import (
	"go/format"
	"strings"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/google/go-cmp/cmp"
)

func Test_IsDomainScoped(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		scope accesstypes.PermissionScope
		want  bool
	}{
		{name: "absent annotation defaults to global", scope: "", want: false},
		{name: "explicit global", scope: accesstypes.GlobalPermissionScope, want: false},
		{name: "domain", scope: accesstypes.DomainPermissionScope, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := (&resourceInfo{PermissionScope: tt.scope}).IsDomainScoped(); got != tt.want {
				t.Errorf("resourceInfo.IsDomainScoped() = %v, want %v", got, tt.want)
			}
			if got := (&computedResource{PermissionScope: tt.scope}).IsDomainScoped(); got != tt.want {
				t.Errorf("computedResource.IsDomainScoped() = %v, want %v", got, tt.want)
			}
			if got := (&rpcMethodInfo{PermissionScope: tt.scope}).IsDomainScoped(); got != tt.want {
				t.Errorf("rpcMethodInfo.IsDomainScoped() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_resourceGenerator_routeBasePaths(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		resourceName  string
		domainScoped  bool
		domainSegment string
		domainParam   string
		wantPath      string
		wantTestURL   string
	}{
		{
			name:         "global scope has no domain segment",
			resourceName: "Widget",
			wantPath:     "/api/widgets",
			wantTestURL:  "/api/widgets",
		},
		{
			name:         "domain scope inserts the domains/{domain} pair after the prefix",
			resourceName: "Widget",
			domainScoped: true,
			wantPath:     "/api/domains/{domain}/widgets",
			wantTestURL:  "/api/domains/testDomain/widgets",
		},
		{
			name:          "the tenant record names the segment pair",
			resourceName:  "Widget",
			domainScoped:  true,
			domainSegment: "organizations",
			domainParam:   "organizationID",
			wantPath:      "/api/organizations/{organizationID}/widgets",
			wantTestURL:   "/api/organizations/testDomain/widgets",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := &resourceGenerator{client: &client{}, routePrefix: "api", domainRouteSegment: "domains", domainRouteParam: "domain"}
			if tt.domainSegment != "" {
				r.domainRouteSegment = tt.domainSegment
			}
			if tt.domainParam != "" {
				r.domainRouteParam = tt.domainParam
			}
			gotPath, gotTestURL := r.routeBasePaths(tt.resourceName, tt.domainScoped, r.routePrefix)
			if gotPath != tt.wantPath {
				t.Errorf("routeBasePaths() path = %q, want %q", gotPath, tt.wantPath)
			}
			if gotTestURL != tt.wantTestURL {
				t.Errorf("routeBasePaths() testURL = %q, want %q", gotTestURL, tt.wantTestURL)
			}
		})
	}
}

// Test_routerTestTemplate_domainRouteParam pins that the generated router test's
// parameter key list includes the domain route parameter whenever domain-scoped routes
// exist — the test's call recorder only captures parameters named in that list, so
// omitting it makes every domain-scoped route assertion fail with an empty value.
func Test_routerTestTemplate_domainRouteParam(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		hasDomainScoped bool
		wantContains    bool
	}{
		{name: "domain-scoped routes emit the domain param key", hasDomainScoped: true, wantContains: true},
		{name: "without domain-scoped routes the key is absent", hasDomainScoped: false, wantContains: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c := &client{}
			out, err := c.generateTemplateOutput("routerTestTemplate", routerTestTemplate, routerFileData{
				Package:          "router",
				HasDomainScoped:  tt.hasDomainScoped,
				DomainRouteParam: "stationID",
			})
			if err != nil {
				t.Fatalf("generateTemplateOutput() error = %v", err)
			}

			if got := strings.Contains(string(out), `"stationID",`); got != tt.wantContains {
				t.Errorf("generatedRouteParameters contains domain param = %v, want %v:\n%s", got, tt.wantContains, out)
			}
		})
	}
}

func Test_generatedRoute_prependDomainTestParam(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		domainScoped bool
		paramKey     string
		params       []routeTestParam
		want         []routeTestParam
	}{
		{
			name:     "global scope leaves params unchanged",
			paramKey: "domain",
			params:   []routeTestParam{{Key: "widgetID", Value: "testWidgetID"}},
			want:     []routeTestParam{{Key: "widgetID", Value: "testWidgetID"}},
		},
		{
			name:         "domain scope prepends the domain param",
			domainScoped: true,
			paramKey:     "domain",
			params:       []routeTestParam{{Key: "widgetID", Value: "testWidgetID"}},
			want: []routeTestParam{
				{Key: "domain", Value: "testDomain"},
				{Key: "widgetID", Value: "testWidgetID"},
			},
		},
		{
			name:         "domain scope on a paramless route with a custom param name",
			domainScoped: true,
			paramKey:     "organizationID",
			want:         []routeTestParam{{Key: "organizationID", Value: "testDomain"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			route := &generatedRoute{DomainScoped: tt.domainScoped, TestParams: tt.params}
			route.prependDomainTestParam(tt.paramKey)
			if diff := cmp.Diff(tt.want, route.TestParams); diff != "" {
				t.Errorf("TestParams mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_validateDomainParamCollision(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		params      []routeTestParam
		domainParam string
		wantErr     bool
	}{
		{
			name:        "no collision",
			params:      []routeTestParam{{Key: "widgetID", Value: "testWidgetID"}},
			domainParam: "domain",
		},
		{
			name:        "primary-key param equals domain param",
			params:      []routeTestParam{{Key: "widgetID", Value: "testWidgetID"}},
			domainParam: "widgetID",
			wantErr:     true,
		},
		{
			name:        "no params never collides",
			domainParam: "domain",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := &resourceGenerator{client: &client{}, domainRouteParam: tt.domainParam}
			err := r.validateDomainParamCollision(tt.params, "Widget")
			if (err != nil) != tt.wantErr {
				t.Errorf("validateDomainParamCollision() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func Test_consolidatedPatchResources(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadCollectionFixture(t))

	tests := []struct {
		name      string
		resources []*resourceInfo
		wantNames []string
	}{
		{
			name: "non-consolidated resources are excluded",
			resources: []*resourceInfo{
				fixtureResource(t, structs, "Fossil", nil),
			},
			wantNames: nil,
		},
		{
			name: "consolidated global-scoped resources pass",
			resources: []*resourceInfo{
				fixtureResource(t, structs, "Fossil", func(res *resourceInfo) { res.IsConsolidated = true }),
			},
			wantNames: []string{"Fossil"},
		},
		{
			name: "consolidated domain-scoped resources pass with domain-embedded operation paths",
			resources: []*resourceInfo{
				fixtureResource(t, structs, "Vault", func(res *resourceInfo) {
					res.IsConsolidated = true
					res.PermissionScope = accesstypes.DomainPermissionScope
				}),
			},
			wantNames: []string{"Vault"},
		},
		{
			name: "the consolidated tenant record passes alongside domain-scoped ones",
			resources: []*resourceInfo{
				fixtureResource(t, structs, "Station", func(res *resourceInfo) {
					res.IsConsolidated = true
					res.IsTenant = true
				}),
				fixtureResource(t, structs, "Vault", func(res *resourceInfo) {
					res.IsConsolidated = true
					res.PermissionScope = accesstypes.DomainPermissionScope
				}),
			},
			wantNames: []string{"Station", "Vault"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := &resourceGenerator{
				client:             &client{},
				domainRouteSegment: "stations",
				domainRouteParam:   "stationID",
			}
			r.resources = tt.resources
			got := r.consolidatedPatchResources()

			gotNames := make([]string, 0, len(got))
			for _, res := range got {
				gotNames = append(gotNames, res.Name())
			}
			if diff := cmp.Diff(tt.wantNames, gotNames); diff != "" && (len(tt.wantNames) != 0 || len(gotNames) != 0) {
				t.Errorf("consolidatedPatchResources() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// Test_handlerContent_domainSource pins the scope argument generated handlers pass to
// the decoders: accesstypes.GlobalScope() for global-scoped resources, the {domain}
// route parameter wrapped in accesstypes.DomainScope for domain-scoped ones.
func Test_handlerContent_domainSource(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadCollectionFixture(t))

	tests := []struct {
		name            string
		scope           accesstypes.PermissionScope
		handlerType     HandlerType
		wantContains    []string
		wantNotContains []string
	}{
		{
			name:        "global scope passes GlobalScope",
			scope:       "",
			handlerType: ListHandler,
			wantContains: []string{
				"decoder.Decode(r, a.UserPermissions(r), accesstypes.GlobalScope())",
			},
			wantNotContains: []string{"router.Domain", "DomainExists"},
		},
		{
			name:        "domain scope reads the route parameter for the tenant scope",
			scope:       accesstypes.DomainPermissionScope,
			handlerType: ListHandler,
			wantContains: []string{
				"domain := httpio.Param[accesstypes.Domain](r, router.Domain)",
				"decoder.Decode(r, a.UserPermissions(r), accesstypes.DomainScope(domain))",
			},
			wantNotContains: []string{"accesstypes.GlobalScope()", "DomainExists"},
		},
		{
			name:        "domain-scoped read handler reads the route parameter",
			scope:       accesstypes.DomainPermissionScope,
			handlerType: ReadHandler,
			wantContains: []string{
				"domain := httpio.Param[accesstypes.Domain](r, router.Domain)",
				"decoder.Decode(r, a.UserPermissions(r), accesstypes.DomainScope(domain))",
			},
			wantNotContains: []string{"accesstypes.GlobalScope()", "DomainExists"},
		},
		{
			name:        "domain-scoped patch handler reads the route parameter before the transaction",
			scope:       accesstypes.DomainPermissionScope,
			handlerType: PatchHandler,
			wantContains: []string{
				"domain := httpio.Param[accesstypes.Domain](r, router.Domain)",
				"accesstypes.DomainScope(domain)",
			},
			wantNotContains: []string{"accesstypes.GlobalScope()", "DomainExists"},
		},
		{
			name:            "global-scoped patch handler has no guard",
			scope:           "",
			handlerType:     PatchHandler,
			wantContains:    []string{"accesstypes.GlobalScope()"},
			wantNotContains: []string{"DomainExists", "router.Domain"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			res := fixtureResource(t, structs, "Fossil", func(res *resourceInfo) {
				res.PermissionScope = tt.scope
			})

			r := &resourceGenerator{
				client:          &client{},
				applicationName: "TestApp",
				receiverName:    "a",
			}
			out, err := r.handlerContent(tt.handlerType, res)
			if err != nil {
				t.Fatalf("handlerContent() error = %v", err)
			}

			for _, want := range tt.wantContains {
				if !strings.Contains(string(out), want) {
					t.Errorf("handlerContent() output missing %q:\n%s", want, out)
				}
			}
			for _, notWant := range tt.wantNotContains {
				if strings.Contains(string(out), notWant) {
					t.Errorf("handlerContent() output must not contain %q:\n%s", notWant, out)
				}
			}
		})
	}
}

// Test_domainParamLine_spliced pins that every handler template with a domain-scoped
// branch reads the domain route parameter — and none of them checks DomainExists: the
// unknown-domain guard is route middleware (DomainGuard), never handler code. The
// consolidated patch handler is deliberately absent: its route is global, so it keeps a
// per-operation-path check in-handler.
func Test_domainParamLine_spliced(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		template string
	}{
		{name: "list template", template: listTemplate},
		{name: "read template", template: readTemplate},
		{name: "patch template", template: patchTemplate},
		{name: "rpc handler template", template: rpcHandlerTemplate},
		{name: "computed handler template", template: computedResourceHandlerTemplate},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if !strings.Contains(tt.template, domainParamLine) {
				t.Errorf("template does not splice domainParamLine")
			}
			if strings.Contains(tt.template, "DomainExists") {
				t.Errorf("template checks DomainExists; the unknown-domain guard belongs to the DomainGuard route middleware")
			}
		})
	}
}

// Test_routesTemplate_domainGuard pins the route-level guard wrapping: GeneratedHandlers
// requires DomainGuard exactly when domain-scoped routes exist, and generatedRoutes
// wraps domain-scoped routes — and only those — in it. Wrapping is per-route, never
// per-subtree: a tenant-record read route (global, sharing the segment pair's position)
// must register unguarded.
func Test_routesTemplate_domainGuard(t *testing.T) {
	t.Parallel()

	guardedRoutes := map[string][]*generatedRoute{
		"Vault": {
			{Method: "GET", Path: "/api/stations/{stationID}/vaults", HandlerFunc: "Vaults", HandlerType: ListHandler, DomainScoped: true},
			{Method: "PATCH", Path: "/api/stations/{stationID}/vaults", HandlerFunc: "PatchVaults", HandlerType: PatchHandler, DomainScoped: true},
		},
		"Station": {
			{Method: "GET", Path: "/api/stations/{stationID}", HandlerFunc: "Station", HandlerType: ReadHandler},
		},
		"Reindex": {
			{Method: "POST", Path: "/api/stations/{stationID}/reindex", HandlerFunc: "Reindex", DomainScoped: true},
		},
	}

	tests := []struct {
		name            string
		data            routerFileData
		wantContains    []string
		wantNotContains []string
	}{
		{
			name: "domain-scoped routes wrap in DomainGuard, global routes register bare",
			data: routerFileData{
				Package:               "router",
				RoutesMap:             guardedRoutes,
				HasDomainScoped:       true,
				HasDomainScopedRoutes: true,
				DomainRouteParam:      "stationID",
			},
			wantContains: []string{
				"DomainGuard() func(http.HandlerFunc) http.HandlerFunc",
				"domainGuard := h.DomainGuard()",
				// Shared handlers (GET+POST) wrap once at the variable.
				`vaultsHandler := domainGuard(h.Vaults())`,
				`r.Patch("/api/stations/{stationID}/vaults", domainGuard(h.PatchVaults()))`,
				`r.Post("/api/stations/{stationID}/reindex", domainGuard(h.Reindex()))`,
				// The tenant-record read route is global: bare registration.
				`stationHandler := h.Station()`,
				`r.Get("/api/stations/{stationID}", stationHandler)`,
			},
			wantNotContains: []string{"domainGuard(stationHandler)"},
		},
		{
			name: "without domain-scoped routes the guard does not exist",
			data: routerFileData{
				Package: "router",
				RoutesMap: map[string][]*generatedRoute{
					"Widget": {{Method: "GET", Path: "/api/widgets", HandlerFunc: "Widgets", HandlerType: ListHandler}},
				},
			},
			wantNotContains: []string{"DomainGuard", "domainGuard"},
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

// Test_domainGuardTemplate pins the generated DomainGuard middleware body: resolve the
// domain route parameter, answer 404 when the application's tenant roster does not
// hold it, then, under concealment alone, answer the same 404 when the caller holds no
// grant in it, and only then run the wrapped handler. The roster is asked first in
// both modes, and the application's own seam (DomainExists, DomainVisible) is gone.
func Test_domainGuardTemplate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		concealed       bool
		wantContains    []string
		wantNotContains []string
	}{
		{
			name: "the roster alone answers",
			wantContains: []string{
				"func (a *App) DomainGuard() func(http.HandlerFunc) http.HandlerFunc {",
				"domain := httpio.Param[accesstypes.Domain](r, router.Domain)",
				"if !a.TenantRoster().Has(domain) {",
				`httpio.NewNotFoundMessagef("unknown domain %q", domain)`,
				"next.ServeHTTP(w, r)",
			},
			wantNotContains: []string{"HasGrants", "DomainExists", "DomainVisible"},
		},
		{
			name:      "concealed domains ask the roster, then the caller's foothold",
			concealed: true,
			wantContains: []string{
				"if !a.TenantRoster().Has(domain) {",
				"if ok, err := a.UserPermissions(r).HasGrants(ctx, accesstypes.DomainScope(domain)); err != nil {",
				`httpio.NewNotFoundMessagef("unknown domain %q", domain)`,
			},
			wantNotContains: []string{"DomainExists", "DomainVisible"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c := &client{}
			out, err := c.generateTemplateOutput("domainGuardTemplate", domainGuardTemplate, &domainGuardData{
				Source:           "resources",
				Package:          "app",
				ApplicationName:  "App",
				ReceiverName:     "a",
				ConcealedDomains: tt.concealed,
			})
			if err != nil {
				t.Fatalf("generateTemplateOutput() error = %v", err)
			}
			if _, err := format.Source(out); err != nil {
				t.Fatalf("the rendered guard does not parse: %v\n%s", err, out)
			}

			for _, want := range tt.wantContains {
				if !strings.Contains(string(out), want) {
					t.Errorf("domainGuardTemplate output missing %q:\n%s", want, out)
				}
			}
			for _, notWant := range tt.wantNotContains {
				if strings.Contains(string(out), notWant) {
					t.Errorf("domainGuardTemplate output must not contain %q:\n%s", notWant, out)
				}
			}
			if tt.concealed {
				s := string(out)
				roster, foothold := strings.Index(s, "a.TenantRoster().Has(domain)"), strings.Index(s, "HasGrants(ctx")
				if roster < 0 || foothold < 0 || roster > foothold {
					t.Errorf("the guard must ask the roster before the foothold (roster at %d, foothold at %d):\n%s", roster, foothold, out)
				}
			}
		})
	}
}

// Test_consolidatedTemplate_domainDispatch pins the consolidated handler's two-level
// dispatch: global resources dispatch on the first path segment, domain-scoped
// resources dispatch under the domain route segment's descent case with the domain
// bound from the operation path — and no batch-level domain state exists (cross-domain
// batches are legal; every operation is checked in its own partition).
func Test_consolidatedTemplate_domainDispatch(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadCollectionFixture(t))
	tenantStructs := fixtureStructs(loadFixture(t, "tenantfixture"))

	globalCase := consolidatedCaseData{
		resourceInfo:    fixtureResource(t, structs, "Fossil", func(res *resourceInfo) { res.IsConsolidated = true }),
		ResourcePackage: "resources",
		ReceiverName:    "a",
	}
	domainCase := consolidatedCaseData{
		resourceInfo: fixtureResource(t, structs, "Vault", func(res *resourceInfo) {
			res.IsConsolidated = true
			res.PermissionScope = accesstypes.DomainPermissionScope
		}),
		DomainPatternPrefix: "/stations/{stationID}",
		ResourcePackage:     "resources",
		ReceiverName:        "a",
	}

	tests := []struct {
		name            string
		data            consolidatedPatchData
		wantContains    []string
		wantNotContains []string
	}{
		{
			name: "global and domain cases dispatch on their own levels",
			data: consolidatedPatchData{
				Resources:           []*resourceInfo{globalCase.resourceInfo, domainCase.resourceInfo},
				GlobalCases:         []consolidatedCaseData{globalCase},
				DomainCases:         []consolidatedCaseData{domainCase},
				DomainRouteSegment:  "stations",
				DomainPatternPrefix: "/stations/{stationID}",
				Package:             "app",
				ResourcePackage:     "resources",
				ApplicationName:     "App",
				ReceiverName:        "a",
				HandlerName:         "PatchResources",
			},
			wantContains: []string{
				`userPermissions := a.UserPermissions(r)`,
				`case "stations":`,
				`op, err := op.WithPrefixPattern("/stations/{stationID}/{resource}")`,
				`domain := httpio.Param[accesstypes.Domain](op.Req, router.Domain)`,
				`if !a.TenantRoster().Has(domain) {`,
				`httpio.NewBadRequestMessagef("unknown domain %q in operation path", domain)`,
				`fossilDecoder.DecodeOperation(op, userPermissions, accesstypes.GlobalScope())`,
				`vaultDecoder.DecodeOperation(op, userPermissions, accesstypes.DomainScope(domain))`,
				`op.ReqWithPattern("/stations/{stationID}/{resource}/{id}"`,
				`unknown domain-scoped resource %q in operation path`,
			},
			wantNotContains: []string{"batchDomain", "UserPermissions(op.Req)", "HasGrants", "DomainExists", "DomainVisible", "tenantsAdded"},
		},
		{
			name: "the tenant record shares the descent case, branching on path depth, and feeds the roster after the commit",
			data: consolidatedPatchData{
				Resources: []*resourceInfo{globalCase.resourceInfo, domainCase.resourceInfo},
				SegmentCase: &consolidatedCaseData{
					resourceInfo:    fixtureResource(t, tenantStructs, "Sector", func(res *resourceInfo) { res.IsTenant = true }),
					ResourcePackage: "resources",
					ReceiverName:    "a",
				},
				HasTenant:           true,
				DomainCases:         []consolidatedCaseData{domainCase},
				DomainRouteSegment:  "sectors",
				DomainPatternPrefix: "/sectors/{sectorID}",
				Package:             "app",
				ResourcePackage:     "resources",
				ApplicationName:     "App",
				ReceiverName:        "a",
				HandlerName:         "PatchResources",
			},
			wantContains: []string{
				`case "sectors":`,
				`if op.PathDepth() <= 2 {`,
				`sectorDecoder.DecodeOperation(op, userPermissions, accesstypes.GlobalScope())`,
				`continue`,
				`op, err := op.WithPrefixPattern("/sectors/{sectorID}/{resource}")`,
				`vaultDecoder.DecodeOperation(op, userPermissions, accesstypes.DomainScope(domain))`,
				"var tenantsAdded, tenantsRemoved []accesstypes.Domain",
				"tenantsAdded, tenantsRemoved = nil, nil",
				"tenantsAdded = append(tenantsAdded, accesstypes.Domain(id))",
				"tenantsRemoved = append(tenantsRemoved, accesstypes.Domain(id))",
				"a.TenantRoster().Add(domain)",
				"a.TenantRoster().Remove(domain)",
				"if err := a.LiveService().Signal(ctx, resource.KindTenants); err != nil {",
				"logger.FromCtx(ctx).Errorf(",
			},
		},
		{
			name: "concealed domains ask the roster, then the caller's foothold",
			data: consolidatedPatchData{
				Resources:           []*resourceInfo{globalCase.resourceInfo, domainCase.resourceInfo},
				GlobalCases:         []consolidatedCaseData{globalCase},
				DomainCases:         []consolidatedCaseData{domainCase},
				DomainRouteSegment:  "stations",
				DomainPatternPrefix: "/stations/{stationID}",
				Package:             "app",
				ResourcePackage:     "resources",
				ApplicationName:     "App",
				ReceiverName:        "a",
				HandlerName:         "PatchResources",
				ConcealedDomains:    true,
			},
			wantContains: []string{
				`if !a.TenantRoster().Has(domain) {`,
				`if ok, err := userPermissions.HasGrants(ctx, accesstypes.DomainScope(domain)); err != nil {`,
				`httpio.NewBadRequestMessagef("unknown domain %q in operation path", domain)`,
			},
			wantNotContains: []string{"DomainExists", "DomainVisible"},
		},
		{
			name: "all-global consolidation has no descent case",
			data: consolidatedPatchData{
				Resources:           []*resourceInfo{globalCase.resourceInfo},
				GlobalCases:         []consolidatedCaseData{globalCase},
				DomainRouteSegment:  "stations",
				DomainPatternPrefix: "/stations/{stationID}",
				Package:             "app",
				ResourcePackage:     "resources",
				ApplicationName:     "App",
				ReceiverName:        "a",
				HandlerName:         "PatchResources",
			},
			wantNotContains: []string{`case "stations":`, "WithPrefixPattern", "TenantRoster()", "router.Domain"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c := &client{}
			out, err := c.generateTemplateOutput("consolidatedPatchTemplate", consolidatedPatchTemplate, tt.data)
			if err != nil {
				t.Fatalf("generateTemplateOutput() error = %v", err)
			}
			if _, err := format.Source(out); err != nil {
				t.Fatalf("the rendered dispatcher does not parse: %v\n%s", err, out)
			}

			for _, want := range tt.wantContains {
				if !strings.Contains(string(out), want) {
					t.Errorf("consolidatedPatchTemplate output missing %q:\n%s", want, out)
				}
			}
			for _, notWant := range tt.wantNotContains {
				if strings.Contains(string(out), notWant) {
					t.Errorf("consolidatedPatchTemplate output must not contain %q:\n%s", notWant, out)
				}
			}
		})
	}
}
