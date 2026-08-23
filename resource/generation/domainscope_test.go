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

			route := &generatedRoute{TestParams: tt.params}
			route.prependDomainTestParam(tt.domainScoped, tt.paramKey)
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
		paramName   string
		wantSegment string
		wantParam   string
		wantErr     bool
	}{
		{
			name:        "custom segment pair",
			segment:     "organization",
			paramName:   "organizationID",
			wantSegment: "organization",
			wantParam:   "organizationID",
		},
		{name: "empty segment errors", segment: "", paramName: "organizationID", wantErr: true},
		{name: "empty paramName errors", segment: "organization", paramName: "", wantErr: true},
		{name: "slash in segment errors", segment: "org/unit", paramName: "orgID", wantErr: true},
		{name: "braces in paramName errors", segment: "organization", paramName: "{orgID}", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := &resourceGenerator{client: &client{}}
			err := resolveOptions(r, []option{WithDomainRoute(tt.segment, tt.paramName)})
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
			if r.domainRouteParam != tt.wantParam {
				t.Errorf("domainRouteParam = %q, want %q", r.domainRouteParam, tt.wantParam)
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
			name: "consolidated resource named like the domain route segment is a generation error",
			resources: []*resourceInfo{
				fixtureResource(t, structs, "Station", func(res *resourceInfo) {
					res.IsConsolidated = true
				}),
			},
			wantErrContains: "domain route segment",
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

// Test_handlerContent_domainSource pins the domain argument generated handlers pass to
// the decoders: the accesstypes.GlobalDomain literal for global-scoped resources, the
// {domain} route parameter for domain-scoped ones.
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
			name:        "global scope passes GlobalDomain",
			scope:       "",
			handlerType: ListHandler,
			wantContains: []string{
				"decoder.Decode(r, a.UserPermissions(r), accesstypes.GlobalDomain)",
			},
			wantNotContains: []string{"router.Domain", "DomainExists"},
		},
		{
			name:        "domain scope reads the route parameter and guards unknown domains",
			scope:       accesstypes.DomainPermissionScope,
			handlerType: ListHandler,
			wantContains: []string{
				"domain := httpio.Param[accesstypes.Domain](r, router.Domain)",
				"if ok, err := a.DomainExists(ctx, domain); err != nil {",
				`httpio.NewNotFoundMessagef("unknown domain %q", domain)`,
				"decoder.Decode(r, a.UserPermissions(r), domain)",
			},
			wantNotContains: []string{"accesstypes.GlobalDomain"},
		},
		{
			name:        "domain-scoped read handler carries the guard",
			scope:       accesstypes.DomainPermissionScope,
			handlerType: ReadHandler,
			wantContains: []string{
				"domain := httpio.Param[accesstypes.Domain](r, router.Domain)",
				"if ok, err := a.DomainExists(ctx, domain); err != nil {",
			},
			wantNotContains: []string{"accesstypes.GlobalDomain"},
		},
		{
			name:        "domain-scoped patch handler guards before the transaction",
			scope:       accesstypes.DomainPermissionScope,
			handlerType: PatchHandler,
			wantContains: []string{
				"domain := httpio.Param[accesstypes.Domain](r, router.Domain)",
				"if ok, err := a.DomainExists(ctx, domain); err != nil {",
			},
			wantNotContains: []string{"accesstypes.GlobalDomain"},
		},
		{
			name:            "global-scoped patch handler has no guard",
			scope:           "",
			handlerType:     PatchHandler,
			wantContains:    []string{"accesstypes.GlobalDomain"},
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

// Test_domainExistsGuard_spliced pins that every handler template with a domain-scoped
// branch embeds the shared unknown-domain guard, so no site can silently lose it.
func Test_domainExistsGuard_spliced(t *testing.T) {
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

			if !strings.Contains(tt.template, domainExistsGuard) {
				t.Errorf("template does not splice domainExistsGuard")
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
				`fossilDecoder.DecodeOperation(op, userPermissions, accesstypes.GlobalDomain)`,
				`vaultDecoder.DecodeOperation(op, userPermissions, domain)`,
				`op.ReqWithPattern("/stations/{stationID}/{resource}/{id}"`,
				`unknown domain-scoped resource %q in operation path`,
			},
			wantNotContains: []string{"batchDomain", "UserPermissions(op.Req)"},
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
