package resource

import (
	"encoding/json"

	"github.com/cccteam/httpio"
	"github.com/go-playground/errors/v5"
)

// A field's former wire name (@formerly in the source struct, the formerly tag on the
// generated request struct) is answered beside its current one while older browser
// applications still send it: a body naming the former name is rewritten to the
// current one before the decoders read it, a columns, sort or filter parameter naming
// it resolves to the same field (RequestFieldMapper, newFilterParserFields), and the
// generated list and read handlers write every row's cell under both names. The column
// never changes; only the wire name moves, so the permission checks run against the
// field's current name.

// rewriteFormerKeys renames every key of a JSON object body that is a field's former
// wire name to the field's current one, so both decodes see the current names. A body
// naming a field under both names is refused: it would say two things about one
// column. A body that is not a JSON object is returned as it was, for the decoders to
// refuse as they do today.
func rewriteFormerKeys(data []byte, former map[string]string) ([]byte, error) {
	object, ok := jsonObject(data)
	if !ok {
		return data, nil
	}

	rewritten := false
	for old, current := range former {
		value, ok := object[old]
		if !ok {
			continue
		}
		if _, both := object[current]; both {
			return nil, httpio.NewBadRequestMessagef("json field %s is the former name of %s: the body names both, send one", old, current)
		}
		object[current] = value
		delete(object, old)
		rewritten = true
	}
	if !rewritten {
		return data, nil
	}

	data, err := json.Marshal(object)
	if err != nil {
		return nil, errors.Wrap(err, "json.Marshal()")
	}

	return data, nil
}

// jsonObject reads a body as a JSON object; ok is false for anything else (an array, a
// scalar, null or malformed JSON), which the decoders refuse as they do today.
func jsonObject(data []byte) (map[string]json.RawMessage, bool) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return nil, false
	}

	return object, object != nil
}
