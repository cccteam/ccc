package ccc

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/go-playground/errors/v5"
)

// Duration represents the elapsed time between two instants
// as an int64 nanosecond count. The representation limits the
// largest representable duration to approximately 290 years.
// This custom type provides support for marshaling to and from
// text, json, and Spanner.
type Duration struct {
	time.Duration
}

// NewDuration returns a Duration from a time.Duration
func NewDuration(d time.Duration) Duration {
	return Duration{Duration: d}
}

// NewDurationFromString parses a Duration from a string representation in
// the same manner as time.ParseDuration(), but with the added step of
// removing any whitespace from the string.
func NewDurationFromString(s string) (Duration, error) {
	duration, err := time.ParseDuration(strings.ReplaceAll(s, " ", ""))
	if err != nil {
		return Duration{}, errors.Newf("time.ParseDuration() error: %s", err)
	}

	return Duration{Duration: duration}, nil
}

// MarshalText implements the encoding.TextMarshaler interface
func (d Duration) MarshalText() ([]byte, error) {
	return []byte(d.String()), nil
}

// UnmarshalText implements the encoding.Unmarshaler interface
func (d *Duration) UnmarshalText(text []byte) error {
	v, err := time.ParseDuration(strings.ReplaceAll(string(text), " ", ""))
	if err != nil {
		return errors.Wrap(err, "time.ParseDuration()")
	}

	d.Duration = v

	return nil
}

// MarshalJSON implements json.Marshaler interface for Duration.
func (d Duration) MarshalJSON() ([]byte, error) {
	b, err := json.Marshal(d.String())
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

// DecodeSpanner implements the spanner.Decoder interface
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

// EncodeSpanner implements the spanner.Encoder interface
func (d Duration) EncodeSpanner() (any, error) {
	return d.String(), nil
}

// NullDuration handles null values of the Duration type
type NullDuration struct {
	Duration
	Valid bool
}

// NewNullDuration returns a NullDuration from a time.Duration
func NewNullDuration(d time.Duration) NullDuration {
	return NullDuration{Duration: Duration{Duration: d}, Valid: true}
}

// NewNullDurationFromString parses a NullDuration from a string representation
func NewNullDurationFromString(s string) (NullDuration, error) {
	duration, err := time.ParseDuration(strings.ReplaceAll(s, " ", ""))
	if err != nil {
		return NullDuration{}, errors.Newf("time.ParseDuration() error: %s", err)
	}

	return NullDuration{Duration: Duration{Duration: duration}, Valid: true}, nil
}

// MarshalText implements the encoder.TextMarshaler interface
func (d NullDuration) MarshalText() ([]byte, error) {
	if !d.Valid {
		return nil, nil
	}

	return []byte(d.String()), nil
}

// UnmarshalText implements the encoder.TextUnmarshaler interface
func (d *NullDuration) UnmarshalText(text []byte) error {
	duration, err := time.ParseDuration(strings.ReplaceAll(string(text), " ", ""))
	if err != nil {
		return errors.Wrap(err, "time.ParseDuration()")
	}

	d.Duration = Duration{Duration: duration}
	d.Valid = true

	return nil
}

// MarshalJSON implements json.Marshaler interface for Duration.
func (d NullDuration) MarshalJSON() ([]byte, error) {
	if !d.Valid {
		return []byte(jsonNull), nil
	}

	b, err := json.Marshal(d.String())
	if err != nil {
		return nil, errors.Wrap(err, "json.Marshal()")
	}

	return b, nil
}

// UnmarshalJSON implements json.Unmarshaler.UnmarshalJSON for Duration.
func (d *NullDuration) UnmarshalJSON(b []byte) error {
	var s *string
	if err := json.Unmarshal(b, &s); err != nil {
		return errors.Newf("json.Unmarshal() error: %s", err)
	}

	if s == nil {
		return nil
	}

	duration, err := time.ParseDuration(strings.ReplaceAll(*s, " ", ""))
	if err != nil {
		return errors.Newf("time.ParseDuration() error: %s", err)
	}

	d.Duration = Duration{Duration: duration}
	d.Valid = true

	return nil
}

// DecodeSpanner implements the spanner.Decode interface
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

// EncodeSpanner implements the spanner.Encode interface
func (d NullDuration) EncodeSpanner() (any, error) {
	if !d.Valid {
		return nil, nil
	}

	return d.String(), nil
}
