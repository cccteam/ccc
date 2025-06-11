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
	Name               string   `json:"name"                 index:"true"`
	Age                int      `json:"age"                  index:"true"`
	Status             string   `json:"status"`
	Email              *string  `json:"email"                index:"true"`
	Salary             float64  `json:"salary"               index:"true"`
	IsActive           bool     `json:"active"               index:"true"`
	ItemIDs            []int    `json:"ids"                  index:"true"`
	Tags               []string `json:"names"                index:"true"`
	LegacyIndexedField string   `json:"legacy_indexed_field" index:"true"`
}

func TestQueryDecoder_parseQuery_Refactored(t *testing.T) {
	test := []struct {
		name                 string
		queryValues          url.Values
		wantErr              bool
		expectedASTString    string
		expectedFilterSet    *Filter
		expectedColumnFields []accesstypes.Field
		expectedErrMsg       string
		expectConflictError  bool
	}{
		// 1. Columns processing first
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
			name:                 "columns with legacy filter",
			queryValues:          url.Values{"columns": []string{"name"}, "legacy_indexed_field": []string{"value"}},
			wantErr:              false,
			expectedColumnFields: []accesstypes.Field{"Name"},
			expectedFilterSet: NewFilter(
				Index,
				map[FilterKey]string{FilterKey("legacy_indexed_field_sql"): "value"},
				map[FilterKey]reflect.Kind{FilterKey("legacy_indexed_field_sql"): reflect.String},
			),
		},
		{
			name:                 "invalid column name",
			queryValues:          url.Values{"columns": []string{"name,nonexistent"}},
			wantErr:              true,
			expectedErrMsg:       "unknown column: nonexistent",
			expectedColumnFields: nil,
		},

		// 2. "filter" parameter processing
		{
			name:              "valid filter only - string",
			queryValues:       url.Values{"filter": []string{"name:eq:John"}},
			wantErr:           false,
			expectedASTString: "name_sql:eq:John",
		},
		{
			name:           "invalid filter only",
			queryValues:    url.Values{"filter": []string{"name:badop:John"}},
			wantErr:        true,
			expectedErrMsg: "unknown operator 'badop' in condition 'name:badop:John'",
		},

		// 3. Legacy filter parameter processing
		{
			name:        "legacy filter only",
			queryValues: url.Values{"legacy_indexed_field": []string{"value"}},
			wantErr:     false,
			expectedFilterSet: NewFilter(
				Index,
				map[FilterKey]string{FilterKey("legacy_indexed_field_sql"): "value"},
				map[FilterKey]reflect.Kind{FilterKey("legacy_indexed_field_sql"): reflect.String},
			),
		},

		// 4. Conflict Check
		{
			name:                "conflict between filter and legacy filter",
			queryValues:         url.Values{"filter": []string{"name:eq:John"}, "legacy_indexed_field": []string{"value"}},
			wantErr:             true,
			expectedASTString:   "name_sql:eq:John",
			expectConflictError: true,
			expectedErrMsg:      "cannot use 'filter' parameter alongside other legacy filterable field parameters",
		},

		// 5. Interaction and error propagation
		{
			name:           "invalid filter with legacy filter present",
			queryValues:    url.Values{"filter": []string{"name:badop:John"}, "legacy_indexed_field": []string{"value"}},
			wantErr:        true,
			expectedErrMsg: "unknown operator 'badop' in condition 'name:badop:John'",
		},
		{
			name:              "valid filter with unknown parameter",
			queryValues:       url.Values{"filter": []string{"name:eq:John"}, "unknown": []string{"value"}},
			wantErr:           true,
			expectedASTString: "name_sql:eq:John",
			expectedErrMsg:    "unknown query parameters: map[unknown:[value]]",
		},
		{
			name:        "legacy filter with unknown parameter",
			queryValues: url.Values{"legacy_indexed_field": []string{"value"}, "unknown": []string{"value"}},
			wantErr:     true,
			expectedFilterSet: NewFilter(
				Index,
				map[FilterKey]string{FilterKey("legacy_indexed_field_sql"): "value"},
				map[FilterKey]reflect.Kind{FilterKey("legacy_indexed_field_sql"): reflect.String},
			),
			expectedErrMsg: "unknown query parameters: map[unknown:[value]]",
		},
		{
			name:                 "columns, valid filter, legacy filter (conflict), and unknown param",
			queryValues:          url.Values{"columns": []string{"age"}, "filter": []string{"name:eq:John"}, "legacy_indexed_field": []string{"value"}, "unknown": []string{"value"}},
			wantErr:              true,
			expectedColumnFields: nil,
			expectedASTString:    "name_sql:eq:John",
			expectedFilterSet:    nil,
			expectConflictError:  true,
			expectedErrMsg:       "cannot use 'filter' parameter alongside other legacy filterable field parameters",
		},
		{
			name:        "empty filter string with legacy filter",
			queryValues: url.Values{"filter": []string{""}, "legacy_indexed_field": []string{"value"}},
			wantErr:     true,
			expectedFilterSet: NewFilter(
				Index,
				map[FilterKey]string{FilterKey("legacy_indexed_field_sql"): "value"},
				map[FilterKey]reflect.Kind{FilterKey("legacy_indexed_field_sql"): reflect.String},
			),
			expectedErrMsg: "unknown query parameters: map[filter:[]]",
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

			columnFields, filterSet, parsedAST, err := decoder.parseQuery(tt.queryValues)

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

			// Check FilterSet
			if tt.expectedFilterSet != nil {
				if filterSet == nil {
					// Only error if we didn't expect an error that might prevent FilterSet parsing.
					// Or if it's a conflict error where FilterSet is expected.
					if !tt.wantErr || (tt.expectConflictError && tt.expectedErrMsg == "cannot use 'filter' parameter alongside other legacy filterable field parameters") {
						t.Errorf("filterSet is nil for test '%s', but expected: %#v", tt.name, tt.expectedFilterSet)
					}
				} else {
					if tt.expectedFilterSet.typ != filterSet.typ {
						t.Errorf("FilterSet Type mismatch for test '%s':\nExpected: %v\nActual:   %v", tt.name, tt.expectedFilterSet.typ, filterSet.typ)
					}
					// Optionally, a more focused check on values if essential for the test
					if !reflect.DeepEqual(tt.expectedFilterSet.values, filterSet.values) {
						t.Errorf("FilterSet Values mismatch for test '%s':\nExpected: %#v\nActual:   %#v", tt.name, tt.expectedFilterSet.values, filterSet.values)
					}
				}
			} else { // tt.expectedFilterSet == nil
				// If no FilterSet is expected, and no error occurred (or error occurred before FilterSet parsing),
				// then filterSet should be nil.
				// Exception: conflict error might parse FilterSet before detecting conflict.
				if filterSet != nil && !tt.wantErr {
					if !(tt.expectConflictError && err != nil && strings.Contains(err.Error(), "cannot use 'filter' parameter")) {
						t.Errorf("filterSet should be nil for test '%s', got: %#v", tt.name, filterSet)
					}
				}
				// Removed extra closing brace that was here
			}
		})
	}
}
