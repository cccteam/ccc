package resource

import (
	"reflect"
	"strings"
)

type SearchKeys struct {
	keys map[SearchType][]string
}

func NewSearchKeys[Resource Resourcer](res Resource) *SearchKeys {
	searchTypes := []SearchType{FullText, Ngram, SubString}

	keys := make(map[SearchType][]string, 0)
	for _, structField := range reflect.VisibleFields(reflect.TypeOf(res)) {
		for _, searchType := range searchTypes {
			keyList := structField.Tag.Get(string(searchType))
			if keyList == "" {
				continue
			}

			keys[searchType] = append(keys[searchType], splitSearchKeys(keyList)...)
		}
	}

	return &SearchKeys{keys: keys}
}

func splitSearchKeys(keys string) []string {
	return strings.Split(keys, ",")
}
