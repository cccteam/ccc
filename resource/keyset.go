package resource

import (
	"fmt"
	"math"
	"math/big"
	"reflect"
	"strings"
	"time"

	gcivil "cloud.google.com/go/civil"
	"cloud.google.com/go/spanner"
	"github.com/cccteam/ccc/accesstypes"
	guid "github.com/google/uuid"
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
		switch v.Value.(type) {
		// Taken from cloud.google.com/go/spanner@v1.83.0/value.go
		// these types are handled by the driver, the rest must be conveted
		case bool, int, int8, int16, int32, uint8, uint16, uint32, int64, float64, float32, []byte,
			spanner.NullInt64, spanner.NullFloat64, spanner.NullFloat32, spanner.NullBool,
			string, spanner.NullString, time.Time, gcivil.Date, spanner.NullTime,
			spanner.NullDate, big.Rat, spanner.NullNumeric, spanner.NullProtoEnum, guid.UUID,
			guid.NullUUID, spanner.NullUUID, spanner.Encoder:
			keys = append(keys, v.Value)
		default:
			// Handle named types by inspecting their underlying kind using reflection.
			refVal := reflect.ValueOf(v.Value)
			switch refVal.Kind() {
			case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
				keys = append(keys, refVal.Int())
			case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
				if u := refVal.Uint(); u <= uint64(math.MaxInt64) {
					keys = append(keys, int64(u))
				} else {
					keys = append(keys, v.Value)
				}
			case reflect.String:
				keys = append(keys, refVal.String())
			case reflect.Float32, reflect.Float64:
				keys = append(keys, refVal.Float())
			case reflect.Bool:
				keys = append(keys, refVal.Bool())
			default:
				keys = append(keys, v.Value)
			}
		}
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
	pKeys := make([]accesstypes.Field, 0, len(p.keyParts))
	for _, keypart := range p.keyParts {
		pKeys = append(pKeys, keypart.Key)
	}

	return pKeys
}
