package resource

import (
	"math/big"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"cloud.google.com/go/spanner"
	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/httpio"
	"github.com/go-playground/errors/v5"
	"github.com/google/go-cmp/cmp"
	"github.com/shopspring/decimal"
)

const limitedResource = accesstypes.Resource("limitedResources")

// limitResource is the resource the value-limit decoder tests decode into.
type limitResource struct {
	ID     ccc.UUID        `spanner:"Id"`
	Name   string          `spanner:"Name"`
	Codes  []string        `spanner:"Codes"`
	Labels namedMarks      `spanner:"Labels"`
	Blob   []byte          `spanner:"Blob"`
	Fee    decimal.Decimal `spanner:"Fee"`
	Note   *string         `spanner:"Note"`
	Count  int64           `spanner:"Count"`
	Frozen string          `spanner:"Frozen"`
}

func (limitResource) Resource() accesstypes.Resource {
	return limitedResource
}

func (limitResource) DefaultConfig() Config {
	return Config{}
}

// limitRequest mirrors a generated patch request struct: one sqltype tag per rule the
// decoder applies, a named slice beside the unnamed one, an untagged integer, and an
// immutable sized field.
type limitRequest struct {
	Name   string          `json:"name"   sqltype:"STRING(4)"`
	Codes  []string        `json:"codes"  sqltype:"ARRAY<STRING(4)>"`
	Labels namedMarks      `json:"labels" sqltype:"ARRAY<STRING(4)>"`
	Blob   []byte          `json:"blob"   sqltype:"BYTES(4)"`
	Fee    decimal.Decimal `json:"fee"    sqltype:"NUMERIC"`
	Note   *string         `json:"note"   sqltype:"STRING(4)"`
	Count  int64           `json:"count"`
	Frozen string          `json:"frozen" immutable:"true"           sqltype:"STRING(4)"`
}

// namedCode is a named string type: string-kinded, sized like a string.
type namedCode string

// namedMarks is a named slice of strings: a slice, whatever its name, sized per element.
type namedMarks []string

func mustDecimal(s string) decimal.Decimal {
	return decimal.RequireFromString(s)
}

func stringPtr(s string) *string {
	return &s
}

func Test_parseColumnType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		sqltype string
		want    columnType
		wantErr bool
	}{
		{name: "a sized string", sqltype: "STRING(64)", want: columnType{name: "STRING", max: 64}},
		{name: "an unbounded string has no length", sqltype: "STRING(MAX)", want: columnType{name: "STRING"}},
		{name: "sized bytes", sqltype: "BYTES(4)", want: columnType{name: "BYTES", max: 4}},
		{name: "a numeric has no length", sqltype: "NUMERIC", want: columnType{name: "NUMERIC"}},
		{name: "an integer parses to its name", sqltype: "INT64", want: columnType{name: "INT64"}},
		{name: "an array of sized strings", sqltype: "ARRAY<STRING(4)>", want: columnType{name: "STRING", max: 4, array: true}},
		{name: "an array of numerics", sqltype: "ARRAY<NUMERIC>", want: columnType{name: "NUMERIC", array: true}},
		{name: "surrounding space is ignored", sqltype: " STRING( 8 ) ", want: columnType{name: "STRING", max: 8}},
		{name: "an empty value is an error", sqltype: "", wantErr: true},
		{name: "an unclosed length is an error", sqltype: "STRING(64", wantErr: true},
		{name: "a non-numeric length is an error", sqltype: "STRING(x)", wantErr: true},
		{name: "a zero length is an error", sqltype: "STRING(0)", wantErr: true},
		{name: "an unclosed array is an error", sqltype: "ARRAY<STRING(4)", wantErr: true},
		{name: "a nested array is an error", sqltype: "ARRAY<ARRAY<STRING(4)>>", wantErr: true},
		{name: "a bare length is an error", sqltype: "(64)", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseColumnType(tt.sqltype)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseColumnType(%q) error = %v, wantErr %v", tt.sqltype, err, tt.wantErr)
			}
			if diff := cmp.Diff(tt.want, got, cmp.AllowUnexported(columnType{})); diff != "" {
				t.Errorf("parseColumnType(%q) mismatch (-want +got):\n%s", tt.sqltype, diff)
			}
		})
	}
}

func TestDeclaredLength(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		sqltype string
		want    int
	}{
		{name: "a sized string", sqltype: "STRING(64)", want: 64},
		{name: "an array element's length", sqltype: "ARRAY<STRING(4)>", want: 4},
		{name: "sized bytes", sqltype: "BYTES(16)", want: 16},
		{name: "MAX has none", sqltype: "STRING(MAX)", want: 0},
		{name: "a numeric has none", sqltype: "NUMERIC", want: 0},
		{name: "an unparseable value has none", sqltype: "STRING(", want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := DeclaredLength(tt.sqltype); got != tt.want {
				t.Errorf("DeclaredLength(%q) = %d, want %d", tt.sqltype, got, tt.want)
			}
		})
	}
}

func TestHasValueLimit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		sqltype string
		kind    ValueKind
		slice   bool
		want    bool
	}{
		{name: "a string on STRING(n)", sqltype: "STRING(64)", kind: ValueKindString, want: true},
		{name: "a string on STRING(MAX) has no rule", sqltype: "STRING(MAX)", kind: ValueKindString, want: false},
		{name: "an unsized type on STRING(n) has no rule", sqltype: "STRING(36)", kind: ValueKindOther, want: false},
		{name: "a decimal on NUMERIC", sqltype: "NUMERIC", kind: ValueKindDecimal, want: true},
		{name: "a string on NUMERIC has no rule", sqltype: "NUMERIC", kind: ValueKindString, want: false},
		{name: "a decimal on STRING(n) has no rule", sqltype: "STRING(64)", kind: ValueKindDecimal, want: false},
		{name: "bytes on BYTES(n)", sqltype: "BYTES(4)", kind: ValueKindBytes, want: true},
		{name: "bytes on BYTES(MAX) has no rule", sqltype: "BYTES(MAX)", kind: ValueKindBytes, want: false},
		{name: "bytes on STRING(n) has no rule", sqltype: "STRING(4)", kind: ValueKindBytes, want: false},
		{name: "a string slice on ARRAY<STRING(n)>", sqltype: "ARRAY<STRING(4)>", kind: ValueKindString, slice: true, want: true},
		{name: "a string on ARRAY<STRING(n)> has no rule", sqltype: "ARRAY<STRING(4)>", kind: ValueKindString, want: false},
		{name: "a string slice on STRING(n) has no rule", sqltype: "STRING(4)", kind: ValueKindString, slice: true, want: false},
		{name: "a decimal slice on ARRAY<NUMERIC>", sqltype: "ARRAY<NUMERIC>", kind: ValueKindDecimal, slice: true, want: true},
		{name: "a bytes slice on ARRAY<BYTES(n)>", sqltype: "ARRAY<BYTES(4)>", kind: ValueKindBytes, slice: true, want: true},
		{name: "an integer column has no rule", sqltype: "INT64", kind: ValueKindOther, want: false},
		{name: "an unparseable type has no rule", sqltype: "STRING(", kind: ValueKindString, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := HasValueLimit(tt.sqltype, tt.kind, tt.slice); got != tt.want {
				t.Errorf("HasValueLimit(%q, %d, %v) = %v, want %v", tt.sqltype, tt.kind, tt.slice, got, tt.want)
			}
		})
	}
}

func Test_valueKindOf(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		typ       reflect.Type
		wantKind  ValueKind
		wantSlice bool
	}{
		{name: "string", typ: reflect.TypeFor[string](), wantKind: ValueKindString},
		{name: "a pointer to string", typ: reflect.TypeFor[*string](), wantKind: ValueKindString},
		{name: "a named string type", typ: reflect.TypeFor[namedCode](), wantKind: ValueKindString},
		{name: "spanner.NullString", typ: reflect.TypeFor[spanner.NullString](), wantKind: ValueKindString},
		{name: "a pointer to spanner.NullString", typ: reflect.TypeFor[*spanner.NullString](), wantKind: ValueKindString},
		{name: "a byte slice is bytes, not a slice", typ: reflect.TypeFor[[]byte](), wantKind: ValueKindBytes},
		{name: "a slice of byte slices", typ: reflect.TypeFor[[][]byte](), wantKind: ValueKindBytes, wantSlice: true},
		{name: "a string slice", typ: reflect.TypeFor[[]string](), wantKind: ValueKindString, wantSlice: true},
		{name: "a named slice of strings", typ: reflect.TypeFor[namedMarks](), wantKind: ValueKindString, wantSlice: true},
		{name: "a slice of a named string type", typ: reflect.TypeFor[[]namedCode](), wantKind: ValueKindString, wantSlice: true},
		{name: "a slice of string pointers", typ: reflect.TypeFor[[]*string](), wantKind: ValueKindString, wantSlice: true},
		{name: "a slice of NullString", typ: reflect.TypeFor[[]spanner.NullString](), wantKind: ValueKindString, wantSlice: true},
		{name: "decimal.Decimal", typ: reflect.TypeFor[decimal.Decimal](), wantKind: ValueKindDecimal},
		{name: "a pointer to decimal.Decimal", typ: reflect.TypeFor[*decimal.Decimal](), wantKind: ValueKindDecimal},
		{name: "decimal.NullDecimal", typ: reflect.TypeFor[decimal.NullDecimal](), wantKind: ValueKindDecimal},
		{name: "spanner.NullNumeric", typ: reflect.TypeFor[spanner.NullNumeric](), wantKind: ValueKindDecimal},
		{name: "a decimal slice", typ: reflect.TypeFor[[]decimal.Decimal](), wantKind: ValueKindDecimal, wantSlice: true},
		{name: "an integer has no kind", typ: reflect.TypeFor[int64](), wantKind: ValueKindOther},
		{name: "a bool has no kind", typ: reflect.TypeFor[bool](), wantKind: ValueKindOther},
		{name: "a UUID has no kind", typ: reflect.TypeFor[ccc.UUID](), wantKind: ValueKindOther},
		{name: "a time has no kind", typ: reflect.TypeFor[time.Time](), wantKind: ValueKindOther},
		{name: "a slice of slices has no kind", typ: reflect.TypeFor[[][]string](), wantKind: ValueKindOther},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			kind, slice := valueKindOf(tt.typ)
			if kind != tt.wantKind || slice != tt.wantSlice {
				t.Errorf("valueKindOf(%s) = (%d, %v), want (%d, %v)", tt.typ, kind, slice, tt.wantKind, tt.wantSlice)
			}
		})
	}
}

func Test_valueLimit_exceeded(t *testing.T) {
	t.Parallel()

	str4 := valueLimit{kind: ValueKindString, max: 4}
	strs4 := valueLimit{kind: ValueKindString, max: 4, perElement: true}
	bytes4 := valueLimit{kind: ValueKindBytes, max: 4}
	byteSlices4 := valueLimit{kind: ValueKindBytes, max: 4, perElement: true}
	numeric := valueLimit{kind: ValueKindDecimal}
	numerics := valueLimit{kind: ValueKindDecimal, perElement: true}

	tests := []struct {
		name  string
		limit valueLimit
		value any
		want  bool
	}{
		// Strings count code points, as Spanner does.
		{name: "four ASCII characters fit STRING(4)", limit: str4, value: "ABCD", want: false},
		{name: "five ASCII characters exceed STRING(4)", limit: str4, value: "ABCDE", want: true},
		{name: "four CJK characters (twelve bytes) fit STRING(4)", limit: str4, value: "日本語字", want: false},
		{name: "five CJK characters exceed STRING(4)", limit: str4, value: "日本語字五", want: true},
		{name: "four characters plus a combining mark are five code points", limit: str4, value: "abcé", want: true},
		{name: "four astral characters fit STRING(4)", limit: str4, value: "😀😀😀😀", want: false},
		{name: "five astral characters exceed STRING(4)", limit: str4, value: "😀😀😀😀😀", want: true},
		{name: "an empty string fits", limit: str4, value: "", want: false},
		{name: "a named string type is sized as a string", limit: str4, value: namedCode("ABCDE"), want: true},
		{name: "a nil pointer has nothing to size", limit: str4, value: (*string)(nil), want: false},
		{name: "a pointer to a long string exceeds", limit: str4, value: stringPtr("ABCDE"), want: true},
		{name: "an invalid NullString has nothing to size", limit: str4, value: spanner.NullString{}, want: false},
		{name: "a valid NullString over the limit exceeds", limit: str4, value: spanner.NullString{StringVal: "ABCDE", Valid: true}, want: true},
		{name: "a valid NullString within the limit fits", limit: str4, value: spanner.NullString{StringVal: "ABCD", Valid: true}, want: false},
		// Bytes count bytes.
		{name: "four bytes fit BYTES(4)", limit: bytes4, value: []byte("ABCD"), want: false},
		{name: "five bytes exceed BYTES(4)", limit: bytes4, value: []byte("ABCDE"), want: true},
		{name: "a nil byte slice fits", limit: bytes4, value: []byte(nil), want: false},
		// Arrays are checked per element.
		{name: "every element within the limit fits", limit: strs4, value: []string{"AB", "ABCD"}, want: false},
		{name: "one element over the limit exceeds", limit: strs4, value: []string{"AB", "ABCDE"}, want: true},
		{name: "a nil element is skipped and a long one exceeds", limit: strs4, value: []*string{nil, stringPtr("ABCDE")}, want: true},
		{name: "a nil array fits", limit: strs4, value: []string(nil), want: false},
		{name: "an invalid NullString element fits", limit: strs4, value: []spanner.NullString{{}}, want: false},
		{name: "a long byte slice element exceeds", limit: byteSlices4, value: [][]byte{[]byte("AB"), []byte("ABCDE")}, want: true},
		// NUMERIC: 29 integer digits, 9 decimals, trailing zeros trimmed first.
		{name: "nine decimals fit", limit: numeric, value: mustDecimal("1.123456789"), want: false},
		{name: "ten decimals exceed", limit: numeric, value: mustDecimal("1.1234567891"), want: true},
		{name: "ten decimals ending in zero are nine", limit: numeric, value: mustDecimal("1.1234567890"), want: false},
		{name: "twenty-nine integer digits fit", limit: numeric, value: mustDecimal("99999999999999999999999999999"), want: false},
		{name: "thirty integer digits exceed", limit: numeric, value: mustDecimal("100000000000000000000000000000"), want: true},
		{name: "an exponent form counts its integer digits", limit: numeric, value: mustDecimal("1e40"), want: true},
		{name: "a negative with ten decimals exceeds", limit: numeric, value: mustDecimal("-1.1234567891"), want: true},
		{name: "a negative with twenty-nine integer digits fits", limit: numeric, value: mustDecimal("-99999999999999999999999999999"), want: false},
		{name: "zero fits", limit: numeric, value: mustDecimal("0"), want: false},
		{name: "zero with nine decimals fits", limit: numeric, value: mustDecimal("0.000000000"), want: false},
		{name: "a tenth decimal on a fraction exceeds", limit: numeric, value: mustDecimal("0.0000000001"), want: true},
		{name: "a nil decimal pointer has nothing to size", limit: numeric, value: (*decimal.Decimal)(nil), want: false},
		{name: "an invalid NullDecimal has nothing to size", limit: numeric, value: decimal.NullDecimal{}, want: false},
		{name: "a valid NullDecimal with ten decimals exceeds", limit: numeric, value: decimal.NullDecimal{Decimal: mustDecimal("1.1234567891"), Valid: true}, want: true},
		{name: "a rational with a terminating ninth decimal fits", limit: numeric, value: spanner.NullNumeric{Numeric: *big.NewRat(1, 8), Valid: true}, want: false},
		{name: "a rational needing a tenth decimal exceeds", limit: numeric, value: spanner.NullNumeric{Numeric: *big.NewRat(1, 1024), Valid: true}, want: true},
		{name: "a repeating rational exceeds", limit: numeric, value: spanner.NullNumeric{Numeric: *big.NewRat(1, 3), Valid: true}, want: true},
		{name: "a rational with thirty integer digits exceeds", limit: numeric, value: spanner.NullNumeric{Numeric: *new(big.Rat).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(29), nil)), Valid: true}, want: true},
		{name: "an invalid NullNumeric has nothing to size", limit: numeric, value: spanner.NullNumeric{}, want: false},
		{name: "a decimal array element with ten decimals exceeds", limit: numerics, value: []decimal.Decimal{mustDecimal("1"), mustDecimal("1.1234567891")}, want: true},
		{name: "a decimal array within bounds fits", limit: numerics, value: []decimal.Decimal{mustDecimal("1"), mustDecimal("2.5")}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.limit.exceeded(reflect.ValueOf(tt.value)); got != tt.want {
				t.Errorf("exceeded(%#v) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

func Test_valueLimit_message(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		limit valueLimit
		want  string
	}{
		{name: "a sized string", limit: valueLimit{kind: ValueKindString, max: 64, jsonField: "name"}, want: "name is limited to 64 characters"},
		{name: "a one-character string", limit: valueLimit{kind: ValueKindString, max: 1, jsonField: "code"}, want: "code is limited to 1 character"},
		{name: "a string array names each value", limit: valueLimit{kind: ValueKindString, max: 4, perElement: true, jsonField: "codes"}, want: "codes: each value is limited to 4 characters"},
		{name: "sized bytes", limit: valueLimit{kind: ValueKindBytes, max: 4, jsonField: "blob"}, want: "blob is limited to 4 bytes"},
		{name: "a numeric", limit: valueLimit{kind: ValueKindDecimal, jsonField: "fee"}, want: "fee is limited to 29 digits before the decimal point and 9 after"},
		{name: "a numeric array names each value", limit: valueLimit{kind: ValueKindDecimal, perElement: true, jsonField: "fees"}, want: "fees: each value is limited to 29 digits before the decimal point and 9 after"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.limit.message(); got != tt.want {
				t.Errorf("message() = %q, want %q", got, tt.want)
			}
		})
	}
}

func Test_valueLimitsOf(t *testing.T) {
	t.Parallel()

	got, err := valueLimitsOf(reflect.TypeFor[limitRequest]())
	if err != nil {
		t.Fatalf("valueLimitsOf() error = %v", err)
	}
	want := map[accesstypes.Field]valueLimit{
		"Name":   {kind: ValueKindString, max: 4, jsonField: "name"},
		"Codes":  {kind: ValueKindString, max: 4, perElement: true, jsonField: "codes"},
		"Labels": {kind: ValueKindString, max: 4, perElement: true, jsonField: "labels"},
		"Blob":   {kind: ValueKindBytes, max: 4, jsonField: "blob"},
		"Fee":    {kind: ValueKindDecimal, jsonField: "fee"},
		"Note":   {kind: ValueKindString, max: 4, jsonField: "note"},
		"Frozen": {kind: ValueKindString, max: 4, jsonField: "frozen"},
	}
	if diff := cmp.Diff(want, got, cmp.AllowUnexported(valueLimit{})); diff != "" {
		t.Errorf("valueLimitsOf() mismatch (-want +got):\n%s", diff)
	}
}

// TestNewSet_valueLimitGuard pins the stale-struct guard: a sqltype tag the runtime
// cannot pair with its field fails Set construction, on the enforced and the unenforced
// path alike, and says to regenerate.
func TestNewSet_valueLimitGuard(t *testing.T) {
	t.Parallel()

	type taggedInteger struct {
		Count int64 `json:"count" sqltype:"STRING(4)"`
	}
	type taggedMax struct {
		Name string `json:"name" sqltype:"STRING(MAX)"`
	}
	type taggedGarbage struct {
		Name string `json:"name" sqltype:"STRING("`
	}
	type taggedArrayOnScalar struct {
		Name string `json:"name" sqltype:"ARRAY<STRING(4)>"`
	}
	type taggedScalarOnSlice struct {
		Names []string `json:"names" sqltype:"STRING(4)"`
	}
	type untagged struct {
		Name string `json:"name"`
	}

	tests := []struct {
		name      string
		construct func() error
		wantErr   string
	}{
		{
			name: "a tag on an integer field",
			construct: func() error {
				_, err := NewSet[limitResource, taggedInteger](accesstypes.Create)

				return err
			},
			wantErr: `sqltype:"STRING(4)" on field Count is not supported: regenerate this struct`,
		},
		{
			name: "a MAX column carries no rule",
			construct: func() error {
				_, err := NewSet[limitResource, taggedMax](accesstypes.Create)

				return err
			},
			wantErr: `sqltype:"STRING(MAX)" on field Name is not supported`,
		},
		{
			name: "an unparseable value",
			construct: func() error {
				_, err := NewSet[limitResource, taggedGarbage](accesstypes.Create)

				return err
			},
			wantErr: `sqltype:"STRING(" on field Name is not supported`,
		},
		{
			name: "an array type on a scalar field",
			construct: func() error {
				_, err := NewSet[limitResource, taggedArrayOnScalar](accesstypes.Create)

				return err
			},
			wantErr: "does not match the field's array shape",
		},
		{
			name: "a scalar type on a slice field",
			construct: func() error {
				_, err := NewSet[limitResource, taggedScalarOnSlice](accesstypes.Create)

				return err
			},
			wantErr: "does not match the field's array shape",
		},
		{
			name: "the unenforced set guards the same way",
			construct: func() error {
				_, err := NewStructDecoder[taggedInteger]()

				return err
			},
			wantErr: `sqltype:"STRING(4)" on field Count is not supported`,
		},
		{
			name: "a struct with no tags constructs",
			construct: func() error {
				_, err := NewSet[limitResource, untagged](accesstypes.Create)

				return err
			},
		},
		{
			name: "the paired tags construct",
			construct: func() error {
				_, err := NewSet[limitResource, limitRequest](accesstypes.Create, accesstypes.Update)

				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.construct()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("construct() error = %v, want nil", err)
				}

				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("construct() error = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

// TestDecodeToPatch_valueLimits pins the decode-time refusal: one 400 naming every
// field over its limit in struct order, only for fields the request carries, after the
// unknown, immutable, and null refusals and before the validator.
func TestDecodeToPatch_valueLimits(t *testing.T) {
	t.Parallel()

	rSet, err := NewSet[limitResource, limitRequest](accesstypes.Create, accesstypes.Update)
	if err != nil {
		t.Fatalf("NewSet() error = %v", err)
	}
	mapper, err := NewRequestFieldMapper(new(limitRequest))
	if err != nil {
		t.Fatalf("NewRequestFieldMapper() error = %v", err)
	}
	validatorRan := errors.New("validator ran")
	failing := &validateMock{
		validateFunc: func(any) error {
			return validatorRan
		},
		validatePartialFunc: func(any, ...string) error {
			return validatorRan
		},
	}

	const (
		nameLimit   = "name is limited to 4 characters"
		codesLimit  = "codes: each value is limited to 4 characters"
		labelsLimit = "labels: each value is limited to 4 characters"
		blobLimit   = "blob is limited to 4 bytes"
		feeLimit    = "fee is limited to 29 digits before the decimal point and 9 after"
		noteLimit   = "note is limited to 4 characters"
	)

	tests := []struct {
		name        string
		method      string
		perm        accesstypes.Permission
		body        string
		validate    ValidatorFunc
		wantMessage string
		wantFields  []accesstypes.Field
	}{
		{
			name:       "values within every limit decode",
			method:     http.MethodPost,
			perm:       accesstypes.Create,
			body:       `{"name":"ABCD","codes":["AB","ABCD"],"blob":"QUJDRA==","fee":1.123456789,"note":"日本語字","count":3}`,
			wantFields: []accesstypes.Field{"Name", "Codes", "Blob", "Fee", "Note", "Count"},
		},
		{
			name:        "a string over its column's length names the field",
			method:      http.MethodPost,
			perm:        accesstypes.Create,
			body:        `{"name":"ABCDE","count":3}`,
			wantMessage: nameLimit,
		},
		{
			name:        "every offending field is named in struct order whatever the body's order",
			method:      http.MethodPost,
			perm:        accesstypes.Create,
			body:        `{"fee":1.1234567891,"codes":["ABCDE"],"count":3,"name":"ABCDE"}`,
			wantMessage: nameLimit + "; " + codesLimit + "; " + feeLimit,
		},
		{
			name:        "a byte slice over its column's length names the field in bytes",
			method:      http.MethodPost,
			perm:        accesstypes.Create,
			body:        `{"blob":"QUJDREU="}`,
			wantMessage: blobLimit,
		},
		{
			name:        "a named slice is sized per element like the unnamed one",
			method:      http.MethodPost,
			perm:        accesstypes.Create,
			body:        `{"labels":["AB","ABCDE"]}`,
			wantMessage: labelsLimit,
		},
		{
			name:       "a named slice within its element limit decodes",
			method:     http.MethodPost,
			perm:       accesstypes.Create,
			body:       `{"labels":["AB","ABCD"]}`,
			wantFields: []accesstypes.Field{"Labels"},
		},
		{
			name:        "a decimal sent as a string is sized the same",
			method:      http.MethodPost,
			perm:        accesstypes.Create,
			body:        `{"fee":"100000000000000000000000000000"}`,
			wantMessage: feeLimit,
		},
		{
			name:       "a null into a nullable sized field has nothing to size",
			method:     http.MethodPost,
			perm:       accesstypes.Create,
			body:       `{"note":null}`,
			wantFields: []accesstypes.Field{"Note"},
		},
		{
			name:        "a long value into a nullable sized field is refused",
			method:      http.MethodPost,
			perm:        accesstypes.Create,
			body:        `{"note":"ABCDE"}`,
			wantMessage: noteLimit,
		},
		{
			name:       "a PATCH checks only the fields it carries",
			method:     http.MethodPatch,
			perm:       accesstypes.Update,
			body:       `{"count":3}`,
			wantFields: []accesstypes.Field{"Count"},
		},
		{
			name:        "a PATCH is sized like a create",
			method:      http.MethodPatch,
			perm:        accesstypes.Update,
			body:        `{"name":"ABCDE"}`,
			wantMessage: nameLimit,
		},
		{
			name:        "the immutable refusal comes first",
			method:      http.MethodPatch,
			perm:        accesstypes.Update,
			body:        `{"frozen":"ABCDE","name":"ABCDE"}`,
			wantMessage: "json field frozen is immutable",
		},
		{
			name:        "the null refusal comes first",
			method:      http.MethodPost,
			perm:        accesstypes.Create,
			body:        `{"name":null,"codes":["ABCDE"]}`,
			wantMessage: "name cannot be null",
		},
		{
			name:        "the unknown-field refusal comes first",
			method:      http.MethodPost,
			perm:        accesstypes.Create,
			body:        `{"bogus":1,"name":"ABCDE"}`,
			wantMessage: "invalid field in json - bogus",
		},
		{
			name:        "the limit is answered before the validator runs",
			method:      http.MethodPost,
			perm:        accesstypes.Create,
			body:        `{"name":"ABCDE"}`,
			validate:    failing,
			wantMessage: nameLimit,
		},
		{
			name:        "a body within its limits reaches the validator",
			method:      http.MethodPost,
			perm:        accesstypes.Create,
			body:        `{"name":"ABCD"}`,
			validate:    failing,
			wantMessage: "failed validating the request",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := httptest.NewRequestWithContext(t.Context(), tt.method, "/", strings.NewReader(tt.body))
			patchSet, _, err := decodeToPatch[limitResource, limitRequest](rSet, mapper, r, tt.validate, tt.perm)
			if tt.wantMessage != "" {
				if err == nil {
					t.Fatalf("decodeToPatch() error = nil, want message %q", tt.wantMessage)
				}
				if got := httpio.Message(err); got != tt.wantMessage {
					t.Errorf("httpio.Message() = %q, want %q", got, tt.wantMessage)
				}

				return
			}
			if err != nil {
				t.Fatalf("decodeToPatch() error = %v", err)
			}
			if diff := cmp.Diff(tt.wantFields, patchSet.Fields()); diff != "" {
				t.Errorf("patchSet.Fields() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
