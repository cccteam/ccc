package resource

import (
	"fmt"
	"strings"
)

type PartialQueryClause struct {
	tree whereClauseExprTree
}

func NewPartialQueryClause() PartialQueryClause {
	return PartialQueryClause{tree: nil}
}

func (p PartialQueryClause) Group(qc QueryClause) QueryClause {
	qc.tree.SetGroup(true)

	root := p.tree
	if root == nil {
		root = qc.tree
	} else {
		root.SetRight(qc.tree)
	}

	return QueryClause{tree: root}
}

type QueryClause struct {
	tree whereClauseExprTree
}

func (x QueryClause) And() PartialQueryClause {
	root := newNode(and)
	root.SetLeft(x.tree)

	return PartialQueryClause{tree: root}
}

func (x QueryClause) Or() PartialQueryClause {
	root := newNode(or)
	root.SetLeft(x.tree)

	return PartialQueryClause{tree: root}
}

type Ident[T comparable] struct {
	column      string
	partialExpr PartialQueryClause
}

func NewIdent[T comparable](column string, px PartialQueryClause) Ident[T] {
	return Ident[T]{column, px}
}

func (i Ident[T]) Equal(v ...T) QueryClause {
	eqNode := &equalityNode[T]{
		node:   newNode(equal),
		column: i.column,
		values: v,
	}

	return QueryClause{tree: addNode(i.partialExpr.tree, eqNode)}
}

func (i Ident[T]) NotEqual(v ...T) QueryClause {
	neqNode := &equalityNode[T]{
		node:   newNode(notEqual),
		column: i.column,
		values: v,
	}

	return QueryClause{tree: addNode(i.partialExpr.tree, neqNode)}
}

func (i Ident[T]) GreaterThan(v T) QueryClause {
	gtNode := &compNode[T]{
		node:   newNode(greaterThan),
		column: i.column,
		value:  v,
	}

	return QueryClause{tree: addNode(i.partialExpr.tree, gtNode)}
}

func (i Ident[T]) GreaterThanEq(v T) QueryClause {
	gteqNode := &compNode[T]{
		node:   newNode(greaterThanEq),
		column: i.column,
		value:  v,
	}

	return QueryClause{tree: addNode(i.partialExpr.tree, gteqNode)}
}

func (i Ident[T]) LessThan(v T) QueryClause {
	ltNode := &compNode[T]{
		node:   newNode(lessThan),
		column: i.column,
		value:  v,
	}

	return QueryClause{tree: addNode(i.partialExpr.tree, ltNode)}
}

func (i Ident[T]) LessThanEq(v T) QueryClause {
	lteqNode := &compNode[T]{
		node:   newNode(lessThanEq),
		column: i.column,
		value:  v,
	}

	return QueryClause{tree: addNode(i.partialExpr.tree, lteqNode)}
}

type nodeType string

const (
	logical    nodeType = "LOGICAL"
	comparison          = "COMPARISON"
)

type action string

const (
	and           action = "AND"
	or                   = "OR"
	equal                = "equal"
	notEqual             = "NOTEQUAL"
	greaterThan          = "GREATERTHAN"
	greaterThanEq        = "GREATERTHANEQ"
	lessThan             = "LESSTHAN"
	lessThanEq           = "LESSTHANEQ"
)

type whereClauseExprTree interface {
	Type() nodeType
	Action() action
	Operator() string
	LeftOperand() string
	RightOperands() []any
	Left() whereClauseExprTree
	Right() whereClauseExprTree
	SetLeft(whereClauseExprTree)
	SetRight(whereClauseExprTree)
	IsGroup() bool
	SetGroup(bool)
}

type node struct {
	left    whereClauseExprTree
	right   whereClauseExprTree
	op      action
	isGroup bool
}

func newNode(op action) *node {
	return &node{
		left:  nil,
		right: nil,
		op:    op,
	}
}

func (n *node) Type() nodeType {
	switch n.op {
	case and, or:
		return logical
	default:
		return comparison
	}
}

func (n *node) Action() action {
	return n.op
}

func (n *node) Operator() string {
	return string(n.Action())
}

func (n *node) LeftOperand() string {
	switch n.Type() {
	case logical:
		return ""
	default:
		panic(fmt.Sprintf("non-logical node type %s must implement LeftOperand()", n.Type()))
	}
}

func (n *node) RightOperands() []any {
	switch n.Type() {
	case logical:
		return []any{}
	default:
		panic(fmt.Sprintf("non-logical node type %s must implement RightOperands()", n.Type()))
	}
}

func (n *node) Left() whereClauseExprTree {
	return n.left
}

func (n *node) Right() whereClauseExprTree {
	return n.right
}

func (n *node) SetLeft(newNode whereClauseExprTree) {
	n.left = newNode
}

func (n *node) SetRight(newNode whereClauseExprTree) {
	n.right = newNode
}

func (n *node) IsGroup() bool {
	return n.isGroup
}

func (n *node) SetGroup(b bool) {
	n.isGroup = b
}

func addNode(tree whereClauseExprTree, n whereClauseExprTree) whereClauseExprTree {
	if tree == nil {
		return n
	}

	root := tree
	root.SetRight(n)

	return tree
}

type equalityNode[T comparable] struct {
	*node
	column string
	values []T
}

func (n *equalityNode[T]) LeftOperand() string {
	return n.column
}

func (n *equalityNode[T]) RightOperands() []any {
	v := make([]any, len(n.values))
	for i := range n.values {
		v[i] = n.values[i]
	}

	return v
}

func (n *equalityNode[T]) Operator() string {
	switch {
	case n.node.op == equal && len(n.values) == 1:
		return "="
	case n.node.op == equal && len(n.values) > 1:
		return "IN"
	case n.node.op == notEqual && len(n.values) == 1:
		return "!="
	case n.node.op == notEqual && len(n.values) > 1:
		return "NOT IN"
	default:
		panic("unreachable: invalid state for equalityNode")
	}
}

type compNode[T comparable] struct {
	*node
	column string
	value  T
}

func (n *compNode[T]) Operator() string {
	switch n.node.op {
	case greaterThan:
		return ">"
	case greaterThanEq:
		return ">="
	case lessThan:
		return "<"
	case lessThanEq:
		return "<="
	default:
		panic("unreachable: invalid state for equalityNode")
	}
}

func (n *compNode[T]) LeftOperand() string {
	return n.column
}

func (n *compNode[T]) RightOperands() []any {
	return []any{n.value}
}

// treeWalker tracks the number of values from visited nodes in the
// query clause expression tree. This enables uniquely identifying parameters
// which must conform to naming requirements in https://cloud.google.com/spanner/docs/reference/standard-sql/lexical#identifiers
type treeWalker struct {
	accumulator map[action]int
	params      map[string]any
}

func newTreeWalker() treeWalker {
	return treeWalker{
		accumulator: map[action]int{
			equal:         0,
			notEqual:      0,
			greaterThan:   0,
			greaterThanEq: 0,
			lessThan:      0,
			lessThanEq:    0,
		},

		params: make(map[string]any),
	}
}

func (t *treeWalker) walk(root whereClauseExprTree) string {
	if root == nil {
		return ""
	}

	b := strings.Builder{}

	if root.IsGroup() {
		b.WriteString("(")
	}

	if root.Left() != nil {
		b.WriteString(fmt.Sprintf("%s ", t.walk(root.Left())))
	}

	b.WriteString(t.visit(root))

	if root.Right() != nil {
		b.WriteString(fmt.Sprintf(" %s", t.walk(root.Right())))
	}

	if root.IsGroup() {
		b.WriteString(")")
	}

	return b.String()
}

func (t *treeWalker) visit(node whereClauseExprTree) string {
	b := strings.Builder{}

	switch node.Type() {
	case comparison:
		b.WriteString(fmt.Sprintf("%s %s ", node.LeftOperand(), node.Operator()))

		values := node.RightOperands()
		if len(values) > 1 {
			b.WriteString("(")
		}

		b.WriteString(fmt.Sprintf("%s", t.newParam(values[0], node.Action())))

		if len(values) > 1 {
			for _, v := range values[1:] {
				b.WriteString(fmt.Sprintf(", %s", t.newParam(v, node.Action())))
			}

			b.WriteString(")")
		}
	case logical:
		b.WriteString(fmt.Sprintf("%s", node.Operator()))
	}

	return b.String()
}

func (t *treeWalker) newParam(v any, a action) string {
	s := fmt.Sprintf("@%s%d", a, t.accumulator[a])
	t.accumulator[a] += 1
	t.params[s] = v

	return s
}
