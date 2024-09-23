package ccc

import (
	"encoding/json"

	"github.com/go-playground/errors/v5"
	"github.com/gofrs/uuid"
)

var NilUUID = UUID{}

type UUID struct {
	uuid.UUID
}

func NewUUID() (UUID, error) {
	uid, err := uuid.NewV4()
	if err != nil {
		return UUID{}, errors.Wrap(err, "uuid.NewV4()")
	}

	return UUID{UUID: uid}, nil
}

func UUIDFromString(s string) (UUID, error) {
	uid, err := uuid.FromString(s)
	if err != nil {
		return UUID{}, errors.Wrap(err, "uuid.FromString()")
	}

	return UUID{UUID: uid}, nil
}

func (u *UUID) DecodeSpanner(val any) error {
	var strVal string
	switch t := val.(type) {
	case string:
		strVal = t
	default:
		return errors.Newf("failed to parse %+v (type %T) as UUID", val, val)
	}

	uid, err := uuid.FromString(strVal)
	if err != nil {
		return errors.Wrap(err, "uuid.FromString()")
	}

	u.UUID = uid

	return nil
}

func (u UUID) EncodeSpanner() (any, error) {
	return u.String(), nil
}

func (u UUID) MarshalText() ([]byte, error) {
	v, err := u.UUID.MarshalText()
	if err != nil {
		return nil, errors.Wrap(err, "u.UUID.MarshalText()")
	}

	return v, nil
}

func (u *UUID) UnmarshalText(text []byte) error {
	uid := &uuid.UUID{}
	if err := uid.UnmarshalText(text); err != nil {
		return errors.Wrap(err, "uid.UnmarshalText()")
	}

	u.UUID = *uid

	return nil
}

func (u UUID) MarshalJSON() ([]byte, error) {
	v, err := u.MarshalText()
	if err != nil {
		return nil, errors.Wrap(err, "u.MarshalText()")
	}

	j, err := json.Marshal(string(v))
	if err != nil {
		return nil, errors.Wrap(err, "json.Marshal()")
	}

	return j, nil
}

func (u *UUID) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return errors.Wrap(err, "json.Unmarshal()")
	}

	if s == "null" {
		return nil
	}

	uid, err := uuid.FromString(s)
	if err != nil {
		return errors.Wrap(err, "uuid.FromString()")
	}

	u.UUID = uid

	return nil
}

type NullUUID struct {
	UUID
	Valid bool
}

func NewNullUUID() (NullUUID, error) {
	uid, err := uuid.NewV4()
	if err != nil {
		return NullUUID{}, errors.Wrap(err, "NewUUID()")
	}

	return NullUUID{UUID: UUID{UUID: uid}, Valid: true}, nil
}

func NullUUIDFromString(s string) (NullUUID, error) {
	uid, err := uuid.FromString(s)
	if err != nil {
		return NullUUID{}, errors.Wrap(err, "uuid.FromString()")
	}

	return NullUUID{UUID: UUID{UUID: uid}, Valid: true}, nil
}

func NullUUIDFromUUID(u UUID) NullUUID {
	return NullUUID{UUID: u, Valid: true}
}

func (u *NullUUID) DecodeSpanner(val any) error {
	var strVal string
	switch t := val.(type) {
	case string:
		strVal = t
	case *string:
		if t == nil {
			return nil
		}
		strVal = *t
	case nil:
		return nil
	default:
		return errors.Newf("failed to parse %+v (type %T) as UUID", val, val)
	}

	uid, err := uuid.FromString(strVal)
	if err != nil {
		return errors.Wrap(err, "uuid.FromString()")
	}

	u.UUID = UUID{UUID: uid}
	u.Valid = true

	return nil
}

func (u NullUUID) EncodeSpanner() (any, error) {
	if !u.Valid {
		return nil, nil
	}

	return u.UUID.String(), nil
}

func (u NullUUID) MarshalText() ([]byte, error) {
	if !u.Valid {
		return nil, nil
	}

	v, err := u.UUID.MarshalText()
	if err != nil {
		return nil, errors.Wrap(err, "u.UUID.MarshalText()")
	}

	return v, nil
}

func (u *NullUUID) UnmarshalText(text []byte) error {
	uid := &UUID{}
	if err := uid.UnmarshalText(text); err != nil {
		return errors.Wrap(err, "uid.UnmarchalText()")
	}

	u.UUID = *uid
	u.Valid = true

	return nil
}

func (u NullUUID) MarshalJSON() ([]byte, error) {
	if !u.Valid {
		return []byte("null"), nil
	}

	v, err := u.MarshalText()
	if err != nil {
		return nil, errors.Wrap(err, "u.MarshalText()")
	}

	j, err := json.Marshal(string(v))
	if err != nil {
		return nil, errors.Wrap(err, "json.Marshal()")
	}

	return j, nil
}

func (u *NullUUID) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return errors.Wrap(err, "json.Unmarshal()")
	}

	if s == "null" {
		u.Valid = false

		return nil
	}

	uid, err := uuid.FromString(s)
	if err != nil {
		return errors.Wrap(err, "uuid.FromString()")
	}

	u.UUID = UUID{UUID: uid}
	u.Valid = true

	return nil
}

// IsNil implements NullableValue.IsNil for NullUUID.
func (u NullUUID) IsNil() bool {
	return !u.Valid
}
