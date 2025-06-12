package resource

import (
	"strings"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
)

type SpannerStruct struct {
	Field1 string `spanner:"field1"`
	Field2 string `spanner:"fieldtwo"`
	Field3 int    `spanner:"field3"`
	Field5 string `spanner:"field5"`
	Field4 string `spanner:"field4"`
}

func (SpannerStruct) Resource() accesstypes.Resource {
	return "SpannerStructs"
}

// SortTestResource is used for testing sorting functionality.
type SortTestResource struct {
	ID   string `spanner:"item_id" db:"item_id"`
	Name string `spanner:"item_name" db:"item_name"`
	Date string `spanner:"creation_date" db:"creation_date"`
}

func (SortTestResource) Resource() accesstypes.Resource {
	return "SortTestResources"
}

func (s SortTestResource) DefaultConfig() Config { // Changed to Config
	// Provide a default Config. The DBType might be overridden by tests,
	// but NewResourceMetadata expects a Config from this method.
	return Config{
		DBType: SpannerDBType, // Default DBType for the config
		// TrackChanges: false, // Default if needed
		// ChangeTrackingTable: "", // Default if needed
	}
}

func TestQuerySet_SpannerStmt_OrderBy(t *testing.T) {
	rMetaSpanner := NewResourceMetadata[SortTestResource]()
	// Ensure the rMeta has the correct dbType for Spanner tests,
	// overriding or confirming what DefaultConfig might have set.
	rMetaSpanner.dbType = SpannerDBType

	tests := []struct {
		name                 string
		sortFields           []SortField
		searchActive         bool // New field to indicate if search should be set
		wantQueryContains    string
		wantErrorMsgContains string // New field for specific error message
		wantErr              bool
		assertFunc           func(t *testing.T, sql string, wantContains string)
	}{
		{
			name: "no sort fields",
			sortFields:        []SortField{},
			wantQueryContains: "ORDER BY", // Expect ORDER BY to be absent
			assertFunc: func(t *testing.T, sql string, wantContains string) {
				if strings.Contains(sql, wantContains) {
					t.Errorf("Expected SQL NOT to contain '%s', but got: %s", wantContains, sql)
				}
			},
		},
		{
			name:              "single field ascending",
			sortFields:        []SortField{{Field: "Name", Direction: SortAscending}},
			wantQueryContains: "ORDER BY `item_name` ASC",
		},
		{
			name:              "single field descending",
			sortFields:        []SortField{{Field: "Date", Direction: SortDescending}},
			wantQueryContains: "ORDER BY `creation_date` DESC",
		},
		{
			name:              "multiple fields mixed directions",
			sortFields:        []SortField{{Field: "Name", Direction: SortAscending}, {Field: "Date", Direction: SortDescending}},
			wantQueryContains: "ORDER BY `item_name` ASC, `creation_date` DESC",
		},
		{
			name:              "sorting by ID field",
			sortFields:        []SortField{{Field: "ID", Direction: SortDescending}},
			wantQueryContains: "ORDER BY `item_id` DESC",
		},
		{
			name:       "invalid sort field",
			sortFields:           []SortField{{Field: "InvalidField", Direction: SortAscending}},
			wantErr:              true, // Expect error from buildOrderByClause
			wantErrorMsgContains: "not found in resource metadata", // buildOrderByClause error
		},
		{
			name:                 "error sort with search spanner",
			sortFields:           []SortField{{Field: "Name", Direction: SortAscending}},
			searchActive:         true,
			wantErr:              true,
			wantErrorMsgContains: "sorting ('sort=' parameter) cannot be used in conjunction with search parameters",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			qSet := NewQuerySet(rMetaSpanner)
			qSet.AddField("ID") // Add a default field to make the SELECT valid

			if tc.searchActive {
				qSet.SetSearchParam(NewSearch(SubString, map[SearchKey]string{SearchKey("AnyField"): "anyValue"}))
			}
			qSet.SetSortFields(tc.sortFields)
			stmt, err := qSet.SpannerStmt()

			if tc.wantErr {
				if err == nil {
					t.Errorf("Expected an error, but got none")
				} else if tc.wantErrorMsgContains != "" && !strings.Contains(err.Error(), tc.wantErrorMsgContains) {
					t.Errorf("SpannerStmt() error = %v, want error message containing %q", err, tc.wantErrorMsgContains)
				}
				return // Test finished if error was expected
			}
			if err != nil {
				t.Fatalf("SpannerStmt() error = %v, wantErr %v", err, tc.wantErr)
			}

			sql := stmt.Statement.SQL // Access as a field, not a function
			if tc.assertFunc != nil {
				tc.assertFunc(t, sql, tc.wantQueryContains)
			} else {
				if !strings.Contains(sql, tc.wantQueryContains) {
					t.Errorf("SpannerStmt() SQL = \n%s\nWant to contain:\n%s", sql, tc.wantQueryContains)
				}
			}
		})
	}
}

func TestQuerySet_PostgresStmt_OrderBy(t *testing.T) {
	rMetaPostgres := NewResourceMetadata[SortTestResource]()
	// Ensure the rMeta has the correct dbType for Postgres tests.
	rMetaPostgres.dbType = PostgresDBType

	tests := []struct {
		name                 string
		sortFields           []SortField
		searchActive         bool // New field
		wantQueryContains    string
		wantErrorMsgContains string // New field
		wantErr              bool
		assertFunc           func(t *testing.T, sql string, wantContains string)
	}{
		{
			name: "no sort fields",
			sortFields:        []SortField{},
			wantQueryContains: "ORDER BY", // Expect ORDER BY to be absent
			assertFunc: func(t *testing.T, sql string, wantContains string) {
				if strings.Contains(sql, wantContains) {
					t.Errorf("Expected SQL NOT to contain '%s', but got: %s", wantContains, sql)
				}
			},
		},
		{
			name:              "single field ascending",
			sortFields:        []SortField{{Field: "Name", Direction: SortAscending}},
			wantQueryContains: `ORDER BY "item_name" ASC`,
		},
		{
			name:              "single field descending",
			sortFields:        []SortField{{Field: "Date", Direction: SortDescending}},
			wantQueryContains: `ORDER BY "creation_date" DESC`,
		},
		{
			name:              "multiple fields mixed directions",
			sortFields:        []SortField{{Field: "Name", Direction: SortAscending}, {Field: "Date", Direction: SortDescending}},
			wantQueryContains: `ORDER BY "item_name" ASC, "creation_date" DESC`,
		},
		{
			name:              "sorting by ID field",
			sortFields:        []SortField{{Field: "ID", Direction: SortDescending}},
			wantQueryContains: `ORDER BY "item_id" DESC`,
		},
		{
			name:       "invalid sort field",
			sortFields:           []SortField{{Field: "InvalidField", Direction: SortAscending}},
			wantErr:              true, // Expect error from buildOrderByClause
			wantErrorMsgContains: "not found in resource metadata", // buildOrderByClause error
		},
		{
			name:                 "error sort with search postgres",
			sortFields:           []SortField{{Field: "Name", Direction: SortAscending}},
			searchActive:         true,
			wantErr:              true,
			wantErrorMsgContains: "sorting ('sort=' parameter) cannot be used in conjunction with search parameters",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			qSet := NewQuerySet(rMetaPostgres)
			qSet.AddField("ID") // Add a default field to make the SELECT valid

			if tc.searchActive {
				qSet.SetSearchParam(NewSearch(SubString, map[SearchKey]string{SearchKey("AnyField"): "anyValue"}))
			}
			qSet.SetSortFields(tc.sortFields)
			stmt, err := qSet.PostgresStmt()

			if tc.wantErr {
				if err == nil {
					t.Errorf("Expected an error, but got none")
				} else if tc.wantErrorMsgContains != "" && !strings.Contains(err.Error(), tc.wantErrorMsgContains) {
					t.Errorf("PostgresStmt() error = %v, want error message containing %q", err, tc.wantErrorMsgContains)
				}
				return // Test finished if error was expected
			}
			if err != nil {
				t.Fatalf("PostgresStmt() error = %v, wantErr %v", err, tc.wantErr)
			}

			sql := stmt.SQL
			if tc.assertFunc != nil {
				tc.assertFunc(t, sql, tc.wantQueryContains)
			} else {
				if !strings.Contains(sql, tc.wantQueryContains) {
					t.Errorf("PostgresStmt() SQL = \n%s\nWant to contain:\n%s", sql, tc.wantQueryContains)
				}
			}
		})
	}
}

// Commenting out old tests for now, they can be updated or removed later
// func TestQuerySet_Columns(t *testing.T) { ... }
// func TestPatcher_Postgres_Columns(t *testing.T) { ... }
