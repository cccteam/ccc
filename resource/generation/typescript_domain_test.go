package generation

import (
	"strings"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
)

// Test_typescriptResourcesTemplate_domain pins the TypeScript route/metadata awareness
// of the domain segment: domain-scoped resources render their route pair prefix, every
// resource gets a ResourceScopes entry, and DomainRouteParam is exported exactly when
// something is domain-scoped.
func Test_typescriptResourcesTemplate_domain(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadCollectionFixture(t))

	tests := []struct {
		name            string
		scope           accesstypes.PermissionScope
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
				"[Resources.Widgets]: PermissionScopes.domain,",
				"export const ResourceScopes: Record<Resource, PermissionScope> = {",
				"import { PermissionScope, PermissionScopes, Resources } from './zz_gen_constants';",
			},
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
				"stations/{stationID}",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			res := fixtureResource(t, structs, "Widget", func(res *resourceInfo) {
				res.PermissionScope = tt.scope
			})

			c := &client{}
			out, err := c.generateTemplateOutput("typescriptResourcesTemplate", typescriptResourcesTemplate, tsResourcesData{
				File:              &typescriptGenerator{client: c},
				Resources:         []*resourceInfo{res},
				GenPrefix:         "zz_gen",
				DomainRoutePrefix: "stations/{stationID}",
				DomainRouteParam:  "stationID",
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
