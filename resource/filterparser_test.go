package resource

import (
	"strings"
	"testing"
)

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
			wantErrMsg:   "'badop' in condition",
		},
		{
			name:         "missing closing parenthesis",
			filterString: "(name:eq:John",
			wantErrMsg:   "expected peek token to be 2, got 0 instead",
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
		{
			name:         "condition with empty value in 'eq'",
			filterString: "field:eq:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			lexer := NewLexer(tt.filterString)
			parser, err := NewParser(lexer)
			if err != nil {
				if tt.wantErrMsg == "" {
					t.Fatalf("NewParser() error = %v, want no error", err)
				}
				if !strings.Contains(err.Error(), tt.wantErrMsg) {
					t.Fatalf("NewParser() error = %q, wantErrMsg substring %q", err.Error(), tt.wantErrMsg)
				}

				return
			}

			_, parseErr := parser.Parse()
			if tt.wantErrMsg == "" {
				if parseErr != nil {
					t.Errorf("parser.Parse() error = %v, want no error", parseErr)
				}
			} else {
				if parseErr == nil {
					t.Errorf("parser.Parse() error = nil, wantErrMsg substring %q", tt.wantErrMsg)
				} else {
					if !strings.Contains(parseErr.Error(), tt.wantErrMsg) {
						t.Errorf("parser.Parse() error = %q, wantErrMsg substring %q", parseErr.Error(), tt.wantErrMsg)
					}
				}
			}
		})
	}
}
