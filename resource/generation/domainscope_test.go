package generation

import (
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
			name:          "WithDomainRoute customizes the segment pair",
			resourceName:  "Widget",
			domainScoped:  true,
			domainSegment: "organization",
			domainParam:   "organizationID",
			wantPath:      "/api/organization/{organizationID}/widgets",
			wantTestURL:   "/api/organization/testDomain/widgets",
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
			gotPath, gotTestURL := r.routeBasePaths(tt.resourceName, tt.domainScoped)
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

func Test_WithDomainRoute(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		segment     string
		wantSegment string
		wantErr     bool
	}{
		{
			name:        "custom segment",
			segment:     "organizations",
			wantSegment: "organizations",
		},
		{name: "empty segment errors", segment: "", wantErr: true},
		{name: "slash in segment errors", segment: "org/unit", wantErr: true},
		{name: "braces in segment errors", segment: "{organizations}", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := &resourceGenerator{client: &client{}}
			err := resolveOptions(r, []option{WithDomainRoute(tt.segment)})
			if tt.wantErr {
				if err == nil {
					t.Fatal("WithDomainRoute() expected an error, got nil")
				}

				return
			}
			if err != nil {
				t.Fatalf("WithDomainRoute() error = %v", err)
			}
			if r.domainRouteSegment != tt.wantSegment {
				t.Errorf("domainRouteSegment = %q, want %q", r.domainRouteSegment, tt.wantSegment)
			}
			if r.domainRouteParam != defaultDomainRouteParam {
				t.Errorf("domainRouteParam = %q, want the pre-derivation default %q: the option never configures it", r.domainRouteParam, defaultDomainRouteParam)
			}
		})
	}
}

// Test_deriveDomainRouteParam pins the domain route parameter's derivation: a global,
// single-key resource whose route name equals the domain route segment (the
// tenant-record pattern) forces the parameter to its read-route parameter — chi
// permits one wildcard name per tree position — and every other configuration keeps
// the default "domain".
func Test_deriveDomainRouteParam(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadCollectionFixture(t))

	tests := []struct {
		name      string
		resources []*resourceInfo
		want      string
	}{
		{
			name:      "tenant-record resource derives its read-route parameter",
			resources: []*resourceInfo{fixtureResource(t, structs, "Station", nil)},
			want:      "stationID",
		},
		{
			name:      "no resource matches the segment: default",
			resources: []*resourceInfo{fixtureResource(t, structs, "Vault", nil)},
			want:      "domain",
		},
		{
			name: "a domain-scoped match lives under the segment pair: default",
			resources: []*resourceInfo{
				fixtureResource(t, structs, "Station", func(res *resourceInfo) {
					res.PermissionScope = accesstypes.DomainPermissionScope
				}),
			},
			want: "domain",
		},
		{
			name: "a compound-key match is rejected elsewhere: default",
			resources: []*resourceInfo{
				fixtureResource(t, structs, "Station", func(res *resourceInfo) {
					res.PkCount = 2
					res.Fields[1].IsPrimaryKey = true
				}),
			},
			want: "domain",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := &resourceGenerator{
				client:             &client{},
				domainRouteSegment: "stations",
				domainRouteParam:   "domain",
			}
			r.resources = tt.resources

			r.deriveDomainRouteParam()
			if r.domainRouteParam != tt.want {
				t.Errorf("domainRouteParam = %q, want %q", r.domainRouteParam, tt.want)
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
		name            string
		resources       []*resourceInfo
		wantNames       []string
		wantErrContains string
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
			name: "segment-named consolidated resource passes alongside domain-scoped ones (tenant-record pattern)",
			resources: []*resourceInfo{
				fixtureResource(t, structs, "Station", func(res *resourceInfo) {
					res.IsConsolidated = true
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
			got, err := r.consolidatedPatchResources()
			if tt.wantErrContains != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErrContains) {
					t.Fatalf("consolidatedPatchResources() error = %v, want error containing %q", err, tt.wantErrContains)
				}

				return
			}
			if err != nil {
				t.Fatalf("consolidatedPatchResources() error = %v", err)
			}

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
// domain route parameter, 404 unknown domains via the application's DomainExists, and
// only then run the wrapped handler.
func Test_domainGuardTemplate(t *testing.T) {
	t.Parallel()

	c := &client{}
	out, err := c.generateTemplateOutput("domainGuardTemplate", domainGuardTemplate, &domainGuardData{
		Source:          "resources",
		Package:         "app",
		ApplicationName: "App",
		ReceiverName:    "a",
	})
	if err != nil {
		t.Fatalf("generateTemplateOutput() error = %v", err)
	}

	for _, want := range []string{
		"func (a *App) DomainGuard() func(http.HandlerFunc) http.HandlerFunc {",
		"domain := httpio.Param[accesstypes.Domain](r, router.Domain)",
		"if ok, err := a.DomainExists(ctx, domain); err != nil {",
		`httpio.NewNotFoundMessagef("unknown domain %q", domain)`,
		"next.ServeHTTP(w, r)",
	} {
		if !strings.Contains(string(out), want) {
			t.Errorf("domainGuardTemplate output missing %q:\n%s", want, out)
		}
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
			},
			wantContains: []string{
				`userPermissions := a.UserPermissions(r)`,
				`case "stations":`,
				`op, err := op.WithPrefixPattern("/stations/{stationID}/{resource}")`,
				`domain := httpio.Param[accesstypes.Domain](op.Req, router.Domain)`,
				`if ok, err := a.DomainExists(ctx, domain); err != nil {`,
				`httpio.NewBadRequestMessagef("unknown domain %q in operation path", domain)`,
				`fossilDecoder.DecodeOperation(op, userPermissions, accesstypes.GlobalScope())`,
				`vaultDecoder.DecodeOperation(op, userPermissions, accesstypes.DomainScope(domain))`,
				`op.ReqWithPattern("/stations/{stationID}/{resource}/{id}"`,
				`unknown domain-scoped resource %q in operation path`,
			},
			wantNotContains: []string{"batchDomain", "UserPermissions(op.Req)"},
		},
		{
			name: "segment-named resource shares the descent case, branching on path depth",
			data: consolidatedPatchData{
				Resources: []*resourceInfo{globalCase.resourceInfo, domainCase.resourceInfo},
				SegmentCase: &consolidatedCaseData{
					resourceInfo:    fixtureResource(t, structs, "Station", nil),
					ResourcePackage: "resources",
					ReceiverName:    "a",
				},
				DomainCases:         []consolidatedCaseData{domainCase},
				DomainRouteSegment:  "stations",
				DomainPatternPrefix: "/stations/{stationID}",
				Package:             "app",
				ResourcePackage:     "resources",
				ApplicationName:     "App",
				ReceiverName:        "a",
			},
			wantContains: []string{
				`case "stations":`,
				`if op.PathDepth() <= 2 {`,
				`stationDecoder.DecodeOperation(op, userPermissions, accesstypes.GlobalScope())`,
				`continue`,
				`op, err := op.WithPrefixPattern("/stations/{stationID}/{resource}")`,
				`vaultDecoder.DecodeOperation(op, userPermissions, accesstypes.DomainScope(domain))`,
			},
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
			},
			wantNotContains: []string{`case "stations":`, "WithPrefixPattern", "DomainExists", "router.Domain"},
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

// Test_validateDomainSegmentResources pins the tenant-record pattern's structural
// requirement: a resource named like the domain route segment must have a single
// primary key; the validation only applies when domain-scoped routes exist at all.
// (Read-route parameter alignment is unrepresentable: deriveDomainRouteParam derives
// the domain route parameter from the matching resource.)
func Test_validateDomainSegmentResources(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadCollectionFixture(t))

	domainScopedVault := func() *resourceInfo {
		return fixtureResource(t, structs, "Vault", func(res *resourceInfo) {
			res.PermissionScope = accesstypes.DomainPermissionScope
		})
	}

	tests := []struct {
		name            string
		domainParam     string
		resources       []*resourceInfo
		wantErrContains string
	}{
		{
			name:        "tenant-record resource with single key and matching param passes",
			domainParam: "stationID",
			resources: []*resourceInfo{
				fixtureResource(t, structs, "Station", nil),
				domainScopedVault(),
			},
		},
		{
			name:        "compound primary key is a generation error",
			domainParam: "stationID",
			resources: []*resourceInfo{
				fixtureResource(t, structs, "Station", func(res *resourceInfo) {
					res.PkCount = 2
					res.Fields[1].IsPrimaryKey = true
				}),
				domainScopedVault(),
			},
			wantErrContains: "single primary key",
		},
		{
			name:        "a domain-scoped resource named like the segment shares no position with it",
			domainParam: "stationID",
			resources: []*resourceInfo{
				fixtureResource(t, structs, "Station", func(res *resourceInfo) {
					res.PermissionScope = accesstypes.DomainPermissionScope
				}),
				domainScopedVault(),
			},
		},
		{
			name:        "without domain-scoped routes the validation does not apply",
			domainParam: "orgID",
			resources: []*resourceInfo{
				fixtureResource(t, structs, "Station", nil),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := &resourceGenerator{
				client:             &client{},
				domainRouteSegment: "stations",
				domainRouteParam:   tt.domainParam,
			}
			r.resources = tt.resources

			err := r.validateDomainSegmentResources()
			if tt.wantErrContains == "" {
				if err != nil {
					t.Fatalf("validateDomainSegmentResources() error = %v, want nil", err)
				}

				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErrContains) {
				t.Fatalf("validateDomainSegmentResources() error = %v, want error containing %q", err, tt.wantErrContains)
			}
		})
	}
}
