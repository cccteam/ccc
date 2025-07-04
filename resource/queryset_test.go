package resource

import (
	"strings"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
)

// SortTestResource is used for testing sorting functionality.
type SortTestResource struct {
	ID   string `spanner:"Id"   db:"Id"`
	Name string `spanner:"Name" db:"Name"`
	Date string `spanner:"Date" db:"Date"`
}

func (SortTestResource) Resource() accesstypes.Resource {
	return "SortTestResources"
}

func (s SortTestResource) DefaultConfig() Config {
	return Config{
		DBType: SpannerDBType,
	}
}

func TestQuerySet_SpannerStmt_OrderBy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                 string
		sortFields           []SortField
		wantQueryContains    string
		wantErrorMsgContains string
		wantErr              bool
		assertFunc           func(t *testing.T, sql string, wantContains string)
	}{
		{
			name:              "no sort fields",
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
			wantQueryContains: "ORDER BY `Name` ASC",
		},
		{
			name:              "single field descending",
			sortFields:        []SortField{{Field: "Date", Direction: SortDescending}},
			wantQueryContains: "ORDER BY `Date` DESC",
		},
		{
			name:              "multiple fields mixed directions",
			sortFields:        []SortField{{Field: "Name", Direction: SortAscending}, {Field: "Date", Direction: SortDescending}},
			wantQueryContains: "ORDER BY `Name` ASC, `Date` DESC",
		},
		{
			name:              "sorting by ID field",
			sortFields:        []SortField{{Field: "ID", Direction: SortDescending}},
			wantQueryContains: "ORDER BY `Id` DESC",
		},
		{
			name:                 "invalid sort field",
			sortFields:           []SortField{{Field: "InvalidField", Direction: SortAscending}},
			wantErr:              true,                             // Expect error from buildOrderByClause
			wantErrorMsgContains: "not found in resource metadata", // buildOrderByClause error
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rMetaPostgres := NewResourceMetadata[SortTestResource]()
			rMetaPostgres.dbType = SpannerDBType

			qSet := NewQuerySet(rMetaPostgres)
			qSet.AddField("ID")

			qSet.SetSortFields(tt.sortFields)
			stmt, err := qSet.SpannerStmt()

			if tt.wantErr {
				if err == nil {
					t.Errorf("Expected an error, but got none")
				} else if tt.wantErrorMsgContains != "" && !strings.Contains(err.Error(), tt.wantErrorMsgContains) {
					t.Errorf("SpannerStmt() error = %v, want error message containing %q", err, tt.wantErrorMsgContains)
				}

				return // Test finished if error was expected
			}
			if err != nil {
				t.Fatalf("SpannerStmt() error = %v, wantErr %v", err, tt.wantErr)
			}

			sql := stmt.Statement.SQL // Access as a field, not a function
			if tt.assertFunc != nil {
				tt.assertFunc(t, sql, tt.wantQueryContains)
			} else {
				if !strings.Contains(sql, tt.wantQueryContains) {
					t.Errorf("SpannerStmt() SQL = \n%s\nWant to contain:\n%s", sql, tt.wantQueryContains)
				}
			}
		})
	}
}

func TestQuerySet_PostgresStmt_OrderBy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                 string
		sortFields           []SortField
		wantQueryContains    string
		wantErrorMsgContains string
		wantErr              bool
		assertFunc           func(t *testing.T, sql string, wantContains string)
	}{
		{
			name:              "no sort fields",
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
			wantQueryContains: `ORDER BY "Name" ASC`,
		},
		{
			name:              "single field descending",
			sortFields:        []SortField{{Field: "Date", Direction: SortDescending}},
			wantQueryContains: `ORDER BY "Date" DESC`,
		},
		{
			name:              "multiple fields mixed directions",
			sortFields:        []SortField{{Field: "Name", Direction: SortAscending}, {Field: "Date", Direction: SortDescending}},
			wantQueryContains: `ORDER BY "Name" ASC, "Date" DESC`,
		},
		{
			name:              "sorting by ID field",
			sortFields:        []SortField{{Field: "ID", Direction: SortDescending}},
			wantQueryContains: `ORDER BY "Id" DESC`,
		},
		{
			name:                 "invalid sort field",
			sortFields:           []SortField{{Field: "InvalidField", Direction: SortAscending}},
			wantErr:              true,                             // Expect error from buildOrderByClause
			wantErrorMsgContains: "not found in resource metadata", // buildOrderByClause error
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rMetaPostgres := NewResourceMetadata[SortTestResource]()
			rMetaPostgres.dbType = PostgresDBType

			qSet := NewQuerySet(rMetaPostgres)
			qSet.AddField("ID") // Add a default field to make the SELECT valid

			qSet.SetSortFields(tt.sortFields)
			stmt, err := qSet.PostgresStmt()
			if tt.wantErr {
				if err == nil {
					t.Errorf("Expected an error, but got none")
				} else if tt.wantErrorMsgContains != "" && !strings.Contains(err.Error(), tt.wantErrorMsgContains) {
					t.Errorf("PostgresStmt() error = %v, want error message containing %q", err, tt.wantErrorMsgContains)
				}

				return // Test finished if error was expected
			}
			if err != nil {
				t.Fatalf("PostgresStmt() error = %v, wantErr %v", err, tt.wantErr)
			}

			sql := stmt.SQL
			if tt.assertFunc != nil {
				tt.assertFunc(t, sql, tt.wantQueryContains)
			} else {
				if !strings.Contains(sql, tt.wantQueryContains) {
					t.Errorf("PostgresStmt() SQL = \n%s\nWant to contain:\n%s", sql, tt.wantQueryContains)
				}
			}
		})
	}
}
