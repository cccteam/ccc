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

// Test_typescriptResourcesTemplate_filterable pins FieldMeta.filterable: the metadata
// marks a column the server will filter by the eligibility the query decoder applies —
// an indexed or unique-indexed table or view field 'always', an allow_filter table or
// view field 'withIndexed' (the database parse wants an indexed field in the same
// filter), a computed resource's allow_filter field 'always' (its List function filters
// in memory) — and marks nothing on any other field.
func Test_typescriptResourcesTemplate_filterable(t *testing.T) {
	t.Parallel()

	stringTyped := func(fields []*resourceField) {
		for _, f := range fields {
			f.typescriptType = "string"
		}
	}
	collection := fixtureStructs(loadCollectionFixture(t))
	wire := fixtureStructs(loadFixture(t, "wirefixture"))
	paging := fixtureStructs(loadFixture(t, "pagingfixture"))

	tests := []struct {
		name            string
		data            func() tsResourcesData
		wantContains    []string
		wantNotContains []string
	}{
		{
			name: "an indexed table field is always filterable, a unique index too, a plain field is not",
			data: func() tsResourcesData {
				widget := fixtureResource(t, collection, "Widget", func(res *resourceInfo) {
					stringTyped(res.Fields)
					for _, f := range res.Fields {
						switch f.Name() {
						case "Name":
							f.IsIndex = true
						case "Code":
							f.IsUniqueIndex = true
						}
					}
				})

				return tsResourcesData{Resources: []*resourceInfo{widget}, GenPrefix: "zz_gen"}
			},
			wantContains: []string{
				"{ fieldName: 'name', displayType: 'string', required: true, isIndex: true, filterable: 'always' }",
				"{ fieldName: 'code', displayType: 'string', required: true, isIndex: false, filterable: 'always' }",
				"{ fieldName: 'listedName', displayType: 'string', required: true, isIndex: false }",
			},
			wantNotContains: []string{"filterable: 'withIndexed'"},
		},
		{
			name: "an allow_filter table field is filterable with an indexed companion",
			data: func() tsResourcesData {
				board := fixtureResource(t, wire, "Board", func(res *resourceInfo) {
					stringTyped(res.Fields)
				})

				return tsResourcesData{Resources: []*resourceInfo{board}, GenPrefix: "zz_gen"}
			},
			wantContains: []string{
				"{ fieldName: 'name', displayType: 'string', required: true, isIndex: false, filterable: 'withIndexed' }",
			},
			wantNotContains: []string{"filterable: 'always'"},
		},
		{
			name: "a computed resource's allow_filter field stands alone, its other fields carry nothing",
			data: func() tsResourcesData {
				board := fixtureComputedResource(t, paging, "Board")
				for _, f := range board.Fields {
					f.typescriptType = "string"
				}

				return tsResourcesData{ComputedResources: []*computedResource{board}, GenPrefix: "zz_gen"}
			},
			wantContains: []string{
				"{ fieldName: 'shipName', displayType: 'string', required: false, isIndex: false, filterable: 'always' }",
				"{ fieldName: 'subsystem', displayType: 'string', required: false, isIndex: false }",
			},
			wantNotContains: []string{"filterable: 'withIndexed'"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c := &client{}
			data := tt.data()
			data.File = &typescriptGenerator{client: c}
			out, err := c.generateTemplateOutput("typescriptResourcesTemplate", typescriptResourcesTemplate, data)
			if err != nil {
				t.Fatalf("generateTemplateOutput() error = %v", err)
			}
			for _, want := range tt.wantContains {
				if !strings.Contains(string(out), want) {
					t.Errorf("output does not contain %q:\n%s", want, out)
				}
			}
			for _, unwanted := range tt.wantNotContains {
				if strings.Contains(string(out), unwanted) {
					t.Errorf("output contains %q:\n%s", unwanted, out)
				}
			}
		})
	}
}
