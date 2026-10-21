package generation

import (
	"strings"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
)

// Test_typescriptResourcesTemplate_domain pins the TypeScript route/metadata awareness
// of the domain segment: domain-scoped resources render their route pair prefix, every
// resource gets a ResourceScopes entry, DomainRouteParam is exported exactly when
// something is domain-scoped, and a consolidated resource names its route and nothing
// more (the per-resource operation types are gone; the client carries its own).
func Test_typescriptResourcesTemplate_domain(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadCollectionFixture(t))

	tests := []struct {
		name            string
		scope           accesstypes.PermissionScope
		consolidated    bool
		hasDomainScoped bool
		wantContains    []string
		wantNotContains []string
	}{
		{
			name:            "domain-scoped resource renders the route pair prefix",
			scope:           accesstypes.DomainPermissionScope,
			hasDomainScoped: true,
			wantContains: []string{
				"route: 'stations/{stationID}/widgets',",
				"export const DomainRouteParam = 'stationID';",
				"export const DomainRoutePath = 'stations/:stationID';",
				"[Resources.Widgets]: PermissionScopes.domain,",
				"export const ResourceScopes: Record<Resource, PermissionScope> = {",
				"import { PermissionScope, PermissionScopes, Resources } from './zz_gen_constants';",
			},
			wantNotContains: []string{"consolidatedRoute:"},
		},
		{
			name:  "global resource renders a bare route and no domain param export",
			scope: "",
			wantContains: []string{
				"route: 'widgets',",
				"[Resources.Widgets]: PermissionScopes.global,",
			},
			wantNotContains: []string{
				"DomainRouteParam",
				"DomainRoutePath",
				"stations/{stationID}",
			},
		},
		{
			name:         "consolidated resource names its route and no operation type",
			scope:        "",
			consolidated: true,
			wantContains: []string{
				"route: 'widgets',",
				"consolidatedRoute: 'resources',",
			},
			wantNotContains: []string{
				"OperationType",
				"WidgetsOperation",
				"ConsolidatedOperation",
				"Partial<Widgets>",
			},
		},
		{
			name:            "consolidated domain-scoped resource names its route under the domain pair and no operation type",
			scope:           accesstypes.DomainPermissionScope,
			consolidated:    true,
			hasDomainScoped: true,
			wantContains: []string{
				"route: 'stations/{stationID}/widgets',",
				"consolidatedRoute: 'resources',",
			},
			wantNotContains: []string{
				"OperationType",
				"WidgetsOperation",
				"ConsolidatedOperation",
				"${string}",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			res := fixtureResource(t, structs, "Widget", func(res *resourceInfo) {
				res.PermissionScope = tt.scope
				res.IsConsolidated = tt.consolidated
				// The metadata renders only the display-type vocabulary, so every field
				// carries a type, as a run resolves before rendering.
				for _, f := range res.Fields {
					f.typescriptType = "string"
				}
			})

			c := &client{}
			out, err := c.generateTemplateOutput("typescriptResourcesTemplate", typescriptResourcesTemplate, tsResourcesData{
				File:              &typescriptGenerator{client: c},
				Resources:         []*resourceInfo{res},
				ConsolidatedRoute: "resources",
				GenPrefix:         "zz_gen",
				DomainRoutePrefix: "stations/{stationID}",
				DomainRouteParam:  "stationID",
				DomainRoutePath:   "stations/:stationID",
				HasDomainScoped:   tt.hasDomainScoped,
			})
			if err != nil {
				t.Fatalf("generateTemplateOutput() error = %v", err)
			}

			for _, want := range tt.wantContains {
				if !strings.Contains(string(out), want) {
					t.Errorf("typescriptResourcesTemplate output missing %q:\n%s", want, out)
				}
			}
			for _, notWant := range tt.wantNotContains {
				if strings.Contains(string(out), notWant) {
					t.Errorf("typescriptResourcesTemplate output must not contain %q:\n%s", notWant, out)
				}
			}
		})
	}
}

// Test_typescriptConstantsTemplate_permissionScopes pins the PermissionScopes const:
// scopes are locally typed (the mislabeled ccc-lib Domain import is gone) and rendered
// sorted.
func Test_typescriptConstantsTemplate_permissionScopes(t *testing.T) {
	t.Parallel()

	c := &client{}
	out, err := c.generateTemplateOutput("typescriptConstantsTemplate", typescriptConstantsTemplate, tsConstantsData{
		File: &typescriptGenerator{client: c},
		Data: &resource.TypescriptData{
			Permissions:      []accesstypes.Permission{accesstypes.List},
			PermissionScopes: []accesstypes.PermissionScope{accesstypes.DomainPermissionScope, accesstypes.GlobalPermissionScope},
		},
	})
	if err != nil {
		t.Fatalf("generateTemplateOutput() error = %v", err)
	}

	for _, want := range []string{
		"export type PermissionScope = 'global' | 'domain';",
		"domain: 'domain' as PermissionScope,",
		"global: 'global' as PermissionScope,",
	} {
		if !strings.Contains(string(out), want) {
			t.Errorf("typescriptConstantsTemplate output missing %q:\n%s", want, out)
		}
	}
	for _, notWant := range []string{"as Domain,", "export const Domains"} {
		if strings.Contains(string(out), notWant) {
			t.Errorf("typescriptConstantsTemplate output must not contain %q:\n%s", notWant, out)
		}
	}
}

// Test_domainRoutePath pins the tenant segment as the browser's router matches it: the
// API's pair with the parameter written for the router, for the derived names and for
// the defaults alike.
func Test_domainRoutePath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		segment string
		param   string
		want    string
	}{
		{name: "the names derived from a tenant record", segment: "stations", param: "stationID", want: "stations/:stationID"},
		{name: "the default names", segment: "domains", param: "domain", want: "domains/:domain"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := domainRoutePath(tt.segment, tt.param); got != tt.want {
				t.Errorf("domainRoutePath(%q, %q) = %q, want %q", tt.segment, tt.param, got, tt.want)
			}
		})
	}
}

// Test_anyDomainScoped pins when the metadata's domain route exports are emitted: for a
// resource under the tenant segment, a computed one, or a user surface's tenant partition,
// and not for a metadata that serves nothing under it.
func Test_anyDomainScoped(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadCollectionFixture(t))
	domain := fixtureResource(t, structs, "Widget", func(res *resourceInfo) { res.PermissionScope = accesstypes.DomainPermissionScope })
	global := fixtureResource(t, structs, "Widget", func(res *resourceInfo) { res.PermissionScope = "" })

	tests := []struct {
		name      string
		resources []*resourceInfo
		computed  []*computedResource
		users     []*tsUserResource
		want      bool
	}{
		{name: "nothing under the segment", resources: []*resourceInfo{global}, users: []*tsUserResource{{Scope: accesstypes.GlobalPermissionScope}}, want: false},
		{name: "a resource under the segment", resources: []*resourceInfo{global, domain}, want: true},
		{name: "a computed resource under the segment", computed: []*computedResource{{PermissionScope: accesstypes.DomainPermissionScope}}, want: true},
		{name: "a user surface's tenant partition", resources: []*resourceInfo{global}, users: []*tsUserResource{{Scope: accesstypes.GlobalPermissionScope}, {Scope: accesstypes.DomainPermissionScope}}, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := anyDomainScoped(tt.resources, tt.computed, tt.users); got != tt.want {
				t.Errorf("anyDomainScoped() = %v, want %v", got, tt.want)
			}
		})
	}
}
