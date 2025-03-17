package resource

import (
	"reflect"
	"strings"

	"github.com/go-playground/errors/v5"
)

type FilterKeys struct {
	keys  map[FilterKey]FilterType
	kinds map[FilterKey]reflect.Kind
}

func NewFilterKeys[Req any](res Resourcer) (*FilterKeys, error) {
	var filterTypes []FilterType

	switch res.DefaultConfig().DBType {
	case SpannerDBType:
		filterTypes = []FilterType{Index, FullText, Ngram, SubString}
	case PostgresDBType:
		filterTypes = []FilterType{}
	}

	keys := make(map[FilterKey]FilterType, 0)
	kinds := make(map[FilterKey]reflect.Kind, 0)
	for _, structField := range reflect.VisibleFields(reflect.TypeFor[Req]()) {
		for _, filterType := range filterTypes {
			keyList := structField.Tag.Get(string(filterType))
			if keyList == "" {
				continue
			}

			switch filterType {
			case Index:
				if keyList != "true" {
					continue
				}

				raw := structField.Tag.Get("json")
				jsonTag, _, _ := strings.Cut(raw, ",")
				if jsonTag == "" {
					return nil, errors.Newf("struct field %s, does not have a json tag", structField.Name)
				}

				keys[FilterKey(jsonTag)] = filterType

				if typ := structField.Type; typ.Kind() == reflect.Pointer {
					kinds[FilterKey(structField.Name)] = typ.Elem().Kind()
				} else {
					kinds[FilterKey(structField.Name)] = structField.Type.Kind()
				}

			case Ngram, SubString, FullText:
				for _, key := range splitFilterKeys(keyList) {
					keys[key] = filterType
				}
			}

		}
	}

	return &FilterKeys{keys: keys, kinds: kinds}, nil
}

func splitFilterKeys(keys string) []FilterKey {
	split := strings.Split(keys, ",")

	filterKeys := make([]FilterKey, 0, len(split))
	for _, str := range split {
		filterKeys = append(filterKeys, FilterKey(str))
	}

	return filterKeys
}
