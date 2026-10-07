package resource

import (
	"fmt"
	"math/big"
	"slices"
	"strings"

	"cloud.google.com/go/spanner"
	"github.com/go-playground/errors/v5"
	"github.com/shopspring/decimal"
)

// This file extends the module's single SQL emitter with the node shapes the
// condition lowering produces (ABAC design plan §05): NOT, EXISTS, the null
// guard, named-parameter and column comparands, scalar subqueries, and a
// statement-scoped parameter registry so every fragment of one statement —
// row-visibility WHERE, per-column CASE, check-SELECT booleans — allocates
// from one namespace. The new node types are unexported and constructor-fed
// by the lowering alone, so filter-reachable paths can never produce them,
// and they render only under a registry.

// SQL relational operator spellings shared by the filter path and the
// condition lowering, and the literal an empty disjunction collapses to.
const (
	sqlNotEqual  = "<>"
	sqlLessEq    = "<="
	sqlGreaterEq = ">="
	sqlFalse     = "FALSE"
)

// Reserved named parameters the lowered SQL binds. The statement builder
// supplies their values: the checked user's identity, the request's decision
// instant (the same value the check folded with), and the request partition.
const (
	subjectParamName = "subject"
	nowParamName     = "now"
	domainParamName  = "domain"
)

// paramRegistry is the statement-scoped parameter and alias namespace. Bound
// values allocate @_c placeholders (disjoint from the filter path's @_p, so a
// filter and lowered conditions coexist in one statement); fixed named
// parameters are recorded so the statement builder knows which values the
// statement needs; table aliases stay unique across every fragment.
//
// Postgres types a parameter from the context it meets, and cannot where a parameter
// meets another or a function: a comparison of two bound values, or a proposed value
// against a literal, fails with an undetermined type. Spanner types each parameter from
// its Go value, so the Postgres statement says what the value is: a registry made for
// Postgres records the type of each value a lowered comparison binds, and the comparison
// renders the placeholder under a CAST to it where nothing else types it (placeholderStyle).
type paramRegistry struct {
	dbType     DBType
	paramCount int
	aliasCount int
	params     []QueryParam
	named      map[string]struct{}
	// casts are the Postgres types of the bound values by name, empty where the value's
	// type is not known (a NULL).
	casts map[string]string
}

func newParamRegistry(dbType DBType) *paramRegistry {
	return &paramRegistry{dbType: dbType, named: make(map[string]struct{}), casts: make(map[string]string)}
}

// allocate binds value under a fresh name, typed when the caller renders the value
// where Postgres cannot infer its type from the other operand.
func (r *paramRegistry) allocate(value any, typed bool) string {
	r.paramCount++
	name := fmt.Sprintf("_c%d", r.paramCount)
	bound := paramValue(value)
	r.params = append(r.params, QueryParam{Name: name, Value: bound})
	if typed && r.dbType == PostgresDBType {
		if cast := postgresCast(bound); cast != "" {
			r.casts[name] = cast
		}
	}

	return name
}

// bind allocates a placeholder for value and returns it, @-prefixed.
func (r *paramRegistry) bind(value any) string {
	return "@" + r.allocate(value, false)
}

// placeholderStyle is how a lowered comparison renders a parameter on Postgres, which
// types a parameter from what it meets.
type placeholderStyle int

// The styles differ for strings alone. A number, an instant or a boolean keeps its CAST
// beside a column too: Postgres compares the column in the parameter's type, so a NUMERIC
// beside an integer column compares exactly, as on Spanner, where a bare parameter would
// have to become an integer first. A string's CAST to TEXT would fail beside a column of
// another type that a condition states as a string: a uuid, an enumeration, a JSON
// document.
const (
	// placeholderTyped renders the parameter under a CAST to its value's type, a string
	// under the byte-order collation: the other operand is a parameter or a function, so
	// nothing else types it.
	placeholderTyped placeholderStyle = iota
	// placeholderInferred renders a string parameter bare: the column or scalar subquery
	// on the other side types it.
	placeholderInferred
	// placeholderOrdered renders a string parameter bare under the byte-order collation:
	// the other side types it, and the comparison orders, which the condition language
	// does by code point.
	placeholderOrdered
)

// bindTyped allocates a placeholder for a value a lowered comparison renders where
// nothing else types it: on Postgres it carries the CAST to the value's type.
func (r *paramRegistry) bindTyped(value any) string {
	return r.bindStyled(value, placeholderTyped)
}

// bindStyled allocates a placeholder for a value a lowered comparison renders, in the
// style the comparison calls for.
func (r *paramRegistry) bindStyled(value any, style placeholderStyle) string {
	return r.placeholder(r.allocate(value, true), style)
}

// bindName allocates a typed parameter for value and returns its name, without the @,
// for a caller that renders it later through reference.
func (r *paramRegistry) bindName(value any) string {
	return r.allocate(value, true)
}

// placeholder renders a parameter's name as the statement names it: @-prefixed, and under
// the CAST or the collation its recorded type and the style call for.
func (r *paramRegistry) placeholder(name string, style placeholderStyle) string {
	cast, ok := r.casts[name]
	switch {
	case !ok:
		return "@" + name
	case cast != postgresText:
		return fmt.Sprintf("CAST(@%s AS %s)", name, cast)
	case style == placeholderInferred:
		return "@" + name
	case style == placeholderOrdered:
		// The condition language compares strings by code point, as Spanner does; a
		// locale collation would order a comparison of two parameters, or of a column in
		// one, differently. An explicit collation wins over a column's.
		return fmt.Sprintf(`(@%s COLLATE "C")`, name)
	default:
		return fmt.Sprintf(`(CAST(@%s AS %s) COLLATE "C")`, name, cast)
	}
}

// reference records the statement's use of a named parameter, fixed or bound, and
// returns the placeholder to render, typed where its value's type was recorded.
func (r *paramRegistry) reference(name string) string {
	return r.referenceStyled(name, placeholderTyped)
}

// referenceStyled records the statement's use of a named parameter and returns the
// placeholder to render in the style the comparison calls for.
//
// A bound string referenced bare, where the other operand types it, renders a copy of
// its own: pgx sends a named parameter as one positional parameter, and Postgres deduces
// one type for it, so a proposed value compared once beside a uuid column and once under
// its CAST to TEXT would be refused with inconsistent types. Each bare use takes its type
// from its own operand.
func (r *paramRegistry) referenceStyled(name string, style placeholderStyle) string {
	r.named[name] = struct{}{}
	if style != placeholderTyped && r.casts[name] == postgresText {
		if value, ok := r.boundValue(name); ok {
			return r.placeholder(r.allocate(value, true), style)
		}
	}

	return r.placeholder(name, style)
}

// boundValue returns the value bound under name, if the registry bound it.
func (r *paramRegistry) boundValue(name string) (any, bool) {
	for _, param := range r.params {
		if param.Name == name {
			return param.Value, true
		}
	}

	return nil, false
}

// paramValue normalizes a value for query-parameter typing. Spanner types a query
// parameter from its Go value, and Encoder-backed types encode only a wire value:
// decimal.Decimal encodes as a STRING, which types the parameter STRING wherever it
// meets a NUMERIC column — a masked-cell filler's CASE and a post-image comparison
// both fail with a type mismatch. big.Rat and NullNumeric carry the NUMERIC typing
// the column context requires. (Mutations are unaffected: the server types mutation
// values from the target column.) The mapping is Spanner-shaped: the Postgres runtime
// converts the values back at its boundary (postgresValue), and a lowered comparison
// binds them under a CAST (bindTyped).
func paramValue(value any) any {
	switch v := value.(type) {
	case decimal.Decimal:
		return v.Rat()
	case *decimal.Decimal:
		if v == nil {
			return (*big.Rat)(nil)
		}

		return v.Rat()
	case decimal.NullDecimal:
		if !v.Valid {
			return spanner.NullNumeric{}
		}

		return spanner.NullNumeric{Numeric: *v.Decimal.Rat(), Valid: true}
	default:
		return value
	}
}

// alias allocates a statement-unique table alias.
func (r *paramRegistry) alias() string {
	r.aliasCount++

	return fmt.Sprintf("ca%d", r.aliasCount)
}

// boundParams returns the values bound so far, in allocation order.
func (r *paramRegistry) boundParams() []QueryParam {
	return r.params
}

// referencedNames returns the fixed named parameters the statement uses,
// sorted for deterministic assembly.
func (r *paramRegistry) referencedNames() []string {
	if len(r.named) == 0 {
		return nil
	}
	names := make([]string, 0, len(r.named))
	for name := range r.named {
		names = append(names, name)
	}
	slices.Sort(names)

	return names
}

// columnRef is a (possibly alias-qualified) column reference.
type columnRef struct {
	qualifier string // "" renders the bare column
	column    string
}

// comparandKind is the closed set of things a lowered comparison compares.
type comparandKind int

const (
	comparandColumn comparandKind = iota
	comparandValue
	comparandNamed
	comparandSubquery
)

// comparand is one side of a lowered comparison: a column reference, a bound
// literal value, a fixed named parameter, or a scalar subquery.
type comparand struct {
	kind     comparandKind
	column   columnRef
	value    any
	named    string
	subquery *scalarSubqueryNode
}

func columnComparand(qualifier, column string) comparand {
	return comparand{kind: comparandColumn, column: columnRef{qualifier: qualifier, column: column}}
}

func valueComparand(value any) comparand {
	return comparand{kind: comparandValue, value: value}
}

func namedComparand(name string) comparand {
	return comparand{kind: comparandNamed, named: name}
}

func subqueryComparand(subquery *scalarSubqueryNode) comparand {
	return comparand{kind: comparandSubquery, subquery: subquery}
}

// typesOperand reports whether the comparand types the other side of a comparison for
// Postgres: a column and a scalar subquery do, a parameter does not.
func (c *comparand) typesOperand() bool {
	return c.kind == comparandColumn || c.kind == comparandSubquery
}

// isOrderingOperator reports whether the SQL operator orders its operands rather than
// testing their equality.
func isOrderingOperator(op string) bool {
	switch op {
	case "<", sqlLessEq, ">", sqlGreaterEq:
		return true
	default:
		return false
	}
}

// loweredComparisonNode relates two comparands with a relational operator.
type loweredComparisonNode struct {
	left  comparand
	op    string // the SQL operator: = <> < <= > >=
	right comparand
}

func (n *loweredComparisonNode) String() string {
	return fmt.Sprintf("lowered(%v %s %v)", n.left, n.op, n.right)
}

// loweredInNode tests an attribute value against a literal list.
type loweredInNode struct {
	left    comparand
	negated bool
	values  []any
}

func (n *loweredInNode) String() string {
	return fmt.Sprintf("lowered(%v in %v)", n.left, n.values)
}

// loweredNullTestNode is IS [NOT] NULL on an attribute value.
type loweredNullTestNode struct {
	left    comparand
	negated bool
}

func (n *loweredNullTestNode) String() string {
	return fmt.Sprintf("lowered(%v is null, negated=%v)", n.left, n.negated)
}

// notNode negates its expression.
type notNode struct {
	expr ExpressionNode
}

func (n *notNode) String() string {
	return fmt.Sprintf("NOT (%s)", n.expr.String())
}

// existsNode is a correlated EXISTS over one aliased table; correlation
// equalities and inner predicates all live in where.
type existsNode struct {
	table string
	alias string
	where ExpressionNode
}

func (n *existsNode) String() string {
	return fmt.Sprintf("EXISTS(%s %s: %s)", n.table, n.alias, n.where.String())
}

// nullGuardNode makes a predicate UNKNOWN where a value is NULL: CASE WHEN
// value IS NULL THEN NULL ELSE expr END. It guards the EXISTS a subject set
// renders, which is TRUE or FALSE on its own, so a membership test against no
// value reads as SQL's UNKNOWN the way a comparison does.
type nullGuardNode struct {
	value comparand
	expr  ExpressionNode
}

func (n *nullGuardNode) String() string {
	return fmt.Sprintf("guard(%v is null: %s)", n.value, n.expr.String())
}

// truthNode is a constant boolean predicate; residual trees are usually
// fact-folded before lowering, so it renders only defensively.
type truthNode struct {
	value bool
}

func (n *truthNode) String() string {
	if n.value {
		return "TRUE"
	}

	return sqlFalse
}

// scalarSubqueryNode selects one column off an aliased table under a
// predicate — the @subjectValue rendering: an empty result is SQL NULL, which
// no condition can find TRUE, so a missing row fails closed.
type scalarSubqueryNode struct {
	table  string
	alias  string
	column string
	where  ExpressionNode
}

// The lowered-node rendering. Every lowered node requires the registry: the
// lowering allocates parameters and aliases from the statement's namespace,
// never from the per-call filter counter.

// generateLowered renders a lowered expression under the statement's
// registry.
func (s *sqlGenerator) generateLowered(node ExpressionNode, registry *paramRegistry) (string, error) {
	if registry == nil {
		return "", errors.New("lowered SQL generation requires a statement registry")
	}
	prev := s.registry
	s.registry = registry
	defer func() { s.registry = prev }()

	sql, params, err := s.generateSQLRecursive(node)
	if err != nil {
		return "", err
	}
	if len(params) > 0 {
		// Old-style nodes allocate from the per-call counter; a lowered tree
		// must never contain one, or two fragments of a statement collide.
		return "", errors.New("lowered expression contains filter-path nodes")
	}

	return sql, nil
}

func (s *sqlGenerator) generateLoweredComparisonSQL(n *loweredComparisonNode) (string, error) {
	// A parameter beside a column or a subquery is typed by it; one beside another
	// parameter says its own type.
	style := placeholderTyped
	if n.left.typesOperand() || n.right.typesOperand() {
		style = placeholderInferred
		if isOrderingOperator(n.op) {
			style = placeholderOrdered
		}
	}
	left, err := s.renderComparand(&n.left, style)
	if err != nil {
		return "", err
	}
	right, err := s.renderComparand(&n.right, style)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("%s %s %s", left, n.op, right), nil
}

// renderComparand renders one side of a comparison; a parameter renders in the given
// style.
func (s *sqlGenerator) renderComparand(c *comparand, style placeholderStyle) (string, error) {
	switch c.kind {
	case comparandColumn:
		return s.renderColumnRef(c.column), nil
	case comparandValue:
		return s.registry.bindStyled(c.value, style), nil
	case comparandNamed:
		return s.registry.referenceStyled(c.named, style), nil
	case comparandSubquery:
		return s.renderScalarSubquery(c.subquery)
	default:
		return "", errors.Newf("unsupported comparand kind %d", c.kind)
	}
}

func (s *sqlGenerator) renderColumnRef(ref columnRef) string {
	if ref.qualifier == "" {
		return s.quoteIdentifier(ref.column)
	}

	return s.quoteIdentifier(ref.qualifier) + "." + s.quoteIdentifier(ref.column)
}

func (s *sqlGenerator) generateLoweredInSQL(n *loweredInNode) (string, error) {
	// The lowering admits no bound value on the left, so a left parameter is a proposed
	// value, which says its own type; the list's values are typed by a column on the
	// left and say their own type beside a parameter.
	left, err := s.renderComparand(&n.left, placeholderTyped)
	if err != nil {
		return "", err
	}
	style := placeholderTyped
	if n.left.typesOperand() {
		style = placeholderInferred
	}
	placeholders := make([]string, 0, len(n.values))
	for _, v := range n.values {
		placeholders = append(placeholders, s.registry.bindStyled(v, style))
	}
	op := "IN"
	if n.negated {
		op = "NOT IN"
	}

	return fmt.Sprintf("%s %s (%s)", left, op, strings.Join(placeholders, ", ")), nil
}

func (s *sqlGenerator) generateLoweredNullTestSQL(n *loweredNullTestNode) (string, error) {
	// A bare parameter has no type for IS NULL to test; a proposed value says its own.
	left, err := s.renderComparand(&n.left, placeholderTyped)
	if err != nil {
		return "", err
	}
	if n.negated {
		return left + " IS NOT NULL", nil
	}

	return left + " IS NULL", nil
}

func (s *sqlGenerator) generateNotSQL(n *notNode) (string, error) {
	inner, _, err := s.generateSQLRecursive(n.expr)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("NOT (%s)", inner), nil
}

func (s *sqlGenerator) generateNullGuardSQL(n *nullGuardNode) (string, error) {
	value, err := s.renderComparand(&n.value, placeholderTyped)
	if err != nil {
		return "", err
	}
	inner, _, err := s.generateSQLRecursive(n.expr)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("CASE WHEN %s IS NULL THEN NULL ELSE %s END", value, inner), nil
}

func (s *sqlGenerator) generateExistsSQL(n *existsNode) (string, error) {
	where, _, err := s.generateSQLRecursive(n.where)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("EXISTS (SELECT 1 FROM %s %s WHERE %s)", s.quoteIdentifier(n.table), s.quoteIdentifier(n.alias), where), nil
}

func (s *sqlGenerator) renderScalarSubquery(n *scalarSubqueryNode) (string, error) {
	where, _, err := s.generateSQLRecursive(n.where)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("(SELECT %s FROM %s %s WHERE %s)",
		s.renderColumnRef(columnRef{qualifier: n.alias, column: n.column}),
		s.quoteIdentifier(n.table), s.quoteIdentifier(n.alias), where), nil
}
