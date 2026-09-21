package generation

import (
	"go/types"

	"github.com/cccteam/ccc/resource"
)

// Qualified names of the wrapper types the patch decoder sizes, and of the one
// standard-library JSON value, which it does not: json.RawMessage is a named byte slice
// to reflection but JSON on the wire, so no byte limit applies to it.
const (
	spannerNullStringType  = "cloud.google.com/go/spanner.NullString"
	spannerNullNumericType = "cloud.google.com/go/spanner.NullNumeric"
	decimalDecimalType     = "github.com/shopspring/decimal.Decimal"
	decimalNullDecimalType = "github.com/shopspring/decimal.NullDecimal"
	jsonRawMessageType     = "encoding/json.RawMessage"
)

// valueKindOf classifies a source field's Go type the way the resource package
// classifies the request struct's field at runtime (resource.ValueKind): the kind of
// the scalar, and whether the field is a slice of it. A pointer is its element, a named
// slice type is its underlying slice (type Marks []string is a slice of strings, as
// reflection reads it), []byte is bytes rather than a slice of anything, and a slice of
// slices has no kind. The named wrappers are structs, so reading the underlying type
// for the slice match leaves them to scalarValueKind, which resolves them by name.
func valueKindOf(t types.Type) (kind resource.ValueKind, slice bool) {
	t = types.Unalias(t)
	if p, ok := t.(*types.Pointer); ok {
		t = types.Unalias(p.Elem())
	}
	if s, ok := t.Underlying().(*types.Slice); ok && !isByte(s.Elem()) {
		elemKind, nested := valueKindOf(s.Elem())
		if nested {
			return resource.ValueKindOther, false
		}

		return elemKind, true
	}

	return scalarValueKind(t), false
}

// scalarValueKind classifies a non-slice, non-pointer type: the named wrappers by their
// qualified name (json.RawMessage among them, a JSON value and no byte slice to size),
// then a string-kinded underlying type or a byte slice.
func scalarValueKind(t types.Type) resource.ValueKind {
	if named, ok := t.(*types.Named); ok {
		switch qualifiedTypeName(named) {
		case spannerNullStringType:
			return resource.ValueKindString
		case decimalDecimalType, decimalNullDecimalType, spannerNullNumericType:
			return resource.ValueKindDecimal
		case jsonRawMessageType:
			return resource.ValueKindOther
		}
	}

	switch u := t.Underlying().(type) {
	case *types.Basic:
		if u.Info()&types.IsString != 0 {
			return resource.ValueKindString
		}
	case *types.Slice:
		if isByte(u.Elem()) {
			return resource.ValueKindBytes
		}
	}

	return resource.ValueKindOther
}

// isByte reports whether a type is byte (uint8) or an alias of it.
func isByte(t types.Type) bool {
	b, ok := types.Unalias(t).Underlying().(*types.Basic)

	return ok && b.Kind() == types.Uint8
}

// qualifiedTypeName is a named type's import path and name, "pkg/path.Name".
func qualifiedTypeName(named *types.Named) string {
	obj := named.Obj()
	if obj.Pkg() == nil {
		return obj.Name()
	}

	return obj.Pkg().Path() + "." + obj.Name()
}
