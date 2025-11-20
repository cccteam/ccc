package securehash

import (
	"reflect"
	"testing"
)

func Test_argon2Key_MarshalText(t *testing.T) {
	t.Parallel()

	type fields struct {
		key           []byte
		salt          []byte
		argon2Options argon2Options
	}
	tests := []struct {
		name    string
		fields  fields
		want    []byte
		wantErr bool
	}{
		{
			name: "t1",
			fields: fields{
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
			want: []byte(`$12$8$4$bXktc2FsdA==.bXkta2V5`),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			a2k := &argon2Key{
				key:           tt.fields.key,
				salt:          tt.fields.salt,
				argon2Options: tt.fields.argon2Options,
			}
			got, err := a2k.MarshalText()
			if (err != nil) != tt.wantErr {
				t.Errorf("argon2Key.MarshalText() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("argon2Key.MarshalText() = %v, want %v", string(got), string(tt.want))
			}
		})
	}
}
