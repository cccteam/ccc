package resource

import (
	"fmt"
	"strings"

	"github.com/go-playground/errors/v5"
)

// SQLDialect defines the type of SQL dialect to generate.
type SQLDialect int

const (
	PostgreSQL SQLDialect = iota
	Spanner
)

// ErrUnsupportedNodeType indicates an AST node type that the SQL generator cannot handle.
var ErrUnsupportedNodeType = errors.New("unsupported AST node type for SQL generation")

// ErrUnsupportedOperator indicates a condition operator that the SQL generator cannot handle.
var ErrUnsupportedOperator = errors.New("unsupported operator for SQL generation")

// SQLGenerator translates an ExpressionNode AST into a SQL query string and parameters.
type SQLGenerator struct {
	dialect    SQLDialect
	paramCount int
}

// NewSQLGenerator creates a new SQL generator for the specified dialect.
func NewSQLGenerator(dialect SQLDialect) *SQLGenerator {

	return &SQLGenerator{
		dialect: dialect,
	}
}

// GenerateSQL converts an AST node into a SQL string and a list of parameters.
// It resets the parameter counter for each top-level call.
func (s *SQLGenerator) GenerateSQL(node ExpressionNode) (string, []interface{}, error) {
	s.paramCount = 0 // Reset for each new generation pass

	if node == nil { // Handle empty filter string resulting in nil node


		return "1=1", []interface{}{}, nil
	}


	return s.generateSQLRecursive(node)
}

// generateSQLRecursive is the internal recursive part of SQL generation.
func (s *SQLGenerator) generateSQLRecursive(node ExpressionNode) (string, []interface{}, error) {
	if node == nil {
		// This case should ideally be handled by the caller or specific node generators
		// if an optional part of an expression is nil.
		// For a nil root, GenerateSQL handles it. If nil occurs deeper, it's an issue.


		return "", nil, errors.New("unexpected nil node during recursive generation")
	}

	switch n := node.(type) {
	case *ConditionNode:


		return s.generateConditionSQL(n)
	case *LogicalOpNode:


		return s.generateLogicalOpSQL(n)
	case *GroupNode:


		return s.generateGroupSQL(n)
	default:


		return "", nil, errors.Wrapf(ErrUnsupportedNodeType, "type: %T", n)
	}
}

func (s *SQLGenerator) quoteIdentifier(identifier string) string {
	// Basic quoting, can be expanded if needed (e.g. to handle already quoted identifiers or special chars)
	// Spanner identifiers are case-sensitive and should be quoted with backticks if they contain non-alphanumeric chars or match keywords.
	// PostgreSQL identifiers are folded to lower case unless quoted with double quotes.
	if s.dialect == Spanner {


		return "`" + strings.ReplaceAll(identifier, "`", "``") + "`"
	}
	// For PostgreSQL, double quotes preserve case and allow special characters.


	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}

func (s *SQLGenerator) nextPlaceholder() string {
	s.paramCount++
	if s.dialect == Spanner {


		return fmt.Sprintf("@p%d", s.paramCount)
	}


	return fmt.Sprintf("$%d", s.paramCount)
}

func (s *SQLGenerator) generateConditionSQL(cn *ConditionNode) (string, []interface{}, error) {
	field := s.quoteIdentifier(cn.Condition.Field)
	op := strings.ToLower(cn.Condition.Operator)
	params := []interface{}{}

	switch op {
	case "eq":
		placeholder := s.nextPlaceholder()
		params = append(params, cn.Condition.Value)


		return fmt.Sprintf("%s = %s", field, placeholder), params, nil
	case "ne":
		placeholder := s.nextPlaceholder()
		params = append(params, cn.Condition.Value)


		return fmt.Sprintf("%s <> %s", field, placeholder), params, nil // Or use !=, <> is more standard
	case "gt":
		placeholder := s.nextPlaceholder()
		params = append(params, cn.Condition.Value)


		return fmt.Sprintf("%s > %s", field, placeholder), params, nil
	case "lt":
		placeholder := s.nextPlaceholder()
		params = append(params, cn.Condition.Value)


		return fmt.Sprintf("%s < %s", field, placeholder), params, nil
	case "gte":
		placeholder := s.nextPlaceholder()
		params = append(params, cn.Condition.Value)


		return fmt.Sprintf("%s >= %s", field, placeholder), params, nil
	case "lte":
		placeholder := s.nextPlaceholder()
		params = append(params, cn.Condition.Value)


		return fmt.Sprintf("%s <= %s", field, placeholder), params, nil
	case "like":
		placeholder := s.nextPlaceholder()
		params = append(params, cn.Condition.Value)


		return fmt.Sprintf("%s LIKE %s", field, placeholder), params, nil
	case "ilike":
		placeholder := s.nextPlaceholder()
		params = append(params, cn.Condition.Value)
		if s.dialect == PostgreSQL {


			return fmt.Sprintf("%s ILIKE %s", field, placeholder), params, nil
		}
		// For Spanner, ILIKE is not directly supported.
		// One common approach is LOWER(field) LIKE LOWER(value).
		// However, the prompt asked to map ilike to LIKE for Spanner initially.


		return fmt.Sprintf("%s LIKE %s", field, placeholder), params, nil
	case "in", "notin":
		if len(cn.Condition.Values) == 0 {
			// This case (e.g. field:in:()) should ideally be caught by the parser.
			// SQL doesn't support empty IN lists well. `field IN ()` is a syntax error.
			// Depending on desired behavior:
			// - `1=0` (always false) for `IN ()`
			// - `1=1` (always true) for `NOT IN ()`
			// For now, returning an error as it's ambiguous.


			return "", nil, errors.Wrapf(ErrUnsupportedOperator, "operator '%s' with empty list", op)
		}
		placeholders := make([]string, len(cn.Condition.Values))
		for i, v := range cn.Condition.Values {
			placeholders[i] = s.nextPlaceholder()
			params = append(params, v)
		}
		sqlOp := "IN"
		if op == "notin" {
			sqlOp = "NOT IN"
		}


		return fmt.Sprintf("%s %s (%s)", field, sqlOp, strings.Join(placeholders, ", ")), params, nil
	case "isnull":


		return fmt.Sprintf("%s IS NULL", field), params, nil
	case "isnotnull":


		return fmt.Sprintf("%s IS NOT NULL", field), params, nil
	// contains, startswith, endswith are often implemented with LIKE
	case "contains":
		placeholder := s.nextPlaceholder()
		params = append(params, "%"+cn.Condition.Value+"%")


		return fmt.Sprintf("%s LIKE %s", field, placeholder), params, nil
	case "startswith":
		placeholder := s.nextPlaceholder()
		params = append(params, cn.Condition.Value+"%")


		return fmt.Sprintf("%s LIKE %s", field, placeholder), params, nil
	case "endswith":
		placeholder := s.nextPlaceholder()
		params = append(params, "%"+cn.Condition.Value)


		return fmt.Sprintf("%s LIKE %s", field, placeholder), params, nil
	default:


		return "", nil, errors.Wrapf(ErrUnsupportedOperator, "operator: %s", op)
	}
}

func (s *SQLGenerator) generateLogicalOpSQL(ln *LogicalOpNode) (string, []interface{}, error) {
	leftSQL, leftParams, err := s.generateSQLRecursive(ln.Left)
	if err != nil {


		return "", nil, errors.Wrap(err, "failed to generate left side of logical operation")
	}

	rightSQL, rightParams, err := s.generateSQLRecursive(ln.Right)
	if err != nil {


		return "", nil, errors.Wrap(err, "failed to generate right side of logical operation")
	}

	sqlOperator := ""
	switch ln.Operator {
	case OperatorAnd:
		sqlOperator = "AND"
	case OperatorOr:
		sqlOperator = "OR"
	default:


		return "", nil, errors.Wrapf(ErrUnsupportedOperator, "logical operator: %s", ln.Operator)
	}

	// Handle cases where one side might be empty (e.g. if parser allowed incomplete logical ops, though current parser does not)
	if leftSQL == "" {


		return rightSQL, rightParams, nil
	}
	if rightSQL == "" {


		return leftSQL, leftParams, nil
	}

	combinedSQL := fmt.Sprintf("(%s %s %s)", leftSQL, sqlOperator, rightSQL)
	allParams := append(leftParams, rightParams...)


	return combinedSQL, allParams, nil
}

func (s *SQLGenerator) generateGroupSQL(gn *GroupNode) (string, []interface{}, error) {
	exprSQL, exprParams, err := s.generateSQLRecursive(gn.Expression)
	if err != nil {


		return "", nil, errors.Wrap(err, "failed to generate grouped expression")
	}

	// Avoid empty parentheses like "()" if the inner expression was somehow empty.
	if exprSQL == "" {
		// This might mean an empty group like `()` which parser should prevent,
		// or an expression that validly generates no SQL (e.g. a future "always true" node).
		// For now, assume inner expression should always produce some SQL.


		return "", nil, errors.New("grouped expression generated empty SQL")
	}


	return fmt.Sprintf("(%s)", exprSQL), exprParams, nil
}
