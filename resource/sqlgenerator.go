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

// QueryParam holds a single query parameter's name and value.
type QueryParam struct {
	Name  string
	Value any
}

// ErrUnsupportedNodeType indicates an AST node type that the SQL generator cannot handle.
var ErrUnsupportedNodeType = errors.New("unsupported AST node type for SQL generation")

// ErrUnsupportedOperator indicates a condition operator that the SQL generator cannot handle.
var ErrUnsupportedOperator = errors.New("unsupported operator for SQL generation")

// sqlGenerator translates an ExpressionNode AST into a SQL query string and parameters.
type sqlGenerator struct {
	dialect    SQLDialect
	paramCount int
}

// newSQLGenerator creates a new SQL generator for the specified dialect.
func newSQLGenerator(dialect SQLDialect) *sqlGenerator {
	return &sqlGenerator{
		dialect: dialect,
	}
}

// GenerateSQL converts an AST node into a SQL string and a list of parameters.
// It resets the parameter counter for each top-level call.
func (s *sqlGenerator) GenerateSQL(node ExpressionNode) (string, []QueryParam, error) {
	s.paramCount = 0 // Reset for each new generation pass

	return s.generateSQLRecursive(node)
}

// generateSQLRecursive is the internal recursive part of SQL generation.
func (s *sqlGenerator) generateSQLRecursive(node ExpressionNode) (string, []QueryParam, error) {
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

func (s *sqlGenerator) quoteIdentifier(identifier string) string {
	// Spanner identifiers are case-sensitive and should be quoted with backticks if they contain non-alphanumeric chars or match keywords.
	// PostgreSQL identifiers are folded to lower case unless quoted with double quotes.
	if s.dialect == Spanner {
		return "`" + strings.ReplaceAll(identifier, "`", "``") + "`"
	}
	// For PostgreSQL, double quotes preserve case and allow special characters.

	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}

func (s *sqlGenerator) nextPlaceholder() string {
	s.paramCount++
	if s.dialect == Spanner {
		return fmt.Sprintf("@p%d", s.paramCount)
	}

	return fmt.Sprintf("$%d", s.paramCount)
}

func (s *sqlGenerator) generateConditionSQL(cn *ConditionNode) (string, []QueryParam, error) {
	field := s.quoteIdentifier(cn.Condition.Field)
	op := strings.ToLower(cn.Condition.Operator)
	var params []QueryParam

	switch op {
	case "eq", "ne", "gt", "lt", "gte", "lte":
		placeholder := s.nextPlaceholder()
		params = append(params, QueryParam{Name: strings.TrimPrefix(placeholder, "@"), Value: cn.Condition.Value})
		sqlOp := ""
		switch op {
		case "eq":
			sqlOp = "="
		case "ne":
			sqlOp = "<>"
		case "gt":
			sqlOp = ">"
		case "lt":
			sqlOp = "<"
		case "gte":
			sqlOp = ">="
		case "lte":
			sqlOp = "<="
		}

		return fmt.Sprintf("%s %s %s", field, sqlOp, placeholder), params, nil
	case "in", "notin":
		placeholders := make([]string, len(cn.Condition.Values))
		params = make([]QueryParam, 0, len(cn.Condition.Values))
		for i, v := range cn.Condition.Values {
			placeholder := s.nextPlaceholder()
			placeholders[i] = placeholder
			params = append(params, QueryParam{Name: strings.TrimPrefix(placeholder, "@"), Value: v})
		}
		sqlOp := "IN"
		if op == "notin" {
			sqlOp = "NOT IN"
		}

		return fmt.Sprintf("%s %s (%s)", field, sqlOp, strings.Join(placeholders, ", ")), params, nil
	case "isnull":
		return fmt.Sprintf("%s IS NULL", field), nil, nil
	case "isnotnull":
		return fmt.Sprintf("%s IS NOT NULL", field), nil, nil
	default:
		return "", nil, errors.Wrapf(ErrUnsupportedOperator, "operator: %s", op)
	}
}

func (s *sqlGenerator) generateLogicalOpSQL(ln *LogicalOpNode) (string, []QueryParam, error) {
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

	combinedSQL := fmt.Sprintf("%s %s %s", leftSQL, sqlOperator, rightSQL)
	allParams := append(leftParams, rightParams...)

	return combinedSQL, allParams, nil
}

func (s *sqlGenerator) generateGroupSQL(gn *GroupNode) (string, []QueryParam, error) {
	exprSQL, exprParams, err := s.generateSQLRecursive(gn.Expression)
	if err != nil {
		return "", nil, errors.Wrap(err, "failed to generate grouped expression")
	}

	return fmt.Sprintf("(%s)", exprSQL), exprParams, nil
}

// PostgreSQLGenerator is a SQL generator for PostgreSQL.
type PostgreSQLGenerator struct {
	*sqlGenerator
}

// NewPostgreSQLGenerator creates a new PostgreSQLGenerator.
func NewPostgreSQLGenerator() *PostgreSQLGenerator {
	return &PostgreSQLGenerator{
		sqlGenerator: newSQLGenerator(PostgreSQL),
	}
}

// GenerateSQL generates SQL for the given node.
func (g *PostgreSQLGenerator) GenerateSQL(node ExpressionNode) (string, []any, error) {
	sqlStr, queryParams, err := g.sqlGenerator.GenerateSQL(node)
	if err != nil {
		return "", nil, err
	}

	params := make([]any, 0, len(queryParams))
	if queryParams != nil {
		for _, qp := range queryParams {
			params = append(params, qp.Value)
		}
	}

	return sqlStr, params, nil
}

// SpannerGenerator is a SQL generator for Spanner.
type SpannerGenerator struct {
	*sqlGenerator
}

// NewSpannerGenerator creates a new SpannerGenerator.
func NewSpannerGenerator() *SpannerGenerator {
	return &SpannerGenerator{
		sqlGenerator: newSQLGenerator(Spanner),
	}
}

// GenerateSQL generates SQL for the given node and returns named parameters.
func (g *SpannerGenerator) GenerateSQL(node ExpressionNode) (string, map[string]any, error) {
	sqlStr, queryParams, err := g.sqlGenerator.GenerateSQL(node)
	if err != nil {
		return "", nil, err
	}

	namedParams := make(map[string]any)

	if queryParams != nil {
		for _, qp := range queryParams {
			namedParams[qp.Name] = qp.Value
		}
	}

	return sqlStr, namedParams, nil
}
