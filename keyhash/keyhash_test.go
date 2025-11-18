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

			kh, err := NewKeyHasher(CustomArgon2(1*MiB, 1, 1, 8, 8))
			if err != nil {
				t.Fatalf("could not construct receiver type: %v", err)
			}

			key, err := kh.Key(tt.plaintext)
			if err != nil {
				t.Fatal(err)
			}

			upgrade, err := kh.Compare(key, []byte(string(tt.plaintext)+"a"))
			if err != nil {
				t.Logf("err: %v", err)
			}

			t.Logf("key needs upgraded: %v", upgrade)

			kh2, err := NewKeyHasher(CustomArgon2(1*MiB, 2, 1, 8, 8))
			if err != nil {
				t.Fatalf("could not construct receiver type: %v", err)
			}

			upgrade, err = kh2.Compare(key, tt.plaintext)
			if err != nil {
				t.Logf("err: %v", err)
			}

			t.Logf("key needs upgraded: %v", upgrade)
		})
	}
}
