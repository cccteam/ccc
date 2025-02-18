package resource

import (
	"reflect"
	"strings"
)

type FilterKeys struct {
	keys map[FilterKey]FilterType
}

func NewFilterKeys[Req any](res Resourcer) *FilterKeys {
	var filterTypes []FilterType

	switch res.DefaultConfig().DBType {
	case SpannerDBType:
		filterTypes = []FilterType{Index, FullText, Ngram, SubString}
	case PostgresDBType:
		filterTypes = []FilterType{}
	}

	keys := make(map[FilterKey]FilterType, 0)
	for _, structField := range reflect.VisibleFields(reflect.TypeFor[Req]()) {
		for _, filterType := range filterTypes {
			keyList := structField.Tag.Get(string(filterType))
			if keyList == "" {
				continue
			}

			for _, key := range splitFilterKeys(keyList) {
				keys[key] = filterType
			}
		}
	}

	return &FilterKeys{
		keys: keys,
	}
}

func splitFilterKeys(keys string) []FilterKey {
	split := strings.Split(keys, ",")

	filterKeys := make([]FilterKey, 0, len(split))
	for _, str := range split {
		filterKeys = append(filterKeys, FilterKey(str))
	}

	return filterKeys
}
