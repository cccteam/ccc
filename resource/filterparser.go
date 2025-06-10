package resource

import (
	"fmt"
	"strings"

	"github.com/go-playground/errors/v5"
)

// Error constants for parsing.
var (
	ErrInvalidConditionFormat = errors.New("invalid condition format")
	ErrUnknownOperator        = errors.New("unknown operator")
	ErrMissingValue           = errors.New("missing value for operator")
	ErrUnexpectedToken        = errors.New("unexpected token")
	ErrExpectedExpression     = errors.New("expected an expression")
	ErrExpectedRightParen     = errors.New("expected ')'")
	ErrInvalidValueFormat     = errors.New("invalid value format for operator")
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
	Value    string   // For eq, ne, gt, lt, gte, lte
	Values   []string // For in, notin
	IsNullOp bool     // For isnull, isnotnull
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
		return fmt.Sprintf("%s:%s:(%s)", cn.Condition.Field, cn.Condition.Operator, strings.Join(cn.Condition.Values, ","))
	}

	return fmt.Sprintf("%s:%s:%s", cn.Condition.Field, cn.Condition.Operator, cn.Condition.Value)
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

// Operator precedence
const (
	_ int = iota
	LOWEST
	OR  // |
	AND // ,
)

var precedences = map[TokenType]int{
	TokenPipe:  OR,
	TokenComma: AND,
}

// Parser builds an AST from tokens.
type Parser struct {
	lexer   *Lexer
	current Token
	peek    Token
	errors  []error

	prefixParseFns map[TokenType]prefixParseFn
	infixParseFns  map[TokenType]infixParseFn
}

type (
	prefixParseFn func() (ExpressionNode, error)
	infixParseFn  func(ExpressionNode) (ExpressionNode, error)
)

func NewParser(lexer *Lexer) (*Parser, error) {
	p := &Parser{
		lexer:          lexer,
		prefixParseFns: make(map[TokenType]prefixParseFn),
		infixParseFns:  make(map[TokenType]infixParseFn),
	}

	// Register prefix parsing functions
	p.prefixParseFns[TokenCondition] = p.parseConditionToken
	p.prefixParseFns[TokenLParen] = p.parseGroupedExpression

	// Register infix parsing functions
	p.infixParseFns[TokenComma] = p.parseInfixExpression
	p.infixParseFns[TokenPipe] = p.parseInfixExpression

	// Prime the pump. Need to call twice to fill current and peek.
	// we don't return error immediately from advance here if it's EOF.
	// The parser might handle an initial EOF (e.g. empty filter string).
	_ = p.advance() // current will be TokenEOF if input is empty
	_ = p.advance() // peek will be TokenEOF if input is empty or has one token

	return p, nil
}

func (p *Parser) Errors() []error {
	return p.errors
}

func (p *Parser) addError(err error) {
	if err != nil {
		p.errors = append(p.errors, err)
	}
}

func (p *Parser) advance() error {
	p.current = p.peek
	var err error
	p.peek, err = p.lexer.NextToken()
	if err != nil {
		p.addError(errors.Wrap(err, "lexer error during advance"))

		return err
	}

	return nil
}

func (p *Parser) expectPeek(t TokenType) error {
	if p.peek.Type == t {
		return p.advance()
	}
	p.addError(errors.Wrapf(ErrUnexpectedToken, "expected peek token to be %v, got %v instead", t, p.peek.Type))

	return ErrUnexpectedToken // Return the specific error for control flow
}

func (p *Parser) currentPrecedence() int {
	if p, ok := precedences[p.current.Type]; ok {
		return p
	}

	return LOWEST
}

func (p *Parser) peekPrecedence() int {
	if p, ok := precedences[p.peek.Type]; ok {
		return p
	}

	return LOWEST
}

// Parse is the main entry point for parsing the filter string.
func (p *Parser) Parse() (ExpressionNode, error) {
	if p.current.Type == TokenEOF && p.peek.Type == TokenEOF {
		return nil, nil
	}

	expression, err := p.parseExpression(LOWEST)
	if err != nil {
		p.addError(err) // Ensure this error is added
	}

	if p.peek.Type != TokenEOF {
		p.addError(errors.Wrapf(ErrUnexpectedToken, "expected EOF after parsing, got %v", p.peek.Type))
	}

	if len(p.errors) > 0 {
		// Combine errors into a single error. This part might need a more sophisticated error reporting.
		var errMsgs []string
		for _, e := range p.errors {
			errMsgs = append(errMsgs, e.Error())
		}

		return nil, errors.New(strings.Join(errMsgs, "; "))
	}

	return expression, nil
}

func (p *Parser) parseExpression(precedence int) (ExpressionNode, error) {
	prefix := p.prefixParseFns[p.current.Type]
	if prefix == nil {
		return nil, errors.Wrapf(ErrExpectedExpression, "no prefix parse function for token type %v (value: '%s')", p.current.Type, p.current.Value)
	}

	leftExp, err := prefix()
	if err != nil {
		return nil, err
	}

	for p.peek.Type != TokenEOF && precedence < p.peekPrecedence() {
		infix := p.infixParseFns[p.peek.Type]
		if infix == nil {
			// This means we have a token that should be an infix operator but isn't registered,
			// or it's a token that shouldn't appear in an infix position.
			// For example, two conditions back-to-back without an operator.
			p.addError(errors.Wrapf(ErrUnexpectedToken, "expected operator, got %v (value: '%s')", p.peek.Type, p.peek.Value))

			return leftExp, nil // Return the left expression parsed so far
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
		err := errors.Wrapf(ErrUnexpectedToken, "unexpected token %v for infix operator", p.current.Type)
		p.addError(err)

		return nil, err
	}

	precedence := p.currentPrecedence()
	if err := p.advance(); err != nil { // Consume the operator token itself
		return nil, err
	}
	var err error
	node.Right, err = p.parseExpression(precedence)
	if err != nil {
		// Error already added by parseExpression or one of its children
		return nil, err
	}
	if node.Right == nil { // Should be caught by parseExpression returning an error
		err = errors.Wrap(ErrExpectedExpression, "missing right-hand side of infix expression")
		p.addError(err)

		return nil, err
	}

	return node, nil
}

func (p *Parser) parseConditionToken() (ExpressionNode, error) {
	parts := strings.SplitN(p.current.Value, ":", 3)
	if len(parts) < 2 {
		err := errors.Wrapf(ErrInvalidConditionFormat, "condition '%s' must have at least field:operator", p.current.Value)
		p.addError(err)

		return nil, err
	}

	condition := Condition{
		Field:    strings.TrimSpace(parts[0]),
		Operator: strings.ToLower(strings.TrimSpace(parts[1])),
	}

	if condition.Field == "" {
		err := errors.Wrapf(ErrInvalidConditionFormat, "field name cannot be empty in condition '%s'", p.current.Value)
		p.addError(err)

		return nil, err
	}

	switch condition.Operator {
	case "isnull", "isnotnull":
		if len(parts) > 2 && strings.TrimSpace(parts[2]) != "" {
			err := errors.Wrapf(ErrInvalidConditionFormat, "operator '%s' does not take a value, but got '%s'", condition.Operator, parts[2])
			p.addError(err)

			return nil, err
		}
		condition.IsNullOp = true
	case "in", "notin":
		if len(parts) < 3 {
			err := errors.Wrapf(ErrMissingValue, "operator '%s' requires a value part", condition.Operator)
			p.addError(err)

			return nil, err
		}
		valPart := strings.TrimSpace(parts[2])
		if !strings.HasPrefix(valPart, "(") || !strings.HasSuffix(valPart, ")") {
			err := errors.Wrapf(ErrInvalidValueFormat, "value for '%s' must be in parentheses, e.g., (v1,v2), got '%s'", condition.Operator, valPart)
			p.addError(err)

			return nil, err
		}
		valPart = valPart[1 : len(valPart)-1] // Remove parentheses
		if valPart == "" {                    // e.g. name:in:()
			err := errors.Wrapf(ErrInvalidValueFormat, "value list for '%s' cannot be empty", condition.Operator)
			p.addError(err)

			return nil, err
		}
		values := strings.Split(valPart, ",")
		condition.Values = make([]string, 0, len(values))
		for _, v := range values {
			trimmed := strings.TrimSpace(v)
			if trimmed == "" { // e.g. name:in:(v1,,v2)
				err := errors.Wrapf(ErrInvalidValueFormat, "empty value in list for operator '%s'", condition.Operator)
				p.addError(err)

				return nil, err
			}
			condition.Values = append(condition.Values, trimmed)
		}
		if len(condition.Values) == 0 { // Should be caught by valPart == "" earlier, but good for safety
			err := errors.Wrapf(ErrInvalidValueFormat, "value list for '%s' resolved to empty", condition.Operator)
			p.addError(err)

			return nil, err
		}
	case "eq", "ne", "gt", "lt", "gte", "lte", "contains", "startswith", "endswith", "like", "ilike":
		if len(parts) < 3 {
			err := errors.Wrapf(ErrMissingValue, "operator '%s' requires a value", condition.Operator)
			p.addError(err)

			return nil, err
		}
		condition.Value = strings.TrimSpace(parts[2])
		// It's debatable if an empty value is allowed, e.g. name:eq:
		// For now, allowing it. Add validation if empty values are disallowed.
	default:
		err := errors.Wrapf(ErrUnknownOperator, "'%s' in condition '%s'", condition.Operator, p.current.Value)
		p.addError(err)

		return nil, err
	}

	return &ConditionNode{Condition: condition}, nil
}

func (p *Parser) parseGroupedExpression() (ExpressionNode, error) {
	if err := p.advance(); err != nil { // Consume '('
		return nil, err
	}

	// Check for empty group ()
	if p.current.Type == TokenRParen {
		err := errors.Wrap(ErrExpectedExpression, "empty group '()' is not allowed")
		p.addError(err)

		return nil, err
	}

	expression, err := p.parseExpression(LOWEST)
	if err != nil {
		// Error already added by parseExpression or its children
		return nil, err
	}
	if expression == nil { // Should be caught by parseExpression returning an error
		err = errors.Wrap(ErrExpectedExpression, "no expression inside parentheses")
		p.addError(err)

		return nil, err
	}

	if err := p.expectPeek(TokenRParen); err != nil { // Checks p.peek and advances if it's RParen
		// Error (ErrExpectedRightParen or ErrUnexpectedToken) already added by expectPeek
		return nil, err
	}

	return &GroupNode{Expression: expression}, nil
}
