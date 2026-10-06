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
		"Crates": {PkCount: 1, Columns: map[string]columnMeta{
			"Id":     {IsPrimaryKey: true, SpannerType: "STRING(36)"},
			"Labels": column("ARRAY<STRING(MAX)>", false),
		}, Indexes: primaryKey},
		"Parcels": {PkCount: 1, Columns: map[string]columnMeta{
			"Id":   {IsPrimaryKey: true, SpannerType: "STRING(36)"},
			"Seal": column("BYTES(32)", false),
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

// Test_listFieldEnumerate pins the field-scope @enumerate a list field refuses at
// extraction, on every path that reads the annotation: a slice on a table, a named list
// type and a Go array on a view, and a slice of keys on a computed resource and on a
// request, each refused naming the struct and the field with the one sentence and
// nothing about the argument, since the refusal precedes its reading; and the byte slice
// admitted beside them on a table and on a request, one value on the wire, whose
// declaration is recorded or resolved as any other.
func Test_listFieldEnumerate(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadFixture(t, "arrayfixture"))

	tests := []struct {
		name       string
		structName string
		kind       string // the struct's kind keyword
		wantField  string // the refused field; empty when the declaration is admitted
	}{
		{name: "a slice on a table is refused", structName: "Crate", kind: resourceKeyword, wantField: "Labels"},
		{name: "a byte slice on a table is admitted", structName: "Parcel", kind: resourceKeyword},
		{name: "a named list type on a view is refused", structName: "NamedListView", kind: virtualKeyword, wantField: "Sizes"},
		{name: "an array on a view is refused", structName: "ArrayView", kind: virtualKeyword, wantField: "Slots"},
		{name: "a slice of keys on a computed resource is refused", structName: "Ledger", kind: computedKeyword, wantField: "HoldIDs"},
		{name: "a slice of keys on a request is refused", structName: "Stow", kind: rpcKeyword, wantField: "HoldIDs"},
		{name: "a byte slice on a request is admitted", structName: "Stamp", kind: rpcKeyword},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c := &client{
				tableMap:  arrayFixtureTables(),
				resources: []*resourceInfo{fixtureResource(t, structs, "Manifest", nil)},
			}
			pStruct := []*parser.Struct{structs[tt.structName]}
			var (
				err      error
				declared bool
			)
			switch tt.kind {
			case resourceKeyword:
				var resources []*resourceInfo
				resources, err = c.structsToResources(pStruct)
				declared = err == nil && resources[0].Fields[1].HasDeclaredEnumeration()
			case virtualKeyword:
				_, err = c.structsToVirtualResources(pStruct)
			case computedKeyword:
				_, err = c.structsToCompResources(pStruct)
			case rpcKeyword:
				var methods []*rpcMethodInfo
				methods, err = c.structsToRPCMethods(pStruct)
				declared = err == nil && methods[0].Fields[0].IsEnumerated()
			default:
				t.Fatalf("unknown struct kind %q", tt.kind)
			}
			if tt.wantField == "" {
				if err != nil {
					t.Fatalf("extraction error = %v, want none", err)
				}
				if !declared {
					t.Error("the admitted declaration was dropped")
				}

				return
			}
			if err == nil {
				t.Fatalf("extraction error = nil, want %q on %s", listFieldEnumerateRefusal, tt.wantField)
			}
			got := err.Error()
			if !strings.Contains(got, tt.wantField) || !strings.Contains(got, listFieldEnumerateRefusal) {
				t.Errorf("extraction error = %v, want it to name %s with %q", err, tt.wantField, listFieldEnumerateRefusal)
			}
			if strings.Count(got, listFieldEnumerateRefusal) != 1 || strings.Contains(got, "@"+enumerateKeyword+"(") {
				t.Errorf("extraction error = %v, want the one sentence and nothing about the argument", err)
			}
		})
	}
}
