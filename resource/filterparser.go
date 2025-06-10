package resource

import (
	"fmt"

	"github.com/go-playground/errors/v5"
)

// TokenType defines the types of tokens in the filter string.
type TokenType int

const (
	TokenLParen    TokenType = iota // (
	TokenRParen                     // )
	TokenComma                      // ,
	TokenPipe                       // |
	TokenCondition                  // field:operator:value or field:operator:(value1,value2,...) or field:operator
)

// Token represents a single token.
type Token struct {
	Type  TokenType
	Value string
}

// Lexer parses the filter producing tokens
type Lexer struct {
	input string
	pos   int
}

func NewLexer(input string) *Lexer {
	return &Lexer{
		input: input,
		pos:   0,
	}
}

func (l *Lexer) NextToken() (Token, error) {
	if l.pos >= len(l.input) {
		return Token{}, errors.New("end of input")
	}

	switch l.input[l.pos] {
	case '(':
		l.pos++

		return Token{Type: TokenLParen, Value: "("}, nil
	case ')':
		l.pos++

		return Token{Type: TokenRParen, Value: ")"}, nil
	case ',':
		l.pos++

		return Token{Type: TokenComma, Value: ","}, nil
	case '|':
		l.pos++

		return Token{Type: TokenPipe, Value: "|"}, nil
	}

	start := l.pos
	parenCount := 0
LOOP:
	for i := l.pos; i < len(l.input); i++ {
		l.pos++
		switch l.input[i] {
		case '(':
			parenCount++
			if parenCount > 1 {
				return Token{}, fmt.Errorf("nested parentheses in condition at position %d", l.pos)
			}
		case ')':
			if parenCount > 0 {
				parenCount--

				continue
			}

			fallthrough
		case ',', '|':
			if parenCount > 0 {
				continue
			}
			l.pos--

			break LOOP
		}
	}

	return Token{Type: TokenCondition, Value: l.input[start:l.pos]}, nil
}

// Condition represents a single condition (e.g., name:eq:John).
type Condition struct {
	Field    string
	Operator string
	Value    string   // For eq, ne, gt, lt, gte, lte
	Values   []string // For in, notin
	IsNullOp bool     // For isnull, isnotnull
}

// Parser builds an AST from tokens.
type Parser struct {
	lexer   *Lexer
	current Token
	peek    Token
}

func NewParser(lexer *Lexer) (*Parser, error) {
	p := &Parser{
		lexer: lexer,
	}
	if err := p.advance(); err != nil {
		return nil, err
	}
	if err := p.advance(); err != nil {
		return nil, err
	}

	return p, nil
}

func (p *Parser) advance() (err error) {
	p.current = p.peek
	p.peek, err = p.lexer.NextToken()
	if err != nil {
		return errors.Wrap(err, "failed to advance parser")
	}

	return nil
}
