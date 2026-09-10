package resource

import (
	"iter"
	"reflect"

	"github.com/go-playground/errors/v5"
)

// The computed-resource query contract. The generated list handler applies the
// request's filter, sort, and page over the rows a computed resource's List
// function yields, so a List function is correct with no query code at all. A
// body that runs its own queries may take pieces of the work to push them down
// — Filter().Take for conditions, TakeSort for the order, TakePage for the page —
// and the handler applies whatever was not taken. Taking is the only way to opt
// out: a body cannot claim work it did not do, so it can never make the result
// wrong.

// TakeSort returns the list's total order (the request's sort or the declared
// default, then the primary key) and marks it as the body's: the handler will
// not sort. A body that takes the sort must yield its rows in exactly this
// order, or its pages are wrong.
func (q *QuerySet[Resource]) TakeSort() []SortField {
	q.sortTaken = true

	return q.readOrder()
}

// PageBounds is the page a body pages for itself: how many rows to yield and
// where to start. Order is the read order the rows must arrive in — the total
// order, or its reverse when the request walks back — and Boundary the values of
// the row the page starts after, one per order field, nil on a first page. The
// body yields the rows strictly after the boundary in that order, up to Fetch
// of them: the page and one more, which tells the handler a next page exists and
// is never encoded.
type PageBounds struct {
	order    []SortField
	boundary []any
	size     uint64
}

// Order is the order the body's rows must arrive in.
func (b PageBounds) Order() []SortField { return b.order }

// Boundary is the row the page starts after, as one value per order field in the
// row's own Go types (a nil entry is a NULL); nil on a first page.
func (b PageBounds) Boundary() []any { return b.boundary }

// Fetch is how many rows the body should yield: the page size plus one.
func (b PageBounds) Fetch() uint64 { return b.size + 1 }

// TakePage returns the page to yield and marks paging as the body's, when
// paging in the body is correct: no filter condition remains for the handler,
// the sort was taken (the body controls the order), the request asks for a page
// rather than every row, and no count was asked for (a total needs every row).
// Otherwise it answers false and the handler pages; a body written for pushdown
// then degrades to the correct answer instead of the wrong page.
func (q *QuerySet[Resource]) TakePage() (PageBounds, bool) {
	if q.page == nil || q.page.all || q.page.count || !q.sortTaken || !q.Filter().residualEmpty() {
		return PageBounds{}, false
	}
	bounds := PageBounds{order: q.readOrder(), size: q.page.size}
	if q.cursor != nil {
		var r Resource
		values, err := decodeBoundary(reflect.TypeOf(r), bounds.order, q.cursor.Keys)
		if err != nil {
			return PageBounds{}, false
		}
		bounds.boundary = values
	}
	q.pageTaken = true

	return bounds, true
}

// decodeBoundary reads a cursor's values into the row type's own field types.
func decodeBoundary(rowType reflect.Type, order []SortField, keys []*string) ([]any, error) {
	if len(keys) != len(order) {
		return nil, errInvalidCursor
	}
	values := make([]any, len(order))
	for i, sf := range order {
		if keys[i] == nil {
			continue
		}
		field, ok := structField(rowType, sf.Field)
		if !ok {
			return nil, errors.Newf("resource: sort field %s is not a field of %s", sf.Field, rowType)
		}
		value, err := cursorValue(*keys[i], field.Type)
		if err != nil {
			return nil, err
		}
		values[i] = value
	}

	return values, nil
}

// Collect runs a computed List function's rows through the parts of the query
// the body did not take — the residual filter, the sort, the cursor position,
// and the page — and returns the page the handler encodes, with the headers it
// writes. The order inside is fixed: filter, then count, then sort, then page,
// because any other order gives a different answer.
func (q *QuerySet[Resource]) Collect(rows iter.Seq2[*Resource, error]) (*Page[Resource], error) {
	filter := q.Filter()
	var kept []*Resource
	for row, err := range rows {
		if err != nil {
			return nil, err
		}
		ok, err := filter.Match(row)
		if err != nil {
			return nil, errors.Wrap(err, "FilterShape.Match()")
		}
		if ok {
			kept = append(kept, row)
		}
	}

	page := q.Page()
	if q.page != nil && q.page.count {
		total := int64(len(kept))
		page.total = &total
	}

	if !q.sortTaken {
		if err := SortRows(kept, q.readOrder()); err != nil {
			return nil, err
		}
	}
	if q.cursor != nil && !q.pageTaken {
		var err error
		kept, err = q.afterBoundary(kept)
		if err != nil {
			return nil, err
		}
	}

	for _, row := range kept {
		// A computed row is never masked (a Conditional decision is refused at
		// decode), so the envelope carries the data alone.
		if !page.Add(&Row[Resource]{Data: *row}) {
			break
		}
		page.rows = append(page.rows, row)
	}

	return page, nil
}

// afterBoundary keeps the rows strictly after the cursor's boundary row in the
// read order, the in-memory form of the cursor predicate.
func (q *QuerySet[Resource]) afterBoundary(rows []*Resource) ([]*Resource, error) {
	var r Resource
	order := q.readOrder()
	boundary, err := decodeBoundary(reflect.TypeOf(r), order, q.cursor.Keys)
	if err != nil {
		return nil, err
	}

	kept := make([]*Resource, 0, len(rows))
	for _, row := range rows {
		after, err := rowAfter(reflect.ValueOf(row).Elem(), order, boundary)
		if err != nil {
			return nil, err
		}
		if after {
			kept = append(kept, row)
		}
	}

	return kept, nil
}

// rowAfter reports whether a row sorts strictly after the boundary values in the
// order, with the same NULL placement the ORDER BY states.
func rowAfter(row reflect.Value, order []SortField, boundary []any) (bool, error) {
	for i, sf := range order {
		field := fieldValue(row, sf.Field)
		var bound reflect.Value
		if boundary[i] != nil {
			bound = reflect.ValueOf(boundary[i])
		} else {
			bound = reflect.Zero(reflect.PointerTo(field.Type()))
		}
		cmp, err := compareOrdered(field, bound, sf.Direction)
		if err != nil {
			return false, err
		}
		if cmp != 0 {
			return cmp > 0, nil
		}
	}

	return false, nil
}
