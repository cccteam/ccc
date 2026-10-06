package generation

import (
	"go/format"
	"strings"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/generation/parser"
)

// Test_resolveFieldFormerly pins @formerly on a resource's fields: a renamed field keeps
// its former name, and the struct form, the field's current name, another field's wire
// name and a former name two fields share are each refused naming the rule.
func Test_resolveFieldFormerly(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadFixture(t, "formerlyfixture"))

	tests := []struct {
		name       string
		structName string
		wantFormer map[string]string
		wantErr    string
	}{
		{
			name:       "a renamed field keeps its former name",
			structName: "Article",
			wantFormer: map[string]string{"Headline": "Title"},
		},
		{
			name:       "the field's current name is refused",
			structName: "SameName",
			wantErr:    "struct SameName field Headline: @formerly(headline) names the field's current name; a former name is the one older applications still send",
		},
		{
			name:       "another field's wire name is refused",
			structName: "Clash",
			wantErr:    `struct Clash field Headline: @formerly(Body) names field Body's wire name "body"; a former name belongs to no field`,
		},
		{
			name:       "one former name on two fields is refused",
			structName: "TwoFormer",
			wantErr:    "struct TwoFormer field Body: @formerly(Title) names the former name field Headline already carries; a former name belongs to one field",
		},
		{
			name:       "the struct form on a resource is refused",
			structName: "Renamed",
			wantErr:    "struct Renamed: @formerly renames a field or a method; a resource keeps its name",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			pStruct, annotations := scanFixtureStruct(t, structs, tt.structName)
			res := fixtureResource(t, structs, tt.structName, nil)
			err := resolveFieldFormerly(res, pStruct, annotations)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("resolveFieldFormerly() error = %v, want it to contain %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("resolveFieldFormerly() error = %v", err)
			}
			for _, field := range res.Fields {
				if got, want := field.Formerly, tt.wantFormer[field.Name()]; got != want {
					t.Errorf("%s.Formerly = %q, want %q", field.Name(), got, want)
				}
			}
		})
	}
}

// Test_resourceField_formerlyTags pins what a renamed field renders onto the request
// structs: its former wire name under the formerly tag on the list, read and patch
// structs, and nothing on a field never renamed.
func Test_resourceField_formerlyTags(t *testing.T) {
	t.Parallel()

	res := formerlyFixtureResource(t, fixtureStructs(loadFixture(t, "formerlyfixture")))

	tests := []struct {
		name         string
		field        string
		wantWire     string
		wantTag      string
		wantPatchTag string
	}{
		{name: "the renamed field", field: "Headline", wantWire: "title", wantTag: `formerly:"title"`, wantPatchTag: `formerly:"title"`},
		{name: "a field never renamed", field: "Body"},
		{name: "the key", field: "ID"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var field *resourceField
			for _, f := range res.Fields {
				if f.Name() == tt.field {
					field = f
				}
			}
			if field == nil {
				t.Fatalf("field %s not in the fixture", tt.field)
			}
			if got := field.FormerWireName(); got != tt.wantWire {
				t.Errorf("FormerWireName() = %q, want %q", got, tt.wantWire)
			}
			if got := field.FormerlyTag(); got != tt.wantTag {
				t.Errorf("FormerlyTag() = %q, want %q", got, tt.wantTag)
			}
			if got := field.FormerlyTagForPatch(); got != tt.wantPatchTag {
				t.Errorf("FormerlyTagForPatch() = %q, want %q", got, tt.wantPatchTag)
			}
		})
	}
}

// formerlyFixtureResource builds the Article resource with its @formerly resolved.
func formerlyFixtureResource(t *testing.T, structs map[string]*parser.Struct) *resourceInfo {
	t.Helper()

	pStruct, annotations := scanFixtureStruct(t, structs, "Article")
	res := fixtureResource(t, structs, "Article", nil)
	if err := resolveFieldFormerly(res, pStruct, annotations); err != nil {
		t.Fatalf("resolveFieldFormerly() error = %v", err)
	}

	return res
}

// Test_rejectFormerlyAnnotations pins the refusal on a kind that carries no former
// name, on the struct and on a field.
func Test_rejectFormerlyAnnotations(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadFixture(t, "formerlyfixture"))

	tests := []struct {
		name       string
		structName string
		wantErr    string
	}{
		{
			name:       "on a field",
			structName: "Article",
			wantErr:    "struct Article field Headline: @formerly renames a field of a resource or an RPC method; a computed resource carries no former name",
		},
		{
			name:       "on the struct",
			structName: "Renamed",
			wantErr:    "struct Renamed: @formerly renames a field of a resource or an RPC method; a computed resource carries no former name",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			pStruct, annotations := scanFixtureStruct(t, structs, tt.structName)
			err := rejectFormerlyAnnotations(pStruct, annotations, "computed resource")
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("rejectFormerlyAnnotations() error = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

// Test_structsToRPCMethods_formerly pins @formerly through RPC extraction: a renamed
// method carries its former name, a renamed request field and a renamed result field
// carry theirs, and the method's own name, another method's name, and a field naming
// another field's wire name in the request or the result are refused.
func Test_structsToRPCMethods_formerly(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadFixture(t, "formerlyfixture"))

	tests := []struct {
		name        string
		structNames []string
		wantErr     string
	}{
		{name: "a renamed method with renamed request and result fields", structNames: []string{"Publish", "Published"}},
		{
			name:        "the method's own name is refused",
			structNames: []string{"Withdraw"},
			wantErr:     "struct Withdraw: @formerly(Withdraw) names the method's current name; a former name is the one older applications still execute",
		},
		{
			name:        "another method's name is refused",
			structNames: []string{"Publish", "Published", "Retract"},
			wantErr:     "struct Retract: @formerly(Publish) names method Publish; a former name belongs to no method",
		},
		{
			name:        "a request field naming another field's wire name is refused",
			structNames: []string{"Reword"},
			wantErr:     `struct Reword field Headline: @formerly(Body) names field Body's wire name "body"; a former name belongs to no field`,
		},
		{
			name:        "a result field naming another field's wire name is refused",
			structNames: []string{"Restate", "Restated"},
			wantErr:     `struct Restated field Headline, the result of Restate: @formerly(Body) names field Body's wire name "body"; a former name belongs to no field`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			pStructs := make([]*parser.Struct, 0, len(tt.structNames))
			for _, name := range tt.structNames {
				if structs[name] == nil {
					t.Fatalf("struct %q not in the fixture", name)
				}
				pStructs = append(pStructs, structs[name])
			}
			methods, err := (&client{}).structsToRPCMethods(pStructs)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("structsToRPCMethods() error = %v, want it to contain %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("structsToRPCMethods() error = %v", err)
			}
			if len(methods) != 1 {
				t.Fatalf("structsToRPCMethods() returned %d methods, want 1", len(methods))
			}
			method := methods[0]
			if got, want := method.Formerly, "Release"; got != want {
				t.Errorf("Formerly = %q, want %q", got, want)
			}
			if got, want := method.FormerRouteName(), "release"; got != want {
				t.Errorf("FormerRouteName() = %q, want %q", got, want)
			}
			if got, want := method.Fields[1].Formerly, "Title"; got != want {
				t.Errorf("Fields[1].Formerly = %q, want %q", got, want)
			}
			if got, want := method.Fields[1].FormerlyTag(), `formerly:"title"`; got != want {
				t.Errorf("Fields[1].FormerlyTag() = %q, want %q", got, want)
			}
			if got := method.Fields[0].FormerlyTag(); got != "" {
				t.Errorf("Fields[0].FormerlyTag() = %q, want empty", got)
			}
			if got, want := method.Result.Fields[1].FormerName, "title"; got != want {
				t.Errorf("Result.Fields[1].FormerName = %q, want %q", got, want)
			}
			if got, want := method.Result.Fields[1].FormerGoName(), "Title"; got != want {
				t.Errorf("Result.Fields[1].FormerGoName() = %q, want %q", got, want)
			}
			if !method.Request.ConvertsWhole(toSource) {
				t.Error("Request.ConvertsWhole(toSource) = false, want true: the request mirror carries no extra field")
			}
			if method.Result.ConvertsWhole(toMirror) {
				t.Error("Result.ConvertsWhole(toMirror) = true, want false: the response mirror carries the former name")
			}
		})
	}
}

// formerlyFixtureMethod extracts the renamed method with its result from the fixture.
func formerlyFixtureMethod(t *testing.T, structs map[string]*parser.Struct) *rpcMethodInfo {
	t.Helper()

	methods, err := (&client{}).structsToRPCMethods([]*parser.Struct{structs["Publish"], structs["Published"]})
	if err != nil {
		t.Fatalf("structsToRPCMethods() error = %v", err)
	}

	return methods[0]
}

// Test_rpcRoute_formerPath pins the former route beside a renamed method's route, at
// the kebab-cased former name under the outlet prefix, and under the domain segment
// pair for a domain-scoped method.
func Test_rpcRoute_formerPath(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadFixture(t, "formerlyfixture"))

	tests := []struct {
		name          string
		scope         accesstypes.PermissionScope
		wantPath      string
		wantFormer    string
		wantFormerURL string
	}{
		{name: "a global method", wantPath: "/api/publish", wantFormer: "/api/release", wantFormerURL: "/api/release"},
		{
			name:          "a domain-scoped method",
			scope:         accesstypes.DomainPermissionScope,
			wantPath:      "/api/sectors/{sectorID}/publish",
			wantFormer:    "/api/sectors/{sectorID}/release",
			wantFormerURL: "/api/sectors/" + domainTestValue + "/release",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			method := formerlyFixtureMethod(t, structs)
			method.PermissionScope = tt.scope
			r := &resourceGenerator{client: &client{}, domainRouteSegment: "sectors", domainRouteParam: "sectorID"}
			route := r.rpcRoute(method, "api")
			if route.Path != tt.wantPath {
				t.Errorf("Path = %q, want %q", route.Path, tt.wantPath)
			}
			if route.FormerPath != tt.wantFormer {
				t.Errorf("FormerPath = %q, want %q", route.FormerPath, tt.wantFormer)
			}
			if route.FormerTestURL != tt.wantFormerURL {
				t.Errorf("FormerTestURL = %q, want %q", route.FormerTestURL, tt.wantFormerURL)
			}
		})
	}
}

// Test_formerlyTemplates pins what the templates render for a former name: the request
// structs carry the formerly tag, the rows carry both keys under the one masking check,
// the RPC handler's request mirror carries the tag and its response mirror the former
// field with the same value, the router registers the former route on the same handler
// and its test drives it, the collection carries the former names, and the TypeScript
// knows only the current name.
func Test_formerlyTemplates(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadFixture(t, "formerlyfixture"))
	res := formerlyFixtureResource(t, structs)
	method := formerlyFixtureMethod(t, structs)
	c := &client{rpcMethods: []*rpcMethodInfo{method}}
	r := &resourceGenerator{client: c}
	route := r.rpcRoute(method, "api")
	portalRoute := r.rpcRoute(method, "portal/api")
	handlerData := handlerContentData{ResourcePackage: "resources", Resource: res, VirtualResourcesPackage: "virtualresources", ApplicationName: "App", ReceiverName: "a"}

	tests := []struct {
		name            string
		template        string
		goSource        bool
		data            any
		wantContains    []string
		wantNotContains []string
	}{
		{
			name:     "list handler",
			template: listTemplate,
			data:     handlerData,
			wantContains: []string{
				`json:"headline" formerly:"title"`,
				`if !row.Masked("headline") { rmap["headline"] = rec.Headline rmap["title"] = rec.Headline }`,
			},
			wantNotContains: []string{`row.Masked("title")`},
		},
		{
			name:     "read handler",
			template: readTemplate,
			data:     handlerData,
			wantContains: []string{
				`json:"headline" formerly:"title"`,
				`if !row.Masked("headline") { rmap["headline"] = rec.Headline rmap["title"] = rec.Headline }`,
			},
			wantNotContains: []string{`row.Masked("title")`},
		},
		{
			name:         "patch handler",
			template:     patchTemplate,
			data:         handlerData,
			wantContains: []string{`json:"headline" formerly:"title"`},
		},
		{
			name:     "rpc handler",
			template: rpcHandlerTemplate,
			goSource: true,
			data:     &rpcHandlerData{Source: "pkg/rpc", RPCMethod: method, Package: "app", ApplicationName: "App", ReceiverName: "a", ResourcesPackage: "resources"},
			wantContains: []string{
				"Headline string `json:\"headline\" formerly:\"title\"`",
				"Title string `json:\"title\"`",
				"mirrorResponse := func(src formerlyfixture.Published) *response {",
				"Headline: view.Headline, Title: view.Headline",
				"mirrorResponse(*result)",
				"p := (*formerlyfixture.Publish)(params)",
			},
			wantNotContains: []string{"(*response)(result)", "sourceRequest"},
		},
		{
			name:     "routes",
			template: routesTemplate,
			goSource: true,
			data: &routerFileData{
				ServesSessions:  true,
				Package:         "router",
				RoutesMap:       map[string][]*generatedRoute{"Publish": {route}},
				ResourcePackage: "resources",
				RoutePrefix:     "api",
				ExtraOutlets: []*outletRouteData{{
					Name: "portal", Suffix: "Portal", Prefix: "portal/api",
					RoutesMap: map[string][]*generatedRoute{"Publish": {portalRoute}},
				}},
			},
			wantContains: []string{
				"publishHandler := h.Publish()",
				`r.Post("/api/publish", publishHandler)`,
				`r.Post("/api/release", publishHandler)`,
				`r.Post("/portal/api/publish", publishHandler)`,
				`r.Post("/portal/api/release", publishHandler)`,
			},
		},
		{
			name:     "router test",
			template: routerTestTemplate,
			goSource: true,
			data:     &routerFileData{Package: "router", RouterTestRoutes: []*generatedRoute{route}, RoutePrefix: "api"},
			wantContains: []string{
				`url: "/api/publish", method: http.MethodPost, handlerFunc: "Publish",`,
				`url: "/api/release", method: http.MethodPost, handlerFunc: "Publish",`,
			},
		},
		{
			name:     "collection",
			template: collectionTemplate,
			goSource: true,
			data: &collectionFileData{Source: "pkg/resources", Package: "router", Data: resource.CollectionData{Resources: []resource.CollectionResource{
				{
					Name: "Articles", Scope: accesstypes.GlobalPermissionScope,
					Permissions: []accesstypes.Permission{accesstypes.List},
					Tags:        []resource.TagData{{Name: "headline", Formerly: "title"}, {Name: "id"}},
				},
				{Name: "Publish", Scope: accesstypes.GlobalPermissionScope, Permissions: []accesstypes.Permission{accesstypes.Execute}, Formerly: "Release"},
			}}},
			wantContains: []string{
				`{Name: "headline", Formerly: "title"},`,
				`Name: "Publish", Scope: accesstypes.GlobalPermissionScope, Formerly: "Release", Permissions: []accesstypes.Permission{accesstypes.Execute},`,
			},
		},
		{
			name:            "typescript methods",
			template:        typescriptMethodsTemplate,
			data:            tsMethodsData{File: &typescriptGenerator{client: c}, RPCMethods: c.rpcMethods, GenPrefix: "zz_gen"},
			wantContains:    []string{"headline"},
			wantNotContains: []string{"title", "Title", "release", "Release"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			out, err := r.generateTemplateOutput(tt.name, tt.template, tt.data)
			if err != nil {
				t.Fatalf("generateTemplateOutput() error = %v", err)
			}
			if tt.goSource {
				if out, err = format.Source(out); err != nil {
					t.Fatalf("format.Source() error = %v", err)
				}
			}
			// gofmt aligns fields and tags, and a handler fragment carries the empty
			// tags' spaces, so space runs collapse before matching.
			normalized := strings.Join(strings.Fields(string(out)), " ")
			for _, want := range tt.wantContains {
				if !strings.Contains(normalized, want) {
					t.Errorf("output lacks %q:\n%s", want, out)
				}
			}
			for _, unwanted := range tt.wantNotContains {
				if strings.Contains(normalized, unwanted) {
					t.Errorf("output must not contain %q:\n%s", unwanted, out)
				}
			}
		})
	}
}
