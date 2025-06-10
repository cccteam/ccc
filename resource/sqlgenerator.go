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
func (s *SQLGenerator) GenerateSQL(node ExpressionNode) (string, []any, error) {
	s.paramCount = 0 // Reset for each new generation pass

	return s.generateSQLRecursive(node)
}

// generateSQLRecursive is the internal recursive part of SQL generation.
func (s *SQLGenerator) generateSQLRecursive(node ExpressionNode) (string, []any, error) {
	switch n := node.(type) {
	case *ConditionNode:
		return s.generateConditionSQL(n)
	case *LogicalOpNode:
		return s.generateLogicalOpSQL(n)
	case *GroupNode:
		return s.generateGroupSQL(n)
	case nil:
		return "", nil, nil
	default:
		return "", nil, errors.Wrapf(ErrUnsupportedNodeType, "type: %T", n)
	}
}

func (s *SQLGenerator) quoteIdentifier(identifier string) string {
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

func (s *SQLGenerator) generateConditionSQL(cn *ConditionNode) (string, []any, error) {
	field := s.quoteIdentifier(cn.Condition.Field)
	op := strings.ToLower(cn.Condition.Operator)
	params := []any{}

	switch op {
	case "eq":
		placeholder := s.nextPlaceholder()
		params = append(params, cn.Condition.Value)

		return fmt.Sprintf("%s = %s", field, placeholder), params, nil
	case "ne":
		placeholder := s.nextPlaceholder()
		params = append(params, cn.Condition.Value)

		return fmt.Sprintf("%s <> %s", field, placeholder), params, nil
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
	case "in", "notin":
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
	default:

		return "", nil, errors.Wrapf(ErrUnsupportedOperator, "operator: %s", op)
	}
}

func (s *SQLGenerator) generateLogicalOpSQL(ln *LogicalOpNode) (string, []any, error) {
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

func (s *SQLGenerator) generateGroupSQL(gn *GroupNode) (string, []any, error) {
	exprSQL, exprParams, err := s.generateSQLRecursive(gn.Expression)
	if err != nil {
		return "", nil, errors.Wrap(err, "failed to generate grouped expression")
	}

	return fmt.Sprintf("(%s)", exprSQL), exprParams, nil
}
