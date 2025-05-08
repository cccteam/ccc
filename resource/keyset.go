package resource

import (
	"fmt"
	"strings"

	"cloud.google.com/go/spanner"
	"github.com/cccteam/ccc/accesstypes"
)

type KeyPart struct {
	Key   accesstypes.Field
	Value any
}

// KeySet is an object that represents a single or composite primary key and its value.
type KeySet struct {
	keyParts []KeyPart
}

// Add adds an additional column to the primary key creating a composite primary key
//   - PrimaryKey is immutable.
//   - Add returns a new PrimaryKey that should be used for all subsequent operations.
func (p KeySet) Add(key accesstypes.Field, value any) KeySet {
	p.keyParts = append(p.keyParts, KeyPart{
		Key:   key,
		Value: value,
	})

	return p
}

func (p KeySet) RowID() string {
	if len(p.keyParts) == 0 {
		return ""
	}

	var id strings.Builder
	for _, v := range p.keyParts {
		id.WriteString(fmt.Sprintf("|%v", v.Value))
	}

	return id.String()[1:]
}

func (p KeySet) KeySet() spanner.KeySet {
	keys := make(spanner.Key, 0, len(p.keyParts))
	for _, v := range p.keyParts {
		keys = append(keys, v.Value)
	}

	return keys
}

func (p KeySet) KeyMap() map[accesstypes.Field]any {
	pKeyMap := make(map[accesstypes.Field]any)
	for _, keypart := range p.keyParts {
		pKeyMap[keypart.Key] = keypart.Value
	}

	return pKeyMap
}

func (p KeySet) Parts() []KeyPart {
	return p.keyParts
}

func (p KeySet) Len() int {
	return len(p.keyParts)
}

func (p KeySet) keys() []accesstypes.Field {
	var pKeys []accesstypes.Field
	for _, keypart := range p.keyParts {
		pKeys = append(pKeys, keypart.Key)
	}

	return pKeys
}
