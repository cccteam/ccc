package resource

import (
	"crypto/sha256"
	"encoding/base64"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"aidanwoods.dev/go-paseto"
	"cloud.google.com/go/civil"
	"cloud.google.com/go/spanner"
	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/httpio"
	"github.com/google/go-cmp/cmp"
	"github.com/shopspring/decimal"
	"golang.org/x/crypto/hkdf"
)

// testCookieKey is 32 bytes of key material, base64 as an application configures it.
var testCookieKey = base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))

func mustCursorKey(t *testing.T, cookieKey string) *CursorKey {
	t.Helper()

	key, err := NewCursorKey(cookieKey)
	if err != nil {
		t.Fatalf("NewCursorKey() error = %v", err)
	}

	return key
}

func strPtr(s string) *string { return &s }

func TestCursorKey_sealAndOpen(t *testing.T) {
	t.Parallel()

	key := mustCursorKey(t, testCookieKey)
	want := cursor{Query: "7f4c", Direction: pageNext, Keys: []*string{strPtr("3"), nil, strPtr("0193e2a7-522c-708f-bfd0-4adf33486bb1")}}

	token, err := key.seal(want)
	if err != nil {
		t.Fatalf("seal() error = %v", err)
	}
	if !strings.HasPrefix(token, "v4.local.") {
		t.Errorf("seal() = %q, want a v4 local token", token)
	}
	// The boundary values are inside the ciphertext: nothing readable is in the URL.
	if strings.Contains(token, "0193e2a7") {
		t.Errorf("seal() leaks a boundary value: %q", token)
	}

	got, err := key.open(token)
	if err != nil {
		t.Fatalf("open() error = %v", err)
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("open() mismatch (-want +got):\n%s", diff)
	}
}

// sessionCookieDerivation reproduces session/cookie's key derivation over the
// same material: same hash and salt, the cookie info string.
func sessionCookieDerivation(t *testing.T, cookieKey string) paseto.V4SymmetricKey {
	t.Helper()

	material, err := base64.StdEncoding.DecodeString(cookieKey)
	if err != nil {
		t.Fatal(err)
	}
	derived := make([]byte, 32)
	if _, err := io.ReadFull(hkdf.New(sha256.New, material, []byte(cursorKeySalt), []byte("paseto-hkdf-info-v1")), derived); err != nil {
		t.Fatal(err)
	}
	key, err := paseto.V4SymmetricKeyFromBytes(derived)
	if err != nil {
		t.Fatal(err)
	}

	return key
}

func TestCursorKey_open_refusals(t *testing.T) {
	t.Parallel()

	key := mustCursorKey(t, testCookieKey)
	genuine, err := key.seal(cursor{Query: "q", Direction: pageNext, Keys: []*string{strPtr("1")}})
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name  string
		token func(t *testing.T) string
	}{
		{
			name: "an altered token",
			token: func(*testing.T) string {
				b := []byte(genuine)
				b[len(b)-1] ^= 0x01

				return string(b)
			},
		},
		{
			name: "a token sealed under another application's key",
			token: func(t *testing.T) string {
				other := mustCursorKey(t, base64.StdEncoding.EncodeToString([]byte("fedcba9876543210fedcba9876543210")))
				token, err := other.seal(cursor{Query: "q", Direction: pageNext, Keys: []*string{strPtr("1")}})
				if err != nil {
					t.Fatal(err)
				}

				return token
			},
		},
		{
			name: "a session cookie sealed from the same cookie key",
			token: func(t *testing.T) string {
				token, err := paseto.NewTokenFromClaimsJSON([]byte(`{"q":"q","d":1,"k":["1"]}`), nil)
				if err != nil {
					t.Fatal(err)
				}

				return token.V4Encrypt(sessionCookieDerivation(t, testCookieKey), nil)
			},
		},
		{
			name:  "a string that is not a token",
			token: func(*testing.T) string { return "page-2" },
		},
		{
			name: "a genuine token with an unknown direction",
			token: func(t *testing.T) string {
				token, err := paseto.NewTokenFromClaimsJSON([]byte(`{"q":"q","d":2,"k":["1"]}`), nil)
				if err != nil {
					t.Fatal(err)
				}

				return token.V4Encrypt(key.key, nil)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := key.open(tt.token(t))
			if err == nil {
				t.Fatal("open() expected an error, got nil")
			}
			if !httpio.HasBadRequest(err) {
				t.Errorf("open() error = %v, want a 400", err)
			}
		})
	}
}

func TestNewCursorKey_refusesUnusableMaterial(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		cookieKey string
	}{
		{name: "empty", cookieKey: ""},
		{name: "not base64", cookieKey: "not base64!"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if _, err := NewCursorKey(tt.cookieKey); err == nil {
				t.Error("NewCursorKey() expected an error, got nil")
			}
		})
	}
}

func TestCursorText_roundTrip(t *testing.T) {
	t.Parallel()

	stamp := time.Date(2027, 1, 15, 9, 0, 0, 123456789, time.UTC)
	id := mustUUIDFromString("0193e2a7-522c-708f-bfd0-4adf33486bb1")

	tests := []struct {
		name     string
		value    any
		wantText *string
		wantBack any
	}{
		{name: "string", value: "Kingfisher", wantText: strPtr("Kingfisher"), wantBack: "Kingfisher"},
		{name: "int64", value: int64(-3), wantText: strPtr("-3"), wantBack: int64(-3)},
		{name: "float64 at full precision", value: float64(1) / 3, wantText: strPtr("0.3333333333333333"), wantBack: float64(1) / 3},
		{name: "bool", value: true, wantText: strPtr("true"), wantBack: true},
		{name: "time at nanosecond precision", value: stamp, wantText: strPtr("2027-01-15T09:00:00.123456789Z"), wantBack: stamp},
		{name: "date", value: civil.Date{Year: 2026, Month: 12, Day: 1}, wantText: strPtr("2026-12-01"), wantBack: civil.Date{Year: 2026, Month: 12, Day: 1}},
		{name: "decimal as its decimal string", value: decimal.RequireFromString("24000.125"), wantText: strPtr("24000.125"), wantBack: decimal.RequireFromString("24000.125")},
		{name: "uuid as text", value: id, wantText: strPtr("0193e2a7-522c-708f-bfd0-4adf33486bb1"), wantBack: id},
		{name: "nil pointer is the NULL region", value: (*string)(nil), wantText: nil},
		{name: "pointer to a value", value: strPtr("note"), wantText: strPtr("note"), wantBack: "note"},
		{name: "invalid Null wrapper is the NULL region", value: spanner.NullString{}, wantText: nil},
		{name: "valid Null wrapper", value: spanner.NullString{StringVal: "x", Valid: true}, wantText: strPtr("x"), wantBack: "x"},
		{name: "valid NullUUID", value: ccc.NullUUID{UUID: id, Valid: true}, wantText: strPtr("0193e2a7-522c-708f-bfd0-4adf33486bb1"), wantBack: id},
		{name: "valid NullDecimal", value: decimal.NullDecimal{Decimal: decimal.RequireFromString("8000"), Valid: true}, wantText: strPtr("8000"), wantBack: decimal.RequireFromString("8000")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			text, err := cursorText(reflect.ValueOf(tt.value))
			if err != nil {
				t.Fatalf("cursorText() error = %v", err)
			}
			if (text == nil) != (tt.wantText == nil) || (text != nil && *text != *tt.wantText) {
				t.Fatalf("cursorText() = %v, want %v", deref(text), deref(tt.wantText))
			}
			if text == nil {
				return
			}

			back, err := cursorValue(*text, reflect.TypeOf(tt.value))
			if err != nil {
				t.Fatalf("cursorValue() error = %v", err)
			}
			if diff := cmp.Diff(tt.wantBack, back); diff != "" {
				t.Errorf("cursorValue() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func deref(s *string) string {
	if s == nil {
		return "<nil>"
	}

	return *s
}

func TestCursorText_unsupportedType(t *testing.T) {
	t.Parallel()

	if _, err := cursorText(reflect.ValueOf([]int{1})); err == nil {
		t.Error("cursorText() on a slice expected an error, got nil")
	}
}

func TestQueryHash(t *testing.T) {
	t.Parallel()

	hash := func() string {
		return queryHash("Missions", accesstypes.DomainScope("anvil"), "hazard:gt:2", []SortField{{Field: "Deadline", Direction: SortAscending}, {Field: "ID", Direction: SortAscending}}, "25")
	}
	base := hash()
	if again := hash(); again != base {
		t.Fatalf("queryHash() is not deterministic: %q then %q", base, again)
	}
	if len(base) != 16 {
		t.Errorf("queryHash() length = %d, want 16", len(base))
	}

	tests := []struct {
		name string
		hash string
	}{
		{name: "another resource", hash: queryHash("Ships", accesstypes.DomainScope("anvil"), "hazard:gt:2", []SortField{{Field: "Deadline", Direction: SortAscending}, {Field: "ID", Direction: SortAscending}}, "25")},
		{name: "another tenant", hash: queryHash("Missions", accesstypes.DomainScope("bastion"), "hazard:gt:2", []SortField{{Field: "Deadline", Direction: SortAscending}, {Field: "ID", Direction: SortAscending}}, "25")},
		{name: "another filter", hash: queryHash("Missions", accesstypes.DomainScope("anvil"), "hazard:gt:3", []SortField{{Field: "Deadline", Direction: SortAscending}, {Field: "ID", Direction: SortAscending}}, "25")},
		{name: "another direction", hash: queryHash("Missions", accesstypes.DomainScope("anvil"), "hazard:gt:2", []SortField{{Field: "Deadline", Direction: SortDescending}, {Field: "ID", Direction: SortAscending}}, "25")},
		{name: "another page size", hash: queryHash("Missions", accesstypes.DomainScope("anvil"), "hazard:gt:2", []SortField{{Field: "Deadline", Direction: SortAscending}, {Field: "ID", Direction: SortAscending}}, "50")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if tt.hash == base {
				t.Errorf("queryHash() with %s equals the base hash", tt.name)
			}
		})
	}
}
