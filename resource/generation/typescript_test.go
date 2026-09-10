package generation

import (
	"strings"
	"testing"
)

// Test_typescriptResourcesTemplate_optionalFields pins the cell-masking wire contract:
// resource interfaces emit non-key fields optional (absent = masked; explicit null =
// genuine NULL) while primary keys stay required — a row whose key is masked must not
// appear at all — and the metadata's required property carries required-ness.
func Test_typescriptResourcesTemplate_optionalFields(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadCollectionFixture(t))

	widget := fixtureResource(t, structs, "Widget", func(res *resourceInfo) {
		for _, f := range res.Fields {
			f.typescriptType = "string"
		}
	})
	gadget := fixtureComputedResource(t, structs, "Gadget")
	for _, f := range gadget.Fields {
		f.typescriptType = "string"
	}

	tests := []struct {
		name            string
		data            tsResourcesData
		wantContains    []string
		wantNotContains []string
	}{
		{
			name: "resource interface fields are optional except primary keys",
			data: tsResourcesData{
				Resources: []*resourceInfo{widget},
				GenPrefix: "zz_gen",
			},
			wantContains: []string{
				"export interface Widgets {",
				"  id: string;",
				"  name?: string;",
				"{ fieldName: 'name', displayType: 'string', required: true, isIndex: false }",
				"{ fieldName: 'id', primaryKey: { ordinalPosition: 0 }, displayType: 'string', required: false, isIndex: false }",
			},
			wantNotContains: []string{
				"  id?: string;",
				"  name: string;",
			},
		},
		{
			name: "computed resource interface fields are optional except primary keys",
			data: tsResourcesData{
				ComputedResources: []*computedResource{gadget},
				GenPrefix:         "zz_gen",
			},
			wantContains: []string{
				"export interface Gadgets {",
				"  id: string;",
				"  name?: string;",
				"{ fieldName: 'name', displayType: 'string', required: false, isIndex: false }",
			},
			wantNotContains: []string{
				"  id?: string;",
				"  name: string;",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c := &client{}
			tt.data.File = &typescriptGenerator{client: c}
			out, err := c.generateTemplateOutput("typescriptResourcesTemplate", typescriptResourcesTemplate, tt.data)
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
