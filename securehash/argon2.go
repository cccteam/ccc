package securehash

import (
	"bytes"
	"encoding"
	"fmt"
	"math"
	"strconv"

	"github.com/go-playground/errors/v5"
	"golang.org/x/crypto/argon2"
)

const argon2Kdf = "argon2ID"

type argon2Key struct {
	key  []byte
	salt []byte

	argon2Options
}

var (
	_ encoding.TextMarshaler   = &argon2Key{}
	_ encoding.TextUnmarshaler = &argon2Key{}
)

func (a2k *argon2Key) compare(plaintext []byte) bool {
	return bytes.Equal(a2k.key, a2k.keyWithSalt(plaintext, a2k.salt))
}

func (a2k *argon2Key) MarshalText() ([]byte, error) {
	b := fmt.Appendf(nil, "$%d", a2k.Memory)
	b = fmt.Appendf(b, "$%d", a2k.Times)
	b = fmt.Appendf(b, "$%d", a2k.Parallelism)
	b = fmt.Appendf(b, "$%s", encodeBase64(a2k.salt))
	b = fmt.Appendf(b, ".%s", encodeBase64(a2k.key))

	return b, nil
}

func (a2k *argon2Key) UnmarshalText(b []byte) error {
	parts, err := parse("$memory$times$parallelism$salt.hash", b)
	if err != nil {
		return err
	}

	key, err := decodeBase64(parts["hash"])
	if err != nil {
		return err
	}
	a2k.key = key
	if l := len(a2k.key); l <= math.MaxUint32 {
		a2k.KeyLen = uint32(l)
	} else {
		return errors.New("key is too long")
	}

	memory, err := strconv.ParseUint(string(parts["memory"]), 10, 32)
	if err != nil {
		return errors.Wrap(err, "strconv.Atoi()")
	}
	a2k.Memory = uint32(memory)

	times, err := strconv.ParseUint(string(parts["times"]), 10, 32)
	if err != nil {
		return errors.Wrap(err, "strconv.Atoi()")
	}
	a2k.Times = uint32(times)

	parallelism, err := strconv.ParseUint(string(parts["parallelism"]), 10, 8)
	if err != nil {
		return errors.Wrap(err, "strconv.Atoi()")
	}
	a2k.Parallelism = uint8(parallelism)

	salt, err := decodeBase64(parts["salt"])
	if err != nil {
		return err
	}
	a2k.salt = salt
	if l := len(a2k.salt); l <= math.MaxUint32 {
		a2k.SaltLen = uint32(l)
	} else {
		return errors.New("salt is too long")
	}

	return nil
}

type argon2Options struct {
	Memory      uint32
	Times       uint32
	Parallelism uint8
	KeyLen      uint32
	SaltLen     uint32
}

// Argon2 initializes argon2 with Owasp recommended settings.
func Argon2() HashAlgorithm {
	return func(kh *SecureHasher) error {
		kh.argon2 = &argon2Options{
			Memory:      7 * 1024,
			Times:       5,
			Parallelism: 1,
			KeyLen:      16,
			SaltLen:     8,
		}

		return nil
	}
}

// argon2WithOptions initializes argon2 with user defined settings.
// This is for specialized use and most users should use the DefaultArgon2 initializer instead.
// The memory parameter specifies the size of the memory in KiB
func argon2WithOptions(memory, times uint32, parallelism uint8, keyLen, saltLen uint32) HashAlgorithm {
	return func(kh *SecureHasher) error {
		kh.argon2 = &argon2Options{
			Memory:      memory,
			Times:       times,
			Parallelism: parallelism,
			KeyLen:      keyLen,
			SaltLen:     saltLen,
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
