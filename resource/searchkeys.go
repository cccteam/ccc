package resource

import (
	"reflect"
	"strings"
)

type SearchKeys struct {
	keys map[SearchKey]SearchType
}

func NewSearchKeys[Req any](res Resourcer) *SearchKeys {
	var searchTypes []SearchType

	switch res.DefaultConfig().DBType {
	case SpannerDBType:
		searchTypes = []SearchType{FullText, Ngram, SubString}
	case PostgresDBType:
		searchTypes = []SearchType{}
	}

	keys := make(map[SearchKey]SearchType, 0)
	for _, structField := range reflect.VisibleFields(reflect.TypeFor[Req]()) {
		for _, searchType := range searchTypes {
			keyList := structField.Tag.Get(string(searchType))
			if keyList == "" {
				continue
			}

			for _, key := range splitSearchKeys(keyList) {
				keys[key] = searchType
			}
		}
	}

	return &SearchKeys{
		keys: keys,
	}
}

func splitSearchKeys(keys string) []SearchKey {
	split := strings.Split(keys, ",")

	searchKeys := make([]SearchKey, 0, len(split))
	for _, str := range split {
		searchKeys = append(searchKeys, SearchKey(str))
	}

	return searchKeys
}
