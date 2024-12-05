package resource

import (
	"reflect"
	"strings"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/go-playground/errors/v5"
)

type FieldMapper struct {
	jsonTagToFields map[string]accesstypes.Field
	fields          []accesstypes.Field
}

func NewFieldMapper(v any) (*FieldMapper, error) {
	jsonTagToFields, fields, err := tagToFieldMap(v)
	if err != nil {
		return nil, err
	}

	return &FieldMapper{
		jsonTagToFields: jsonTagToFields,
		fields:          fields,
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
	return f.fields
}

func tagToFieldMap(v any) (map[string]accesstypes.Field, []accesstypes.Field, error) {
	vType := reflect.TypeOf(v)

	if vType.Kind() == reflect.Ptr {
		vType = vType.Elem()
	}
	if vType.Kind() != reflect.Struct {
		return nil, nil, errors.Newf("argument v must be a struct, received %v", vType.Kind())
	}

	tfMap := make(map[string]accesstypes.Field)
	fields := make([]accesstypes.Field, 0, vType.NumField())
	for _, field := range reflect.VisibleFields(vType) {
		tag := field.Tag.Get("json")
		if tag == "" {
			if _, ok := tfMap[field.Name]; ok {
				return nil, nil, errors.Newf("field name %s collides with another field tag", field.Name)
			}
			tfMap[field.Name] = accesstypes.Field(field.Name)
			fields = append(fields, accesstypes.Field(field.Name))
			if lowerFieldName := strings.ToLower(field.Name); lowerFieldName != field.Name {
				if _, ok := tfMap[lowerFieldName]; ok {
					return nil, nil, errors.Newf("field name %s has multiple matches", field.Name)
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
			return nil, nil, errors.Newf("tag %s has multiple matches", tag)
		}
		tfMap[tag] = accesstypes.Field(field.Name)
		fields = append(fields, accesstypes.Field(field.Name))
	}

	return tfMap, fields, nil
}
