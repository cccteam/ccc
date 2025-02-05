package resource

import (
	"encoding/json"

	"github.com/cccteam/ccc"
	"github.com/go-playground/errors/v5"
)

type Link struct {
	ID       ccc.UUID `json:"id"`
	Resource string   `json:"resource"`
	Text     string   `json:"text"`
}

func (l Link) EncodeSpanner() (any, error) {
	return l.MarshalJSON()
}

func (l *Link) DecodeSpanner(val any) error {
	var jsonVal string
	switch t := val.(type) {
	case string:
		jsonVal = t
	default:
		return errors.Newf("failed to parse %+v (type %T) as Link", val, val)
	}

	if err := l.UnmarshalJSON([]byte(jsonVal)); err != nil {
		return errors.Wrap(err, "l.MarshalJSON()")
	}

	return nil
}

func (l Link) MarshalJSON() ([]byte, error) {
	type linkAlias Link

	link := linkAlias(l)
	b, err := json.Marshal(link)
	if err != nil {
		return nil, errors.Wrap(err, "json.Marshal()")
	}

	return b, nil
}

func (l *Link) UnmarshalJSON(data []byte) error {
	if data == nil {
		return nil
	}
	if string(data) == "null" {
		return nil
	}

	type linkAlias Link
	link := linkAlias{}

	if err := json.Unmarshal(data, &link); err != nil {
		return errors.Wrap(err, "json.Unmarshal()")
	}

	*l = Link(link)

	return nil
}

func (l Link) IsNull() bool {
	return l.ID.IsNil()
}

type NullLink struct {
	Link  Link
	Valid bool
}

func (nl NullLink) EncodeSpanner() (any, error) {
	if !nl.Valid {
		return nil, nil
	}

	return nl.Link.MarshalJSON()
}

func (nl *NullLink) DecodeSpanner(val any) error {
	var jsonVal string
	switch t := val.(type) {
	case string:
		jsonVal = t
	case *string:
		if t == nil {
			nl.Valid = false

			return nil
		}
		jsonVal = *t
	case nil:
		nl.Valid = false

		return nil
	default:
		return errors.Newf("failed to parse %+v (type %T) as NullLink", val, val)
	}

	if err := nl.UnmarshalJSON([]byte(jsonVal)); err != nil {
		return errors.Wrap(err, "nl.UnmarshalJSON()")
	}

	return nil
}

func (nl NullLink) MarshalJSON() ([]byte, error) {
	if !nl.Valid {
		return []byte("null"), nil
	}

	b, err := nl.Link.MarshalJSON()
	if err != nil {
		return nil, errors.Wrap(err, "json.Marshal()")
	}

	return b, nil
}

func (nl *NullLink) UnmarshalJSON(data []byte) error {
	if data == nil {
		nl.Valid = false

		return nil
	}
	if string(data) == "null" {
		nl.Valid = false

		return nil
	}

	link := Link{}
	if err := link.UnmarshalJSON(data); err != nil {
		return errors.Wrap(err, "link.UnmarshalJSON()")
	}

	nl.Link = link
	nl.Valid = true

	return nil
}
