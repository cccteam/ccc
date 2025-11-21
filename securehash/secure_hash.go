// Package securehash provides a secure and easy way to hash and compare secrets.
// It supports bcrypt and argon2 as hashing algorithms and can be used to upgrade
// hashes over time as security best practices change.
//
// A SecureHasher is created with a specific algorithm and its parameters. It can
// then be used to hash new secrets or compare existing hashes with a plaintext
// secret. When comparing, it will also indicate if the hash needs to be upgraded
// to the current configuration.
//
// The Hash type represents a hashed secret and can be marshaled to and unmarshaled
// from text for easy storage.
package securehash

import (
	"crypto/rand"

	"github.com/go-playground/errors/v5"
)

const (
	bcryptKdf = ""
	argon2Kdf = "1"
)

// SecureHasher is used for deriving and comparing
type SecureHasher struct {
	kdf    string
	bcrypt *BcryptOptions
	argon2 *Argon2Options
}

// New configures a SecureHasher using the provided initialization function.
func New(algo HashAlgorithm) *SecureHasher {
	kh := &SecureHasher{}
	algo.apply(kh)

	return kh
}

// Compare compares a key of any supported type and a plaintext secret. It returns an error if they do not match, and a boolean indicating if the
// key needs to be upgraded(rehashed) with the current configuration.
func (s *SecureHasher) Compare(hash *Hash, plaintext []byte) (bool, error) {
	if err := hash.underlying.compare(plaintext); err != nil {
		return false, err
	}

	if s.kdf != hash.kdf {
		return true, nil
	}

	// fixme(bswaney): add a method to secure hasher to remove these type asserts
	switch hash.kdf {
	case bcryptKdf:
		bk, _ := hash.underlying.(*bcryptKey)
		if !bk.cmpOptions(s.bcrypt) {
			return true, nil
		}

	case argon2Kdf:
		a2k, _ := hash.underlying.(*argon2Key)
		if !a2k.cmpOptions(s.argon2) {
			return true, nil
		}
	}

	return false, nil
}

// Hash builds and returns a hashed and safe to store key based off the provided plaintext input.
func (s *SecureHasher) Hash(plaintext []byte) (*Hash, error) {
	h := &Hash{
		kdf: s.kdf,
	}
	switch s.kdf {
	case argon2Kdf:
		key, err := s.argon2.key(plaintext)
		if err != nil {
			return nil, err
		}
		h.underlying = key
	case bcryptKdf:
		key, err := s.bcrypt.key(plaintext)
		if err != nil {
			return nil, err
		}
		h.underlying = key
	default:
		panic("internal error: invalid kdf")
	}

	return h, nil
}

func newSalt(size uint32) ([]byte, error) {
	b := make([]byte, size)
	_, err := rand.Read(b)
	if err != nil {
		return nil, errors.Wrap(err, "rand.Read()")
	}

	return b, nil
}
