package securehash

import (
	"crypto/subtle"
	"encoding"
	"encoding/base64"
	"fmt"
	"math"

	"github.com/go-playground/errors/v5"
	"golang.org/x/crypto/argon2"
)

// Argon2Options hold hashing options for a Argon2ID key
type Argon2Options struct {
	memory      uint32
	times       uint32
	parallelism uint8
	saltLen     uint32
	keyLen      uint32
}

// Argon2 initializes argon2 with Owasp recommended settings.
func Argon2() *Argon2Options {
	return &Argon2Options{
		memory:      12 * 1024,
		times:       3,
		parallelism: 1,
		saltLen:     16,
		keyLen:      32,
	}
}

// argon2WithOptions initializes argon2 with user defined settings.
// This is for specialized use and most users should use the DefaultArgon2 initializer instead.
// The memory parameter specifies the size of the memory in KiB
func argon2WithOptions(memory, times uint32, parallelism uint8, saltLen, keyLen uint32) *Argon2Options {
	return &Argon2Options{
		memory:      memory,
		times:       times,
		parallelism: parallelism,
		saltLen:     saltLen,
		keyLen:      keyLen,
	}
}

func (a *Argon2Options) apply(h *SecureHasher) {
	h.argon2 = a
}

func (a *Argon2Options) key(plaintext []byte) (*argon2Key, error) {
	salt, err := newSalt(a.saltLen)
	if err != nil {
		return nil, err
	}

	return &argon2Key{
		key:           a.keyWithSalt(plaintext, salt),
		salt:          salt,
		Argon2Options: *a,
	}, nil
}

func (a *Argon2Options) keyWithSalt(plaintext, salt []byte) []byte {
	return argon2.IDKey(plaintext, salt, a.times, a.memory, a.parallelism, a.keyLen)
}

var (
	_ encoding.TextMarshaler   = &argon2Key{}
	_ encoding.TextUnmarshaler = &argon2Key{}
)

type argon2Key struct {
	key  []byte
	salt []byte

	Argon2Options
}

func (a *argon2Key) compare(plaintext []byte) error {
	key := a.keyWithSalt(plaintext, a.salt)
	if len(key) != len(a.key) || subtle.ConstantTimeCompare(a.key, key) != 1 {
		return errors.New("plaintext does not match key")
	}

	return nil
}

func (a *argon2Key) cmpOptions(target *SecureHasher) bool {
	if target.argon2 == nil {
		return false
	}

	return a.Argon2Options == *target.argon2
}

func (a *argon2Key) MarshalText() ([]byte, error) {
	b := make([]byte, 0, 28+base64.StdEncoding.EncodedLen((len(a.salt))+base64.StdEncoding.EncodedLen(len(a.key))))
	b = fmt.Appendf(b, "$%d", a.memory)
	b = fmt.Appendf(b, "$%d", a.times)
	b = fmt.Appendf(b, "$%d", a.parallelism)
	b = fmt.Appendf(b, "$%s", encodeBase64(a.salt))
	b = fmt.Appendf(b, ".%s", encodeBase64(a.key))

	return b, nil
}

func (a *argon2Key) UnmarshalText(hash []byte) error {
	var err error
	hash, err = removeLeadingSeperator(sep, hash)
	if err != nil {
		return err
	}

	a.memory, hash, err = parseUint32(sep, hash)
	if err != nil {
		return errors.Wrapf(err, "failed to parse memory")
	}

	a.times, hash, err = parseUint32(sep, hash)
	if err != nil {
		return errors.Wrap(err, "failed to parse times")
	}

	a.parallelism, hash, err = parseUint8(sep, hash)
	if err != nil {
		return errors.Wrap(err, "failed to parse parallelism")
	}
	a.salt, hash, err = parseBase64(dot, hash)
	if err != nil {
		return errors.Wrap(err, "failed to parse salt")
	}
	if l := len(a.salt); l <= math.MaxUint32 {
		a.saltLen = uint32(l)
	} else {
		return errors.New("salt is too long")
	}

	a.key, _, err = parseBase64(eol, hash)
	if err != nil {
		return errors.Wrap(err, "failed to parse key")
	}
	if l := len(a.key); l <= math.MaxUint32 {
		a.keyLen = uint32(l)
	} else {
		return errors.New("key is too long")
	}

	return nil
}
