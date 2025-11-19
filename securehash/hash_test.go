package securehash

import (
	"reflect"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestHash_MarshalText(t *testing.T) {
	tests := []struct {
		name    string
		h       *Hash
		want    []byte
		wantErr bool
	}{
		{
			name: "Bcrypt",
			h: &Hash{
				kdf: bcryptKdf,
				underlying: &bcryptKey{
					hash: []byte("$2a$15$sJmPZT22fY8WmU5IlKvlWO7W6io2lxylIyElzH9KmfA/Nr6v/Vc4q"),
				},
			},
			want: []byte("0$2a$15$sJmPZT22fY8WmU5IlKvlWO7W6io2lxylIyElzH9KmfA/Nr6v/Vc4q"),
		},
		{
			name: "Argon2ID",
			h: &Hash{
				kdf: argon2Kdf,
				underlying: &argon2Key{
					key:  []byte("my-key"),
					salt: []byte("my-salt"),
					argon2Options: argon2Options{
						Memory:      12,
						Times:       8,
						Parallelism: 4,
						SaltLen:     7,
						KeyLen:      6,
					},
				},
			},
			want: []byte(`1$12$8$4$6d792d73616c74.6d792d6b6579`),
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := tt.h.MarshalText()
			if (err != nil) != tt.wantErr {
				t.Errorf("Hash.MarshalText() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Hash.MarshalText() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestHash_UnmarshalText(t *testing.T) {
	tests := []struct {
		name    string
		hash    []byte
		want    Hash
		wantErr bool
	}{
		{
			name: "Bcrypt",
			hash: []byte("0$2a$15$sJmPZT22fY8WmU5IlKvlWO7W6io2lxylIyElzH9KmfA/Nr6v/Vc4q"),
			want: Hash{
				kdf: bcryptKdf,
				underlying: &bcryptKey{
					hash: []byte("$2a$15$sJmPZT22fY8WmU5IlKvlWO7W6io2lxylIyElzH9KmfA/Nr6v/Vc4q"),
					bcryptOptions: bcryptOptions{
						Cost: 15,
					},
				},
			},
		},
		{
			name: "Argon2ID",
			hash: []byte(`1$12$8$4$6d792d73616c74.6d792d6b6579`),
			want: Hash{
				kdf: argon2Kdf,
				underlying: &argon2Key{
					key:  []byte("my-key"),
					salt: []byte("my-salt"),
					argon2Options: argon2Options{
						Memory:      12,
						Times:       8,
						Parallelism: 4,
						SaltLen:     7,
						KeyLen:      6,
					},
				},
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h := &Hash{}

			if err := h.UnmarshalText(tt.hash); (err != nil) != tt.wantErr {
				t.Errorf("Hash.UnmarshalText() error = %v, wantErr %v", err, tt.wantErr)
			}

			if diff := (cmp.Diff(tt.want.kdf, h.kdf)); diff != "" {
				t.Errorf("kdf does not match, diff = %s", diff)
			}
			if diff := (cmp.Diff(tt.want.underlying, h.underlying, cmp.AllowUnexported(argon2Key{}, bcryptKey{}))); diff != "" {
				t.Errorf("underlying object does not match, diff = %s", diff)
			}
		})
	}
}
