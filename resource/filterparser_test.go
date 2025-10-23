package resource

import (
	"reflect"
	"strings"
	"testing"

	"github.com/cccteam/httpio"
)

// defaultTestJSONToSQLNameMap provides a standard map for most test cases.
var defaultTestJSONToSQLNameMap = map[jsonFieldName]FilterFieldInfo{
	"status":   {dbColumnNames: map[DBType]string{SpannerDBType: "Status"}, Kind: reflect.String, Indexed: true},
	"user_id":  {dbColumnNames: map[DBType]string{SpannerDBType: "UserId"}, Kind: reflect.Int, Indexed: true},
	"price":    {dbColumnNames: map[DBType]string{SpannerDBType: "Price"}, Kind: reflect.Float64, Indexed: true},
	"stock":    {dbColumnNames: map[DBType]string{SpannerDBType: "Stock"}, Kind: reflect.Int, Indexed: true},
	"rating":   {dbColumnNames: map[DBType]string{SpannerDBType: "Rating"}, Kind: reflect.Int, Indexed: true},
	"name":     {dbColumnNames: map[DBType]string{SpannerDBType: "Name"}, Kind: reflect.String, Indexed: true},
	"age":      {dbColumnNames: map[DBType]string{SpannerDBType: "Age"}, Kind: reflect.Int64, Indexed: true},
	"category": {dbColumnNames: map[DBType]string{SpannerDBType: "Category"}, Kind: reflect.String, Indexed: true},
	"email":    {dbColumnNames: map[DBType]string{SpannerDBType: "Email"}, Kind: reflect.String, Indexed: true},
	"active":   {dbColumnNames: map[DBType]string{SpannerDBType: "Active"}, Kind: reflect.Bool, Indexed: true},
	"field":    {dbColumnNames: map[DBType]string{SpannerDBType: "Field"}, Kind: reflect.String, Indexed: true},
}

func TestNewLexer(t *testing.T) {
	t.Parallel()

	type args struct {
		input string
	}
	tests := []struct {
		name    string
		args    args
		want    []Token
		wantErr bool
	}{
		{
			name: "name:eq:John",
			args: args{
				input: "name:eq:John",
			},
			want: []Token{
				{Type: TokenCondition, Value: "name:eq:John"},
			},
		},
		{
			name: "age:gte:30",
			args: args{
				input: "age:gte:30",
			},
			want: []Token{
				{Type: TokenCondition, Value: "age:gte:30"},
			},
		},
		{
			name: "status:isnull",
			args: args{
				input: "status:isnull",
			},
			want: []Token{
				{Type: TokenCondition, Value: "status:isnull"},
			},
		},
		{
			name: "name:eq:John,age:gte:30",
			args: args{
				input: "name:eq:John,age:gte:30",
			},
			want: []Token{
				{Type: TokenCondition, Value: "name:eq:John"},
				{Type: TokenComma, Value: ","},
				{Type: TokenCondition, Value: "age:gte:30"},
			},
		},
		{
			name: "name:eq:John|name:eq:Jane",
			args: args{
				input: "name:eq:John|name:eq:Jane",
			},
			want: []Token{
				{Type: TokenCondition, Value: "name:eq:John"},
				{Type: TokenPipe, Value: "|"},
				{Type: TokenCondition, Value: "name:eq:Jane"},
			},
		},
		{
			name: "(name:eq:John|name:eq:Jane),age:gte:30",
			args: args{
				input: "(name:eq:John|name:eq:Jane),age:gte:30",
			},
			want: []Token{
				{Type: TokenLParen, Value: "("},
				{Type: TokenCondition, Value: "name:eq:John"},
				{Type: TokenPipe, Value: "|"},
				{Type: TokenCondition, Value: "name:eq:Jane"},
				{Type: TokenRParen, Value: ")"},
				{Type: TokenComma, Value: ","},
				{Type: TokenCondition, Value: "age:gte:30"},
			},
		},
		{
			name: "(category:in:(books,movies)|status:eq:active),price:lt:100",
			args: args{
				input: "(category:in:(books,movies)|status:eq:active),price:lt:100",
			},
			want: []Token{
				{Type: TokenLParen, Value: "("},
				{Type: TokenCondition, Value: "category:in:(books,movies)"},
				{Type: TokenPipe, Value: "|"},
				{Type: TokenCondition, Value: "status:eq:active"},
				{Type: TokenRParen, Value: ")"},
				{Type: TokenComma, Value: ","},
				{Type: TokenCondition, Value: "price:lt:100"},
			},
		},
		{
			name: "(status:eq:active|category:in:(books,movies)),price:lt:100",
			args: args{
				input: "(status:eq:active|category:in:(books,movies)),price:lt:100",
			},
			want: []Token{
				{Type: TokenLParen, Value: "("},
				{Type: TokenCondition, Value: "status:eq:active"},
				{Type: TokenPipe, Value: "|"},
				{Type: TokenCondition, Value: "category:in:(books,movies)"},
				{Type: TokenRParen, Value: ")"},
				{Type: TokenComma, Value: ","},
				{Type: TokenCondition, Value: "price:lt:100"},
			},
		},
		{
			name: "category:in:(books,movies)",
			args: args{
				input: "category:in:(books,movies)",
			},
			want: []Token{
				{Type: TokenCondition, Value: "category:in:(books,movies)"},
			},
		},
		{
			name: "user_id:notin:(1,2,3)",
			args: args{
				input: "user_id:notin:(1,2,3)",
			},
			want: []Token{
				{Type: TokenCondition, Value: "user_id:notin:(1,2,3)"},
			},
		},
		{
			name: "name:eq:John Doe",
			args: args{
				input: "name:eq:John Doe",
			},
			want: []Token{
				{Type: TokenCondition, Value: "name:eq:John Doe"},
			},
		},
		{
			name: "category:in:(sci-fi,non-fiction)",
			args: args{
				input: "category:in:(sci-fi,non-fiction)",
			},
			want: []Token{
				{Type: TokenCondition, Value: "category:in:(sci-fi,non-fiction)"},
			},
		},
		{
			name: "email:isnotnull,age:gt:18",
			args: args{
				input: "email:isnotnull,age:gt:18",
			},
			want: []Token{
				{Type: TokenCondition, Value: "email:isnotnull"},
				{Type: TokenComma, Value: ","},
				{Type: TokenCondition, Value: "age:gt:18"},
			},
		},
		{
			name: "(name:isnull|name:eq:Unknown)",
			args: args{
				input: "(name:isnull|name:eq:Unknown)",
			},
			want: []Token{
				{Type: TokenLParen, Value: "("},
				{Type: TokenCondition, Value: "name:isnull"},
				{Type: TokenPipe, Value: "|"},
				{Type: TokenCondition, Value: "name:eq:Unknown"},
				{Type: TokenRParen, Value: ")"},
			},
		},
		{
			name: "(name:eq:John|name:eq:Jane),(category:in:(books,movies)|status:eq:active)",
			args: args{
				input: "(name:eq:John|name:eq:Jane),(category:in:(books,movies)|status:eq:active)",
			},
			want: []Token{
				{Type: TokenLParen, Value: "("},
				{Type: TokenCondition, Value: "name:eq:John"},
				{Type: TokenPipe, Value: "|"},
				{Type: TokenCondition, Value: "name:eq:Jane"},
				{Type: TokenRParen, Value: ")"},
				{Type: TokenComma, Value: ","},
				{Type: TokenLParen, Value: "("},
				{Type: TokenCondition, Value: "category:in:(books,movies)"},
				{Type: TokenPipe, Value: "|"},
				{Type: TokenCondition, Value: "status:eq:active"},
				{Type: TokenRParen, Value: ")"},
			},
		},
		{
			name: "(status:eq:active|status:eq:pending),user_id:notin:(1,2)",
			args: args{
				input: "(status:eq:active|status:eq:pending),user_id:notin:(1,2)",
			},
			want: []Token{
				{Type: TokenLParen, Value: "("},
				{Type: TokenCondition, Value: "status:eq:active"},
				{Type: TokenPipe, Value: "|"},
				{Type: TokenCondition, Value: "status:eq:pending"},
				{Type: TokenRParen, Value: ")"},
				{Type: TokenComma, Value: ","},
				{Type: TokenCondition, Value: "user_id:notin:(1,2)"},
			},
		},
		{
			name: "empty input",
			args: args{
				input: "",
			},
			want: []Token{},
		},
		{
			name: "1=1",
			args: args{
				input: "1=1",
			},
			want: []Token{
				{Type: TokenCondition, Value: "1=1"},
			},
		},
		{
			name: "category:in:(single)",
			args: args{
				input: "category:in:(single)",
			},
			want: []Token{
				{Type: TokenCondition, Value: "category:in:(single)"},
			},
		},
		{
			name: "nested parentheses in condition token",
			args: args{
				input: "category:in:(value,(value,value))",
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			l := NewFilterLexer(tt.args.input)
			var tokens []Token
			for {
				token, err := l.NextToken()
				if tt.wantErr != (err != nil) {
					t.Fatalf("l.NextToken() returned error: %v", err)
				}
				if token.Type == TokenEOF {
					break
				}
				tokens = append(tokens, token)
			}
			if len(tokens) != len(tt.want) {
				t.Fatalf("NewLexer() = %v, want %v. Count mismatch: got %d, want %d", tokens, tt.want, len(tokens), len(tt.want))
			}
			for i := range tokens {
				if tokens[i] != tt.want[i] {
					t.Errorf("Token mismatch at index %d: NewLexer() = %v, want %v", i, tokens[i], tt.want[i])
				}
			}
		})
	}
}

func TestParser_Parse_Errors(t *testing.T) {
	t.Parallel()
	type errorTestCase struct {
		name               string
		filterString       string
		wantErrMsgContains string
		customMap          map[jsonFieldName]FilterFieldInfo
		isHTTPError        bool
	}
	tests := []errorTestCase{
		{
			name:               "invalid condition - missing value",
			filterString:       "name:eq",
			wantErrMsgContains: "operator 'eq' requires a value",
			isHTTPError:        true,
		},
		{
			name:               "invalid condition - empty field",
			filterString:       ":eq:value",
			wantErrMsgContains: "field name cannot be empty in condition ':eq:value'",
			isHTTPError:        true,
		},
		{
			name:               "in operator with empty value list",
			filterString:       "category:in:()",
			wantErrMsgContains: "value list for 'in' cannot be empty in condition 'category:in:()'",
			isHTTPError:        true,
		},
		{
			name:               "notin operator with empty value list",
			filterString:       "category:notin:()",
			wantErrMsgContains: "value list for 'notin' cannot be empty in condition 'category:notin:()'",
			isHTTPError:        true,
		},
		{
			name:               "unknown operator",
			filterString:       "name:badop:John",
			wantErrMsgContains: "unknown operator 'badop' in condition 'name:badop:John'",
			isHTTPError:        true,
		},
		{
			name:               "missing closing parenthesis",
			filterString:       "(name:eq:John",
			wantErrMsgContains: "expected next token to be TokenRParen, got TokenEOF instead",
			isHTTPError:        true,
		},
		{
			name:               "unmatched closing parenthesis at start",
			filterString:       ")name:eq:John",
			wantErrMsgContains: "Invalid filter query. Unexpected token ')' (type: TokenRParen) at the beginning of an expression",
			isHTTPError:        true,
		},
		{
			name:               "unmatched closing parenthesis after expression",
			filterString:       "name:eq:John)",
			wantErrMsgContains: "Invalid filter query. Unexpected characters ')' (type: TokenRParen) found after the end of the query",
			isHTTPError:        true,
		},
		{
			name:               "unexpected token - double comma",
			filterString:       "name:eq:John,,age:gte:30",
			wantErrMsgContains: "Invalid filter query. Unexpected token ',' (type: TokenComma) at the beginning of an expression",
			isHTTPError:        true,
		},
		{
			name:               "operator isnull with value",
			filterString:       "name:isnull:extra",
			wantErrMsgContains: "operator 'isnull' does not take a value, but got 'extra' in condition 'name:isnull:extra'",
			isHTTPError:        true,
		},
		{
			name:               "operator isnotnull with value",
			filterString:       "name:isnotnull:extra",
			wantErrMsgContains: "operator 'isnotnull' does not take a value, but got 'extra' in condition 'name:isnotnull:extra'",
			isHTTPError:        true,
		},
		{
			name:               "empty group",
			filterString:       "()",
			wantErrMsgContains: "Invalid filter query. Empty groups '()' are not allowed.",
			isHTTPError:        true,
		},
		{
			name:               "group with only operator",
			filterString:       "(,)",
			wantErrMsgContains: "Invalid filter query. Unexpected token ',' (type: TokenComma) at the beginning of an expression",
			isHTTPError:        true,
		},
		{
			name:               "trailing operator comma",
			filterString:       "name:eq:John,",
			wantErrMsgContains: "Invalid filter query. Unexpected token '' (type: TokenEOF) at the beginning of an expression",
			isHTTPError:        true,
		},
		{
			name:               "trailing operator pipe",
			filterString:       "name:eq:John|",
			wantErrMsgContains: "Invalid filter query. Unexpected token '' (type: TokenEOF) at the beginning of an expression",
			isHTTPError:        true,
		},
		{
			name:               "leading operator comma - NEW",
			filterString:       ",name:eq:John",
			wantErrMsgContains: "Invalid filter query. Unexpected token ',' (type: TokenComma) at the beginning of an expression",
			isHTTPError:        true,
		},
		{
			name:               "leading operator pipe",
			filterString:       "|name:eq:John",
			wantErrMsgContains: "Invalid filter query. Unexpected token '|' (type: TokenPipe) at the beginning of an expression",
			isHTTPError:        true,
		},
		{
			name:               "condition with invalid value format for in",
			filterString:       "field:in:novalue",
			wantErrMsgContains: "value for 'in' must be in parentheses, e.g., (v1,v2), got 'novalue' in condition 'field:in:novalue'",
			isHTTPError:        true,
		},
		{
			name:               "condition with invalid value format for notin (missing closing paren)",
			filterString:       "field:notin:(v1,v2",
			wantErrMsgContains: "value for 'notin' must be in parentheses, e.g., (v1,v2), got '(v1,v2' in condition 'field:notin:(v1,v2'",
			isHTTPError:        true,
		},
		{
			name:               "condition with empty item in 'in' list",
			filterString:       "field:in:(v1,,v2)",
			wantErrMsgContains: "empty value in list for operator 'in' in condition 'field:in:(v1,,v2)'",
			isHTTPError:        true,
		},
		{
			name:               "invalid field name - using empty map",
			filterString:       "unknown_field:eq:value",
			wantErrMsgContains: "'unknown_field' is not indexed but was included in condition 'unknown_field:eq:value'",
			customMap:          map[jsonFieldName]FilterFieldInfo{},
			isHTTPError:        true,
		},
		{
			name:               "invalid field name in group - using empty map",
			filterString:       "(unknown_field:eq:value,another_unknown:eq:Test)",
			customMap:          map[jsonFieldName]FilterFieldInfo{},
			wantErrMsgContains: "'unknown_field' is not indexed but was included in condition 'unknown_field:eq:value'",
			isHTTPError:        true,
		},
		{
			name:               "invalid field name with pipe - using map without the specific field",
			filterString:       "name:eq:Test|unknown_field:eq:value",
			customMap:          map[jsonFieldName]FilterFieldInfo{"name": {dbColumnNames: map[DBType]string{SpannerDBType: "Name"}, Kind: reflect.String}},
			wantErrMsgContains: "'unknown_field' is not indexed but was included in condition 'unknown_field:eq:value'",
			isHTTPError:        true,
		},
		// New test cases
		{
			name:               "unexpected token at beginning (original issue)",
			filterString:       ",submittalSource:eq:M",
			wantErrMsgContains: "Invalid filter query. Unexpected token ',' (type: TokenComma) at the beginning of an expression",
			isHTTPError:        true,
		},
		{
			name:               "two conditions back-to-back - (Lexer makes this one token, so parser sees valid single condition)",
			filterString:       "name:eq:Value1 name:eq:Value2",
			wantErrMsgContains: "", // No error expected with current lexer/parser (i.e. will become name = 'Value1 name:eq:Value2')
			isHTTPError:        false,
		},
		{
			name:               "unexpected characters at end - revised (Lexer makes this one token, so parser sees valid single condition)",
			filterString:       "name:eq:Value1 (name:eq:Value2)",
			wantErrMsgContains: "", // No error expected with current lexer
			isHTTPError:        false,
		},
		{
			name:               "nested parentheses in condition token (Lexer error)",
			filterString:       "field:op:(val(nested))",
			wantErrMsgContains: "Invalid filter query. Nested parentheses are not allowed within a single condition segment. Found near character 14 of field:op:(val(nested))",
			isHTTPError:        true,
		},
		{
			name:         "unsupported value type in convertValue",
			filterString: "unsupported_field:eq:somevalue",
			customMap: map[jsonFieldName]FilterFieldInfo{
				"unsupported_field": {dbColumnNames: map[DBType]string{SpannerDBType: "UnsupportedField"}, Kind: reflect.Chan, Indexed: true},
			},
			wantErrMsgContains: "Invalid value format. The value 'somevalue' in condition 'unsupported_field:eq:somevalue' cannot be processed due to an unsupported data type: chan.",
			isHTTPError:        true,
		},
		{
			name:               "Lexer error during advance (NewParser)",
			filterString:       "field:op:(val(nes)ted)",
			wantErrMsgContains: "Invalid filter query. Nested parentheses are not allowed",
			isHTTPError:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			lexer := NewFilterLexer(tt.filterString)
			currentMap := defaultTestJSONToSQLNameMap
			if tt.customMap != nil {
				currentMap = tt.customMap
			}

			// Test NewParser for errors first, especially for lexer errors
			parser, err := NewFilterParser(lexer, currentMap)
			if err != nil { // Error from NewParser
				if tt.wantErrMsgContains == "" {
					t.Fatalf("NewParser() error = %v, want no error. Input: %q", err, tt.filterString)
				}
				if !strings.Contains(err.Error(), tt.wantErrMsgContains) {
					t.Fatalf("NewParser() error = %q, wantErrMsg substring %q. Input: %q", err.Error(), tt.wantErrMsgContains, tt.filterString)
				}
				if tt.isHTTPError {
					if !httpio.HasBadRequest(err) {
						t.Errorf("NewParser() error: Input %q: Expected HTTP 400 Bad Request error, but got: %v", tt.filterString, err)
					}
				}

				return // Test ends here if NewParser fails as expected
			}

			// Proceed to test parser.Parse() if NewParser succeeded
			_, parseErr := parser.Parse(SpannerDBType)
			if tt.wantErrMsgContains == "" { // No error expected from Parse()
				if parseErr != nil {
					t.Errorf("parser.Parse() error = %v, want no error. Input: %q", parseErr, tt.filterString)
				}

				return // Test ends here if no error was expected from Parse()
			}

			// An error was expected from Parse()
			if parseErr == nil {
				t.Fatalf("parser.Parse() error = nil, wantErrMsg substring %q. Input: %q", tt.wantErrMsgContains, tt.filterString)
			}

			if !strings.Contains(parseErr.Error(), tt.wantErrMsgContains) {
				t.Errorf("parser.Parse() error = %q, wantErrMsg substring %q. Input: %q", parseErr.Error(), tt.wantErrMsgContains, tt.filterString)
			}

			if tt.isHTTPError {
				if !httpio.HasBadRequest(parseErr) {
					t.Errorf("parser.Parse() error: Input %q: Expected HTTP 400 Bad Request error, but got: %v", tt.filterString, parseErr)
				}
			}
		})
	}
}

func TestParser_Parse_Successful(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		filterString string
		wantNode     ExpressionNode
	}{
		{
			name:         "simple condition with status",
			filterString: "status:eq:active",
			wantNode:     &ConditionNode{Condition: Condition{Field: "Status", Operator: eqStr, Value: "active"}},
		},
		{
			name:         "simple condition with empty status",
			filterString: "status:eq:",
			wantNode:     &ConditionNode{Condition: Condition{Field: "Status", Operator: eqStr, Value: ""}},
		},
		{
			name:         "True condition with active",
			filterString: "active:eq:true",
			wantNode:     &ConditionNode{Condition: Condition{Field: "Active", Operator: eqStr, Value: true}},
		},
		{
			name:         "False condition with active",
			filterString: "active:eq:false",
			wantNode:     &ConditionNode{Condition: Condition{Field: "Active", Operator: eqStr, Value: false}},
		},
		{
			name:         "simple condition with user_id",
			filterString: "user_id:in:(1,2,3)",
			wantNode:     &ConditionNode{Condition: Condition{Field: "UserId", Operator: inStr, Values: []any{1, 2, 3}}},
		},
		{
			name:         "simple condition with price",
			filterString: "price:gte:100.50",
			wantNode:     &ConditionNode{Condition: Condition{Field: "Price", Operator: gteStr, Value: 100.50}},
		},
		{
			name:         "simple condition with name - mapped",
			filterString: "name:eq:Test Name",
			wantNode:     &ConditionNode{Condition: Condition{Field: "Name", Operator: eqStr, Value: "Test Name"}},
		},
		{
			name:         "simple condition with age - mapped",
			filterString: "age:lt:30",
			wantNode:     &ConditionNode{Condition: Condition{Field: "Age", Operator: ltStr, Value: "30"}},
		},
		{
			name:         "simple condition with category - mapped",
			filterString: "category:isnotnull",
			wantNode:     &ConditionNode{Condition: Condition{Field: "Category", Operator: "isnotnull", IsNullOp: true}},
		},
		{
			name:         "simple condition with email - mapped",
			filterString: "email:notin:(a@b.com,c@d.com)",
			wantNode:     &ConditionNode{Condition: Condition{Field: "Email", Operator: notinStr, Values: []any{"a@b.com", "c@d.com"}}},
		},
		{
			name:         "grouped condition with translated fields",
			filterString: "(user_id:eq:10,status:eq:pending)|price:gt:50",
			wantNode: &LogicalOpNode{
				Left: &GroupNode{
					Expression: &LogicalOpNode{
						Left:     &ConditionNode{Condition: Condition{Field: "UserId", Operator: eqStr, Value: 10}},
						Operator: OperatorAnd,
						Right:    &ConditionNode{Condition: Condition{Field: "Status", Operator: eqStr, Value: "pending"}},
					},
				},
				Operator: OperatorOr,
				Right:    &ConditionNode{Condition: Condition{Field: "Price", Operator: gtStr, Value: 50}},
			},
		},
		{
			name:         "empty filter string",
			filterString: "",
			wantNode:     nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			lexer := NewFilterLexer(tt.filterString)
			parser, err := NewFilterParser(lexer, defaultTestJSONToSQLNameMap)
			if err != nil {
				t.Fatalf("NewParser() error = %v for input '%s'", err, tt.filterString)
			}

			gotNode, parseErr := parser.Parse(SpannerDBType)
			if parseErr != nil {
				t.Fatalf("parser.Parse() for input '%s', error = %v, want no error for successful parse tests", tt.filterString, parseErr)
			}

			var gotNodeStr, wantNodeStr string
			if gotNode != nil {
				gotNodeStr = gotNode.String()
			}
			if tt.wantNode != nil {
				wantNodeStr = tt.wantNode.String()
			}

			if gotNodeStr != wantNodeStr {
				t.Errorf("parser.Parse() for input '%s'\ngotNode = %s\nwantNode = %s", tt.filterString, gotNodeStr, wantNodeStr)
			}
		})
	}
}
