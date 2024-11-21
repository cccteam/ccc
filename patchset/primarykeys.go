package patchset

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

// PrimaryKey is an object that represents a single or composite primary key and its value.
type PrimaryKey struct {
	keyParts []KeyPart
}

func NewPrimaryKey(key accesstypes.Field, value any) PrimaryKey {
	return PrimaryKey{
		keyParts: []KeyPart{
			{Key: key, Value: value},
		},
	}
}

// Add adds an additional column to the primary key creating a composite primary key
//   - PrimaryKey is immutable.
//   - Add returns a new PrimaryKey that should be used for all subsequent operations.
func (p PrimaryKey) Add(key accesstypes.Field, value any) PrimaryKey {
	p.keyParts = append(p.keyParts, KeyPart{
		Key:   key,
		Value: value,
	})

	return p
}

func (p PrimaryKey) RowID() string {
	var id strings.Builder
	for _, v := range p.keyParts {
		id.WriteString(fmt.Sprintf("|%v", v.Value))
	}

	return id.String()[1:]
}

func (p PrimaryKey) String() string {
	var values strings.Builder
	for _, keyPart := range p.keyParts {
		values.WriteString(fmt.Sprintf(", %s: %v", keyPart.Key, keyPart.Value))
	}

	return values.String()[2:]
}

func (p PrimaryKey) KeySet() spanner.KeySet {
	keys := make(spanner.Key, 0, len(p.keyParts))
	for _, v := range p.keyParts {
		keys = append(keys, v.Value)
	}

	return keys
}

func (p PrimaryKey) Map() map[accesstypes.Field]any {
	pKeyMap := make(map[accesstypes.Field]any)
	for _, keypart := range p.keyParts {
		pKeyMap[keypart.Key] = keypart.Value
	}

	return pKeyMap
}

func (p PrimaryKey) Parts() []KeyPart {
	return p.keyParts
}

func (p PrimaryKey) Len() int {
	return len(p.keyParts)
}
