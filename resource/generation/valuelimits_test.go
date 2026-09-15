package generation

import (
	"testing"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
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
		}, Indexes: []indexMeta{{Name: "PRIMARY_KEY", PrimaryKey: true, Unique: true, Key: []indexColumn{{Column: "Id"}}}}},
	}
	for _, table := range tables {
		table.deriveIndexFlags()
	}

	return tables
}

// Test_sqltypeTag pins where the patch request structs carry the column's type and
// what the TypeScript metadata makes of it: the tag exactly on the pairs the decoder
// sizes, the character limit only behind a string-kinded field, and the rendered
// fragment parsing back through the runtime's tag reader.
func Test_sqltypeTag(t *testing.T) {
	t.Parallel()

	c := &client{tableMap: limitFixtureTables()}
	pkg := loadFixture(t, "limitfixture")
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

	tests := []struct {
		name          string
		field         string
		wantTag       string
		wantMaxLength int
	}{
		{name: "a string on STRING(n) carries the column type and its character limit", field: "Label", wantTag: `sqltype:"STRING(64)"`, wantMaxLength: 64},
		{name: "a nullable string on STRING(MAX) carries nothing", field: "Notes"},
		{name: "a NullString on STRING(n) carries both", field: "Alias", wantTag: `sqltype:"STRING(8)"`, wantMaxLength: 8},
		{name: "a named string type is string-kinded", field: "Kind", wantTag: `sqltype:"STRING(3)"`, wantMaxLength: 3},
		{name: "a byte slice on BYTES(n) carries the tag and no character limit", field: "Seal", wantTag: `sqltype:"BYTES(4)"`},
		{name: "a byte slice on BYTES(MAX) carries nothing", field: "Manifest"},
		{name: "a string slice on ARRAY<STRING(n)> carries the tag and the element limit", field: "Tags", wantTag: `sqltype:"ARRAY<STRING(4)>"`, wantMaxLength: 4},
		{name: "a decimal on NUMERIC carries the tag and no character limit", field: "Weight", wantTag: `sqltype:"NUMERIC"`},
		{name: "a NullDecimal on NUMERIC carries the tag", field: "Rebate", wantTag: `sqltype:"NUMERIC"`},
		{name: "a decimal slice on ARRAY<NUMERIC> carries the tag", field: "Fees", wantTag: `sqltype:"ARRAY<NUMERIC>"`},
		{name: "an integer carries nothing", field: "Count"},
		{name: "a UUID on STRING(36) carries nothing", field: "OwnerID"},
		{name: "the primary key carries nothing", field: "ID"},
		{name: "an immutable sized string carries the tag beside immutable", field: "Serial", wantTag: `sqltype:"STRING(16)"`, wantMaxLength: 16},
		{name: "an output-only field is hidden from the patch wire and carries nothing", field: "Stamp"},
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
