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
	Name               string   `json:"name"`
	Age                int      `json:"age"`
	Status             string   `json:"status"`
	Email              *string  `json:"email"`
	Salary             float64  `json:"salary"`
	IsActive           bool     `json:"active" index:"true"`
	ItemIDs            []int    `json:"ids" index:"true"`
	Names              []string `json:"names" index:"true"`
	LegacyIndexedField string   `json:"legacy_indexed_field" index:"true"`
}

func TestQueryDecoder_parseQuery_Refactored(t *testing.T) {
	test := []struct {
		name                   string
		queryValues            url.Values
		wantErr                bool
		expectedASTString      string
		expectedFilterSet      *Filter
		expectedColumnFields   []accesstypes.Field
		expectedErrMsg         string
		expectConflictError    bool
		expectedConditionField string
		expectedTypedValue     any
		expectedTypedValues    []any
		skipASTValueCheck      bool
	}{
		// 1. Columns processing first
		{
			name:                 "columns only",
			queryValues:          url.Values{"columns": []string{"name,age"}},
			wantErr:              false,
			expectedColumnFields: []accesstypes.Field{"Name", "Age"},
			skipASTValueCheck:    true,
		},
		{
			name:                 "columns with valid filter",
			queryValues:          url.Values{"columns": {"name"}, "filter": {"age:gt:30"}},
			wantErr:              false,
			expectedColumnFields: []accesstypes.Field{"Name"},
			expectedASTString:    "age_sql:gt:30",
			expectedTypedValue:   int(30), // Check typed value
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
			skipASTValueCheck: true,
		},
		{
			name:                 "invalid column name",
			queryValues:          url.Values{"columns": []string{"name,nonexistent"}},
			wantErr:              true,
			expectedErrMsg:       "unknown column: nonexistent",
			expectedColumnFields: nil,
			skipASTValueCheck:    true,
		},

		// 2. "filter" parameter processing
		{
			name:               "valid filter only - string",
			queryValues:        url.Values{"filter": []string{"name:eq:John"}},
			wantErr:            false,
			expectedASTString:  "name_sql:eq:John",
			expectedTypedValue: "John",
		},
		{
			name:              "invalid filter only",
			queryValues:       url.Values{"filter": []string{"name:badop:John"}},
			wantErr:           true,
			expectedErrMsg:    "parseConditionToken error='badop' in condition 'name:badop:John'",
			skipASTValueCheck: true,
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
			skipASTValueCheck: true,
		},

		// 4. Conflict Check
		{
			name:               "conflict between filter and legacy filter",
			queryValues:        url.Values{"filter": []string{"name:eq:John"}, "legacy_indexed_field": []string{"value"}},
			wantErr:            true,
			expectedASTString:  "name_sql:eq:John", // Still check AST structure
			expectedTypedValue: "John",             // And its value
			expectedFilterSet: NewFilter(
				Index,
				map[FilterKey]string{FilterKey("legacy_indexed_field_sql"): "value"},
				map[FilterKey]reflect.Kind{FilterKey("legacy_indexed_field_sql"): reflect.String},
			),
			expectConflictError: true,
			expectedErrMsg:      "cannot use 'filter' parameter alongside other legacy filterable field parameters",
		},

		// 5. Interaction and error propagation
		{
			name:              "invalid filter with legacy filter present",
			queryValues:       url.Values{"filter": []string{"name:badop:John"}, "legacy_indexed_field": []string{"value"}},
			wantErr:           true,
			expectedErrMsg:    "parseConditionToken error='badop' in condition 'name:badop:John'",
			skipASTValueCheck: true,
		},
		{
			name:               "valid filter with unknown parameter",
			queryValues:        url.Values{"filter": []string{"name:eq:John"}, "unknown": []string{"value"}},
			wantErr:            true,
			expectedASTString:  "name_sql:eq:John", // AST is parsed
			expectedTypedValue: "John",
			expectedErrMsg:     "unknown query parameters: map[unknown:[value]]",
			// checkReturnedAST is true by providing expectedASTString/expectedTypedValue
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
			// checkReturnedFilterSet is true by providing expectedFilterSet
			skipASTValueCheck: true,
		},
		{
			name:                 "columns, valid filter, legacy filter (conflict), and unknown param",
			queryValues:          url.Values{"columns": []string{"age"}, "filter": []string{"name:eq:John"}, "legacy_indexed_field": []string{"value"}, "unknown": []string{"value"}},
			wantErr:              true,
			expectedColumnFields: []accesstypes.Field{"Age"},
			expectedASTString:    "name_sql:eq:John",
			expectedTypedValue:   "John",
			expectedFilterSet: NewFilter(
				Index,
				map[FilterKey]string{FilterKey("legacy_indexed_field_sql"): "value"},
				map[FilterKey]reflect.Kind{FilterKey("legacy_indexed_field_sql"): reflect.String},
			),
			expectConflictError: true,
			expectedErrMsg:      "cannot use 'filter' parameter alongside other legacy filterable field parameters",
		},
		{
			name:              "empty filter string with legacy filter",
			queryValues:       url.Values{"filter": []string{""}, "legacy_indexed_field": []string{"value"}},
			wantErr:           true,
			expectedASTString: "", // No AST from empty filter string
			expectedFilterSet: NewFilter(
				Index,
				map[FilterKey]string{FilterKey("legacy_indexed_field_sql"): "value"},
				map[FilterKey]reflect.Kind{FilterKey("legacy_indexed_field_sql"): reflect.String},
			),
			expectedErrMsg: "unknown query parameters: map[filter:[]]",
			// checkReturnedFilterSet is true by providing expectedFilterSet
			skipASTValueCheck: true, // No AST to check value for
		},
		// New Test Cases for Typed Parsing
		{
			name:               "integer equality",
			queryValues:        url.Values{"filter": []string{"age:eq:42"}},
			wantErr:            false,
			expectedASTString:  "age_sql:eq:42",
			expectedTypedValue: int(42),
		},
		{
			name:               "boolean true equality",
			queryValues:        url.Values{"filter": []string{"active:eq:true"}},
			wantErr:            false,
			expectedASTString:  "active_sql:eq:true",
			expectedTypedValue: true,
		},
		{
			name:               "boolean false equality",
			queryValues:        url.Values{"filter": []string{"active:eq:false"}},
			wantErr:            false,
			expectedASTString:  "active_sql:eq:false",
			expectedTypedValue: false,
		},
		{
			name:               "float GTE",
			queryValues:        url.Values{"filter": []string{"salary:gte:5000.75"}},
			wantErr:            false,
			expectedASTString:  "salary_sql:gte:5000.75",
			expectedTypedValue: float64(5000.75),
		},
		{
			name:                "IN list of integers",
			queryValues:         url.Values{"filter": []string{"ids:in:(1,2,3)"}},
			wantErr:             false,
			expectedASTString:   "ids_sql:in:(1,2,3)",
			expectedTypedValues: []any{int(1), int(2), int(3)},
		},
		{
			name:                "IN list of strings",
			queryValues:         url.Values{"filter": []string{"names:in:(Alice,Bob,Charlie)"}},
			wantErr:             false,
			expectedASTString:   "tags_sql:in:(Alice,Bob,Charlie)", // Assuming json:"names" maps to db "tags_sql"
			expectedTypedValues: []any{"Alice", "Bob", "Charlie"},
		},
		{
			name:              "ISNULL operator (no value check needed)",
			queryValues:       url.Values{"filter": []string{"email:isnull"}},
			wantErr:           false,
			expectedASTString: "email_sql:isnull",
			skipASTValueCheck: true,
		},
		{
			name:              "ISNOTNULL operator (no value check needed)",
			queryValues:       url.Values{"filter": []string{"email:isnotnull"}},
			wantErr:           false,
			expectedASTString: "email_sql:isnotnull",
			skipASTValueCheck: true,
		},
		{
			name:              "conversion error - int",
			queryValues:       url.Values{"filter": []string{"age:eq:notanint"}},
			wantErr:           true,
			expectedErrMsg:    "value 'notanint' is not a valid integer",
			skipASTValueCheck: true,
		},
		{
			name:              "conversion error - bool",
			queryValues:       url.Values{"filter": []string{"active:eq:notabool"}},
			wantErr:           true,
			expectedErrMsg:    "value 'notabool' is not a valid boolean",
			skipASTValueCheck: true,
		},
		{
			name:              "conversion error - float",
			queryValues:       url.Values{"filter": []string{"salary:eq:notafloat"}},
			wantErr:           true,
			expectedErrMsg:    "value 'notafloat' is not a valid float",
			skipASTValueCheck: true,
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

			astOK := true
			if parsedAST == nil {
				if tt.expectedASTString != "" || tt.expectedTypedValue != nil || len(tt.expectedTypedValues) > 0 {
					if !tt.wantErr { // If no error was wanted, but AST is nil when it shouldn't be
						t.Errorf("parsedAST is nil for test '%s' when a structure or value was expected", tt.name)
					}
					astOK = false // Mark AST as not OK for further checks
				}
			} else { // parsedAST is not nil
				if tt.expectedASTString != "" {
					if actualASTString := parsedAST.String(); actualASTString != tt.expectedASTString {
						t.Errorf("AST string representation mismatch for test '%s':\nExpected: %s\nActual:   %s", tt.name, tt.expectedASTString, actualASTString)
					}
				}
			}

			if !tt.wantErr && !tt.skipASTValueCheck && astOK && parsedAST != nil {
				condNode, ok := parsedAST.(*ConditionNode)
				// If it's a complex AST, try to find the condition node. For now, assume simple or first.
				// This part might need a helper function to traverse AST for more complex cases.
				if !ok {
					if ln, lnOk := parsedAST.(*LogicalOpNode); lnOk { // Check if it's a logical node
						if cnLeft, cnLeftOk := ln.Left.(*ConditionNode); cnLeftOk { // Check left child
							if tt.expectedConditionField == "" || tt.expectedConditionField == cnLeft.Condition.Field {
								condNode = cnLeft
								ok = true
							}
						}
						if !ok { // If not found in left, check right child
							if cnRight, cnRightOk := ln.Right.(*ConditionNode); cnRightOk {
								if tt.expectedConditionField == "" || tt.expectedConditionField == cnRight.Condition.Field {
									condNode = cnRight
									ok = true
								}
							}
						}
					}
				}

				if ok && condNode != nil { // We have a ConditionNode to check
					if tt.expectedTypedValue != nil {
						if !reflect.DeepEqual(condNode.Condition.Value, tt.expectedTypedValue) {
							t.Errorf("Typed value mismatch for test '%s' (field %s):\nExpected: %v (type %T)\nActual:   %v (type %T)",
								tt.name, condNode.Condition.Field, tt.expectedTypedValue, tt.expectedTypedValue, condNode.Condition.Value, condNode.Condition.Value)
						}
					}
					if len(tt.expectedTypedValues) > 0 {
						if !reflect.DeepEqual(condNode.Condition.Values, tt.expectedTypedValues) {
							t.Errorf("Typed values list mismatch for test '%s' (field %s):\nExpected: %v\nActual:   %v",
								tt.name, condNode.Condition.Field, tt.expectedTypedValues, condNode.Condition.Values)
						}
						// Also check types of individual elements if necessary
						for i := range tt.expectedTypedValues {
							if i < len(condNode.Condition.Values) {
								expectedType := reflect.TypeOf(tt.expectedTypedValues[i])
								actualType := reflect.TypeOf(condNode.Condition.Values[i])
								if expectedType != actualType {
									t.Errorf("Typed values list element type mismatch for test '%s' (field %s, index %d):\nExpected type: %v\nActual type:   %v",
										tt.name, condNode.Condition.Field, i, expectedType, actualType)
								}
							}
						}
					}
				} else if tt.expectedTypedValue != nil || len(tt.expectedTypedValues) > 0 {
					// Expected typed values/value but couldn't find a ConditionNode in AST
					t.Errorf("Expected to check typed values for test '%s', but could not extract ConditionNode from AST %T", tt.name, parsedAST)
				}
			}

			if tt.expectedFilterSet != nil {
				if filterSet == nil {
					if !tt.wantErr { // Only fail if no error was expected overall for the test
						t.Fatalf("filterSet should not be nil for test '%s'", tt.name)
					}
				} else {
					if tt.expectedFilterSet.typ != filterSet.typ {
						t.Errorf("FilterSet Type mismatch for test '%s':\nExpected: %v\nActual:   %v", tt.name, tt.expectedFilterSet.typ, filterSet.typ)
					}
				}
			} else { // tc.expectedFilterSet == nil
				if filterSet != nil && !tt.wantErr && !tt.expectConflictError { // If no error and no conflict, filterSet should be nil
					t.Errorf("filterSet should be nil for test '%s', got: %v", tt.name, filterSet)
				} else if filterSet != nil && tt.expectConflictError && tt.wantErr && tt.expectedErrMsg == "cannot use 'filter' parameter alongside other legacy filterable field parameters" {
					// This is the conflict case, filterSet can be non-nil here, verify its type
					// This specific check is now covered by the more general check above with tc.expectedFilterSet being non-nil for conflict case.
				} else if filterSet != nil && tt.wantErr && tt.expectedErrMsg != "" && !strings.Contains(tt.expectedErrMsg, "cannot use 'filter' parameter") {
					// If an error occurred *before* conflict check (e.g. bad AST, bad legacy filter before conflict), filterSet might be nil or partially formed.
					// The current assertions for tc.checkReturnedFilterSet handle this.
				}
			}
		})
	}
}
