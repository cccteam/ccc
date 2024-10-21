package ccc

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/go-playground/errors/v5"
)

type Duration struct {
	time.Duration
}

func NewDuration(d time.Duration) Duration {
	return Duration{Duration: d}
}

func NewDurationFromString(s string) (Duration, error) {
	duration, err := time.ParseDuration(strings.ReplaceAll(s, " ", ""))
	if err != nil {
		return Duration{}, errors.Newf("time.ParseDuration() error: %s", err)
	}

	return Duration{Duration: duration}, nil
}

func (d Duration) MarshalText() ([]byte, error) {
	return []byte(d.String()), nil
}

func (d *Duration) UnmarshalText(text []byte) error {
	v, err := time.ParseDuration(strings.ReplaceAll(string(text), " ", ""))
	if err != nil {
		return errors.Wrap(err, "time.ParseDuration()")
	}

	d.Duration = v

	return nil
}

// MarshalJSON implements json.Marshaler.MarshalJSON for Duration.
func (d Duration) MarshalJSON() ([]byte, error) {
	b, err := json.Marshal(d.Duration.String())
	if err != nil {
		return nil, errors.Wrap(err, "json.Marshal()")
	}

	return b, nil
}

// UnmarshalJSON implements json.Unmarshaler.UnmarshalJSON for Duration.
func (d *Duration) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return errors.Newf("json.Unmarshal() error: %s", err)
	}

	duration, err := time.ParseDuration(strings.ReplaceAll(s, " ", ""))
	if err != nil {
		return errors.Newf("time.ParseDuration() error: %s", err)
	}

	d.Duration = duration

	return nil
}

func (d *Duration) DecodeSpanner(val any) error {
	var strVal string
	switch t := val.(type) {
	case string:
		strVal = t
	case []byte:
		strVal = string(t)
	default:
		return errors.Newf("failed to parse %+v (type %T) as Duration", val, val)
	}

	pd, err := time.ParseDuration(strVal)
	if err != nil {
		return errors.Wrap(err, "time.ParseDuration()")
	}

	d.Duration = pd

	return nil
}

func (d Duration) EncodeSpanner() (any, error) {
	return d.String(), nil
}

type NullDuration struct {
	Duration
	Valid bool
}

func NewNullDuration(d time.Duration) NullDuration {
	return NullDuration{Duration: Duration{Duration: d}, Valid: true}
}

func NewNullDurationFromString(s string) (NullDuration, error) {
	duration, err := time.ParseDuration(strings.ReplaceAll(s, " ", ""))
	if err != nil {
		return NullDuration{}, errors.Newf("time.ParseDuration() error: %s", err)
	}

	return NullDuration{Duration: Duration{Duration: duration}, Valid: true}, nil
}

func (d NullDuration) MarshalText() ([]byte, error) {
	if !d.Valid {
		return nil, nil
	}

	return []byte(d.String()), nil
}

func (d *NullDuration) UnmarshalText(text []byte) error {
	duration, err := time.ParseDuration(strings.ReplaceAll(string(text), " ", ""))
	if err != nil {
		return errors.Wrap(err, "time.ParseDuration()")
	}

	d.Duration = Duration{Duration: duration}
	d.Valid = true

	return nil
}

// MarshalJSON implements json.Marshaler.MarshalJSON for Duration.
func (d NullDuration) MarshalJSON() ([]byte, error) {
	if !d.Valid {
		return []byte("null"), nil
	}

	b, err := json.Marshal(d.Duration.String())
	if err != nil {
		return nil, errors.Wrap(err, "json.Marshal()")
	}

	return b, nil
}

// UnmarshalJSON implements json.Unmarshaler.UnmarshalJSON for Duration.
func (d *NullDuration) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return errors.Newf("json.Unmarshal() error: %s", err)
	}

	if s == "null" {
		d.Valid = false

		return nil
	}

	duration, err := time.ParseDuration(strings.ReplaceAll(s, " ", ""))
	if err != nil {
		return errors.Newf("time.ParseDuration() error: %s", err)
	}

	d.Duration = Duration{Duration: duration}
	d.Valid = true

	return nil
}

func (d *NullDuration) DecodeSpanner(val any) error {
	var strVal string
	switch t := val.(type) {
	case string:
		strVal = t
	case *string:
		if t == nil {
			return nil
		}
		strVal = *t
	case []byte:
		strVal = string(t)
	default:
		return errors.Newf("failed to parse %+v (type %T) as Duration", val, val)
	}

	pd, err := time.ParseDuration(strVal)
	if err != nil {
		return errors.Wrap(err, "time.ParseDuration()")
	}

	d.Duration = Duration{Duration: pd}
	d.Valid = true

	return nil
}

func (d NullDuration) EncodeSpanner() (any, error) {
	if !d.Valid {
		return nil, nil
	}

	return d.String(), nil
}
