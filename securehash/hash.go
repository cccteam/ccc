package securehash

import (
	"bytes"
	"encoding"
	"fmt"

	"github.com/go-playground/errors/v5"
)

var (
	_ encoding.TextMarshaler   = &Hash{}
	_ encoding.TextUnmarshaler = &Hash{}
)

// Hash represents a hashed secret.
type Hash struct {
	kdf        string
	underlying comparer
}

// MarshalText implements encoding.TextMarshaler for storing a hashed secret.
func (h *Hash) MarshalText() ([]byte, error) {
	var k comparer

	switch t := h.underlying.(type) {
	case *bcryptKey:
		k = t
	case *argon2Key:
		k = t
	default:
		panic("mismatched kdf and underlying type")
	}

	key, err := k.MarshalText()
	if err != nil {
		return nil, errors.Wrap(err, "encoding.MarshalText()")
	}

	return fmt.Append([]byte(h.kdf), string(key)), nil
}

// UnmarshalText implements encoding.TextUnmarshaler for loading a secret from storage
func (h *Hash) UnmarshalText(hash []byte) error {
	var k comparer

	firstSep := bytes.Index(hash, []byte{sep})
	if firstSep == -1 {
		return errors.Newf("invalid hash format: does not contain params")
	}

	kdfName := string(hash[:firstSep])
	switch kdfName {
	case bcryptKdf:
		k = &bcryptKey{}

	case argon2Kdf:
		k = &argon2Key{}

	default:
		return errors.Newf("did not recognize kdf function name prefix %q", kdfName)
	}

	if err := k.UnmarshalText(hash[firstSep:]); err != nil {
		return errors.Wrap(err, "encoding.UnmarshalText()")
	}

	h.kdf = kdfName
	h.underlying = k

	return nil
}

// DecodeSpanner implements the spanner.Decoder interface
func (h *Hash) DecodeSpanner(val any) error {
	var b []byte
	switch t := val.(type) {
	case string:
		b = []byte(t)
	case []byte:
		b = t
	default:
		return errors.Newf("failed to parse %+v (type %T) as Hash", val, val)
	}

	if err := h.UnmarshalText(b); err != nil {
		return errors.Wrap(err, "u.UnmarshalText()")
	}

	return nil
}

// EncodeSpanner implements the spanner.Encoder interface
func (h Hash) EncodeSpanner() (any, error) {
	b, err := h.MarshalText()
	if err != nil {
		return nil, errors.Wrap(err, "u.MarshalText()")
	}

	return b, nil
}
