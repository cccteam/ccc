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

func (b *BcryptOptions) apply(kh *SecureHasher) {
	kh.kdf = bcryptKdf
	kh.bcrypt = b
}

func (b *BcryptOptions) cmpOptions(target *BcryptOptions) bool {
	return *b == *target
}

func (b *BcryptOptions) key(plaintext []byte) (*bcryptKey, error) {
	hash, err := bcrypt.GenerateFromPassword(plaintext, b.cost)
	if err != nil {
		return nil, errors.Wrap(err, "bcrypt.GenerateFromPassword()")
	}

	return &bcryptKey{
		hash:          hash,
		BcryptOptions: *b,
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

func (b *bcryptKey) MarshalText() ([]byte, error) {
	return b.hash, nil
}

func (b *bcryptKey) UnmarshalText(hash []byte) error {
	b.hash = hash

	cost, err := bcrypt.Cost(hash)
	if err != nil {
		return errors.Wrap(err, "bcrypt.Cost()")
	}

	b.cost = cost

	return nil
}

func (b *bcryptKey) compare(plaintext []byte) error {
	if err := bcrypt.CompareHashAndPassword(b.hash, plaintext); err != nil {
		return errors.Wrap(err, "bcrypt.CompareHashAndPassword()")
	}

	return nil
}
