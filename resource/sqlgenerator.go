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

// generateSQLRecursive is the internal recursive part of SQL generation for expression trees.
func (s *sqlGenerator) generateSQLRecursive(node ExpressionNode) (string, []QueryParam, error) {
	switch n := node.(type) {
	case *ConditionNode:
		return s.generateConditionSQL(n)
	case *LogicalOpNode:
		return s.generateLogicalOpSQL(n)
	case *GroupNode:
		return s.generateGroupSQL(n)
	case nil:
		return "", nil, nil // A nil node means no condition, which is valid.
	default:
		return "", nil, errors.Wrapf(ErrUnsupportedNodeType, "type: %T", n)
	}
}

func (s *sqlGenerator) quoteIdentifier(identifier string) string {
	if s.dialect == Spanner {
		return "`" + strings.ReplaceAll(identifier, "`", "``") + "`"
	}
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}

func (s *sqlGenerator) nextPlaceholder() string {
	s.paramCount++
	if s.dialect == Spanner {
		// Spanner params are @p1, @p2, etc. Name in QueryParam doesn't include "@"
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
		paramName := strings.TrimPrefix(placeholder, "@") // For Spanner, remove "@" for map key
		params = append(params, QueryParam{Name: paramName, Value: cn.Condition.Value})
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
		if len(cn.Condition.Values) == 0 { // Handle empty IN/NOT IN list
			if op == "in" {
				return "1=0", nil, nil // Or specific SQL for "false" based on dialect if necessary
			}
			return "1=1", nil, nil // Or specific SQL for "true"
		}
		placeholders := make([]string, len(cn.Condition.Values))
		params = make([]QueryParam, 0, len(cn.Condition.Values))
		for i, v := range cn.Condition.Values {
			placeholder := s.nextPlaceholder()
			paramName := strings.TrimPrefix(placeholder, "@")
			placeholders[i] = placeholder
			params = append(params, QueryParam{Name: paramName, Value: v})
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

	// If one side is empty, just return the other side.
	// This can happen if a nil node was passed on one side.
	if leftSQL == "" && rightSQL == "" {
		return "", nil, nil
	}
	if leftSQL == "" {
		return rightSQL, rightParams, nil
	}
	if rightSQL == "" {
		return leftSQL, leftParams, nil
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
	if exprSQL == "" { // Avoid empty parentheses if the inner expression was nil
		return "", nil, nil
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

// GenerateSQL generates SQL for the given query components.
func (g *PostgreSQLGenerator) GenerateSQL(baseTable string, joins []*JoinClauseNode, filter ExpressionNode) (sqlStr string, params []any, err error) {
	g.sqlGenerator.paramCount = 0 // Reset parameter count for a new query
	var allQueryParams []QueryParam
	var sb strings.Builder

	// SELECT (assuming * for now, selectFields will be used in a future step)
	sb.WriteString("SELECT * FROM ")
	sb.WriteString(g.sqlGenerator.quoteIdentifier(baseTable))

	// JOIN clauses
	for _, joinNode := range joins {
		if joinNode == nil || joinNode.On.tree == nil { // Defensive check
			return "", nil, errors.New("invalid join node or join condition")
		}
		sb.WriteString(fmt.Sprintf(" %s JOIN %s ON ", string(joinNode.Type), g.sqlGenerator.quoteIdentifier(joinNode.Target)))
		onSQL, onParams, joinErr := g.sqlGenerator.generateSQLRecursive(joinNode.On.tree)
		if joinErr != nil {
			return "", nil, errors.Wrapf(joinErr, "generating JOIN ON clause for target %s", joinNode.Target)
		}
		if onSQL == "" { // Should not happen with valid On clause, but good to check
			return "", nil, errors.Errorf("empty ON clause generated for target %s", joinNode.Target)
		}
		sb.WriteString(onSQL)
		allQueryParams = append(allQueryParams, onParams...)
	}

	// WHERE clause
	if filter != nil {
		filterSQL, filterParams, filterErr := g.sqlGenerator.generateSQLRecursive(filter)
		if filterErr != nil {
			return "", nil, errors.Wrap(filterErr, "generating WHERE clause")
		}
		if filterSQL != "" {
			sb.WriteString(" WHERE ")
			sb.WriteString(filterSQL)
			allQueryParams = append(allQueryParams, filterParams...)
		}
	}

	// Convert QueryParams to []any for PostgreSQL
	finalParams := make([]any, 0, len(allQueryParams))
	// Ensure params are ordered correctly by $1, $2, ...
	// The paramCount in sqlGenerator ensures nextPlaceholder generates them in order.
	// QueryParam.Name for PostgreSQL is not strictly needed for $N style, but Value is.
	// We can sort by Name (which would be "p1", "p2" if derived from spanner style, or just use index)
	// For simplicity, as nextPlaceholder for PG already generates $1, $2, etc. in order,
	// and generateSQLRecursive appends params in order of appearance,
	// we can just iterate allQueryParams.
	for _, qp := range allQueryParams {
		finalParams = append(finalParams, qp.Value)
	}

	return sb.String(), finalParams, nil
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

// GenerateSQL generates SQL for the given query components and returns named parameters.
func (g *SpannerGenerator) GenerateSQL(baseTable string, joins []*JoinClauseNode, filter ExpressionNode) (sqlStr string, params map[string]any, err error) {
	g.sqlGenerator.paramCount = 0 // Reset parameter count for a new query
	var allQueryParams []QueryParam
	var sb strings.Builder

	// SELECT (assuming * for now)
	sb.WriteString("SELECT * FROM ")
	sb.WriteString(g.sqlGenerator.quoteIdentifier(baseTable))

	// JOIN clauses
	for _, joinNode := range joins {
		if joinNode == nil || joinNode.On.tree == nil { // Defensive check
			return "", nil, errors.New("invalid join node or join condition")
		}
		sb.WriteString(fmt.Sprintf(" %s JOIN %s ON ", string(joinNode.Type), g.sqlGenerator.quoteIdentifier(joinNode.Target)))
		onSQL, onParams, joinErr := g.sqlGenerator.generateSQLRecursive(joinNode.On.tree)
		if joinErr != nil {
			return "", nil, errors.Wrapf(joinErr, "generating JOIN ON clause for target %s", joinNode.Target)
		}
		if onSQL == "" {
			return "", nil, errors.Errorf("empty ON clause generated for target %s", joinNode.Target)
		}
		sb.WriteString(onSQL)
		allQueryParams = append(allQueryParams, onParams...)
	}

	// WHERE clause
	if filter != nil {
		filterSQL, filterParams, filterErr := g.sqlGenerator.generateSQLRecursive(filter)
		if filterErr != nil {
			return "", nil, errors.Wrap(filterErr, "generating WHERE clause")
		}
		if filterSQL != "" {
			sb.WriteString(" WHERE ")
			sb.WriteString(filterSQL)
			allQueryParams = append(allQueryParams, filterParams...)
		}
	}

	// Convert QueryParams to map[string]any for Spanner
	namedParams := make(map[string]any)
	for _, qp := range allQueryParams {
		// qp.Name already has "pX" from nextPlaceholder logic for Spanner
		namedParams[qp.Name] = qp.Value
	}

	return sb.String(), namedParams, nil
}

// substituteSQLParams remains unchanged for this subtask.
// ... (rest of the substituteSQLParams function as it was)
func substituteSQLParams(sql string, params any, dialect SQLDialect) (string, error) {
	if sql == "" {
		return "", nil
	}

	switch dialect {
	case Spanner:
		spannerParams, ok := params.(map[string]any)
		if !ok {
			if params == nil {
				return sql, nil
			}
			return "", errors.New("SubstituteSQLParams: for Spanner dialect, params must be map[string]any")
		}
		if len(spannerParams) == 0 {
			return sql, nil
		}
		keys := make([]string, 0, len(spannerParams))
		for k := range spannerParams {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool {
			if len(keys[i]) == len(keys[j]) {
				return keys[i] > keys[j]
			}
			return len(keys[i]) > len(keys[j])
		})

		for _, k := range keys {
			v := spannerParams[k]
			placeholder := "@" + k
			valStr := ""
			if strVal, isStr := v.(string); isStr {
				valStr = "'" + strings.ReplaceAll(strVal, "'", "''") + "'"
			} else {
				valStr = fmt.Sprintf("%v", v)
			}
			sql = strings.ReplaceAll(sql, placeholder, valStr)
		}

	case PostgreSQL:
		pgParams, ok := params.([]any)
		if !ok {
			if params == nil {
				return sql, nil
			}
			return "", errors.New("SubstituteSQLParams: for PostgreSQL dialect, params must be []any")
		}
		if len(pgParams) == 0 {
			return sql, nil
		}
		for i := len(pgParams) - 1; i >= 0; i-- {
			placeholder := fmt.Sprintf("$%d", i+1)
			v := pgParams[i]
			valStr := ""
			if strVal, isStr := v.(string); isStr {
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
