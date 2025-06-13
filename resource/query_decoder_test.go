package resource

import (
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
)

type TestResource struct {
	ID                 string   `spanner:"id"`
	Name               string   `spanner:"name_sql"`
	Age                int      `spanner:"age_sql"`
	Status             string   `spanner:"status_sql"`
	Email              *string  `spanner:"email_sql"`
	Salary             float64  `spanner:"salary_sql"`
	IsActive           bool     `spanner:"active_sql"`
	ItemIDs            []int    `spanner:"item_ids_sql"`
	Tags               []string `spanner:"tags_sql"`
	LegacyIndexedField string   `spanner:"legacy_indexed_field_sql"`
}

func (tr TestResource) Resource() accesstypes.Resource { return "testresources" }
func (tr TestResource) DefaultConfig() Config {
	return Config{
		DBType:              SpannerDBType,
		ChangeTrackingTable: "",
		TrackChanges:        false,
	}
}

type TestRequest struct {
	Name               string   `json:"name"                 index:"true"  substring:"SearchTokens"`
	Age                int      `json:"age"                  index:"true"`
	Status             string   `json:"status"`
	Email              *string  `json:"email"                index:"true"`
	Salary             float64  `json:"salary"               index:"true"`
	IsActive           bool     `json:"active"               index:"true"`
	ItemIDs            []int    `json:"ids"                  index:"true"`
	Tags               []string `json:"names"                index:"true"`
	LegacyIndexedField string   `json:"legacy_indexed_field" index:"true"`
}

func TestQueryDecoder_parseQuery(t *testing.T) {
	test := []struct {
		name                 string
		queryValues          url.Values
		wantErr              bool
		expectedASTString    string
		expectedSearchSet    *Search
		expectedColumnFields []accesstypes.Field
		expectedSortFields   []SortField
		expectedErrMsg       string
		expectConflictError  bool
	}{
		// Columns processing first
		{
			name:                 "columns only",
			queryValues:          url.Values{"columns": []string{"name,age"}},
			wantErr:              false,
			expectedColumnFields: []accesstypes.Field{"Name", "Age"},
		},
		{
			name:                 "columns with valid filter",
			queryValues:          url.Values{"columns": {"name"}, "filter": {"age:gt:30"}},
			wantErr:              false,
			expectedColumnFields: []accesstypes.Field{"Name"},
			expectedASTString:    "age_sql:gt:30",
		},
		{
			name:           "columns with legacy filter (now unsupported)",
			queryValues:    url.Values{"columns": []string{"name"}, "legacy_indexed_field": []string{"value"}},
			wantErr:        true,
			expectedErrMsg: "unknown query parameters",
		},
		{
			name:           "invalid column name",
			queryValues:    url.Values{"columns": []string{"name,nonexistent"}},
			wantErr:        true,
			expectedErrMsg: "unknown column: nonexistent",
		},

		// "filter" parameter processing
		{
			name:              "valid filter only - string",
			queryValues:       url.Values{"filter": []string{"name:eq:John"}},
			wantErr:           false,
			expectedASTString: "name_sql:eq:John",
		},
		{
			name:        "valid search",
			queryValues: url.Values{"SearchTokens": []string{"find this and this"}},
			wantErr:     false,
			expectedSearchSet: &Search{
				typ: SubString,
				values: map[SearchKey]string{
					"SearchTokens": "find this and this",
				},
			},
		},
		{
			name:           "invalid filter only",
			queryValues:    url.Values{"filter": []string{"name:badop:John"}},
			wantErr:        true,
			expectedErrMsg: "unknown operator 'badop' in condition 'name:badop:John'",
		},

		// Conflict Check (legacy_indexed_field is now an unknown parameter)
		{
			name:           "filter with legacy field (now unknown param)",
			queryValues:    url.Values{"filter": []string{"name:eq:John"}, "legacy_indexed_field": []string{"value"}},
			wantErr:        true,
			expectedErrMsg: "unknown query parameters",
		},
		{
			name:           "filter with search parameter",
			queryValues:    url.Values{"filter": []string{"name:eq:John"}, "SearchTokens": []string{"find this and this"}},
			wantErr:        true,
			expectedErrMsg: "cannot use 'filter' parameter alongside 'search' parameter",
		},

		// Interaction and error propagation
		{
			name:           "invalid filter with legacy field (now unknown param)",
			queryValues:    url.Values{"filter": []string{"name:badop:John"}, "legacy_indexed_field": []string{"value"}},
			wantErr:        true,
			expectedErrMsg: "unknown operator 'badop' in condition 'name:badop:John'",
		},
		{
			name:           "valid filter with unknown parameter",
			queryValues:    url.Values{"filter": []string{"name:eq:John"}, "unknown": []string{"value"}},
			wantErr:        true,
			expectedErrMsg: "unknown query parameters: map[unknown:[value]]",
		},
		{
			name:           "legacy field (now unknown) with another unknown parameter",
			queryValues:    url.Values{"legacy_indexed_field": []string{"value"}, "unknown": []string{"value"}},
			wantErr:        true,
			expectedErrMsg: "unknown query parameters",
		},
		{
			name:           "columns, valid filter, legacy field (now unsupported), and unknown param",
			queryValues:    url.Values{"columns": []string{"age"}, "filter": []string{"name:eq:John"}, "legacy_indexed_field": []string{"value"}, "unknown": []string{"value"}},
			wantErr:        true,
			expectedErrMsg: "unknown query parameters",
		},
		{
			name:           "empty filter string with legacy field (now unsupported)",
			queryValues:    url.Values{"filter": []string{""}, "legacy_indexed_field": []string{"value"}},
			wantErr:        true,
			expectedErrMsg: "unknown query parameters",
		},
		{
			name:              "integer equality",
			queryValues:       url.Values{"filter": []string{"age:eq:42"}},
			wantErr:           false,
			expectedASTString: "age_sql:eq:42",
		},
		{
			name:              "boolean true equality",
			queryValues:       url.Values{"filter": []string{"active:eq:true"}},
			wantErr:           false,
			expectedASTString: "active_sql:eq:true",
		},
		{
			name:              "boolean false equality",
			queryValues:       url.Values{"filter": []string{"active:eq:false"}},
			wantErr:           false,
			expectedASTString: "active_sql:eq:false",
		},
		{
			name:              "float GTE",
			queryValues:       url.Values{"filter": []string{"salary:gte:5000.75"}},
			wantErr:           false,
			expectedASTString: "salary_sql:gte:5000.75",
		},
		{
			name:              "IN list of integers",
			queryValues:       url.Values{"filter": []string{"ids:in:(1,2,3)"}},
			wantErr:           false,
			expectedASTString: "item_ids_sql:in:(1,2,3)",
		},
		{
			name:              "IN list of strings",
			queryValues:       url.Values{"filter": []string{"names:in:(Alice,Bob,Charlie)"}},
			wantErr:           false,
			expectedASTString: "tags_sql:in:(Alice,Bob,Charlie)",
		},
		{
			name:              "ISNULL operator",
			queryValues:       url.Values{"filter": []string{"email:isnull"}},
			wantErr:           false,
			expectedASTString: "email_sql:isnull",
		},
		{
			name:              "ISNOTNULL operator",
			queryValues:       url.Values{"filter": []string{"email:isnotnull"}},
			wantErr:           false,
			expectedASTString: "email_sql:isnotnull",
		},
		{
			name:           "index error - status is not indexed, cant filter on it",
			queryValues:    url.Values{"filter": []string{"status:eq:active"}},
			wantErr:        true,
			expectedErrMsg: "'status' is not indexed but was included in condition 'status:eq:active'",
		},
		{
			name:           "conversion error - int",
			queryValues:    url.Values{"filter": []string{"age:eq:notanint"}},
			wantErr:        true,
			expectedErrMsg: "value 'notanint' in condition 'age:eq:notanint' is not a valid integer",
		},
		{
			name:           "conversion error - bool",
			queryValues:    url.Values{"filter": []string{"active:eq:notabool"}},
			wantErr:        true,
			expectedErrMsg: "value 'notabool' in condition 'active:eq:notabool' is not a valid boolean",
		},
		{
			name:           "conversion error - float",
			queryValues:    url.Values{"filter": []string{"salary:eq:notafloat"}},
			wantErr:        true,
			expectedErrMsg: "value 'notafloat' in condition 'salary:eq:notafloat' is not a valid float",
		},

		// Sort parameter processing
		{
			name:               "sort single field default direction",
			queryValues:        url.Values{"sort": []string{"name"}},
			wantErr:            false,
			expectedSortFields: []SortField{{Field: "Name", Direction: SortAscending}},
		},
		{
			name:               "sort single field asc",
			queryValues:        url.Values{"sort": []string{"name:asc"}},
			wantErr:            false,
			expectedSortFields: []SortField{{Field: "Name", Direction: SortAscending}},
		},
		{
			name:               "sort single field desc",
			queryValues:        url.Values{"sort": []string{"age:desc"}},
			wantErr:            false,
			expectedSortFields: []SortField{{Field: "Age", Direction: SortDescending}},
		},
		{
			name:               "sort multi-field",
			queryValues:        url.Values{"sort": []string{"name:asc,age:desc"}},
			wantErr:            false,
			expectedSortFields: []SortField{{Field: "Name", Direction: SortAscending}, {Field: "Age", Direction: SortDescending}},
		},
		{
			name:               "sort multi-field with spaces",
			queryValues:        url.Values{"sort": []string{" name : asc , age : desc "}},
			wantErr:            false,
			expectedSortFields: []SortField{{Field: "Name", Direction: SortAscending}, {Field: "Age", Direction: SortDescending}},
		},
		{
			name:           "sort invalid field name",
			queryValues:    url.Values{"sort": []string{"nonexistent:asc"}},
			wantErr:        true,
			expectedErrMsg: "unknown sort field: nonexistent",
		},
		{
			name:               "sort legacy_indexed_field (is sortable)",
			queryValues:        url.Values{"sort": []string{"legacy_indexed_field:asc"}},
			wantErr:            false,
			expectedSortFields: []SortField{{Field: "LegacyIndexedField", Direction: SortAscending}},
		},
		{
			name:           "sort invalid direction",
			queryValues:    url.Values{"sort": []string{"name:invalid"}},
			wantErr:        true,
			expectedErrMsg: "invalid sort direction for field 'name': invalid. Must be 'asc' or 'desc'",
		},
		{
			name:           "sort with search parameter",
			queryValues:    url.Values{"sort": []string{"name:asc"}, "SearchTokens": []string{"find this"}},
			wantErr:        true,
			expectedErrMsg: "sorting ('sort=' parameter) cannot be used in conjunction with search parameters",
		},
		{
			name:           "sort empty parameter",
			queryValues:    url.Values{"sort": []string{""}},
			wantErr:        true,
			expectedErrMsg: "unknown query parameters: map[sort:[]]",
		},
		{
			name:           "sort with empty parts",
			queryValues:    url.Values{"sort": []string{"name,,age"}},
			wantErr:        true,
			expectedErrMsg: "invalid sort field, found empty part in sort parameter: name,,age",
		},
		{
			name:           "sort with leading comma",
			queryValues:    url.Values{"sort": []string{",name"}},
			wantErr:        true,
			expectedErrMsg: "invalid sort field, found empty part in sort parameter: ,name",
		},
		{
			name:           "sort with only colon",
			queryValues:    url.Values{"sort": []string{":"}},
			wantErr:        true,
			expectedErrMsg: "sort field name cannot be empty",
		},
		{
			name:           "sort field name ends with colon",
			queryValues:    url.Values{"sort": []string{"name:"}},
			wantErr:        true,
			expectedErrMsg: "invalid sort direction for field 'name': . Must be 'asc' or 'desc'",
		},
	}

	for _, tt := range test {
		t.Run(tt.name, func(t *testing.T) {
			resSet, err := NewResourceSet[TestResource, TestRequest]()
			if err != nil {
				t.Fatalf("Failed to create ResourceSet for test case %s: %v", tt.name, err)
			}
			decoder, err := NewQueryDecoder[TestResource, TestRequest](resSet)
			if err != nil {
				t.Fatalf("NewQueryDecoder should not fail with default setup for test case %s: %v", tt.name, err)
			}

			columnFields, sortFields, searchSet, parsedAST, err := decoder.parseQuery(tt.queryValues)

			// Check sortFields
			if !reflect.DeepEqual(tt.expectedSortFields, sortFields) {
				// Handle special case for error expectations:
				// If wantErr is true and expectedSortFields is nil (common for error cases where parsing might not complete or is irrelevant),
				// and actual sortFields is also nil or empty, treat as equal. This simplifies test case definitions for errors.
				isNilOrEmptySortField := func(s []SortField) bool { return s == nil || len(s) == 0 }
				if !(tt.wantErr && isNilOrEmptySortField(tt.expectedSortFields) && isNilOrEmptySortField(sortFields)) {
					t.Errorf("SortFields mismatch for test '%s':\nExpected: %#v\nActual:   %#v", tt.name, tt.expectedSortFields, sortFields)
				}
			}

			if tt.wantErr {
				if err == nil {
					t.Errorf("Expected an error for test '%s', got nil", tt.name)
				} else {
					if tt.expectedErrMsg != "" && !strings.Contains(err.Error(), tt.expectedErrMsg) {
						t.Errorf("Error message mismatch for test '%s':\nExpected to contain: %s\nActual error: %s", tt.name, tt.expectedErrMsg, err.Error())
					}
					// Specific check for conflict error message is implicitly covered if tt.expectedErrMsg is set to the conflict message.
					// The tt.expectConflictError flag can be used to ensure that *if* an error occurs, it *is* the conflict error.
					if tt.expectConflictError && !strings.Contains(err.Error(), "cannot use 'filter' parameter alongside other legacy filterable field parameters") {
						t.Errorf("Expected conflict error for test '%s', but got: %s", tt.name, err.Error())
					}
				}
			} else { // !tt.wantErr
				if err != nil {
					t.Errorf("Did not expect an error for test '%s', got: %v", tt.name, err)
				}
			}

			// Check columnFields
			// Use reflect.DeepEqual for slice comparison.
			if !reflect.DeepEqual(tt.expectedColumnFields, columnFields) {
				// Handle special case: if wantErr and expectedColumnFields is nil (default for error cases),
				// and actual is empty slice, treat as equal. This simplifies test case definitions.
				isNilOrEmpty := func(s []accesstypes.Field) bool { return s == nil || len(s) == 0 }
				if !(tt.wantErr && isNilOrEmpty(tt.expectedColumnFields) && isNilOrEmpty(columnFields)) {
					t.Errorf("ColumnFields mismatch for test '%s':\nExpected: %#v\nActual:   %#v", tt.name, tt.expectedColumnFields, columnFields)
				}
			}

			// Check AST string representation
			if tt.expectedASTString != "" {
				if parsedAST == nil {
					if !tt.wantErr { // Only error if we didn't expect an error that might prevent AST parsing
						t.Errorf("parsedAST is nil for test '%s', but expected AST string: %s", tt.name, tt.expectedASTString)
					}
				} else if actualASTString := parsedAST.String(); actualASTString != tt.expectedASTString {
					t.Errorf("AST string representation mismatch for test '%s':\nExpected: %s\nActual:   %s", tt.name, tt.expectedASTString, actualASTString)
				}
			} else if parsedAST != nil && !tt.wantErr { // If no AST string is expected and no error, AST should be nil
				// Exception: conflict error might parse AST before detecting conflict.
				// If tt.expectConflictError is true, parsedAST might be non-nil.
				if !(tt.expectConflictError && err != nil && strings.Contains(err.Error(), "cannot use 'filter' parameter")) {
					t.Errorf("Expected nil parsedAST for test '%s' (no expected AST string and no error), got: %s", tt.name, parsedAST.String())
				}
			}

			// Check SearchSet
			if tt.expectedSearchSet != nil {
				if searchSet == nil {
					// Only error if we didn't expect an error that might prevent FilterSet parsing.
					// Or if it's a conflict error where FilterSet is expected.
					if !tt.wantErr || (tt.expectConflictError && tt.expectedErrMsg == "cannot use 'filter' parameter alongside other legacy filterable field parameters") {
						t.Errorf("searchSet is nil for test '%s', but expected: %#v", tt.name, tt.expectedSearchSet)
					}
				} else {
					if tt.expectedSearchSet.typ != searchSet.typ {
						t.Errorf("FilterSet Type mismatch for test '%s':\nExpected: %v\nActual:   %v", tt.name, tt.expectedSearchSet.typ, searchSet.typ)
					}
					// Optionally, a more focused check on values if essential for the test
					if !reflect.DeepEqual(tt.expectedSearchSet.values, searchSet.values) {
						t.Errorf("FilterSet Values mismatch for test '%s':\nExpected: %#v\nActual:   %#v", tt.name, tt.expectedSearchSet.values, searchSet.values)
					}
				}
			} else { // tt.expectedsearchSet == nil
				// If no SearchSet is expected, and no error occurred (or error occurred before FilterSet parsing),
				// then searchSet should be nil.
				// Exception: conflict error might parse FilterSet before detecting conflict.
				if searchSet != nil && !tt.wantErr {
					if !(tt.expectConflictError && err != nil && strings.Contains(err.Error(), "cannot use 'filter' parameter")) {
						t.Errorf("searchSet should be nil for test '%s', got: %#v", tt.name, searchSet)
					}
				}
			}
		})
	}
}
