package generation

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

// Test_postgresColumnType pins how a PostgreSQL type, as format_type spells it, is stated
// in the Spanner vocabulary the generator parses.
func Test_postgresColumnType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		formatted string
		want      string
		wantErr   bool
	}{
		{formatted: "character varying(64)", want: "STRING(64)"},
		{formatted: "character varying", want: "STRING(MAX)"},
		{formatted: "character(4)", want: "STRING(4)"},
		{formatted: "text", want: "STRING(MAX)"},
		{formatted: "uuid", want: "UUID"},
		{formatted: "smallint", want: "INT64"},
		{formatted: "integer", want: "INT64"},
		{formatted: "bigint", want: "INT64"},
		{formatted: "real", want: "FLOAT32"},
		{formatted: "double precision", want: "FLOAT64"},
		{formatted: "numeric", want: "NUMERIC"},
		{formatted: "numeric(12,2)", want: "NUMERIC"},
		{formatted: "boolean", want: "BOOL"},
		{formatted: "timestamp with time zone", want: "TIMESTAMP"},
		{formatted: "timestamp without time zone", want: "TIMESTAMP"},
		{formatted: "date", want: "DATE"},
		{formatted: "bytea", want: "BYTES(MAX)"},
		{formatted: "json", want: "JSON"},
		{formatted: "jsonb", want: "JSON"},
		{formatted: "text[]", want: "ARRAY<STRING(MAX)>"},
		{formatted: "character varying(8)[]", want: "ARRAY<STRING(8)>"},
		{formatted: "bigint[]", want: "ARRAY<INT64>"},
		{formatted: "tsvector", want: "TSVECTOR"},
		{formatted: "Character Varying(10)", want: "STRING(10)"},
		{formatted: "weird(", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.formatted, func(t *testing.T) {
			t.Parallel()

			got, err := postgresColumnType(tt.formatted)
			if (err != nil) != tt.wantErr {
				t.Fatalf("postgresColumnType() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("postgresColumnType(%q) = %q, want %q", tt.formatted, got, tt.want)
			}
		})
	}
}

// Test_postgresSchemaResults pins the fold of the columns and the keys into the rows the
// Spanner query yields: key membership and position, the foreign key's delete rule, and
// the one extra jump to the table a referenced foreign key finally refers to.
func Test_postgresSchemaResults(t *testing.T) {
	t.Parallel()

	str := func(s string) *string { return &s }
	columns := []postgresColumnRow{
		{table: "A", column: "Id", columnType: "text", ordinal: 1},
		{table: "B", column: "Id", columnType: "text", ordinal: 1},
		{table: "B", column: "ARef", columnType: "text", ordinal: 2, nullable: true},
		{table: "C", column: "BRef", columnType: "text", ordinal: 1},
		{table: "C", column: "N", columnType: "bigint", ordinal: 2},
	}
	keys := []postgresKeyRow{
		{table: "A", column: "Id", kind: "p", position: 1},
		{table: "B", column: "Id", kind: "p", position: 1},
		{table: "B", column: "ARef", kind: "f", position: 1, referencedTable: str("A"), referencedColumn: str("Id")},
		{table: "C", column: "BRef", kind: "p", position: 1},
		{table: "C", column: "N", kind: "p", position: 2},
		{table: "C", column: "BRef", kind: "f", position: 1, referencedTable: str("B"), referencedColumn: str("ARef"), deleteCascades: true},
	}

	got, err := postgresSchemaResults(columns, keys)
	if err != nil {
		t.Fatalf("postgresSchemaResults() error = %v", err)
	}

	noAction, cascade := "NO ACTION", "CASCADE"
	want := []informationSchemaResult{
		{TableName: "A", ColumnName: "Id", SpannerType: "STRING(MAX)", IsPrimaryKey: true, OrdinalPosition: 1, KeyOrdinalPosition: 1},
		{TableName: "B", ColumnName: "Id", SpannerType: "STRING(MAX)", IsPrimaryKey: true, OrdinalPosition: 1, KeyOrdinalPosition: 1},
		{TableName: "B", ColumnName: "ARef", SpannerType: "STRING(MAX)", IsNullable: true, IsForeignKey: true, ReferencedTable: str("A"), ReferencedColumn: str("Id"), DeleteRule: &noAction, OrdinalPosition: 2, KeyOrdinalPosition: 1},
		// C.BRef references B.ARef, itself a foreign key to A.Id: the table it finally
		// belongs to is A.
		{TableName: "C", ColumnName: "BRef", SpannerType: "STRING(MAX)", IsPrimaryKey: true, IsForeignKey: true, ReferencedTable: str("A"), ReferencedColumn: str("Id"), DeleteRule: &cascade, OrdinalPosition: 1, KeyOrdinalPosition: 1},
		{TableName: "C", ColumnName: "N", SpannerType: "INT64", IsPrimaryKey: true, OrdinalPosition: 2, KeyOrdinalPosition: 2},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("postgresSchemaResults() mismatch (-want +got):\n%s", diff)
	}
}

// Test_createPostgresTableMap reads the PostgreSQL twin of the index-shapes fixture
// through a container and pins what the system catalogs yield, as
// Test_createTableMapUsingQuery pins the Spanner read: every index's composition, the
// column flags derived from it, and the foreign keys' delete rules. PostgreSQL has no
// interleaving and creates no index for a foreign key, so those facts differ from
// Spanner's. Requires a container runtime (podman/docker).
func Test_createPostgresTableMap(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("the schema read requires a PostgreSQL container")
	}

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed")
	}
	migrations := "file://" + filepath.Join(filepath.Dir(thisFile), "testdata", "postgresmigrations")

	ctx := context.Background()
	db, container, err := createPostgresDB(ctx, "17", []string{migrations})
	if err != nil {
		t.Fatalf("createPostgresDB() error = %v", err)
	}
	t.Cleanup(func() {
		db.Close()
		releasePostgresContainer(context.Background(), container)
	})

	tableMap, err := createPostgresTableMap(ctx, db.Pool)
	if err != nil {
		t.Fatalf("createPostgresTableMap() error = %v", err)
	}

	tests := []struct {
		name        string
		table       string
		wantIndexes []indexMeta
		wantFlags   map[string][2]bool // column -> IsIndex, IsUniqueIndex
		wantTypes   map[string]string
		// wantDeleteRules maps a column to its foreign key's delete rule, "" where it has none.
		wantDeleteRules map[string]string
	}{
		{
			name:        "a table with only its primary key",
			table:       "Tenants",
			wantIndexes: []indexMeta{{Name: "PRIMARY_KEY", PrimaryKey: true, Unique: true, Key: []indexColumn{{Column: "Id"}}}},
			wantFlags:   map[string][2]bool{"Id": {true, true}},
			wantTypes:   map[string]string{"Id": "STRING(36)"},
		},
		{
			name:  "composite, unique, partial and including indexes",
			table: "Orders",
			wantIndexes: []indexMeta{
				{Name: "OrdersByExternalRef", Unique: true, NullFiltered: true, Key: []indexColumn{{Column: "ExternalRef"}}},
				{Name: "OrdersByNote", NullFiltered: true, Key: []indexColumn{{Column: "Note"}}},
				{Name: "OrdersByReference", Unique: true, Key: []indexColumn{{Column: "Reference"}}},
				{Name: "OrdersByTenantIdPlacedAt", Key: []indexColumn{{Column: "TenantId"}, {Column: "PlacedAt", Descending: true}}, Storing: []string{"Note"}},
				{Name: "PRIMARY_KEY", PrimaryKey: true, Unique: true, Key: []indexColumn{{Column: "Id"}}},
			},
			wantFlags: map[string][2]bool{
				"Id": {true, true}, "TenantId": {true, false}, "PlacedAt": {false, false},
				"Reference": {true, true}, "ExternalRef": {true, true}, "Note": {true, false},
			},
			wantTypes:       map[string]string{"TenantId": "STRING(36)", "PlacedAt": "TIMESTAMP", "Reference": "STRING(MAX)"},
			wantDeleteRules: map[string]string{"TenantId": "NO ACTION", "PlacedAt": ""},
		},
		{
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
		{
			// No index backs a foreign key here, so neither foreign key column is indexed.
			name:  "a cascading foreign key and a plain one report each rule, and back no index",
			table: "Attachments",
			wantIndexes: []indexMeta{
				{Name: "PRIMARY_KEY", PrimaryKey: true, Unique: true, Key: []indexColumn{{Column: "Id"}}},
			},
			wantFlags:       map[string][2]bool{"Id": {true, true}, "OrderId": {false, false}, "TenantId": {false, false}},
			wantDeleteRules: map[string]string{"OrderId": "CASCADE", "TenantId": "NO ACTION", "StoreKey": ""},
		},
		{
			name:        "a child keyed by its parent's key cascades through its foreign key",
			table:       "OrderLines",
			wantIndexes: []indexMeta{{Name: "PRIMARY_KEY", PrimaryKey: true, Unique: true, Key: []indexColumn{{Column: "Id"}, {Column: "LineNumber"}}}},
			wantFlags:   map[string][2]bool{"Id": {true, false}, "LineNumber": {false, false}},
			wantTypes:   map[string]string{"StoreKey": "STRING(36)"},
			wantDeleteRules: map[string]string{
				"Id": "CASCADE",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			table, ok := tableMap[tt.table]
			if !ok {
				t.Fatalf("table %s not in the table map", tt.table)
			}
			sortByName := cmpopts.SortSlices(func(a, b indexMeta) bool { return a.Name < b.Name })
			if diff := cmp.Diff(tt.wantIndexes, table.Indexes, sortByName, cmpopts.EquateEmpty()); diff != "" {
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
			for column, want := range tt.wantDeleteRules {
				if got := table.Columns[column].DeleteRule; got != want {
					t.Errorf("%s.%s DeleteRule = %q, want %q", tt.table, column, got, want)
				}
			}
			if table.IsInterleaved || table.ParentTable != "" || table.OnDeleteCascade {
				t.Errorf("%s reports an interleave (%v, %q, %v), which PostgreSQL has none of", tt.table, table.IsInterleaved, table.ParentTable, table.OnDeleteCascade)
			}
		})
	}

	t.Run("every column type the read maps", func(t *testing.T) {
		t.Parallel()

		want := map[string]string{
			"Id": "UUID", "Label": "STRING(64)", "Fixed": "STRING(4)", "Body": "STRING(MAX)", "Small": "INT64", "Count": "INT64",
			"Big": "INT64", "Ratio32": "FLOAT32", "Ratio64": "FLOAT64", "Price": "NUMERIC", "Flag": "BOOL", "At": "TIMESTAMP",
			"AtLocal": "TIMESTAMP", "Day": "DATE", "Raw": "BYTES(MAX)", "Doc": "JSON", "Names": "ARRAY<STRING(MAX)>",
			"Codes": "ARRAY<STRING(8)>", "Search": "TSVECTOR", "Derived": "INT64", "Serial": "INT64", "Defaulted": "STRING(MAX)",
		}
		table := tableMap["Shapes"]
		for column, wantType := range want {
			if got := table.Columns[column].SpannerType; got != wantType {
				t.Errorf("Shapes.%s SpannerType = %q, want %q", column, got, wantType)
			}
		}
		for column, wantDefault := range map[string]bool{"Id": false, "Derived": false, "Serial": true, "Defaulted": true} {
			if got := table.Columns[column].HasDefault; got != wantDefault {
				t.Errorf("Shapes.%s HasDefault = %v, want %v", column, got, wantDefault)
			}
		}
		if table.Columns["Label"].IsNullable || !table.Columns["Body"].IsNullable {
			t.Errorf("Shapes nullability: Label nullable = %v, Body nullable = %v, want false and true", table.Columns["Label"].IsNullable, table.Columns["Body"].IsNullable)
		}
		if !table.Columns["Id"].IsPrimaryKey {
			t.Error("Shapes.Id is not a primary key")
		}
	})

	t.Run("a domain reads as its base type and an enumeration as text", func(t *testing.T) {
		t.Parallel()

		want := map[string]string{"Handle": "STRING(40)", "Feeling": "STRING(MAX)", "Feelings": "ARRAY<STRING(MAX)>"}
		for column, wantType := range want {
			if got := tableMap["Typed"].Columns[column].SpannerType; got != wantType {
				t.Errorf("Typed.%s SpannerType = %q, want %q", column, got, wantType)
			}
		}
	})

	t.Run("a foreign key is followed one more jump to its table", func(t *testing.T) {
		t.Parallel()

		// ShapeNoteTags.NoteRef refers to ShapeNotes.Id, a primary key and no foreign key:
		// the first hop stands. ShapeNotes.OrderRef refers to Orders.Id likewise.
		if got := tableMap["ShapeNoteTags"].Columns["NoteRef"]; got.ReferencedTable != "ShapeNotes" || got.ReferencedColumn != "Id" {
			t.Errorf("ShapeNoteTags.NoteRef references %s.%s, want ShapeNotes.Id", got.ReferencedTable, got.ReferencedColumn)
		}
		if got := tableMap["ShapeNotes"].Columns["OrderRef"]; got.ReferencedTable != "Orders" || got.ReferencedColumn != "Id" {
			t.Errorf("ShapeNotes.OrderRef references %s.%s, want Orders.Id", got.ReferencedTable, got.ReferencedColumn)
		}
	})

	t.Run("enumeration tables are read and the feature flags are not", func(t *testing.T) {
		t.Parallel()

		enums, err := fetchPostgresEnumValues(ctx, db.Pool)
		if err != nil {
			t.Fatalf("fetchPostgresEnumValues() error = %v", err)
		}
		want := map[string][]*enumData{"Statuses": {{ID: "closed", Description: "Closed"}, {ID: "open", Description: "Open"}}}
		if diff := cmp.Diff(want, enums); diff != "" {
			t.Errorf("fetchPostgresEnumValues() mismatch (-want +got):\n%s", diff)
		}
	})
}

// Test_structsToResources_postgres extracts resources whose fields carry the postgres tag
// alone over the schema read from PostgreSQL: each field finds its column, nullability is
// checked against the column's, the key is read from the key's position, and the index
// flags come from PostgreSQL's indexes.
func Test_structsToResources_postgres(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("the schema read requires a PostgreSQL container")
	}

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed")
	}
	migrations := "file://" + filepath.Join(filepath.Dir(thisFile), "testdata", "postgresmigrations")

	ctx := context.Background()
	db, container, err := createPostgresDB(ctx, "17", []string{migrations})
	if err != nil {
		t.Fatalf("createPostgresDB() error = %v", err)
	}
	t.Cleanup(func() {
		db.Close()
		releasePostgresContainer(context.Background(), container)
	})
	tableMap, err := createPostgresTableMap(ctx, db.Pool)
	if err != nil {
		t.Fatalf("createPostgresTableMap() error = %v", err)
	}

	c := &client{tableMap: tableMap}
	pkg := loadFixture(t, "postgresfixture")
	resources, err := c.structsToResources(pkg.Structs)
	if err != nil {
		t.Fatalf("structsToResources() error = %v", err)
	}

	type fieldFacts struct {
		column   string
		indexed  bool
		primary  bool
		nullable bool
	}
	got := make(map[string]map[string]fieldFacts)
	for _, res := range resources {
		got[res.Name()] = make(map[string]fieldFacts, len(res.Fields))
		for _, field := range res.Fields {
			got[res.Name()][field.Name()] = fieldFacts{column: fieldColumn(field), indexed: field.IsIndex, primary: field.IsPrimaryKey, nullable: field.IsNullable}
		}
	}

	want := map[string]map[string]fieldFacts{
		"Tenant": {"ID": {column: "Id", indexed: true, primary: true}},
		"Order": {
			"ID":          {column: "Id", indexed: true, primary: true},
			"TenantID":    {column: "TenantId", indexed: true},
			"PlacedAt":    {column: "PlacedAt"},
			"Reference":   {column: "Reference", indexed: true},
			"ExternalRef": {column: "ExternalRef", indexed: true, nullable: true},
			"Note":        {column: "Note", indexed: true, nullable: true},
		},
		"Seat": {
			"OrderID": {column: "OrderId", indexed: true, primary: true},
			"UserID":  {column: "UserId", primary: true},
			"Row":     {column: "Row"},
			"Note":    {column: "Note", indexed: true, nullable: true},
		},
	}
	if diff := cmp.Diff(want, got, cmp.AllowUnexported(fieldFacts{})); diff != "" {
		t.Errorf("extracted fields mismatch (-want +got):\n%s", diff)
	}
}
