package resource

import (
	"strings"
	"testing"
)

// DONE: Implement a lexer that will lex the filter values and produce tokens.
// DONE: Implement a parser that uses the tokens to generate the SQL output.

/*
filter: name:eq:John
SQL: name = ?
Params: [John]

filter: age:gte:30
SQL: age >= ?
Params: [30]

filter: status:isnull
SQL: status IS NULL
Params: []

filter: name:eq:John,age:gte:30
SQL: name = ? AND age >= ?
Params: [John 30]

filter: name:eq:John|name:eq:Jane
SQL: name = ? OR name = ?
Params: [John Jane]

filter: (name:eq:John|name:eq:Jane),age:gte:30
SQL: (name = ? OR name = ?) AND age >= ?
Params: [John Jane 30]

filter: (category:in:(books,movies)|status:eq:active),price:lt:100
SQL: (category IN (?, ?) OR status = ?) AND price < ?
Params: [books movies active 100]

filter: category:in:(books,movies)
SQL: category IN (?, ?)
Params: [books movies]

filter: user_id:notin:(1,2,3)
SQL: user_id NOT IN (?, ?, ?)
Params: [1 2 3]

filter: name:eq:John Doe
SQL: name = ?
Params: [John Doe]

filter: category:in:(sci-fi,non-fiction)
SQL: category IN (?, ?)
Params: [sci-fi non-fiction]

filter: email:isnotnull,age:gt:18
SQL: email IS NOT NULL AND age > ?
Params: [18]

filter: (name:isnull|name:eq:Unknown)
SQL: name IS NULL OR name = ?
Params: [Unknown]

filter: (name:eq:John|name:eq:Jane),(category:in:(books,movies)|status:eq:active)
SQL: (name = ? OR name = ?) AND (category IN (?, ?) OR status = ?)
Params: [John Jane books movies active]

filter: ((status:eq:active|status:eq:pending),user_id:notin:(1,2)),price:gte:50
SQL: ((status = ? OR status = ?) AND user_id NOT IN (?, ?)) AND price >= ?
Params: [active pending 1 2 50]

filter:
SQL: 1=1
Params: []

filter: category:in:(single)
SQL: category IN (?)
Params: [single]

filter: name:eq
SQL: Error: invalid condition: name:eq

filter: category:in:()
SQL: Error: in/notin require non-empty value list: category:in:()

filter: secret:eq:hack
SQL: Error: invalid field: secret

filter: name:bad:John
SQL: Error: invalid operator: bad

filter: (name:eq:John
SQL: Error: missing closing parenthesis

filter: name:eq:John)
SQL: Error: unmatched closing parenthesis at position ...

filter: category:in:(books,(nested))
SQL: Error: nested parentheses in condition at position ...

filter: name:eq:John,,age:gte:30
SQL: Error: invalid condition at position ...

filter: name:isnull:extra
SQL: Error: isnull/isnotnull take no value: name:isnull:extra

*/

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
				{Type: TokenType(TokenComma), Value: ","},
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
				{Type: TokenType(TokenComma), Value: ","},
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
				{Type: TokenType(TokenComma), Value: ","},
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
				{Type: TokenType(TokenPipe), Value: "|"},
				{Type: TokenType(TokenCondition), Value: "name:eq:Jane"},
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
				{Type: TokenType(TokenPipe), Value: "|"},
				{Type: TokenType(TokenCondition), Value: "status:eq:pending"},
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

			l := NewLexer(tt.args.input)
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
	tests := []struct {
		name         string
		filterString string
		wantErrMsg   string
	}{
		{
			name:         "invalid condition - missing value",
			filterString: "name:eq",
			wantErrMsg:   "operator 'eq' requires a value",
		},
		{
			name:         "invalid condition - empty field",
			filterString: ":eq:value",
			wantErrMsg:   "field name cannot be empty",
		},
		{
			name:         "in operator with empty value list",
			filterString: "category:in:()",
			wantErrMsg:   "value list for 'in' cannot be empty",
		},
		{
			name:         "notin operator with empty value list",
			filterString: "category:notin:()",
			wantErrMsg:   "value list for 'notin' cannot be empty",
		},
		{
			name:         "unknown operator",
			filterString: "name:badop:John",
			wantErrMsg:   "unknown operator",
		},
		{
			name:         "missing closing parenthesis",
			filterString: "(name:eq:John",
			wantErrMsg:   "expected peek token to be 2, got 0",
		},
		{
			name:         "unmatched closing parenthesis at start",
			filterString: ")name:eq:John",
			wantErrMsg:   "no prefix parse function for token type 2",
		},
		{
			name:         "unmatched closing parenthesis after expression",
			filterString: "name:eq:John)",
			wantErrMsg:   "expected EOF after parsing, got 2",
		},
		{
			name:         "unexpected token - double comma",
			filterString: "name:eq:John,,age:gte:30",
			wantErrMsg:   "no prefix parse function for token type 3",
		},
		{
			name:         "operator isnull with value",
			filterString: "name:isnull:extra",
			wantErrMsg:   "operator 'isnull' does not take a value",
		},
		{
			name:         "operator isnotnull with value",
			filterString: "name:isnotnull:extra",
			wantErrMsg:   "operator 'isnotnull' does not take a value",
		},
		{
			name:         "empty group",
			filterString: "()",
			wantErrMsg:   "empty group '()' is not allowed",
		},
		{
			name:         "group with only operator",
			filterString: "(,)",
			wantErrMsg:   "no prefix parse function for token type 3",
		},
		{
			name:         "trailing operator comma",
			filterString: "name:eq:John,",
			wantErrMsg:   "no prefix parse function for token type 0",
		},
		{
			name:         "trailing operator pipe",
			filterString: "name:eq:John|",
			wantErrMsg:   "no prefix parse function for token type 0",
		},
		{
			name:         "leading operator comma",
			filterString: ",name:eq:John",
			wantErrMsg:   "no prefix parse function for token type 3",
		},
		{
			name:         "leading operator pipe",
			filterString: "|name:eq:John",
			wantErrMsg:   "no prefix parse function for token type 4",
		},
		{
			name:         "condition with invalid value format for in",
			filterString: "field:in:novalue",
			wantErrMsg:   "value for 'in' must be in parentheses",
		},
		{
			name:         "condition with invalid value format for notin (missing closing paren)",
			filterString: "field:notin:(v1,v2",
			wantErrMsg:   "value for 'notin' must be in parentheses",
		},
		{
			name:         "condition with empty item in 'in' list",
			filterString: "field:in:(v1,,v2)",
			wantErrMsg:   "empty value in list for operator 'in'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			lexer := NewLexer(tt.filterString)
			parser, initErr := NewParser(lexer) // NewParser itself can't error with current design unless lexer does on advance

			// General parser error handling
			if initErr != nil {
				// This case is unlikely now as NewParser collects errors rather than returning them directly
				if !strings.Contains(initErr.Error(), tt.wantErrMsg) {
					t.Fatalf("NewParser() init error = %v, wantErrMsg %s", initErr, tt.wantErrMsg)
				}
				return
			}

			_, parseErr := parser.Parse()

			if parseErr == nil {
				// If Parse() returns nil, check if errors were collected in the parser
				collectedErrors := parser.Errors()
				if len(collectedErrors) > 0 {
					found := false
					for _, e := range collectedErrors {
						if strings.Contains(e.Error(), tt.wantErrMsg) {
							found = true
							break
						}
					}
					if !found {
						t.Errorf("parser.Parse() error = nil, collected errors = %v, wantErrMsg substring %q", collectedErrors, tt.wantErrMsg)
					}
				} else {
					t.Errorf("parser.Parse() error = nil, wantErrMsg substring %q", tt.wantErrMsg)
				}
			} else {
				// If Parse() returns an error, check it
				if !strings.Contains(parseErr.Error(), tt.wantErrMsg) {
					t.Errorf("parser.Parse() error = %q, wantErrMsg substring %q", parseErr.Error(), tt.wantErrMsg)
				}
			}
		})
	}
}
