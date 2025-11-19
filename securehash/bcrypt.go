package securehash

import (
	"encoding"

	"github.com/go-playground/errors/v5"
	"golang.org/x/crypto/bcrypt"
)

type bcryptKey struct {
	hash []byte

	bcryptOptions
}

var (
	_ encoding.TextMarshaler   = &bcryptKey{}
	_ encoding.TextUnmarshaler = &bcryptKey{}
)

func (bk *bcryptKey) MarshalText() ([]byte, error) {
	return bk.hash, nil
}

func (bk *bcryptKey) UnmarshalText(b []byte) error {
	bk.hash = b

	cost, err := bcrypt.Cost(b)
	if err != nil {
		return errors.Wrap(err, "bcrypt.Cost()")
	}

	bk.Cost = cost

	return nil
}

func (bk *bcryptKey) compare(plaintext []byte) error {
	if err := bcrypt.CompareHashAndPassword(bk.hash, plaintext); err != nil {
		return errors.Wrap(err, "bcrypt.CompareHashAndPassword()")
	}

	return nil
}

type bcryptOptions struct {
	Cost int
}

// Bcrypt initializes bcrypt with recommended settings.
func Bcrypt() HashAlgorithm {
	return func(kh *SecureHasher) error {
		kh.kdf = bcryptKdf
		kh.bcrypt = &bcryptOptions{Cost: 15}

		return nil
	}
}

func (bcr *bcryptOptions) cmpOptions(target *bcryptOptions) bool {
	return *bcr == *target
}

func (bcr *bcryptOptions) key(plaintext []byte) (*bcryptKey, error) {
	hash, err := bcrypt.GenerateFromPassword(plaintext, bcr.Cost)
	if err != nil {
		return nil, errors.Wrap(err, "bcrypt.GenerateFromPassword()")
	}

	return &bcryptKey{
		hash:          hash,
		bcryptOptions: *bcr,
	}, nil
}
