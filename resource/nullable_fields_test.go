package resource

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"cloud.google.com/go/spanner"
	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/httpio"
	"github.com/google/go-cmp/cmp"
)

const nullableResources = accesstypes.Resource("nullableResources")

// nullableResource is the resource the nullable-slice decoder tests decode into.
type nullableResource struct {
	ID    ccc.UUID           `spanner:"Id"`
	Tags  []string           `spanner:"Tags"`
	Bays  []int64            `spanner:"Bays"`
	Seal  []byte             `spanner:"Seal"`
	Note  *string            `spanner:"Note"`
	Alias spanner.NullString `spanner:"Alias"`
	Name  string             `spanner:"Name"`
}

func (nullableResource) Resource() accesstypes.Resource {
	return nullableResources
}

func (nullableResource) DefaultConfig() Config {
	return Config{}
}

// nullableRequest mirrors a generated patch request struct: nullable:"true" on the slice
// fields whose columns allow NULL, one of them sized too, a slice on a NOT NULL column
// with no tag, a pointer, a Spanner wrapper, and a string.
type nullableRequest struct {
	Tags  []string           `json:"tags"  nullable:"true" sqltype:"ARRAY<STRING(4)>"`
	Bays  []int64            `json:"bays"  nullable:"true"`
	Seal  []byte             `json:"seal"`
	Note  *string            `json:"note"`
	Alias spanner.NullString `json:"alias"`
	Name  string             `json:"name"`
}

// TestNullableFieldsOf pins the tag reader: the marked slice fields keyed by struct
// field, nil where the struct carries none, and the stale-struct guard on a value the
// generator never writes or a tag on a field that is not a slice.
func TestNullableFieldsOf(t *testing.T) {
	t.Parallel()

	type staleValue struct {
		Bays []int64 `json:"bays" nullable:"yes"`
	}
	type staleKind struct {
		Note *string `json:"note" nullable:"true"`
	}
	type staleScalar struct {
		Name string `json:"name" nullable:"true"`
	}

	tests := []struct {
		name    string
		typ     reflect.Type
		want    map[accesstypes.Field]struct{}
		wantErr string
	}{
		{name: "the marked slice fields, keyed by struct field", typ: reflect.TypeFor[nullableRequest](), want: map[accesstypes.Field]struct{}{"Tags": {}, "Bays": {}}},
		{name: "a struct with no tag reads nil", typ: reflect.TypeFor[limitRequest]()},
		{name: "a value other than true is the stale-struct guard", typ: reflect.TypeFor[staleValue](), wantErr: `nullable:"yes" on field Bays is not supported: regenerate this struct`},
		{name: "a tag on a pointer is the stale-struct guard", typ: reflect.TypeFor[staleKind](), wantErr: `nullable:"true" on field Note is not supported`},
		{name: "a tag on a scalar is the stale-struct guard", typ: reflect.TypeFor[staleScalar](), wantErr: `nullable:"true" on field Name is not supported`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := nullableFieldsOf(tt.typ)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("nullableFieldsOf() error = %v, want it to contain %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("nullableFieldsOf() error = %v", err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("nullableFieldsOf() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestNewSet_nullableGuard pins that the stale-struct guard fails Set construction on
// the enforced and the unenforced path alike, and says to regenerate.
func TestNewSet_nullableGuard(t *testing.T) {
	t.Parallel()

	type staleValue struct {
		Bays []int64 `json:"bays" nullable:"yes"`
	}

	tests := []struct {
		name      string
		construct func() error
		wantErr   string
	}{
		{
			name: "the enforced Set",
			construct: func() error {
				_, err := NewSet[nullableResource, staleValue](accesstypes.Create, accesstypes.Update)

				return err
			},
			wantErr: `nullable:"yes" on field Bays is not supported: regenerate this struct`,
		},
		{
			name: "the unenforced Set",
			construct: func() error {
				_, err := newUnenforcedSet[nullableResource, staleValue]()

				return err
			},
			wantErr: `nullable:"yes" on field Bays is not supported: regenerate this struct`,
		},
		{
			name: "a struct carrying the tag the generator writes constructs",
			construct: func() error {
				_, err := NewSet[nullableResource, nullableRequest](accesstypes.Create, accesstypes.Update)

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

// TestDecodeToPatch_nullableSlice pins the decode-time rule: a JSON null lands in a
// marked slice field as the nil slice, which the client writes as NULL, on a create and
// a PATCH alike and with nothing to size; a null into an unmarked slice, whose column is
// NOT NULL, keeps today's refusal, as a null into a string does; a null into a pointer
// or a Spanner wrapper is accepted as before; and an empty array is a value, never a
// null.
func TestDecodeToPatch_nullableSlice(t *testing.T) {
	t.Parallel()

	rSet, err := NewSet[nullableResource, nullableRequest](accesstypes.Create, accesstypes.Update)
	if err != nil {
		t.Fatalf("NewSet() error = %v", err)
	}
	mapper, err := NewRequestFieldMapper(new(nullableRequest))
	if err != nil {
		t.Fatalf("NewRequestFieldMapper() error = %v", err)
	}

	tests := []struct {
		name        string
		method      string
		perm        accesstypes.Permission
		body        string
		wantMessage string
		wantFields  []accesstypes.Field
		// wantValues are the decoded values by field; a nil slice pins a null.
		wantValues map[accesstypes.Field]any
	}{
		{
			name:       "a null into a marked slice is accepted as the nil slice",
			method:     http.MethodPost,
			perm:       accesstypes.Create,
			body:       `{"bays":null}`,
			wantFields: []accesstypes.Field{"Bays"},
			wantValues: map[accesstypes.Field]any{"Bays": []int64(nil)},
		},
		{
			name:       "a null into a marked sized slice has nothing to size",
			method:     http.MethodPost,
			perm:       accesstypes.Create,
			body:       `{"tags":null}`,
			wantFields: []accesstypes.Field{"Tags"},
			wantValues: map[accesstypes.Field]any{"Tags": []string(nil)},
		},
		{
			name:       "a PATCH accepts the null the same way",
			method:     http.MethodPatch,
			perm:       accesstypes.Update,
			body:       `{"bays":null}`,
			wantFields: []accesstypes.Field{"Bays"},
			wantValues: map[accesstypes.Field]any{"Bays": []int64(nil)},
		},
		{
			name:       "an empty array is a value, not a null",
			method:     http.MethodPost,
			perm:       accesstypes.Create,
			body:       `{"bays":[]}`,
			wantFields: []accesstypes.Field{"Bays"},
			wantValues: map[accesstypes.Field]any{"Bays": []int64{}},
		},
		{
			name:       "a value into a marked slice is stored whole",
			method:     http.MethodPost,
			perm:       accesstypes.Create,
			body:       `{"bays":[1,2]}`,
			wantFields: []accesstypes.Field{"Bays"},
			wantValues: map[accesstypes.Field]any{"Bays": []int64{1, 2}},
		},
		{
			name:        "a null into an unmarked slice is refused, its column being NOT NULL",
			method:      http.MethodPost,
			perm:        accesstypes.Create,
			body:        `{"seal":null}`,
			wantMessage: "seal cannot be null",
		},
		{
			name:        "a null into a string is refused as before",
			method:      http.MethodPost,
			perm:        accesstypes.Create,
			body:        `{"name":null}`,
			wantMessage: "name cannot be null",
		},
		{
			name:       "a null into a pointer is accepted as before",
			method:     http.MethodPost,
			perm:       accesstypes.Create,
			body:       `{"note":null}`,
			wantFields: []accesstypes.Field{"Note"},
			wantValues: map[accesstypes.Field]any{"Note": (*string)(nil)},
		},
		{
			name:       "a null into a Spanner wrapper is accepted as before",
			method:     http.MethodPost,
			perm:       accesstypes.Create,
			body:       `{"alias":null}`,
			wantFields: []accesstypes.Field{"Alias"},
			wantValues: map[accesstypes.Field]any{"Alias": spanner.NullString{}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := httptest.NewRequestWithContext(t.Context(), tt.method, "/", strings.NewReader(tt.body))
			patchSet, _, err := decodeToPatch[nullableResource, nullableRequest](rSet, mapper, r, nil, tt.perm)
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
			for field, want := range tt.wantValues {
				got := patchSet.Get(field)
				if !reflect.DeepEqual(got, want) {
					t.Errorf("patchSet.Get(%s) = %#v, want %#v", field, got, want)
				}
			}
		})
	}
}
