package resource

import (
	"math/big"
	"strconv"
	"strings"
	"time"

	"cloud.google.com/go/civil"
	"github.com/cccteam/ccc/accesstypes/condition"
	"github.com/go-playground/errors/v5"
)

// The lowering pass (ABAC design plan §05): the translation from the
// condition vocabulary AST — binding names, subject vocabulary, facts — onto
// the module's ExpressionNode pipeline, which stays the single SQL emitter.
// The resource layer renders and never evaluates: there is no Go-side
// comparison of app-typed values here; literals bind as parameters and the
// database is the one comparison engine.
//
// Facts still present in a residual tree bind as the reserved named
// parameters (@subject, @now, @domain) — the identical values the check
// folded with, supplied by the statement builder — never re-sampled, never
// the database's CURRENT_TIMESTAMP.

// loweringContext carries what one condition's lowering resolves against.
type loweringContext struct {
	// outer qualifies the checked resource's columns; empty renders bare
	// column names.
	outer string

	// bindings is the checked resource's vocabulary: every binding name the
	// condition references must resolve here.
	bindings Bindings

	// collection resolves the subject vocabulary, declared on anchor tables
	// elsewhere in the application.
	collection *GeneratedCollection

	// partitioned marks a request in a tenant partition: subject subqueries
	// over domain-scoped anchor tables filter to @domain (derived, never
	// authored); a global request has no partition to filter by.
	partitioned bool

	// proposed carries the touched columns' proposed values — the post-write
	// overlay: new.attr reads the proposed value where the mutation touches
	// the column and the existing column where it doesn't. Values bind as
	// parameters lazily, on first reference. Nil marks a read context, where
	// new. cannot appear (rejected upstream; an error here).
	proposed *proposedOverlay

	// insertImage marks an insert's check context: there is one image,
	// written unqualified, and no existing row — every local column resolves
	// to its proposed parameter, and referencing a column the insert does not
	// set is an error (rule validation rejects those conditions at deploy;
	// this is the runtime backstop).
	insertImage bool
}

// lowerCondition translates a compiled condition onto the ExpressionNode
// pipeline, allocating table aliases from the statement registry so several
// lowered fragments coexist in one statement.
func lowerCondition(expr condition.Expr, ctx *loweringContext, registry *paramRegistry) (ExpressionNode, error) {
	switch n := expr.(type) {
	case condition.And:
		return lowerLogicChain(n.Operands, OperatorAnd, ctx, registry)
	case condition.Or:
		return lowerLogicChain(n.Operands, OperatorOr, ctx, registry)
	case condition.Not:
		inner, err := lowerCondition(n.Operand, ctx, registry)
		if err != nil {
			return nil, err
		}

		return &notNode{expr: inner}, nil
	case condition.Truth:
		return &truthNode{value: n.Value}, nil
	case condition.Comparison:
		return lowerComparison(&n, ctx, registry)
	case condition.In:
		return lowerIn(&n, ctx, registry)
	case condition.NullTest:
		return lowerNullTest(n, ctx, registry)
	default:
		return nil, errors.Newf("condition lowering: unsupported expression node %T", expr)
	}
}

func lowerLogicChain(operands []condition.Expr, op LogicalOperator, ctx *loweringContext, registry *paramRegistry) (ExpressionNode, error) {
	lowered := make([]ExpressionNode, 0, len(operands))
	for _, operand := range operands {
		node, err := lowerCondition(operand, ctx, registry)
		if err != nil {
			return nil, err
		}
		lowered = append(lowered, node)
	}

	chain := lowered[0]
	for _, next := range lowered[1:] {
		chain = &LogicalOpNode{Left: chain, Operator: op, Right: next}
	}

	return &GroupNode{Expression: chain}, nil
}

func lowerComparison(cmp *condition.Comparison, ctx *loweringContext, registry *paramRegistry) (ExpressionNode, error) {
	op, err := sqlCompareOp(cmp.Op)
	if err != nil {
		return nil, err
	}

	if cmp.Left.IsNow() {
		right, err := lowerOperand(cmp.Right, AttributeTypeTimestamp, ctx, registry)
		if err != nil {
			return nil, err
		}

		return &loweredComparisonNode{left: namedComparand(nowParamName), op: op, right: right}, nil
	}

	target, err := ctx.resolveRef(cmp.Left, registry)
	if err != nil {
		return nil, err
	}
	binding, _ := ctx.attribute(cmp.Left.Name)
	right, err := lowerOperand(cmp.Right, binding.Type, ctx, registry)
	if err != nil {
		return nil, err
	}

	return &loweredComparisonNode{left: target, op: op, right: right}, nil
}

func lowerIn(in *condition.In, ctx *loweringContext, registry *paramRegistry) (ExpressionNode, error) {
	target, err := ctx.resolveRef(in.Left, registry)
	if err != nil {
		return nil, err
	}
	if target.kind == comparandValue {
		return nil, errors.Newf("condition lowering: IN requires an attribute value")
	}

	if in.SubjectSet != "" {
		exists, err := ctx.subjectSetExists(in.SubjectSet, &target, registry)
		if err != nil {
			return nil, err
		}
		membership := exists
		if in.Negated {
			membership = &notNode{expr: exists}
		}

		// An EXISTS is never UNKNOWN, but a membership test against no value
		// is (design plan §05): the attribute's nullness decides before the
		// anchor is consulted, so NOT IN over a NULL attribute cannot permit.
		return &nullGuardNode{value: target, expr: membership}, nil
	}

	binding, _ := ctx.attribute(in.Left.Name)
	values := make([]any, 0, len(in.Literals))
	for _, literal := range in.Literals {
		value, err := literalValue(literal, binding.Type)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}

	return &loweredInNode{left: target, negated: in.Negated, values: values}, nil
}

func lowerNullTest(test condition.NullTest, ctx *loweringContext, registry *paramRegistry) (ExpressionNode, error) {
	target, err := ctx.resolveRef(test.Left, registry)
	if err != nil {
		return nil, err
	}
	if target.kind == comparandValue {
		return nil, errors.Newf("condition lowering: IS NULL requires an attribute value")
	}

	return &loweredNullTestNode{left: target, negated: test.Negated}, nil
}

// lowerOperand lowers a comparison's right side; typ is the left side's
// comparison type, which a literal binds as (see literalValue).
func lowerOperand(operand condition.Operand, typ AttributeType, ctx *loweringContext, registry *paramRegistry) (comparand, error) {
	switch o := operand.(type) {
	case condition.Literal:
		value, err := literalValue(o, typ)
		if err != nil {
			return comparand{}, err
		}

		return valueComparand(value), nil
	case condition.Subject:
		return namedComparand(subjectParamName), nil
	case condition.Now:
		return namedComparand(nowParamName), nil
	case condition.SubjectValue:
		subquery, err := ctx.subjectValueSubquery(o.Name, registry)
		if err != nil {
			return comparand{}, err
		}

		return subqueryComparand(subquery), nil
	case condition.Ref:
		// The old-vs-new form's right side: the same row's pre-image
		// attribute (always unqualified — the parser is the gate). Deploy
		// validation restricts it to column attributes; a join path renders
		// as EXISTS, which has no scalar to compare against.
		binding, ok := ctx.attribute(o.Name)
		if !ok {
			return comparand{}, errors.Newf("condition lowering: %q is not an attribute of the checked resource", o.Name)
		}
		if len(binding.Path) > 0 {
			return comparand{}, errors.Newf("condition lowering: %s is a join-path attribute and cannot stand on the right side of an old-vs-new comparison", o.Name)
		}
		target, err := ctx.resolveRef(o, registry)
		if err != nil {
			return comparand{}, err
		}

		return target, nil
	default:
		return comparand{}, errors.Newf("condition lowering: unsupported operand %T", operand)
	}
}

// resolveRef resolves an attribute reference to its comparand. A column
// binding is the checked row's column; a join-path binding is a scalar
// subquery per hop that reads the related row's column, or NULL where the
// departure key is NULL, a hop reaches no row, or the terminal column is
// NULL — the attribute is then "no value", and every comparison, membership
// test, and null test over it takes SQL's own three-valued reading (design
// plan §05). Generation validates each hop many-to-one, so a hop yields at
// most one row. A post-write reference resolves to the proposed value's
// parameter where the mutation touches the column, and to the existing
// column where it doesn't — the overlay semantics. In an insert's check
// context there is one image, written unqualified: every local column is
// its proposed parameter, and a join path leaves through the proposed
// foreign-key value.
func (ctx *loweringContext) resolveRef(ref condition.Ref, registry *paramRegistry) (comparand, error) {
	if ref.IsTemporal() {
		// Temporal terms are environment facts: the engine folds them at check
		// time, so a decision's residue never carries one — SQL never renders
		// timezone arithmetic (design plan §05). Reaching this is an invariant
		// breach, never a rendering request.
		return comparand{}, errors.Newf("condition lowering: invariant breach: %s reached SQL rendering — temporal terms fold at check time", ref.String())
	}

	binding, ok := ctx.attribute(ref.Name)
	if !ok {
		return comparand{}, errors.Newf("condition lowering: %q is not an attribute of the checked resource", ref.Name)
	}

	if ctx.insertImage {
		start, err := ctx.proposedValue(ref.Name, binding.Column, registry)
		if err != nil {
			return comparand{}, err
		}
		if len(binding.Path) == 0 {
			return start, nil
		}

		return pathScalar(&start, binding.Path, registry), nil
	}

	if ref.PostImage {
		if ctx.proposed == nil {
			return comparand{}, errors.Newf("condition lowering: new.%s outside a write context", ref.Name)
		}
		if len(binding.Path) > 0 {
			return comparand{}, errors.Newf("condition lowering: new.%s reads a join-path attribute, which has no proposed value", ref.Name)
		}
		if param, touched := ctx.proposed.param(binding.Column, registry); touched {
			return namedComparand(param), nil
		}

		return columnComparand(ctx.outer, binding.Column), nil
	}

	outerColumn := columnComparand(ctx.outer, binding.Column)
	if len(binding.Path) == 0 {
		return outerColumn, nil
	}

	return pathScalar(&outerColumn, binding.Path, registry), nil
}

// pathScalar renders a join path as the value it reaches: one scalar subquery
// per hop, each selecting its column from the row whose join column matches
// the previous hop's value, leaving through start — the departure column on
// the checked row, or its proposed value in an insert's check context. Where
// a key is NULL or no row matches, the subquery is NULL: the attribute reads
// as no value.
func pathScalar(start *comparand, path []BindingHop, registry *paramRegistry) comparand {
	previous := *start
	var subquery *scalarSubqueryNode
	for _, hop := range path {
		alias := registry.alias()
		subquery = &scalarSubqueryNode{
			table:  hop.Table,
			alias:  alias,
			column: hop.Column,
			where: &loweredComparisonNode{
				left:  columnComparand(alias, hop.JoinColumn),
				op:    "=",
				right: previous,
			},
		}
		previous = subqueryComparand(subquery)
	}

	return previous
}

// proposedValue resolves a column to its proposed parameter in an insert's
// check context; a column the insert does not set has no value to evaluate.
func (ctx *loweringContext) proposedValue(name, column string, registry *paramRegistry) (comparand, error) {
	param, touched := ctx.proposed.param(column, registry)
	if !touched {
		return comparand{}, errors.Newf("condition lowering: %s references column %s, which the insert does not set", name, column)
	}

	return namedComparand(param), nil
}

// proposedOverlay lazily binds a mutation's proposed values: a column binds
// once, on its first reference, so untouched conditions add no parameters.
type proposedOverlay struct {
	values map[string]any
	params map[string]string
}

func newProposedOverlay(values map[string]any) *proposedOverlay {
	return &proposedOverlay{values: values, params: make(map[string]string, len(values))}
}

// param returns the column's proposed-value parameter, binding it on first
// use; false means the mutation does not touch the column.
func (o *proposedOverlay) param(column string, registry *paramRegistry) (string, bool) {
	if param, ok := o.params[column]; ok {
		return param, true
	}
	value, ok := o.values[column]
	if !ok {
		return "", false
	}
	param := strings.TrimPrefix(registry.bind(value), "@")
	o.params[column] = param

	return param, true
}

// pathTarget builds the EXISTS chain for a join path leaving through start:
// the shape the structural predicates take — a subject set's dotted value
// matched inside the anchor's EXISTS, an anchor's dotted tenancy filter, and
// a partitioned insert's tenancy proof — where a NULL key rightly matches
// nothing. The returned comparand is the terminal column inside the innermost
// EXISTS, and wrap encloses a leaf predicate in the chain. Attributes render
// through pathScalar instead, so a missing value stays NULL.
func (ctx *loweringContext) pathTarget(start *comparand, path []BindingHop, registry *paramRegistry) (target comparand, wrap func(ExpressionNode) ExpressionNode) {
	type frame struct {
		hop   BindingHop
		alias string
	}
	frames := make([]frame, 0, len(path))
	for _, hop := range path {
		frames = append(frames, frame{hop: hop, alias: registry.alias()})
	}

	terminal := frames[len(frames)-1]
	target = columnComparand(terminal.alias, terminal.hop.Column)

	wrap = func(leaf ExpressionNode) ExpressionNode {
		node := leaf
		for i := len(frames) - 1; i >= 0; i-- {
			f := frames[i]
			prev := *start
			if i > 0 {
				prev = columnComparand(frames[i-1].alias, frames[i-1].hop.Column)
			}
			join := &loweredComparisonNode{
				left:  columnComparand(f.alias, f.hop.JoinColumn),
				op:    "=",
				right: prev,
			}
			node = &existsNode{
				table: f.hop.Table,
				alias: f.alias,
				where: &LogicalOpNode{Left: join, Operator: OperatorAnd, Right: node},
			}
		}

		return node
	}

	return target, wrap
}

// subjectSetExists renders `attr IN subject.<name>`: a correlated EXISTS over
// the anchor table matching the requester's rows whose value equals the
// attribute — with the anchor's own tenancy filter when both the request
// and the anchor table are partitioned. A dotted value (`value: F.G`)
// continues from the anchor row through its join path, so the equality
// lands on the terminal column inside the nested EXISTS chain, the same
// shape a join-path attribute renders. An empty set matches nothing:
// fail-closed for free.
func (ctx *loweringContext) subjectSetExists(name string, attr *comparand, registry *paramRegistry) (ExpressionNode, error) {
	anchor, ok := ctx.collection.SubjectSet(name)
	if !ok {
		return nil, errors.Newf("condition lowering: subject.%s is not a declared subject set", name)
	}

	alias := registry.alias()
	requester := &loweredComparisonNode{
		left:  columnComparand(alias, anchor.Binding.UserColumn),
		op:    "=",
		right: namedComparand(subjectParamName),
	}

	var match ExpressionNode
	if len(anchor.Binding.Path) == 0 {
		match = &loweredComparisonNode{left: columnComparand(alias, anchor.Binding.Column), op: "=", right: *attr}
	} else {
		anchorColumn := columnComparand(alias, anchor.Binding.Column)
		target, wrap := ctx.pathTarget(&anchorColumn, anchor.Binding.Path, registry)
		match = wrap(&loweredComparisonNode{left: target, op: "=", right: *attr})
	}

	where := ctx.withAnchorTenancy(andChain(requester, match), &anchor, alias, registry)

	return &existsNode{table: string(anchor.Resource), alias: alias, where: where}, nil
}

// subjectValueSubquery renders a scalar subject.<name>: one value off the
// anchor table's requester row. The anchor is unique-indexed by generation,
// so the database enforces at most one row; none yields NULL, which no
// condition finds TRUE. A dotted value (`value: F.G`) nests one scalar
// subquery per hop: each hop selects its column from the row its join
// column matches in the previous hop's value, and every hop resolves
// many-to-one by generation, so each level yields at most one row.
func (ctx *loweringContext) subjectValueSubquery(name string, registry *paramRegistry) (*scalarSubqueryNode, error) {
	anchor, ok := ctx.collection.SubjectValue(name)
	if !ok {
		return nil, errors.Newf("condition lowering: subject.%s is not a declared subject value", name)
	}

	alias := registry.alias()
	var where ExpressionNode = &loweredComparisonNode{
		left:  columnComparand(alias, anchor.Binding.UserColumn),
		op:    "=",
		right: namedComparand(subjectParamName),
	}
	where = ctx.withAnchorTenancy(where, &anchor, alias, registry)

	subquery := &scalarSubqueryNode{table: string(anchor.Resource), alias: alias, column: anchor.Binding.Column, where: where}
	for _, hop := range anchor.Binding.Path {
		hopAlias := registry.alias()
		subquery = &scalarSubqueryNode{
			table:  hop.Table,
			alias:  hopAlias,
			column: hop.Column,
			where: &loweredComparisonNode{
				left:  columnComparand(hopAlias, hop.JoinColumn),
				op:    "=",
				right: subqueryComparand(subquery),
			},
		}
	}

	return subquery, nil
}

// withAnchorTenancy adds the anchor table's derived tenancy filter: applied
// exactly when the request is partitioned and the anchor table carries a
// domain binding — a membership in another tenant can never satisfy a
// condition in this one. A global request, or a global anchor table, adds
// nothing (§07's policy-shared pattern is deliberate).
func (ctx *loweringContext) withAnchorTenancy(where ExpressionNode, anchor *SubjectAnchor, alias string, registry *paramRegistry) ExpressionNode {
	if !ctx.partitioned || anchor.Domain == nil {
		return where
	}

	var filter ExpressionNode
	if len(anchor.Domain.Path) == 0 {
		filter = &loweredComparisonNode{
			left:  columnComparand(alias, anchor.Domain.Column),
			op:    "=",
			right: namedComparand(domainParamName),
		}
	} else {
		anchorColumn := columnComparand(alias, anchor.Domain.Column)
		target, wrap := ctx.pathTarget(&anchorColumn, anchor.Domain.Path, registry)
		filter = wrap(&loweredComparisonNode{left: target, op: "=", right: namedComparand(domainParamName)})
	}

	return andChain(where, filter)
}

// attribute resolves a binding name on the checked resource.
func (ctx *loweringContext) attribute(name string) (AttributeData, bool) {
	for _, attr := range ctx.bindings.Attributes {
		if attr.Name == name {
			return attr, true
		}
	}

	return AttributeData{}, false
}

func andChain(nodes ...ExpressionNode) ExpressionNode {
	chain := nodes[0]
	for _, next := range nodes[1:] {
		chain = &LogicalOpNode{Left: chain, Operator: OperatorAnd, Right: next}
	}

	return chain
}

// sqlCompareOp maps the condition language's operators onto SQL; != is the
// language's only not-equal spelling, and the emitter writes <>.
func sqlCompareOp(op condition.CompareOp) (string, error) {
	switch op {
	case condition.Eq:
		return "=", nil
	case condition.NotEq:
		return "<>", nil
	case condition.Less:
		return "<", nil
	case condition.LessEq:
		return sqlLessEq, nil
	case condition.Greater:
		return ">", nil
	case condition.GreaterEq:
		return sqlGreaterEq, nil
	default:
		return "", errors.Newf("condition lowering: unsupported operator %q", op)
	}
}

// numberValue types a verbatim numeric literal by shape: an integer binds as
// INT64, a decimal as NUMERIC (*big.Rat), the exact value the text spells.
// The database widens each to the attribute's storage type — exact against
// INT64 and NUMERIC, the nearest double against FLOAT64 (design plan §05) —
// so a NUMERIC column is never compared through a double. MigrateRoles
// validates literals against attribute types at deploy.
func numberValue(text string) any {
	if !strings.Contains(text, ".") {
		if i, err := strconv.ParseInt(text, 10, 64); err == nil {
			return i
		}
	}
	if r, ok := new(big.Rat).SetString(text); ok {
		return r
	}
	f, _ := strconv.ParseFloat(text, 64)

	return f
}

// literalValue converts a condition literal to the Go value it binds as,
// typed by the attribute it compares against (design plan §05: a literal
// takes the attribute's type). A string literal against a timestamp
// attribute binds as the instant it spells and against a date attribute as
// the date, so the parameter carries the column's own type: the database
// coerces a STRING parameter beside a column, but not beside another
// parameter — the proposed value in an insert or update check — where the
// comparison would be refused. MigrateRoles validates the literal's form at
// deploy; a malformed one here is the runtime backstop.
func literalValue(literal condition.Literal, typ AttributeType) (any, error) {
	switch l := literal.(type) {
	case condition.StringLiteral:
		switch typ {
		case AttributeTypeTimestamp:
			instant, err := time.Parse(time.RFC3339, l.Value)
			if err != nil {
				return nil, errors.Newf("condition lowering: %q is not an RFC 3339 timestamp", l.Value)
			}

			return instant.UTC(), nil
		case AttributeTypeDate:
			date, err := civil.ParseDate(l.Value)
			if err != nil {
				return nil, errors.Newf("condition lowering: %q is not a YYYY-MM-DD date", l.Value)
			}

			return date, nil
		default:
			return l.Value, nil
		}
	case condition.NumberLiteral:
		return numberValue(l.Text), nil
	case condition.BoolLiteral:
		return l.Value, nil
	default:
		return nil, errors.Newf("condition lowering: unsupported literal %T", literal)
	}
}
