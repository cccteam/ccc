package securehash

import (
	"reflect"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestHash_MarshalText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		h       *Hash
		want    []byte
		wantErr bool
	}{
		{
			name: "Bcrypt",
			h: &Hash{
				underlying: &bcryptHash{
					hash: []byte("$2a$15$sJmPZT22fY8WmU5IlKvlWO7W6io2lxylIyElzH9KmfA/Nr6v/Vc4q"),
				},
			},
			want: []byte("$2a$15$sJmPZT22fY8WmU5IlKvlWO7W6io2lxylIyElzH9KmfA/Nr6v/Vc4q"),
		},
		{
			name: "Argon2ID",
			h: &Hash{
				underlying: &argon2Key{
					key:  []byte("my-key"),
					salt: []byte("my-salt"),
					Argon2Options: Argon2Options{
						memory:      12,
						times:       8,
						parallelism: 4,
						saltLen:     7,
						keyLen:      6,
					},
				},
			},
			want: []byte(`1$12$8$4$bXktc2FsdA==.bXkta2V5`),
		},
	}
	for _, tt := range tests {
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
	t.Parallel()

	tests := []struct {
		name    string
		hash    []byte
		want    Hash
		wantErr bool
	}{
		{
			name: "Bcrypt",
			hash: []byte("$2a$15$sJmPZT22fY8WmU5IlKvlWO7W6io2lxylIyElzH9KmfA/Nr6v/Vc4q"),
			want: Hash{
				underlying: &bcryptHash{
					hash: []byte("$2a$15$sJmPZT22fY8WmU5IlKvlWO7W6io2lxylIyElzH9KmfA/Nr6v/Vc4q"),
					BcryptOptions: BcryptOptions{
						cost: 15,
					},
				},
			},
		},
		{
			name: "Argon2ID",
			hash: []byte(`1$12$8$4$bXktc2FsdA==.bXkta2V5`),
			want: Hash{
				underlying: &argon2Key{
					salt: []byte("my-salt"),
					key:  []byte("my-key"),
					Argon2Options: Argon2Options{
						memory:      12,
						times:       8,
						parallelism: 4,
						saltLen:     7,
						keyLen:      6,
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := &Hash{}
			if err := h.UnmarshalText(tt.hash); (err != nil) != tt.wantErr {
				t.Errorf("Hash.UnmarshalText() error = %v, wantErr %v", err, tt.wantErr)
			}

			if diff := (cmp.Diff(tt.want.underlying, h.underlying, cmp.AllowUnexported(argon2Key{}, Argon2Options{}, bcryptHash{}, BcryptOptions{}))); diff != "" {
				t.Errorf("UnmarshalText() mismatch (-want +got): underlying object does not match\n%s", diff)
			}
		})
	}
}

func TestHash_EncodeSpanner(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		h       *Hash
		want    string
		wantErr bool
	}{
		{
			name: "Bcrypt",
			h: &Hash{
				underlying: &bcryptHash{
					hash: []byte("$2a$15$sJmPZT22fY8WmU5IlKvlWO7W6io2lxylIyElzH9KmfA/Nr6v/Vc4q"),
				},
			},
			want: "$2a$15$sJmPZT22fY8WmU5IlKvlWO7W6io2lxylIyElzH9KmfA/Nr6v/Vc4q",
		},
		{
			name: "Argon2ID",
			h: &Hash{
				underlying: &argon2Key{
					key:  []byte("my-key"),
					salt: []byte("my-salt"),
					Argon2Options: Argon2Options{
						memory:      12,
						times:       8,
						parallelism: 4,
						saltLen:     7,
						keyLen:      6,
					},
				},
			},
			want: `1$12$8$4$bXktc2FsdA==.bXkta2V5`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := tt.h.EncodeSpanner()
			if (err != nil) != tt.wantErr {
				t.Errorf("Hash.EncodeSpanner() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Hash.EncodeSpanner() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestHash_DecodeSpanner(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		hash    any
		want    Hash
		wantErr bool
	}{
		{
			name: "Bcrypt string",
			hash: "$2a$15$sJmPZT22fY8WmU5IlKvlWO7W6io2lxylIyElzH9KmfA/Nr6v/Vc4q",
			want: Hash{
				underlying: &bcryptHash{
					hash: []byte("$2a$15$sJmPZT22fY8WmU5IlKvlWO7W6io2lxylIyElzH9KmfA/Nr6v/Vc4q"),
					BcryptOptions: BcryptOptions{
						cost: 15,
					},
				},
			},
		},
		{
			name: "Argon2ID string",
			hash: `1$12$8$4$bXktc2FsdA==.bXkta2V5`,
			want: Hash{
				underlying: &argon2Key{
					salt: []byte("my-salt"),
					key:  []byte("my-key"),
					Argon2Options: Argon2Options{
						memory:      12,
						times:       8,
						parallelism: 4,
						saltLen:     7,
						keyLen:      6,
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := &Hash{}
			if err := h.DecodeSpanner(tt.hash); (err != nil) != tt.wantErr {
				t.Errorf("Hash.DecodeSpanner() error = %v, wantErr %v", err, tt.wantErr)
			}

			if diff := (cmp.Diff(tt.want.underlying, h.underlying, cmp.AllowUnexported(argon2Key{}, Argon2Options{}, bcryptHash{}, BcryptOptions{}))); diff != "" {
				t.Errorf("DecodeSpanner() mismatch (-want +got): underlying object does not match\n%s", diff)
			}
		})
	}
}
