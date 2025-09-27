package resource

import (
	"fmt"

	stderr "errors"
)

type PartialQueryClause struct {
	tree            ExpressionNode
	hasIndexedField bool
}

func NewPartialQueryClause() PartialQueryClause {
	return PartialQueryClause{tree: nil}
}

func (p PartialQueryClause) Group(qc QueryClause) QueryClause {
	groupedExpr := &GroupNode{Expression: qc.tree}
	if p.tree == nil {
		return QueryClause{tree: groupedExpr, hasIndexedField: qc.hasIndexedField}
	}
	logicalNode, ok := p.tree.(*LogicalOpNode)
	if !ok {
		panic(fmt.Sprintf("Expected LogicalOpNode, got %T", p.tree))
	}
	logicalNode.Right = groupedExpr
	finalHasIndexedField := p.hasIndexedField || qc.hasIndexedField

	return QueryClause{tree: logicalNode, hasIndexedField: finalHasIndexedField}
}

type QueryClause struct {
	tree            ExpressionNode
	hasIndexedField bool
}

// Validate checks if the query clause has at least one indexed field.
func (qc QueryClause) Validate() error {
	if !qc.hasIndexedField {
		return stderr.New("invalid filter query, filter must contain at least one column that is indexed")
	}

	return nil
}

func (x QueryClause) And() PartialQueryClause {
	return PartialQueryClause{
		tree: &LogicalOpNode{
			Left:     x.tree,
			Operator: OperatorAnd,
		},
		hasIndexedField: x.hasIndexedField,
	}
}

func (x QueryClause) Or() PartialQueryClause {
	return PartialQueryClause{
		tree: &LogicalOpNode{
			Left:     x.tree,
			Operator: OperatorOr,
		},
		hasIndexedField: x.hasIndexedField,
	}
}

type Ident[T comparable] struct {
	column      string
	partialExpr PartialQueryClause
	indexed     bool
}

func NewIdent[T comparable](column string, px PartialQueryClause, indexed bool) Ident[T] {
	return Ident[T]{column, px, indexed}
}

func (i Ident[T]) Equal(v ...T) QueryClause {
	var conditionNode *ConditionNode
	if len(v) == 1 {
		conditionNode = &ConditionNode{
			Condition: Condition{
				Field:    i.column,
				Operator: eqStr,
				Value:    v[0],
			},
		}
	} else {
		values := make([]any, len(v))
		for idx, val := range v {
			values[idx] = val
		}
		conditionNode = &ConditionNode{
			Condition: Condition{
				Field:    i.column,
				Operator: inStr,
				Values:   values,
			},
		}
	}

	finalHasIndexedField := i.partialExpr.hasIndexedField || i.indexed

	if i.partialExpr.tree == nil {
		return QueryClause{tree: conditionNode, hasIndexedField: finalHasIndexedField}
	}

	logicalNode, ok := i.partialExpr.tree.(*LogicalOpNode)
	if !ok {
		panic(fmt.Sprintf("Expected LogicalOpNode, got %T", i.partialExpr.tree))
	}
	logicalNode.Right = conditionNode

	return QueryClause{tree: logicalNode, hasIndexedField: finalHasIndexedField}
}

func (i Ident[T]) NotEqual(v ...T) QueryClause {
	var conditionNode *ConditionNode
	if len(v) == 1 {
		conditionNode = &ConditionNode{
			Condition: Condition{
				Field:    i.column,
				Operator: neStr,
				Value:    v[0],
			},
		}
	} else {
		values := make([]any, len(v))
		for idx, val := range v {
			values[idx] = val
		}
		conditionNode = &ConditionNode{
			Condition: Condition{
				Field:    i.column,
				Operator: notinStr,
				Values:   values,
			},
		}
	}

	finalHasIndexedField := i.partialExpr.hasIndexedField || i.indexed

	if i.partialExpr.tree == nil {
		return QueryClause{tree: conditionNode, hasIndexedField: finalHasIndexedField}
	}

	logicalNode, ok := i.partialExpr.tree.(*LogicalOpNode)
	if !ok {
		panic(fmt.Sprintf("Expected LogicalOpNode, got %T", i.partialExpr.tree))
	}
	logicalNode.Right = conditionNode

	return QueryClause{tree: logicalNode, hasIndexedField: finalHasIndexedField}
}

func (i Ident[T]) IsNull() QueryClause {
	conditionNode := &ConditionNode{
		Condition: Condition{
			Field:    i.column,
			Operator: "isnull",
			IsNullOp: true,
		},
	}
	finalHasIndexedField := i.partialExpr.hasIndexedField || i.indexed

	if i.partialExpr.tree == nil {
		return QueryClause{tree: conditionNode, hasIndexedField: finalHasIndexedField}
	}

	logicalNode, ok := i.partialExpr.tree.(*LogicalOpNode)
	if !ok {
		panic(fmt.Sprintf("Expected LogicalOpNode, got %T", i.partialExpr.tree))
	}
	logicalNode.Right = conditionNode

	return QueryClause{tree: logicalNode, hasIndexedField: finalHasIndexedField}
}

func (i Ident[T]) IsNotNull() QueryClause {
	conditionNode := &ConditionNode{
		Condition: Condition{
			Field:    i.column,
			Operator: "isnotnull",
			IsNullOp: true,
		},
	}
	finalHasIndexedField := i.partialExpr.hasIndexedField || i.indexed

	if i.partialExpr.tree == nil {
		return QueryClause{tree: conditionNode, hasIndexedField: finalHasIndexedField}
	}

	logicalNode, ok := i.partialExpr.tree.(*LogicalOpNode)
	if !ok {
		panic(fmt.Sprintf("Expected LogicalOpNode, got %T", i.partialExpr.tree))
	}
	logicalNode.Right = conditionNode

	return QueryClause{tree: logicalNode, hasIndexedField: finalHasIndexedField}
}

func (i Ident[T]) GreaterThan(v T) QueryClause {
	conditionNode := &ConditionNode{
		Condition: Condition{
			Field:    i.column,
			Operator: gtStr,
			Value:    v,
		},
	}
	finalHasIndexedField := i.partialExpr.hasIndexedField || i.indexed

	if i.partialExpr.tree == nil {
		return QueryClause{tree: conditionNode, hasIndexedField: finalHasIndexedField}
	}

	logicalNode, ok := i.partialExpr.tree.(*LogicalOpNode)
	if !ok {
		panic(fmt.Sprintf("Expected LogicalOpNode, got %T", i.partialExpr.tree))
	}
	logicalNode.Right = conditionNode

	return QueryClause{tree: logicalNode, hasIndexedField: finalHasIndexedField}
}

func (i Ident[T]) GreaterThanEq(v T) QueryClause {
	conditionNode := &ConditionNode{
		Condition: Condition{
			Field:    i.column,
			Operator: gteStr,
			Value:    v,
		},
	}
	finalHasIndexedField := i.partialExpr.hasIndexedField || i.indexed

	if i.partialExpr.tree == nil {
		return QueryClause{tree: conditionNode, hasIndexedField: finalHasIndexedField}
	}

	logicalNode, ok := i.partialExpr.tree.(*LogicalOpNode)
	if !ok {
		panic(fmt.Sprintf("Expected LogicalOpNode, got %T", i.partialExpr.tree))
	}
	logicalNode.Right = conditionNode

	return QueryClause{tree: logicalNode, hasIndexedField: finalHasIndexedField}
}

func (i Ident[T]) LessThan(v T) QueryClause {
	conditionNode := &ConditionNode{
		Condition: Condition{
			Field:    i.column,
			Operator: ltStr,
			Value:    v,
		},
	}
	finalHasIndexedField := i.partialExpr.hasIndexedField || i.indexed

	if i.partialExpr.tree == nil {
		return QueryClause{tree: conditionNode, hasIndexedField: finalHasIndexedField}
	}

	logicalNode, ok := i.partialExpr.tree.(*LogicalOpNode)
	if !ok {
		panic(fmt.Sprintf("Expected LogicalOpNode, got %T", i.partialExpr.tree))
	}
	logicalNode.Right = conditionNode

	return QueryClause{tree: logicalNode, hasIndexedField: finalHasIndexedField}
}

func (i Ident[T]) LessThanEq(v T) QueryClause {
	conditionNode := &ConditionNode{
		Condition: Condition{
			Field:    i.column,
			Operator: lteStr,
			Value:    v,
		},
	}
	finalHasIndexedField := i.partialExpr.hasIndexedField || i.indexed

	if i.partialExpr.tree == nil {
		return QueryClause{tree: conditionNode, hasIndexedField: finalHasIndexedField}
	}

	logicalNode, ok := i.partialExpr.tree.(*LogicalOpNode)
	if !ok {
		panic(fmt.Sprintf("Expected LogicalOpNode, got %T", i.partialExpr.tree))
	}
	logicalNode.Right = conditionNode

	return QueryClause{tree: logicalNode, hasIndexedField: finalHasIndexedField}
}
