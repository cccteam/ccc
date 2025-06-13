package resource

import (
	"errors"
	"fmt"
)

// JoinType enum
type JoinType string

const (
	InnerJoin JoinType = "INNER"
	LeftJoin  JoinType = "LEFT"
	RightJoin JoinType = "RIGHT"
	FullJoin  JoinType = "FULL"
)

// JoinClauseNode struct
type JoinClauseNode struct {
	Type   JoinType
	Target string // Name of the resource/table to join with
	On     QueryClause // The ON condition (uses existing QueryClause logic)
}

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

// QuerySet[T] struct
type QuerySet[T Resource] struct {
	rMeta         *ResourceMetadata[T]
	filterAst     ExpressionNode
	joins         []*JoinClauseNode // New field for joins
	limit         *LimitNode
	offset        *OffsetNode
	orderByFields []OrderByNode
	selectFields  []string
	err           error
}

// NewQuerySet initializes a new QuerySet.
func NewQuerySet[T Resource](rMeta *ResourceMetadata[T]) *QuerySet[T] {
	return &QuerySet[T]{
		rMeta: rMeta,
		joins: make([]*JoinClauseNode, 0), // Initialize the slice
		// ... other initializations will be added here by existing code or later steps
	}
}

// Generic Join method
func (qs *QuerySet[T]) Join(joinType JoinType, targetResourceName string, on QueryClause) *QuerySet[T] {
	if qs.err != nil {
		return qs
	}
	if targetResourceName == "" {
		qs.err = errors.New("join target resource name cannot be empty")
		return qs
	}

	joinNode := &JoinClauseNode{
		Type:   joinType,
		Target: targetResourceName,
		On:     on,
	}
	qs.joins = append(qs.joins, joinNode)
	return qs
}

// Convenience methods for specific join types
func (qs *QuerySet[T]) InnerJoin(targetResourceName string, on QueryClause) *QuerySet[T] {
	return qs.Join(InnerJoin, targetResourceName, on)
}

func (qs *QuerySet[T]) LeftJoin(targetResourceName string, on QueryClause) *QuerySet[T] {
	return qs.Join(LeftJoin, targetResourceName, on)
}

func (qs *QuerySet[T]) RightJoin(targetResourceName string, on QueryClause) *QuerySet[T] {
	return qs.Join(RightJoin, targetResourceName, on)
}

func (qs *QuerySet[T]) FullJoin(targetResourceName string, on QueryClause) *QuerySet[T] {
	return qs.Join(FullJoin, targetResourceName, on)
}

// Placeholder for ExpressionNode and related interfaces/structs
// These would be defined in another file or further down in this file.
type ExpressionNode interface {
	// String() string // Example method for converting to string representation (for debugging or logging)
	// Accept(visitor QueryVisitor) // Example method for visitor pattern (for SQL generation)
}

type LogicalOpNode struct {
	Left     ExpressionNode
	Operator LogicalOperator
	Right    ExpressionNode
}

type ConditionNode struct {
	Condition Condition
}

type GroupNode struct {
	Expression ExpressionNode
}

type Condition struct {
	Field     string
	Operator  string // e.g., "eq", "ne", "gt", "lt", "gte", "lte", "in", "notin", "isnull", "isnotnull"
	Value     any    // For single value operators like eq, ne, gt, lt
	Values    []any  // For multi-value operators like in, notin
	IsNullOp  bool   // True if the operator is of type IS NULL or IS NOT NULL
}

type LogicalOperator string

const (
	OperatorAnd LogicalOperator = "AND"
	OperatorOr  LogicalOperator = "OR"
)

// LimitNode, OffsetNode, OrderByNode, and ResourceMetadata would also be defined elsewhere or further down.
// For the purpose of this subtask, their exact definitions are not strictly needed here,
// but they are part of the QuerySet struct.

type LimitNode struct {
	Value int
}

type OffsetNode struct {
	Value int
}

type OrderByNode struct {
	Field     string
	Direction string // "ASC" or "DESC"
}

// Resource interface and ResourceMetadata struct (simplified for this context)
type Resource interface {
	TableName() string
	Schema() map[string]any // Simplified schema representation
}

type ResourceMetadata[T Resource] struct {
	// Placeholder for metadata fields
	// This would typically include information about the resource,
	// such as its table name, columns, relationships, etc.
}
