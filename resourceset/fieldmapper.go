package resourceset

import (
	"maps"
	"reflect"
	"slices"
	"strings"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/go-playground/errors/v5"
)

type FieldMapper struct {
	jsonTagToFields map[string]accesstypes.Field
}

func NewFieldMapper(v any) (*FieldMapper, error) {
	jsonTagToFields, err := tagToFieldMap(v)
	if err != nil {
		return nil, err
	}

	return &FieldMapper{
		jsonTagToFields: jsonTagToFields,
	}, nil
}

func (f *FieldMapper) StructFieldName(tag string) (accesstypes.Field, bool) {
	fieldName, ok := f.jsonTagToFields[tag]

	return fieldName, ok
}

func (f *FieldMapper) Len() int {
	return len(f.jsonTagToFields)
}

func (f *FieldMapper) Fields() []accesstypes.Field {
	return slices.Collect(maps.Values(f.jsonTagToFields))
}

func tagToFieldMap(v any) (map[string]accesstypes.Field, error) {
	vType := reflect.TypeOf(v)

	if vType.Kind() == reflect.Ptr {
		vType = vType.Elem()
	}
	if vType.Kind() != reflect.Struct {
		return nil, errors.Newf("argument v must be a struct, received %v", vType.Kind())
	}

	tfMap := make(map[string]accesstypes.Field)
	for _, field := range reflect.VisibleFields(vType) {
		tag := field.Tag.Get("json")
		if tag == "" {
			if _, ok := tfMap[field.Name]; ok {
				return nil, errors.Newf("field name %s collides with another field tag", field.Name)
			}
			tfMap[field.Name] = accesstypes.Field(field.Name)
			if lowerFieldName := strings.ToLower(field.Name); lowerFieldName != field.Name {
				if _, ok := tfMap[lowerFieldName]; ok {
					return nil, errors.Newf("field name %s has multiple matches", field.Name)
				}
				tfMap[lowerFieldName] = accesstypes.Field(field.Name)
			}

			continue
		}

		if before, _, found := strings.Cut(tag, ","); found {
			tag = before
		}

		if tag == "-" {
			continue
		}

		if _, ok := tfMap[tag]; ok {
			return nil, errors.Newf("tag %s has multiple matches", tag)
		}
		tfMap[tag] = accesstypes.Field(field.Name)
	}

	return tfMap, nil
}
