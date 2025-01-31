package resource

import (
	"encoding/json"

	"cloud.google.com/go/spanner"
	"github.com/cccteam/ccc"
	"github.com/go-playground/errors/v5"
)

type Link struct {
	ID          ccc.UUID `json:"id"`
	Resource    string   `json:"resource"`
	DisplayName string   `json:"displayName"`
}

func (l Link) EncodeSpanner() (any, error) {
	return spanner.NullJSON{Valid: true, Value: l}, nil
}

func (l *Link) DecodeSpanner(val any) error {
	var jsonVal spanner.NullJSON
	switch t := val.(type) {
	case spanner.NullJSON:
		jsonVal = t
	default:
		return errors.Newf("failed to parse %+v (type %T) as Link", val, val)
	}

	bytes, err := jsonVal.MarshalJSON()
	if err != nil {
		return errors.Wrap(err, "jsonVal.MarshalJSON()")
	}

	if err := l.UnmarshalJSON(bytes); err != nil {
		return errors.Wrap(err, "l.MarshalJSON()")
	}

	return nil
}

func (l Link) MarshalJSON() ([]byte, error) {
	b, err := json.Marshal(l)
	if err != nil {
		return nil, errors.Wrap(err, "json.Marshal()")
	}

	return b, nil
}

func (l *Link) UnmarshalJSON(data []byte) error {
	if data == nil {
		return nil
	}

	var link *Link
	if err := json.Unmarshal(data, &link); err != nil {
		return errors.Wrap(err, "json.Unmarshal()")
	}

	if link == nil {
		return nil
	}

	l.ID = link.ID
	l.Resource = link.Resource
	l.DisplayName = link.DisplayName

	return nil
}

func (l Link) IsNull() bool {
	return l.ID.IsNil()
}
