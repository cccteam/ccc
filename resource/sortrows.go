package resource

import (
	"encoding"
	"reflect"
	"slices"
	"strings"
	"time"

	"cloud.google.com/go/civil"
	"github.com/go-playground/errors/v5"
	"github.com/shopspring/decimal"
)

// SortRows orders rows in memory by the given fields, the way the database orders
// a table: each field by its type, ascending with NULLs last or descending with
// NULLs first, stably so equal rows keep their yielded order. A computed
// resource's generated handler sorts the body's rows with it; a body that takes
// the sort (QuerySet.TakeSort) orders its own query instead.
func SortRows[T any](rows []*T, order []SortField) error {
	if len(order) == 0 {
		return nil
	}
	var sortErr error
	slices.SortStableFunc(rows, func(a, b *T) int {
		if sortErr != nil {
			return 0
		}
		va, vb := reflect.ValueOf(a).Elem(), reflect.ValueOf(b).Elem()
		for _, sf := range order {
			fa, fb := va.FieldByName(sf.Field), vb.FieldByName(sf.Field)
			if !fa.IsValid() {
				sortErr = errors.Newf("resource.SortRows: %s is not a field of %s", sf.Field, va.Type())

				return 0
			}
			cmp, err := compareOrdered(fa, fb, sf.Direction)
			if err != nil {
				sortErr = err

				return 0
			}
			if cmp != 0 {
				return cmp
			}
		}

		return 0
	})

	return sortErr
}

// compareOrdered compares two field values under a sort direction with the NULL
// placement the ORDER BY states: ascending NULLS LAST, descending NULLS FIRST.
func compareOrdered(a, b reflect.Value, direction SortDirection) (int, error) {
	va, aNull := derefNullable(a)
	vb, bNull := derefNullable(b)
	switch {
	case aNull && bNull:
		return 0, nil
	case aNull:
		if direction == SortDescending {
			return -1, nil
		}

		return 1, nil
	case bNull:
		if direction == SortDescending {
			return 1, nil
		}

		return -1, nil
	}
	cmp, err := compareValues(va, vb)
	if err != nil {
		return 0, err
	}
	if direction == SortDescending {
		return -cmp, nil
	}

	return cmp, nil
}

// compareValues compares two non-NULL values of the same type: numbers
// numerically, text lexically, booleans false before true, timestamps and dates
// in time, decimals by value, and any other text-marshaled type by its text.
func compareValues(a, b reflect.Value) (int, error) {
	if a.Type() != b.Type() {
		return 0, errors.Newf("resource: cannot compare %s with %s", a.Type(), b.Type())
	}
	switch x := a.Interface().(type) {
	case time.Time:
		if y, ok := b.Interface().(time.Time); ok {
			return x.Compare(y), nil
		}
	case civil.Date:
		if y, ok := b.Interface().(civil.Date); ok {
			return x.Compare(y), nil
		}
	case decimal.Decimal:
		if y, ok := b.Interface().(decimal.Decimal); ok {
			return x.Cmp(y), nil
		}
	}
	switch a.Kind() {
	case reflect.String:
		return strings.Compare(a.String(), b.String()), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return compareNumbers(a.Int(), b.Int()), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return compareNumbers(a.Uint(), b.Uint()), nil
	case reflect.Float32, reflect.Float64:
		return compareNumbers(a.Float(), b.Float()), nil
	case reflect.Bool:
		switch {
		case a.Bool() == b.Bool():
			return 0, nil
		case a.Bool():
			return 1, nil
		default:
			return -1, nil
		}
	default:
		ma, aok := a.Interface().(encoding.TextMarshaler)
		mb, bok := b.Interface().(encoding.TextMarshaler)
		if !aok || !bok {
			return 0, errors.Newf("resource: values of type %s cannot be ordered", a.Type())
		}
		ta, err := ma.MarshalText()
		if err != nil {
			return 0, errors.Wrap(err, "encoding.TextMarshaler.MarshalText()")
		}
		tb, err := mb.MarshalText()
		if err != nil {
			return 0, errors.Wrap(err, "encoding.TextMarshaler.MarshalText()")
		}

		return strings.Compare(string(ta), string(tb)), nil
	}
}

func compareNumbers[N int64 | uint64 | float64](a, b N) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

// sortableType reports whether a field's type can be ordered by: text, numbers,
// booleans, timestamps, dates, decimals, UUIDs, and pointers or Null* wrappers
// of them. A slice, a map, or a plain struct (a nested computed field) cannot.
func sortableType(t reflect.Type) bool {
	t = nullableBaseType(t)
	if t == reflect.TypeFor[time.Time]() || t == reflect.TypeFor[civil.Date]() || t == reflect.TypeFor[decimal.Decimal]() {
		return true
	}
	switch t.Kind() {
	case reflect.String, reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return true
	case reflect.Struct, reflect.Array:
		// A UUID and its kin: text-marshaled, so ordered by text.
		return reflect.PointerTo(t).Implements(reflect.TypeFor[encoding.TextUnmarshaler]()) &&
			t.Implements(reflect.TypeFor[encoding.TextMarshaler]())
	default:
		return false
	}
}
