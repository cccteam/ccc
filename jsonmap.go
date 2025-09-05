package ccc

import (
	"bytes"
	"encoding/json"

	"github.com/go-playground/errors/v5"
)

// JSONMap is a map that can be unmarshalled from JSON,
// converting all json.Number values to their appropriate
// int or float64 types.
type JSONMap map[string]any

// UnmarshalJSON implements the json.Unmarshaler interface
func (jM *JSONMap) UnmarshalJSON(b []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.UseNumber()

	var tempMap map[string]any
	if err := decoder.Decode(&tempMap); err != nil {
		return errors.Wrapf(err, "json.Decoder.Decode()")
	}

	resolveJSONNumbers(tempMap)

	*jM = tempMap

	return nil
}

func resolveJSONNumbers(v any) {
	switch v := v.(type) {
	case map[string]any:
		for key, elem := range v {
			switch elem := elem.(type) {
			case map[string]any, []any:
				resolveJSONNumbers(elem)
			case json.Number:
				v[key] = resolveToPrimitive(elem)
			}
		}
	case []any:
		for idx, elem := range v {
			switch elem := elem.(type) {
			case map[string]any, []any:
				resolveJSONNumbers(elem)
			case json.Number:
				v[idx] = resolveToPrimitive(elem)
			}
		}
	}
}

func resolveToPrimitive(num json.Number) any {
	if intValue, err := num.Int64(); err == nil {
		return int(intValue)
	}

	if floatValue, err := num.Float64(); err == nil {
		return floatValue
	}

	// The code should never reach this point, but in case it does, return the original json.Number
	return num
}
