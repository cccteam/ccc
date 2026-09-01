package resource

import (
	"fmt"
	"slices"
	"strings"

	"github.com/go-playground/errors/v5"
)

// This file extends the module's single SQL emitter with the node shapes the
// condition lowering produces (ABAC design plan §05): NOT, EXISTS,
// named-parameter and column comparands, scalar subqueries, and a
// statement-scoped parameter registry so every fragment of one statement —
// row-visibility WHERE, per-column CASE, check-SELECT booleans — allocates
// from one namespace. The new node types are unexported and constructor-fed
// by the lowering alone, so filter-reachable paths can never produce them,
// and they render only under a registry.

// SQL relational operator spellings shared by the filter path and the
// condition lowering.
const (
	sqlLessEq    = "<="
	sqlGreaterEq = ">="
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
type paramRegistry struct {
	paramCount int
	aliasCount int
	params     []QueryParam
	named      map[string]struct{}
}

func newParamRegistry() *paramRegistry {
	return &paramRegistry{named: make(map[string]struct{})}
}

// bind allocates a placeholder for value and returns it, @-prefixed.
func (r *paramRegistry) bind(value any) string {
	r.paramCount++
	name := fmt.Sprintf("_c%d", r.paramCount)
	r.params = append(r.params, QueryParam{Name: name, Value: value})

	return "@" + name
}

// reference records the statement's use of a fixed named parameter and
// returns it, @-prefixed.
func (r *paramRegistry) reference(name string) string {
	r.named[name] = struct{}{}

	return "@" + name
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

// truthNode is a constant boolean predicate; residual trees are usually
// fact-folded before lowering, so it renders only defensively.
type truthNode struct {
	value bool
}

func (n *truthNode) String() string {
	if n.value {
		return "TRUE"
	}

	return "FALSE"
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
	left, err := s.renderComparand(&n.left)
	if err != nil {
		return "", err
	}
	right, err := s.renderComparand(&n.right)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("%s %s %s", left, n.op, right), nil
}

func (s *sqlGenerator) renderComparand(c *comparand) (string, error) {
	switch c.kind {
	case comparandColumn:
		return s.renderColumnRef(c.column), nil
	case comparandValue:
		return s.registry.bind(c.value), nil
	case comparandNamed:
		return s.registry.reference(c.named), nil
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
	left, err := s.renderComparand(&n.left)
	if err != nil {
		return "", err
	}
	placeholders := make([]string, 0, len(n.values))
	for _, v := range n.values {
		placeholders = append(placeholders, s.registry.bind(v))
	}
	op := "IN"
	if n.negated {
		op = "NOT IN"
	}

	return fmt.Sprintf("%s %s (%s)", left, op, strings.Join(placeholders, ", ")), nil
}

func (s *sqlGenerator) generateLoweredNullTestSQL(n *loweredNullTestNode) (string, error) {
	left, err := s.renderComparand(&n.left)
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
