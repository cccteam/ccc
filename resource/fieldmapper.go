package resource

import (
	"reflect"
	"strings"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/go-playground/errors/v5"
)

// RequestFieldMapper provides a mapping between JSON field names (from struct tags)
// and their corresponding Go struct field names. A field's former wire name (its
// formerly tag) maps to the field as its current name does, so a request naming the
// former name reaches the same field.
type RequestFieldMapper struct {
	jsonTagToFields map[string]accesstypes.Field
	fieldToJSONName map[accesstypes.Field]string
	// formerToJSONName maps each former wire name to the field's current one, for the
	// body rewrite; nil when no field declares one.
	formerToJSONName map[string]string
	fields           []accesstypes.Field
}

// NewRequestFieldMapper creates a new RequestFieldMapper by inspecting the struct tags of the provided value.
func NewRequestFieldMapper(v any) (*RequestFieldMapper, error) {
	mapper, err := tagToFieldMap(v)
	if err != nil {
		return nil, err
	}

	return mapper, nil
}

// StructFieldName retrieves the Go struct field name for a given JSON tag, the
// field's current wire name or its former one.
func (f *RequestFieldMapper) StructFieldName(jsonTag string) (accesstypes.Field, bool) {
	fieldName, ok := f.jsonTagToFields[jsonTag]

	return fieldName, ok
}

// Len returns the number of mapped names, former names included.
func (f *RequestFieldMapper) Len() int {
	return len(f.jsonTagToFields)
}

// Fields returns a slice of all Go struct field names.
func (f *RequestFieldMapper) Fields() []accesstypes.Field {
	return f.fields
}

// JSONNames returns the mapping from Go struct field names to their primary
// JSON names — a field without a json tag maps to its own name.
func (f *RequestFieldMapper) JSONNames() map[accesstypes.Field]string {
	return f.fieldToJSONName
}

// FormerNames returns the mapping from each field's former wire name to its current
// one; empty when no field declares a former name.
func (f *RequestFieldMapper) FormerNames() map[string]string {
	return f.formerToJSONName
}

func tagToFieldMap(v any) (*RequestFieldMapper, error) {
	vType := reflect.TypeOf(v)

	if vType.Kind() == reflect.Pointer {
		vType = vType.Elem()
	}
	if vType.Kind() != reflect.Struct {
		return nil, errors.Newf("argument v must be a struct, received %v", vType.Kind())
	}

	mapper := &RequestFieldMapper{
		jsonTagToFields: make(map[string]accesstypes.Field),
		fieldToJSONName: make(map[accesstypes.Field]string),
		fields:          make([]accesstypes.Field, 0, vType.NumField()),
	}
	for _, field := range reflect.VisibleFields(vType) {
		tag := field.Tag.Get(jsonTagKey)
		if tag == "" {
			if _, ok := mapper.jsonTagToFields[field.Name]; ok {
				return nil, errors.Newf("field name %s collides with another field tag", field.Name)
			}
			mapper.jsonTagToFields[field.Name] = accesstypes.Field(field.Name)
			mapper.fieldToJSONName[accesstypes.Field(field.Name)] = field.Name
			mapper.fields = append(mapper.fields, accesstypes.Field(field.Name))
			if lowerFieldName := strings.ToLower(field.Name); lowerFieldName != field.Name {
				if _, ok := mapper.jsonTagToFields[lowerFieldName]; ok {
					return nil, errors.Newf("field name %s has multiple matches", field.Name)
				}
				mapper.jsonTagToFields[lowerFieldName] = accesstypes.Field(field.Name)
			}

			continue
		}

		if before, _, found := strings.Cut(tag, ","); found {
			tag = before
		}

		if tag == "-" {
			continue
		}

		if _, ok := mapper.jsonTagToFields[tag]; ok {
			return nil, errors.Newf("tag %s has multiple matches", tag)
		}
		mapper.jsonTagToFields[tag] = accesstypes.Field(field.Name)
		mapper.fieldToJSONName[accesstypes.Field(field.Name)] = tag
		mapper.fields = append(mapper.fields, accesstypes.Field(field.Name))

		if err := mapper.addFormerName(accesstypes.Field(field.Name), field.Tag.Get(formerlyTagKey), tag); err != nil {
			return nil, err
		}
	}

	return mapper, nil
}

// addFormerName registers a field's former wire name beside its current one: a
// request naming either reaches the field. A former name that is another field's
// name, current or former, is refused, as the generator refuses it.
func (f *RequestFieldMapper) addFormerName(field accesstypes.Field, former, tag string) error {
	if former == "" {
		return nil
	}
	if _, ok := f.jsonTagToFields[former]; ok {
		return errors.Newf("former name %s of field %s collides with another field's name", former, field)
	}
	f.jsonTagToFields[former] = field
	if f.formerToJSONName == nil {
		f.formerToJSONName = make(map[string]string)
	}
	f.formerToJSONName[former] = tag

	return nil
}
