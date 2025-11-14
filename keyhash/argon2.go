package keyhash

import (
	"encoding"
	"encoding/base64"
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

func (a2k *argon2Key) MarshalText() ([]byte, error) {
	if err := a2k.validate(); err != nil {
		return nil, errors.Wrap(err, "Argon2Key failed validation when attempting to marshall")
	}

	encKey := make([]byte, base64.StdEncoding.EncodedLen(len(a2k.key)))
	base64.StdEncoding.Encode(encKey, a2k.key)

	return fmt.Appendf(nil, "$%d$%d$%d$%d$%v.%v", a2k.Memory, a2k.Times, a2k.Parallelism, a2k.KeyLen, a2k.salt, string(encKey)), nil
}

func (a2k *argon2Key) UnmarshalText([]byte) error {
	panic("Not Implemented")
}

type argon2Options struct {
	Memory      uint32
	Times       uint32
	Parallelism uint32
	KeyLen      uint32
}

func DefaultArgon2() Initialization {
	return func(kh *KeyHasher) error {
		kh.argon2 = &argon2Options{
			Memory:      7 * MiB,
			Times:       5,
			Parallelism: 1,
			KeyLen:      128,
		}

		return nil
	}
}

func CustomArgon2(memory, times, parallelism, keyLen uint32) Initialization {
	return func(kh *KeyHasher) error {
		kh.argon2 = &argon2Options{
			Memory:      memory,
			Times:       times,
			Parallelism: parallelism,
			KeyLen:      keyLen,
		}

		return nil
	}
}

func (a2 *argon2Options) key(plaintext []byte) *argon2Key {
	salt := randSalt()
	key := argon2.IDKey(plaintext, salt, a2.Times, a2.Memory, uint8(a2.Parallelism), a2.KeyLen)

	return &argon2Key{
		key:           key,
		salt:          salt,
		argon2Options: *a2,
	}
}
