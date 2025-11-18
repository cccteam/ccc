package keyhash

import (
	"bytes"
	"encoding"
	"encoding/base64"
	"fmt"
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

	encKey := make([]byte, base64.StdEncoding.EncodedLen(len(a2k.key)))
	base64.StdEncoding.Encode(encKey, a2k.key)

	encSalt := make([]byte, base64.StdEncoding.EncodedLen(len(a2k.salt)))
	base64.StdEncoding.Encode(encSalt, a2k.salt)

	b := fmt.Appendf(nil, "$memory=%s", strconv.Itoa(int(a2k.Memory)))
	b = fmt.Appendf(b, "$times=%s", strconv.Itoa(int(a2k.Times)))
	b = fmt.Appendf(b, "$parallelism=%s", strconv.Itoa(int(a2k.Parallelism)))
	b = fmt.Appendf(b, "$salt=%v", string(encSalt))
	b = fmt.Appendf(b, ".%v", string(encKey))

	return b, nil
}

func (a2k *argon2Key) UnmarshalText(b []byte) error {
	const (
		memoryP      = "memory"
		timesP       = "times"
		parallelismP = "parallelism"
		saltP        = "salt"
	)

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

	decKey := make([]byte, base64.StdEncoding.DecodedLen(len(keyPart)))
	keyN, err := base64.StdEncoding.Decode(decKey, keyPart)
	if err != nil {
		return errors.Wrap(err, "base64.Encoding.Decode()")
	}
	a2k.key = decKey[:keyN]
	a2k.KeyLen = uint32(len(a2k.key))

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
		case memoryP:
			memory, err := strconv.Atoi(string(v))
			if err != nil {
				return errors.Wrap(err, "strconv.Atoi()")
			}
			a2k.Memory = uint32(memory)

		case timesP:
			times, err := strconv.Atoi(string(v))
			if err != nil {
				return errors.Wrap(err, "strconv.Atoi()")
			}
			a2k.Times = uint32(times)

		case parallelismP:
			if len(v) > 1 {
				return errors.Newf("expected 8bit value for key %s, found %d", k, 1<<(len(v)-1)*8)
			}
			parallelism, err := strconv.Atoi(string(v))
			if err != nil {
				return errors.Wrap(err, "strconv.Atoi()")
			}
			a2k.Parallelism = uint8(parallelism)

		case saltP:
			salt := v
			decSalt := make([]byte, base64.StdEncoding.DecodedLen(len(salt)))
			saltN, err := base64.StdEncoding.Decode(decSalt, salt)
			if err != nil {
				return errors.Wrap(err, "base64.Encoding.Decode()")
			}
			a2k.salt = decSalt[:saltN]
			a2k.SaltLen = uint32(len(a2k.salt))

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
			Memory:      7 * MiB,
			Times:       5,
			Parallelism: 1,
			KeyLen:      128,
			SaltLen:     32,
		}

		return nil
	}
}

// CustomArgon2 initializes argon2 with user defined settings.
// This is for specialized use and most users should use the DefaultArgon2 initializer instead.
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
