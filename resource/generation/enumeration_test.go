package generation

import (
	"slices"
	"strings"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
)

// The enumeration rules: a struct backing an @enumerate table is read-only by
// derivation and refuses what only a mutable table needs, and a foreign key into an
// @enumerate table renders from the generated values before any resource rule.

func TestDeriveEnumerationResource(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		mutate     func(*resourceInfo)
		wantErr    string
		wantSuppr  []HandlerType
		wantConsol bool
	}{
		{
			name:      "a plain struct becomes read-only",
			mutate:    func(res *resourceInfo) { res.IsConsolidated = true },
			wantSuppr: []HandlerType{PatchHandler},
		},
		{
			name:      "an explicit @suppress(PatchHandler) is not doubled",
			mutate:    func(res *resourceInfo) { res.SuppressedHandlers = []HandlerType{PatchHandler} },
			wantSuppr: []HandlerType{PatchHandler},
		},
		{
			name:      "a suppressed list handler is kept alongside",
			mutate:    func(res *resourceInfo) { res.SuppressedHandlers = []HandlerType{ListHandler} },
			wantSuppr: []HandlerType{ListHandler, PatchHandler},
		},
		{
			name:    "create defaults contradict a read-only table",
			mutate:  func(res *resourceInfo) { res.DefaultsCreateType = "WidgetKindDefaults" },
			wantErr: "remove @" + defaultsCreateTypeKeyword,
		},
		{
			name: "every mutable-table declaration is named",
			mutate: func(res *resourceInfo) {
				res.ValidateUpdateType = "V"
				res.ManualAddResourceSets = []HandlerType{PatchHandler}
			},
			wantErr: "remove @" + validateUpdateTypeKeyword + ", @" + manualAddResourceSetKeyword + "(" + string(PatchHandler) + ")",
		},
	}

	structs := fixtureStructs(loadCollectionFixture(t))
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			res := fixtureResource(t, structs, "Widget", tt.mutate)
			err := deriveEnumerationResource(res, "WidgetKind")
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("deriveEnumerationResource() error = %v, want it to contain %q", err, tt.wantErr)
				}
				if res.IsEnumeration() {
					t.Error("a refused struct must not be marked as an enumeration")
				}

				return
			}
			if err != nil {
				t.Fatalf("deriveEnumerationResource() error = %v", err)
			}
			if !res.IsEnumeration() || res.EnumerationType != "WidgetKind" {
				t.Errorf("EnumerationType = %q, want WidgetKind", res.EnumerationType)
			}
			if !slices.Equal(res.SuppressedHandlers, tt.wantSuppr) {
				t.Errorf("SuppressedHandlers = %v, want %v", res.SuppressedHandlers, tt.wantSuppr)
			}
			if res.IsConsolidated != tt.wantConsol {
				t.Errorf("IsConsolidated = %v, want %v", res.IsConsolidated, tt.wantConsol)
			}
			if !res.CreateHandlerDisabled() || !res.UpdateHandlerDisabled() || !res.DeleteHandlerDisabled() {
				t.Error("every mutation handler must be disabled")
			}
		})
	}
}

func TestResourceFieldsTypescriptType_enumeration(t *testing.T) {
	t.Parallel()

	kinds := []*enumData{{ID: "gear", Description: "Gear"}, {ID: "lever", Description: "Lever"}}
	newGenerator := func(excluded bool) *typescriptGenerator {
		g := &typescriptGenerator{
			client: &client{
				enumerateTables: map[string]string{"WidgetKinds": "WidgetKind"},
				enumValues:      map[string][]*enumData{"WidgetKinds": kinds},
			},
			typescriptOverrides:  map[string]string{},
			routerResources:      []accesstypes.Resource{"WidgetKinds", "Suppliers"},
			outletExcludedTables: map[string]struct{}{},
		}
		if excluded {
			g.outletExcludedTables["WidgetKinds"] = struct{}{}
		}

		return g
	}

	tests := []struct {
		name            string
		referenced      string
		foreignKey      bool
		excluded        bool
		wantEnumerated  bool
		wantEnumeration string
	}{
		{name: "a key into an enumeration table renders from the values", referenced: "WidgetKinds", foreignKey: true, wantEnumerated: true, wantEnumeration: "WidgetKind"},
		{name: "the enumeration wins even though the table is also a resource", referenced: "WidgetKinds", foreignKey: true, wantEnumerated: true, wantEnumeration: "WidgetKind"},
		{name: "an enumeration table off the outlet falls back to the resource rule", referenced: "WidgetKinds", foreignKey: true, excluded: true, wantEnumerated: true},
		{name: "a key into an ordinary resource is resource-backed", referenced: "Suppliers", foreignKey: true, wantEnumerated: true},
		{name: "a key into a table that is neither stays a string", referenced: "Ledgers", foreignKey: true},
		{name: "a plain column is untouched", referenced: "WidgetKinds"},
	}

	structs := fixtureStructs(loadCollectionFixture(t))
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			res := fixtureResource(t, structs, "Widget", func(res *resourceInfo) {
				res.Fields[1].IsForeignKey = tt.foreignKey
				res.Fields[1].ReferencedResource = tt.referenced
			})
			fields := newGenerator(tt.excluded).resourceFieldsTypescriptType(res.Fields)
			got := fields[1]
			if got.IsEnumerated != tt.wantEnumerated {
				t.Errorf("IsEnumerated = %v, want %v", got.IsEnumerated, tt.wantEnumerated)
			}
			if got.Enumeration != tt.wantEnumeration {
				t.Errorf("Enumeration = %q, want %q", got.Enumeration, tt.wantEnumeration)
			}
			if tt.wantEnumeration != "" && !slices.Equal(got.EnumerationValues, kinds) {
				t.Errorf("EnumerationValues = %v, want the table's rows", got.EnumerationValues)
			}
			want := "string"
			if tt.wantEnumerated {
				want = "enumerated"
			}
			if got.TypescriptDisplayType() != want {
				t.Errorf("TypescriptDisplayType() = %q, want %q", got.TypescriptDisplayType(), want)
			}
		})
	}
}

func Test_typescriptResourcesTemplate_enumeration(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadCollectionFixture(t))
	keyed := fixtureResource(t, structs, "Widget", func(res *resourceInfo) {
		for _, f := range res.Fields {
			f.typescriptType = "string"
		}
		res.Fields[1].IsForeignKey = true
		res.Fields[1].IsEnumerated = true
		res.Fields[1].ReferencedResource = "WidgetKinds"
		res.Fields[1].Enumeration = "WidgetKind"
		res.Fields[1].EnumerationValues = []*enumData{{ID: "gear", Description: "Gear"}, {ID: "lever", Description: "Lever \"L\""}}
	})
	catalog := fixtureResource(t, structs, "Widget", func(res *resourceInfo) {
		for _, f := range res.Fields {
			f.typescriptType = "string"
		}
		res.EnumerationType = "WidgetKind"
		res.SuppressedHandlers = []HandlerType{PatchHandler}
	})

	tests := []struct {
		name            string
		data            tsResourcesData
		wantContains    []string
		wantNotContains []string
	}{
		{
			name: "a key into an enumeration carries the values inline",
			data: tsResourcesData{Resources: []*resourceInfo{keyed}, GenPrefix: "zz_gen"},
			wantContains: []string{
				`{ fieldName: 'name', displayType: 'enumerated', required: true, isIndex: false, enumeration: [{ id: "gear", display: "Gear" }, { id: "lever", display: "Lever \"L\"" }] }`,
			},
			wantNotContains: []string{"enumeratedResource: Resources.WidgetKinds"},
		},
		{
			name: "a struct backing an enumeration is read-only in every field",
			data: tsResourcesData{Resources: []*resourceInfo{catalog}, GenPrefix: "zz_gen"},
			wantContains: []string{
				"{ fieldName: 'name', displayType: 'string', required: true, isIndex: false, readOnly: true }",
				"createDisabled: true,",
				"updateDisabled: true,",
				"deleteDisabled: true,",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			g := &typescriptGenerator{client: &client{}}
			tt.data.File = g
			out, err := g.generateTemplateOutput("typescriptResourcesTemplate", typescriptResourcesTemplate, tt.data)
			if err != nil {
				t.Fatalf("generateTemplateOutput() error = %v", err)
			}
			for _, want := range tt.wantContains {
				if !strings.Contains(string(out), want) {
					t.Errorf("output lacks %q\n%s", want, out)
				}
			}
			for _, unwanted := range tt.wantNotContains {
				if strings.Contains(string(out), unwanted) {
					t.Errorf("output must not contain %q", unwanted)
				}
			}
		})
	}
}
