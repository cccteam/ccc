package securehash

import (
	"crypto/rand"
	"encoding"

	"github.com/go-playground/errors/v5"
)

const (
	bcryptKdf = "0"
	argon2Kdf = "1"
)

type comparer interface {
	compare(plaintext []byte) error
	encoding.TextMarshaler
	encoding.TextUnmarshaler
}

// HashAlgorithm is used to specify a configuration for a new SecureHasher.
type HashAlgorithm func(*SecureHasher) error

// SecureHasher is used for deriving and comparing
type SecureHasher struct {
	kdf    string
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
func (kh *SecureHasher) Compare(hash *Hash, plaintext []byte) (bool, error) {
	if err := hash.underlying.compare(plaintext); err != nil {
		return false, err
	}

	if kh.kdf != hash.kdf {
		return true, nil
	}

	// fixme(bswaney): add a method to secure hasher to remove these type asserts
	switch hash.kdf {
	case bcryptKdf:
		bk, _ := hash.underlying.(*bcryptKey)
		if !bk.cmpOptions(kh.bcrypt) {
			return true, nil
		}

	case argon2Kdf:
		a2k, _ := hash.underlying.(*argon2Key)
		if !a2k.cmpOptions(kh.argon2) {
			return true, nil
		}
	}

	return false, nil
}

// Hash builds and returns a hashed and safe to store key based off the provided plaintext input.
func (kh *SecureHasher) Hash(plaintext []byte) (*Hash, error) {
	h := &Hash{
		kdf: kh.kdf,
	}
	if kh.argon2 != nil {
		key, err := kh.argon2.key(plaintext)
		if err != nil {
			return nil, err
		}
		h.underlying = key

	} else if kh.bcrypt != nil {
		key, err := kh.bcrypt.key(plaintext)
		if err != nil {
			return nil, err
		}
		h.underlying = key
	} else {
		return nil, errors.New("SecureHasher is not initialized")
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
