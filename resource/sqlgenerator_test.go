package resource

import (
	"reflect"
	"strings"
	"testing"
)

// sqlGeneratorTestMap provides the necessary field name mappings for these tests.
var sqlGeneratorTestMap = map[string]string{
	"status":   "Status",
	"user_id":  "UserId",
	"price":    "Price",
	"stock":    "Stock",
	"rating":   "Rating",
	"name":     "Name",
	"age":      "Age",
	"category": "Category",
	"email":    "Email",
}

func TestSQLGenerator_GenerateSQL(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name         string
		filterString string
		dialect      SQLDialect
		wantSQL      string
		wantParams   any
		wantErrMsg   string
	}

	tests := []testCase{
		// Empty filter
		{
			name:         "empty filter pg",
			filterString: "",
			dialect:      PostgreSQL,
			wantSQL:      "",
			wantParams:   []any{},
		},
		{
			name:         "empty filter spanner",
			filterString: "",
			dialect:      Spanner,
			wantSQL:      "",
			wantParams:   map[string]any{},
		},

		// name:eq:John
		{
			name:         "name:eq:John pg",
			filterString: "name:eq:John",
			dialect:      PostgreSQL,
			wantSQL:      `"Name" = $1`,
			wantParams:   []any{"John"},
		},
		{
			name:         "name:eq:John spanner",
			filterString: "name:eq:John",
			dialect:      Spanner,
			wantSQL:      "`Name` = @p1",
			wantParams:   map[string]any{"p1": "John"},
		},

		// age:gte:30
		{
			name:         "age:gte:30 pg",
			filterString: "age:gte:30",
			dialect:      PostgreSQL,
			wantSQL:      `"Age" >= $1`,
			wantParams:   []any{"30"},
		},
		{
			name:         "age:gte:30 spanner",
			filterString: "age:gte:30",
			dialect:      Spanner,
			wantSQL:      "`Age` >= @p1",
			wantParams:   map[string]any{"p1": "30"},
		},

		// status:isnull
		{
			name:         "status:isnull pg",
			filterString: "status:isnull",
			dialect:      PostgreSQL,
			wantSQL:      `"Status" IS NULL`,
			wantParams:   []any{},
		},
		{
			name:         "status:isnull spanner",
			filterString: "status:isnull",
			dialect:      Spanner,
			wantSQL:      "`Status` IS NULL",
			wantParams:   map[string]any{},
		},
		// email:isnotnull
		{
			name:         "email:isnotnull pg",
			filterString: "email:isnotnull",
			dialect:      PostgreSQL,
			wantSQL:      `"Email" IS NOT NULL`,
			wantParams:   []any{},
		},
		{
			name:         "email:isnotnull spanner",
			filterString: "email:isnotnull",
			dialect:      Spanner,
			wantSQL:      "`Email` IS NOT NULL",
			wantParams:   map[string]any{},
		},
		// name:eq:John,age:gte:30
		{
			name:         "name:eq:John,age:gte:30 pg",
			filterString: "name:eq:John,age:gte:30",
			dialect:      PostgreSQL,
			wantSQL:      `"Name" = $1 AND "Age" >= $2`,
			wantParams:   []any{"John", "30"},
		},
		{
			name:         "name:eq:John,age:gte:30 spanner",
			filterString: "name:eq:John,age:gte:30",
			dialect:      Spanner,
			wantSQL:      "`Name` = @p1 AND `Age` >= @p2",
			wantParams:   map[string]any{"p1": "John", "p2": "30"},
		},

		// name:eq:John|name:eq:Jane
		{
			name:         "name:eq:John|name:eq:Jane pg",
			filterString: "name:eq:John|name:eq:Jane",
			dialect:      PostgreSQL,
			wantSQL:      `"Name" = $1 OR "Name" = $2`,
			wantParams:   []any{"John", "Jane"},
		},
		{
			name:         "name:eq:John|name:eq:Jane spanner",
			filterString: "name:eq:John|name:eq:Jane",
			dialect:      Spanner,
			wantSQL:      "`Name` = @p1 OR `Name` = @p2",
			wantParams:   map[string]any{"p1": "John", "p2": "Jane"},
		},

		// (name:eq:John|name:eq:Jane),age:gte:30
		{
			name:         "(name:eq:John|name:eq:Jane),age:gte:30 pg",
			filterString: "(name:eq:John|name:eq:Jane),age:gte:30",
			dialect:      PostgreSQL,
			wantSQL:      `("Name" = $1 OR "Name" = $2) AND "Age" >= $3`,
			wantParams:   []any{"John", "Jane", "30"},
		},
		{
			name:         "(name:eq:John|name:eq:Jane),age:gte:30 spanner",
			filterString: "(name:eq:John|name:eq:Jane),age:gte:30",
			dialect:      Spanner,
			wantSQL:      "(`Name` = @p1 OR `Name` = @p2) AND `Age` >= @p3",
			wantParams:   map[string]any{"p1": "John", "p2": "Jane", "p3": "30"},
		},

		// category:in:(books,movies)
		{
			name:         "category:in:(books,movies) pg",
			filterString: "category:in:(books,movies)",
			dialect:      PostgreSQL,
			wantSQL:      `"Category" IN ($1, $2)`,
			wantParams:   []any{"books", "movies"},
		},
		{
			name:         "category:in:(books,movies) spanner",
			filterString: "category:in:(books,movies)",
			dialect:      Spanner,
			wantSQL:      "`Category` IN (@p1, @p2)",
			wantParams:   map[string]any{"p1": "books", "p2": "movies"},
		},
		// category:in:(single)
		{
			name:         "category:in:(single) pg",
			filterString: "category:in:(single)",
			dialect:      PostgreSQL,
			wantSQL:      `"Category" IN ($1)`,
			wantParams:   []any{"single"},
		},
		{
			name:         "category:in:(single) spanner",
			filterString: "category:in:(single)",
			dialect:      Spanner,
			wantSQL:      "`Category` IN (@p1)",
			wantParams:   map[string]any{"p1": "single"},
		},
		// user_id:notin:(1,2,3)
		{
			name:         "user_id:notin:(1,2,3) pg",
			filterString: "user_id:notin:(1,2,3)",
			dialect:      PostgreSQL,
			wantSQL:      `"UserId" NOT IN ($1, $2, $3)`,
			wantParams:   []any{"1", "2", "3"},
		},

		// (category:in:(books,movies)|status:eq:active),price:lt:100
		{
			name:         "(category:in:(books,movies)|status:eq:active),price:lt:100 pg",
			filterString: "(category:in:(books,movies)|status:eq:active),price:lt:100",
			dialect:      PostgreSQL,
			wantSQL:      `("Category" IN ($1, $2) OR "Status" = $3) AND "Price" < $4`,
			wantParams:   []any{"books", "movies", "active", "100"},
		},
		{
			name:         "(category:in:(books,movies)|status:eq:active),price:lt:100 spanner",
			filterString: "(category:in:(books,movies)|status:eq:active),price:lt:100",
			dialect:      Spanner,
			wantSQL:      "(`Category` IN (@p1, @p2) OR `Status` = @p3) AND `Price` < @p4",
			wantParams:   map[string]any{"p1": "books", "p2": "movies", "p3": "active", "p4": "100"},
		},
		// name:eq:John Doe
		{
			name:         "name:eq:John Doe pg",
			filterString: "name:eq:John Doe",
			dialect:      PostgreSQL,
			wantSQL:      `"Name" = $1`,
			wantParams:   []any{"John Doe"},
		},
		// category:in:(sci-fi,non-fiction)
		{
			name:         "category:in:(sci-fi,non-fiction) pg",
			filterString: "category:in:(sci-fi,non-fiction)",
			dialect:      PostgreSQL,
			wantSQL:      `"Category" IN ($1, $2)`,
			wantParams:   []any{"sci-fi", "non-fiction"},
		},
		// email:isnotnull,age:gt:18
		{
			name:         "email:isnotnull,age:gt:18 pg",
			filterString: "email:isnotnull,age:gt:18",
			dialect:      PostgreSQL,
			wantSQL:      `"Email" IS NOT NULL AND "Age" > $1`,
			wantParams:   []any{"18"},
		},
		// (name:isnull|name:eq:Unknown)
		{
			name:         "(name:isnull|name:eq:Unknown) pg",
			filterString: "(name:isnull|name:eq:Unknown)",
			dialect:      PostgreSQL,
			wantSQL:      `("Name" IS NULL OR "Name" = $1)`,
			wantParams:   []any{"Unknown"},
		},
		// (name:eq:John|name:eq:Jane),(category:in:(books,movies)|status:eq:active)
		{
			name:         "(name:eq:John|name:eq:Jane),(category:in:(books,movies)|status:eq:active) pg",
			filterString: "(name:eq:John|name:eq:Jane),(category:in:(books,movies)|status:eq:active)",
			dialect:      PostgreSQL,
			wantSQL:      `("Name" = $1 OR "Name" = $2) AND ("Category" IN ($3, $4) OR "Status" = $5)`,
			wantParams:   []any{"John", "Jane", "books", "movies", "active"},
		},
		// ((status:eq:active|status:eq:pending),user_id:notin:(1,2)),price:gte:50
		{
			name:         "((status:eq:active|status:eq:pending),user_id:notin:(1,2)),price:gte:50 pg",
			filterString: "((status:eq:active|status:eq:pending),user_id:notin:(1,2)),price:gte:50",
			dialect:      PostgreSQL,
			wantSQL:      `(("Status" = $1 OR "Status" = $2) AND "UserId" NOT IN ($3, $4)) AND "Price" >= $5`,
			wantParams:   []any{"active", "pending", "1", "2", "50"},
		},
		// Test for "ne" operator
		{
			name:         "status:ne:inactive pg",
			filterString: "status:ne:inactive",
			dialect:      PostgreSQL,
			wantSQL:      `"Status" <> $1`,
			wantParams:   []any{"inactive"},
		},
		{
			name:         "status:ne:inactive spanner",
			filterString: "status:ne:inactive",
			dialect:      Spanner,
			wantSQL:      "`Status` <> @p1",
			wantParams:   map[string]any{"p1": "inactive"},
		},
		// Test for "lt" operator
		{
			name:         "price:lt:10 pg",
			filterString: "price:lt:10",
			dialect:      PostgreSQL,
			wantSQL:      `"Price" < $1`,
			wantParams:   []any{"10"},
		},
		// Test for "lte" operator
		{
			name:         "stock:lte:5 pg",
			filterString: "stock:lte:5",
			dialect:      PostgreSQL,
			wantSQL:      `"Stock" <= $1`,
			wantParams:   []any{"5"},
		},
		// Test for "gt" operator
		{
			name:         "rating:gt:4 pg",
			filterString: "rating:gt:4",
			dialect:      PostgreSQL,
			wantSQL:      `"Rating" > $1`,
			wantParams:   []any{"4"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			lexer := NewLexer(tt.filterString)
			parser, err := NewParser(lexer, sqlGeneratorTestMap)
			if err != nil {
				t.Fatalf("NewParser() error = %v, wantErr %v", err, tt.wantErrMsg)
			}

			ast, parseErr := parser.Parse()
			if tt.wantErrMsg != "" {
				if parseErr == nil {
					// If parseErr is nil, proceed to generation, the error might be from generation step
				} else if !strings.Contains(parseErr.Error(), tt.wantErrMsg) {
					t.Fatalf("parser.Parse() error = %v, wantErrMsg %s", parseErr, tt.wantErrMsg)
				} else { // Expected parse error occurred
					return // Test successful
				}
			} else if parseErr != nil { // Unexpected parse error
				t.Fatalf("parser.Parse() error = %v, want nil", parseErr)
			}

			var gotSQL string
			var paramsResult any
			var genErr error

			if tt.dialect == PostgreSQL {
				sqlGen := NewPostgreSQLGenerator()
				gotSQL, paramsResult, genErr = sqlGen.GenerateSQL(ast)
			} else if tt.dialect == Spanner {
				sqlGen := NewSpannerGenerator()
				gotSQL, paramsResult, genErr = sqlGen.GenerateSQL(ast)
			} else {
				t.Fatalf("Unsupported dialect in test case: %v", tt.dialect)
			}

			if tt.wantErrMsg != "" {
				if genErr == nil {
					t.Errorf("GenerateSQL() error = nil, wantErrMsg %s", tt.wantErrMsg)
				} else if !strings.Contains(genErr.Error(), tt.wantErrMsg) {
					t.Errorf("GenerateSQL() error = %v, wantErrMsg %s", genErr, tt.wantErrMsg)
				}

				return // Expected error, test done
			}
			if genErr != nil {
				t.Fatalf("GenerateSQL() error = %v, want nil", genErr)
			}

			if gotSQL != tt.wantSQL {
				t.Errorf("GenerateSQL() gotSQL = %q, want %q", gotSQL, tt.wantSQL)
			}

			if tt.dialect == PostgreSQL {
				wantPgParams, ok := tt.wantParams.([]any)
				if !ok && tt.wantParams != nil {
					t.Fatalf("PostgreSQL test case %s has wantParams not of type []any: %T", tt.name, tt.wantParams)
				}

				gotPgParams, ok := paramsResult.([]any)
				if !ok && paramsResult != nil {
					t.Fatalf("PostgreSQL generator did not return []any for test %s: %T", tt.name, paramsResult)
				}

				if !reflect.DeepEqual(gotPgParams, wantPgParams) {
					t.Errorf("PostgreSQLGenerator.GenerateSQL() gotParams = %#v, want %#v", gotPgParams, wantPgParams)
				}
			} else if tt.dialect == Spanner {
				wantSpannerParams, ok := tt.wantParams.(map[string]any)
				if !ok && tt.wantParams != nil {
					t.Fatalf("Spanner test case %s has wantParams not of type map[string]any: %T", tt.name, tt.wantParams)
				}

				gotSpannerParams, ok := paramsResult.(map[string]any)
				if !ok && paramsResult != nil {
					t.Fatalf("Spanner generator did not return map[string]any for test %s: %T", tt.name, paramsResult)
				}

				if !reflect.DeepEqual(gotSpannerParams, wantSpannerParams) {
					t.Errorf("SpannerGenerator.GenerateSQL() gotParams = %#v, want %#v", gotSpannerParams, wantSpannerParams)
				}
			}
		})
	}
}
