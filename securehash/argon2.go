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

type argon2Key struct {
	key  []byte
	salt []byte

	argon2Options
}

var (
	_ encoding.TextMarshaler   = &argon2Key{}
	_ encoding.TextUnmarshaler = &argon2Key{}
)

func (a2k *argon2Key) compare(plaintext []byte) error {
	key := a2k.keyWithSalt(plaintext, a2k.salt)
	if len(key) != len(a2k.key) || subtle.ConstantTimeCompare(a2k.key, key) != 1 {
		return errors.New("plaintext does not match key")
	}

	return nil
}

func (a2k *argon2Key) MarshalText() ([]byte, error) {
	b := make([]byte, 0, 28+base64.StdEncoding.EncodedLen((len(a2k.salt))+base64.StdEncoding.EncodedLen(len(a2k.key))))
	b = fmt.Appendf(b, "$%d", a2k.Memory)
	b = fmt.Appendf(b, "$%d", a2k.Times)
	b = fmt.Appendf(b, "$%d", a2k.Parallelism)
	b = fmt.Appendf(b, "$%s", encodeBase64(a2k.salt))
	b = fmt.Appendf(b, ".%s", encodeBase64(a2k.key))

	return b, nil
}

func (a2k *argon2Key) UnmarshalText(b []byte) error {
	var err error
	a2k.Memory, b, err = parseUint32(sep, b)
	if err != nil {
		return errors.Wrapf(err, "failed to parse memory")
	}

	a2k.Times, b, err = parseUint32(sep, b)
	if err != nil {
		return err
	}

	a2k.Parallelism, b, err = parseUint8(sep, b)
	if err != nil {
		return err
	}
	a2k.salt, b, err = parseBase64(dot, b)
	if err != nil {
		return err
	}
	if l := len(a2k.salt); l <= math.MaxUint32 {
		a2k.SaltLen = uint32(l)
	} else {
		return errors.New("salt is too long")
	}

	a2k.key, _, err = parseBase64(eol, b)
	if err != nil {
		return err
	}
	if l := len(a2k.key); l <= math.MaxUint32 {
		a2k.KeyLen = uint32(l)
	} else {
		return errors.New("key is too long")
	}

	return nil
}

type argon2Options struct {
	Memory      uint32
	Times       uint32
	Parallelism uint8
	SaltLen     uint32
	KeyLen      uint32
}

// Argon2 initializes argon2 with Owasp recommended settings.
func Argon2() HashAlgorithm {
	return func(kh *SecureHasher) error {
		kh.kdf = argon2Kdf
		kh.argon2 = &argon2Options{
			Memory:      12 * 1024,
			Times:       3,
			Parallelism: 1,
			SaltLen:     16,
			KeyLen:      32,
		}

		return nil
	}
}

// argon2WithOptions initializes argon2 with user defined settings.
// This is for specialized use and most users should use the DefaultArgon2 initializer instead.
// The memory parameter specifies the size of the memory in KiB
func argon2WithOptions(memory, times uint32, parallelism uint8, keyLen, saltLen uint32) HashAlgorithm {
	return func(kh *SecureHasher) error {
		kh.kdf = argon2Kdf
		kh.argon2 = &argon2Options{
			Memory:      memory,
			Times:       times,
			Parallelism: parallelism,
			SaltLen:     saltLen,
			KeyLen:      keyLen,
		}

		return nil
	}
}

func (a2 *argon2Options) cmpOptions(target *argon2Options) bool {
	return *a2 == *target
}

func (a2 *argon2Options) key(plaintext []byte) (*argon2Key, error) {
	salt, err := newSalt(a2.SaltLen)
	if err != nil {
		return nil, err
	}

	return &argon2Key{
		key:           a2.keyWithSalt(plaintext, salt),
		salt:          salt,
		argon2Options: *a2,
	}, nil
}

func (a2 *argon2Options) keyWithSalt(plaintext, salt []byte) []byte {
	return argon2.IDKey(plaintext, salt, a2.Times, a2.Memory, a2.Parallelism, a2.KeyLen)
}
