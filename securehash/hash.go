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

	switch h.kdf {
	case bcryptKdf:
		bk, ok := h.underlying.(*bcryptKey)
		if !ok {
			panic("mismatched kdf and underlying type")
		}

		k = bk

	case argon2Kdf:
		a2k, ok := h.underlying.(*argon2Key)
		if !ok {
			panic("mismatched kdf and underlying type")
		}

		k = a2k
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
