package resource

import "fmt"

type partialExpr struct {
	tree Node
}

func (px partialExpr) Group(x expr) expr {
	if px.tree == nil {
		root := x.tree
		root.SetGroup(true)

		return expr{
			tree: root,
		}
	}

	root := px.tree
	for root.Right() != nil {
		root = root.Right()
	}

	root.SetRight(x.tree)
	root.SetGroup(true)

	return expr{
		tree: px.tree,
	}
}

type expr struct {
	tree Node
}

func (x expr) And() partialExpr {
	root := newNode(and)

	// AND has a higher operator precedence than OR so we need to reorder the tree
	// https://www.postgresql.org/docs/current/sql-syntax-lexical.html#SQL-PRECEDENCE
	if !x.tree.IsGroup() && x.tree.Operator() == or {
		root.SetLeft(x.tree.Right())
		x.tree.SetRight(root)

		return partialExpr{
			tree: x.tree,
		}
	}

	root.SetLeft(x.tree)

	return partialExpr{
		tree: root,
	}
}

func (x expr) Or() partialExpr {
	root := newNode(or)
	root.SetLeft(x.tree)

	return partialExpr{
		tree: root,
	}
}

type ident[T comparable] struct {
	column      string
	partialExpr partialExpr
}

func (i ident[T]) Equal(v ...T) expr {
	eqNode := &equalityNode[T]{
		node:   newNode(equal),
		column: i.column,
		values: v,
	}

	return expr{
		tree: addNode(i.partialExpr.tree, eqNode),
	}
}

func (i ident[T]) NotEqual(v ...T) expr {
	neqNode := &equalityNode[T]{
		node:   newNode(notEqual),
		column: i.column,
		values: v,
	}

	return expr{
		tree: addNode(i.partialExpr.tree, neqNode),
	}
}

func (i ident[T]) GreaterThan(v T) expr {
	gtNode := &compNode[T]{
		node:   newNode(greaterThan),
		column: i.column,
		value:  v,
	}

	return expr{
		tree: addNode(i.partialExpr.tree, gtNode),
	}
}

func (i ident[T]) GreaterThanEq(v T) expr {
	gteqNode := &compNode[T]{
		node:   newNode(greaterThanEq),
		column: i.column,
		value:  v,
	}

	return expr{
		tree: addNode(i.partialExpr.tree, gteqNode),
	}
}

func (i ident[T]) LessThan(v T) expr {
	ltNode := &compNode[T]{
		node:   newNode(lessThan),
		column: i.column,
		value:  v,
	}

	return expr{
		tree: addNode(i.partialExpr.tree, ltNode),
	}
}

func (i ident[T]) LessThanEq(v T) expr {
	lteqNode := &compNode[T]{
		node:   newNode(lessThanEq),
		column: i.column,
		value:  v,
	}

	return expr{
		tree: addNode(i.partialExpr.tree, lteqNode),
	}
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

type Node interface {
	Type() nodeType
	Operator() operator
	Left() Node
	Right() Node
	SetLeft(Node)
	SetRight(Node)
	IsGroup() bool
	SetGroup(bool)
}

type node struct {
	left    Node
	right   Node
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

func (n *node) Left() Node {
	return n.left
}

func (n *node) Right() Node {
	return n.right
}

func (n *node) SetLeft(newNode Node) {
	n.left = newNode
}

func (n *node) SetRight(newNode Node) {
	n.right = newNode
}

func (n *node) IsGroup() bool {
	return n.isGroup
}

func (n *node) SetGroup(b bool) {
	n.isGroup = b
}

func addNode(tree Node, n Node) Node {
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
