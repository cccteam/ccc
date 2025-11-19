package securehash

import (
	"bytes"
	"crypto/rand"
	"fmt"

	"github.com/go-playground/errors/v5"
)

const (
	bcryptKdf = "0"
	argon2Kdf = "1"
)

// HashAlgorithm is used to specify a configuration for a new SecureHasher.
type HashAlgorithm func(*SecureHasher) error

// SecureHasher is used for deriving and comparing
type SecureHasher struct {
	bcrypt *bcryptOptions
	argon2 *argon2Options
}

// NewSecureHasher configures a SecureHasher using the provided initialization function.
func NewSecureHasher(algo HashAlgorithm) (*SecureHasher, error) {
	kh := &SecureHasher{}
	if err := algo(kh); err != nil {
		return nil, err
	}

	return kh, nil
}

// Compare compares a key of any supported type and a plaintext secret. It returns an error if they do not match, and a boolean indicating if the
// key needs to be upgraded(rehashed) with the current configuration.
func (kh *SecureHasher) Compare(hash, plaintext []byte) (bool, error) {
	firstSep := bytes.Index(hash, []byte{sep})
	if firstSep == 0 {
		hash = append([]byte(bcryptKdf), hash...)
	}

	switch kdfName := string(hash[:firstSep]); kdfName {
	case bcryptKdf:
		k := &bcryptKey{}
		if err := k.UnmarshalText(hash[firstSep:]); err != nil {
			return false, errors.Wrap(err, "bcryptKey.UnmarshalText()")
		}

		if err := k.compare(plaintext); err != nil {
			return false, err
		}

		if !k.cmpOptions(kh.bcrypt) || firstSep == 0 {
			return true, nil
		}

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

	default:
		return false, errors.Newf("did not recognize kdf function name prefix %s", kdfName)
	}

	return false, nil
}

// Hash builds and returns a hashed and safe to store key based off the provided plaintext input.
func (kh *SecureHasher) Hash(plaintext []byte) ([]byte, error) {
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
	} else if kh.bcrypt != nil {
		key, err := kh.bcrypt.key(plaintext)
		if err != nil {
			return nil, err
		}

		bKey, err := key.MarshalText()
		if err != nil {
			return nil, err
		}

		return fmt.Append([]byte(bcryptKdf), string(bKey)), nil
	}

	return nil, errors.New("SecureHasher is not initialized")
}

func newSalt(size uint32) ([]byte, error) {
	b := make([]byte, size)
	_, err := rand.Read(b)
	if err != nil {
		return nil, errors.Wrap(err, "rand.Read()")
	}

	return b, nil
}
