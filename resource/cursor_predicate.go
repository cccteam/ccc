package resource

import (
	"reflect"
	"strings"

	"github.com/go-playground/errors/v5"
)

// cursorTerm is one column of the walk's total order as the predicate renderer
// sees it: quoted for the database, with its direction, nullability, where the
// database places NULL in that direction, and the boundary row's value (nil in
// the NULL region).
type cursorTerm struct {
	column    string
	direction SortDirection
	nullable  bool
	// nullsFirst is the database's NULL placement for this direction: true when
	// the NULL region precedes every value, false when it follows them.
	nullsFirst bool
	fieldType  reflect.Type
	boundary   *string
	// param is the placeholder the boundary value is bound under, allocated on
	// first use so every disjunct that names the value shares one parameter.
	param string
}

// nullsFirst reports where the database places NULL in an ORDER BY term with
// no stated placement: Spanner sorts NULL as the smallest value (first
// ascending, last descending), PostgreSQL as the largest (last ascending, first
// descending). The ORDER BY states nothing, so the cursor predicate takes the
// placement from here.
func nullsFirst(dbType DBType, direction SortDirection) bool {
	if dbType == SpannerDBType {
		return direction == SortAscending
	}

	return direction == SortDescending
}

// renderCursorPredicate renders the rows strictly after the boundary row in the
// order the terms describe, one disjunct per term:
//
//	c0 > @k0 OR (c0 = @k0 AND c1 > @k1) OR (c0 = @k0 AND c1 = @k1 AND c2 > @k2)
//
// Each column takes the comparison matching its direction. A nullable column's
// NULL region sits where its database puts it (nullsFirst): when the region
// follows the values, the disjunct after a non-null boundary also admits it and
// the disjunct after a NULL boundary is empty; when the region precedes the
// values, the disjunct after a non-null boundary is the comparison alone and
// the disjunct after a NULL boundary admits every non-null row. The equality
// term on a NULL boundary is IS NULL. The previous page is the same rendering
// over the flipped order, whose placement flips with it.
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
// "" when no row can follow it (a NULL boundary with the NULL region last).
func (t *cursorTerm) strictlyAfter(registry *paramRegistry) (string, error) {
	if t.boundary == nil {
		if !t.nullable {
			return "", errInvalidCursor
		}
		if t.nullsFirst {
			return t.column + " IS NOT NULL", nil
		}

		return "", nil
	}

	param, err := t.bind(registry)
	if err != nil {
		return "", err
	}
	comparison := t.column + " > " + param
	if t.direction == SortDescending {
		comparison = t.column + " < " + param
	}
	if t.nullable && !t.nullsFirst {
		return "(" + comparison + " OR " + t.column + " IS NULL)", nil
	}

	return comparison, nil
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
