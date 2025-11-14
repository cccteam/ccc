package keyhash

import (
	"crypto/rand"
	"encoding/base64"

	"github.com/go-playground/errors/v5"
)

const MiB = 1 << (10 * 2)

type Initialization func(*KeyHasher) error

type KeyHasher struct {
	// bcrypt *BcryptOptions
	argon2 *argon2Options
}

func NewKeyHasher(init Initialization) (*KeyHasher, error) {
	kh := &KeyHasher{}
	if err := init(kh); err != nil {
		return nil, err
	}

	return kh, nil
}

func (kh *KeyHasher) Compare(key, plaintext []byte) (kdfMatches bool, err error) {
	return false, nil
}

func (kh *KeyHasher) Key(plaintext []byte) ([]byte, error) {
	decPlaintext := make([]byte, base64.StdEncoding.DecodedLen(len(plaintext)))
	n, err := base64.StdEncoding.Decode(decPlaintext, plaintext)
	if err != nil {
		return nil, errors.Wrap(err, "base64.Encoding.Decode()")
	}

	if kh.argon2 != nil {
		return kh.argon2.key(decPlaintext[:n]).MarshalText()
	}

	return nil, errors.New("KeyHasher is not initialized")
}

func CompareKey(key, plaintext []byte) bool {
	return false
}

func randSalt() []byte {
	return []byte(rand.Text())
}
