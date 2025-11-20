package securehash

import (
	"bytes"
	"encoding"
	"fmt"

	"github.com/go-playground/errors/v5"
)

type Hash struct {
	kdf        string
	underlying comparer
}

var (
	_ encoding.TextMarshaler   = &Hash{}
	_ encoding.TextUnmarshaler = &Hash{}
)

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
		return nil, err
	}

	return fmt.Append([]byte(h.kdf), string(key)), nil
}

func (h *Hash) UnmarshalText(hash []byte) error {
	var k comparer

	firstSep := bytes.Index(hash, []byte{sep})
	if firstSep == -1 {
		return errors.Newf("invalid hash format: does not contain params")
	}
	if firstSep == 0 { // legacy bcrypt support
		firstSep = len(bcryptKdf)
		hash = append([]byte(bcryptKdf), hash...)
	}

	kdfName := string(hash[:firstSep])
	switch kdfName {
	case bcryptKdf:
		k = &bcryptKey{}

	case argon2Kdf:
		k = &argon2Key{}

	default:
		return errors.Newf("did not recognize kdf function name prefix %s", kdfName)
	}

	if err := k.UnmarshalText(hash[firstSep:]); err != nil {
		return errors.Wrap(err, "encoding.UnmarshalText()")
	}

	h.kdf = kdfName
	h.underlying = k

	return nil
}
