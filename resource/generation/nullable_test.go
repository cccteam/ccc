package generation

import (
	"strings"
	"testing"

	"github.com/cccteam/ccc/resource/generation/parser"
	"golang.org/x/tools/go/packages"
)

// A slice-typed field takes its nullability from its column: the check leaves slices
// out, the nullable tag carries the column's answer onto the patch request struct, the
// metadata's required follows it, and a pointer to a slice, which the Spanner client
// cannot decode into, is refused naming the plain slice.

// nullableFixtureTables is the synthetic schema behind the nullablefixture structs: the
// BYTES and ARRAY columns as the information schema spells them, nullable and NOT NULL,
// beside the scalar columns the check still reads off the field types.
func nullableFixtureTables() map[string]*tableMetadata {
	column := func(spannerType string, nullable bool) columnMeta {
		return columnMeta{SpannerType: spannerType, IsNullable: nullable}
	}
	primaryKey := []indexMeta{{Name: "PRIMARY_KEY", PrimaryKey: true, Unique: true, Key: []indexColumn{{Column: "Id"}}}}

	tables := map[string]*tableMetadata{
		"Rosters": {PkCount: 1, Columns: map[string]columnMeta{
			"Id":     {IsPrimaryKey: true, SpannerType: "STRING(36)"},
			"Seal":   column("BYTES(32)", true),
			"Bays":   column("ARRAY<INT64>", true),
			"Named":  column("ARRAY<STRING(16)>", true),
			"Digest": column("BYTES(32)", false),
			"Tags":   column("ARRAY<STRING(8)>", false),
			"Nulled": column("ARRAY<STRING(8)>", false),
			"Stamp":  column("BYTES(MAX)", true),
			"Label":  column("STRING(MAX)", true),
			"Note":   column("STRING(MAX)", false),
		}, Indexes: primaryKey},
		"Ledgers": {PkCount: 1, Columns: map[string]columnMeta{
			"Id":    {IsPrimaryKey: true, SpannerType: "STRING(36)"},
			"Count": column("INT64", true),
			"Bays":  column("ARRAY<INT64>", true),
		}, Indexes: primaryKey},
		"Pins": {PkCount: 1, Columns: map[string]columnMeta{
			"Id":    {IsPrimaryKey: true, SpannerType: "STRING(36)"},
			"Seal":  column("BYTES(32)", true),
			"Bays":  column("ARRAY<INT64>", true),
			"Named": column("ARRAY<STRING(16)>", true),
		}, Indexes: primaryKey},
	}
	for _, table := range tables {
		table.deriveIndexFlags()
	}

	return tables
}

// nullableFixtureClient builds a client over the nullable fixture and its synthetic
// schema, the loaded package noted so the column typing resolves the fixture's named
// types without loading the package again.
func nullableFixtureClient(t *testing.T) *client {
	t.Helper()

	loaded, _ := loadFixturePackage(t, "nullablefixture")
	c := &client{tableMap: nullableFixtureTables()}
	c.notePackages(map[string]*packages.Package{loaded.Name: loaded})

	return c
}

// extractNullableFixture extracts one fixture struct as a table-backed resource, the
// nullability check included.
func extractNullableFixture(t *testing.T, c *client, name string) (*resourceInfo, error) {
	t.Helper()

	structs := fixtureStructs(loadFixture(t, "nullablefixture"))
	pStruct := structs[name]
	if pStruct == nil {
		t.Fatalf("struct %q not found in fixture package", name)
	}
	resources, err := c.structsToResources([]*parser.Struct{pStruct})
	if err != nil {
		return nil, err
	}
	if len(resources) != 1 {
		t.Fatalf("structsToResources(%s) extracted %d resources, want 1", name, len(resources))
	}

	return resources[0], nil
}

// Test_validateNullability is the check's first dedicated test: a slice-typed field is
// left out of the mismatch table whatever its column says, on a byte slice, a slice of
// integers, a named slice, and a Null-prefixed named slice alike, while a scalar whose
// field type and column disagree is still reported, in the table, alone.
func Test_validateNullability(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		structName string
		wantErr    []string
		absent     []string
	}{
		{
			name:       "every slice shape passes on a nullable and a NOT NULL column alike, beside matching scalars",
			structName: "Roster",
		},
		{
			name:       "a scalar mismatch is reported in the table and the slice beside it is not",
			structName: "Ledger",
			wantErr:    []string{"found mismatching nullability between the struct fields and columns", "| Count "},
			absent:     []string{"| Bays "},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := extractNullableFixture(t, nullableFixtureClient(t), tt.structName)
			if len(tt.wantErr) == 0 {
				if err != nil {
					t.Fatalf("structsToResources() error = %v, want none", err)
				}

				return
			}
			if err == nil {
				t.Fatalf("structsToResources() error = nil, want the mismatch table")
			}
			got := err.Error()
			for _, want := range tt.wantErr {
				if !strings.Contains(got, want) {
					t.Errorf("error is missing %q:\n%s", want, got)
				}
			}
			for _, absent := range tt.absent {
				if strings.Contains(got, absent) {
					t.Errorf("error carries %q, which the table must leave out:\n%s", absent, got)
				}
			}
		})
	}
}

// Test_nullableTag pins where the patch request structs carry the nullable marker and
// what the metadata's required says: the tag on exactly the slice-typed fields whose
// columns allow NULL, never on a NOT NULL slice, a pointer, a scalar, a key, or an
// output-only field; required false where the column allows NULL and true where it does
// not; and the rendered fragment parsing back through the runtime's tag reader.
func Test_nullableTag(t *testing.T) {
	t.Parallel()

	roster, err := extractNullableFixture(t, nullableFixtureClient(t), "Roster")
	if err != nil {
		t.Fatalf("structsToResources() error = %v", err)
	}

	tests := []struct {
		name         string
		field        string
		wantTag      string
		wantRequired bool
	}{
		{name: "a byte slice on a nullable BYTES column carries the tag and is not required", field: "Seal", wantTag: `nullable:"true"`},
		{name: "a slice of integers on a nullable ARRAY column carries the tag", field: "Bays", wantTag: `nullable:"true"`},
		{name: "a named slice on a nullable ARRAY column carries the tag", field: "Named", wantTag: `nullable:"true"`},
		{name: "a byte slice on a NOT NULL BYTES column carries nothing and is required", field: "Digest", wantRequired: true},
		{name: "a slice of strings on a NOT NULL ARRAY column carries nothing and is required", field: "Tags", wantRequired: true},
		{name: "a Null-prefixed named slice on a NOT NULL column carries nothing", field: "Nulled", wantRequired: true},
		{name: "an output-only slice is hidden from the patch wire and carries nothing", field: "Stamp"},
		{name: "a pointer on a nullable column says so by its type and carries nothing", field: "Label"},
		{name: "a string on a NOT NULL column carries nothing and is required", field: "Note", wantRequired: true},
		{name: "the primary key carries nothing", field: "ID"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var field *resourceField
			for _, f := range roster.Fields {
				if f.Name() == tt.field {
					field = f
				}
			}
			if field == nil {
				t.Fatalf("field %s not extracted", tt.field)
			}
			if got := field.NullableTag(); got != tt.wantTag {
				t.Errorf("NullableTag() = %q, want %q", got, tt.wantTag)
			}
			if got := field.IsRequired(); got != tt.wantRequired {
				t.Errorf("IsRequired() = %t, want %t", got, tt.wantRequired)
			}

			var wantNullable string
			if tt.wantTag != "" {
				wantNullable = "true"
			}
			ft := fieldTagsFromTemplateTags(field.Name(), field.JSONTagForPatch(), field.ImmutableTag(), field.SqltypeTag(), field.NullableTag())
			if ft.Nullable != wantNullable {
				t.Errorf("rendered tag parses to Nullable %q, want %q", ft.Nullable, wantNullable)
			}
		})
	}
}

// Test_pointerToSliceRefusal pins the column typing's refusal of a pointer to a slice:
// on a table, every pointer-to-slice field is refused naming the plain slice and quoting
// the client's own decode error for the column's type, with no @typescript clause, since
// no declaration can fix it; on a view, which has no schema type, the quote is left out.
func Test_pointerToSliceRefusal(t *testing.T) {
	t.Parallel()

	c := nullableFixtureClient(t)
	generator := &typescriptGenerator{client: c, outletExcludedTables: map[string]struct{}{}}

	table, err := extractNullableFixture(t, c, "Pin")
	if err != nil {
		t.Fatalf("structsToResources() error = %v", err)
	}
	view := fixtureResource(t, fixtureStructs(loadFixture(t, "nullablefixture")), "Pin", nil)

	tests := []struct {
		name   string
		res    *resourceInfo
		want   []string
		absent []string
	}{
		{
			name: "on a table, each pointer to a slice is refused with the client's words for its column",
			res:  table,
			want: []string{
				"Pins.Seal: *[]byte cannot be read by the Spanner client (type **[]uint8 cannot be used for decoding BYTES); type the column with the plain slice []byte, which reads NULL as nil",
				"Pins.Bays: *[]int64 cannot be read by the Spanner client (type **[]int64 cannot be used for decoding ARRAY[INT64]); type the column with the plain slice []int64, which reads NULL as nil",
				"Pins.Named: *nullablefixture.Marks cannot be read by the Spanner client (type **nullablefixture.Marks cannot be used for decoding ARRAY[STRING]); type the column with the plain slice nullablefixture.Marks, which reads NULL as nil",
			},
			absent: []string{"declare the type's TypeScript form"},
		},
		{
			name: "on a view, which has no schema type, the refusal stands without the quote",
			res:  view,
			want: []string{
				"Pins.Bays: *[]int64 cannot be read by the Spanner client; type the column with the plain slice []int64, which reads NULL as nil",
			},
			absent: []string{"cannot be used for decoding", "declare the type's TypeScript form"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := generator.resourceFieldsTypescriptType(tt.res)
			if err == nil {
				t.Fatal("resourceFieldsTypescriptType() error = nil, want every refusal")
			}
			got := err.Error()
			for _, want := range tt.want {
				if !strings.Contains(got, want) {
					t.Errorf("error is missing %q:\n%s", want, got)
				}
			}
			for _, absent := range tt.absent {
				if strings.Contains(got, absent) {
					t.Errorf("error carries %q, which the refusal must leave out:\n%s", absent, got)
				}
			}
		})
	}
}

// Test_typescriptResourcesTemplate_nullableSlice pins the metadata a slice column
// renders: required false where the column allows NULL and true where it does not, on a
// byte slice and an array alike, with the display type and the element limit unchanged.
func Test_typescriptResourcesTemplate_nullableSlice(t *testing.T) {
	t.Parallel()

	c := nullableFixtureClient(t)
	roster, err := extractNullableFixture(t, c, "Roster")
	if err != nil {
		t.Fatalf("structsToResources() error = %v", err)
	}
	generator := &typescriptGenerator{client: c, outletExcludedTables: map[string]struct{}{}}
	if err := generator.resourceFieldsTypescriptType(roster); err != nil {
		t.Fatalf("resourceFieldsTypescriptType() error = %v", err)
	}

	data := tsResourcesData{Resources: []*resourceInfo{roster}, GenPrefix: "zz_gen", File: generator}
	out, err := c.generateTemplateOutput("typescriptResourcesTemplate", typescriptResourcesTemplate, data)
	if err != nil {
		t.Fatalf("generateTemplateOutput() error = %v", err)
	}

	tests := []struct {
		name string
		want string
	}{
		{name: "a byte slice on a nullable column is not required", want: "{ fieldName: 'seal', displayType: 'bytes', required: false, isIndex: false }"},
		{name: "an integer array on a nullable column is not required", want: "{ fieldName: 'bays', displayType: 'number[]', required: false, isIndex: false }"},
		{name: "a named string array on a nullable column is not required", want: "{ fieldName: 'named', displayType: 'string[]', required: false, isIndex: false }"},
		{name: "a byte slice on a NOT NULL column is required", want: "{ fieldName: 'digest', displayType: 'bytes', required: true, isIndex: false }"},
		{name: "a string array on a NOT NULL column is required", want: "{ fieldName: 'tags', displayType: 'string[]', required: true, isIndex: false, maxLength: 8 }"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if !strings.Contains(string(out), tt.want) {
				t.Errorf("typescriptResourcesTemplate output missing %q:\n%s", tt.want, out)
			}
		})
	}
}
