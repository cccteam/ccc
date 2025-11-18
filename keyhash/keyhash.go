package keyhash

import (
	"bytes"
	"crypto/rand"
	"fmt"

	"github.com/go-playground/errors/v5"
)

const (
	dot        = '.'
	assignment = '='
	paramSep   = '$'
)

// HashAlgorithm is used to specify a configuration for a new KeyHasher.
type HashAlgorithm func(*KeyHasher) error

// KeyHasher is used for deriving and comparing
type KeyHasher struct {
	// bcrypt *BcryptOptions
	argon2 *argon2Options
}

// NewHasher takes configures a KeyHasher using the provided initialization function.
func NewHasher(algo HashAlgorithm) (*KeyHasher, error) {
	kh := &KeyHasher{}
	if err := algo(kh); err != nil {
		return nil, err
	}

	return kh, nil
}

// Compare compares a key of any supported type and a plaintext secret. It returns an error if they do not match, and a boolean indicating if the
// key needs to be upgraded(rehashed) with the current configuration.
func (kh *KeyHasher) Compare(hash, plaintext []byte) (bool, error) {
	firstSep := bytes.Index(hash, []byte{paramSep})
	if firstSep == 0 {
		return false, errors.New("did not find a kdf function name prefix")
	}

	switch kdfName := string(hash[:firstSep]); kdfName {
	case argon2Kdf:
		k := &argon2Key{}
		if err := k.UnmarshalText(hash[firstSep:]); err != nil {
			return false, errors.Wrap(err, "argon2Key.UnmarshalText()")
		}

		if !k.compare(plaintext) {
			return false, errors.New("key did not match")
		}

		if !k.cmpOptions(kh.argon2) {
			return true, nil
		}

		return false, nil

	default:
		return false, errors.Newf("did not recognize kdf function name prefix %s", kdfName)
	}
}

// Hash builds and returns a hashed and safe to store key based off the provided plaintext input.
func (kh *KeyHasher) Hash(plaintext []byte) ([]byte, error) {
	if kh.argon2 != nil {
		key, err := kh.argon2.key(plaintext)
		if err != nil {
			return nil, err
		}

		bKey, err := key.MarshalText()
		if err != nil {
			return nil, err
		}

		return fmt.Append([]byte(argon2Kdf), string(bKey)), nil
	}

	return nil, errors.New("KeyHasher is not initialized")
}

func newSalt(size uint32) ([]byte, error) {
	b := make([]byte, size)
	_, err := rand.Read(b)
	if err != nil {
		return nil, errors.Wrap(err, "rand.Read()")
	}

	return b, nil
}
