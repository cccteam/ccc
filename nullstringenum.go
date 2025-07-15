package ccc

import (
	"encoding/json"

	"github.com/go-playground/errors/v5"
)

type NullStringEnum[T ~string] struct {
	Value T
	Valid bool
}

func (l *NullStringEnum[T]) DecodeSpanner(val any) error {
	if val == nil {
		l.Valid = false
		l.Value = ""

		return nil
	}

	if str, ok := val.(string); ok {
		l.Valid = true
		l.Value = T(str)

		return nil
	}

	return errors.Newf("failed to parse %+v (type %T) as NullStringEnum[%T]", val, val, l.Value)
}

func (l NullStringEnum[T]) EncodeSpanner() (any, error) {
	if !l.Valid {
		return nil, nil
	}

	return string(l.Value), nil
}

func (l NullStringEnum[T]) MarshalText() ([]byte, error) {
	if !l.Valid {
		return nil, nil
	}

	return []byte(l.Value), nil
}

func (l *NullStringEnum[T]) UnmarshalText(text []byte) error {
	l.Value = T(text)
	l.Valid = true

	return nil
}

func (l NullStringEnum[T]) MarshalJSON() ([]byte, error) {
	if !l.Valid {
		return []byte(jsonNull), nil
	}

	b, err := json.Marshal(l.Value)
	if err != nil {
		return nil, errors.Wrap(err, "json.Marshal()")
	}

	return b, nil
}

func (l *NullStringEnum[T]) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return errors.Newf("json.Unmarshal() error: %s", err)
	}

	if s == jsonNull {
		l.Valid = false

		return nil
	}

	l.Valid = true
	l.Value = T(s)

	return nil
}
