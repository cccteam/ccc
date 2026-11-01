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

// join appends a condition to the partial clause: the right side of the pending logical
// operation, or the whole clause when nothing is pending.
func (p PartialQueryClause) join(node ExpressionNode, indexed bool) QueryClause {
	hasIndexedField := p.hasIndexedField || indexed
	if p.tree == nil {
		return QueryClause{tree: node, hasIndexedField: hasIndexedField}
	}
	logicalNode, ok := p.tree.(*LogicalOpNode)
	if !ok {
		panic(fmt.Sprintf("Expected LogicalOpNode, got %T", p.tree))
	}
	logicalNode.Right = node

	return QueryClause{tree: logicalNode, hasIndexedField: hasIndexedField}
}

// TextIdent is a text column's identifier with the search vocabulary its schema gives it,
// the field's search tag (README section 4). The generated query builder declares one per
// text field that answers a match and forwards only the matches the field answers, so a
// match the field cannot answer does not compile; a match it answers builds the same
// condition the filter grammar's operator does, rendered through the same columns.
type TextIdent struct {
	Ident[string]
	field *searchField
}

// NewTextIdent creates a text column's identifier from its search tag. A tag the runtime
// cannot read is generated code out of step with the package and panics, like every
// other generated-code mismatch.
func NewTextIdent(column string, px PartialQueryClause, indexed bool, tag string) TextIdent {
	spec, err := parseSearchSpec(tag)
	if err != nil {
		panic(fmt.Sprintf("resource.NewTextIdent(%s): %v", column, err))
	}

	return TextIdent{
		Ident: NewIdent[string](column, px, indexed),
		field: &searchField{name: column, column: column, goField: column, jsonName: column, spec: spec},
	}
}

// StartsWith creates a startswith condition: the column's value begins with the term.
func (i TextIdent) StartsWith(term string) QueryClause {
	return i.match(startswithStr, term)
}

// EndsWith creates an endswith condition: the column's value ends with the term.
func (i TextIdent) EndsWith(term string) QueryClause {
	return i.match(endswithStr, term)
}

// Contains creates a contains condition: the term appears anywhere in the column's value.
func (i TextIdent) Contains(term string) QueryClause {
	return i.match(containsStr, term)
}

// match builds one text match. The generated builder forwards only the matches the
// field answers, so a match reaching here unanswered is hand-written code against the
// schema and panics.
func (i TextIdent) match(operator, term string) QueryClause {
	if !i.field.spec.Answers(operator) {
		panic(fmt.Sprintf("resource.TextIdent(%s): the field does not answer %s; it answers %s", i.column, operator, answeredMatches(i.field.spec)))
	}
	node := &ConditionNode{Condition: Condition{Field: i.column, Operator: operator, Value: term, match: i.field}}

	return i.partialExpr.join(node, i.indexed || i.field.spec.Token != "" || i.field.spec.Lower != "")
}

// SearchColumn is one searchable column of a resource as the generated query builder
// declares it for the search box: the column and its search tag.
type SearchColumn struct {
	Column string
	Tag    string
}

// Search creates the search box over the resource's searchable columns, as the filter
// grammar's *:search:"…" does: every term of the text must be found in one of the
// columns, or in one of the resource's @searchAlso branches. The generated query builder
// passes the columns in declaration order, the resource's single-column key (which a box
// over several search indexes joins its key sets on; "" when the resource declares
// none) and its branches. The grammar's limits apply, so a text they refuse is refused
// here with the same message.
func (p PartialQueryClause) Search(text, key string, columns []SearchColumn, branches []SearchBranch) (QueryClause, error) {
	terms, err := splitSearchTerms(text)
	if err != nil {
		return QueryClause{}, err
	}
	fields := make([]searchField, 0, len(columns))
	for _, column := range columns {
		spec, err := parseSearchSpec(column.Tag)
		if err != nil {
			return QueryClause{}, fmt.Errorf("resource.Search(%s): %w", column.Column, err)
		}
		if !spec.JoinsBox() {
			continue
		}
		fields = append(fields, searchField{name: column.Column, column: column.Column, goField: column.Column, jsonName: column.Column, spec: spec})
	}
	if len(fields) == 0 && len(branches) == 0 {
		return QueryClause{}, stderr.New("resource.Search: no column joins the search box")
	}
	node := &ConditionNode{Condition: Condition{
		Field:    searchBoxField,
		Operator: searchStr,
		Value:    text,
		search:   &searchCondition{text: text, terms: terms, fields: fields, branches: branches, key: key},
	}}

	return p.join(node, true), nil
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
			Operator: isnullStr,
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
			Operator: isnotnullStr,
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
