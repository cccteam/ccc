package resource

import (
	"encoding/json"
	"reflect"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/go-playground/errors/v5"
)

type TypeMapper struct {
	fieldToTypeFuncs map[accesstypes.Field]func(string) (any, error)
}

func NewTypeMapper(_ any) (*TypeMapper, error) {
	return nil, nil
}

func fieldToTypeFuncs(v any) (map[accesstypes.Field]func(string) (any, error), error) {
	vType := reflect.TypeOf(v)

	if vType.Kind() == reflect.Ptr {
		vType = vType.Elem()
	}
	if vType.Kind() != reflect.Struct {
		return nil, errors.Newf("argument v must be a struct, received %v", vType.Kind())
	}

	tfMap := make(map[accesstypes.Field]func(string) (any, error))
	for _, field := range reflect.VisibleFields(vType) {
		_ = field
		tfMap[accesstypes.Field(field.Name)] = func(s string) (any, error) {
			typ := reflect.New(field.Type).Interface()

			if err := json.Unmarshal([]byte(s), typ); err != nil {
				return nil, errors.Wrap(err, "json.Unmarshal")
			}

			return typ, nil
		}
	}

	return tfMap, nil
}
