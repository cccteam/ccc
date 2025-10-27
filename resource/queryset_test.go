package resource

import (
	"strings"
	"testing"

	"github.com/cccteam/ccc"
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
	return Config{}
}

func TestQuerySet_Stmt_OrderBy_Limit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                 string
		sortFields           []SortField
		limit                *uint64
		wantQueryContains    string
		wantErrorMsgContains string
		wantErr              bool
		assertFunc           func(t *testing.T, sql string, _ string)
	}{
		{
			name:              "with limit",
			limit:             ccc.Ptr(uint64(10)),
			wantQueryContains: "LIMIT 10",
		},
		{
			name:       "no sort fields or limit",
			sortFields: []SortField{},
			assertFunc: func(t *testing.T, sql string, _ string) {
				if strings.Contains(sql, "ORDER BY") {
					t.Errorf("Expected SQL NOT to contain 'ORDER BY', but got: %s", sql)
				}
				if strings.Contains(sql, "LIMIT") {
					t.Errorf("Expected SQL NOT to contain 'LIMIT', but got: %s", sql)
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

			rMeta := NewMetadata[SortTestResource]()

			qSet := NewQuerySet(rMeta)
			qSet.AddField("ID")

			qSet.SetSortFields(tt.sortFields)
			if tt.limit != nil {
				qSet.SetLimit(tt.limit)
			}
			stmt, err := qSet.stmt(SpannerDBType)

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

			sql := stmt.SQL // Access as a field, not a function
			if tt.assertFunc != nil {
				tt.assertFunc(t, sql, tt.wantQueryContains)
			} else if !strings.Contains(sql, tt.wantQueryContains) {
				t.Errorf("SpannerStmt() SQL = \n%s\nWant to contain:\n%s", sql, tt.wantQueryContains)
			}
		})
	}
}
