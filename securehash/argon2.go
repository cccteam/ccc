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

func (a2 *Argon2Options) apply(kh *SecureHasher) {
	kh.kdf = argon2Kdf
	kh.argon2 = a2
}

func (a2 *Argon2Options) cmpOptions(target *Argon2Options) bool {
	return *a2 == *target
}

func (a2 *Argon2Options) key(plaintext []byte) (*argon2Key, error) {
	salt, err := newSalt(a2.saltLen)
	if err != nil {
		return nil, err
	}

	return &argon2Key{
		key:           a2.keyWithSalt(plaintext, salt),
		salt:          salt,
		Argon2Options: *a2,
	}, nil
}

func (a2 *Argon2Options) keyWithSalt(plaintext, salt []byte) []byte {
	return argon2.IDKey(plaintext, salt, a2.times, a2.memory, a2.parallelism, a2.keyLen)
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

func (a2k *argon2Key) compare(plaintext []byte) error {
	key := a2k.keyWithSalt(plaintext, a2k.salt)
	if len(key) != len(a2k.key) || subtle.ConstantTimeCompare(a2k.key, key) != 1 {
		return errors.New("plaintext does not match key")
	}

	return nil
}

func (a2k *argon2Key) MarshalText() ([]byte, error) {
	b := make([]byte, 0, 28+base64.StdEncoding.EncodedLen((len(a2k.salt))+base64.StdEncoding.EncodedLen(len(a2k.key))))
	b = fmt.Appendf(b, "$%d", a2k.memory)
	b = fmt.Appendf(b, "$%d", a2k.times)
	b = fmt.Appendf(b, "$%d", a2k.parallelism)
	b = fmt.Appendf(b, "$%s", encodeBase64(a2k.salt))
	b = fmt.Appendf(b, ".%s", encodeBase64(a2k.key))

	return b, nil
}

func (a2k *argon2Key) UnmarshalText(b []byte) error {
	var err error
	b, err = removeLeadingSeperator(sep, b)
	if err != nil {
		return err
	}

	a2k.memory, b, err = parseUint32(sep, b)
	if err != nil {
		return errors.Wrapf(err, "failed to parse memory")
	}

	a2k.times, b, err = parseUint32(sep, b)
	if err != nil {
		return errors.Wrap(err, "failed to parse times")
	}

	a2k.parallelism, b, err = parseUint8(sep, b)
	if err != nil {
		return errors.Wrap(err, "failed to parse parallelism")
	}
	a2k.salt, b, err = parseBase64(dot, b)
	if err != nil {
		return errors.Wrap(err, "failed to parse salt")
	}
	if l := len(a2k.salt); l <= math.MaxUint32 {
		a2k.saltLen = uint32(l)
	} else {
		return errors.New("salt is too long")
	}

	a2k.key, _, err = parseBase64(eol, b)
	if err != nil {
		return errors.Wrap(err, "failed to parse key")
	}
	if l := len(a2k.key); l <= math.MaxUint32 {
		a2k.keyLen = uint32(l)
	} else {
		return errors.New("key is too long")
	}

	return nil
}
