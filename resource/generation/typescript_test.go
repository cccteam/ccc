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

// Test_typescriptResourcesTemplate_bytes pins how a byte slice renders: the interface
// declares the field a string, since the wire carries base64, and the metadata carries
// the bytes display type, on a table field and on a computed field alike.
func Test_typescriptResourcesTemplate_bytes(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadCollectionFixture(t))

	widget := fixtureResource(t, structs, "Widget", func(res *resourceInfo) {
		for _, f := range res.Fields {
			f.typescriptType = "string"
			if f.Name() == "Name" {
				f.typescriptType = bytesTSType
			}
		}
	})
	gadget := fixtureComputedResource(t, structs, "Gadget")
	for _, f := range gadget.Fields {
		f.typescriptType = "string"
		if f.Name() == "Name" {
			f.typescriptType = bytesTSType
		}
	}

	tests := []struct {
		name         string
		data         tsResourcesData
		wantContains []string
	}{
		{
			name: "a table field of bytes is a string with the bytes display type",
			data: tsResourcesData{Resources: []*resourceInfo{widget}, GenPrefix: "zz_gen"},
			wantContains: []string{
				"  name?: string;",
				"{ fieldName: 'name', displayType: 'bytes', required: true, isIndex: false }",
			},
		},
		{
			name: "a computed field of bytes is a string with the bytes display type",
			data: tsResourcesData{ComputedResources: []*computedResource{gadget}, GenPrefix: "zz_gen"},
			wantContains: []string{
				"  name?: string;",
				"{ fieldName: 'name', displayType: 'bytes', required: false, isIndex: false }",
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
		})
	}
}

// Test_typescriptResourcesTemplate_arrays pins how a list renders on the two resource
// paths: the interface carries the element's interface type with [], and the metadata
// the leaf's display name lower-cased with [], a member of the vocabulary the client's
// union lists; and that a display type outside the vocabulary fails generation naming
// it, so the generator never emits what the client would refuse.
func Test_typescriptResourcesTemplate_arrays(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadCollectionFixture(t))
	typed := func(table string, computed string) (*resourceInfo, *computedResource) {
		widget := fixtureResource(t, structs, "Widget", func(res *resourceInfo) {
			for _, f := range res.Fields {
				f.typescriptType = "string"
				if f.Name() == "Name" {
					f.typescriptType = table
				}
			}
		})
		gadget := fixtureComputedResource(t, structs, "Gadget")
		for _, f := range gadget.Fields {
			f.typescriptType = "string"
			if f.Name() == "Name" {
				f.typescriptType = computed
			}
		}

		return widget, gadget
	}

	tests := []struct {
		name         string
		table        string
		computed     string
		wantContains []string
		wantErr      string
	}{
		{
			name:     "a list of numbers and a list of civil dates render their members",
			table:    numberTSType + sliceSuffix,
			computed: civilDateTSType + sliceSuffix,
			wantContains: []string{
				"  name?: number[];",
				"{ fieldName: 'name', displayType: 'number[]', required: true, isIndex: false }",
				"  name?: Date[];",
				"{ fieldName: 'name', displayType: 'civildate[]', required: false, isIndex: false }",
			},
		},
		{
			name:     "a list of UUIDs and a list of timestamps render their members",
			table:    uuidTSType + sliceSuffix,
			computed: dateTSType + sliceSuffix,
			wantContains: []string{
				"  name?: string[];",
				"{ fieldName: 'name', displayType: 'uuid[]', required: true, isIndex: false }",
				"  name?: Date[];",
				"{ fieldName: 'name', displayType: 'date[]', required: false, isIndex: false }",
			},
		},
		{
			name:     "a display type outside the vocabulary fails generation naming it",
			table:    "link",
			computed: "string",
			wantErr:  `"link" is not a display type`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			widget, gadget := typed(tt.table, tt.computed)
			c := &client{}
			out, err := c.generateTemplateOutput("typescriptResourcesTemplate", typescriptResourcesTemplate, tsResourcesData{
				File:              &typescriptGenerator{client: c},
				Resources:         []*resourceInfo{widget},
				ComputedResources: []*computedResource{gadget},
				GenPrefix:         "zz_gen",
			})
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("generateTemplateOutput() error = %v, want it to contain %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("generateTemplateOutput() error = %v", err)
			}
			for _, want := range tt.wantContains {
				if !strings.Contains(string(out), want) {
					t.Errorf("typescriptResourcesTemplate output missing %q:\n%s", want, out)
				}
			}
		})
	}
}

// Test_typescriptMethodsTemplate_arrays pins the RPC path's rendering of a list: the
// leaf's display name lower-cased with [], the same member the resource paths emit.
func Test_typescriptMethodsTemplate_arrays(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadCollectionFixture(t))

	tests := []struct {
		name     string
		leaf     string
		wantMeta string
	}{
		{name: "a list of UUIDs", leaf: uuidTSType + sliceSuffix, wantMeta: "{ fieldName: 'input', displayType: 'uuid[]' }"},
		{name: "a list of civil dates", leaf: civilDateTSType + sliceSuffix, wantMeta: "{ fieldName: 'input', displayType: 'civildate[]' }"},
		{name: "a list of byte slices", leaf: bytesTSType + sliceSuffix, wantMeta: "{ fieldName: 'input', displayType: 'bytes[]' }"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			field := &rpcField{Field: structs["DoSomething"].Fields()[0], typescriptType: tt.leaf}
			method := &rpcMethodInfo{Struct: structs["DoSomething"], Fields: []*rpcField{field}}
			c := &client{rpcMethods: []*rpcMethodInfo{method}}
			out, err := c.generateTemplateOutput("typescriptMethodsTemplate", typescriptMethodsTemplate, tsMethodsData{
				File:       &typescriptGenerator{client: c},
				RPCMethods: c.rpcMethods,
				GenPrefix:  "zz_gen",
			})
			if err != nil {
				t.Fatalf("generateTemplateOutput() error = %v", err)
			}
			if !strings.Contains(string(out), tt.wantMeta) {
				t.Errorf("typescriptMethodsTemplate output missing %q:\n%s", tt.wantMeta, out)
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
			name: "a positional field says so in its metadata, a concealing one says nothing",
			data: func() tsResourcesData {
				beacon := fixtureResource(t, collection, "Beacon", func(res *resourceInfo) {
					stringTyped(res.Fields)
				})

				return tsResourcesData{Resources: []*resourceInfo{beacon}, GenPrefix: "zz_gen"}
			},
			wantContains: []string{
				"{ fieldName: 'deadline', displayType: 'string', required: true, isIndex: false, masking: 'positional' }",
				"{ fieldName: 'name', displayType: 'string', required: true, isIndex: false }",
			},
			wantNotContains: []string{"masking: 'concealing'"},
		},
		{
			name: "a sized string field carries its character limit, an unbounded one nothing",
			data: func() tsResourcesData {
				widget := fixtureResource(t, collection, "Widget", func(res *resourceInfo) {
					stringTyped(res.Fields)
					for _, f := range res.Fields {
						switch f.Name() {
						case "Name":
							f.SpannerType = "STRING(64)"
						case "ListedName":
							f.SpannerType = "STRING(MAX)"
						case "Code":
							f.SpannerType = "STRING(16)"
						}
					}
				})

				return tsResourcesData{Resources: []*resourceInfo{widget}, GenPrefix: "zz_gen"}
			},
			wantContains: []string{
				"{ fieldName: 'name', displayType: 'string', required: true, isIndex: false, maxLength: 64 }",
				"{ fieldName: 'listedName', displayType: 'string', required: true, isIndex: false }",
				"{ fieldName: 'code', displayType: 'string', required: true, isIndex: false, maxLength: 16 }",
			},
			wantNotContains: []string{"maxLength: 0"},
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
