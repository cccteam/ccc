package generation

import (
	"strings"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/generation/parser"
	"github.com/google/go-cmp/cmp"
)

// featureFixtureClient loads the feature fixture and registers its flags: the client
// every @feature resolution test resolves against.
func featureFixtureClient(t *testing.T) (c *client, structs map[string]*parser.Struct) {
	t.Helper()

	pkg := loadFixture(t, "featurefixture")
	c = &client{}
	if err := c.registerFeatures(pkg.Constants); err != nil {
		t.Fatalf("registerFeatures() error = %v", err)
	}

	return c, fixtureStructs(pkg)
}

// Test_registerFeatures pins the declarations: every resource.Feature constant is one,
// by constant, with its value as the name and its doc comment as the description, in
// constant order; a constant of another type declares nothing; a name declared twice is
// refused naming both constants.
func Test_registerFeatures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		fixture string
		want    []resource.FeatureDeclaration
		wantErr string
	}{
		{
			name:    "the flags, in constant order, with their descriptions",
			fixture: "featurefixture",
			want: []resource.FeatureDeclaration{
				{Name: "cargo_manifest", Description: "CargoManifest shows a ship's cargo bays.", Constant: "CargoManifest"},
				{Name: "debriefs", Description: "Debriefs lets a crew write and read mission debriefs.", Constant: "Debriefs"},
			},
		},
		{
			name:    "a name declared twice is refused naming both constants",
			fixture: "featureduplicate",
			wantErr: "Debriefs",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c := &client{}
			err := c.registerFeatures(loadFixture(t, tt.fixture).Constants)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) || !strings.Contains(err.Error(), "Reports") {
					t.Fatalf("registerFeatures() error = %v, want one naming Debriefs and Reports", err)
				}

				return
			}
			if err != nil {
				t.Fatalf("registerFeatures() error = %v", err)
			}
			if diff := cmp.Diff(tt.want, c.featureDeclarations); diff != "" {
				t.Errorf("featureDeclarations mismatch (-want +got):\n%s", diff)
			}
			if _, ok := c.features["NotAFlag"]; ok {
				t.Error("a constant of another type was registered as a flag")
			}
		})
	}
}

// Test_resolveResourceFeatures pins @feature on a table resource: the struct's gate, a
// field's gate, and the refusals (a key, a column a create must supply, an unknown
// constant, a value in place of the constant).
func Test_resolveResourceFeatures(t *testing.T) {
	t.Parallel()

	c, structs := featureFixtureClient(t)

	tests := []struct {
		name        string
		structName  string
		mutate      func(*resourceInfo)
		wantGate    string
		wantFields  map[string]string
		wantErrPart string
	}{
		{name: "a struct gate", structName: "Debrief", wantGate: "Debriefs"},
		{name: "a field gate on a nullable column", structName: "Ship", mutate: nullableCargoBays, wantFields: map[string]string{"CargoBays": "CargoManifest"}},
		{name: "a gated key is refused", structName: "GatedKey", wantErrPart: "cannot hide the primary key"},
		{name: "a gated NOT NULL column is refused on a creatable resource", structName: "GatedRequired", wantErrPart: "a column a create must supply"},
		{
			name:       "a gated NOT NULL column is accepted on a view",
			structName: "GatedRequired",
			mutate:     func(res *resourceInfo) { res.IsVirtual = true },
			wantFields: map[string]string{"Name": "Debriefs"},
		},
		{
			name:       "a gated NOT NULL column is accepted where the create is suppressed",
			structName: "GatedRequired",
			mutate:     func(res *resourceInfo) { res.SuppressedHandlers = []HandlerType{PatchHandler} },
			wantFields: map[string]string{"Name": "Debriefs"},
		},
		{name: "an unknown constant is refused naming the declared ones", structName: "UnknownFlag", wantErrPart: "names no resource.Feature constant in the resources package; declared: [CargoManifest Debriefs]"},
		{name: "a value in place of the constant is refused", structName: "QuotedFlag", wantErrPart: "names a flag by its constant's identifier"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			pStruct, annotations := scanFixtureStruct(t, structs, tt.structName)
			res := fixtureResource(t, structs, tt.structName, tt.mutate)
			err := c.resolveResourceFeatures(res, pStruct, annotations)
			if tt.wantErrPart != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErrPart) {
					t.Fatalf("resolveResourceFeatures() error = %v, want one containing %q", err, tt.wantErrPart)
				}

				return
			}
			if err != nil {
				t.Fatalf("resolveResourceFeatures() error = %v", err)
			}
			if got := gateConstant(res.Feature); got != tt.wantGate {
				t.Errorf("struct gate = %q, want %q", got, tt.wantGate)
			}
			got := map[string]string{}
			for _, f := range res.Fields {
				if f.Feature != nil {
					got[f.Name()] = f.Feature.Constant
				}
			}
			if len(got) == 0 {
				got = nil
			}
			if diff := cmp.Diff(tt.wantFields, got); diff != "" {
				t.Errorf("field gates mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// nullableCargoBays marks Ship's gated column nullable, as the schema would.
func nullableCargoBays(res *resourceInfo) {
	for _, f := range res.Fields {
		if f.Name() == "CargoBays" {
			f.IsNullable = true
		}
	}
}

func gateConstant(gate *featureGate) string {
	if gate == nil {
		return ""
	}

	return gate.Constant
}

// Test_resolveComputedFeatures pins @feature on a computed resource: a field gate
// resolves, a gated key is refused.
func Test_resolveComputedFeatures(t *testing.T) {
	t.Parallel()

	c, structs := featureFixtureClient(t)

	tests := []struct {
		name        string
		structName  string
		wantFields  map[string]string
		wantErrPart string
	}{
		{name: "a field gate", structName: "Manifest", wantFields: map[string]string{"Bays": "CargoManifest"}},
		{name: "a gated key is refused", structName: "GatedManifestKey", wantErrPart: "cannot hide the primary key"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			res := fixtureComputedView(t, structs, tt.structName)
			_, annotations := scanFixtureStruct(t, structs, tt.structName)
			err := c.resolveComputedFeatures(res, annotations)
			if tt.wantErrPart != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErrPart) {
					t.Fatalf("resolveComputedFeatures() error = %v, want one containing %q", err, tt.wantErrPart)
				}

				return
			}
			if err != nil {
				t.Fatalf("resolveComputedFeatures() error = %v", err)
			}
			got := map[string]string{}
			for _, f := range res.Fields {
				if f.Feature != nil {
					got[f.Name()] = f.Feature.Constant
				}
			}
			if diff := cmp.Diff(tt.wantFields, got); diff != "" {
				t.Errorf("field gates mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// Test_resolveRPCFeature pins @feature on a method: the struct's gate resolves, a
// field's is refused.
func Test_resolveRPCFeature(t *testing.T) {
	t.Parallel()

	c, structs := featureFixtureClient(t)

	tests := []struct {
		name        string
		structName  string
		wantGate    string
		wantErrPart string
	}{
		{name: "a struct gate", structName: "GatedMethod", wantGate: "Debriefs"},
		{name: "a field gate is refused", structName: "FieldGatedMethod", wantErrPart: "gates a method whole, not field by field"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, annotations := scanFixtureStruct(t, structs, tt.structName)
			method := &rpcMethodInfo{Struct: structs[tt.structName]}
			err := c.resolveRPCFeature(method, annotations)
			if tt.wantErrPart != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErrPart) {
					t.Fatalf("resolveRPCFeature() error = %v, want one containing %q", err, tt.wantErrPart)
				}

				return
			}
			if err != nil {
				t.Fatalf("resolveRPCFeature() error = %v", err)
			}
			if got := gateConstant(method.Feature); got != tt.wantGate {
				t.Errorf("method gate = %q, want %q", got, tt.wantGate)
			}
		})
	}
}

// Test_rejectReservedFeatureNames pins that the library's resource and method names are
// refused on a struct and on a manual registration.
func Test_rejectReservedFeatureNames(t *testing.T) {
	t.Parallel()

	c, structs := featureFixtureClient(t)

	tests := []struct {
		name    string
		err     func() error
		wantErr bool
	}{
		{name: "a struct whose resource would be FeatureFlags", err: func() error { return c.rejectReservedResourceName(structs["FeatureFlag"], "table-backed resource") }, wantErr: true},
		{name: "an ordinary struct", err: func() error { return c.rejectReservedResourceName(structs["Ship"], "table-backed resource") }},
		{name: "an RPC struct named SetFeature", err: func() error { return rejectReservedMethodName(structs["SetFeature"]) }, wantErr: true},
		{name: "an ordinary RPC struct", err: func() error { return rejectReservedMethodName(structs["GatedMethod"]) }},
		{
			name: "a manual registration of FeatureFlags",
			err: func() error {
				return rejectReservedManualRegistrations([]ManualRegistration{{Resource: resource.FeatureFlagsResource, Permission: accesstypes.List}})
			},
			wantErr: true,
		},
		{
			name: "a manual registration of SetFeature",
			err: func() error {
				return rejectReservedManualRegistrations([]ManualRegistration{{Resource: resource.SetFeatureMethod, Permission: accesstypes.Execute}})
			},
			wantErr: true,
		},
		{
			name: "an ordinary manual registration",
			err: func() error {
				return rejectReservedManualRegistrations([]ManualRegistration{{Resource: "Ledgers", Permission: accesstypes.List}})
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if err := tt.err(); (err != nil) != tt.wantErr {
				t.Errorf("error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// featureFixtureGenerator builds the generator the gate tests share: a gated resource, a
// resource with a gated field, a computed resource with a gated field, a gated method,
// routes on the default outlet and a session-less extra outlet.
func featureFixtureGenerator(t *testing.T) *resourceGenerator {
	t.Helper()

	c, structs := featureFixtureClient(t)
	c.genRPCMethods = true
	resolve := func(name string, mutate func(*resourceInfo)) *resourceInfo {
		t.Helper()
		pStruct, annotations := scanFixtureStruct(t, structs, name)
		res := fixtureResource(t, structs, name, mutate)
		if err := c.resolveResourceFeatures(res, pStruct, annotations); err != nil {
			t.Fatalf("resolveResourceFeatures(%s) error = %v", name, err)
		}

		return res
	}
	c.resources = []*resourceInfo{resolve("Debrief", nil), resolve("Ship", nullableCargoBays)}
	manifest := fixtureComputedView(t, structs, "Manifest")
	_, manifestAnnotations := scanFixtureStruct(t, structs, "Manifest")
	if err := c.resolveComputedFeatures(manifest, manifestAnnotations); err != nil {
		t.Fatalf("resolveComputedFeatures() error = %v", err)
	}
	c.computedResources = []*computedResource{manifest}
	method := &rpcMethodInfo{Struct: structs["GatedMethod"], Form: rpcFormTxn}
	_, methodAnnotations := scanFixtureStruct(t, structs, "GatedMethod")
	if err := c.resolveRPCFeature(method, methodAnnotations); err != nil {
		t.Fatalf("resolveRPCFeature() error = %v", err)
	}
	c.rpcMethods = []*rpcMethodInfo{method}

	return &resourceGenerator{
		client:             c,
		genRoutes:          true,
		routePrefix:        "api",
		domainRouteSegment: "sectors",
		domainRouteParam:   "sectorID",
		extraOutlets:       []routerOutlet{{name: "droids", prefix: "droids"}},
	}
}

// Test_featureGates pins the generated FeatureGates: every gated resource by its plural,
// every gated field by "Plural.wireName", every gated method by name, sorted by key.
func Test_featureGates(t *testing.T) {
	t.Parallel()

	r := featureFixtureGenerator(t)
	want := []featureGateEntry{
		{Key: "Debriefs", Constant: "Debriefs"},
		{Key: "GatedMethod", Constant: "Debriefs"},
		{Key: "Manifests.bays", Constant: "CargoManifest"},
		{Key: "Ships.cargoBays", Constant: "CargoManifest"},
	}
	if diff := cmp.Diff(want, r.featureGates()); diff != "" {
		t.Errorf("featureGates() mismatch (-want +got):\n%s", diff)
	}
	if !r.hasGates() {
		t.Error("hasGates() = false with gates declared")
	}
	if (&client{}).hasGates() {
		t.Error("hasGates() = true with nothing declared")
	}
}

// Test_featureDeclarationsTemplate pins the resources package's generated file: the
// declarations and the gates as Go literals, nil for an application that declares
// nothing.
func Test_featureDeclarationsTemplate(t *testing.T) {
	t.Parallel()

	r := featureFixtureGenerator(t)

	tests := []struct {
		name            string
		data            *featureDeclarationsData
		wantContains    []string
		wantNotContains []string
	}{
		{
			name: "declarations and gates render as literals",
			data: &featureDeclarationsData{Package: "resources", Declarations: r.featureDeclarations, Gates: r.featureGates()},
			wantContains: []string{
				`{Name: CargoManifest, Description: "CargoManifest shows a ship's cargo bays.", Constant: "CargoManifest"},`,
				`{Name: Debriefs, Description: "Debriefs lets a crew write and read mission debriefs.", Constant: "Debriefs"},`,
				`"Ships.cargoBays": CargoManifest,`,
				`"GatedMethod": Debriefs,`,
			},
			wantNotContains: []string{"return nil"},
		},
		{
			name:         "nothing declared renders nil twice",
			data:         &featureDeclarationsData{Package: "resources"},
			wantContains: []string{"func Features() []resource.FeatureDeclaration {\n\treturn nil\n}", "func FeatureGates() resource.FeatureGates {\n\treturn nil\n}"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			out, err := (&client{}).generateTemplateOutput("featureDeclarationsTemplate", featureDeclarationsTemplate, tt.data)
			if err != nil {
				t.Fatalf("generateTemplateOutput() error = %v", err)
			}
			assertContains(t, "featureDeclarationsTemplate", string(out), tt.wantContains, tt.wantNotContains)
		})
	}
}

// Test_featuresTemplate pins the handler package's generated file: the features route
// on every application, the FeatureFlags routes and SetFeature where routes are
// generated, the FeatureGuard where anything is gated.
func Test_featuresTemplate(t *testing.T) {
	t.Parallel()

	base := featuresData{Package: "app", ApplicationName: "App", ReceiverName: "a", RouterPackage: "router", RoutePrefix: "api"}
	withRoutes, withGates := base, base
	withRoutes.HasRoutes = true
	withGates.HasRoutes, withGates.HasGates = true, true

	tests := []struct {
		name            string
		data            featuresData
		wantContains    []string
		wantNotContains []string
	}{
		{
			name:            "no routes, nothing gated: the features route alone",
			data:            base,
			wantContains:    []string{"func (a *App) Features() http.HandlerFunc {\n\treturn resource.FeaturesHandler(a.FeatureSet())\n}"},
			wantNotContains: []string{"FeatureFlags()", "SetFeature()", "FeatureGuard()"},
		},
		{
			name: "routes: the flags' handlers over the generated collection and the live service",
			data: withRoutes,
			wantContains: []string{
				"return resource.FeatureFlagsHandler(a, router.Collection())",
				"return resource.FeatureFlagHandler(a, router.Collection())",
				"return resource.SetFeatureHandler(a, router.Collection(), a.LiveService())",
				"GET /api/feature-flags/{featureFlagName}",
			},
			wantNotContains: []string{"FeatureGuard()"},
		},
		{
			name:         "gates: the guard over the application's feature set",
			data:         withGates,
			wantContains: []string{"func (a *App) FeatureGuard() func(resource.Feature) func(http.HandlerFunc) http.HandlerFunc {", "return resource.FeatureGuard(a.FeatureSet(), feature)"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			out, err := (&client{}).generateTemplateOutput("featuresTemplate", featuresTemplate, &tt.data)
			if err != nil {
				t.Fatalf("generateTemplateOutput() error = %v", err)
			}
			assertContains(t, "featuresTemplate", string(out), tt.wantContains, tt.wantNotContains)
		})
	}
}

// Test_routesTemplate_featureGuard pins the route registration: a gated route is wrapped
// in the feature guard with its constant, outside the domain guard where both apply; an
// ungated route registers as before; without gates the guard does not exist.
func Test_routesTemplate_featureGuard(t *testing.T) {
	t.Parallel()

	debriefs := &featureGate{Constant: "Debriefs", Name: "debriefs"}
	gated := map[string][]*generatedRoute{
		"Debrief": {
			{Method: "GET", Path: "/api/debriefs", HandlerFunc: "Debriefs", HandlerType: ListHandler, Feature: debriefs},
			{Method: "PATCH", Path: "/api/debriefs", HandlerFunc: "PatchDebriefs", HandlerType: PatchHandler, Feature: debriefs},
		},
		"Vault": {
			{Method: "GET", Path: "/api/stations/{stationID}/vaults", HandlerFunc: "Vaults", HandlerType: ListHandler, DomainScoped: true, Feature: debriefs},
		},
		"Ship": {
			{Method: "GET", Path: "/api/ships", HandlerFunc: "Ships", HandlerType: ListHandler},
		},
	}

	tests := []struct {
		name            string
		data            routerFileData
		wantContains    []string
		wantNotContains []string
	}{
		{
			name: "gated routes wrap in the feature guard, a domain-scoped one outside the domain guard",
			data: routerFileData{
				ServesSessions:        true,
				Package:               "router",
				RoutesMap:             gated,
				HasDomainScoped:       true,
				HasDomainScopedRoutes: true,
				HasGatedRoutes:        true,
				StubFeatureGuard:      true,
				DomainRouteParam:      "stationID",
				ResourcePackage:       "resources",
			},
			wantContains: []string{
				"FeatureGuard() func(resource.Feature) func(http.HandlerFunc) http.HandlerFunc",
				"featureGuard := h.FeatureGuard()",
				"debriefsHandler := featureGuard(resources.Debriefs)(h.Debriefs())",
				`bounded.Patch("/api/debriefs", featureGuard(resources.Debriefs)(h.PatchDebriefs()))`,
				"vaultsHandler := featureGuard(resources.Debriefs)(domainGuard(h.Vaults()))",
				"shipsHandler := h.Ships()",
			},
			wantNotContains: []string{"featureGuard(resources.Debriefs)(h.Ships())"},
		},
		{
			name: "without gated routes the guard does not exist",
			data: routerFileData{
				ServesSessions:  true,
				Package:         "router",
				RoutesMap:       map[string][]*generatedRoute{"Ship": gated["Ship"]},
				ResourcePackage: "resources",
			},
			wantNotContains: []string{"FeatureGuard", "featureGuard"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			out, err := (&client{}).generateTemplateOutput("routesTemplate", routesTemplate, &tt.data)
			if err != nil {
				t.Fatalf("generateTemplateOutput() error = %v", err)
			}
			assertContains(t, "routesTemplate", string(out), tt.wantContains, tt.wantNotContains)
		})
	}
}

// Test_routerTestTemplate_featureGuard pins the router-test stub: the feature flag read
// route's parameter is recorded, and the FeatureGuard passes through where any outlet
// has a gated route.
func Test_routerTestTemplate_featureGuard(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		data            routerFileData
		wantContains    []string
		wantNotContains []string
	}{
		{
			name:         "a gated route anywhere stubs the guard",
			data:         routerFileData{Package: "router", StubFeatureGuard: true},
			wantContains: []string{`"featureFlagName",`, "func (s *generatedHandlersStub) FeatureGuard() func(resource.Feature) func(http.HandlerFunc) http.HandlerFunc {"},
		},
		{
			name:            "no gated route, no stub",
			data:            routerFileData{Package: "router"},
			wantContains:    []string{`"featureFlagName",`},
			wantNotContains: []string{"FeatureGuard"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			out, err := (&client{}).generateTemplateOutput("routerTestTemplate", routerTestTemplate, &tt.data)
			if err != nil {
				t.Fatalf("generateTemplateOutput() error = %v", err)
			}
			assertContains(t, "routerTestTemplate", string(out), tt.wantContains, tt.wantNotContains)
		})
	}
}

// Test_accumulateFeatureRoutes pins the routes every outlet gains: the features route
// on each, the FeatureFlags list and read and SetFeature on the session-serving ones,
// all of them in the dispatch test.
func Test_accumulateFeatureRoutes(t *testing.T) {
	t.Parallel()

	outlets := []routerOutlet{{name: "default", prefix: "api", servesSessions: true}, {name: "droids", prefix: "droids"}}
	outletRoutes := []*outletRouteData{{RoutesMap: map[string][]*generatedRoute{}}, {RoutesMap: map[string][]*generatedRoute{}}}

	routes := accumulateFeatureRoutes(outlets, outletRoutes)

	var urls []string
	for _, route := range routes {
		for _, method := range route.TestMethods() {
			urls = append(urls, method+" "+route.TestURL+" -> "+route.HandlerFunc)
		}
	}
	want := []string{
		"http.MethodGet /api/features -> Features",
		"http.MethodGet /api/feature-flags -> FeatureFlags",
		"http.MethodPost /api/feature-flags -> FeatureFlags",
		"http.MethodGet /api/feature-flags/testFeatureFlagName -> FeatureFlag",
		"http.MethodPost /api/feature-flags/testFeatureFlagName -> FeatureFlag",
		"http.MethodPost /api/set-feature -> SetFeature",
		"http.MethodGet /droids/features -> Features",
	}
	if diff := cmp.Diff(want, urls); diff != "" {
		t.Errorf("dispatch tests mismatch (-want +got):\n%s", diff)
	}
	if got := len(outletRoutes[0].RoutesMap); got != 3 {
		t.Errorf("the session outlet's route map has %d feature groups, want 3", got)
	}
	if got := len(outletRoutes[1].RoutesMap); got != 1 {
		t.Errorf("the session-less outlet's route map has %d feature groups, want 1", got)
	}
	read := outletRoutes[0].RoutesMap[featureFlagRoutesKey][1]
	if read.Path != "/api/feature-flags/{featureFlagName}" {
		t.Errorf("read route path = %q", read.Path)
	}
}

// Test_featureAuthzCases pins the matrix's feature cases: the features route open on
// every outlet; FeatureFlags list and read as query pairs and SetFeature denied-only,
// with its dry run, on the session-serving outlets alone.
func Test_featureAuthzCases(t *testing.T) {
	t.Parallel()

	r := featureFixtureGenerator(t)
	want := []authzCase{
		{Name: "Features", Method: "http.MethodGet", URL: "/api/features", Open: true},
		{Name: "FeatureFlags", Method: "http.MethodGet", URL: "/api/feature-flags", Permission: "List"},
		{Name: "FeatureFlag", Method: "http.MethodGet", URL: "/api/feature-flags/authz-test-key", Permission: "Read"},
		{Name: "SetFeature", Method: "http.MethodPost", URL: "/api/set-feature", Body: "{}", DeniedOnly: true},
		{Name: "SetFeature dry run", Method: "http.MethodPost", URL: "/api/set-feature", Body: "{}", Headers: []authzHeader{{Name: "X-Dry-Run", Value: "true"}}, DeniedOnly: true},
		{Name: "Features (droids)", Method: "http.MethodGet", URL: "/droids/features", Open: true},
	}
	if diff := cmp.Diff(want, r.featureAuthzCases(), cmp.AllowUnexported(authzCase{})); diff != "" {
		t.Errorf("featureAuthzCases() mismatch (-want +got):\n%s", diff)
	}
}

// Test_authzTestTemplate_open pins an open case: one entry, no grant, 200 alone.
func Test_authzTestTemplate_open(t *testing.T) {
	t.Parallel()

	out, err := (&client{}).generateTemplateOutput("authzTestTemplate", authzTestTemplate, &authzTestData{
		Package: "authz",
		Cases:   []authzCase{{Name: "Features", Method: "http.MethodGet", URL: "/api/features", Open: true}},
	})
	if err != nil {
		t.Fatalf("generateTemplateOutput() error = %v", err)
	}
	assertContains(t, "authzTestTemplate", string(out),
		[]string{"name:         \"Features open\",", "wantStatuses: []int{http.StatusOK},"},
		[]string{"Features denied", "Features granted"})
}

// Test_featureTests pins the generated gate tests' cases: every gated route on the
// outlets serving it, a query route with its permission and a mutation route without,
// and every gated field's list asking for the column.
func Test_featureTests(t *testing.T) {
	t.Parallel()

	r := featureFixtureGenerator(t)
	data, err := r.featureTests()
	if err != nil {
		t.Fatalf("featureTests() error = %v", err)
	}
	wantRoutes := []featureGateTest{
		{Name: "Debriefs", Method: "http.MethodGet", URL: "/api/debriefs?sort=id", Constant: "Debriefs", Permission: "List"},
		{Name: "Debrief", Method: "http.MethodGet", URL: "/api/debriefs/00000000-0000-0000-0000-000000000001", Constant: "Debriefs", Permission: "Read"},
		{Name: "PatchDebriefs", Method: "http.MethodPatch", URL: "/api/debriefs", Body: `[{"op":"remove","path":"/authz-test-key"}]`, Constant: "Debriefs"},
		{Name: "GatedMethod", Method: "http.MethodPost", URL: "/api/gated-method", Body: "{}", Constant: "Debriefs"},
	}
	if diff := cmp.Diff(wantRoutes, data.Routes); diff != "" {
		t.Errorf("Routes mismatch (-want +got):\n%s", diff)
	}
	wantFields := []featureFieldTest{
		{Name: "Ship.CargoBays", URL: "/api/ships?columns=cargoBays&sort=id", Constant: "CargoManifest"},
		{Name: "Manifest.Bays", URL: "/api/manifests?columns=bays&sort=id", Constant: "CargoManifest"},
	}
	if diff := cmp.Diff(wantFields, data.Fields); diff != "" {
		t.Errorf("Fields mismatch (-want +got):\n%s", diff)
	}

	// The generator under test names no output packages; the file's are set as the
	// run would set them.
	data.Package, data.ResourcePackage = "authz", "resources"
	out, err := r.generateTemplateOutput("featureTestsTemplate", featureTestsTemplate, data)
	if err != nil {
		t.Fatalf("generateTemplateOutput() error = %v", err)
	}
	assertContains(t, "featureTestsTemplate", string(out), []string{
		"func TestGeneratedFeatureGates(t *testing.T) {",
		"feature: resources.Debriefs,",
		"grants:  grants{accesstypes.List: true},",
		"wantOn:  []int{http.StatusForbidden},",
		`{name: "Ship.CargoBays", feature: resources.CargoManifest, target: "/api/ships?columns=cargoBays&sort=id"},`,
		"resource.MigrateFeatures(ctx, client, resources.Features())",
		`rr.Body.String() != "Not Found\n"`,
	}, nil)
}

// Test_apiClientData_features pins the TypeScript client's feature surface: the
// features route on every descriptor, the Feature union and constants when flags are
// declared, the gate on a resource's and a method's entry, and the library's
// FeatureFlags resource and SetFeature method where the collection carries them.
func Test_apiClientData_features(t *testing.T) {
	t.Parallel()

	r := featureFixtureGenerator(t)
	for _, res := range r.resources {
		for _, f := range res.Fields {
			f.typescriptType = "string"
		}
	}
	collection, err := r.computeCollectionData()
	if err != nil {
		t.Fatalf("computeCollectionData() error = %v", err)
	}

	tests := []struct {
		name            string
		generator       func() *typescriptGenerator
		wantContains    []string
		wantNotContains []string
	}{
		{
			name: "flags declared, the collection carrying the library's resource and method",
			generator: func() *typescriptGenerator {
				return &typescriptGenerator{client: r.client, rc: resource.MustNewGeneratedCollection(collection)}
			},
			wantContains: []string{
				"export type Feature = 'cargo_manifest' | 'debriefs';",
				"export const Feature = {\n  CargoManifest: 'cargo_manifest' as Feature,\n  Debriefs: 'debriefs' as Feature,\n};",
				"features: { route: 'features' },",
				"route: 'debriefs',\n      scope: 'global',\n      consolidated: false,\n      keys: ['id'],\n      operations: ['list', 'read', 'create', 'patch', 'remove'],\n      page: { default: 50 },\n      patchable: ['title'],\n      feature: 'debriefs',",
				"[Methods.GatedMethod]: { method: Methods.GatedMethod, property: 'gatedMethod', route: 'gated-method', scope: 'global', feature: 'debriefs' },",
				"[Resources.FeatureFlags]: {\n      resource: Resources.FeatureFlags,\n      property: 'featureFlags',\n      route: 'feature-flags',\n      scope: 'global',\n      consolidated: false,\n      keys: ['name'],\n      operations: ['list', 'read'],\n      page: { default: 50 },\n      order: [{ field: 'name', direction: 'asc' }],\n    },",
				"[Methods.SetFeature]: { method: Methods.SetFeature, property: 'setFeature', route: 'set-feature', scope: 'global', answers: true },",
				"featureFlags: ResourceHandle<FeatureFlags, FeatureFlagsKey, 'list' | 'read'>;",
				"setFeature: MethodHandle<SetFeature, SetFeatureResult>;",
				"export type FeatureFlagsKey = [name: string];",
			},
		},
		{
			name: "nothing declared and no collection: the route alone",
			generator: func() *typescriptGenerator {
				return &typescriptGenerator{client: &client{}}
			},
			wantContains:    []string{"features: { route: 'features' },"},
			wantNotContains: []string{"export type Feature", "FeatureFlags", "SetFeature", "feature:"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gen := tt.generator()
			out, err := gen.generateTemplateOutput("typescriptAPITemplate", typescriptAPITemplate, gen.apiClientData())
			if err != nil {
				t.Fatalf("generateTemplateOutput() error = %v", err)
			}
			assertContains(t, "typescriptAPITemplate", string(out), tt.wantContains, tt.wantNotContains)
		})
	}
}

// Test_typescriptMetadata_features pins the metadata files: the FeatureFlags interface,
// map entry and scope, the SetFeature shapes and map entry, and the gate on a field's,
// a resource's and a method's metadata.
func Test_typescriptMetadata_features(t *testing.T) {
	t.Parallel()

	r := featureFixtureGenerator(t)
	for _, res := range r.resources {
		for _, f := range res.Fields {
			f.typescriptType = "string"
		}
	}
	for _, f := range r.computedResources[0].Fields {
		f.typescriptType = "string"
	}
	gen := &typescriptGenerator{client: r.client}

	resources, err := gen.generateTemplateOutput(typescriptResourcesTemplate, typescriptResourcesTemplate, tsResourcesData{
		File:              gen,
		Resources:         r.resources,
		ComputedResources: r.computedResources,
		GenPrefix:         "zz_gen",
		FeatureFlags:      true,
	})
	if err != nil {
		t.Fatalf("generateTemplateOutput(resources) error = %v", err)
	}
	assertContains(t, "typescriptResourcesTemplate", string(resources), []string{
		"export interface FeatureFlags {\n  name: string;\n  description?: string;\n  enabled?: boolean;\n  updatedAt?: Date;\n  updatedBy?: string;\n}",
		"[Resources.FeatureFlags]: {\n    route: 'feature-flags',\n    createDisabled: true,\n    updateDisabled: true,\n    deleteDisabled: true,",
		"{ fieldName: 'name', primaryKey: { ordinalPosition: 1 }, displayType: 'string', required: true, isIndex: true },",
		"[Resources.FeatureFlags]: PermissionScopes.global,",
		"[Resources.Debriefs]: {\n    route: 'debriefs',\n    feature: 'debriefs',",
		"isIndex: false, feature: 'cargo_manifest' },",
		"[Resources.Manifests]: {\n    route: 'manifests',\n    createDisabled: true,",
		"{ fieldName: 'bays', displayType: 'string', required: false, isIndex: false, feature: 'cargo_manifest' },",
	}, nil)

	methods, err := gen.generateTemplateOutput(typescriptMethodsTemplate, typescriptMethodsTemplate, tsMethodsData{
		File:         gen,
		RPCMethods:   r.rpcMethods,
		GenPrefix:    "zz_gen",
		FeatureFlags: true,
	})
	if err != nil {
		t.Fatalf("generateTemplateOutput(methods) error = %v", err)
	}
	assertContains(t, "typescriptMethodsTemplate", string(methods), []string{
		"export interface SetFeature {\n  name: string;\n  enabled: boolean;\n}",
		"export interface SetFeatureResult {\n  name: string;\n  enabled: boolean;\n  updatedAt: Date;\n}",
		"[Methods.SetFeature]: {\n    route: 'set-feature',\n    answers: true,",
		"[Methods.GatedMethod]: {\n    route: 'gated-method',\n    feature: 'debriefs',",
		"feature?: string;",
	}, nil)
}

// assertContains checks a rendered template for the wanted and unwanted fragments.
func assertContains(t *testing.T, name, out string, wantContains, wantNotContains []string) {
	t.Helper()

	for _, want := range wantContains {
		if !strings.Contains(out, want) {
			t.Errorf("%s output missing %q:\n%s", name, want, out)
		}
	}
	for _, notWant := range wantNotContains {
		if strings.Contains(out, notWant) {
			t.Errorf("%s output must not contain %q:\n%s", name, notWant, out)
		}
	}
}
