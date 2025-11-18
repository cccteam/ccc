package keyhash

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

const (
	argon2Memory      = "memory"
	argon2Times       = "times"
	argon2Parallelism = "parallelism"
	argon2Salt        = "salt"
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

func (a2k *argon2Key) validate() error {
	switch {
	case a2k.key == nil:
		return errors.New("found a zero length key")
	case a2k.salt == nil:
		return errors.New("found a zero length salt")
	case a2k.Memory == 0:
		return errors.New("found a zero value for Argon2 memory parameter")
	case a2k.Times == 0:
		return errors.New("found a zero value for Argon2 times parameter")
	case a2k.Parallelism == 0:
		return errors.New("found a zero value for Argon2 parallelism parameter")
	case a2k.KeyLen == 0:
		return errors.New("found a zero value for Argon2 key length parameter")
	case a2k.SaltLen == 0:
		return errors.New("found a zero value for Argon2 salt length parameter")
	}

	return nil
}

func (a2k *argon2Key) compare(plaintext []byte) bool {
	return bytes.Equal(a2k.key, a2k.keyWithSalt(plaintext, a2k.salt))
}

func (a2k *argon2Key) MarshalText() ([]byte, error) {
	if err := a2k.validate(); err != nil {
		return nil, errors.Wrap(err, "Argon2Key failed validation when attempting to marshall")
	}

	b := fmt.Appendf(nil, "$%s=%d", argon2Memory, a2k.Memory)
	b = fmt.Appendf(b, "$%s=%d", argon2Times, a2k.Times)
	b = fmt.Appendf(b, "$%s=%d", argon2Parallelism, a2k.Parallelism)
	b = fmt.Appendf(b, "$%s=%s", argon2Salt, encodeBase64(a2k.salt))
	b = fmt.Appendf(b, ".%s", encodeBase64(a2k.key))

	return b, nil
}

func (a2k *argon2Key) UnmarshalText(b []byte) error {
	parts := bytes.Split(b, []byte{dot})
	if len(parts) != 2 {
		return errors.Newf("expected to find a single %q sep in input, found %d", dot, len(parts)-1)
	}

	paramsPart := parts[0]
	keyPart := parts[1]

	if len(paramsPart) == 0 {
		return errors.New("found empty params in input")
	}

	if len(keyPart) == 0 {
		return errors.New("found empty key in input")
	}

	key, err := decodeBase64(keyPart)
	if err != nil {
		return err
	}
	a2k.key = key
	if l := len(a2k.key); l <= math.MaxUint32 {
		a2k.KeyLen = uint32(l)
	} else {
		return errors.New("key is too long")
	}

	paramsParts := bytes.Split(paramsPart[1:], []byte{paramSep})
	if len(paramsParts) != 4 {
		return errors.Newf("expected to find a four params in input, found %d", len(paramsParts))
	}

	for _, param := range paramsParts {
		parts := bytes.SplitN(param, []byte{assignment}, 2)
		if len(parts) != 2 {
			return errors.Newf("did not find params in \"$<param name>%s<param value>\" format", string(assignment))
		}

		k, v := parts[0], parts[1]
		if len(v) == 0 {
			return errors.Newf("found empty value for key %s", k)
		}

		switch strK := string(k); strK {
		case argon2Memory:
			memory, err := strconv.ParseUint(string(v), 10, 32)
			if err != nil {
				return errors.Wrap(err, "strconv.Atoi()")
			}
			a2k.Memory = uint32(memory)

		case argon2Times:
			times, err := strconv.ParseUint(string(v), 10, 32)
			if err != nil {
				return errors.Wrap(err, "strconv.Atoi()")
			}
			a2k.Times = uint32(times)

		case argon2Parallelism:
			parallelism, err := strconv.ParseUint(string(v), 10, 8)
			if err != nil {
				return errors.Wrap(err, "strconv.Atoi()")
			}
			a2k.Parallelism = uint8(parallelism)

		case argon2Salt:
			salt, err := decodeBase64(v)
			if err != nil {
				return err
			}
			a2k.salt = salt
			if l := len(a2k.salt); l <= math.MaxUint32 {
				a2k.SaltLen = uint32(l)
			} else {
				return errors.New("salt is too long")
			}

		default:
			return errors.Newf("did not recognize param key %s", k)
		}
	}

	return a2k.validate()
}

type argon2Options struct {
	Memory      uint32
	Times       uint32
	Parallelism uint8
	KeyLen      uint32
	SaltLen     uint32
}

// DefaultArgon2 initializes argon2 with Owasp recommended settings.
func DefaultArgon2() Initialization {
	return func(kh *KeyHasher) error {
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

// CustomArgon2 initializes argon2 with user defined settings.
// This is for specialized use and most users should use the DefaultArgon2 initializer instead.
// The memory parameter specifies the size of the memory in KiB
func CustomArgon2(memory, times uint32, parallelism uint8, keyLen, saltLen uint32) Initialization {
	return func(kh *KeyHasher) error {
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
	return a2.Memory == target.Memory &&
		a2.Times == target.Times &&
		a2.Parallelism == target.Parallelism &&
		a2.KeyLen == target.KeyLen &&
		a2.SaltLen == target.SaltLen
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
