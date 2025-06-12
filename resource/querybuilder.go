package resource

import (
	"fmt"
)

type PartialQueryClause struct {
	tree ExpressionNode
}

func NewPartialQueryClause() PartialQueryClause {
	return PartialQueryClause{tree: nil}
}

func (p PartialQueryClause) Group(qc QueryClause) QueryClause {
	groupedExpr := &GroupNode{Expression: qc.tree}
	if p.tree == nil {
		return QueryClause{tree: groupedExpr}
	}
	logicalNode, ok := p.tree.(*LogicalOpNode)
	if !ok {
		panic(fmt.Sprintf("Expected LogicalOpNode, got %T", p.tree))
	}
	logicalNode.Right = groupedExpr

	return QueryClause{tree: logicalNode}
}

type QueryClause struct {
	tree ExpressionNode
}

func (x QueryClause) And() PartialQueryClause {
	return PartialQueryClause{
		tree: &LogicalOpNode{
			Left:     x.tree,
			Operator: OperatorAnd,
		},
	}
}

func (x QueryClause) Or() PartialQueryClause {
	return PartialQueryClause{
		tree: &LogicalOpNode{
			Left:     x.tree,
			Operator: OperatorOr,
		},
	}
}

type Ident[T comparable] struct {
	column      string
	partialExpr PartialQueryClause
}

func NewIdent[T comparable](column string, px PartialQueryClause) Ident[T] {
	return Ident[T]{column, px}
}

func (i Ident[T]) Equal(v ...T) QueryClause {
	var conditionNode *ConditionNode
	if len(v) == 1 {
		conditionNode = &ConditionNode{
			Condition: Condition{
				Field:    i.column,
				Operator: "eq",
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
				Operator: "in",
				Values:   values,
			},
		}
	}

	if i.partialExpr.tree == nil {
		return QueryClause{tree: conditionNode}
	}

	logicalNode, ok := i.partialExpr.tree.(*LogicalOpNode)
	if !ok {
		panic(fmt.Sprintf("Expected LogicalOpNode, got %T", i.partialExpr.tree))
	}
	logicalNode.Right = conditionNode

	return QueryClause{tree: logicalNode}
}

func (i Ident[T]) NotEqual(v ...T) QueryClause {
	var conditionNode *ConditionNode
	if len(v) == 1 {
		conditionNode = &ConditionNode{
			Condition: Condition{
				Field:    i.column,
				Operator: "ne",
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
				Operator: "notin",
				Values:   values,
			},
		}
	}

	if i.partialExpr.tree == nil {
		return QueryClause{tree: conditionNode}
	}

	logicalNode, ok := i.partialExpr.tree.(*LogicalOpNode)
	if !ok {
		panic(fmt.Sprintf("Expected LogicalOpNode, got %T", i.partialExpr.tree))
	}
	logicalNode.Right = conditionNode

	return QueryClause{tree: logicalNode}
}

func (i Ident[T]) IsNull() QueryClause {
	conditionNode := &ConditionNode{
		Condition: Condition{
			Field:    i.column,
			Operator: "isnull",
			IsNullOp: true,
		},
	}

	if i.partialExpr.tree == nil {
		return QueryClause{tree: conditionNode}
	}

	logicalNode, ok := i.partialExpr.tree.(*LogicalOpNode)
	if !ok {
		panic(fmt.Sprintf("Expected LogicalOpNode, got %T", i.partialExpr.tree))
	}
	logicalNode.Right = conditionNode

	return QueryClause{tree: logicalNode}
}

func (i Ident[T]) IsNotNull() QueryClause {
	conditionNode := &ConditionNode{
		Condition: Condition{
			Field:    i.column,
			Operator: "isnotnull",
			IsNullOp: true,
		},
	}

	if i.partialExpr.tree == nil {
		return QueryClause{tree: conditionNode}
	}

	logicalNode, ok := i.partialExpr.tree.(*LogicalOpNode)
	if !ok {
		panic(fmt.Sprintf("Expected LogicalOpNode, got %T", i.partialExpr.tree))
	}
	logicalNode.Right = conditionNode

	return QueryClause{tree: logicalNode}
}

func (i Ident[T]) GreaterThan(v T) QueryClause {
	conditionNode := &ConditionNode{
		Condition: Condition{
			Field:    i.column,
			Operator: "gt",
			Value:    v,
		},
	}

	if i.partialExpr.tree == nil {
		return QueryClause{tree: conditionNode}
	}

	logicalNode, ok := i.partialExpr.tree.(*LogicalOpNode)
	if !ok {
		panic(fmt.Sprintf("Expected LogicalOpNode, got %T", i.partialExpr.tree))
	}
	logicalNode.Right = conditionNode

	return QueryClause{tree: logicalNode}
}

func (i Ident[T]) GreaterThanEq(v T) QueryClause {
	conditionNode := &ConditionNode{
		Condition: Condition{
			Field:    i.column,
			Operator: "gte",
			Value:    v,
		},
	}

	if i.partialExpr.tree == nil {
		return QueryClause{tree: conditionNode}
	}

	logicalNode, ok := i.partialExpr.tree.(*LogicalOpNode)
	if !ok {
		panic(fmt.Sprintf("Expected LogicalOpNode, got %T", i.partialExpr.tree))
	}
	logicalNode.Right = conditionNode

	return QueryClause{tree: logicalNode}
}

func (i Ident[T]) LessThan(v T) QueryClause {
	conditionNode := &ConditionNode{
		Condition: Condition{
			Field:    i.column,
			Operator: "lt",
			Value:    v,
		},
	}

	if i.partialExpr.tree == nil {
		return QueryClause{tree: conditionNode}
	}

	logicalNode, ok := i.partialExpr.tree.(*LogicalOpNode)
	if !ok {
		panic(fmt.Sprintf("Expected LogicalOpNode, got %T", i.partialExpr.tree))
	}
	logicalNode.Right = conditionNode

	return QueryClause{tree: logicalNode}
}

func (i Ident[T]) LessThanEq(v T) QueryClause {
	conditionNode := &ConditionNode{
		Condition: Condition{
			Field:    i.column,
			Operator: "lte",
			Value:    v,
		},
	}

	if i.partialExpr.tree == nil {
		return QueryClause{tree: conditionNode}
	}

	logicalNode, ok := i.partialExpr.tree.(*LogicalOpNode)
	if !ok {
		panic(fmt.Sprintf("Expected LogicalOpNode, got %T", i.partialExpr.tree))
	}
	logicalNode.Right = conditionNode

	return QueryClause{tree: logicalNode}
}
