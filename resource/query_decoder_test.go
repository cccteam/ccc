package resource

import (
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/cccteam/ccc"
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
	IsActive           bool     `json:"isActive"               index:"true"`
	ItemIDs            []int    `json:"itemIDs"                  index:"true"`
	Tags               []string `json:"tags"                index:"true"`
	LegacyIndexedField string   `json:"legacyIndexedField" index:"true"`
}

func TestQueryDecoder_parseQuery(t *testing.T) {
	t.Parallel()

	test := []struct {
		name                string
		queryValues         url.Values
		wantErr             bool
		expectedResult      *parsedQueryParams
		expectedASTString   string
		expectedErrMsg      string
		expectConflictError bool
	}{
		{
			name:        "limit only",
			queryValues: url.Values{"limit": []string{"10"}},
			wantErr:     false,
			expectedResult: &parsedQueryParams{
				Limit: ccc.Ptr(uint64(10)),
			},
		},
		{
			name:        "offset only",
			queryValues: url.Values{"offset": []string{"10"}},
			wantErr:     false,
			expectedResult: &parsedQueryParams{
				Limit:  ccc.Ptr(uint64(50)),
				Offset: ccc.Ptr(uint64(10)),
			},
		},
		{
			name:        "default limit",
			queryValues: url.Values{},
			wantErr:     false,
			expectedResult: &parsedQueryParams{
				Limit: ccc.Ptr(uint64(50)),
			},
		},
		{
			name:        "limit and offset",
			queryValues: url.Values{"limit": []string{"20"}, "offset": []string{"10"}},
			wantErr:     false,
			expectedResult: &parsedQueryParams{
				Limit:  ccc.Ptr(uint64(20)),
				Offset: ccc.Ptr(uint64(10)),
			},
		},
		{
			name:           "invalid offset - negative",
			queryValues:    url.Values{"offset": []string{"-1"}},
			wantErr:        true,
			expectedErrMsg: "invalid offset value: -1",
		},
		{
			name:           "invalid offset - non-integer",
			queryValues:    url.Values{"offset": []string{"abc"}},
			wantErr:        true,
			expectedErrMsg: "invalid offset value: abc",
		},
		{
			name:           "invalid limit - negative",
			queryValues:    url.Values{"limit": []string{"-1"}},
			wantErr:        true,
			expectedErrMsg: "invalid limit value: -1",
		},
		{
			name:           "invalid limit - non-integer",
			queryValues:    url.Values{"limit": []string{"abc"}},
			wantErr:        true,
			expectedErrMsg: "invalid limit value: abc",
		},
		// Columns processing first
		{
			name:        "columns only",
			queryValues: url.Values{"columns": []string{"name,age"}},
			wantErr:     false,
			expectedResult: &parsedQueryParams{
				ColumnFields: []accesstypes.Field{"Name", "Age"},
				Limit:        ccc.Ptr(uint64(50)),
			},
		},
		{
			name:        "columns with valid filter",
			queryValues: url.Values{"columns": {"name"}, "filter": {"age:gt:30"}},
			wantErr:     false,
			expectedResult: &parsedQueryParams{
				ColumnFields: []accesstypes.Field{"Name"},
				Limit:        ccc.Ptr(uint64(50)),
			},
			expectedASTString: "age_sql:gt:30",
		},
		{
			name:           "columns with legacy filter (now unsupported)",
			queryValues:    url.Values{"columns": []string{"name"}, "legacyIndexedField": []string{"value"}},
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
			expectedResult: &parsedQueryParams{
				Limit: ccc.Ptr(uint64(50)),
			},
		},
		{
			name:        "valid search",
			queryValues: url.Values{"SearchTokens": []string{"find this and this"}},
			wantErr:     false,
			expectedResult: &parsedQueryParams{
				Search: &Search{
					typ: SubString,
					values: map[SearchKey]string{
						"SearchTokens": "find this and this",
					},
				},
				Limit: ccc.Ptr(uint64(50)),
			},
		},
		{
			name:           "invalid filter only",
			queryValues:    url.Values{"filter": []string{"name:badop:John"}},
			wantErr:        true,
			expectedErrMsg: "unknown operator 'badop' in condition 'name:badop:John'",
		},

		// all togeather now
		{
			name:              "limit only",
			queryValues:       url.Values{"columns": []string{"name,age"}, "filter": []string{"name:eq:John"}, "limit": []string{"10"}},
			wantErr:           false,
			expectedASTString: "name_sql:eq:John",
			expectedResult: &parsedQueryParams{
				ColumnFields: []accesstypes.Field{"Name", "Age"},
				Limit:        ccc.Ptr(uint64(10)),
			},
		},

		// Conflict Check (legacyIndexedField is now an unknown parameter)
		{
			name:           "filter with legacy field (now unknown param)",
			queryValues:    url.Values{"filter": []string{"name:eq:John"}, "legacyIndexedField": []string{"value"}},
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
			queryValues:    url.Values{"filter": []string{"name:badop:John"}, "legacyIndexedField": []string{"value"}},
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
			queryValues:    url.Values{"legacyIndexedField": []string{"value"}, "unknown": []string{"value"}},
			wantErr:        true,
			expectedErrMsg: "unknown query parameters",
		},
		{
			name:           "columns, valid filter, legacy field (now unsupported), and unknown param",
			queryValues:    url.Values{"columns": []string{"age"}, "filter": []string{"name:eq:John"}, "legacyIndexedField": []string{"value"}, "unknown": []string{"value"}},
			wantErr:        true,
			expectedErrMsg: "unknown query parameters",
		},
		{
			name:           "empty filter string with legacy field (now unsupported)",
			queryValues:    url.Values{"filter": []string{""}, "legacyIndexedField": []string{"value"}},
			wantErr:        true,
			expectedErrMsg: "unknown query parameters",
		},
		{
			name:              "integer equality",
			queryValues:       url.Values{"filter": []string{"age:eq:42"}},
			wantErr:           false,
			expectedASTString: "age_sql:eq:42",
			expectedResult: &parsedQueryParams{
				Limit: ccc.Ptr(uint64(50)),
			},
		},
		{
			name:              "boolean true equality",
			queryValues:       url.Values{"filter": []string{"isActive:eq:true"}},
			wantErr:           false,
			expectedASTString: "active_sql:eq:true",
			expectedResult: &parsedQueryParams{
				Limit: ccc.Ptr(uint64(50)),
			},
		},
		{
			name:              "boolean false equality",
			queryValues:       url.Values{"filter": []string{"isActive:eq:false"}},
			wantErr:           false,
			expectedASTString: "active_sql:eq:false",
			expectedResult: &parsedQueryParams{
				Limit: ccc.Ptr(uint64(50)),
			},
		},
		{
			name:              "float GTE",
			queryValues:       url.Values{"filter": []string{"salary:gte:5000.75"}},
			wantErr:           false,
			expectedASTString: "salary_sql:gte:5000.75",
			expectedResult: &parsedQueryParams{
				Limit: ccc.Ptr(uint64(50)),
			},
		},
		{
			name:              "IN list of integers",
			queryValues:       url.Values{"filter": []string{"itemIDs:in:(1,2,3)"}},
			wantErr:           false,
			expectedASTString: "item_ids_sql:in:(1,2,3)",
			expectedResult: &parsedQueryParams{
				Limit: ccc.Ptr(uint64(50)),
			},
		},
		{
			name:              "IN list of strings",
			queryValues:       url.Values{"filter": []string{"tags:in:(Alice,Bob,Charlie)"}},
			wantErr:           false,
			expectedASTString: "tags_sql:in:(Alice,Bob,Charlie)",
			expectedResult: &parsedQueryParams{
				Limit: ccc.Ptr(uint64(50)),
			},
		},
		{
			name:              "ISNULL operator",
			queryValues:       url.Values{"filter": []string{"email:isnull"}},
			wantErr:           false,
			expectedASTString: "email_sql:isnull",
			expectedResult: &parsedQueryParams{
				Limit: ccc.Ptr(uint64(50)),
			},
		},
		{
			name:              "ISNOTNULL operator",
			queryValues:       url.Values{"filter": []string{"email:isnotnull"}},
			wantErr:           false,
			expectedASTString: "email_sql:isnotnull",
			expectedResult: &parsedQueryParams{
				Limit: ccc.Ptr(uint64(50)),
			},
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
			queryValues:    url.Values{"filter": []string{"isActive:eq:notabool"}},
			wantErr:        true,
			expectedErrMsg: "value 'notabool' in condition 'isActive:eq:notabool' is not a valid boolean",
		},
		{
			name:           "conversion error - float",
			queryValues:    url.Values{"filter": []string{"salary:eq:notafloat"}},
			wantErr:        true,
			expectedErrMsg: "value 'notafloat' in condition 'salary:eq:notafloat' is not a valid float",
		},

		// Sort parameter processing
		{
			name:        "sort single field default direction",
			queryValues: url.Values{"sort": []string{"name"}},
			wantErr:     false,
			expectedResult: &parsedQueryParams{
				SortFields: []SortField{{Field: "Name", Direction: SortAscending}},
				Limit:      ccc.Ptr(uint64(50)),
			},
		},
		{
			name:        "sort single field asc",
			queryValues: url.Values{"sort": []string{"name:asc"}},
			wantErr:     false,
			expectedResult: &parsedQueryParams{
				SortFields: []SortField{{Field: "Name", Direction: SortAscending}},
				Limit:      ccc.Ptr(uint64(50)),
			},
		},
		{
			name:        "sort single field desc",
			queryValues: url.Values{"sort": []string{"age:desc"}},
			wantErr:     false,
			expectedResult: &parsedQueryParams{
				SortFields: []SortField{{Field: "Age", Direction: SortDescending}},
				Limit:      ccc.Ptr(uint64(50)),
			},
		},
		{
			name:        "sort multi-field",
			queryValues: url.Values{"sort": []string{"name:asc,age:desc"}},
			wantErr:     false,
			expectedResult: &parsedQueryParams{
				SortFields: []SortField{{Field: "Name", Direction: SortAscending}, {Field: "Age", Direction: SortDescending}},
				Limit:      ccc.Ptr(uint64(50)),
			},
		},
		{
			name:        "sort multi-field with spaces",
			queryValues: url.Values{"sort": []string{" name : asc , age : desc "}},
			wantErr:     false,
			expectedResult: &parsedQueryParams{
				SortFields: []SortField{{Field: "Name", Direction: SortAscending}, {Field: "Age", Direction: SortDescending}},
				Limit:      ccc.Ptr(uint64(50)),
			},
		},
		{
			name:           "sort invalid field name",
			queryValues:    url.Values{"sort": []string{"nonexistent:asc"}},
			wantErr:        true,
			expectedErrMsg: "unknown sort field: nonexistent",
		},
		{
			name:        "sort legacyIndexedField (is sortable)",
			queryValues: url.Values{"sort": []string{"legacyIndexedField:asc"}},
			wantErr:     false,
			expectedResult: &parsedQueryParams{
				SortFields: []SortField{{Field: "LegacyIndexedField", Direction: SortAscending}},
				Limit:      ccc.Ptr(uint64(50)),
			},
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
			t.Parallel()
			resSet, err := NewResourceSet[TestResource, TestRequest]()
			if err != nil {
				t.Fatalf("Failed to create ResourceSet for test case %s: %v", tt.name, err)
			}
			decoder, err := NewQueryDecoder[TestResource, TestRequest](resSet)
			if err != nil {
				t.Fatalf("NewQueryDecoder should not fail with default setup for test case %s: %v", tt.name, err)
			}

			parsedQuery, err := decoder.parseQuery(tt.queryValues)

			if tt.wantErr {
				if err == nil {
					t.Errorf("Expected an error for test '%s', got nil", tt.name)
				} else {
					if tt.expectedErrMsg != "" && !strings.Contains(err.Error(), tt.expectedErrMsg) {
						t.Errorf("Error message mismatch for test '%s':\nExpected to contain: %s\nActual error: %s", tt.name, tt.expectedErrMsg, err.Error())
					}
					if tt.expectConflictError && !strings.Contains(err.Error(), "cannot use 'filter' parameter alongside other legacy filterable field parameters") {
						t.Errorf("Expected conflict error for test '%s', but got: %s", tt.name, err.Error())
					}
				}
			} else { // !tt.wantErr
				if err != nil {
					t.Errorf("Did not expect an error for test '%s', got: %v", tt.name, err)
				}
			}

			if tt.expectedResult != nil {
				if tt.expectedResult.Limit != nil {
					if parsedQuery.Limit == nil {
						t.Errorf("Expected limit %d, got nil", *tt.expectedResult.Limit)
					} else if *tt.expectedResult.Limit != *parsedQuery.Limit {
						t.Errorf("Expected limit %d, got %d", *tt.expectedResult.Limit, *parsedQuery.Limit)
					}
				} else if parsedQuery.Limit != nil {
					t.Errorf("Expected nil limit, got %d", *parsedQuery.Limit)
				}

				if tt.expectedResult.Offset != nil {
					if parsedQuery.Offset == nil {
						t.Errorf("Expected offset %d, got nil", *tt.expectedResult.Offset)
					} else if *tt.expectedResult.Offset != *parsedQuery.Offset {
						t.Errorf("Expected offset %d, got %d", *tt.expectedResult.Offset, *parsedQuery.Offset)
					}
				} else if parsedQuery.Offset != nil {
					t.Errorf("Expected nil offset, got %d", *parsedQuery.Offset)
				}

				if !reflect.DeepEqual(tt.expectedResult.SortFields, parsedQuery.SortFields) {
					isNilOrEmptySortField := func(s []SortField) bool { return len(s) == 0 }
					if !(tt.wantErr && isNilOrEmptySortField(tt.expectedResult.SortFields) && isNilOrEmptySortField(parsedQuery.SortFields)) {
						t.Errorf("SortFields mismatch for test '%s':\nExpected: %#v\nActual:   %#v", tt.name, tt.expectedResult.SortFields, parsedQuery.SortFields)
					}
				}

				if !reflect.DeepEqual(tt.expectedResult.ColumnFields, parsedQuery.ColumnFields) {
					isNilOrEmpty := func(s []accesstypes.Field) bool { return len(s) == 0 }
					if !(tt.wantErr && isNilOrEmpty(tt.expectedResult.ColumnFields) && isNilOrEmpty(parsedQuery.ColumnFields)) {
						t.Errorf("ColumnFields mismatch for test '%s':\nExpected: %#v\nActual:   %#v", tt.name, tt.expectedResult.ColumnFields, parsedQuery.ColumnFields)
					}
				}

				if tt.expectedResult.Search != nil {
					if parsedQuery.Search == nil {
						if !tt.wantErr {
							t.Errorf("searchSet is nil for test '%s', but expected: %#v", tt.name, tt.expectedResult.Search)
						}
					} else {
						if tt.expectedResult.Search.typ != parsedQuery.Search.typ {
							t.Errorf("FilterSet Type mismatch for test '%s':\nExpected: %v\nActual:   %v", tt.name, tt.expectedResult.Search.typ, parsedQuery.Search.typ)
						}
						if !reflect.DeepEqual(tt.expectedResult.Search.values, parsedQuery.Search.values) {
							t.Errorf("FilterSet Values mismatch for test '%s':\nExpected: %#v\nActual:   %#v", tt.name, tt.expectedResult.Search.values, parsedQuery.Search.values)
						}
					}
				} else if parsedQuery.Search != nil && !tt.wantErr {
					if !(tt.expectConflictError && err != nil && strings.Contains(err.Error(), "cannot use 'filter' parameter")) {
						t.Errorf("searchSet should be nil for test '%s', got: %#v", tt.name, parsedQuery.Search)
					}
				}
			}

			// Check AST string representation
			if tt.expectedASTString != "" {
				if parsedQuery.ParsedAST == nil {
					if !tt.wantErr { // Only error if we didn't expect an error that might prevent AST parsing
						t.Errorf("parsedAST is nil for test '%s', but expected AST string: %s", tt.name, tt.expectedASTString)
					}
				} else if actualASTString := parsedQuery.ParsedAST.String(); actualASTString != tt.expectedASTString {
					t.Errorf("AST string representation mismatch for test '%s':\nExpected: %s\nActual:   %s", tt.name, tt.expectedASTString, actualASTString)
				}
			} else if parsedQuery != nil && parsedQuery.ParsedAST != nil && !tt.wantErr { // If no AST string is expected and no error, AST should be nil
				if !(tt.expectConflictError && err != nil && strings.Contains(err.Error(), "cannot use 'filter' parameter")) {
					t.Errorf("Expected nil parsedAST for test '%s' (no expected AST string and no error), got: %s", tt.name, parsedQuery.ParsedAST.String())
				}
			}
		})
	}
}

func TestQueryDecoder_DecodeWithoutPermissions(t *testing.T) {
	t.Parallel()
	resSet, err := NewResourceSet[TestResource, TestRequest]()
	if err != nil {
		t.Fatalf("Failed to create ResourceSet: %v", err)
	}
	decoder, err := NewQueryDecoder[TestResource, TestRequest](resSet)
	if err != nil {
		t.Fatalf("NewQueryDecoder should not fail with default setup: %v", err)
	}

	testCases := []struct {
		name              string
		method            string
		urlValues         string
		body              string
		expectedASTString string
		expectedErrMsg    string
		expectErr         bool
	}{
		{
			name:              "GET with filter in query",
			method:            http.MethodGet,
			urlValues:         "filter=name:eq:John",
			body:              "",
			expectedASTString: "name_sql:eq:John",
			expectErr:         false,
		},
		{
			name:              "POST with filter in body",
			method:            http.MethodPost,
			urlValues:         "",
			body:              `{"filter": "name:eq:John"}`,
			expectedASTString: "name_sql:eq:John",
			expectErr:         false,
		},
		{
			name:              "POST with filter in URL",
			method:            http.MethodPost,
			urlValues:         "filter=name:eq:John",
			body:              "{}",
			expectedASTString: "name_sql:eq:John",
			expectErr:         false,
		},
		{
			name:           "POST with filter in body and query",
			method:         http.MethodPost,
			urlValues:      "filter=age:gt:30",
			body:           `{"filter": "name:eq:John"}`,
			expectedErrMsg: "cannot have 'filter' parameter in both query and body",
			expectErr:      true,
		},
		{
			name:           "POST with invalid JSON body",
			method:         http.MethodPost,
			urlValues:      "",
			body:           `{"filter": "name:eq:John"`,
			expectedErrMsg: "failed to decode request body",
			expectErr:      true,
		},
		{
			name:      "POST with empty body",
			method:    http.MethodPost,
			urlValues: "",
			body:      "",
			expectErr: true,
		},
		{
			name:              "POST invalid field in body",
			method:            http.MethodPost,
			urlValues:         "filter=name:eq:John",
			body:              `{"a": "value"}`,
			expectedASTString: "",
			expectErr:         true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req, err := http.NewRequest(tc.method, "http://test?"+tc.urlValues, strings.NewReader(tc.body))
			if err != nil {
				t.Fatalf("Failed to create request: %v", err)
			}
			if tc.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}

			qSet, err := decoder.DecodeWithoutPermissions(req)

			if tc.expectErr {
				if err == nil {
					t.Errorf("Expected an error, got nil")
				} else if !strings.Contains(err.Error(), tc.expectedErrMsg) {
					t.Errorf("Expected error message to contain '%s', got '%s'", tc.expectedErrMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Did not expect an error, got: %v", err)
				}
				if qSet.filterAst == nil {
					t.Errorf("Expected AST to be parsed, but it was nil")
				} else if qSet.filterAst.String() != tc.expectedASTString {
					t.Errorf("Expected AST string '%s', got '%s'", tc.expectedASTString, qSet.filterAst.String())
				}
			}
		})
	}
}
