package ccc

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/go-playground/errors/v5"
)

type NullEnum[T ~string | ~int | ~int64 | ~float64] struct {
	Value T
	Valid bool
}

func (n *NullEnum[T]) DecodeSpanner(val any) error {
	if val == nil {
		return nil
	}

	if v, ok := val.(T); ok {
		n.Valid = true
		n.Value = v

		return nil
	}

	return errors.Newf("failed to parse %+v (type %T) as NullStringEnum[%T]", val, val, n.Value)
}

func (n NullEnum[T]) EncodeSpanner() (any, error) {
	if !n.Valid {
		return nil, nil
	}

	return n.Value, nil
}

func (n NullEnum[T]) MarshalText() ([]byte, error) {
	if !n.Valid {
		return nil, nil
	}

	switch t := any(n.Value).(type) {
	case string:
		return []byte(t), nil
	case int:
		return []byte(strconv.Itoa(t)), nil
	case int64:
		return []byte(strconv.FormatInt(t, 10)), nil
	case float64:
		return []byte(strconv.FormatFloat(t, 'f', -1, 64)), nil
	default:
		return nil, fmt.Errorf("unsupported type %T", t)
	}
}

func (n *NullEnum[T]) UnmarshalText(text []byte) error {
	if text == nil {
		return nil
	}

	switch t := any(n.Value).(type) {
	case string:
		t = string(text)
		n.Valid = true
	case int:
		v, err := strconv.Atoi(string(text))
		if err != nil {
			return errors.Wrap(err, "strconv.Atoi()")
		}
		t = v
		n.Valid = true
	case int64:
		v, err := strconv.ParseInt(string(text), 10, 64)
		if err != nil {
			return errors.Wrap(err, "strconv.ParseInt()")
		}
		t = v
		n.Valid = true
	case float64:
		v, err := strconv.ParseFloat(string(text), 64)
		if err != nil {
			return errors.Wrap(err, "strconv.ParseFloat()")
		}
		t = v
		n.Valid = true
	default:
		return fmt.Errorf("unsupported type %T", t)
	}

	return nil
}

func (n NullEnum[T]) MarshalJSON() ([]byte, error) {
	if !n.Valid {
		return []byte(jsonNull), nil
	}

	b, err := json.Marshal(n.Value)
	if err != nil {
		return nil, errors.Wrap(err, "json.Marshal()")
	}

	return b, nil
}

func (n *NullEnum[T]) UnmarshalJSON(b []byte) error {
	var s T
	if err := json.Unmarshal(b, &s); err != nil {
		return errors.Newf("json.Unmarshal() error: %s", err)
	}

	n.Valid = true
	n.Value = s

	return nil
}
