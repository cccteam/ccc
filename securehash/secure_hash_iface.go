package securehash

import "encoding"

type comparer interface {
	compare(plaintext []byte) error
	encoding.TextMarshaler
	encoding.TextUnmarshaler
}

// HashAlgorithm is used to specify a configuration for a new SecureHasher.
type HashAlgorithm interface {
	apply(*SecureHasher)
}
