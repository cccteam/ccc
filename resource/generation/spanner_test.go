package generation

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

// Test_createTableMapUsingQuery reads the index-shapes fixture schema through the
// emulator and pins what the information schema yields: every index's composition,
// the primary key and Spanner's foreign-key backing indexes included, and the column
// flags derived from it. Orders and Seats share the column name Note and every table
// carries a PRIMARY_KEY index, so an index joined to its columns on index name alone
// would leak across tables here. Requires the Spanner emulator (podman/docker).
func Test_createTableMapUsingQuery(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("the schema read requires the Spanner emulator")
	}

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed")
	}
	migrations := "file://" + filepath.Join(filepath.Dir(thisFile), "testdata", "migrations")

	ctx := context.Background()
	db, err := createSpannerDB(ctx, "1.5.56", []string{migrations})
	if err != nil {
		t.Fatalf("createSpannerDB() error = %v", err)
	}
	t.Cleanup(func() {
		if err := db.DropDatabase(context.Background()); err != nil {
			t.Errorf("DropDatabase() error = %v", err)
		}
		if err := db.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})

	tableMap, err := createTableMapUsingQuery(ctx, db.Client)
	if err != nil {
		t.Fatalf("createTableMapUsingQuery() error = %v", err)
	}

	// Spanner names the indexes it manages for foreign keys itself, so those compare
	// by shape with the name blanked.
	unnamed := cmp.Transformer("managed", func(index indexMeta) indexMeta {
		if index.Managed {
			index.Name = ""
		}

		return index
	})

	tests := []struct {
		name        string
		table       string
		wantIndexes []indexMeta
		wantFlags   map[string][2]bool // column -> IsIndex, IsUniqueIndex
		wantTypes   map[string]string  // column -> SpannerType, as the information schema spells it
	}{
		{
			name:        "a table with only its primary key, sharing the key column name with Orders",
			table:       "Tenants",
			wantIndexes: []indexMeta{{Name: "PRIMARY_KEY", PrimaryKey: true, Unique: true, Key: []indexColumn{{Column: "Id"}}}},
			wantFlags:   map[string][2]bool{"Id": {true, true}},
			wantTypes:   map[string]string{"Id": "STRING(36)"},
		},
		{
			name:  "a table with composite, unique, null-filtered unique, null-filtered, and backing indexes",
			table: "Orders",
			wantIndexes: []indexMeta{
				{Managed: true, Key: []indexColumn{{Column: "TenantId"}}},
				{Name: "OrdersByExternalRef", Unique: true, NullFiltered: true, Key: []indexColumn{{Column: "ExternalRef"}}},
				{Name: "OrdersByNote", NullFiltered: true, Key: []indexColumn{{Column: "Note"}}},
				{Name: "OrdersByReference", Unique: true, Key: []indexColumn{{Column: "Reference"}}},
				{Name: "OrdersByTenantIdPlacedAt", Key: []indexColumn{{Column: "TenantId"}, {Column: "PlacedAt", Descending: true}}, Storing: []string{"Note"}},
				{Name: "PRIMARY_KEY", PrimaryKey: true, Unique: true, Key: []indexColumn{{Column: "Id"}}},
			},
			// PlacedAt trails TenantId in the composite index and leads nothing, so it is
			// not indexed at table level. Note is stored by that index, which alone would
			// not index it, but it leads OrdersByNote.
			wantFlags: map[string][2]bool{
				"Id": {true, true}, "TenantId": {true, false}, "PlacedAt": {false, false},
				"Reference": {true, true}, "ExternalRef": {true, true}, "Note": {true, false},
			},
			wantTypes: map[string]string{"TenantId": "STRING(36)", "PlacedAt": "TIMESTAMP", "Reference": "STRING(MAX)"},
		},
		{
			// The foreign key on OrderId needs no backing index: the primary key leads
			// with it, so Spanner manages none. Neither the composite key nor the
			// composite unique index identifies a row by one column, and their trailing
			// columns UserId and Row lead nothing, so they are not indexed.
			name:  "a composite key and a composite unique index identify a row by no single column",
			table: "Seats",
			wantIndexes: []indexMeta{
				{Name: "PRIMARY_KEY", PrimaryKey: true, Unique: true, Key: []indexColumn{{Column: "OrderId"}, {Column: "UserId"}}},
				{Name: "SeatsByNote", Key: []indexColumn{{Column: "Note"}}},
				{Name: "SeatsByOrderIdRow", Unique: true, Key: []indexColumn{{Column: "OrderId"}, {Column: "Row"}}},
			},
			wantFlags: map[string][2]bool{"OrderId": {true, false}, "UserId": {false, false}, "Row": {false, false}, "Note": {true, false}},
			wantTypes: map[string]string{"Row": "INT64", "Note": "STRING(MAX)"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			table, ok := tableMap[tt.table]
			if !ok {
				t.Fatalf("table %s not in the table map", tt.table)
			}
			sortByName := cmpopts.SortSlices(func(a, b indexMeta) bool {
				return a.Name < b.Name
			})
			if diff := cmp.Diff(tt.wantIndexes, table.Indexes, unnamed, sortByName, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("%s indexes mismatch (-want +got):\n%s", tt.table, diff)
			}
			for column, want := range tt.wantFlags {
				meta, ok := table.Columns[column]
				if !ok {
					t.Errorf("column %s.%s not in the table map", tt.table, column)

					continue
				}
				if got := [2]bool{meta.IsIndex, meta.IsUniqueIndex}; got != want {
					t.Errorf("%s.%s flags (IsIndex, IsUniqueIndex) = %v, want %v", tt.table, column, got, want)
				}
			}
			for column, want := range tt.wantTypes {
				if got := table.Columns[column].SpannerType; got != want {
					t.Errorf("%s.%s SpannerType = %q, want %q", tt.table, column, got, want)
				}
			}
		})
	}
}
