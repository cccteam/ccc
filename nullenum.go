package ccc

import (
	"encoding/json"
	"fmt"
	"reflect"
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

	var ok bool
	var v any
	nType := reflect.TypeOf(n.Value)
	switch nType.Kind() {
	case reflect.String:
		v, ok = val.(string)
	case reflect.Int:
		if valInt64, isInt64 := val.(int64); isInt64 {
			v = valInt64
			ok = true
		} else {
			v, ok = val.(int)
		}
	case reflect.Int64:
		v, ok = val.(int64)
	case reflect.Float64:
		v, ok = val.(float64)
	default:
		panic("implementation logic error: missing type in switch")
	}
	if !ok {
		return errors.Newf("failed to parse %+v (type %T) as NullEnum[%T]", val, val, n.Value)
	}

	n.Value, ok = reflect.ValueOf(v).Convert(nType).Interface().(T)
	if !ok {
		return errors.Newf("failed to convert %v to type %T", v, new(T))
	}
	n.Valid = true

	return nil
}

func (n NullEnum[T]) EncodeSpanner() (any, error) {
	if !n.Valid {
		return nil, nil
	}

	val := reflect.ValueOf(n.Value)
	switch val.Kind() {
	case reflect.String:
		return val.String(), nil
	case reflect.Int:
		return int(val.Int()), nil
	case reflect.Int64:
		return val.Int(), nil
	case reflect.Float64:
		return val.Float(), nil
	default:
		panic("implementation logic error: missing type in switch")
	}
}

func (n NullEnum[T]) MarshalText() ([]byte, error) {
	if !n.Valid {
		return nil, nil
	}

	val := reflect.ValueOf(n.Value)
	switch val.Kind() {
	case reflect.String:
		return []byte(val.String()), nil
	case reflect.Int, reflect.Int64:
		return []byte(strconv.FormatInt(val.Int(), 10)), nil
	case reflect.Float64:
		return []byte(strconv.FormatFloat(val.Float(), 'f', -1, 64)), nil
	default:
		return nil, fmt.Errorf("unsupported type %T", n.Value)
	}
}

func (n *NullEnum[T]) UnmarshalText(text []byte) error {
	if len(text) == 0 {
		return nil
	}

	var val any
	var err error
	nType := reflect.TypeOf(n.Value)
	switch nType.Kind() {
	case reflect.String:
		val = string(text)
	case reflect.Int:
		val, err = strconv.Atoi(string(text))
		if err != nil {
			return errors.Wrap(err, "strconv.Atoi()")
		}
	case reflect.Int64:
		val, err = strconv.ParseInt(string(text), 10, 64)
		if err != nil {
			return errors.Wrap(err, "strconv.ParseInt()")
		}
	case reflect.Float64:
		val, err = strconv.ParseFloat(string(text), 64)
		if err != nil {
			return errors.Wrap(err, "strconv.ParseFloat()")
		}
	default:
		return errors.Newf("unsupported type %T", n.Value)
	}

	var ok bool
	n.Value, ok = reflect.ValueOf(val).Convert(nType).Interface().(T)
	if !ok {
		return errors.Newf("failed to convert %v to type %T", val, new(T))
	}
	n.Valid = true

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
	var s *T
	if err := json.Unmarshal(b, &s); err != nil {
		return errors.Newf("json.Unmarshal() error: %s", err)
	}

	if s == nil {
		return nil
	}

	n.Valid = true
	n.Value = *s

	return nil
}
