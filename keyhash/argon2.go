package keyhash

import (
	"bytes"
	"encoding"
	"encoding/base64"
	"encoding/binary"
	"fmt"

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

func (a2k *argon2Key) validate() error {
	switch {
	case a2k.Memory == 0:
		return errors.New("found a zero value for Argon2 memory parameter")
	case a2k.Times == 0:
		return errors.New("found a zero value for Argon2 times parameter")
	case a2k.Parallelism == 0:
		return errors.New("found a zero value for Argon2 parallelism parameter")
	case a2k.KeyLen == 0:
		return errors.New("found a zero value for Argon2 key length parameter")
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

	b := fmt.Appendf(nil, "$memory=%d", a2k.Memory)
	b = fmt.Appendf(b, "$times=%d", a2k.Times)
	b = fmt.Appendf(b, "$parallelism=%d", a2k.Parallelism)
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

	if keyN > 1<<32 {
		return errors.Newf("key length overflows uint32")
	}
	keylen := uint32(keyN)

	paramsParts := bytes.Split(paramsPart[1:], []byte{paramSep})
	if len(paramsParts) != 4 {
		return errors.Newf("expected to find a four params in input, found %d", len(paramsParts))
	}

	paramMap := make(map[string][]byte, 4)
	for _, param := range paramsParts {
		parts := bytes.SplitN(param, []byte{assignment}, 2)
		if len(parts) != 2 {
			return errors.Newf("did not find params in \"$<param name>%s<param value>\" format", string(assignment))
		}

		k, v := parts[0], parts[1]
		if len(v) == 0 {
			return errors.Newf("found empty value for key %s", k)
		}

		if paramMap[string(k)] != nil {
			return errors.Newf("found duplicated param in input %s", string(k))
		}

		switch strK := string(k); strK {
		case memoryP, timesP, saltP:
			paramMap[strK] = v
		case parallelismP:
			if len(v) > 1 {
				return errors.Newf("expected 8bit value for key %s, found %d", k, 1<<(len(v)-1)*8)
			}
			paramMap[strK] = v
		default:
			return errors.Newf("did not recognize param key %s", k)
		}
	}

	salt := paramMap[saltP]
	decSalt := make([]byte, base64.StdEncoding.DecodedLen(len(salt)))
	saltN, err := base64.StdEncoding.Decode(decSalt, salt)
	if err != nil {
		return errors.Wrap(err, "base64.Encoding.Decode()")
	}

	a2k.key = decKey[:keyN]
	a2k.KeyLen = keylen
	a2k.salt = decSalt[:saltN]
	a2k.Memory = binary.BigEndian.Uint32(paramMap[memoryP]) // TODO(bswaney): move these conversions up into the switch
	a2k.Times = binary.BigEndian.Uint32(paramMap[timesP])
	a2k.Parallelism = paramMap[parallelismP][0]

	return nil
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
	return false
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
