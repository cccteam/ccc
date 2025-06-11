package resource

import (
	"reflect"
	"strings"

	"github.com/go-playground/errors/v5"
)

type FilterKeys struct {
	keys  map[FilterKey]FilterType
	types map[FilterKey]reflect.Type
}

func NewFilterKeys[Req any](res Resourcer) (*FilterKeys, error) {
	var filterTypes []FilterType

	switch res.DefaultConfig().DBType {
	case SpannerDBType:
		filterTypes = []FilterType{Index, FullText, Ngram, SubString}
	case PostgresDBType:
		filterTypes = []FilterType{Index}
	}

	keys := make(map[FilterKey]FilterType, 0)
	types := make(map[FilterKey]reflect.Type, 0)
	for _, structField := range reflect.VisibleFields(reflect.TypeFor[Req]()) {
		for _, filterType := range filterTypes {
			tag := structField.Tag.Get(string(filterType))
			if tag == "" {
				continue
			}

			switch filterType {
			case Index:
				if tag != "true" {
					continue
				}

				raw := structField.Tag.Get("json")
				jsonTag, _, _ := strings.Cut(raw, ",")
				if jsonTag == "" {
					return nil, errors.Newf("struct field %s, does not have a json tag", structField.Name)
				}

				keys[FilterKey(jsonTag)] = filterType
				types[FilterKey(structField.Name)] = structField.Type
			case Ngram, SubString, FullText:
				for _, key := range splitFilterKeys(tag) {
					keys[key] = filterType
				}
			}
		}
	}

	return &FilterKeys{keys: keys, types: types}, nil
}

func splitFilterKeys(keys string) []FilterKey {
	split := strings.Split(keys, ",")

	filterKeys := make([]FilterKey, 0, len(split))
	for _, str := range split {
		filterKeys = append(filterKeys, FilterKey(str))
	}

	return filterKeys
}
