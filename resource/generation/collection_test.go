package generation

import (
	"go/format"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/generation/parser"
	"github.com/cccteam/ccc/resource/generation/parser/genlang"
	"github.com/google/go-cmp/cmp"
)

func loadCollectionFixture(t *testing.T) *parser.Package {
	t.Helper()

	// Other tests in the package chdir to the module root (client construction does),
	// so resolve the fixture relative to this source file, not the working directory.
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed")
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd() error = %v", err)
	}
	fixtureDir, err := filepath.Rel(cwd, filepath.Join(filepath.Dir(thisFile), "testdata", "collectionfixture"))
	if err != nil {
		t.Fatalf("filepath.Rel() error = %v", err)
	}

	pkgs, err := parser.LoadPackages(fixtureDir)
	if err != nil {
		t.Fatalf("parser.LoadPackages() error = %v", err)
	}
	pkg := pkgs["collectionfixture"]
	if pkg == nil {
		t.Fatal("fixture package collectionfixture not loaded")
	}

	return parser.ParsePackage(pkg)
}

func fixtureStructs(pkg *parser.Package) map[string]*parser.Struct {
	structs := make(map[string]*parser.Struct)
	for _, s := range pkg.Structs {
		structs[s.Name()] = s
	}

	return structs
}

func fixtureResource(t *testing.T, structs map[string]*parser.Struct, name string, mutate func(*resourceInfo)) *resourceInfo {
	t.Helper()

	s := structs[name]
	if s == nil {
		t.Fatalf("struct %q not found in fixture package", name)
	}

	res := &resourceInfo{
		TypeInfo: s.TypeInfo,
		PkCount:  1,
	}
	for _, f := range s.Fields() {
		res.Fields = append(res.Fields, &resourceField{
			Field:        f,
			Parent:       res,
			IsPrimaryKey: f.Name() == "ID",
		})
	}
	if mutate != nil {
		mutate(res)
	}

	return res
}

func fixtureComputedResource(t *testing.T, structs map[string]*parser.Struct, name string) *computedResource {
	t.Helper()

	s := structs[name]
	if s == nil {
		t.Fatalf("struct %q not found in fixture package", name)
	}

	res := &computedResource{Struct: s}
	for _, f := range s.Fields() {
		res.Fields = append(res.Fields, &computedField{Field: f, IsPrimaryKey: f.Name() == "ID"})
	}

	return res
}

// collectionFixtureGenerator builds the resourceGenerator both computeCollectionData
// tests share: the same resources, computed resources, RPC methods, and manual
// registrations, differing only in whether route generation is enabled.
func collectionFixtureGenerator(t *testing.T) *resourceGenerator {
	t.Helper()

	structs := fixtureStructs(loadCollectionFixture(t))

	r := &resourceGenerator{client: &client{
		genComputedResources: true,
		genRPCMethods:        true,
	}}
	r.resources = []*resourceInfo{
		fixtureResource(t, structs, "Fossil", func(res *resourceInfo) {
			res.IsConsolidated = true
			res.SuppressedRoutes = []RouteType{AllRoutes}
		}),
		fixtureResource(t, structs, "Gadget", func(res *resourceInfo) {
			res.IsVirtual = true
			res.PermissionScope = accesstypes.DomainPermissionScope
		}),
		fixtureResource(t, structs, "Ledger", func(res *resourceInfo) {
			res.SuppressedHandlers = []HandlerType{ListHandler, ReadHandler, PatchHandler}
			res.ManualAddResourceSets = []HandlerType{ListHandler, ReadHandler}
		}),
		fixtureResource(t, structs, "Relic", func(res *resourceInfo) {
			res.SuppressedRoutes = []RouteType{AllRoutes}
		}),
		fixtureResource(t, structs, "Vault", func(res *resourceInfo) {
			res.SuppressedHandlers = []HandlerType{ListHandler, ReadHandler, PatchHandler}
			res.ManualAddResourceSets = []HandlerType{ListHandler}
			res.PermissionScope = accesstypes.DomainPermissionScope
		}),
		fixtureResource(t, structs, "Sprocket", func(res *resourceInfo) {
			res.IsConsolidated = true
		}),
		fixtureResource(t, structs, "Widget", nil),
	}
	r.computedResources = []*computedResource{
		fixtureComputedResource(t, structs, "Summary"),
	}
	r.rpcMethods = []*rpcMethodInfo{
		{Struct: structs["DoSomething"], PermissionScope: accesstypes.DomainPermissionScope},
		{Struct: structs["HiddenMethod"], SuppressHandler: true},
	}
	r.manualRegistrations = []ManualRegistration{
		{Permission: accesstypes.Execute, Resource: "UploadThings"},
		{Scope: accesstypes.DomainPermissionScope, Permission: accesstypes.Read, Resource: "ScopedThings"},
	}

	return r
}

// Test_computeCollectionData pins the static permission computation against the exact
// registrations the generated handlers perform at runtime, across the fixture's field
// tag shapes and resource kinds. Without route generation no generated wiring exists,
// so only the manual declarations are collected.
func Test_computeCollectionData(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		genRoutes bool
		want      resource.CollectionData
	}{
		{
			name:      "with route generation",
			genRoutes: true,
			want: resource.CollectionData{Resources: []resource.CollectionResource{
				{
					// @permissionScope(domain) on an RPC method.
					Name:        "DoSomething",
					Scope:       accesstypes.DomainPermissionScope,
					Permissions: []accesstypes.Permission{accesstypes.Execute},
				},
				{
					// @permissionScope(domain) on a generated (virtual) resource.
					Name:        "Gadgets",
					Scope:       accesstypes.DomainPermissionScope,
					Permissions: []accesstypes.Permission{accesstypes.List},
					Tags: []resource.TagData{
						{Name: "id"},
						{Name: "name"},
					},
				},
				{
					Name:        "ScopedThings",
					Scope:       accesstypes.DomainPermissionScope,
					Permissions: []accesstypes.Permission{accesstypes.Read},
				},
				{
					// @permissionScope(domain) with a manual Set.
					Name:        "Vaults",
					Scope:       accesstypes.DomainPermissionScope,
					Permissions: []accesstypes.Permission{accesstypes.List},
					Tags: []resource.TagData{
						{Name: "id"},
						{Name: "name"},
					},
				},
				{
					// Consolidated and routing-disabled: no list/read routes, but the shared
					// consolidated patch handler still registers it.
					Name:        "Fossils",
					Scope:       accesstypes.GlobalPermissionScope,
					Permissions: []accesstypes.Permission{accesstypes.Create, accesstypes.Delete, accesstypes.Update},
					Tags: []resource.TagData{
						{Name: "name"},
					},
				},
				{
					// All handlers suppressed (hand-written), Sets declared via
					// @manualAddResourceSet.
					Name:        "Ledgers",
					Scope:       accesstypes.GlobalPermissionScope,
					Permissions: []accesstypes.Permission{accesstypes.List, accesstypes.Read},
					Tags: []resource.TagData{
						{Name: "id"},
						{Name: "total", Permissions: []accesstypes.Permission{accesstypes.Read}},
					},
				},
				{
					Name:        "Sprockets",
					Scope:       accesstypes.GlobalPermissionScope,
					Permissions: []accesstypes.Permission{accesstypes.Create, accesstypes.Delete, accesstypes.List, accesstypes.Read, accesstypes.Update},
					Tags: []resource.TagData{
						{Name: "id"},
						{Name: "name", Permissions: []accesstypes.Permission{accesstypes.Update}},
					},
				},
				{
					Name:        "Summaries",
					Scope:       accesstypes.GlobalPermissionScope,
					Permissions: []accesstypes.Permission{accesstypes.List, accesstypes.Read},
					Tags: []resource.TagData{
						{Name: "id"},
						{Name: "total"},
					},
				},
				{
					Name:        "UploadThings",
					Scope:       accesstypes.GlobalPermissionScope,
					Permissions: []accesstypes.Permission{accesstypes.Execute},
				},
				{
					Name:        "Widgets",
					Scope:       accesstypes.GlobalPermissionScope,
					Permissions: []accesstypes.Permission{accesstypes.Create, accesstypes.Delete, accesstypes.List, accesstypes.Read, accesstypes.Update},
					Tags: []resource.TagData{
						{Name: "code", Permissions: []accesstypes.Permission{accesstypes.Update}},
						{Name: "derived"},
						{Name: "id"},
						{Name: "listedName", Permissions: []accesstypes.Permission{accesstypes.List}},
						{Name: "name", Permissions: []accesstypes.Permission{accesstypes.Read, accesstypes.Update}},
						{Name: "secret"},
					},
					ImmutableTags: []accesstypes.Tag{"code"},
				},
			}},
		},
		{
			name: "without route generation",
			want: resource.CollectionData{Resources: []resource.CollectionResource{
				{
					Name:        "ScopedThings",
					Scope:       accesstypes.DomainPermissionScope,
					Permissions: []accesstypes.Permission{accesstypes.Read},
				},
				{
					Name:        "Vaults",
					Scope:       accesstypes.DomainPermissionScope,
					Permissions: []accesstypes.Permission{accesstypes.List},
					Tags: []resource.TagData{
						{Name: "id"},
						{Name: "name"},
					},
				},
				{
					Name:        "Ledgers",
					Scope:       accesstypes.GlobalPermissionScope,
					Permissions: []accesstypes.Permission{accesstypes.List, accesstypes.Read},
					Tags: []resource.TagData{
						{Name: "id"},
						{Name: "total", Permissions: []accesstypes.Permission{accesstypes.Read}},
					},
				},
				{
					Name:        "UploadThings",
					Scope:       accesstypes.GlobalPermissionScope,
					Permissions: []accesstypes.Permission{accesstypes.Execute},
				},
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := collectionFixtureGenerator(t)
			r.genRoutes = tt.genRoutes

			got, err := r.computeCollectionData()
			if err != nil {
				t.Fatalf("computeCollectionData() error = %v", err)
			}

			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("computeCollectionData() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// Test_collectionTemplate renders the generated collection file and requires it to be
// valid Go that reconstructs the same CollectionData it was rendered from.
func Test_collectionTemplate(t *testing.T) {
	t.Parallel()

	data := resource.CollectionData{Resources: []resource.CollectionResource{
		{
			Name:        "Widgets",
			Scope:       accesstypes.GlobalPermissionScope,
			Permissions: []accesstypes.Permission{accesstypes.List, accesstypes.Read},
			Tags: []resource.TagData{
				{Name: "id", Permissions: []accesstypes.Permission{accesstypes.List}},
				{Name: "name"},
			},
			ImmutableTags: []accesstypes.Tag{"code"},
		},
		{
			Name:        "DoSomething",
			Scope:       accesstypes.DomainPermissionScope,
			Permissions: []accesstypes.Permission{accesstypes.Execute},
		},
	}}

	r := &resourceGenerator{client: &client{}}
	output, err := r.generateTemplateOutput("collectionTemplate", collectionTemplate, &collectionFileData{
		Source:  "pkg/resources",
		Package: "router",
		Data:    data,
	})
	if err != nil {
		t.Fatalf("generateTemplateOutput() error = %v", err)
	}

	formatted, err := format.Source(output)
	if err != nil {
		t.Fatalf("rendered collection file is not valid Go: %v\n%s", err, output)
	}

	// gofmt aligns struct-literal fields, so collapse space runs before matching.
	normalized := strings.Join(strings.Fields(string(formatted)), " ")
	for _, want := range []string{
		"package router",
		"func Collection() *resource.GeneratedCollection {",
		"resource.MustNewGeneratedCollection(resource.CollectionData{",
		`Name: "Widgets",`,
		"Scope: accesstypes.GlobalPermissionScope,",
		"Permissions: []accesstypes.Permission{accesstypes.List, accesstypes.Read},",
		`{Name: "id", Permissions: []accesstypes.Permission{accesstypes.List}},`,
		`{Name: "name"},`,
		`ImmutableTags: []accesstypes.Tag{"code"},`,
		"Scope: accesstypes.DomainPermissionScope,",
		"Permissions: []accesstypes.Permission{accesstypes.Execute},",
	} {
		if !strings.Contains(normalized, want) {
			t.Errorf("rendered collection file missing %q:\n%s", want, formatted)
		}
	}
}

// Test_manualRegistrationsFromConstants pins @manualAddResource extraction: doc-comment
// and line-comment placement, explicit scope, and that unannotated constants (dormant
// declarations) contribute nothing.
func Test_manualRegistrationsFromConstants(t *testing.T) {
	t.Parallel()

	pkg := loadCollectionFixture(t)

	got, err := manualRegistrationsFromConstants(pkg.Constants)
	if err != nil {
		t.Fatalf("manualRegistrationsFromConstants() error = %v", err)
	}

	// An absent scope stays empty; the global default applies at registration.
	want := []ManualRegistration{
		{Permission: accesstypes.Execute, Resource: "ManualThings"},
		{Scope: accesstypes.DomainPermissionScope, Permission: accesstypes.Read, Resource: "ScopedThings"},
		{Permission: accesstypes.Execute, Resource: "UploadThings"},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("manualRegistrationsFromConstants() mismatch (-want +got):\n%s", diff)
	}
}

func Test_parseManualAddResourceArgs(t *testing.T) {
	t.Parallel()

	pkg := loadCollectionFixture(t)
	var constant *parser.Constant
	for _, c := range pkg.Constants {
		if c.Name() == "DormantThing" {
			constant = c
		}
	}
	if constant == nil {
		t.Fatal("DormantThing constant not found in fixture")
	}

	tests := []struct {
		name    string
		arg     string
		want    ManualRegistration
		wantErr bool
	}{
		{
			name: "permission only leaves the scope empty for the app-wide default",
			arg:  "Execute",
			want: ManualRegistration{Permission: accesstypes.Execute, Resource: "DormantThings"},
		},
		{
			name: "permission with scope",
			arg:  "Read, domain",
			want: ManualRegistration{Scope: accesstypes.DomainPermissionScope, Permission: accesstypes.Read, Resource: "DormantThings"},
		},
		{name: "unknown scope", arg: "Read, bogus", wantErr: true},
		{name: "empty permission", arg: "", wantErr: true},
		{name: "too many arguments", arg: "Read, domain, extra", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseManualAddResourceArgs(constant, tt.arg)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseManualAddResourceArgs() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("parseManualAddResourceArgs() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// Test_optionDependencies pins GenerateTypescript's option resolution: repeatable once
// per target directory, duplicate directories rejected, and routeless runs resolve —
// whether their requested outputs have a permission source is validated at Generate
// time (see Test_validateTypescriptTargets).
func Test_optionDependencies(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		options     []ResourceOption
		wantTargets int
		wantErr     bool
	}{
		{
			name:        "typescript with routes is valid",
			options:     []ResourceOption{GenerateRoutes("pkg/router", "api"), GenerateTypescript("gui/src")},
			wantTargets: 1,
		},
		{
			name:        "typescript without routes resolves",
			options:     []ResourceOption{GenerateTypescript("gui/src", GenerateEnums())},
			wantTargets: 1,
		},
		{
			name: "typescript-specific options are accepted",
			options: []ResourceOption{
				GenerateRoutes("pkg/router", "api"),
				GenerateTypescript("gui/src", GenerateMetadata(), GeneratePermissions(), GenerateEnums()),
			},
			wantTargets: 1,
		},
		{
			name: "repeated calls collect one target per destination",
			options: []ResourceOption{
				GenerateTypescript("apps/admin/gui/src", GenerateEnums()),
				GenerateTypescript("apps/portal/gui/src", GenerateEnums()),
				GenerateTypescript("apps/partner/gui/src", GenerateEnums()),
			},
			wantTargets: 3,
		},
		{
			name: "duplicate destinations are an error",
			options: []ResourceOption{
				GenerateTypescript("gui/src", GenerateEnums()),
				GenerateTypescript("gui/src", GenerateMetadata()),
			},
			wantErr: true,
		},
		{
			name: "duplicate destinations differing only in path form are an error",
			options: []ResourceOption{
				GenerateTypescript("gui/src", GenerateEnums()),
				GenerateTypescript("gui/src/", GenerateEnums()),
			},
			wantErr: true,
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
			if (err != nil) != tt.wantErr {
				t.Fatalf("resolveOptions() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && len(r.typescriptTargets) != tt.wantTargets {
				t.Errorf("len(typescriptTargets) = %d, want %d", len(r.typescriptTargets), tt.wantTargets)
			}
		})
	}
}

// Test_validateTypescriptTargets pins the routeless rule: enums-only targets carry no
// requirement, while GeneratePermissions/GenerateMetadata need a permission source —
// GenerateRoutes or at least one manual declaration (WithManualResources /
// @manualAddResource / @manualAddResourceSet) — because the collection they render is
// otherwise empty.
func Test_validateTypescriptTargets(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                string
		genRoutes           bool
		tsOptions           []TSOption
		manualRegistrations []ManualRegistration
		manualSets          []HandlerType
		wantErr             bool
	}{
		{
			name:      "enums-only without routes is valid",
			tsOptions: []TSOption{GenerateEnums()},
		},
		{
			name:      "permissions without routes or manual declarations is an error",
			tsOptions: []TSOption{GeneratePermissions()},
			wantErr:   true,
		},
		{
			name:      "metadata without routes or manual declarations is an error",
			tsOptions: []TSOption{GenerateMetadata()},
			wantErr:   true,
		},
		{
			name:      "permissions with routes is valid",
			genRoutes: true,
			tsOptions: []TSOption{GeneratePermissions(), GenerateMetadata()},
		},
		{
			name:                "permissions with a manual registration is valid",
			tsOptions:           []TSOption{GeneratePermissions()},
			manualRegistrations: []ManualRegistration{{Permission: accesstypes.Execute, Resource: "UploadThings"}},
		},
		{
			name:       "permissions with a manual resource Set is valid",
			tsOptions:  []TSOption{GeneratePermissions(), GenerateMetadata()},
			manualSets: []HandlerType{ListHandler},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := &resourceGenerator{client: &client{}}
			r.genRoutes = tt.genRoutes
			r.typescriptTargets = []typescriptTarget{{destination: "gui/src", options: tt.tsOptions}}
			r.manualRegistrations = tt.manualRegistrations
			r.resources = []*resourceInfo{{ManualAddResourceSets: tt.manualSets}}

			if err := r.validateTypescriptTargets(); (err != nil) != tt.wantErr {
				t.Errorf("validateTypescriptTargets() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// Test_applyManualAddResourceSetDirectives pins @manualAddResourceSet argument parsing.
// Conflicts with generated handlers are validated separately in
// validateManualAddResourceSets, so parsing succeeds regardless of suppression state.
func Test_applyManualAddResourceSetDirectives(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		args    []string
		want    []HandlerType
		wantErr bool
	}{
		{
			name: "single handler",
			args: []string{"listHandler"},
			want: []HandlerType{ListHandler},
		},
		{
			name: "comma list",
			args: []string{"listHandler, readHandler"},
			want: []HandlerType{ListHandler, ReadHandler},
		},
		{
			name: "repeated annotations",
			args: []string{"listHandler", "readHandler"},
			want: []HandlerType{ListHandler, ReadHandler},
		},
		{
			name: "allHandlers expands",
			args: []string{"allHandlers"},
			want: []HandlerType{ListHandler, ReadHandler, PatchHandler},
		},
		{
			name:    "duplicate declaration is an error",
			args:    []string{"listHandler", "listHandler"},
			wantErr: true,
		},
		{
			name:    "unknown argument is an error",
			args:    []string{"bogusHandler"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			res := &resourceInfo{}
			err := applyManualAddResourceSetDirectives(res, slices.Values(tt.args))
			if (err != nil) != tt.wantErr {
				t.Fatalf("applyManualAddResourceSetDirectives() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if diff := cmp.Diff(tt.want, res.ManualAddResourceSets); diff != "" {
				t.Errorf("ManualAddResourceSets mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// Test_validateManualAddResourceSets pins the conflict validation between
// @manualAddResourceSet declarations and the generated route wiring.
func Test_validateManualAddResourceSets(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadCollectionFixture(t))

	tests := []struct {
		name      string
		genRoutes bool
		mutate    func(*resourceInfo)
		wantErr   bool
	}{
		{
			name:      "declaration for a generated handler conflicts",
			genRoutes: true,
			mutate: func(res *resourceInfo) {
				res.ManualAddResourceSets = []HandlerType{ListHandler}
			},
			wantErr: true,
		},
		{
			name:      "suppressed handler does not conflict",
			genRoutes: true,
			mutate: func(res *resourceInfo) {
				res.SuppressedHandlers = []HandlerType{ListHandler}
				res.ManualAddResourceSets = []HandlerType{ListHandler}
			},
		},
		{
			name:      "routing-disabled resource does not conflict",
			genRoutes: true,
			mutate: func(res *resourceInfo) {
				res.SuppressedRoutes = []RouteType{AllRoutes}
				res.ManualAddResourceSets = []HandlerType{ListHandler, ReadHandler, PatchHandler}
			},
		},
		{
			name:      "patch on a consolidated resource is rejected",
			genRoutes: true,
			mutate: func(res *resourceInfo) {
				res.IsConsolidated = true
				res.SuppressedHandlers = []HandlerType{ListHandler, ReadHandler}
				res.ManualAddResourceSets = []HandlerType{PatchHandler}
			},
			wantErr: true,
		},
		{
			name:      "patch on a routing-disabled consolidated resource is still rejected",
			genRoutes: true,
			mutate: func(res *resourceInfo) {
				res.IsConsolidated = true
				res.SuppressedRoutes = []RouteType{AllRoutes}
				res.ManualAddResourceSets = []HandlerType{PatchHandler}
			},
			wantErr: true,
		},
		{
			name:      "without GenerateRoutes nothing conflicts",
			genRoutes: false,
			mutate: func(res *resourceInfo) {
				res.ManualAddResourceSets = []HandlerType{ListHandler, ReadHandler, PatchHandler}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := &resourceGenerator{client: &client{}}
			r.genRoutes = tt.genRoutes
			r.resources = []*resourceInfo{fixtureResource(t, structs, "Widget", tt.mutate)}

			if err := r.validateManualAddResourceSets(); (err != nil) != tt.wantErr {
				t.Errorf("validateManualAddResourceSets() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// Test_manualAddResourceSet_annotationResolution pins the end-to-end path: the fixture
// struct's @suppress, @manualAddResourceSet, and @permissionScope doc comments scan and
// resolve into the resourceInfo the static computation consumes.
func Test_manualAddResourceSet_annotationResolution(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadCollectionFixture(t))

	tests := []struct {
		name           string
		structName     string
		wantManualSets []HandlerType
		wantSuppressed []HandlerType
		wantScope      accesstypes.PermissionScope // empty defaults to global
	}{
		{
			name:           "declared Sets with the default scope",
			structName:     "Ledger",
			wantManualSets: []HandlerType{ListHandler, ReadHandler},
			wantSuppressed: []HandlerType{ListHandler, ReadHandler, PatchHandler},
		},
		{
			name:           "declared Set with an explicit domain scope",
			structName:     "Vault",
			wantManualSets: []HandlerType{ListHandler},
			wantSuppressed: []HandlerType{ListHandler, ReadHandler, PatchHandler},
			wantScope:      accesstypes.DomainPermissionScope,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := structs[tt.structName]
			if s == nil {
				t.Fatalf("struct %q not found in fixture package", tt.structName)
			}

			annotations, err := genlang.NewScanner(resourceKeywords()).ScanStruct(s)
			if err != nil {
				t.Fatalf("ScanStruct() error = %v", err)
			}

			res := &resourceInfo{TypeInfo: s.TypeInfo}
			if err := resolveResourceAnnotations(res, annotations); err != nil {
				t.Fatalf("resolveResourceAnnotations() error = %v", err)
			}

			if diff := cmp.Diff(tt.wantManualSets, res.ManualAddResourceSets); diff != "" {
				t.Errorf("ManualAddResourceSets mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.wantSuppressed, res.SuppressedHandlers); diff != "" {
				t.Errorf("SuppressedHandlers mismatch (-want +got):\n%s", diff)
			}
			if res.PermissionScope != tt.wantScope {
				t.Errorf("PermissionScope = %q, want %q", res.PermissionScope, tt.wantScope)
			}
		})
	}
}

// Test_parsePermissionScopeAnnotation pins @permissionScope argument validation.
func Test_parsePermissionScopeAnnotation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		arg     string
		want    accesstypes.PermissionScope
		wantErr bool
	}{
		{arg: "global", want: accesstypes.GlobalPermissionScope},
		{arg: "domain", want: accesstypes.DomainPermissionScope},
		{arg: " domain ", want: accesstypes.DomainPermissionScope},
		{arg: "tenant", wantErr: true},
		{arg: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.arg, func(t *testing.T) {
			t.Parallel()

			got, err := parsePermissionScopeAnnotation(genlang.Arg(tt.arg))
			if (err != nil) != tt.wantErr {
				t.Fatalf("parsePermissionScopeAnnotation(%q) error = %v, wantErr %v", tt.arg, err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("parsePermissionScopeAnnotation(%q) = %q, want %q", tt.arg, got, tt.want)
			}
		})
	}
}
