package resource

import (
	"database/sql"
	"encoding/json"
	"math/big"
	"reflect"
	"time"

	"cloud.google.com/go/civil"
	"cloud.google.com/go/spanner"
	"github.com/go-playground/errors/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"
)

// This file is the value boundary of the Postgres runtime: how a statement's
// parameters and a write's column values leave for pgx, and how a column lands in a
// resource struct's field.
//
// The package's value types carry their storage form through the Spanner client's two
// interfaces, spanner.Encoder and spanner.Decoder (ccc.UUID, the nullable enums, the
// JSON types the generator writes), and its statement builders type their parameters
// the way Spanner wants them (paramValue). The Postgres runtime reads both: an Encoder's
// output is normalized to what pgx encodes, and a Decoder is fed the column as the
// database/sql driver value (a JSON column as its text, a UUID as its canonical string),
// the form the Decoders already accept. A type that is neither is left to pgx, which
// handles the base kinds, time.Time, and database/sql's Scanner and Valuer (civil.Date
// and the decimal types among them).

// postgresStatementArgs converts a statement's parameters into the named arguments pgx
// rewrites the statement's @name placeholders with.
func postgresStatementArgs(params map[string]any) (pgx.NamedArgs, error) {
	args := make(pgx.NamedArgs, len(params))
	for name, value := range params {
		arg, err := postgresValue(value)
		if err != nil {
			return nil, errors.Wrapf(err, "parameter %s", name)
		}
		args[name] = arg
	}

	return args, nil
}

// postgresValue normalizes one value for pgx: the Spanner-shaped values the statement
// builders and the patch resolution produce become the Go types pgx encodes, and a
// typed nil pointer is the untyped nil that encodes as NULL.
func postgresValue(value any) (any, error) {
	switch v := value.(type) {
	case nil:
		return nil, nil
	case *big.Rat:
		if v == nil {
			return nil, nil
		}

		return decimal.NewFromBigRat(v, decimalDivisionPrecision), nil
	case big.Rat:
		return decimal.NewFromBigRat(&v, decimalDivisionPrecision), nil
	case spanner.NullNumeric:
		if !v.Valid {
			return nil, nil
		}

		return decimal.NewFromBigRat(&v.Numeric, decimalDivisionPrecision), nil
	case spanner.NullJSON:
		return postgresJSON(v)
	case spanner.NullString:
		return nullValue(v.Valid, v.StringVal), nil
	case spanner.NullInt64:
		return nullValue(v.Valid, v.Int64), nil
	case spanner.NullFloat64:
		return nullValue(v.Valid, v.Float64), nil
	case spanner.NullBool:
		return nullValue(v.Valid, v.Bool), nil
	case spanner.NullTime:
		return nullValue(v.Valid, v.Time), nil
	case spanner.NullDate:
		return nullValue(v.Valid, v.Date), nil
	case spanner.Encoder:
		if rv := reflect.ValueOf(v); rv.Kind() == reflect.Pointer && rv.IsNil() {
			return nil, nil
		}
		encoded, err := v.EncodeSpanner()
		if err != nil {
			return nil, errors.Wrapf(err, "%T.EncodeSpanner()", v)
		}

		return postgresValue(encoded)
	}

	if rv := reflect.ValueOf(value); rv.Kind() == reflect.Pointer && rv.IsNil() {
		return nil, nil
	}

	return value, nil
}

// decimalDivisionPrecision bounds the digits a *big.Rat that is not a finite decimal
// converts to. The Rats the package binds come from decimal.Decimal.Rat(), which are
// finite, so the bound is never reached by them.
const decimalDivisionPrecision = 32

// nullValue returns value when valid and the untyped nil otherwise.
func nullValue[T any](valid bool, value T) any {
	if !valid {
		return nil
	}

	return value
}

// postgresJSON encodes a JSON column's value: the text of what Value marshals to, nil
// when the value is NULL.
func postgresJSON(v spanner.NullJSON) (any, error) {
	if !v.Valid {
		return nil, nil
	}
	b, err := json.Marshal(v.Value)
	if err != nil {
		return nil, errors.Wrap(err, "json.Marshal()")
	}

	return b, nil
}

// postgresDestination returns what pgx scans a column into for the field that dest
// points at: dest itself where pgx reads the type, an adapter where the type is one pgx
// does not (a spanner.Decoder).
func postgresDestination(dest reflect.Value, oid uint32) any {
	if adapter, ok := scanAdapter(dest, oid); ok {
		return adapter
	}
	if elem := dest.Type().Elem(); elem.Kind() == reflect.Pointer {
		// A nullable column is a pointer field, nil for NULL; a pointer to a type pgx does
		// not read is allocated for a value and read through the type's adapter.
		if _, ok := scanAdapter(reflect.New(elem.Elem()), oid); ok {
			return &nullablePointer{dest: dest, elem: elem.Elem(), oid: oid}
		}
	}

	return dest.Interface()
}

// scanAdapter returns the sql.Scanner that reads a column of the given type into the value
// dest points at, when the value's type is a spanner.Decoder. The Decoder is the package's
// own contract for a value type's storage form and comes before a Scanner the type may
// carry by embedding: ccc.NullUUID embeds ccc.UUID, whose promoted Scan would read the
// column and never mark the value valid.
func scanAdapter(dest reflect.Value, oid uint32) (any, bool) {
	if target, ok := dest.Interface().(spanner.Decoder); ok {
		return decoderScanner{target: target, json: oid == pgtype.JSONOID || oid == pgtype.JSONBOID}, true
	}

	return nil, false
}

// decoderScanner reads a column into a spanner.Decoder, handing it the column as the
// database/sql driver value: the form the package's Decoders accept. A JSON column's
// value is its text, as Spanner hands it; pgx delivers it as bytes.
type decoderScanner struct {
	target spanner.Decoder
	json   bool
}

// Scan implements sql.Scanner.
func (s decoderScanner) Scan(src any) error {
	if raw, ok := src.([]byte); ok && s.json {
		src = string(raw)
	}
	if err := s.target.DecodeSpanner(src); err != nil {
		return errors.Wrapf(err, "%T.DecodeSpanner()", s.target)
	}

	return nil
}

// nullablePointer reads a column into a pointer field whose type pgx does not read: NULL
// leaves the pointer nil, anything else allocates the value and scans into it.
type nullablePointer struct {
	dest reflect.Value
	elem reflect.Type
	oid  uint32
}

// Scan implements sql.Scanner.
func (n *nullablePointer) Scan(src any) error {
	if src == nil {
		n.dest.Elem().SetZero()

		return nil
	}
	allocated := reflect.New(n.elem)
	adapter, _ := scanAdapter(allocated, n.oid)
	if err := adapter.(sql.Scanner).Scan(src); err != nil { //nolint:forcetypeassert // scanAdapter returns sql.Scanners only
		return errors.Wrap(err, "sql.Scanner.Scan()")
	}
	n.dest.Elem().Set(allocated)

	return nil
}

// The Postgres types a bound value's CAST gives its parameter (postgresCast).
const (
	postgresText        = "TEXT"
	postgresTimestamptz = "TIMESTAMPTZ"
	postgresDate        = "DATE"
	postgresNumeric     = "NUMERIC"
	postgresBoolean     = "BOOLEAN"
	postgresBigint      = "BIGINT"
	postgresDouble      = "DOUBLE PRECISION"
)

// postgresCast names the Postgres type a bound value's CAST gives its parameter, "" where
// the value's type is not known (a NULL) and the context must type it.
func postgresCast(value any) string {
	switch value.(type) {
	case nil:
		return ""
	case time.Time, *time.Time:
		return postgresTimestamptz
	case civil.Date, *civil.Date:
		return postgresDate
	case *big.Rat, big.Rat, spanner.NullNumeric, decimal.Decimal, decimal.NullDecimal:
		return postgresNumeric
	}

	t := reflect.TypeOf(value)
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.String:
		return postgresText
	case reflect.Bool:
		return postgresBoolean
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32:
		return postgresBigint
	case reflect.Float32, reflect.Float64:
		return postgresDouble
	default:
		return ""
	}
}
