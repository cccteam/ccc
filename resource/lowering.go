package resource

import (
	"strconv"
	"strings"

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

	// newValueParams maps a touched column to the named parameter carrying
	// its proposed value — the post-write overlay: new.attr reads the
	// parameter where the mutation touches the column and the existing column
	// where it doesn't. Nil marks a read context, where new. cannot appear
	// (rejected upstream; an error here).
	newValueParams map[string]string
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
		return lowerComparison(n, ctx, registry)
	case condition.In:
		return lowerIn(n, ctx, registry)
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

func lowerComparison(cmp condition.Comparison, ctx *loweringContext, registry *paramRegistry) (ExpressionNode, error) {
	right, err := lowerOperand(cmp.Right, ctx, registry)
	if err != nil {
		return nil, err
	}
	op, err := sqlCompareOp(cmp.Op)
	if err != nil {
		return nil, err
	}

	if cmp.Left.IsNow() {
		return &loweredComparisonNode{left: namedComparand(nowParamName), op: op, right: right}, nil
	}

	target, wrap, err := ctx.resolveRef(cmp.Left, registry)
	if err != nil {
		return nil, err
	}

	return wrap(&loweredComparisonNode{left: target, op: op, right: right}), nil
}

func lowerIn(in condition.In, ctx *loweringContext, registry *paramRegistry) (ExpressionNode, error) {
	target, wrap, err := ctx.resolveRef(in.Left, registry)
	if err != nil {
		return nil, err
	}
	if target.kind != comparandColumn {
		return nil, errors.Newf("condition lowering: IN requires a column attribute, not the post-write overlay of an untouched context")
	}

	if in.SubjectSet != "" {
		exists, err := ctx.subjectSetExists(in.SubjectSet, target.column, registry)
		if err != nil {
			return nil, err
		}
		if in.Negated {
			return wrap(&notNode{expr: exists}), nil
		}

		return wrap(exists), nil
	}

	values := make([]any, 0, len(in.Literals))
	for _, literal := range in.Literals {
		values = append(values, literalValue(literal))
	}

	return wrap(&loweredInNode{column: target.column, negated: in.Negated, values: values}), nil
}

func lowerNullTest(test condition.NullTest, ctx *loweringContext, registry *paramRegistry) (ExpressionNode, error) {
	target, wrap, err := ctx.resolveRef(test.Left, registry)
	if err != nil {
		return nil, err
	}
	if target.kind != comparandColumn {
		return nil, errors.Newf("condition lowering: IS NULL requires a column attribute")
	}

	return wrap(&loweredNullTestNode{column: target.column, negated: test.Negated}), nil
}

func lowerOperand(operand condition.Operand, ctx *loweringContext, registry *paramRegistry) (comparand, error) {
	switch o := operand.(type) {
	case condition.StringLiteral:
		return valueComparand(o.Value), nil
	case condition.NumberLiteral:
		return valueComparand(numberValue(o.Text)), nil
	case condition.BoolLiteral:
		return valueComparand(o.Value), nil
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
	default:
		return comparand{}, errors.Newf("condition lowering: unsupported operand %T", operand)
	}
}

// resolveRef resolves an attribute reference to its comparand plus a wrap
// that encloses the leaf predicate in the reference's join-path EXISTS
// chain (identity for column bindings). A post-write reference resolves to
// the proposed value's parameter where the mutation touches the column, and
// to the existing column where it doesn't — the overlay semantics.
func (ctx *loweringContext) resolveRef(ref condition.Ref, registry *paramRegistry) (comparand, func(ExpressionNode) ExpressionNode, error) {
	identity := func(node ExpressionNode) ExpressionNode { return node }

	binding, ok := ctx.attribute(ref.Name)
	if !ok {
		return comparand{}, nil, errors.Newf("condition lowering: %q is not an attribute of the checked resource", ref.Name)
	}

	if ref.PostImage {
		if ctx.newValueParams == nil {
			return comparand{}, nil, errors.Newf("condition lowering: new.%s outside a write context", ref.Name)
		}
		if len(binding.Path) > 0 {
			return comparand{}, nil, errors.Newf("condition lowering: new.%s reads a join-path attribute, which has no proposed value", ref.Name)
		}
		if param, touched := ctx.newValueParams[binding.Column]; touched {
			return namedComparand(param), identity, nil
		}

		return columnComparand(ctx.outer, binding.Column), identity, nil
	}

	if len(binding.Path) == 0 {
		return columnComparand(ctx.outer, binding.Column), identity, nil
	}

	return ctx.pathTarget(ctx.outer, binding.Column, binding.Path, registry)
}

// pathTarget builds the EXISTS chain for a join path leaving through
// startColumn on the qualifier's row: the returned comparand is the terminal
// column inside the innermost EXISTS, and wrap encloses a leaf predicate in
// the chain.
func (ctx *loweringContext) pathTarget(qualifier, startColumn string, path []BindingHop, registry *paramRegistry) (comparand, func(ExpressionNode) ExpressionNode, error) {
	type frame struct {
		hop   BindingHop
		alias string
	}
	frames := make([]frame, 0, len(path))
	for _, hop := range path {
		frames = append(frames, frame{hop: hop, alias: registry.alias()})
	}

	terminal := frames[len(frames)-1]
	target := columnComparand(terminal.alias, terminal.hop.Column)

	wrap := func(leaf ExpressionNode) ExpressionNode {
		node := leaf
		for i := len(frames) - 1; i >= 0; i-- {
			f := frames[i]
			prevQualifier, prevColumn := qualifier, startColumn
			if i > 0 {
				prevQualifier, prevColumn = frames[i-1].alias, frames[i-1].hop.Column
			}
			join := &loweredComparisonNode{
				left:  columnComparand(f.alias, f.hop.JoinColumn),
				op:    "=",
				right: columnComparand(prevQualifier, prevColumn),
			}
			node = &existsNode{
				table: f.hop.Table,
				alias: f.alias,
				where: &LogicalOpNode{Left: join, Operator: OperatorAnd, Right: node},
			}
		}

		return node
	}

	return target, wrap, nil
}

// subjectSetExists renders `attr IN subject.<name>`: a correlated EXISTS over
// the anchor table matching the requester's rows whose value column equals
// the attribute — with the anchor's own tenancy filter when both the request
// and the anchor table are partitioned. An empty set matches nothing:
// fail-closed for free.
func (ctx *loweringContext) subjectSetExists(name string, attr columnRef, registry *paramRegistry) (ExpressionNode, error) {
	anchor, ok := ctx.collection.SubjectSet(name)
	if !ok {
		return nil, errors.Newf("condition lowering: subject.%s is not a declared subject set", name)
	}
	if len(anchor.Binding.Path) > 0 {
		return nil, errors.Newf("condition lowering: subject.%s: dotted value paths are not renderable yet", name)
	}

	alias := registry.alias()
	where := andChain(
		&loweredComparisonNode{
			left:  columnComparand(alias, anchor.Binding.UserColumn),
			op:    "=",
			right: namedComparand(subjectParamName),
		},
		&loweredComparisonNode{
			left:  columnComparand(alias, anchor.Binding.Column),
			op:    "=",
			right: comparand{kind: comparandColumn, column: attr},
		},
	)
	where, err := ctx.withAnchorTenancy(where, &anchor, alias, registry)
	if err != nil {
		return nil, err
	}

	return &existsNode{table: string(anchor.Resource), alias: alias, where: where}, nil
}

// subjectValueSubquery renders a scalar subject.<name>: one value off the
// anchor table's requester row. The anchor is unique-indexed by generation,
// so the database enforces at most one row; none yields NULL, which no
// condition finds TRUE.
func (ctx *loweringContext) subjectValueSubquery(name string, registry *paramRegistry) (*scalarSubqueryNode, error) {
	anchor, ok := ctx.collection.SubjectValue(name)
	if !ok {
		return nil, errors.Newf("condition lowering: subject.%s is not a declared subject value", name)
	}
	if len(anchor.Binding.Path) > 0 {
		return nil, errors.Newf("condition lowering: subject.%s: dotted value paths are not renderable yet", name)
	}

	alias := registry.alias()
	var where ExpressionNode = &loweredComparisonNode{
		left:  columnComparand(alias, anchor.Binding.UserColumn),
		op:    "=",
		right: namedComparand(subjectParamName),
	}
	where, err := ctx.withAnchorTenancy(where, &anchor, alias, registry)
	if err != nil {
		return nil, err
	}

	return &scalarSubqueryNode{table: string(anchor.Resource), alias: alias, column: anchor.Binding.Column, where: where}, nil
}

// withAnchorTenancy adds the anchor table's derived tenancy filter: applied
// exactly when the request is partitioned and the anchor table carries a
// domain binding — a membership in another tenant can never satisfy a
// condition in this one. A global request, or a global anchor table, adds
// nothing (§07's policy-shared pattern is deliberate).
func (ctx *loweringContext) withAnchorTenancy(where ExpressionNode, anchor *SubjectAnchor, alias string, registry *paramRegistry) (ExpressionNode, error) {
	if !ctx.partitioned || anchor.Domain == nil {
		return where, nil
	}

	var filter ExpressionNode
	if len(anchor.Domain.Path) == 0 {
		filter = &loweredComparisonNode{
			left:  columnComparand(alias, anchor.Domain.Column),
			op:    "=",
			right: namedComparand(domainParamName),
		}
	} else {
		target, wrap, err := ctx.pathTarget(alias, anchor.Domain.Column, anchor.Domain.Path, registry)
		if err != nil {
			return nil, err
		}
		filter = wrap(&loweredComparisonNode{left: target, op: "=", right: namedComparand(domainParamName)})
	}

	return andChain(where, filter), nil
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
		return "<=", nil
	case condition.Greater:
		return ">", nil
	case condition.GreaterEq:
		return ">=", nil
	default:
		return "", errors.Newf("condition lowering: unsupported operator %q", op)
	}
}

// numberValue types a verbatim numeric literal by shape: integers bind as
// int64, decimals as float64. Finer typing is the database's business — the
// one comparison engine — and MigrateRoles validates literals against
// attribute types at deploy.
func numberValue(text string) any {
	if !strings.Contains(text, ".") {
		if i, err := strconv.ParseInt(text, 10, 64); err == nil {
			return i
		}
	}
	f, _ := strconv.ParseFloat(text, 64)

	return f
}

// literalValue converts a condition literal to its bound Go value.
func literalValue(literal condition.Literal) any {
	switch l := literal.(type) {
	case condition.StringLiteral:
		return l.Value
	case condition.NumberLiteral:
		return numberValue(l.Text)
	case condition.BoolLiteral:
		return l.Value
	default:
		return nil
	}
}
