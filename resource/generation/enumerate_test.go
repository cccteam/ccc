package generation

import (
	"slices"
	"strings"
	"testing"

	"github.com/cccteam/ccc/resource/generation/parser"
	"github.com/cccteam/ccc/resource/generation/parser/genlang"
)

// The field-scope @enumerate on resource fields: the declaration is captured at
// extraction and resolved once every kind is extracted, under four rules — a plain
// column names the picker's resource or an enumeration table, a foreign key may name
// a view keyed like its target but not the target itself, and no declaration may sit
// on a key into an enumeration table.

// Test_declareFieldEnumerations pins that extraction records the annotation on the
// field that carries it and no other.
func Test_declareFieldEnumerations(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadFixture(t, "hygienefixture"))
	pStruct := structs["EnumeratedField"]
	if pStruct == nil {
		t.Fatal("struct EnumeratedField not found in fixture package")
	}
	annotations, err := genlang.NewScanner(resourceKeywords()).ScanStruct(pStruct)
	if err != nil {
		t.Fatalf("ScanStruct() error = %v", err)
	}
	fields := make([]*resourceField, 0, len(pStruct.Fields()))
	for _, f := range pStruct.Fields() {
		fields = append(fields, &resourceField{Field: f})
	}
	if err := declareFieldEnumerations(pStruct, fields, annotations); err != nil {
		t.Fatalf("declareFieldEnumerations() error = %v", err)
	}

	tests := []struct {
		name  string
		field *resourceField
		want  string
	}{
		{name: "the annotated field carries the argument", field: fields[1], want: "Widgets"},
		{name: "an unannotated field carries none", field: fields[0]},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.field.HasDeclaredEnumeration(); got != (tt.want != "") {
				t.Fatalf("HasDeclaredEnumeration() = %v, want %v", got, tt.want != "")
			}
			if tt.want != "" && string(*tt.field.enumerateArg) != tt.want {
				t.Errorf("enumerateArg = %q, want %q", *tt.field.enumerateArg, tt.want)
			}
		})
	}
}

func Test_resolveFieldEnumerations(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadCollectionFixture(t))
	kinds := []*enumData{{ID: "gear", Description: "Gear"}, {ID: "lever", Description: "Lever"}}
	c := &client{
		resources: []*resourceInfo{
			fixtureResource(t, structs, "Gadget", nil),
			fixtureResource(t, structs, "Curio", func(res *resourceInfo) {
				res.IsVirtual = true
			}),
		},
		computedResources: []*computedResource{
			{Struct: structs["Summary"], Fields: []*computedField{{Field: structs["Summary"].Fields()[0], IsPrimaryKey: true}}},
		},
		enumerateTables: map[string]string{"WidgetKinds": "WidgetKind"},
		enumValues:      map[string][]*enumData{"WidgetKinds": kinds},
	}

	tests := []struct {
		name            string
		virtual         bool
		foreignKey      string // the referenced table when the field is a foreign key
		arg             genlang.Arg
		wantResource    string
		wantEnumeration string
		wantErr         string
	}{
		{name: "a plain column names the resource the picker lists", arg: "Gadgets", wantResource: "Gadgets"},
		{name: "a plain column names a computed resource", arg: "Summaries", wantResource: "Summaries"},
		{name: "a plain column naming an enumeration table renders inline", arg: "WidgetKinds", wantResource: "WidgetKinds", wantEnumeration: "WidgetKind"},
		{name: "a virtual field names the resource the picker lists", virtual: true, arg: "Curios", wantResource: "Curios"},
		{name: "a foreign key names a view keyed like its target", foreignKey: "Gadgets", arg: "Curios", wantResource: "Curios"},
		{name: "a foreign key naming its own target is refused as redundant", foreignKey: "Gadgets", arg: "Gadgets", wantErr: "struct Widget field Name: @enumerate(Gadgets) is redundant: the schema's foreign key already names Gadgets"},
		{name: "a declaration on a key into an enumeration table is a contradiction", foreignKey: "WidgetKinds", arg: "Curios", wantErr: "struct Widget field Name: @enumerate(Curios) contradicts the foreign key into WidgetKinds, an enumeration table (@enumerate on type WidgetKind)"},
		{name: "a foreign key naming an enumeration table is refused", foreignKey: "Gadgets", arg: "WidgetKinds", wantErr: "struct Widget field Name: @enumerate(WidgetKinds) names an enumeration table, but the foreign key constrains the column to the rows of Gadgets"},
		{name: "an unknown resource is refused with the field named", arg: "Gizmos", wantErr: `struct Widget field Name: @enumerate(Gizmos): resource "Gizmos" does not exist`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			arg := tt.arg
			res := fixtureResource(t, structs, "Widget", func(res *resourceInfo) {
				res.IsVirtual = tt.virtual
				res.Fields[1].enumerateArg = &arg
				res.Fields[1].IsForeignKey = tt.foreignKey != ""
				res.Fields[1].ReferencedResource = tt.foreignKey
			})

			err := c.resolveFieldEnumerations([]*resourceInfo{res}, nil)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("resolveFieldEnumerations() error = %v, want it to contain %q", err, tt.wantErr)
				}
				if res.Fields[1].IsEnumerated {
					t.Error("a refused declaration must not mark the field enumerated")
				}

				return
			}
			if err != nil {
				t.Fatalf("resolveFieldEnumerations() error = %v", err)
			}
			got := res.Fields[1]
			if !got.IsEnumerated || got.TypescriptDisplayType() != enumeratedDisplayType {
				t.Errorf("IsEnumerated = %v, TypescriptDisplayType() = %q, want an enumerated field", got.IsEnumerated, got.TypescriptDisplayType())
			}
			if got.EnumeratedResource() != tt.wantResource {
				t.Errorf("EnumeratedResource() = %q, want %q", got.EnumeratedResource(), tt.wantResource)
			}
			if got.Enumeration != tt.wantEnumeration {
				t.Errorf("Enumeration = %q, want %q", got.Enumeration, tt.wantEnumeration)
			}
			if tt.wantEnumeration != "" && !slices.Equal(got.EnumerationValues, kinds) {
				t.Errorf("EnumerationValues = %v, want the table's rows", got.EnumerationValues)
			}
		})
	}
}

// Test_resolveFieldEnumerations_computed pins the computed side: a declared leaf
// field names its picker's resource, and a refusal names the struct and the field.
func Test_resolveFieldEnumerations_computed(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadCollectionFixture(t))
	c := &client{resources: []*resourceInfo{fixtureResource(t, structs, "Gadget", nil)}}

	tests := []struct {
		name         string
		arg          genlang.Arg
		wantResource string
		wantErr      string
	}{
		{name: "a computed field names the resource the picker lists", arg: "Gadgets", wantResource: "Gadgets"},
		{name: "an unknown resource is refused with the field named", arg: "Gizmos", wantErr: `struct Summary field Total: @enumerate(Gizmos): resource "Gizmos" does not exist`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			arg := tt.arg
			res := fixtureComputedResource(t, structs, "Summary")
			res.Fields[1].enumerateArg = &arg
			res.Fields[1].typescriptType = "number"

			err := c.resolveFieldEnumerations(nil, []*computedResource{res})
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("resolveFieldEnumerations() error = %v, want it to contain %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("resolveFieldEnumerations() error = %v", err)
			}
			got := res.Fields[1]
			if !got.IsEnumerated || got.EnumeratedResource() != tt.wantResource {
				t.Errorf("IsEnumerated = %v, EnumeratedResource() = %q, want %q", got.IsEnumerated, got.EnumeratedResource(), tt.wantResource)
			}
			if got.TypescriptDisplayType() != enumeratedDisplayType {
				t.Errorf("TypescriptDisplayType() = %q, want %q", got.TypescriptDisplayType(), enumeratedDisplayType)
			}
		})
	}
}

// Test_computedFields_enumerate pins the capture on a computed struct: a leaf field's
// declaration is recorded for the later resolution, and a nested field, which is
// opaque, refuses one naming the field.
func Test_computedFields_enumerate(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadFixture(t, "pagingfixture"))

	tests := []struct {
		name       string
		structName string
		wantArg    string
		wantErr    string
	}{
		{name: "a leaf field's declaration is recorded", structName: "EnumeratedBoard", wantArg: "Boards"},
		{name: "a nested field refuses a declaration", structName: "EnumeratedNestedBoard", wantErr: "EnumeratedNestedBoard.Readings: a nested field is opaque and cannot carry a field-scope @enumerate"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c := &client{}
			resources, err := c.structsToCompResources([]*parser.Struct{structs[tt.structName]})
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("structsToCompResources() error = %v, want it to contain %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("structsToCompResources() error = %v", err)
			}
			got := resources[0].Fields[1]
			if got.enumerateArg == nil || string(*got.enumerateArg) != tt.wantArg {
				t.Fatalf("enumerateArg = %v, want %q", got.enumerateArg, tt.wantArg)
			}
			if got.IsEnumerated {
				t.Error("extraction records the declaration; resolution marks the field enumerated later")
			}
		})
	}
}

func Test_enumerationLiteral(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		values []*enumData
		want   string
	}{
		{name: "rows render as id and display objects", values: []*enumData{{ID: "gear", Description: "Gear"}, {ID: "lever", Description: `Lever "L"`}}, want: `[{ id: "gear", display: "Gear" }, { id: "lever", display: "Lever \"L\"" }]`},
		{name: "no rows render an empty list", want: "[]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := enumerationLiteral(tt.values); got != tt.want {
				t.Errorf("enumerationLiteral() = %s, want %s", got, tt.want)
			}
		})
	}
}
