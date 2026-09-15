package generation

import (
	"strings"
	"testing"

	"github.com/cccteam/ccc/resource/generation/parser"
)

// arrayFixtureTables is the synthetic schema behind the arrayfixture's table-backed
// structs: the ARRAY and BYTES columns as the information schema spells them, with the
// nullability the struct's field types declare.
func arrayFixtureTables() map[string]*tableMetadata {
	column := func(spannerType string, nullable bool) columnMeta {
		return columnMeta{SpannerType: spannerType, IsNullable: nullable}
	}
	primaryKey := []indexMeta{{Name: "PRIMARY_KEY", PrimaryKey: true, Unique: true, Key: []indexColumn{{Column: "Id"}}}}

	tables := map[string]*tableMetadata{
		"Holds": {PkCount: 1, Columns: map[string]columnMeta{
			"Id":   {IsPrimaryKey: true, SpannerType: "STRING(36)"},
			"Bays": column("ARRAY<INT64>", false),
		}, Indexes: primaryKey},
		"Manifests": {PkCount: 1, Columns: map[string]columnMeta{
			"Id":    {IsPrimaryKey: true, SpannerType: "STRING(36)"},
			"Bays":  column("ARRAY<INT64>", false),
			"Named": column("ARRAY<INT64>", false),
			"Seal":  column("BYTES(32)", false),
			"Hash":  column("BYTES(32)", true),
		}, Indexes: primaryKey},
	}
	for _, table := range tables {
		table.deriveIndexFlags()
	}

	return tables
}

// Test_listColumnTags pins the query tags a list column refuses at extraction, on the
// table path and the view path alike: allow_filter, index, and uniqueindex on a field
// whose type is a slice, a named slice type, or a pointer to one, each refused naming
// the field with the sentence the computed path uses; and the shapes admitted beside
// them, a list with no query tag and a filterable byte slice, named or behind a pointer,
// which is one value.
func Test_listColumnTags(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadFixture(t, "arrayfixture"))

	tests := []struct {
		name       string
		structName string
		virtual    bool
		wantField  string
		wantErr    string
	}{
		{name: "allow_filter on a table's ARRAY column is refused", structName: "Hold", wantField: "Bays", wantErr: "allow_filter on a list field; a filter compares single values"},
		{name: "a list with no query tag and filterable byte slices are admitted", structName: "Manifest"},
		{name: "index on a view's ARRAY column is refused", structName: "IndexedView", virtual: true, wantField: "Tags", wantErr: "index on a list field; no index serves an ARRAY column"},
		{name: "uniqueindex on a view's ARRAY column is refused", structName: "UniqueView", virtual: true, wantField: "Codes", wantErr: "uniqueindex on a list field; no index serves an ARRAY column"},
		{name: "allow_filter on a named list type is refused", structName: "FilteredView", virtual: true, wantField: "Sizes", wantErr: "allow_filter on a list field; a filter compares single values"},
		{name: "allow_filter on a pointer to a list is refused", structName: "PointerView", virtual: true, wantField: "Sizes", wantErr: "allow_filter on a list field; a filter compares single values"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c := &client{tableMap: arrayFixtureTables()}
			var err error
			if tt.virtual {
				_, err = c.structsToVirtualResources([]*parser.Struct{structs[tt.structName]})
			} else {
				_, err = c.structsToResources([]*parser.Struct{structs[tt.structName]})
			}
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("extraction error = %v, want none", err)
				}

				return
			}
			if err == nil {
				t.Fatalf("extraction error = nil, want %q on %s", tt.wantErr, tt.wantField)
			}
			if got := err.Error(); !strings.Contains(got, tt.wantField) || !strings.Contains(got, tt.wantErr) {
				t.Errorf("extraction error = %v, want it to name %s with %q", err, tt.wantField, tt.wantErr)
			}
		})
	}
}
