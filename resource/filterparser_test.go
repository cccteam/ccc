package resource

import (
	"testing"
)

func TestNewLexer(t *testing.T) {
	t.Parallel()

	type args struct {
		input string
	}
	tests := []struct {
		name string
		args args
		want []Token
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Write the test
			l := NewLexer(tt.args.input)
			var tokens []Token
			for {
				token, err := l.NextToken()
				if err != nil {
					break
				}
				tokens = append(tokens, token)
			}
			if len(tokens) != len(tt.want) {
				t.Errorf("NewLexer() = %v, want %v", tokens, tt.want)
			}
			for i := range tokens {
				if i >= len(tt.want) || tokens[i] != tt.want[i] {
					t.Errorf("NewLexer() = %v, want %v", tokens, tt.want)
				}
			}
		})
	}
}
