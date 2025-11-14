package keyhash

import (
	"encoding/base64"
	"testing"
)

func TestKeyHasher_Key(t *testing.T) {
	tests := []struct {
		name      string
		plaintext []byte
	}{
		{
			name:      "t1",
			plaintext: []byte("hello"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kh, err := NewKeyHasher(testArgon2())
			if err != nil {
				t.Fatalf("could not construct receiver type: %v", err)
			}

			encPassword := make([]byte, base64.StdEncoding.EncodedLen(len(tt.plaintext)))
			base64.StdEncoding.Encode(encPassword, tt.plaintext)

			key, err := kh.Key(encPassword)
			if err != nil {
				t.Fatal(err)
			}

			t.Log(key)
		})
	}
}

func testArgon2() Initialization {
	return func(kh *KeyHasher) error {
		kh.argon2 = &argon2Options{
			Memory:      1 * MiB,
			Times:       1,
			Parallelism: 1,
			KeyLen:      128,
		}

		return nil
	}
}

// "\x85\xfa\xbe\xa6 B\xf3\xb9j>\xa1\xf1\xb31f\xb1\xd1k\xba\x1c\xb0\xac\x83\x0e\x9eM=:ĔQm\x88\xe7d\xb5\n\x12\xea#\xc1\x1e\xb6>p\x0fk\x10\xae\xde`S\x01t\xa9\xa41z܇\xa6c\xe0\x99Zo^\xcfϭ\x9b2\xcf\x1cnY\x84\v\v\x8bz\xe9l(Q\xb4\xf3\xe7C\xbfpۺ\x12\x89T@\\@\xd2¦q\xecݰ\xb2\xea\x03_4\xc1>\xc4T\x18\xe9R\f\xf0\xb6\v\x10g\x91\xf4\x170"
