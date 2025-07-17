package resource

import (
	"fmt"
	"sort"
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
func (g *PostgreSQLGenerator) GenerateSQL(node ExpressionNode) (sqlStr string, params []any, err error) {
	sqlStr, queryParams, err := g.sqlGenerator.GenerateSQL(node)
	if err != nil {
		return "", nil, err
	}

	params = make([]any, 0, len(queryParams))
	for _, qp := range queryParams {
		params = append(params, qp.Value)
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
func (g *SpannerGenerator) GenerateSQL(node ExpressionNode) (sqlStr string, params map[string]any, err error) {
	sqlStr, queryParams, err := g.sqlGenerator.GenerateSQL(node)
	if err != nil {
		return "", nil, err
	}

	namedParams := make(map[string]any)
	for _, qp := range queryParams {
		namedParams[qp.Name] = qp.Value
	}

	return sqlStr, namedParams, nil
}

// substituteSQLParams replaces placeholders in an SQL string with their actual values.
// This function is intended for logging or debugging purposes and does NOT sanitize inputs,
// so it should NOT be used for executing queries against a database to prevent SQL injection.
func substituteSQLParams(sql string, params any, dialect SQLDialect) (string, error) {
	if sql == "" {
		return "", nil
	}

	switch dialect {
	case Spanner:
		spannerParams, ok := params.(map[string]any)
		if !ok {
			if params == nil { // Allow nil params for Spanner if no substitution is needed
				return sql, nil
			}

			return "", errors.New("SubstituteSQLParams: for Spanner dialect, params must be map[string]any")
		}
		if len(spannerParams) == 0 {
			return sql, nil
		}

		// Create a list of keys and sort by length in descending order
		// to prevent shorter keys from replacing parts of longer keys (e.g., @p before @p1).
		keys := make([]string, 0, len(spannerParams))
		for k := range spannerParams {
			keys = append(keys, k)
		}
		// Sort keys by length descending
		// In case of equal length, sort alphabetically for stable output (optional)
		sort.Slice(keys, func(i, j int) bool {
			if len(keys[i]) == len(keys[j]) {
				return keys[i] > keys[j] // Secondary sort: alphabetical descending for stability
			}

			return len(keys[i]) > len(keys[j])
		})

		for _, k := range keys {
			v := spannerParams[k]
			placeholder := "@" + k
			// Using fmt.Sprintf for value substitution, similar to original behavior.
			// String values ideally should be SQL-escaped and quoted for actual query execution.
			valStr := ""
			if strVal, isStr := v.(string); isStr {
				// Simple quoting for strings for debug output
				valStr = "'" + strings.ReplaceAll(strVal, "'", "''") + "'"
			} else {
				valStr = fmt.Sprintf("%v", v)
			}
			// Regex might be safer, but strings.ReplaceAll should work for simple @key placeholders.
			// Need to be careful if keys can appear in other contexts.
			sql = strings.ReplaceAll(sql, placeholder, valStr)
		}

	case PostgreSQL:
		pgParams, ok := params.([]any)
		if !ok {
			if params == nil { // Allow nil params for PostgreSQL if no substitution is needed
				return sql, nil
			}

			return "", errors.New("SubstituteSQLParams: for PostgreSQL dialect, params must be []any")
		}
		if len(pgParams) == 0 {
			return sql, nil
		}

		// Iterate backwards to correctly replace multi-digit placeholders (e.g., $10 before $1)
		for i := len(pgParams) - 1; i >= 0; i-- {
			placeholder := fmt.Sprintf("$%d", i+1)
			v := pgParams[i]
			// Using fmt.Sprintf for value substitution.
			// String values ideally should be SQL-escaped and quoted for actual query execution.
			valStr := ""
			if strVal, isStr := v.(string); isStr {
				// Simple quoting for strings for debug output
				valStr = "'" + strings.ReplaceAll(strVal, "'", "''") + "'"
			} else {
				valStr = fmt.Sprintf("%v", v)
			}
			sql = strings.ReplaceAll(sql, placeholder, valStr)
		}

	default:
		return "", errors.Newf("SubstituteSQLParams: unsupported SQL dialect %v", dialect)
	}

	return sql, nil
}
