package generation

import (
	"testing"

	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/generation/parser"
	"github.com/google/go-cmp/cmp"
)

// indexFixtureTables is the synthetic schema behind the indexfixture structs: every
// table carries its PRIMARY_KEY index and the foreign key's managed backing index on
// the tenant column, as the schema read reports them, plus the index shape the struct's
// doc comment names.
func indexFixtureTables() map[string]*tableMetadata {
	pk := columnMeta{IsPrimaryKey: true, IsIndex: true, IsUniqueIndex: true}
	tenant := columnMeta{IsForeignKey: true, IsIndex: true, ReferencedTable: "Tenants", ReferencedColumn: "Id"}
	plain := columnMeta{}
	primaryKey := indexMeta{Name: "PRIMARY_KEY", PrimaryKey: true, Unique: true, Key: []indexColumn{{Column: "Id"}}}
	backing := func(table string) indexMeta {
		return indexMeta{Name: "IDX_" + table + "_TenantId", Managed: true, Key: []indexColumn{{Column: "TenantId"}}}
	}

	return map[string]*tableMetadata{
		"Tenants": {PkCount: 1, Columns: map[string]columnMeta{"Id": pk}, Indexes: []indexMeta{primaryKey}},
		"Serveds": {PkCount: 1, Columns: map[string]columnMeta{
			"Id": pk, "TenantId": tenant, "PlacedAt": {IsIndex: true}, "Priority": {IsIndex: true},
		}, Indexes: []indexMeta{primaryKey, backing("Serveds"), {
			Name: "ServedsByTenantIdPlacedAtPriority",
			Key:  []indexColumn{{Column: "TenantId"}, {Column: "PlacedAt", Descending: true}, {Column: "Priority"}},
		}}},
		"Unserveds": {PkCount: 1, Columns: map[string]columnMeta{
			"Id": pk, "TenantId": tenant, "PlacedAt": plain, "Priority": plain,
		}, Indexes: []indexMeta{primaryKey, backing("Unserveds")}},
		"Misdirecteds": {PkCount: 1, Columns: map[string]columnMeta{
			"Id": pk, "TenantId": tenant, "PlacedAt": {IsIndex: true},
		}, Indexes: []indexMeta{primaryKey, backing("Misdirecteds"), {
			Name: "MisdirectedsByTenantIdPlacedAt",
			Key:  []indexColumn{{Column: "TenantId"}, {Column: "PlacedAt"}},
		}}},
		"NullFiltereds": {PkCount: 1, Columns: map[string]columnMeta{
			"Id": pk, "TenantId": tenant, "PlacedAt": {IsIndex: true, IsNullable: true},
		}, Indexes: []indexMeta{primaryKey, backing("NullFiltereds"), {
			Name:         "NullFilteredsByTenantIdPlacedAt",
			NullFiltered: true,
			Key:          []indexColumn{{Column: "TenantId"}, {Column: "PlacedAt"}},
		}}},
		"Unlisteds": {PkCount: 1, Columns: map[string]columnMeta{
			"Id": pk, "TenantId": tenant, "PlacedAt": plain,
		}, Indexes: []indexMeta{primaryKey, backing("Unlisteds")}},
		"Unordereds": {PkCount: 1, Columns: map[string]columnMeta{
			"Id": pk, "TenantId": tenant, "Name": plain,
		}, Indexes: []indexMeta{primaryKey, backing("Unordereds")}},
		"Keyeds": {PkCount: 2, Columns: map[string]columnMeta{
			"TenantId": {IsPrimaryKey: true, IsForeignKey: true, IsIndex: true, ReferencedTable: "Tenants", ReferencedColumn: "Id"},
			"Sequence": {IsPrimaryKey: true, IsIndex: true, KeyOrdinalPosition: 1},
			"Name":     plain,
		}, Indexes: []indexMeta{
			{Name: "PRIMARY_KEY", PrimaryKey: true, Unique: true, Key: []indexColumn{{Column: "TenantId"}, {Column: "Sequence"}}},
			backing("Keyeds"),
		}},
		"Routeds": {PkCount: 1, Columns: map[string]columnMeta{
			"Id": pk, "UnservedId": {IsForeignKey: true, IsIndex: true, ReferencedTable: "Unserveds", ReferencedColumn: "Id"}, "Name": plain,
		}, Indexes: []indexMeta{primaryKey, {Name: "IDX_Routeds_UnservedId", Managed: true, Key: []indexColumn{{Column: "UnservedId"}}}}},
		"Globals": {PkCount: 1, Columns: map[string]columnMeta{"Id": pk, "Name": plain}, Indexes: []indexMeta{primaryKey}},
	}
}

// TestSchemaWarnings pins the two generation-time schema warnings over the
// indexfixture resources: which shapes warn, which stay silent, and the exact values
// a warning carries.
func TestSchemaWarnings(t *testing.T) {
	t.Parallel()

	c := &client{tableMap: indexFixtureTables()}
	pkg := loadFixture(t, "indexfixture")

	tables, err := c.structsToResources(pkg.Structs)
	if err != nil {
		t.Fatalf("structsToResources() error = %v", err)
	}
	views, err := c.structsToVirtualResources([]*parser.Struct{fixtureStructs(pkg)["Projected"]})
	if err != nil {
		t.Fatalf("structsToVirtualResources() error = %v", err)
	}
	got := c.schemaWarnings(append(tables, views...))

	byResource := make(map[string]Warning, len(got))
	for _, w := range got {
		switch w := w.(type) {
		case IndexWarning:
			byResource[w.Resource] = w
		case JoinPathWarning:
			byResource[w.Resource] = w
		default:
			t.Fatalf("unexpected warning kind %T", w)
		}
	}

	tests := []struct {
		name     string
		resource string
		want     Warning
	}{
		{name: "the index present with a trailing column and the direction matched is silent", resource: "Served"},
		{
			name:     "the backing index alone warns, naming the index wanted",
			resource: "Unserved",
			want: IndexWarning{
				Resource: "Unserved", Table: "Unserveds", TenantColumn: "TenantId",
				Order: []resource.SortField{{Field: "PlacedAt", Direction: resource.SortAscending}, {Field: "Priority", Direction: resource.SortDescending}},
				Index: "CREATE INDEX UnservedsByTenantIdPlacedAtPriority ON Unserveds(TenantId, PlacedAt, Priority DESC)",
			},
		},
		{
			name:     "a composite index in the other direction warns",
			resource: "Misdirected",
			want: IndexWarning{
				Resource: "Misdirected", Table: "Misdirecteds", TenantColumn: "TenantId",
				Order: []resource.SortField{{Field: "PlacedAt", Direction: resource.SortDescending}},
				Index: "CREATE INDEX MisdirectedsByTenantIdPlacedAt ON Misdirecteds(TenantId, PlacedAt DESC)",
			},
		},
		{
			name:     "a null-filtered composite index warns",
			resource: "NullFiltered",
			want: IndexWarning{
				Resource: "NullFiltered", Table: "NullFiltereds", TenantColumn: "TenantId",
				Order: []resource.SortField{{Field: "PlacedAt", Direction: resource.SortAscending}},
				Index: "CREATE INDEX NullFilteredsByTenantIdPlacedAt ON NullFiltereds(TenantId, PlacedAt)",
			},
		},
		{name: "a suppressed list handler is silent", resource: "Unlisted"},
		{name: "a bare tenant column with no order is silent", resource: "Unordered"},
		{name: "an order the primary key serves is silent", resource: "Keyed"},
		{
			name:     "a join path warns that the lists scan the table",
			resource: "Routed",
			want: JoinPathWarning{
				Resource: "Routed", Table: "Routeds", Column: "UnservedId",
				Path: []resource.BindingHop{{Table: "Unserveds", JoinColumn: "Id", Column: "TenantId"}},
			},
		},
		{name: "a global resource is silent", resource: "Global"},
		{name: "a view with a bare tenant column is silent", resource: "Projected"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if diff := cmp.Diff(tt.want, byResource[tt.resource]); diff != "" {
				t.Errorf("schemaWarnings() for %s mismatch (-want +got):\n%s", tt.resource, diff)
			}
		})
	}

	if len(got) != 4 {
		t.Errorf("schemaWarnings() raised %d warnings, want 4: %v", len(got), got)
	}
}

// TestWarning_String pins the one-line texts a runner prints.
func TestWarning_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		warning Warning
		want    string
	}{
		{
			name: "index warning with a descending order column",
			warning: IndexWarning{
				Resource: "Consignment", Table: "Consignments", TenantColumn: "SectorId",
				Order: []resource.SortField{{Field: "ReleasedAt", Direction: resource.SortDescending}},
				Index: "CREATE INDEX ConsignmentsBySectorIdReleasedAt ON Consignments(SectorId, ReleasedAt DESC)",
			},
			want: "Consignment lists in SectorId, ReleasedAt DESC order with no index leading with those columns, so every page sorts the tenant's partition; wanted: CREATE INDEX ConsignmentsBySectorIdReleasedAt ON Consignments(SectorId, ReleasedAt DESC)",
		},
		{
			name: "join path warning over two hops",
			warning: JoinPathWarning{
				Resource: "RefitTask", Table: "RefitTasks", Column: "ShipId",
				Path: []resource.BindingHop{{Table: "Ships", JoinColumn: "Id", Column: "HangarId"}, {Table: "Hangars", JoinColumn: "Id", Column: "SectorId"}},
			},
			want: "RefitTask resolves its tenant through ShipId, Ships.HangarId, Hangars.SectorId, so its lists scan all of RefitTasks, every tenant, and no index on RefitTasks changes that; a table listed at volume carries the tenant key on the row",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.warning.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

// Test_tableMetadata_deriveIndexFlags pins the per-column flags the index composition
// yields: keyed or stored is indexed; the whole key of a unique index identifies a row,
// one column of a composite key or composite unique index does not.
func Test_tableMetadata_deriveIndexFlags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		indexes []indexMeta
		want    map[string]columnMeta
	}{
		{
			name:    "a single-column primary key identifies a row",
			indexes: []indexMeta{{Name: "PRIMARY_KEY", PrimaryKey: true, Unique: true, Key: []indexColumn{{Column: "Id"}}}},
			want:    map[string]columnMeta{"Id": {IsIndex: true, IsUniqueIndex: true}, "UserId": {}, "Note": {}},
		},
		{
			name:    "a composite primary key indexes its columns and identifies nothing by one",
			indexes: []indexMeta{{Name: "PRIMARY_KEY", PrimaryKey: true, Unique: true, Key: []indexColumn{{Column: "Id"}, {Column: "UserId"}}}},
			want:    map[string]columnMeta{"Id": {IsIndex: true}, "UserId": {IsIndex: true}, "Note": {}},
		},
		{
			name:    "a composite unique index indexes its columns and identifies nothing by one",
			indexes: []indexMeta{{Name: "ByUserIdNote", Unique: true, Key: []indexColumn{{Column: "UserId"}, {Column: "Note"}}}},
			want:    map[string]columnMeta{"Id": {}, "UserId": {IsIndex: true}, "Note": {IsIndex: true}},
		},
		{
			name:    "a non-unique single-column index identifies nothing",
			indexes: []indexMeta{{Name: "ByUserId", Key: []indexColumn{{Column: "UserId"}}}},
			want:    map[string]columnMeta{"Id": {}, "UserId": {IsIndex: true}, "Note": {}},
		},
		{
			name:    "a single-column unique index identifies a row, null-filtered or not",
			indexes: []indexMeta{{Name: "ByUserId", Unique: true, NullFiltered: true, Key: []indexColumn{{Column: "UserId"}}}},
			want:    map[string]columnMeta{"Id": {}, "UserId": {IsIndex: true, IsUniqueIndex: true}, "Note": {}},
		},
		{
			name:    "a stored column is indexed and never a key",
			indexes: []indexMeta{{Name: "ByUserId", Unique: true, Key: []indexColumn{{Column: "UserId"}}, Storing: []string{"Note"}}},
			want:    map[string]columnMeta{"Id": {}, "UserId": {IsIndex: true, IsUniqueIndex: true}, "Note": {IsIndex: true}},
		},
		{
			name:    "a column the table does not report is skipped",
			indexes: []indexMeta{{Name: "ByHidden", Key: []indexColumn{{Column: "Hidden_HIDDEN"}}, Storing: []string{"Other_HIDDEN"}}},
			want:    map[string]columnMeta{"Id": {}, "UserId": {}, "Note": {}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			table := &tableMetadata{Columns: map[string]columnMeta{"Id": {}, "UserId": {}, "Note": {}}, Indexes: tt.indexes}
			table.deriveIndexFlags()
			if diff := cmp.Diff(tt.want, table.Columns); diff != "" {
				t.Errorf("deriveIndexFlags() columns mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// Test_tableMetadata_addIndexResult pins the folding of index rows into an index
// list: rows group by index name, key columns keep their order and direction, and a
// row without an ordinal position is a stored column.
func Test_tableMetadata_addIndexResult(t *testing.T) {
	t.Parallel()

	ordinal := func(n int64) *int64 {
		return &n
	}
	ordering := func(s string) *string {
		return &s
	}

	tests := []struct {
		name string
		rows []indexSchemaResult
		want []indexMeta
	}{
		{
			name: "the primary key, then a composite index with a stored column",
			rows: []indexSchemaResult{
				{TableName: "Orders", IndexName: "PRIMARY_KEY", IndexType: "PRIMARY_KEY", IsUnique: true, ColumnName: "Id", OrdinalPosition: ordinal(1), ColumnOrdering: ordering("ASC")},
				{TableName: "Orders", IndexName: "OrdersByTenantIdPlacedAt", IndexType: "INDEX", ColumnName: "TenantId", OrdinalPosition: ordinal(1), ColumnOrdering: ordering("ASC")},
				{TableName: "Orders", IndexName: "OrdersByTenantIdPlacedAt", IndexType: "INDEX", ColumnName: "PlacedAt", OrdinalPosition: ordinal(2), ColumnOrdering: ordering("DESC")},
				{TableName: "Orders", IndexName: "OrdersByTenantIdPlacedAt", IndexType: "INDEX", ColumnName: "Note"},
			},
			want: []indexMeta{
				{Name: "PRIMARY_KEY", PrimaryKey: true, Unique: true, Key: []indexColumn{{Column: "Id"}}},
				{Name: "OrdersByTenantIdPlacedAt", Key: []indexColumn{{Column: "TenantId"}, {Column: "PlacedAt", Descending: true}}, Storing: []string{"Note"}},
			},
		},
		{
			name: "a managed null-filtered index keeps its flags",
			rows: []indexSchemaResult{
				{TableName: "Orders", IndexName: "IDX_Orders_TenantId_ABC", IndexType: "INDEX", IsNullFiltered: true, IsManaged: true, ColumnName: "TenantId", OrdinalPosition: ordinal(1), ColumnOrdering: ordering("ASC")},
			},
			want: []indexMeta{{Name: "IDX_Orders_TenantId_ABC", NullFiltered: true, Managed: true, Key: []indexColumn{{Column: "TenantId"}}}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			table := &tableMetadata{Columns: map[string]columnMeta{}}
			for i := range tt.rows {
				table.addIndexResult(&tt.rows[i])
			}
			if diff := cmp.Diff(tt.want, table.Indexes); diff != "" {
				t.Errorf("addIndexResult() indexes mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
