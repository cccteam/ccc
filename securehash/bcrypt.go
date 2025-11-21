package securehash

import (
	"encoding"

	"github.com/go-playground/errors/v5"
	"golang.org/x/crypto/bcrypt"
)

// BcryptOptions hold options for generating a bcrypt hash
type BcryptOptions struct {
	cost int
}

// Bcrypt initializes bcrypt with recommended settings.
func Bcrypt() *BcryptOptions {
	return &BcryptOptions{cost: 15}
}

func (bcr *BcryptOptions) apply(kh *SecureHasher) {
	kh.kdf = bcryptKdf
	kh.bcrypt = bcr
}

func (bcr *BcryptOptions) cmpOptions(target *BcryptOptions) bool {
	return *bcr == *target
}

func (bcr *BcryptOptions) key(plaintext []byte) (*bcryptKey, error) {
	hash, err := bcrypt.GenerateFromPassword(plaintext, bcr.cost)
	if err != nil {
		return nil, errors.Wrap(err, "bcrypt.GenerateFromPassword()")
	}

	return &bcryptKey{
		hash:          hash,
		BcryptOptions: *bcr,
	}, nil
}

var (
	_ encoding.TextMarshaler   = &bcryptKey{}
	_ encoding.TextUnmarshaler = &bcryptKey{}
)

type bcryptKey struct {
	hash []byte

	BcryptOptions
}

func (bk *bcryptKey) MarshalText() ([]byte, error) {
	return bk.hash, nil
}

func (bk *bcryptKey) UnmarshalText(b []byte) error {
	bk.hash = b

	cost, err := bcrypt.Cost(b)
	if err != nil {
		return errors.Wrap(err, "bcrypt.Cost()")
	}

	bk.cost = cost

	return nil
}

func (bk *bcryptKey) compare(plaintext []byte) error {
	if err := bcrypt.CompareHashAndPassword(bk.hash, plaintext); err != nil {
		return errors.Wrap(err, "bcrypt.CompareHashAndPassword()")
	}

	return nil
}
