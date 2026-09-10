package resource

import (
	"crypto/sha256"
	"encoding"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"strconv"
	"strings"
	"time"

	"aidanwoods.dev/go-paseto"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/httpio"
	"github.com/go-playground/errors/v5"
	"golang.org/x/crypto/hkdf"
)

// The cursor key is derived from the application's cookie key material with HKDF,
// the way the session library derives its cookie key: same hash, same salt, a
// different info string. The two derivations share every byte but the info, so a
// cursor can never be presented as a session cookie or a cookie as a cursor, and
// rotating the cookie key ends outstanding cursors as it ends sessions. The salt
// must stay byte-identical to session/cookie's; the info is this package's own.
const (
	cursorKeySalt = "paseto-hkdf-salt-v1"
	cursorKeyInfo = "cursor"
)

// CursorKey seals and opens list cursors. An application builds one from its
// cookie key (NewCursorKey) and hands it to every query decoder
// (QueryDecoder.WithCursorKey); a decoder without one refuses paging past the
// first page.
type CursorKey struct {
	key paseto.V4SymmetricKey
}

// NewCursorKey derives the cursor sealing key from the application's
// base64-encoded cookie key, the value the session library is configured with.
func NewCursorKey(cookieKeyBase64 string) (*CursorKey, error) {
	keyMaterial, err := base64.StdEncoding.DecodeString(cookieKeyBase64)
	if err != nil {
		return nil, errors.Wrap(err, "base64.StdEncoding.DecodeString()")
	}
	if len(keyMaterial) == 0 {
		return nil, errors.New("resource.NewCursorKey: the cookie key is empty")
	}

	derived := make([]byte, 32)
	if _, err := io.ReadFull(hkdf.New(sha256.New, keyMaterial, []byte(cursorKeySalt), []byte(cursorKeyInfo)), derived); err != nil {
		return nil, errors.Wrap(err, "hkdf.New()")
	}
	key, err := paseto.V4SymmetricKeyFromBytes(derived)
	if err != nil {
		return nil, errors.Wrap(err, "paseto.V4SymmetricKeyFromBytes()")
	}

	return &CursorKey{key: key}, nil
}

// pageDirection is which way a cursor walks from its boundary row.
type pageDirection int

const (
	pageNext pageDirection = 1
	pagePrev pageDirection = -1
)

// cursor is the sealed token's payload: which query it belongs to, which way it
// walks, and the boundary row's values in the walk's total order, one per order
// field with the key last. A nil value marks a boundary row in a column's NULL
// region.
type cursor struct {
	Query     string        `json:"q"`
	Direction pageDirection `json:"d"`
	Keys      []*string     `json:"k"`
}

// seal encrypts and authenticates a cursor as a PASETO v4 local token.
func (k *CursorKey) seal(c cursor) (string, error) {
	claims, err := json.Marshal(c)
	if err != nil {
		return "", errors.Wrap(err, "json.Marshal()")
	}
	token, err := paseto.NewTokenFromClaimsJSON(claims, nil)
	if err != nil {
		return "", errors.Wrap(err, "paseto.NewTokenFromClaimsJSON()")
	}

	return token.V4Encrypt(k.key, nil), nil
}

// errInvalidCursor is the one client-facing answer to a cursor that does not
// open: altered, sealed under another key, or not a cursor at all.
var errInvalidCursor = httpio.NewBadRequestMessage("invalid cursor: restart the walk from the first page")

// open authenticates and decrypts a token into its cursor. Every failure is the
// same 400: the reason is never the client's business.
func (k *CursorKey) open(token string) (cursor, error) {
	parsed, err := paseto.NewParserWithoutExpiryCheck().ParseV4Local(k.key, token, nil)
	if err != nil {
		return cursor{}, errInvalidCursor
	}
	var c cursor
	if err := json.Unmarshal(parsed.ClaimsJSON(), &c); err != nil {
		return cursor{}, errInvalidCursor
	}
	if c.Direction != pageNext && c.Direction != pagePrev {
		return cursor{}, errInvalidCursor
	}

	return c, nil
}

// queryHash fingerprints the request a cursor belongs to: the resource, the
// scope, the filter, the total order, and the page size. A genuine cursor
// presented with any of them changed is refused, so a walk cannot be carried
// from one query, tenant, or page size to another.
func queryHash(res accesstypes.Resource, scope accesstypes.Scope, filter string, order []SortField, limit string) string {
	h := sha256.New()
	for _, part := range []string{string(res), scope.String(), filter, sortString(order), limit} {
		_, _ = h.Write([]byte(part))
		_, _ = h.Write([]byte{0})
	}

	return hex.EncodeToString(h.Sum(nil))[:16]
}

// sortString renders an order the way the sort parameter spells it.
func sortString(order []SortField) string {
	parts := make([]string, 0, len(order))
	for _, sf := range order {
		parts = append(parts, sf.Field+":"+string(sf.Direction))
	}

	return strings.Join(parts, ",")
}

// cursorText encodes one boundary value at full precision: a TextMarshaler
// (time.Time at nanosecond precision, civil.Date, decimal.Decimal, ccc.UUID)
// by its text, numbers and booleans by strconv. A nil pointer or an invalid
// Null* wrapper is the NULL region, encoded as nil.
func cursorText(v reflect.Value) (*string, error) {
	v, isNull := derefNullable(v)
	if isNull {
		return nil, nil
	}

	if marshaler, ok := v.Interface().(encoding.TextMarshaler); ok {
		text, err := marshaler.MarshalText()
		if err != nil {
			return nil, errors.Wrap(err, "encoding.TextMarshaler.MarshalText()")
		}
		s := string(text)

		return &s, nil
	}

	var s string
	switch v.Kind() {
	case reflect.String:
		s = v.String()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		s = strconv.FormatInt(v.Int(), 10)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		s = strconv.FormatUint(v.Uint(), 10)
	case reflect.Float32, reflect.Float64:
		s = strconv.FormatFloat(v.Float(), 'g', -1, 64)
	case reflect.Bool:
		s = strconv.FormatBool(v.Bool())
	default:
		return nil, errors.Newf("cursor value of type %s cannot be encoded; a sort column must be a text, number, boolean, time, date, decimal, or UUID column", v.Type())
	}

	return &s, nil
}

// derefNullable follows a pointer or a Null* wrapper (a struct with a Valid
// flag and one value field) to the value it holds, reporting the NULL region.
func derefNullable(v reflect.Value) (value reflect.Value, isNull bool) {
	for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return v, true
		}
		v = v.Elem()
	}
	if v.Kind() == reflect.Struct {
		if valid, ok := v.Type().FieldByName("Valid"); ok && valid.Type.Kind() == reflect.Bool {
			if !v.FieldByIndex(valid.Index).Bool() {
				return v, true
			}
			for i := range v.NumField() {
				if v.Type().Field(i).Name != "Valid" {
					return derefNullable(v.Field(i))
				}
			}
		}
	}

	return v, false
}

// nullableBaseType is the type a column's values take once pointers and Null*
// wrappers are stripped: the type a cursor value decodes to.
func nullableBaseType(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() == reflect.Struct {
		if valid, ok := t.FieldByName("Valid"); ok && valid.Type.Kind() == reflect.Bool {
			for i := range t.NumField() {
				if f := t.Field(i); f.Name != "Valid" {
					return nullableBaseType(f.Type)
				}
			}
		}
	}

	return t
}

// cursorValue decodes one boundary value to the column's Go type, the inverse
// of cursorText, so the predicate's parameter carries the column's own typing.
func cursorValue(text string, t reflect.Type) (any, error) {
	t = nullableBaseType(t)
	if t == reflect.TypeFor[time.Time]() {
		ts, err := time.Parse(time.RFC3339Nano, text)
		if err != nil {
			return nil, errInvalidCursor
		}

		return ts, nil
	}
	if reflect.PointerTo(t).Implements(reflect.TypeFor[encoding.TextUnmarshaler]()) {
		value := reflect.New(t)
		unmarshaler, ok := value.Interface().(encoding.TextUnmarshaler)
		if !ok || unmarshaler.UnmarshalText([]byte(text)) != nil {
			return nil, errInvalidCursor
		}

		return value.Elem().Interface(), nil
	}

	switch t.Kind() {
	case reflect.String:
		return text, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		i, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			return nil, errInvalidCursor
		}

		return i, nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		u, err := strconv.ParseUint(text, 10, 64)
		if err != nil {
			return nil, errInvalidCursor
		}

		return u, nil
	case reflect.Float32, reflect.Float64:
		f, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return nil, errInvalidCursor
		}

		return f, nil
	case reflect.Bool:
		b, err := strconv.ParseBool(text)
		if err != nil {
			return nil, errInvalidCursor
		}

		return b, nil
	default:
		return nil, fmt.Errorf("cursor value of type %s cannot be decoded", t)
	}
}
