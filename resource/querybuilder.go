package resource

import (
	"fmt"

	stderr "errors"
)

// PartialQueryClause represents an incomplete query clause, typically the left-hand side of a logical operation.
type PartialQueryClause struct {
	tree            ExpressionNode
	hasIndexedField bool
}

// NewPartialQueryClause creates an empty PartialQueryClause.
func NewPartialQueryClause() PartialQueryClause {
	return PartialQueryClause{tree: nil}
}

// Group wraps a QueryClause in parentheses.
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

// QueryClause represents a complete, valid query expression that can be part of a WHERE clause.
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

// And starts a logical AND operation, returning a PartialQueryClause to which the right-hand side can be appended.
func (qc QueryClause) And() PartialQueryClause {
	return PartialQueryClause{
		tree: &LogicalOpNode{
			Left:     qc.tree,
			Operator: OperatorAnd,
		},
		hasIndexedField: qc.hasIndexedField,
	}
}

// Or starts a logical OR operation, returning a PartialQueryClause to which the right-hand side can be appended.
func (qc QueryClause) Or() PartialQueryClause {
	return PartialQueryClause{
		tree: &LogicalOpNode{
			Left:     qc.tree,
			Operator: OperatorOr,
		},
		hasIndexedField: qc.hasIndexedField,
	}
}

// Ident represents a database column identifier in a query, typed to ensure compile-time correctness of comparisons.
type Ident[T comparable] struct {
	column      string
	partialExpr PartialQueryClause
	indexed     bool
}

// NewIdent creates a new identifier for a column.
func NewIdent[T comparable](column string, px PartialQueryClause, indexed bool) Ident[T] {
	return Ident[T]{column, px, indexed}
}

// Equal creates an equality (`=`) or `IN` condition.
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

// NotEqual creates a not-equal (`<>`) or `NOT IN` condition.
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

// IsNull creates an `IS NULL` condition.
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

// IsNotNull creates an `IS NOT NULL` condition.
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

// GreaterThan creates a `>` condition.
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

// GreaterThanEq creates a `>=` condition.
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

// LessThan creates a `<` condition.
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

// LessThanEq creates a `<=` condition.
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
