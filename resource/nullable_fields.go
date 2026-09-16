package resource

import (
	"reflect"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/go-playground/errors/v5"
)

// A slice-typed field takes its nullability from its column. The Spanner client reads a
// NULL BYTES or ARRAY column into a nil slice and writes a nil slice as NULL, and
// encoding/json carries nil as null, so the plain slice types a nullable column and a
// NOT NULL column alike: the generator checks the struct against the schema
// (validateNullability in resource/generation) and writes nullable:"true" onto the patch
// request-struct field where the column allows NULL. The decoder reads the marker here to
// accept a JSON null for the field (decodeToPatch); an unmarked slice keeps refusing
// null, as every other field whose type has no null form does.

// nullableFieldsOf reads the nullable tags of a request struct into the set of fields a
// null is accepted for, keyed by struct field; nil when the struct carries none. A tag
// with a value other than true, or on a field that is not a slice, fails construction:
// the generator writes the tag only there, so the struct is stale.
func nullableFieldsOf(t reflect.Type) (map[accesstypes.Field]struct{}, error) {
	var nullable map[accesstypes.Field]struct{}
	for field := range t.Fields() {
		ft := FieldTagsFromStructTag(accesstypes.Field(field.Name), field.Tag)
		if ft.Nullable == "" {
			continue
		}
		if ft.Nullable != trueStr || field.Type.Kind() != reflect.Slice {
			return nil, errors.Newf("nullable:%q on field %s is not supported: regenerate this struct — only nullable:%q is written, onto a slice-typed field whose column allows NULL", ft.Nullable, field.Name, trueStr)
		}
		if nullable == nil {
			nullable = make(map[accesstypes.Field]struct{})
		}
		nullable[accesstypes.Field(field.Name)] = struct{}{}
	}

	return nullable, nil
}
