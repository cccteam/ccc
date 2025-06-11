package resource

import (
	"net/url"
	"reflect"
	"strings" // Added for strings.Contains
	"testing"

	"github.com/cccteam/ccc/accesstypes"
	// Removed testify imports
)

// --- Test Structures ---

type TestResource struct {
	ID                 string  `spanner:"id"`
	Name               string  `spanner:"name_sql"`
	Age                int     `spanner:"age_sql"`
	Status             string  `spanner:"status_sql"`
	Email              *string `spanner:"email_sql"`
	Salary             float64 `spanner:"salary_sql"`
	LegacyIndexedField string  `spanner:"legacy_indexed_field_sql"`
}

func (tr TestResource) Resource() accesstypes.Resource {
	return "testresources"
}

func (tr TestResource) DefaultConfig() Config {
	return Config{
		DBType:              SpannerDBType,
		ChangeTrackingTable: "",
		TrackChanges:        false,
	}
}

type TestRequest struct {
	Name               string  `json:"name"`
	Age                int     `json:"age"`
	Status             string  `json:"status"`
	Email              *string `json:"email"`
	Salary             float64 `json:"salary"`
	LegacyIndexedField string  `json:"legacy_indexed_field" index:"true"`
}

func TestQueryDecoder_parseQuery_Refactored(t *testing.T) {
	tests := []struct {
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
			queryValues:          url.Values{"columns": {"name,age"}},
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
			queryValues:          url.Values{"columns": {"name"}, "legacy_indexed_field": {"value"}},
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
			queryValues:          url.Values{"columns": {"name,nonexistent"}},
			wantErr:              true,
			expectedErrMsg:       "unknown column: nonexistent",
			expectedColumnFields: nil,
		},

		// 2. "filter" parameter processing
		{
			name:              "valid filter only",
			queryValues:       url.Values{"filter": {"name:eq:John"}},
			wantErr:           false,
			expectedASTString: "name_sql:eq:John",
		},
		{
			name:           "invalid filter only",
			queryValues:    url.Values{"filter": {"name:badop:John"}},
			wantErr:        true,
			expectedErrMsg: "parseConditionToken error='badop' in condition 'name:badop:John'",
		},

		// 3. Legacy filter parameter processing
		{
			name:        "legacy filter only",
			queryValues: url.Values{"legacy_indexed_field": {"value"}},
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
			queryValues:         url.Values{"filter": {"name:eq:John"}, "legacy_indexed_field": {"value"}},
			wantErr:             true,
			expectConflictError: true,
			expectedErrMsg:      "cannot use 'filter' parameter alongside other legacy filterable field parameters",
		},

		// 5. Interaction and error propagation
		{
			name:           "invalid filter with legacy filter present",
			queryValues:    url.Values{"filter": {"name:badop:John"}, "legacy_indexed_field": {"value"}},
			wantErr:        true,
			expectedErrMsg: "parseConditionToken error='badop' in condition 'name:badop:John'",
		},
		{
			name:           "valid filter with unknown parameter",
			queryValues:    url.Values{"filter": {"name:eq:John"}, "unknown": {"value"}},
			wantErr:        true,
			expectedErrMsg: "unknown query parameters: map[unknown:[value]]",
		},
		{
			name:           "legacy filter with unknown parameter",
			queryValues:    url.Values{"legacy_indexed_field": {"value"}, "unknown": {"value"}},
			wantErr:        true,
			expectedErrMsg: "unknown query parameters: map[unknown:[value]]",
		},
		{
			name:                "columns, valid filter, legacy filter (conflict), and unknown param",
			queryValues:         url.Values{"columns": {"age"}, "filter": {"name:eq:John"}, "legacy_indexed_field": {"value"}, "unknown": {"value"}},
			wantErr:             true,
			expectConflictError: true,
			expectedErrMsg:      "cannot use 'filter' parameter alongside other legacy filterable field parameters",
		},
		{
			name:           "empty filter string with legacy filter",
			queryValues:    url.Values{"filter": {""}, "legacy_indexed_field": {"value"}},
			wantErr:        true,
			expectedErrMsg: "unknown query parameters: map[filter:[]]",
		},
	}

	for _, tt := range tests {
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
				} else { // err != nil
					if tt.expectedErrMsg != "" {
						if !strings.Contains(err.Error(), tt.expectedErrMsg) {
							t.Errorf("Error message mismatch for test '%s':\nExpected to contain: %s\nActual error: %s", tt.name, tt.expectedErrMsg, err.Error())
						}
					}
					if tt.expectConflictError {
						if !strings.Contains(err.Error(), "cannot use 'filter' parameter alongside other legacy filterable field parameters") {
							t.Errorf("Expected conflict error for test '%s', got: %s", tt.name, err.Error())
						}
					}
				}
			} else { // !tc.wantErr
				if err != nil {
					t.Errorf("Did not expect an error for test '%s', got: %v", tt.name, err)
				}
			}

			// Check columnFields
			if len(tt.expectedColumnFields) > 0 {
				if !reflect.DeepEqual(tt.expectedColumnFields, columnFields) {
					t.Errorf("ColumnFields mismatch for test '%s':\nExpected: %v\nActual:   %v", tt.name, tt.expectedColumnFields, columnFields)
				}
			} else if tt.wantErr && tt.expectedErrMsg == "unknown column: nonexistent" {
				if columnFields != nil {
					t.Errorf("ColumnFields should be nil on 'unknown column' error for test '%s', got: %v", tt.name, columnFields)
				}
			} else if !tt.wantErr {
				if len(columnFields) != 0 {
					t.Errorf("ColumnFields should be empty for test '%s' (expected no columns and no error), got: %v", tt.name, columnFields)
				}
			}

			// Check parsedAST
			if tt.expectedASTString != "" {
				if parsedAST == nil {
					t.Fatalf("parsedAST should not be nil when checkReturnedAST is true for test '%s'", tt.name)
				}
				if actualASTString := parsedAST.String(); actualASTString != tt.expectedASTString {
					t.Errorf("AST string representation mismatch for test '%s':\nExpected: %s\nActual:   %s", tt.name, tt.expectedASTString, actualASTString)
				}
			} else if tt.expectedASTString != "" && !tt.wantErr {
				if parsedAST == nil {
					t.Fatalf("parsedAST should not be nil for a valid expected AST string for test '%s'", tt.name)
				}
				if actualASTString := parsedAST.String(); actualASTString != tt.expectedASTString {
					t.Errorf("AST string representation mismatch for test '%s':\nExpected: %s\nActual:   %s", tt.name, tt.expectedASTString, actualASTString)
				}
			} else {
				if parsedAST != nil {
					t.Errorf("parsedAST should be nil for test '%s', got: %v", tt.name, parsedAST)
				}
			}

			// Check filterSet
			if tt.expectedFilterSet != nil {
				if filterSet == nil {
					t.Fatalf("filterSet should not be nil when checkReturnedFilterSet is true for test '%s'", tt.name)
				}
				if tt.expectedFilterSet.typ != filterSet.typ { // Accessing unexported field 'typ'
					t.Errorf("FilterSet Type mismatch for test '%s':\nExpected: %v\nActual:   %v", tt.name, tt.expectedFilterSet.typ, filterSet.typ)
				}
			} else if tt.expectedFilterSet != nil && !tt.wantErr {
				if filterSet == nil {
					t.Fatalf("filterSet should not be nil for an expected legacy filter for test '%s'", tt.name)
				}
				if tt.expectedFilterSet != nil {
					if tt.expectedFilterSet.typ != filterSet.typ { // Accessing unexported field 'typ'
						t.Errorf("FilterSet Type mismatch for test '%s':\nExpected: %v\nActual:   %v", tt.name, tt.expectedFilterSet.typ, filterSet.typ)
					}
				}
			} else {
				if filterSet != nil {
					t.Errorf("filterSet should be nil for test '%s', got: %v", tt.name, filterSet)
				}
			}

			// Specific assertions based on new logic from parseQuery
			if !tt.wantErr && !tt.expectConflictError {
				if tt.expectedASTString != "" && tt.expectedFilterSet == nil {
					if filterSet != nil {
						t.Errorf("filterSet should be nil when only 'filter' (AST) is used and valid for test '%s', got: %v", tt.name, filterSet)
					}
				}
				if tt.expectedFilterSet != nil && tt.expectedASTString == "" {
					if parsedAST != nil {
						t.Errorf("parsedAST should be nil when only legacy filters are used and valid for test '%s', got: %v", tt.name, parsedAST)
					}
				}
			}
		})
	}
}
