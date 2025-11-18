package keyhash

import (
	"testing"
)

func TestKeyHasher_Key(t *testing.T) {
	t.Parallel()

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
			t.Parallel()

			kh, err := NewHasher(Argon2())
			if err != nil {
				t.Fatalf("could not construct receiver type: %v", err)
			}

			hash, err := kh.Hash(tt.plaintext)
			if err != nil {
				t.Fatal(err)
			}

			upgrade, err := kh.Compare(hash, []byte(string(tt.plaintext)+"a"))
			if err != nil {
				t.Logf("err: %v", err)
			}

			t.Logf("key needs upgraded: %v", upgrade)

			kh2, err := NewHasher(argon2WithOptions(1*1024, 2, 1, 8, 8))
			if err != nil {
				t.Fatalf("could not construct receiver type: %v", err)
			}

			upgrade, err = kh2.Compare(hash, tt.plaintext)
			if err != nil {
				t.Logf("err: %v", err)
			}

			t.Logf("key needs upgraded: %v", upgrade)
		})
	}
}
