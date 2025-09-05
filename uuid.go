package ccc

import (
	"encoding/json"

	"github.com/go-playground/errors/v5"
	"github.com/gofrs/uuid"
)

// NilUUID represents the nil value of UUID
var NilUUID = UUID{}

// UUID represents a UUID.
// UUID implements the Spanner (DecodeSpanner, EncodeSpanner),
// JSON (MarshalJSON, UnmarshalJSON), and Text (MarshalText, UnmarshalText)
// interfaces.
type UUID struct {
	uuid.UUID
}

// NewUUID returns a new UUID
func NewUUID() (UUID, error) {
	uid, err := uuid.NewV4()
	if err != nil {
		return UUID{}, errors.Wrap(err, "uuid.NewV4()")
	}

	return UUID{UUID: uid}, nil
}

// UUIDFromString parses a UUID from a string representation
func UUIDFromString(s string) (UUID, error) {
	uid, err := uuid.FromString(s)
	if err != nil {
		return UUID{}, errors.Wrap(err, "uuid.FromString()")
	}

	return UUID{UUID: uid}, nil
}

// DecodeSpanner implements the spanner.Decoder interface
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

// EncodeSpanner implements the spanner.Encoder interface
func (u UUID) EncodeSpanner() (any, error) {
	return u.String(), nil
}

// MarshalText implements the encoding.TextMarshaler interface
func (u UUID) MarshalText() ([]byte, error) {
	v, err := u.UUID.MarshalText()
	if err != nil {
		return nil, errors.Wrap(err, "u.UUID.MarshalText()")
	}

	return v, nil
}

// UnmarshalText implements the encoding.TextUnmarshaler interface
func (u *UUID) UnmarshalText(text []byte) error {
	uid := &uuid.UUID{}
	if err := uid.UnmarshalText(text); err != nil {
		return errors.Wrap(err, "uid.UnmarshalText()")
	}

	u.UUID = *uid

	return nil
}

// MarshalJSON implements json.Marshaler interface for UUID.
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

// UnmarshalJSON implements json.Unmarshaler.UnmarshalJSON for UUID.
func (u *UUID) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return errors.Wrap(err, "json.Unmarshal()")
	}

	uid, err := uuid.FromString(s)
	if err != nil {
		return errors.Wrap(err, "uuid.FromString()")
	}

	u.UUID = uid

	return nil
}

// NullUUID represents a UUID that may be null.
// NullUUID implements the Spanner (DecodeSpanner, EncodeSpanner),
// JSON (MarshalJSON, UnmarshalJSON), and Text (MarshalText, UnmarshalText)
// interfaces.
type NullUUID struct {
	UUID
	Valid bool
}

// NewNullUUID returns a new NullUUID
func NewNullUUID() (NullUUID, error) {
	uid, err := uuid.NewV4()
	if err != nil {
		return NullUUID{}, errors.Wrap(err, "NewUUID()")
	}

	return NullUUID{UUID: UUID{UUID: uid}, Valid: true}, nil
}

// NullUUIDFromString parses a NullUUID from a string representation
func NullUUIDFromString(s string) (NullUUID, error) {
	uid, err := uuid.FromString(s)
	if err != nil {
		return NullUUID{}, errors.Wrap(err, "uuid.FromString()")
	}

	return NullUUID{UUID: UUID{UUID: uid}, Valid: true}, nil
}

// NullUUIDFromUUID returns a NullUUID from a UUID
func NullUUIDFromUUID(u UUID) NullUUID {
	return NullUUID{UUID: u, Valid: true}
}

// DecodeSpanner implements the spanner.Decoder interface
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

// EncodeSpanner implements the spanner.Encoder interface
func (u NullUUID) EncodeSpanner() (any, error) {
	if !u.Valid {
		return nil, nil
	}

	return u.String(), nil
}

// MarshalText implements the encoding.TextMarshaler interface
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

// UnmarshalText implements the encoding.TextUnmarshaler interface
func (u *NullUUID) UnmarshalText(text []byte) error {
	uid := &UUID{}
	if err := uid.UnmarshalText(text); err != nil {
		return errors.Wrap(err, "uid.UnmarchalText()")
	}

	u.UUID = *uid
	u.Valid = true

	return nil
}

// MarshalJSON implements json.Marshaler interface for NullUUID.
func (u NullUUID) MarshalJSON() ([]byte, error) {
	if !u.Valid {
		return []byte(jsonNull), nil
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

// UnmarshalJSON implements json.Unmarshaler.UnmarshalJSON for NullUUID.
func (u *NullUUID) UnmarshalJSON(data []byte) error {
	var s *string
	if err := json.Unmarshal(data, &s); err != nil {
		return errors.Wrap(err, "json.Unmarshal()")
	}

	if s == nil {
		return nil
	}

	uid, err := uuid.FromString(*s)
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
