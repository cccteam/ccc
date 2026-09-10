package resource

import (
	"reflect"
	"slices"

	"github.com/go-playground/errors/v5"
)

// FilterShape is the request's filter over Go field names, as a computed
// resource's List function and its generated handler share it. The body may take
// the conditions it will apply in its own queries; the handler evaluates whatever
// was not taken against every row the body yields (Match), so a body that takes
// nothing is correct by default and a body that takes part is never wrong. Take
// is defined on a conjunction: a filter carrying OR takes nothing, and the handler
// evaluates the whole tree.
type FilterShape struct {
	root     ExpressionNode
	taken    map[*ConditionNode]bool
	parseErr error
}

// Filter returns the request's filter over Go field names. Without a filter every
// query answers empty and Match admits every row.
func (q *QuerySet[Resource]) Filter() *FilterShape {
	if q.filterShape != nil {
		return q.filterShape
	}
	q.filterShape = &FilterShape{taken: make(map[*ConditionNode]bool)}
	if q.filterParser != nil {
		root, err := q.filterParser(goFieldNames)
		if err != nil {
			// The decoder parsed this same text against Go field names at decode
			// and refused the request on any error, so a failure here is a
			// programming error, surfaced by the handler's Match.
			q.filterShape.parseErr = err
		}
		q.filterShape.root = root
	}

	return q.filterShape
}

// Conditions lists every condition in the filter, taken or not, in source order.
func (f *FilterShape) Conditions() []Condition {
	all := leaves(f.root)
	out := make([]Condition, 0, len(all))
	for _, leaf := range all {
		out = append(out, leaf.Condition)
	}

	return out
}

// Fields names the fields the filter touches, each once, in source order.
func (f *FilterShape) Fields() []string {
	var out []string
	for _, leaf := range leaves(f.root) {
		if !slices.Contains(out, leaf.Condition.Field) {
			out = append(out, leaf.Condition.Field)
		}
	}

	return out
}

// IsConjunction reports whether the filter is one AND of conditions, the shape
// whose parts can be taken independently.
func (f *FilterShape) IsConjunction() bool {
	return f.root != nil && isConjunction(f.root)
}

// Take removes the conditions on the named fields from what the handler
// evaluates and returns them to the body, which applies them in its own
// queries. On a filter that is not a conjunction it takes nothing: a part of an
// OR cannot be applied apart from the rest.
func (f *FilterShape) Take(fields ...string) []Condition {
	if !f.IsConjunction() {
		return nil
	}
	var out []Condition
	for _, leaf := range leaves(f.root) {
		if slices.Contains(fields, leaf.Condition.Field) && !f.taken[leaf] {
			f.taken[leaf] = true
			out = append(out, leaf.Condition)
		}
	}

	return out
}

// residualEmpty reports whether nothing is left for the handler to evaluate.
func (f *FilterShape) residualEmpty() bool {
	for _, leaf := range leaves(f.root) {
		if !f.taken[leaf] {
			return false
		}
	}

	return true
}

// Match evaluates the conditions the body did not take against one row: the
// same operators, literal typing, and NULL semantics the SQL rendering has.
func (f *FilterShape) Match(row any) (bool, error) {
	if f.parseErr != nil {
		return false, errors.Wrap(f.parseErr, "filter")
	}
	if f.root == nil {
		return true, nil
	}
	value := reflect.ValueOf(row)
	for value.Kind() == reflect.Pointer {
		value = value.Elem()
	}
	if value.Kind() != reflect.Struct {
		return false, errors.Newf("FilterShape.Match: row must be a struct or pointer to one, got %T", row)
	}

	return f.eval(f.root, value)
}

func (f *FilterShape) eval(node ExpressionNode, row reflect.Value) (bool, error) {
	switch n := node.(type) {
	case *ConditionNode:
		if f.taken[n] {
			return true, nil
		}

		return matchCondition(&n.Condition, row)
	case *LogicalOpNode:
		left, err := f.eval(n.Left, row)
		if err != nil {
			return false, err
		}
		if n.Operator == OperatorAnd && !left {
			return false, nil
		}
		if n.Operator == OperatorOr && left {
			return true, nil
		}

		return f.eval(n.Right, row)
	case *GroupNode:
		return f.eval(n.Expression, row)
	default:
		return false, errors.Newf("FilterShape.Match: unsupported filter node %T", node)
	}
}

// matchCondition evaluates one condition against a row. A NULL field (nil
// pointer, invalid Null* wrapper) matches only isnull, as in SQL.
func matchCondition(c *Condition, row reflect.Value) (bool, error) {
	field := fieldValue(row, c.Field)
	if !field.IsValid() {
		return false, errors.Newf("FilterShape.Match: %s is not a field of %s", c.Field, row.Type())
	}
	value, isNull := derefNullable(field)
	if c.IsNullOp {
		return (c.Operator == isnullStr) == isNull, nil
	}
	if isNull {
		return false, nil
	}

	switch c.Operator {
	case inStr, notinStr:
		for _, literal := range c.Values {
			cmp, err := compareToLiteral(value, literal)
			if err != nil {
				return false, err
			}
			if cmp == 0 {
				return c.Operator == inStr, nil
			}
		}

		return c.Operator == notinStr, nil
	default:
		cmp, err := compareToLiteral(value, c.Value)
		if err != nil {
			return false, err
		}
		switch c.Operator {
		case eqStr:
			return cmp == 0, nil
		case neStr:
			return cmp != 0, nil
		case gtStr:
			return cmp > 0, nil
		case gteStr:
			return cmp >= 0, nil
		case ltStr:
			return cmp < 0, nil
		case lteStr:
			return cmp <= 0, nil
		default:
			return false, errors.Newf("FilterShape.Match: unknown operator %q", c.Operator)
		}
	}
}

// compareToLiteral compares a row value with a filter literal (a string for
// text and every marshaled type, a number for numbers, a bool for booleans)
// by the row value's type.
func compareToLiteral(value reflect.Value, literal any) (int, error) {
	text, err := literalText(literal)
	if err != nil {
		return 0, err
	}
	other, err := cursorValue(text, value.Type())
	if err != nil {
		return 0, errors.Newf("FilterShape.Match: literal %q cannot be compared with a %s", text, value.Type())
	}

	return compareValues(value, reflect.ValueOf(other))
}

// literalText renders a parsed filter literal as text so the row type decides how it
// is read: a string as itself, and every typed literal (a number, a boolean, a decimal,
// a time, a date, a UUID) through the encoding a cursor boundary uses.
func literalText(literal any) (string, error) {
	if s, ok := literal.(string); ok {
		return s, nil
	}
	text, err := cursorText(reflect.ValueOf(literal))
	if err != nil {
		return "", errors.Wrapf(err, "FilterShape.Match: unsupported literal %T", literal)
	}
	if text == nil {
		return "", errors.Newf("FilterShape.Match: null literal %T", literal)
	}

	return *text, nil
}

// leaves lists the condition nodes of a tree in source order.
func leaves(node ExpressionNode) []*ConditionNode {
	switch n := node.(type) {
	case *ConditionNode:
		return []*ConditionNode{n}
	case *LogicalOpNode:
		return append(leaves(n.Left), leaves(n.Right)...)
	case *GroupNode:
		return leaves(n.Expression)
	default:
		return nil
	}
}

// isConjunction reports whether a tree is conditions joined by AND alone.
func isConjunction(node ExpressionNode) bool {
	switch n := node.(type) {
	case *ConditionNode:
		return true
	case *LogicalOpNode:
		return n.Operator == OperatorAnd && isConjunction(n.Left) && isConjunction(n.Right)
	case *GroupNode:
		return isConjunction(n.Expression)
	default:
		return false
	}
}
