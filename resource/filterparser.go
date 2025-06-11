package resource

import (
	stderr "errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/go-playground/errors/v5"
)

// Error constants for parsing.
var (
	ErrInvalidConditionFormat = stderr.New("invalid condition format")
	ErrUnknownOperator        = stderr.New("unknown operator")
	ErrMissingValue           = stderr.New("missing value for operator")
	ErrInvalidFieldName       = stderr.New("invalid field name")
	ErrUnexpectedToken        = stderr.New("unexpected token")
	ErrExpectedExpression     = stderr.New("expected an expression")
	ErrExpectedRightParen     = stderr.New("expected ')'")
	ErrInvalidValueFormat     = stderr.New("invalid value format for operator")
)

// TokenType defines the types of tokens in the filter string.
type TokenType int

const (
	TokenEOF       TokenType = iota // End of File
	TokenLParen                     // (
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
		return Token{Type: TokenEOF}, nil
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
				return Token{}, fmt.Errorf("nested parentheses in condition token at position %d", l.pos)
			}
		case ')':
			if parenCount > 0 {
				parenCount--

				continue
			}
			l.pos--

			break LOOP
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

// ExpressionNode represents a node in the filter AST.
type ExpressionNode interface {
	// String returns a string representation of the node (for debugging/testing).
	String() string
}

// Condition represents a single condition (e.g., name:eq:John).
type Condition struct {
	Field    string
	Operator string
	Value    any   // For eq, ne, gt, lt, gte, lte
	Values   []any // For in, notin
	IsNullOp bool  // For isnull, isnotnull
}

// ConditionNode represents a simple condition in the AST.
type ConditionNode struct {
	Condition Condition
}

// String returns a string representation of the ConditionNode.
func (cn *ConditionNode) String() string {
	if cn.Condition.IsNullOp {
		return fmt.Sprintf("%s:%s", cn.Condition.Field, cn.Condition.Operator)
	}
	if len(cn.Condition.Values) > 0 {
		strValues := make([]string, len(cn.Condition.Values))
		for i, v := range cn.Condition.Values {
			strValues[i] = fmt.Sprintf("%v", v)
		}
		return fmt.Sprintf("%s:%s:(%s)", cn.Condition.Field, cn.Condition.Operator, strings.Join(strValues, ","))
	}
	return fmt.Sprintf("%s:%s:%v", cn.Condition.Field, cn.Condition.Operator, cn.Condition.Value)
}

// LogicalOperator defines the type of logical operator (AND, OR).
type LogicalOperator string

const (
	OperatorAnd LogicalOperator = "AND"
	OperatorOr  LogicalOperator = "OR"
)

// LogicalOpNode represents a logical operation (AND/OR) in the AST.
type LogicalOpNode struct {
	Left     ExpressionNode
	Operator LogicalOperator
	Right    ExpressionNode
}

// String returns a string representation of the LogicalOpNode.
func (ln *LogicalOpNode) String() string {
	return fmt.Sprintf("(%s %s %s)", ln.Left.String(), ln.Operator, ln.Right.String())
}

// GroupNode represents a parenthesized group of expressions in the AST.
type GroupNode struct {
	Expression ExpressionNode
}

// String returns a string representation of the GroupNode.
func (gn *GroupNode) String() string {
	return fmt.Sprintf("(%s)", gn.Expression.String())
}

type (
	prefixParseFn func() (ExpressionNode, error)
	infixParseFn  func(ExpressionNode) (ExpressionNode, error)
)

// Parser builds an AST from tokens.
type Parser struct {
	lexer   *Lexer
	current Token
	peek    Token

	prefixParseFns  map[TokenType]prefixParseFn
	infixParseFns   map[TokenType]infixParseFn
	jsonToFieldInfo map[string]FieldInfo
}

func NewParser(lexer *Lexer, jsonToFieldInfo map[string]FieldInfo) (*Parser, error) {
	p := &Parser{
		lexer:           lexer,
		prefixParseFns:  make(map[TokenType]prefixParseFn),
		infixParseFns:   make(map[TokenType]infixParseFn),
		jsonToFieldInfo: jsonToFieldInfo,
	}

	// Register prefix parsing functions
	p.prefixParseFns[TokenCondition] = p.parseConditionToken
	p.prefixParseFns[TokenLParen] = p.parseGroupedExpression

	// Register infix parsing functions
	p.infixParseFns[TokenComma] = p.parseInfixExpression
	p.infixParseFns[TokenPipe] = p.parseInfixExpression

	// Prime the pump. Need to call twice to fill current and peek.
	if err := p.advance(); err != nil {
		return nil, errors.Wrap(err, "failed to advance for current token")
	}
	if err := p.advance(); err != nil {
		return nil, errors.Wrap(err, "failed to advance for peek token")
	}

	return p, nil
}

// Parse is the main entry point for parsing the filter string.
func (p *Parser) Parse() (ExpressionNode, error) {
	if p.current.Type == TokenEOF && p.peek.Type == TokenEOF {
		return nil, nil
	}

	expression, err := p.parseExpression()
	if err != nil {
		return nil, err
	}

	if p.peek.Type != TokenEOF {
		return nil, errors.Wrapf(ErrUnexpectedToken, "expected EOF after parsing, got %v", p.peek.Type)
	}

	return expression, nil
}

func (p *Parser) advance() error {
	p.current = p.peek
	var err error
	p.peek, err = p.lexer.NextToken()
	if err != nil {
		return errors.Wrap(err, "lexer error during advance")
	}

	return nil
}

func (p *Parser) expectPeek(t TokenType) error {
	if p.peek.Type == t {
		return p.advance()
	}

	return errors.Wrapf(ErrUnexpectedToken, "expected peek token to be %v, got %v instead", t, p.peek.Type)
}

func (p *Parser) parseExpression() (ExpressionNode, error) {
	prefix := p.prefixParseFns[p.current.Type]
	if prefix == nil {
		return nil, errors.Wrapf(ErrExpectedExpression, "no prefix parse function for token type %v (value: '%s')", p.current.Type, p.current.Value)
	}

	leftExp, err := prefix()
	if err != nil {
		return nil, err
	}

	for p.peek.Type == TokenComma || p.peek.Type == TokenPipe {
		infix := p.infixParseFns[p.peek.Type]
		if infix == nil {
			// This means we have a token that should be an infix operator but isn't registered,
			// or it's a token that shouldn't appear in an infix position.
			// For example, two conditions back-to-back without an operator.
			return nil, errors.Wrapf(ErrUnexpectedToken, "expected operator, got %v (value: '%s')", p.peek.Type, p.peek.Value)
		}
		if err := p.advance(); err != nil { // Consume the operator
			return nil, err
		}
		leftExp, err = infix(leftExp)
		if err != nil {
			return nil, err // Error already added by infix function
		}
	}

	return leftExp, nil
}

func (p *Parser) parseInfixExpression(left ExpressionNode) (ExpressionNode, error) {
	node := &LogicalOpNode{
		Left: left,
	}

	switch p.current.Type {
	case TokenComma:
		node.Operator = OperatorAnd
	case TokenPipe:
		node.Operator = OperatorOr
	default:
		return nil, errors.Wrapf(ErrUnexpectedToken, "unexpected token %v for infix operator", p.current.Type)
	}

	if err := p.advance(); err != nil {
		return nil, err
	}
	var err error
	node.Right, err = p.parseExpression()
	if err != nil {
		return nil, err
	}
	if node.Right == nil { // Should be caught by parseExpression returning an error
		return nil, errors.Wrap(ErrExpectedExpression, "missing right-hand side of infix expression")
	}

	return node, nil
}

func (p *Parser) parseConditionToken() (ExpressionNode, error) {
	parts := strings.SplitN(p.current.Value, ":", 3)
	if len(parts) < 2 {
		return nil, errors.Wrapf(ErrInvalidConditionFormat, "condition '%s' must have at least field:operator", p.current.Value)
	}

	jsonFieldName := strings.TrimSpace(parts[0])
	if jsonFieldName == "" {
		return nil, errors.Wrapf(ErrInvalidConditionFormat, "field name cannot be empty in condition '%s'", p.current.Value)
	}

	fieldInfo, found := p.jsonToFieldInfo[jsonFieldName]
	if !found {
		return nil, errors.Wrapf(ErrInvalidFieldName, "'%s' in condition '%s'", jsonFieldName, p.current.Value)
	}

	condition := Condition{
		Field:    fieldInfo.Name,
		Operator: strings.ToLower(strings.TrimSpace(parts[1])),
	}

	switch condition.Operator {
	case "isnull", "isnotnull":
		if len(parts) > 2 && strings.TrimSpace(parts[2]) != "" {
			return nil, errors.Wrapf(ErrInvalidConditionFormat, "operator '%s' does not take a value, but got '%s'", condition.Operator, parts[2])
		}
		condition.IsNullOp = true
	case "in", "notin":
		if len(parts) < 3 {
			return nil, errors.Wrapf(ErrMissingValue, "operator '%s' requires a value part", condition.Operator)
		}
		valPart := strings.TrimSpace(parts[2])
		if !strings.HasPrefix(valPart, "(") || !strings.HasSuffix(valPart, ")") {
			return nil, errors.Wrapf(ErrInvalidValueFormat, "value for '%s' must be in parentheses, e.g., (v1,v2), got '%s'", condition.Operator, valPart)
		}
		valPart = valPart[1 : len(valPart)-1] // Remove parentheses
		if valPart == "" {                    // e.g. name:in:()
			return nil, errors.Wrapf(ErrInvalidValueFormat, "value list for '%s' cannot be empty", condition.Operator)
		}
		values := strings.Split(valPart, ",")
		condition.Values = make([]any, 0, len(values))
		for _, v := range values {
			trimmed := strings.TrimSpace(v)
			if trimmed == "" { // e.g. name:in:(v1,,v2)
				return nil, errors.Wrapf(ErrInvalidValueFormat, "empty value in list for operator '%s'", condition.Operator)
			}
			typedValue, err := p.convertValue(trimmed, fieldInfo.Kind)
			if err != nil {
				return nil, err
			}
			condition.Values = append(condition.Values, typedValue)
		}
	case "eq", "ne", "gt", "lt", "gte", "lte":
		if len(parts) < 3 {
			return nil, errors.Wrapf(ErrMissingValue, "operator '%s' requires a value", condition.Operator)
		}
		strValue := strings.TrimSpace(parts[2])
		typedValue, err := p.convertValue(strValue, fieldInfo.Kind)
		if err != nil {
			return nil, err
		}
		condition.Value = typedValue
	default:
		return nil, errors.Wrapf(ErrUnknownOperator, "'%s' in condition '%s'", condition.Operator, p.current.Value)
	}

	return &ConditionNode{Condition: condition}, nil
}

// convertValue converts a string value to the specified reflect.Kind.
func (p *Parser) convertValue(strValue string, kind reflect.Kind) (any, error) {
	switch kind {
	case reflect.String, reflect.Struct:
		return strValue, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		i, err := strconv.Atoi(strValue)
		if err != nil {
			return nil, errors.Wrapf(ErrInvalidValueFormat, "value '%s' is not a valid integer: %v", strValue, err)
		}
		return i, nil
	case reflect.Bool:
		b, err := strconv.ParseBool(strValue)
		if err != nil {
			return nil, errors.Wrapf(ErrInvalidValueFormat, "value '%s' is not a valid boolean: %v", strValue, err)
		}
		return b, nil
	case reflect.Float32, reflect.Float64:
		f, err := strconv.ParseFloat(strValue, 64)
		if err != nil {
			return nil, errors.Wrapf(ErrInvalidValueFormat, "value '%s' is not a valid float: %v", strValue, err)
		}
		if kind == reflect.Float32 {
			return float32(f), nil
		}
		return f, nil
	default:
		return nil, errors.Newf("unsupported kind for value conversion: %v for value '%s'", kind, strValue)
	}
}

func (p *Parser) parseGroupedExpression() (ExpressionNode, error) {
	if err := p.advance(); err != nil { // Consume '('
		return nil, err
	}

	if p.current.Type == TokenRParen {
		return nil, errors.Wrap(ErrExpectedExpression, "empty group '()' is not allowed")
	}

	expression, err := p.parseExpression()
	if err != nil {
		return nil, err
	}

	if err := p.expectPeek(TokenRParen); err != nil {
		return nil, err
	}

	return &GroupNode{Expression: expression}, nil
}
