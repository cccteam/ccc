package generation

import (
	"strings"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
)

// Test_apiClientData pins how the generator derives the client surface from a
// resource: the write shapes only the generator can know (server-owned and immutable
// fields absent, a generated key absent), the key tuple, and the operations the
// server actually generated.
func Test_apiClientData(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadCollectionFixture(t))
	typed := func(res *resourceInfo) {
		for _, f := range res.Fields {
			f.typescriptType = "string"
		}
	}

	tests := []struct {
		name            string
		generator       func() *typescriptGenerator
		wantContains    []string
		wantNotContains []string
	}{
		{
			name: "consolidated global resource: full handle, write shapes drop server-owned and immutable fields",
			generator: func() *typescriptGenerator {
				widget := fixtureResource(t, structs, "Widget", func(res *resourceInfo) {
					typed(res)
					res.IsConsolidated = true
				})
				return &typescriptGenerator{client: &client{resources: []*resourceInfo{widget}}}
			},
			wantContains: []string{
				"export interface WidgetsCreate {",
				"  name: string;",
				"  code: string;",
				"  secret: string;",
				"export interface WidgetsPatch {",
				"  name?: string;",
				"  secret?: string;",
				"export type WidgetsKey = [id: string];",
				"property: 'widgets',",
				"route: 'widgets',",
				"scope: 'global',",
				"consolidated: true,",
				"keys: ['id'],",
				"operations: ['list', 'read', 'create', 'patch', 'remove', 'batch'],",
				// An undeclared page is the generator-wide default with no maximum.
				"page: { default: 50 },",
				// The patchable list mirrors the Patch interface exactly: keys,
				// server-owned, and immutable fields absent.
				"patchable: ['name', 'listedName', 'secret'],",
				"  widgets: ResourceHandle<Widgets, WidgetsKey, 'list' | 'read' | 'create' | 'patch' | 'remove' | 'batch', WidgetsCreate, WidgetsPatch>;",
				"consolidatedRoute: 'resources',",
			},
			wantNotContains: []string{
				"  id: string;",    // a generated uuid key is never client-supplied
				"  id?: string;",   // ...nor patchable
				"  derived",        // output-only never crosses the wire inbound
				"  code?: string;", // immutable: creatable, never patchable
				"domainRoute:",     // no domain-scoped targets
				"MethodHandle",     // no methods
				"import { Methods", // ...so no method constants
				"order:",           // no declared order: the list is in primary-key order
			},
		},
		{
			name: "a declared order reaches the descriptor as JSON names and directions",
			generator: func() *typescriptGenerator {
				widget := fixtureResource(t, structs, "Widget", func(res *resourceInfo) {
					typed(res)
					res.DeclaredOrder = []resource.SortField{
						{Field: "ListedName", Direction: resource.SortDescending},
						{Field: "Name", Direction: resource.SortAscending},
					}
				})
				return &typescriptGenerator{client: &client{resources: []*resourceInfo{widget}}}
			},
			wantContains: []string{
				"page: { default: 50 },\n      order: [{ field: 'listedName', direction: 'desc' }, { field: 'name', direction: 'asc' }],",
			},
		},
		{
			name: "domain-scoped resource and method land on DomainApi; a suppressed method is absent",
			generator: func() *typescriptGenerator {
				gadget := fixtureResource(t, structs, "Gadget", func(res *resourceInfo) {
					typed(res)
					res.PermissionScope = accesstypes.DomainPermissionScope
				})
				return &typescriptGenerator{
					client: &client{
						resources: []*resourceInfo{gadget},
						rpcMethods: []*rpcMethodInfo{
							{Struct: structs["DoSomething"], PermissionScope: accesstypes.DomainPermissionScope},
							{Struct: structs["HiddenMethod"], SuppressHandler: true},
						},
					},
					domainRouteSegment: "stations",
					domainRouteParam:   "stationID",
				}
			},
			wantContains: []string{
				"domainRoute: { segment: 'stations', param: 'stationID' },",
				"[Methods.DoSomething]: { method: Methods.DoSomething, property: 'doSomething', route: 'do-something', scope: 'domain' },",
				"export interface DomainApi {\n  gadgets: ResourceHandle<Gadgets, GadgetsKey, 'list' | 'read' | 'create' | 'patch' | 'remove', GadgetsCreate, GadgetsPatch>;\n  doSomething: MethodHandle<DoSomething>;\n}",
				"export interface GlobalApi {\n}",
				"import { Methods, Resources } from './zz_gen_constants';",
				"import { DoSomething } from './zz_gen_methods';",
			},
			wantNotContains: []string{
				"HiddenMethod",
				"consolidatedRoute:",
			},
		},
		{
			name: "computed resource: list and read only, no write shapes",
			generator: func() *typescriptGenerator {
				summary := fixtureComputedResource(t, structs, "Summary")
				for _, f := range summary.Fields {
					f.typescriptType = "string"
				}
				summary.PageDefault, summary.PageMax = 25, 200
				return &typescriptGenerator{client: &client{computedResources: []*computedResource{summary}}}
			},
			wantContains: []string{
				"export type SummariesKey = [id: string];",
				"operations: ['list', 'read'],",
				// A declared @page reaches the client: the default and the maximum,
				// which also tells it limit=all is refused.
				"page: { default: 25, max: 200 },",
				"  summaries: ResourceHandle<Summaries, SummariesKey, 'list' | 'read'>;",
			},
			wantNotContains: []string{
				"SummariesCreate",
				"SummariesPatch",
				"patchable:",
			},
		},
		{
			name: "handlers the server did not generate are absent from the handle type",
			generator: func() *typescriptGenerator {
				relic := fixtureResource(t, structs, "Relic", func(res *resourceInfo) {
					typed(res)
					res.SuppressedHandlers = []HandlerType{PatchHandler}
				})
				return &typescriptGenerator{client: &client{resources: []*resourceInfo{relic}}}
			},
			wantContains: []string{
				"operations: ['list', 'read'],",
				"  relics: ResourceHandle<Relics, RelicsKey, 'list' | 'read'>;",
			},
			wantNotContains: []string{
				"RelicsCreate",
				"RelicsPatch",
				// String-typed shapes import no value types.
				"NullBoolean",
				"from 'money-types'",
			},
		},
		{
			name: "a NullBoolean field in a write shape imports the value type, and a declared type its module",
			generator: func() *typescriptGenerator {
				widget := fixtureResource(t, structs, "Widget", func(res *resourceInfo) {
					typed(res)
					for _, f := range res.Fields {
						switch f.Name() {
						case "Secret":
							f.typescriptType = booleanStr
							f.IsNullable = true
						case "Code":
							f.setColumnType("Money", objectDisplayType, &tsImport{Name: "Money", From: "money-types"}, false)
						case "Name":
							f.setColumnType("Price", objectDisplayType, &tsImport{Name: "Price", From: "money-types"}, true)
						case "ListedName":
							f.setColumnType("Point", objectDisplayType, &tsImport{Name: "Point", From: "geojson"}, false)
						}
					}
				})
				return &typescriptGenerator{client: &client{resources: []*resourceInfo{widget}}}
			},
			wantContains: []string{
				"import { ApiDescriptor, Client, ClientOptions, createClient, NullBoolean, ResourceHandle } from '@cccteam/resource';\nimport { Point } from 'geojson';\nimport { Money, Price } from 'money-types';\n",
				"  secret?: NullBoolean;",
				"  code: Money;",
				"  name: Price[];",
				"  listedName: Point;",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			g := tt.generator()
			g.ConsolidatedRoute = "resources"
			out, err := g.generateTemplateOutput("typescriptAPITemplate", typescriptAPITemplate, g.apiClientData())
			if err != nil {
				t.Fatalf("generateTemplateOutput() error = %v", err)
			}

			for _, want := range tt.wantContains {
				if !strings.Contains(string(out), want) {
					t.Errorf("typescriptAPITemplate output missing %q:\n%s", want, out)
				}
			}
			for _, notWant := range tt.wantNotContains {
				if strings.Contains(string(out), notWant) {
					t.Errorf("typescriptAPITemplate output must not contain %q:\n%s", notWant, out)
				}
			}
		})
	}
}
