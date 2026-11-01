package resource

import (
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"cloud.google.com/go/civil"
	"github.com/cccteam/ccc"
	"github.com/cccteam/httpio"
	"github.com/go-playground/errors/v5"
	"github.com/shopspring/decimal"
)

//go:generate go run golang.org/x/tools/cmd/stringer -type=TokenType

// TokenType defines the types of tokens in the filter string.
type TokenType int

const (
	// TokenEOF represents the end of the input string.
	TokenEOF TokenType = iota
	// TokenLParen represents '('
	TokenLParen
	// TokenRParen represents ')'
	TokenRParen
	// TokenComma represents ','
	TokenComma
	// TokenPipe represents '|'
	TokenPipe
	// TokenCondition represents 'field:operator:value' or 'field:operator:(value1,value2,...)' or 'field:operator'
	TokenCondition
)

// Token represents a single token.
type Token struct {
	Type  TokenType
	Value string
}

// FilterLexer parses the filter producing tokens
type FilterLexer struct {
	input string
	pos   int
}

// NewFilterLexer creates a new lexer for the given input filter string.
func NewFilterLexer(input string) *FilterLexer {
	return &FilterLexer{
		input: input,
		pos:   0,
	}
}

// Reset resets the lexer to its beginning state so it can be used again
func (l *FilterLexer) Reset() {
	l.pos = 0
}

// NextToken returns the next token from the input string.
func (l *FilterLexer) NextToken() (Token, error) {
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
		case '"':
			// A quoted value runs to its closing quote, a backslash escaping a quote or
			// a backslash inside it, so a comma, a pipe, a parenthesis or a colon in it
			// never ends the condition.
			end, err := l.closingQuote(i)
			if err != nil {
				return Token{}, err
			}
			l.pos = end + 1
			i = end
		case '(':
			parenCount++
			if parenCount > 1 {
				return Token{}, httpio.NewBadRequestMessagef("Invalid filter query. Nested parentheses are not allowed within a single condition segment. Found near character %d of %s.", l.pos, l.input)
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

// closingQuote finds the quote closing the one at open, reading a backslash as the
// escape of the character after it.
func (l *FilterLexer) closingQuote(open int) (int, error) {
	for i := open + 1; i < len(l.input); i++ {
		switch l.input[i] {
		case '\\':
			i++
		case '"':
			return i, nil
		}
	}

	return 0, httpio.NewBadRequestMessagef("Invalid filter query. The quote opened at character %d of %s is never closed.", open+1, l.input)
}

// unquoteFilterValue reads a condition's value: a value in double quotes may contain
// commas, pipes, parentheses, colons and spaces, a backslash escaping a quote or a
// backslash; an unquoted value stays as it is, trimmed, so no existing request changes.
func unquoteFilterValue(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if !strings.HasPrefix(value, `"`) {
		return value, nil
	}
	if len(value) < 2 || !strings.HasSuffix(value, `"`) {
		return "", httpio.NewBadRequestMessagef("Invalid filter query. The quoted value %s is never closed.", raw)
	}
	var b strings.Builder
	inner := value[1 : len(value)-1]
	for i := 0; i < len(inner); i++ {
		c := inner[i]
		switch c {
		case '\\':
			if i+1 >= len(inner) || (inner[i+1] != '"' && inner[i+1] != '\\') {
				return "", httpio.NewBadRequestMessagef("Invalid filter query. A backslash in %s escapes a quote or a backslash only.", raw)
			}
			i++
			c = inner[i]
		case '"':
			return "", httpio.NewBadRequestMessagef("Invalid filter query. The quoted value %s closes early; escape an inner quote with a backslash.", raw)
		}
		b.WriteByte(c)
	}

	return b.String(), nil
}

// splitQuotedList splits an in or notin list at the commas outside quotes.
func splitQuotedList(list string) []string {
	var values []string
	start := 0
	quoted := false
	for i := 0; i < len(list); i++ {
		switch list[i] {
		case '\\':
			if quoted {
				i++
			}
		case '"':
			quoted = !quoted
		case ',':
			if !quoted {
				values = append(values, list[start:i])
				start = i + 1
			}
		}
	}

	return append(values, list[start:])
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
	Value    any   // For eq, ne, gt, lt, gte, lte, startswith, endswith, contains, and the box's text
	Values   []any // For in, notin
	IsNullOp bool  // For isnull, isnotnull

	// match carries the field's search spec behind a startswith, endswith or contains
	// condition: the columns the match renders through. Nil on every other operator.
	match *searchField
	// search is the box behind a *:search condition: its terms, the fields it searches
	// and the child branches. Nil on every other condition.
	search *searchCondition
}

// IsSearch reports whether the condition is the search box, *:search:"…".
func (c *Condition) IsSearch() bool {
	return c.search != nil
}

// TypedValues returns an in or notin list as a slice of the values' own type, the
// shape an array query parameter binds: Spanner types IN UNNEST(@p) from the slice's
// element type and refuses []any. The parser types every value by its column, so the
// slice is []string for a STRING column, []int64 for INT64, and []*big.Rat for
// NUMERIC, following paramValue. A body that pushes a taken filter down to its own
// query binds the result as it is. An empty list, or one the values do not type
// uniformly, is returned unchanged.
func (c *Condition) TypedValues() any {
	if len(c.Values) == 0 {
		return c.Values
	}

	elem := reflect.TypeOf(paramValue(c.Values[0]))
	if elem == nil {
		return c.Values
	}
	typed := reflect.MakeSlice(reflect.SliceOf(elem), 0, len(c.Values))
	for _, v := range c.Values {
		value := paramValue(v)
		if reflect.TypeOf(value) != elem {
			return c.Values
		}
		typed = reflect.Append(typed, reflect.ValueOf(value))
	}

	return typed.Interface()
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
	if cn.Condition.search != nil {
		return fmt.Sprintf("%s:%s:%q", cn.Condition.Field, cn.Condition.Operator, cn.Condition.search.text)
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
	// OperatorAnd represents a logical AND.
	OperatorAnd LogicalOperator = "AND"
	// OperatorOr represents a logical OR.
	OperatorOr LogicalOperator = "OR"
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
	prefixParseFn func(DBType) (ExpressionNode, error)
	infixParseFn  func(ExpressionNode, DBType) (ExpressionNode, error)
)

// FilterFieldInfo holds metadata about a field that can be used in a filter.
type FilterFieldInfo struct {
	JSONFieldName string
	GOFieldName   string
	dbColumnNames map[DBType]string
	Kind          reflect.Kind
	FieldType     reflect.Type
	Indexed       bool
	PII           bool
	// Positional marks a field declared masking:"positional": a filter on it
	// compares the raw column even where the caller's grant is conditional.
	Positional bool
	// Search is the field's search tag, read back from the request struct: the matches
	// it answers and the columns they render through, and whether it joins the box. Nil
	// on a field with neither fact.
	Search *SearchSpec
	// order is the field's position in the request struct: the box lists its fields in
	// declaration order.
	order int
}

// ColumnName returns the column name for the given DBType.
func (f *FilterFieldInfo) ColumnName(dbType DBType) (string, error) {
	name, ok := f.dbColumnNames[dbType]
	if !ok {
		return name, errors.Newf("field %s (json: %s) not found in resource metadata for db type %s", f.GOFieldName, f.JSONFieldName, dbType)
	}

	return name, nil
}

// goFieldNames is the parse target that names conditions by Go field instead of
// by database column: the tree a computed resource's handler evaluates against
// rows in memory, and the one a body takes conditions from.
const goFieldNames DBType = "gofields"

// name returns the identifier a condition on this field carries for the parse target.
func (f *FilterFieldInfo) name(target DBType) (string, error) {
	if target == goFieldNames {
		return f.GOFieldName, nil
	}

	return f.ColumnName(target)
}

// FilterParser builds an AST from tokens.
type FilterParser struct {
	lexer           *FilterLexer
	current         Token
	peek            Token
	hasIndexedField bool

	prefixParseFns  map[TokenType]prefixParseFn
	infixParseFns   map[TokenType]infixParseFn
	jsonToFieldInfo map[jsonFieldName]FilterFieldInfo

	// searchBranches are the resource's @searchAlso branches and searchKeys its key
	// column per parse target, which the box carries (WithSearch); boxSeen refuses a
	// second box in one filter.
	searchBranches []SearchBranch
	searchKeys     map[DBType]string
	boxSeen        bool

	parsedExpression map[DBType]ExpressionNode
}

// WithSearch hands the parser the resource's @searchAlso branches and its key column
// per parse target (the Go field under the in-memory target), which a *:search condition
// carries into the statement. The decoders pass them from the resource's metadata; a
// parser without them serves a resource that declares no branch.
func (p *FilterParser) WithSearch(branches []SearchBranch, keys map[DBType]string) *FilterParser {
	p.searchBranches = branches
	p.searchKeys = keys

	return p
}

// NewFilterParser creates a new parser with the given lexer and field information map.
func NewFilterParser(lexer *FilterLexer, jsonToFieldInfo map[jsonFieldName]FilterFieldInfo) (*FilterParser, error) {
	p := &FilterParser{
		lexer:            lexer,
		prefixParseFns:   make(map[TokenType]prefixParseFn),
		infixParseFns:    make(map[TokenType]infixParseFn),
		jsonToFieldInfo:  jsonToFieldInfo,
		parsedExpression: make(map[DBType]ExpressionNode),
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

// reset resets the parser's internal state to allow for parsing for a new DBType,
// while preserving the cache of already parsed expressions.
func (p *FilterParser) reset() error {
	p.lexer.Reset()
	p.hasIndexedField = false
	p.boxSeen = false

	// Reprime the parser by advancing the tokens back to the start of the input.
	if err := p.advance(); err != nil {
		return errors.Wrap(err, "failed to advance for current token")
	}
	if err := p.advance(); err != nil {
		return errors.Wrap(err, "failed to advance for peek token")
	}

	return nil
}

// ParseFields parses the filter with every condition named by Go field, for
// evaluation against rows in memory rather than rendering into SQL.
func (p *FilterParser) ParseFields() (ExpressionNode, error) {
	return p.Parse(goFieldNames)
}

// Parse is the main entry point for parsing the filter string.
func (p *FilterParser) Parse(dbType DBType) (ExpressionNode, error) {
	if exp, found := p.parsedExpression[dbType]; found {
		return exp, nil
	}

	if p.current.Type == TokenEOF && p.peek.Type == TokenEOF {
		return nil, nil
	}

	expression, err := p.parseExpression(dbType)
	if err != nil {
		return nil, err
	}

	if p.peek.Type != TokenEOF {
		return nil, httpio.NewBadRequestMessagef("Invalid filter query. Unexpected characters '%s' (type: %s) found after the end of the query.", p.peek.Value, p.peek.Type)
	}

	// A table filter must touch an indexed column so the database has a path
	// into it; a computed resource's rows are already in memory, so any
	// filterable field serves.
	if !p.hasIndexedField && dbType != goFieldNames {
		return nil, httpio.NewBadRequestMessagef("Invalid filter query. Filter must contain at least one column that is indexed for dbType %s", dbType)
	}

	p.parsedExpression[dbType] = expression
	if err := p.reset(); err != nil {
		return nil, err
	}

	return expression, nil
}

func (p *FilterParser) advance() error {
	p.current = p.peek
	var err error
	p.peek, err = p.lexer.NextToken()
	if err != nil {
		return errors.Wrap(err, "lexer error during advance")
	}

	return nil
}

func (p *FilterParser) expectPeek(t TokenType) error {
	if p.peek.Type == t {
		return p.advance()
	}

	return httpio.NewBadRequestMessagef("expected next token to be %s, got %s instead", t, p.peek.Type)
}

func (p *FilterParser) parseExpression(dbType DBType) (ExpressionNode, error) {
	prefix := p.prefixParseFns[p.current.Type]
	if prefix == nil {
		return nil, httpio.NewBadRequestMessagef(
			"Invalid filter query. Unexpected token '%s' (type: %s) at the beginning of an expression or after an operator. Please ensure your query is correctly formatted (e.g., 'field:operator:value').",
			p.current.Value,
			p.current.Type,
		)
	}

	leftExp, err := prefix(dbType)
	if err != nil {
		return nil, err
	}

	for p.peek.Type == TokenComma || p.peek.Type == TokenPipe {
		infix := p.infixParseFns[p.peek.Type]
		if infix == nil {
			// This means we have a token that should be an infix operator but isn't registered,
			// or it's a token that shouldn't appear in an infix position.
			// For example, two conditions back-to-back without an operator.
			return nil, httpio.NewBadRequestMessagef("expected operator, got '%s'", p.peek.Value)
		}
		if err := p.advance(); err != nil { // Consume the operator
			return nil, err
		}
		leftExp, err = infix(leftExp, dbType)
		if err != nil {
			return nil, err // Error already added by infix function
		}
	}

	return leftExp, nil
}

func (p *FilterParser) parseInfixExpression(left ExpressionNode, dbType DBType) (ExpressionNode, error) {
	node := &LogicalOpNode{
		Left: left,
	}

	switch p.current.Type {
	case TokenComma:
		node.Operator = OperatorAnd
	case TokenPipe:
		node.Operator = OperatorOr
	default:
		return nil, httpio.NewBadRequestMessagef("Invalid filter query. Expected a logical operator (',' for AND, '|' for OR) but found '%s' (type: %s).", p.current.Value, p.current.Type)
	}

	if err := p.advance(); err != nil {
		return nil, err
	}
	var err error
	node.Right, err = p.parseExpression(dbType)
	if err != nil {
		return nil, err
	}
	if node.Right == nil { // Should be caught by parseExpression returning an error
		return nil, errors.New("missing right-hand side of infix expression")
	}

	return node, nil
}

func (p *FilterParser) parseConditionToken(dbType DBType) (ExpressionNode, error) {
	parts := strings.SplitN(p.current.Value, ":", 3)
	if len(parts) < 2 {
		return nil, httpio.NewBadRequestMessagef("condition '%s' must have at least field:operator", p.current.Value)
	}

	jsonFieldNameStr := strings.TrimSpace(parts[0])
	if jsonFieldNameStr == "" {
		return nil, httpio.NewBadRequestMessagef("field name cannot be empty in condition '%s'", p.current.Value)
	}
	operator := strings.ToLower(strings.TrimSpace(parts[1]))

	if jsonFieldNameStr == searchBoxField {
		return p.parseSearchCondition(dbType, operator, parts)
	}
	if operator == searchStr {
		return nil, httpio.NewBadRequestMessagef("operator '%s' is the search box's and names no field; write %s:%s:\"…\" instead of condition '%s'", searchStr, searchBoxField, searchStr, p.current.Value)
	}

	fieldInfo, found := p.jsonToFieldInfo[jsonFieldName(jsonFieldNameStr)]
	if !found {
		// Filterable means indexed or allow_filter on a table and allow_filter on a
		// computed resource; one word covers both, since the same parse refuses both.
		return nil, httpio.NewBadRequestMessagef("'%s' is not filterable but was included in condition '%s'", jsonFieldNameStr, p.current.Value)
	}
	if fieldInfo.Indexed {
		p.hasIndexedField = true
	}

	field, err := fieldInfo.name(dbType)
	if err != nil {
		return nil, err
	}

	condition := Condition{
		Field:    field,
		Operator: operator,
	}

	switch condition.Operator {
	case isnullStr, isnotnullStr:
		if len(parts) > 2 && strings.TrimSpace(parts[2]) != "" {
			return nil, httpio.NewBadRequestMessagef("operator '%s' does not take a value, but got '%s' in condition '%s'", condition.Operator, parts[2], p.current.Value)
		}
		condition.IsNullOp = true
	case inStr, notinStr:
		values, err := p.parseListValues(&fieldInfo, field, condition.Operator, parts)
		if err != nil {
			return nil, err
		}
		condition.Values = values
	case eqStr, neStr, gtStr, ltStr, gteStr, lteStr:
		value, err := p.parseComparisonValue(&fieldInfo, condition.Operator, parts)
		if err != nil {
			return nil, err
		}
		condition.Value = value
	case startswithStr, endswithStr, containsStr:
		match, err := p.parseMatchCondition(dbType, &fieldInfo, jsonFieldNameStr, condition.Operator, parts)
		if err != nil {
			return nil, err
		}
		condition.Value = match.term
		condition.match = match.field
		// The search index, or the index the lowercased column leads, serves the
		// match: it is the indexed touch a table filter needs.
		if fieldInfo.Search.Token != "" || fieldInfo.Search.Lower != "" {
			p.hasIndexedField = true
		}
	default:
		return nil, httpio.NewBadRequestMessagef("unknown operator '%s' in condition '%s'", condition.Operator, p.current.Value)
	}

	return &ConditionNode{Condition: condition}, nil
}

// parseListValues reads an in or notin condition's parenthesized list: every value
// unquoted, non-empty, and typed by the field, or by its element type on a slice field.
func (p *FilterParser) parseListValues(fieldInfo *FilterFieldInfo, field, operator string, parts []string) ([]any, error) {
	if len(parts) < 3 {
		return nil, httpio.NewBadRequestMessagef("operator '%s' requires a value part in condition '%s'	", operator, p.current.Value)
	}
	valPart := strings.TrimSpace(parts[2])
	if !strings.HasPrefix(valPart, "(") || !strings.HasSuffix(valPart, ")") {
		return nil, httpio.NewBadRequestMessagef("value for '%s' must be in parentheses, e.g., (v1,v2), got '%s' in condition '%s'", operator, valPart, p.current.Value)
	}
	valPart = valPart[1 : len(valPart)-1] // Remove parentheses
	if valPart == "" {                    // e.g. name:in:()
		return nil, httpio.NewBadRequestMessagef("value list for '%s' cannot be empty in condition '%s'", operator, p.current.Value)
	}
	valueType, valueKind := fieldInfo.FieldType, fieldInfo.Kind
	if valueKind == reflect.Slice || valueKind == reflect.Array {
		if fieldInfo.FieldType == nil {
			return nil, errors.Newf("FieldType not available in FieldInfo for slice/array field '%s' to determine element kind", field)
		}
		valueType = fieldInfo.FieldType.Elem()
		valueKind = valueType.Kind()
	}
	values := splitQuotedList(valPart)
	typed := make([]any, 0, len(values))
	for _, v := range values {
		trimmed, err := unquoteFilterValue(v)
		if err != nil {
			return nil, err
		}
		if trimmed == "" { // e.g. name:in:(v1,,v2)
			return nil, httpio.NewBadRequestMessagef("empty value in list for operator '%s' in condition '%s'", operator, p.current.Value)
		}
		typedValue, err := p.convertValue(trimmed, valueType, valueKind)
		if err != nil {
			return nil, err
		}
		typed = append(typed, typedValue)
	}

	return typed, nil
}

// parseComparisonValue reads the value of an eq, ne, gt, lt, gte or lte condition,
// unquoted and typed by the field.
func (p *FilterParser) parseComparisonValue(fieldInfo *FilterFieldInfo, operator string, parts []string) (any, error) {
	if len(parts) < 3 {
		return nil, httpio.NewBadRequestMessagef("operator '%s' requires a value in condition '%s'", operator, p.current.Value)
	}
	strValue, err := unquoteFilterValue(parts[2])
	if err != nil {
		return nil, err
	}

	return p.convertValue(strValue, fieldInfo.FieldType, fieldInfo.Kind)
}

// parsedMatch is an explicit match as the parser reads it: the term and the field's
// search vocabulary the renderer binds it through.
type parsedMatch struct {
	term  string
	field *searchField
}

// parseMatchCondition reads a startswith, endswith or contains condition: a string
// field, a non-empty value, and a field whose search tag answers the match, refused with
// the same 400 shape as an unknown operator where it does not, since the browser's
// metadata lists the same matches and never draws the control.
func (p *FilterParser) parseMatchCondition(dbType DBType, fieldInfo *FilterFieldInfo, jsonName, operator string, parts []string) (*parsedMatch, error) {
	if fieldInfo.Kind != reflect.String {
		return nil, httpio.NewBadRequestMessagef("operator '%s' matches text and '%s' is not a text field, in condition '%s'", operator, jsonName, p.current.Value)
	}
	if !fieldInfo.Search.Answers(operator) {
		return nil, httpio.NewBadRequestMessagef("unknown operator '%s' for '%s' in condition '%s': the field answers %s", operator, jsonName, p.current.Value, answeredMatches(fieldInfo.Search))
	}
	if len(parts) < 3 {
		return nil, httpio.NewBadRequestMessagef("operator '%s' requires a value in condition '%s'", operator, p.current.Value)
	}
	term, err := unquoteFilterValue(parts[2])
	if err != nil {
		return nil, err
	}
	if term == "" {
		return nil, httpio.NewBadRequestMessagef("operator '%s' requires a non-empty value in condition '%s'", operator, p.current.Value)
	}

	return &parsedMatch{term: term, field: p.searchFieldFor(dbType, fieldInfo, jsonName)}, nil
}

// answeredMatches spells the matches a field answers for a refusal.
func answeredMatches(spec *SearchSpec) string {
	matches := spec.Matches()
	if len(matches) == 0 {
		return "no text match: eq, ne, in, notin, isnull and isnotnull"
	}

	return strings.Join(matches, ", ")
}

// searchFieldFor renders a filterable field as the box or a match carries it under the
// parse target.
func (p *FilterParser) searchFieldFor(dbType DBType, fieldInfo *FilterFieldInfo, jsonName string) *searchField {
	name, _ := fieldInfo.name(dbType)
	column := fieldInfo.GOFieldName
	if dbType != goFieldNames {
		column = name
	}

	return &searchField{
		name:       name,
		column:     column,
		goField:    fieldInfo.GOFieldName,
		jsonName:   jsonName,
		spec:       fieldInfo.Search,
		pii:        fieldInfo.PII,
		positional: fieldInfo.Positional,
	}
}

// parseSearchCondition reads the box, *:search:"nguyen 1987 4321": the text splits into
// terms at whitespace under the limits, and the condition carries every field of the
// resource that joins the box, in declaration order, with the resource's child branches.
// One box per filter; a box counts as the indexed touch a table filter needs, since the
// search index serves it.
func (p *FilterParser) parseSearchCondition(dbType DBType, operator string, parts []string) (ExpressionNode, error) {
	if operator != searchStr {
		return nil, httpio.NewBadRequestMessagef("'%s' takes the operator '%s' alone, in condition '%s'", searchBoxField, searchStr, p.current.Value)
	}
	if p.boxSeen {
		return nil, httpio.NewBadRequestMessagef("one search box per filter; a second %s:%s condition was found in '%s'", searchBoxField, searchStr, p.current.Value)
	}
	if len(parts) < 3 {
		return nil, httpio.NewBadRequestMessagef("operator '%s' requires a value in condition '%s'", searchStr, p.current.Value)
	}
	text, err := unquoteFilterValue(parts[2])
	if err != nil {
		return nil, err
	}
	terms, err := splitSearchTerms(text)
	if err != nil {
		return nil, httpio.NewBadRequestMessagef("Invalid filter query. %s, in condition '%s'.", err.Error(), p.current.Value)
	}

	fields := p.boxFields(dbType)
	if len(fields) == 0 && len(p.searchBranches) == 0 {
		return nil, httpio.NewBadRequestMessagef("no field of this resource is searchable, so the search box has nothing to search; a field joins the box through a token column in the table's search index")
	}
	p.boxSeen = true
	p.hasIndexedField = true

	condition := Condition{
		Field:    searchBoxField,
		Operator: searchStr,
		Value:    text,
		search: &searchCondition{
			text:     text,
			terms:    terms,
			fields:   fields,
			branches: p.searchBranches,
			key:      p.searchKeys[dbType],
		},
	}

	return &ConditionNode{Condition: condition}, nil
}

// boxFields lists the resource's fields that join the box, in declaration order.
func (p *FilterParser) boxFields(dbType DBType) []searchField {
	infos := make([]*FilterFieldInfo, 0, len(p.jsonToFieldInfo))
	seen := make(map[string]struct{}, len(p.jsonToFieldInfo))
	for _, info := range p.jsonToFieldInfo {
		if !info.Search.JoinsBox() {
			continue
		}
		// A renamed field is listed under both names; the box names it once, by its
		// current name.
		if _, dup := seen[info.GOFieldName]; dup {
			continue
		}
		seen[info.GOFieldName] = struct{}{}
		infos = append(infos, &info)
	}
	slices.SortFunc(infos, func(a, b *FilterFieldInfo) int { return a.order - b.order })

	fields := make([]searchField, 0, len(infos))
	for _, info := range infos {
		fields = append(fields, *p.searchFieldFor(dbType, info, info.JSONFieldName))
	}

	return fields
}

// convertValue types a filter value for the field it compares against: by the field's
// Go type when the field is a struct (a decimal, a time, a date, a UUID, or a nullable
// wrapper), else by its kind.
func (p *FilterParser) convertValue(strValue string, fieldType reflect.Type, kind reflect.Kind) (any, error) {
	if kind == reflect.Struct && fieldType != nil {
		return p.convertStructValue(strValue, fieldType)
	}

	switch kind {
	case reflect.String, reflect.Struct:
		return strValue, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		i, err := strconv.Atoi(strValue)
		if err != nil {
			return nil, httpio.NewBadRequestMessagef("value '%s' in condition '%s' is not a valid integer: %v", strValue, p.current.Value, err)
		}

		return i, nil
	case reflect.Bool:
		b, err := strconv.ParseBool(strValue)
		if err != nil {
			return nil, httpio.NewBadRequestMessagef("value '%s' in condition '%s' is not a valid boolean: %v", strValue, p.current.Value, err)
		}

		return b, nil
	case reflect.Float32, reflect.Float64:
		f, err := strconv.ParseFloat(strValue, 64)
		if err != nil {
			return nil, httpio.NewBadRequestMessagef("value '%s' in condition '%s' is not a valid float: %v", strValue, p.current.Value, err)
		}
		if kind == reflect.Float32 {
			return float32(f), nil
		}

		return f, nil
	default:
		return nil, httpio.NewBadRequestMessagef("Invalid value format. The value '%s' in condition '%s' cannot be processed due to an unsupported data type: %v.", strValue, p.current.Value, kind)
	}
}

// convertStructValue types a filter value for a struct-typed column by the column's Go
// type, the way a cursor's boundary values are read: a decimal, a time, a date, or a
// UUID compares as itself, and a nullable wrapper as its base type. Spanner types a
// query parameter from its Go value, so a value left as text would meet a NUMERIC or
// TIMESTAMP column as a STRING and the comparison would fail in the database.
func (p *FilterParser) convertStructValue(strValue string, fieldType reflect.Type) (any, error) {
	value, err := cursorValue(strValue, fieldType)
	switch {
	case err == nil:
		return value, nil
	case errors.Is(err, errInvalidCursor):
		return nil, httpio.NewBadRequestMessagef("value '%s' in condition '%s' is not a valid %s", strValue, p.current.Value, filterValueWord(nullableBaseType(fieldType)))
	default:
		return nil, httpio.NewBadRequestMessagef("Invalid value format. The value '%s' in condition '%s' cannot be processed due to an unsupported data type: %s.", strValue, p.current.Value, fieldType)
	}
}

// filterValueWord names what a filter value for a column of type t must look like.
func filterValueWord(t reflect.Type) string {
	switch t {
	case reflect.TypeFor[time.Time]():
		return "RFC 3339 timestamp"
	case reflect.TypeFor[civil.Date]():
		return "date (YYYY-MM-DD)"
	case reflect.TypeFor[decimal.Decimal]():
		return "decimal number"
	case reflect.TypeFor[ccc.UUID]():
		return "UUID"
	default:
		return t.String()
	}
}

func (p *FilterParser) parseGroupedExpression(dbType DBType) (ExpressionNode, error) {
	if err := p.advance(); err != nil { // Consume '('
		return nil, err
	}

	if p.current.Type == TokenRParen {
		return nil, httpio.NewBadRequestMessagef("Invalid filter query. Empty groups '()' are not allowed.")
	}

	expression, err := p.parseExpression(dbType)
	if err != nil {
		return nil, err
	}

	if err := p.expectPeek(TokenRParen); err != nil {
		return nil, err
	}

	return &GroupNode{Expression: expression}, nil
}
