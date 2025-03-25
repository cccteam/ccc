package resource

import "fmt"

type PartialExpr struct {
	tree ExprTree
}

func NewPartialExpr(t ExprTree) PartialExpr {
	return PartialExpr{tree: t}
}

func (px PartialExpr) Group(x Expr) Expr {
	if px.tree == nil {
		root := x.tree
		root.SetGroup(true)

		return Expr{tree: root}
	}

	root := px.tree
	for root.Right() != nil {
		root = root.Right()
	}

	root.SetRight(x.tree)
	root.SetGroup(true)

	return Expr{tree: px.tree}
}

type Expr struct {
	tree ExprTree
}

func (x Expr) And() PartialExpr {
	root := newNode(and)

	// AND has a higher operator precedence than OR so we need to reorder the tree
	// https://www.postgresql.org/docs/current/sql-syntax-lexical.html#SQL-PRECEDENCE
	if !x.tree.IsGroup() && x.tree.Operator() == or {
		root.SetLeft(x.tree.Right())
		x.tree.SetRight(root)

		return PartialExpr{tree: x.tree}
	}

	root.SetLeft(x.tree)

	return PartialExpr{tree: root}
}

func (x Expr) Or() PartialExpr {
	root := newNode(or)
	root.SetLeft(x.tree)

	return PartialExpr{tree: root}
}

type Ident[T comparable] struct {
	column      string
	partialExpr PartialExpr
}

func NewIdent[T comparable](column string, px PartialExpr) Ident[T] {
	return Ident[T]{column, px}
}

func (i Ident[T]) Equal(v ...T) Expr {
	eqNode := &equalityNode[T]{
		node:   newNode(equal),
		column: i.column,
		values: v,
	}

	return Expr{tree: addNode(i.partialExpr.tree, eqNode)}
}

func (i Ident[T]) NotEqual(v ...T) Expr {
	neqNode := &equalityNode[T]{
		node:   newNode(notEqual),
		column: i.column,
		values: v,
	}

	return Expr{tree: addNode(i.partialExpr.tree, neqNode)}
}

func (i Ident[T]) GreaterThan(v T) Expr {
	gtNode := &compNode[T]{
		node:   newNode(greaterThan),
		column: i.column,
		value:  v,
	}

	return Expr{tree: addNode(i.partialExpr.tree, gtNode)}
}

func (i Ident[T]) GreaterThanEq(v T) Expr {
	gteqNode := &compNode[T]{
		node:   newNode(greaterThanEq),
		column: i.column,
		value:  v,
	}

	return Expr{tree: addNode(i.partialExpr.tree, gteqNode)}
}

func (i Ident[T]) LessThan(v T) Expr {
	ltNode := &compNode[T]{
		node:   newNode(lessThan),
		column: i.column,
		value:  v,
	}

	return Expr{tree: addNode(i.partialExpr.tree, ltNode)}
}

func (i Ident[T]) LessThanEq(v T) Expr {
	lteqNode := &compNode[T]{
		node:   newNode(lessThanEq),
		column: i.column,
		value:  v,
	}

	return Expr{tree: addNode(i.partialExpr.tree, lteqNode)}
}

type nodeType string

const (
	logical     nodeType = "LOGICAL"
	conditional          = "CONDITIONAL"
)

type operator string

const (
	and           operator = "AND"
	or                     = "OR"
	equal                  = "EQUAL"
	notEqual               = "NOTEQUAL"
	greaterThan            = "GREATERTHAN"
	greaterThanEq          = "GREATERTHANEQ"
	lessThan               = "LESSTHAN"
	lessThanEq             = "LESSTHANEQ"
)

type ExprTree interface {
	Type() nodeType
	Operator() operator
	Left() ExprTree
	Right() ExprTree
	SetLeft(ExprTree)
	SetRight(ExprTree)
	IsGroup() bool
	SetGroup(bool)
}

type node struct {
	left    ExprTree
	right   ExprTree
	op      operator
	isGroup bool
}

func newNode(op operator) *node {
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
		return conditional
	}
}

func (n *node) Operator() operator {
	return n.op
}

func (n *node) Left() ExprTree {
	return n.left
}

func (n *node) Right() ExprTree {
	return n.right
}

func (n *node) SetLeft(newNode ExprTree) {
	n.left = newNode
}

func (n *node) SetRight(newNode ExprTree) {
	n.right = newNode
}

func (n *node) IsGroup() bool {
	return n.isGroup
}

func (n *node) SetGroup(b bool) {
	n.isGroup = b
}

func addNode(tree ExprTree, n ExprTree) ExprTree {
	if tree == nil {
		return n
	}

	root := tree
	for root.Right() != nil {
		root = root.Right()
	}
	root.SetRight(n)

	return tree
}

type equalityNode[T comparable] struct {
	*node
	column string
	values []T
}

func (e *equalityNode[T]) Operator() operator {
	op := e.column + "." + string(e.node.op) + "["
	for i := range e.values {
		if i == 0 {
			op += fmt.Sprintf("%v", e.values[i])
			continue
		}
		op += fmt.Sprintf(", %v", e.values[i])
	}
	op += "]"

	return operator(op)
}

type compNode[T comparable] struct {
	*node
	column string
	value  T
}

func (e *compNode[T]) Operator() operator {
	return operator(e.column + "." + string(e.node.op) + "[" + fmt.Sprintf("%v", e.value) + "]")
}
