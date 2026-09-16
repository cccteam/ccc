package generation

import (
	"testing"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
	"golang.org/x/tools/go/packages"
)

// limitFixtureTables is the synthetic schema behind the limitfixture struct: every
// column's declared type as the information schema spells it, with the nullability the
// struct's field types declare.
func limitFixtureTables() map[string]*tableMetadata {
	column := func(spannerType string, nullable bool) columnMeta {
		return columnMeta{SpannerType: spannerType, IsNullable: nullable}
	}

	tables := map[string]*tableMetadata{
		"Parcels": {PkCount: 1, Columns: map[string]columnMeta{
			"Id":       {IsPrimaryKey: true, SpannerType: "STRING(36)"},
			"Label":    column("STRING(64)", false),
			"Notes":    column("STRING(MAX)", true),
			"Alias":    column("STRING(8)", true),
			"Kind":     column("STRING(3)", false),
			"Seal":     column("BYTES(4)", false),
			"Manifest": column("BYTES(MAX)", false),
			"Tags":     column("ARRAY<STRING(4)>", false),
			"Weight":   column("NUMERIC", false),
			"Rebate":   column("NUMERIC", true),
			"Fees":     column("ARRAY<NUMERIC>", false),
			"Count":    column("INT64", false),
			"OwnerId":  column("STRING(36)", false),
			"Serial":   column("STRING(16)", false),
			"Stamp":    column("STRING(10)", false),
			"Stickers": column("ARRAY<STRING(5)>", false),
			"Grades":   column("ARRAY<STRING(3)>", false),
			"Levies":   column("ARRAY<NUMERIC>", false),
			"Hash":     column("BYTES(6)", false),
			"Awards":   column("ARRAY<STRING(7)>", false),
			"Nick":     column("STRING(12)", false),
			// A string key into another resource: the picker's key is a string the
			// column sizes.
			"CategoryId": {IsForeignKey: true, ReferencedTable: "Categories", ReferencedColumn: "Id", SpannerType: "STRING(36)"},
		}, Indexes: []indexMeta{{Name: "PRIMARY_KEY", PrimaryKey: true, Unique: true, Key: []indexColumn{{Column: "Id"}}}}},
	}
	for _, table := range tables {
		table.deriveIndexFlags()
	}

	return tables
}

// Test_sqltypeTag pins where the patch request structs carry the column's type and
// what the TypeScript metadata makes of it: the tag exactly on the pairs the decoder
// sizes, whatever the Go type is named and whatever TypeScript type it declares; the
// character limit only behind a string-kinded field whose display type is string,
// string[], or enumerated; and the rendered fragment parsing back through the
// runtime's tag reader. The TypeScript types are resolved first, as the metadata
// template has them: maxLength reads the field's display type.
func Test_sqltypeTag(t *testing.T) {
	t.Parallel()

	loaded, pkg := loadFixturePackage(t, "limitfixture")
	c := &client{tableMap: limitFixtureTables()}
	c.notePackages(map[string]*packages.Package{loaded.Name: loaded})
	resources, err := c.structsToResources(pkg.Structs)
	if err != nil {
		t.Fatalf("structsToResources() error = %v", err)
	}
	var parcel *resourceInfo
	for _, res := range resources {
		if res.Name() == "Parcel" {
			parcel = res
		}
	}
	if parcel == nil {
		t.Fatal("Parcel not extracted")
	}
	// The key's target is a routed resource, so the key renders as a picker.
	generator := &typescriptGenerator{client: c, outletExcludedTables: map[string]struct{}{}, routerResources: []accesstypes.Resource{"Categories"}}
	if err := generator.resourceFieldsTypescriptType(parcel); err != nil {
		t.Fatalf("resourceFieldsTypescriptType() error = %v", err)
	}

	tests := []struct {
		name            string
		field           string
		wantDisplayType string
		wantTag         string
		wantMaxLength   int
	}{
		{name: "a string on STRING(n) carries the column type and its character limit", field: "Label", wantDisplayType: "string", wantTag: `sqltype:"STRING(64)"`, wantMaxLength: 64},
		{name: "a nullable string on STRING(MAX) carries nothing", field: "Notes", wantDisplayType: "string"},
		{name: "a NullString on STRING(n) carries both", field: "Alias", wantDisplayType: "string", wantTag: `sqltype:"STRING(8)"`, wantMaxLength: 8},
		{name: "a named string type is string-kinded", field: "Kind", wantDisplayType: "string", wantTag: `sqltype:"STRING(3)"`, wantMaxLength: 3},
		{name: "a byte slice on BYTES(n) carries the tag and no character limit", field: "Seal", wantDisplayType: "bytes", wantTag: `sqltype:"BYTES(4)"`},
		{name: "a byte slice on BYTES(MAX) carries nothing", field: "Manifest", wantDisplayType: "bytes"},
		{name: "a string slice on ARRAY<STRING(n)> carries the tag and the element limit", field: "Tags", wantDisplayType: "string[]", wantTag: `sqltype:"ARRAY<STRING(4)>"`, wantMaxLength: 4},
		{name: "a decimal on NUMERIC carries the tag and no character limit", field: "Weight", wantDisplayType: "number", wantTag: `sqltype:"NUMERIC"`},
		{name: "a NullDecimal on NUMERIC carries the tag", field: "Rebate", wantDisplayType: "number", wantTag: `sqltype:"NUMERIC"`},
		{name: "a decimal slice on ARRAY<NUMERIC> carries the tag", field: "Fees", wantDisplayType: "number[]", wantTag: `sqltype:"ARRAY<NUMERIC>"`},
		{name: "an integer carries nothing", field: "Count", wantDisplayType: "number"},
		{name: "a UUID on STRING(36) carries nothing", field: "OwnerID", wantDisplayType: "uuid"},
		{name: "the primary key carries nothing", field: "ID", wantDisplayType: "uuid"},
		{name: "an immutable sized string carries the tag beside immutable", field: "Serial", wantDisplayType: "string", wantTag: `sqltype:"STRING(16)"`, wantMaxLength: 16},
		{name: "an output-only field is hidden from the patch wire and carries nothing", field: "Stamp", wantDisplayType: "string"},
		{name: "a named slice of strings on ARRAY<STRING(n)> is sized like the unnamed slice", field: "Stickers", wantDisplayType: "string[]", wantTag: `sqltype:"ARRAY<STRING(5)>"`, wantMaxLength: 5},
		{name: "a named slice of a named string type is a slice of strings", field: "Grades", wantDisplayType: "string[]", wantTag: `sqltype:"ARRAY<STRING(3)>"`, wantMaxLength: 3},
		{name: "a named slice of decimals on ARRAY<NUMERIC> carries the tag and no character limit", field: "Levies", wantDisplayType: "number[]", wantTag: `sqltype:"ARRAY<NUMERIC>"`},
		{name: "a named byte slice on BYTES(n) is bytes, not a slice of anything", field: "Hash", wantDisplayType: "bytes", wantTag: `sqltype:"BYTES(6)"`},
		{name: "a declared type over a named slice carries the tag and no character limit", field: "Awards", wantDisplayType: "object", wantTag: `sqltype:"ARRAY<STRING(7)>"`},
		{name: "a declared type over a named string carries the tag and no character limit", field: "Nick", wantDisplayType: "object", wantTag: `sqltype:"STRING(12)"`},
		{name: "a string key rendered as a picker keeps its character limit", field: "CategoryID", wantDisplayType: "enumerated", wantTag: `sqltype:"STRING(36)"`, wantMaxLength: 36},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var field *resourceField
			for _, f := range parcel.Fields {
				if f.Name() == tt.field {
					field = f
				}
			}
			if field == nil {
				t.Fatalf("field %s not extracted", tt.field)
			}
			if got := field.TypescriptDisplayType(); got != tt.wantDisplayType {
				t.Errorf("TypescriptDisplayType() = %q, want %q", got, tt.wantDisplayType)
			}
			if got := field.SqltypeTag(); got != tt.wantTag {
				t.Errorf("SqltypeTag() = %q, want %q", got, tt.wantTag)
			}
			if got := field.TypescriptMaxLength(); got != tt.wantMaxLength {
				t.Errorf("TypescriptMaxLength() = %d, want %d", got, tt.wantMaxLength)
			}

			var wantSQLType string
			if tt.wantTag != "" {
				wantSQLType = field.SpannerType
			}
			ft := fieldTagsFromTemplateTags(field.Name(), field.JSONTagForPatch(), field.ImmutableTag(), field.SqltypeTag())
			if ft.SQLType != wantSQLType {
				t.Errorf("rendered tag parses to SQLType %q, want %q", ft.SQLType, wantSQLType)
			}
		})
	}

	if _, err := handlerSetData(parcel, PatchHandler); err != nil {
		t.Errorf("handlerSetData(PatchHandler) over the rendered tags error = %v", err)
	}
	if _, err := resource.NewSetData([]resource.FieldTags{{Field: "Label", JSON: "label", SQLType: "STRING(64)"}}, accesstypes.Create, accesstypes.Update, accesstypes.Delete); err != nil {
		t.Errorf("NewSetData() on the rendered tag error = %v", err)
	}
}
