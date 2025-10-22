package resource

import (
	"reflect"
	"strings"
	"testing"
)

// sqlGeneratorTestMap provides the necessary field name mappings for these tests.
var sqlGeneratorTestMap = map[jsonFieldName]FilterFieldInfo{
	"status":   {DbColumnName: "Status", Kind: reflect.String, Indexed: true},
	"user_id":  {DbColumnName: "UserId", Kind: reflect.Int, Indexed: true},
	"price":    {DbColumnName: "Price", Kind: reflect.Float64, Indexed: true},
	"stock":    {DbColumnName: "Stock", Kind: reflect.Int, Indexed: true},
	"rating":   {DbColumnName: "Rating", Kind: reflect.Int, Indexed: true},
	"name":     {DbColumnName: "Name", Kind: reflect.String, Indexed: true},
	"age":      {DbColumnName: "Age", Kind: reflect.Int64, Indexed: true},
	"category": {DbColumnName: "Category", Kind: reflect.String, Indexed: true},
	"email":    {DbColumnName: "Email", Kind: reflect.String, Indexed: true},
	"active":   {DbColumnName: "Active", Kind: reflect.Bool, Indexed: true},
	"field":    {DbColumnName: "Field", Kind: reflect.String, Indexed: true},
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
			wantParams:   map[string]any{},
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
			wantSQL:      `"Name" = @p1`,
			wantParams:   map[string]any{"p1": "John"},
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
			wantSQL:      `"Age" >= @p1`,
			wantParams:   map[string]any{"p1": 30},
		},
		{
			name:         "age:gte:30 spanner",
			filterString: "age:gte:30",
			dialect:      Spanner,
			wantSQL:      "`Age` >= @p1",
			wantParams:   map[string]any{"p1": 30},
		},

		// status:isnull
		{
			name:         "status:isnull pg",
			filterString: "status:isnull",
			dialect:      PostgreSQL,
			wantSQL:      `"Status" IS NULL`,
			wantParams:   map[string]any{},
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
			wantParams:   map[string]any{},
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
			wantSQL:      `"Name" = @p1 AND "Age" >= @p2`,
			wantParams:   map[string]any{"p1": "John", "p2": 30},
		},
		{
			name:         "name:eq:John,age:gte:30 spanner",
			filterString: "name:eq:John,age:gte:30",
			dialect:      Spanner,
			wantSQL:      "`Name` = @p1 AND `Age` >= @p2",
			wantParams:   map[string]any{"p1": "John", "p2": 30},
		},

		// name:eq:John|name:eq:Jane
		{
			name:         "name:eq:John|name:eq:Jane pg",
			filterString: "name:eq:John|name:eq:Jane",
			dialect:      PostgreSQL,
			wantSQL:      `"Name" = @p1 OR "Name" = @p2`,
			wantParams:   map[string]any{"p1": "John", "p2": "Jane"},
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
			wantSQL:      `("Name" = @p1 OR "Name" = @p2) AND "Age" >= @p3`,
			wantParams:   map[string]any{"p1": "John", "p2": "Jane", "p3": 30},
		},
		{
			name:         "(name:eq:John|name:eq:Jane),age:gte:30 spanner",
			filterString: "(name:eq:John|name:eq:Jane),age:gte:30",
			dialect:      Spanner,
			wantSQL:      "(`Name` = @p1 OR `Name` = @p2) AND `Age` >= @p3",
			wantParams:   map[string]any{"p1": "John", "p2": "Jane", "p3": 30},
		},

		// category:in:(books,movies)
		{
			name:         "category:in:(books,movies) pg",
			filterString: "category:in:(books,movies)",
			dialect:      PostgreSQL,
			wantSQL:      `"Category" IN (@p1, @p2)`,
			wantParams:   map[string]any{"p1": "books", "p2": "movies"},
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
			wantSQL:      `"Category" IN (@p1)`,
			wantParams:   map[string]any{"p1": "single"},
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
			wantSQL:      `"UserId" NOT IN (@p1, @p2, @p3)`,
			wantParams:   map[string]any{"p1": 1, "p2": 2, "p3": 3},
		},

		// (category:in:(books,movies)|status:eq:active),price:lt:100
		{
			name:         "(category:in:(books,movies)|status:eq:active),price:lt:100 pg",
			filterString: "(category:in:(books,movies)|status:eq:active),price:lt:100",
			dialect:      PostgreSQL,
			wantSQL:      `("Category" IN (@p1, @p2) OR "Status" = @p3) AND "Price" < @p4`,
			wantParams:   map[string]any{"p1": "books", "p2": "movies", "p3": "active", "p4": 100.0},
		},
		{
			name:         "(category:in:(books,movies)|status:eq:active),price:lt:100 spanner",
			filterString: "(category:in:(books,movies)|status:eq:active),price:lt:100",
			dialect:      Spanner,
			wantSQL:      "(`Category` IN (@p1, @p2) OR `Status` = @p3) AND `Price` < @p4",
			wantParams:   map[string]any{"p1": "books", "p2": "movies", "p3": "active", "p4": 100.0},
		},
		// name:eq:John Doe
		{
			name:         "name:eq:John Doe pg",
			filterString: "name:eq:John Doe",
			dialect:      PostgreSQL,
			wantSQL:      `"Name" = @p1`,
			wantParams:   map[string]any{"p1": "John Doe"},
		},
		// category:in:(sci-fi,non-fiction)
		{
			name:         "category:in:(sci-fi,non-fiction) pg",
			filterString: "category:in:(sci-fi,non-fiction)",
			dialect:      PostgreSQL,
			wantSQL:      `"Category" IN (@p1, @p2)`,
			wantParams:   map[string]any{"p1": "sci-fi", "p2": "non-fiction"},
		},
		// email:isnotnull,age:gt:18
		{
			name:         "email:isnotnull,age:gt:18 pg",
			filterString: "email:isnotnull,age:gt:18",
			dialect:      PostgreSQL,
			wantSQL:      `"Email" IS NOT NULL AND "Age" > @p1`,
			wantParams:   map[string]any{"p1": 18},
		},
		// (name:isnull|name:eq:Unknown)
		{
			name:         "(name:isnull|name:eq:Unknown) pg",
			filterString: "(name:isnull|name:eq:Unknown)",
			dialect:      PostgreSQL,
			wantSQL:      `("Name" IS NULL OR "Name" = @p1)`,
			wantParams:   map[string]any{"p1": "Unknown"},
		},
		// (name:eq:John|name:eq:Jane),(category:in:(books,movies)|status:eq:active)
		{
			name:         "(name:eq:John|name:eq:Jane),(category:in:(books,movies)|status:eq:active) pg",
			filterString: "(name:eq:John|name:eq:Jane),(category:in:(books,movies)|status:eq:active)",
			dialect:      PostgreSQL,
			wantSQL:      `("Name" = @p1 OR "Name" = @p2) AND ("Category" IN (@p3, @p4) OR "Status" = @p5)`,
			wantParams:   map[string]any{"p1": "John", "p2": "Jane", "p3": "books", "p4": "movies", "p5": "active"},
		},
		// ((status:eq:active|status:eq:pending),user_id:notin:(1,2)),price:gte:50
		{
			name:         "((status:eq:active|status:eq:pending),user_id:notin:(1,2)),price:gte:50 pg",
			filterString: "((status:eq:active|status:eq:pending),user_id:notin:(1,2)),price:gte:50",
			dialect:      PostgreSQL,
			wantSQL:      `(("Status" = @p1 OR "Status" = @p2) AND "UserId" NOT IN (@p3, @p4)) AND "Price" >= @p5`,
			wantParams:   map[string]any{"p1": "active", "p2": "pending", "p3": 1, "p4": 2, "p5": 50.0},
		},
		// Test for "ne" operator
		{
			name:         "status:ne:inactive pg",
			filterString: "status:ne:inactive",
			dialect:      PostgreSQL,
			wantSQL:      `"Status" <> @p1`,
			wantParams:   map[string]any{"p1": "inactive"},
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
			wantSQL:      `"Price" < @p1`,
			wantParams:   map[string]any{"p1": 10.0},
		},
		// Test for "lte" operator
		{
			name:         "stock:lte:5 pg",
			filterString: "stock:lte:5",
			dialect:      PostgreSQL,
			wantSQL:      `"Stock" <= @p1`,
			wantParams:   map[string]any{"p1": 5},
		},
		// Test for "gt" operator
		{
			name:         "rating:gt:4 pg",
			filterString: "rating:gt:4",
			dialect:      PostgreSQL,
			wantSQL:      `"Rating" > @p1`,
			wantParams:   map[string]any{"p1": 4},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			lexer := NewFilterLexer(tt.filterString)
			parser, err := NewFilterParser(lexer, sqlGeneratorTestMap)
			if err != nil {
				t.Fatalf("NewParser() error = %v, wantErr %v", err, tt.wantErrMsg)
			}

			ast, parseErr := parser.Parse()
			if tt.wantErrMsg != "" {
				switch {
				case parseErr == nil:
					// If parseErr is nil, proceed to generation, the error might be from generation step
				case !strings.Contains(parseErr.Error(), tt.wantErrMsg):
					t.Fatalf("parser.Parse() error = %v, wantErrMsg %s", parseErr, tt.wantErrMsg)
				default: // Expected parse error occurred
					return // Test successful
				}
			} else if parseErr != nil { // Unexpected parse error
				t.Fatalf("parser.Parse() error = %v, want nil", parseErr)
			}

			var gotSQL string
			var paramsResult any
			var genErr error

			switch tt.dialect {
			case PostgreSQL:
				sqlGen := NewPostgreSQLGenerator()
				gotSQL, paramsResult, genErr = sqlGen.GenerateSQL(ast)
			case Spanner:
				sqlGen := NewSpannerGenerator()
				gotSQL, paramsResult, genErr = sqlGen.GenerateSQL(ast)
			default:
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

			switch tt.dialect {
			case PostgreSQL:
				wantPgParams, ok := tt.wantParams.(map[string]any)
				if !ok && tt.wantParams != nil {
					t.Fatalf("PostgreSQL test case %s has wantParams not of type []any: %T", tt.name, tt.wantParams)
				}

				gotPgParams, ok := paramsResult.(map[string]any)
				if !ok && paramsResult != nil {
					t.Fatalf("PostgreSQL generator did not return []any for test %s: %T", tt.name, paramsResult)
				}

				if !reflect.DeepEqual(gotPgParams, wantPgParams) {
					t.Errorf("PostgreSQLGenerator.GenerateSQL() for test %s: gotParams = %#v, want %#v", tt.name, gotPgParams, wantPgParams)
				}
			case Spanner:
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

func TestSubstituteSQLParams(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		sqlInput      string
		paramsInput   any
		wantSQLOutput string
		wantErrMsg    string
	}{
		// Dialect Tests
		{
			name:          "basic",
			sqlInput:      "SELECT * FROM t WHERE col = @p1 AND name = @name",
			paramsInput:   map[string]any{"p1": 123, "name": "Test"},
			wantSQLOutput: "SELECT * FROM t WHERE col = 123 AND name = 'Test'",
		},
		{
			name:          "sorted keys",
			sqlInput:      "SELECT * FROM t WHERE col = @p1 AND col10 = @p10",
			paramsInput:   map[string]any{"p1": 1, "p10": 10},
			wantSQLOutput: "SELECT * FROM t WHERE col = 1 AND col10 = 10",
		},
		{
			name:          "no params in SQL",
			sqlInput:      "SELECT * FROM t",
			paramsInput:   map[string]any{"p1": 123},
			wantSQLOutput: "SELECT * FROM t",
		},
		{
			name:          "param in SQL not in map",
			sqlInput:      "SELECT * FROM t WHERE col = @p1",
			paramsInput:   map[string]any{},
			wantSQLOutput: "SELECT * FROM t WHERE col = @p1",
		},
		{
			name:          "nil params map",
			sqlInput:      "SELECT * FROM t WHERE col = @p1",
			paramsInput:   nil, // map[string]any(nil) would also work
			wantSQLOutput: "SELECT * FROM t WHERE col = @p1",
		},
		{
			name:          "empty SQL",
			sqlInput:      "",
			paramsInput:   map[string]any{"p1": 123},
			wantSQLOutput: "",
		},
		{
			name:          "boolean and float",
			sqlInput:      "SELECT * FROM t WHERE active = @active AND price = @price",
			paramsInput:   map[string]any{"active": true, "price": 123.45},
			wantSQLOutput: "SELECT * FROM t WHERE active = true AND price = 123.45",
		},

		// Error Cases
		{
			name:          "Error with slice params",
			sqlInput:      "SELECT * FROM t WHERE col = @p1",
			paramsInput:   []any{123},
			wantSQLOutput: "",
			wantErrMsg:    "SubstituteSQLParams: params must be map[string]any",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotSQL, err := substituteSQLParams(tt.sqlInput, tt.paramsInput)

			if tt.wantErrMsg != "" {
				if err == nil {
					t.Errorf("substituteSQLParams() error = nil, wantErrMsg %q", tt.wantErrMsg)
					return
				}
				if !strings.Contains(err.Error(), tt.wantErrMsg) {
					t.Errorf("SubstituteSQLParams() error = %q, wantErrMsg contains %q", err.Error(), tt.wantErrMsg)
				}
				// If error is expected and occurred, check if output SQL is as expected (usually empty for error cases)
				if gotSQL != tt.wantSQLOutput {
					t.Errorf("SubstituteSQLParams() gotSQL = %q, wantSQLOutput %q for error case", gotSQL, tt.wantSQLOutput)
				}
				return // Test done if error was expected
			}

			if err != nil {
				t.Fatalf("SubstituteSQLParams() unexpected error = %v", err)
			}

			if gotSQL != tt.wantSQLOutput {
				t.Errorf("SubstituteSQLParams() gotSQL = %q, want %q", gotSQL, tt.wantSQLOutput)
			}
		})
	}
}
