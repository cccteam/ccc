package resource

import (
	"fmt"
	"math/big"
	"reflect"
	"strconv"
	"strings"
	"unicode/utf8"

	"cloud.google.com/go/spanner"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/httpio"
	"github.com/go-playground/errors/v5"
	"github.com/shopspring/decimal"
)

// ValueKind is how the patch decoder sizes a request field's value against its column's
// declared type: by code point, by byte, or by decimal digit. The generator classifies a
// source field's Go type into a kind with go/types and the runtime classifies the request
// struct's field with reflection; HasValueLimit is where the two agree on which pairs of
// kind and column type carry a rule.
type ValueKind int

const (
	// ValueKindOther is every type the decoder does not size: keys, integers, floats,
	// booleans, dates, timestamps, JSON, and the null wrappers of those.
	ValueKindOther ValueKind = iota
	// ValueKindString is a string-kinded type: string, a named string type, a pointer to
	// one, or spanner.NullString. Sized by code point against STRING(n), as Spanner
	// counts.
	ValueKindString
	// ValueKindBytes is []byte, sized by byte against BYTES(n).
	ValueKindBytes
	// ValueKindDecimal is decimal.Decimal, a pointer to one, decimal.NullDecimal, or
	// spanner.NullNumeric, sized by digit against NUMERIC.
	ValueKindDecimal
)

// GoogleSQL NUMERIC holds 38 digits of precision with 9 of scale: at most 29 digits
// before the decimal point and 9 after, once trailing zeros are trimmed.
const (
	numericIntegerDigits = 29
	numericScale         = 9
)

// The column type names the decoder sizes.
const (
	columnTypeString  = "STRING"
	columnTypeBytes   = "BYTES"
	columnTypeNumeric = "NUMERIC"
	columnTypeMax     = "MAX"
	arrayPrefix       = "ARRAY<"
)

var (
	nullStringType  = reflect.TypeFor[spanner.NullString]()
	nullNumericType = reflect.TypeFor[spanner.NullNumeric]()
	decimalType     = reflect.TypeFor[decimal.Decimal]()
	nullDecimalType = reflect.TypeFor[decimal.NullDecimal]()
)

// columnType is a parsed sqltype value: the scalar type name, the length a STRING(n) or
// BYTES(n) declares (zero for MAX and for every other type), and whether the column is
// an array of the scalar.
type columnType struct {
	name  string
	max   int
	array bool
}

// parseColumnType reads a Spanner type as INFORMATION_SCHEMA.COLUMNS spells it:
// STRING(64), BYTES(MAX), NUMERIC, INT64, ARRAY<STRING(4)>, and so on. It fails on an
// empty value, unbalanced brackets, a nested array, or a length that is not a positive
// integer.
func parseColumnType(sqltype string) (columnType, error) {
	s := strings.TrimSpace(sqltype)
	if s == "" {
		return columnType{}, errors.New("empty column type")
	}

	var ct columnType
	if strings.HasPrefix(s, arrayPrefix) {
		if !strings.HasSuffix(s, ">") {
			return columnType{}, errors.Newf("column type %q: unclosed ARRAY", sqltype)
		}
		ct.array = true
		s = strings.TrimSpace(s[len(arrayPrefix) : len(s)-1])
		if strings.HasPrefix(s, arrayPrefix) {
			return columnType{}, errors.Newf("column type %q: nested ARRAY", sqltype)
		}
	}

	name, length, hasLength := strings.Cut(s, "(")
	ct.name = strings.TrimSpace(name)
	if ct.name == "" {
		return columnType{}, errors.Newf("column type %q: missing type name", sqltype)
	}
	if !hasLength {
		return ct, nil
	}
	if !strings.HasSuffix(length, ")") {
		return columnType{}, errors.Newf("column type %q: unclosed length", sqltype)
	}
	length = strings.TrimSpace(length[:len(length)-1])
	if length == columnTypeMax {
		return ct, nil
	}
	n, err := strconv.Atoi(length)
	if err != nil || n <= 0 {
		return columnType{}, errors.Newf("column type %q: length %q is not a positive integer", sqltype, length)
	}
	ct.max = n

	return ct, nil
}

// DeclaredLength is the length a STRING(n) or BYTES(n) column declares, or the element
// of an array of one; zero for MAX, for every other type, and for a value that does not
// parse. The TypeScript generator emits it as the field's maxLength.
func DeclaredLength(sqltype string) int {
	ct, err := parseColumnType(sqltype)
	if err != nil {
		return 0
	}

	return ct.max
}

// valueLimit is one request field's rule: the kind it sizes, the bound where the kind
// has one, whether the bound applies per array element, and the wire name the refusal
// names.
type valueLimit struct {
	kind       ValueKind
	max        int
	perElement bool
	jsonField  string
}

// newValueLimit pairs a field's kind with its column's type. It fails exactly where the
// generator writes no tag: a type the package cannot parse, a kind the column type does
// not size, a STRING or BYTES with no declared length, or an array on one side only.
func newValueLimit(sqltype string, kind ValueKind, slice bool) (valueLimit, error) {
	ct, err := parseColumnType(sqltype)
	if err != nil {
		return valueLimit{}, err
	}
	if ct.array != slice {
		return valueLimit{}, errors.Newf("column type %q does not match the field's array shape", sqltype)
	}

	switch kind {
	case ValueKindString:
		if ct.name != columnTypeString || ct.max == 0 {
			return valueLimit{}, errors.Newf("column type %q has no rule for a string-kinded field", sqltype)
		}
	case ValueKindBytes:
		if ct.name != columnTypeBytes || ct.max == 0 {
			return valueLimit{}, errors.Newf("column type %q has no rule for a []byte field", sqltype)
		}
	case ValueKindDecimal:
		if ct.name != columnTypeNumeric {
			return valueLimit{}, errors.Newf("column type %q has no rule for a decimal field", sqltype)
		}
	default:
		return valueLimit{}, errors.Newf("column type %q has no rule for the field's type", sqltype)
	}

	return valueLimit{kind: kind, max: ct.max, perElement: ct.array}, nil
}

// HasValueLimit reports whether the decoder sizes a field of the given kind, or a slice
// of it, declared on a column of the given Spanner type. The generator writes the
// sqltype tag exactly where this is true, so a tag the runtime cannot pair with its
// field is a stale struct.
func HasValueLimit(sqltype string, kind ValueKind, slice bool) bool {
	_, err := newValueLimit(sqltype, kind, slice)

	return err == nil
}

// valueKindOf classifies a request struct field's type: the kind of its scalar, and
// whether the field is a slice of it. A pointer is its element, []byte is bytes rather
// than a slice of anything, and a slice of slices has no kind.
func valueKindOf(t reflect.Type) (kind ValueKind, slice bool) {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() == reflect.Slice && t.Elem().Kind() != reflect.Uint8 {
		elemKind, nested := valueKindOf(t.Elem())
		if nested {
			return ValueKindOther, false
		}

		return elemKind, true
	}

	return scalarValueKind(t), false
}

// scalarValueKind classifies a non-slice, non-pointer type.
func scalarValueKind(t reflect.Type) ValueKind {
	switch t {
	case nullStringType:
		return ValueKindString
	case decimalType, nullDecimalType, nullNumericType:
		return ValueKindDecimal
	}

	switch t.Kind() {
	case reflect.String:
		return ValueKindString
	case reflect.Slice:
		if t.Elem().Kind() == reflect.Uint8 {
			return ValueKindBytes
		}
	default:
	}

	return ValueKindOther
}

// valueLimitsOf reads the sqltype tags of a request struct into per-field limits, keyed
// by struct field; nil when the struct carries none. A tag the package cannot pair with
// the field's type fails construction: the generator writes the tag only where a rule
// exists, so the struct is stale.
func valueLimitsOf(t reflect.Type) (map[accesstypes.Field]valueLimit, error) {
	var limits map[accesstypes.Field]valueLimit
	for field := range t.Fields() {
		ft := FieldTagsFromStructTag(accesstypes.Field(field.Name), field.Tag)
		if ft.SQLType == "" {
			continue
		}

		kind, slice := valueKindOf(field.Type)
		limit, err := newValueLimit(ft.SQLType, kind, slice)
		if err != nil {
			return nil, errors.Wrapf(err, "sqltype:%q on field %s is not supported: regenerate this struct — the generator writes it only onto a field whose type the decoder sizes against that column type", ft.SQLType, field.Name)
		}
		limit.jsonField = ft.JSON
		if limits == nil {
			limits = make(map[accesstypes.Field]valueLimit)
		}
		limits[accesstypes.Field(field.Name)] = limit
	}

	return limits, nil
}

// checkValueLimits sizes the decoded fields against their columns' declared limits, in
// struct-field order, and answers 400 naming every field over its limit, so a form fixes
// everything in one round trip. Only fields the request carried are checked.
func checkValueLimits(limits map[accesstypes.Field]valueLimit, requestType reflect.Type, changes map[accesstypes.Field]any) error {
	if len(limits) == 0 {
		return nil
	}

	var over []string
	for _, f := range reflect.VisibleFields(requestType) {
		field := accesstypes.Field(f.Name)
		limit, ok := limits[field]
		if !ok {
			continue
		}
		value, ok := changes[field]
		if !ok {
			continue
		}
		if limit.exceeded(reflect.ValueOf(value)) {
			over = append(over, limit.message())
		}
	}

	if len(over) > 0 {
		return httpio.NewBadRequestMessage(strings.Join(over, "; "))
	}

	return nil
}

// exceeded reports whether a decoded value is over its limit. A nil pointer and an
// invalid null wrapper have nothing to size; an array is checked per element.
func (l valueLimit) exceeded(v reflect.Value) bool {
	v = derefValue(v)
	if !v.IsValid() {
		return false
	}
	if !l.perElement {
		return l.scalarExceeded(v)
	}

	for i := range v.Len() {
		if elem := derefValue(v.Index(i)); elem.IsValid() && l.scalarExceeded(elem) {
			return true
		}
	}

	return false
}

// scalarExceeded sizes one scalar of the limit's kind.
func (l valueLimit) scalarExceeded(v reflect.Value) bool {
	switch l.kind {
	case ValueKindString:
		if ns, ok := v.Interface().(spanner.NullString); ok {
			return ns.Valid && utf8.RuneCountInString(ns.StringVal) > l.max
		}

		return utf8.RuneCountInString(v.String()) > l.max
	case ValueKindBytes:
		return v.Len() > l.max
	case ValueKindDecimal:
		switch d := v.Interface().(type) {
		case decimal.Decimal:
			return decimalExceeded(d)
		case decimal.NullDecimal:
			return d.Valid && decimalExceeded(d.Decimal)
		case spanner.NullNumeric:
			return d.Valid && ratExceeded(&d.Numeric)
		}
	default:
	}

	return false
}

// derefValue follows pointers and interfaces to the value they hold; a nil yields an
// invalid Value.
func derefValue(v reflect.Value) reflect.Value {
	for v.IsValid() && (v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface) {
		if v.IsNil() {
			return reflect.Value{}
		}
		v = v.Elem()
	}

	return v
}

// decimalExceeded applies NUMERIC's bounds to an exact decimal: trailing zeros trimmed,
// at most 29 digits before the decimal point and 9 after. 1.1234567890 passes as
// 1.123456789; 1.1234567891 does not; 1e40 has 41 integer digits.
func decimalExceeded(d decimal.Decimal) bool {
	coefficient := new(big.Int).Abs(d.Coefficient())
	if coefficient.Sign() == 0 {
		return false
	}
	exponent := int(d.Exponent())

	ten := big.NewInt(10)
	remainder := new(big.Int)
	for {
		quotient, r := new(big.Int).QuoRem(coefficient, ten, remainder)
		if r.Sign() != 0 {
			break
		}
		coefficient = quotient
		exponent++
	}

	digits := len(coefficient.String())
	scale := max(0, -exponent)
	integerDigits := max(0, digits+exponent)

	return scale > numericScale || integerDigits > numericIntegerDigits
}

// ratExceeded applies the same bounds to an exact rational: it has a decimal form within
// the scale only when its reduced denominator divides 10^9, and then its digits are
// those of the numerator scaled to nine decimals.
func ratExceeded(r *big.Rat) bool {
	scaleUnit := new(big.Int).Exp(big.NewInt(10), big.NewInt(numericScale), nil)
	factor, remainder := new(big.Int).QuoRem(scaleUnit, r.Denom(), new(big.Int))
	if remainder.Sign() != 0 {
		return true
	}

	return decimalExceeded(decimal.NewFromBigInt(new(big.Int).Mul(r.Num(), factor), -numericScale))
}

// message is the refusal for a field over its limit, naming the wire field.
func (l valueLimit) message() string {
	var rule string
	switch l.kind {
	case ValueKindString:
		rule = fmt.Sprintf("limited to %s", plural(l.max, "character"))
	case ValueKindBytes:
		rule = fmt.Sprintf("limited to %s", plural(l.max, "byte"))
	case ValueKindDecimal:
		rule = fmt.Sprintf("limited to %d digits before the decimal point and %d after", numericIntegerDigits, numericScale)
	default:
	}

	if l.perElement {
		return fmt.Sprintf("%s: each value is %s", l.jsonField, rule)
	}

	return fmt.Sprintf("%s is %s", l.jsonField, rule)
}

// plural counts a unit: "1 character", "64 characters".
func plural(n int, unit string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, unit)
	}

	return fmt.Sprintf("%d %ss", n, unit)
}
