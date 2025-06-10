package resource

import (
	"reflect"
	"strings"
	"testing"
)

func TestSQLGenerator_GenerateSQL(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name         string
		filterString string
		dialect      SQLDialect
		wantSQL      string
		wantParams   []any
		wantErrMsg   string // Substring of the expected error message
	}

	tests := []testCase{
		// Empty filter
		{
			name:         "empty filter pg",
			filterString: "",
			dialect:      PostgreSQL,
			wantSQL:      "",
			wantParams:   nil,
		},
		{
			name:         "empty filter spanner",
			filterString: "",
			dialect:      Spanner,
			wantSQL:      "",
			wantParams:   nil,
		},

		// name:eq:John
		{
			name:         "name:eq:John pg",
			filterString: "name:eq:John",
			dialect:      PostgreSQL,
			wantSQL:      `"name" = $1`,
			wantParams:   []any{"John"},
		},
		{
			name:         "name:eq:John spanner",
			filterString: "name:eq:John",
			dialect:      Spanner,
			wantSQL:      "`name` = @p1",
			wantParams:   []any{"John"},
		},

		// age:gte:30
		{
			name:         "age:gte:30 pg",
			filterString: "age:gte:30",
			dialect:      PostgreSQL,
			wantSQL:      `"age" >= $1`,
			wantParams:   []any{"30"},
		},
		{
			name:         "age:gte:30 spanner",
			filterString: "age:gte:30",
			dialect:      Spanner,
			wantSQL:      "`age` >= @p1",
			wantParams:   []any{"30"},
		},

		// status:isnull
		{
			name:         "status:isnull pg",
			filterString: "status:isnull",
			dialect:      PostgreSQL,
			wantSQL:      `"status" IS NULL`,
			wantParams:   []any{},
		},
		{
			name:         "status:isnull spanner",
			filterString: "status:isnull",
			dialect:      Spanner,
			wantSQL:      "`status` IS NULL",
			wantParams:   []any{},
		},
		// email:isnotnull
		{
			name:         "email:isnotnull pg",
			filterString: "email:isnotnull",
			dialect:      PostgreSQL,
			wantSQL:      `"email" IS NOT NULL`,
			wantParams:   []any{},
		},
		// name:eq:John,age:gte:30
		{
			name:         "name:eq:John,age:gte:30 pg",
			filterString: "name:eq:John,age:gte:30",
			dialect:      PostgreSQL,
			wantSQL:      `"name" = $1 AND "age" >= $2`,
			wantParams:   []any{"John", "30"},
		},
		{
			name:         "name:eq:John,age:gte:30 spanner",
			filterString: "name:eq:John,age:gte:30",
			dialect:      Spanner,
			wantSQL:      "`name` = @p1 AND `age` >= @p2",
			wantParams:   []any{"John", "30"},
		},

		// name:eq:John|name:eq:Jane
		{
			name:         "name:eq:John|name:eq:Jane pg",
			filterString: "name:eq:John|name:eq:Jane",
			dialect:      PostgreSQL,
			wantSQL:      `"name" = $1 OR "name" = $2`,
			wantParams:   []any{"John", "Jane"},
		},
		{
			name:         "name:eq:John|name:eq:Jane spanner",
			filterString: "name:eq:John|name:eq:Jane",
			dialect:      Spanner,
			wantSQL:      "`name` = @p1 OR `name` = @p2",
			wantParams:   []any{"John", "Jane"},
		},

		// (name:eq:John|name:eq:Jane),age:gte:30
		{
			name:         "(name:eq:John|name:eq:Jane),age:gte:30 pg",
			filterString: "(name:eq:John|name:eq:Jane),age:gte:30",
			dialect:      PostgreSQL,
			wantSQL:      `("name" = $1 OR "name" = $2) AND "age" >= $3`,
			wantParams:   []any{"John", "Jane", "30"},
		},
		{
			name:         "(name:eq:John|name:eq:Jane),age:gte:30 spanner",
			filterString: "(name:eq:John|name:eq:Jane),age:gte:30",
			dialect:      Spanner,
			wantSQL:      "(`name` = @p1 OR `name` = @p2) AND `age` >= @p3",
			wantParams:   []any{"John", "Jane", "30"},
		},

		// category:in:(books,movies)
		{
			name:         "category:in:(books,movies) pg",
			filterString: "category:in:(books,movies)",
			dialect:      PostgreSQL,
			wantSQL:      `"category" IN ($1, $2)`,
			wantParams:   []any{"books", "movies"},
		},
		{
			name:         "category:in:(books,movies) spanner",
			filterString: "category:in:(books,movies)",
			dialect:      Spanner,
			wantSQL:      "`category` IN (@p1, @p2)",
			wantParams:   []any{"books", "movies"},
		},
		// category:in:(single)
		{
			name:         "category:in:(single) pg",
			filterString: "category:in:(single)",
			dialect:      PostgreSQL,
			wantSQL:      `"category" IN ($1)`,
			wantParams:   []any{"single"},
		},

		// user_id:notin:(1,2,3)
		{
			name:         "user_id:notin:(1,2,3) pg",
			filterString: "user_id:notin:(1,2,3)",
			dialect:      PostgreSQL,
			wantSQL:      `"user_id" NOT IN ($1, $2, $3)`,
			wantParams:   []any{"1", "2", "3"},
		},

		// (category:in:(books,movies)|status:eq:active),price:lt:100
		{
			name:         "(category:in:(books,movies)|status:eq:active),price:lt:100 pg",
			filterString: "(category:in:(books,movies)|status:eq:active),price:lt:100",
			dialect:      PostgreSQL,
			wantSQL:      `("category" IN ($1, $2) OR "status" = $3) AND "price" < $4`,
			wantParams:   []any{"books", "movies", "active", "100"},
		},
		{
			name:         "(category:in:(books,movies)|status:eq:active),price:lt:100 spanner",
			filterString: "(category:in:(books,movies)|status:eq:active),price:lt:100",
			dialect:      Spanner,
			wantSQL:      "(`category` IN (@p1, @p2) OR `status` = @p3) AND `price` < @p4",
			wantParams:   []any{"books", "movies", "active", "100"},
		},
		// name:eq:John Doe
		{
			name:         "name:eq:John Doe pg",
			filterString: "name:eq:John Doe",
			dialect:      PostgreSQL,
			wantSQL:      `"name" = $1`,
			wantParams:   []any{"John Doe"},
		},
		// category:in:(sci-fi,non-fiction)
		{
			name:         "category:in:(sci-fi,non-fiction) pg",
			filterString: "category:in:(sci-fi,non-fiction)",
			dialect:      PostgreSQL,
			wantSQL:      `"category" IN ($1, $2)`,
			wantParams:   []any{"sci-fi", "non-fiction"},
		},
		// email:isnotnull,age:gt:18
		{
			name:         "email:isnotnull,age:gt:18 pg",
			filterString: "email:isnotnull,age:gt:18",
			dialect:      PostgreSQL,
			wantSQL:      `"email" IS NOT NULL AND "age" > $1`,
			wantParams:   []any{"18"},
		},
		// (name:isnull|name:eq:Unknown)
		{
			name:         "(name:isnull|name:eq:Unknown) pg",
			filterString: "(name:isnull|name:eq:Unknown)",
			dialect:      PostgreSQL,
			wantSQL:      `("name" IS NULL OR "name" = $1)`,
			wantParams:   []any{"Unknown"},
		},
		// (name:eq:John|name:eq:Jane),(category:in:(books,movies)|status:eq:active)
		{
			name:         "(name:eq:John|name:eq:Jane),(category:in:(books,movies)|status:eq:active) pg",
			filterString: "(name:eq:John|name:eq:Jane),(category:in:(books,movies)|status:eq:active)",
			dialect:      PostgreSQL,
			wantSQL:      `("name" = $1 OR "name" = $2) AND ("category" IN ($3, $4) OR "status" = $5)`,
			wantParams:   []any{"John", "Jane", "books", "movies", "active"},
		},
		// ((status:eq:active|status:eq:pending),user_id:notin:(1,2)),price:gte:50
		{
			name:         "((status:eq:active|status:eq:pending),user_id:notin:(1,2)),price:gte:50 pg",
			filterString: "((status:eq:active|status:eq:pending),user_id:notin:(1,2)),price:gte:50",
			dialect:      PostgreSQL,
			wantSQL:      `(("status" = $1 OR "status" = $2) AND "user_id" NOT IN ($3, $4)) AND "price" >= $5`,
			wantParams:   []any{"active", "pending", "1", "2", "50"},
		},
		// Test for "ne" operator
		{
			name:         "status:ne:inactive pg",
			filterString: "status:ne:inactive",
			dialect:      PostgreSQL,
			wantSQL:      `"status" <> $1`,
			wantParams:   []any{"inactive"},
		},
		{
			name:         "status:ne:inactive spanner",
			filterString: "status:ne:inactive",
			dialect:      Spanner,
			wantSQL:      "`status` <> @p1",
			wantParams:   []any{"inactive"},
		},
		// Test for "lt" operator
		{
			name:         "price:lt:10 pg",
			filterString: "price:lt:10",
			dialect:      PostgreSQL,
			wantSQL:      `"price" < $1`,
			wantParams:   []any{"10"},
		},
		// Test for "lte" operator
		{
			name:         "stock:lte:5 pg",
			filterString: "stock:lte:5",
			dialect:      PostgreSQL,
			wantSQL:      `"stock" <= $1`,
			wantParams:   []any{"5"},
		},
		// Test for "gt" operator
		{
			name:         "rating:gt:4 pg",
			filterString: "rating:gt:4",
			dialect:      PostgreSQL,
			wantSQL:      `"rating" > $1`,
			wantParams:   []any{"4"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			lexer := NewLexer(tt.filterString)
			parser, err := NewParser(lexer)
			if err != nil {
				t.Fatalf("NewParser() error = %v, wantErr %v", err, tt.wantErrMsg)
			}

			ast, parseErr := parser.Parse()
			// Check if parsing itself was expected to fail
			if tt.wantErrMsg != "" { // This test case is designed to check generation errors or specific parse errors
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

			sqlGen := NewSQLGenerator(tt.dialect)
			gotSQL, gotParams, genErr := sqlGen.GenerateSQL(ast)

			if tt.wantErrMsg != "" {
				if genErr == nil {
					t.Errorf("sqlGen.GenerateSQL() error = nil, wantErrMsg %s", tt.wantErrMsg)
				} else if !strings.Contains(genErr.Error(), tt.wantErrMsg) {
					t.Errorf("sqlGen.GenerateSQL() error = %v, wantErrMsg %s", genErr, tt.wantErrMsg)
				}
				return // Expected error, test done
			}
			if genErr != nil {
				t.Errorf("sqlGen.GenerateSQL() error = %v, want nil", genErr)
				return
			}

			if gotSQL != tt.wantSQL {
				t.Errorf("sqlGen.GenerateSQL() gotSQL = %q, want %q", gotSQL, tt.wantSQL)
			}
			if !reflect.DeepEqual(gotParams, tt.wantParams) {
				t.Errorf("sqlGen.GenerateSQL() gotParams = %v, want %v", gotParams, tt.wantParams)
			}
		})
	}
}
