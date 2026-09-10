package resource

import (
	"reflect"
	"strings"

	"github.com/go-playground/errors/v5"
)

// cursorTerm is one column of the walk's total order as the predicate renderer
// sees it: quoted for the database, with its direction, nullability, and the
// boundary row's value (nil in the NULL region).
type cursorTerm struct {
	column    string
	direction SortDirection
	nullable  bool
	fieldType reflect.Type
	boundary  *string
	// param is the placeholder the boundary value is bound under, allocated on
	// first use so every disjunct that names the value shares one parameter.
	param string
}

// renderCursorPredicate renders the rows strictly after the boundary row in the
// order the terms describe, one disjunct per term:
//
//	c0 > @k0 OR (c0 = @k0 AND c1 > @k1) OR (c0 = @k0 AND c1 = @k1 AND c2 > @k2)
//
// Each column takes the comparison matching its direction. A nullable column
// orders with NULLS LAST ascending and NULLS FIRST descending, so after a
// non-null value the ascending disjunct also admits the NULL region, and after a
// NULL boundary the ascending disjunct is empty while the descending one admits
// every non-null row; the equality term on a NULL boundary is IS NULL. The
// rendering is the same on Spanner and PostgreSQL apart from quoting. The
// previous page is the same rendering over the flipped order.
func renderCursorPredicate(terms []cursorTerm, registry *paramRegistry) (string, error) {
	var disjuncts []string
	var equalities []string
	for i := range terms {
		term := &terms[i]
		after, err := term.strictlyAfter(registry)
		if err != nil {
			return "", err
		}
		if after != "" {
			conjuncts := append(append([]string{}, equalities...), after)
			if len(conjuncts) == 1 {
				disjuncts = append(disjuncts, conjuncts[0])
			} else {
				disjuncts = append(disjuncts, "("+strings.Join(conjuncts, " AND ")+")")
			}
		}

		equality, err := term.equality(registry)
		if err != nil {
			return "", err
		}
		equalities = append(equalities, equality)
	}

	if len(disjuncts) == 0 {
		return sqlFalse, nil
	}

	return "(" + strings.Join(disjuncts, " OR ") + ")", nil
}

// strictlyAfter renders the rows after the boundary on this column alone, or
// "" when no row can follow it (a NULL boundary on an ascending column).
func (t *cursorTerm) strictlyAfter(registry *paramRegistry) (string, error) {
	if t.boundary == nil {
		if !t.nullable {
			return "", errInvalidCursor
		}
		if t.direction == SortDescending {
			return t.column + " IS NOT NULL", nil
		}

		return "", nil
	}

	param, err := t.bind(registry)
	if err != nil {
		return "", err
	}
	if t.direction == SortDescending {
		return t.column + " < " + param, nil
	}
	if t.nullable {
		return "(" + t.column + " > " + param + " OR " + t.column + " IS NULL)", nil
	}

	return t.column + " > " + param, nil
}

// equality renders the rows equal to the boundary on this column.
func (t *cursorTerm) equality(registry *paramRegistry) (string, error) {
	if t.boundary == nil {
		if !t.nullable {
			return "", errInvalidCursor
		}

		return t.column + " IS NULL", nil
	}

	param, err := t.bind(registry)
	if err != nil {
		return "", err
	}

	return t.column + " = " + param, nil
}

// bind decodes the boundary value to the column's type and binds it once.
func (t *cursorTerm) bind(registry *paramRegistry) (string, error) {
	if t.param != "" {
		return t.param, nil
	}
	value, err := cursorValue(*t.boundary, t.fieldType)
	if err != nil {
		return "", errors.Wrapf(err, "cursor value for %s", t.column)
	}
	t.param = registry.bind(value)

	return t.param, nil
}

// flipped returns the order with every direction reversed: the previous page is
// the next page of the reversed walk, read and then reversed again.
func flipped(order []SortField) []SortField {
	out := make([]SortField, len(order))
	for i, sf := range order {
		out[i] = SortField{Field: sf.Field, Direction: SortAscending}
		if sf.Direction == SortAscending {
			out[i].Direction = SortDescending
		}
	}

	return out
}
