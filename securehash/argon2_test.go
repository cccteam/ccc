package securehash

import (
	"reflect"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func Test_argon2Key_MarshalText(t *testing.T) {
	t.Parallel()

	type fields struct {
		key           []byte
		salt          []byte
		argon2Options Argon2Options
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
				argon2Options: Argon2Options{
					memory:      12,
					times:       8,
					parallelism: 4,
					saltLen:     7,
					keyLen:      6,
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
				Argon2Options: tt.fields.argon2Options,
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

func Test_argon2Key_UnmarshalText(t *testing.T) {
	t.Parallel()

	type args struct {
		b []byte
	}
	tests := []struct {
		name              string
		args              args
		wantKey           []byte
		wantSalt          []byte
		wantArgon2Options Argon2Options
		wantErr           bool
	}{
		{
			name: "t1",
			args: args{
				b: []byte(`$12$8$4$bXktc2FsdA==.bXkta2V5`),
			},
			wantSalt: []byte("my-salt"),
			wantKey:  []byte("my-key"),
			wantArgon2Options: Argon2Options{
				memory:      12,
				times:       8,
				parallelism: 4,
				saltLen:     7,
				keyLen:      6,
			},
		},
		{
			name: "bad memory",
			args: args{
				b: []byte(`$x$8$4$bXktc2FsdA==.bXkta2V5`),
			},
			wantErr: true,
		},
		{
			name: "bad times",
			args: args{
				b: []byte(`$12$x$4$bXktc2FsdA==.bXkta2V5`),
			},
			wantArgon2Options: Argon2Options{
				memory: 12,
			},
			wantErr: true,
		},
		{
			name: "bad parallelism",
			args: args{
				b: []byte(`$12$8$x$bXktc2FsdA==.bXkta2V5`),
			},
			wantArgon2Options: Argon2Options{
				memory: 12,
				times:  8,
			},
			wantErr: true,
		},
		{
			name: "bad salt",
			args: args{
				b: []byte(`$12$8$4$x.bXkta2V5`),
			},
			wantArgon2Options: Argon2Options{
				memory:      12,
				times:       8,
				parallelism: 4,
			},
			wantErr: true,
		},
		{
			name: "bad key",
			args: args{
				b: []byte(`$12$8$4$bXktc2FsdA==.x`),
			},
			wantSalt: []byte("my-salt"),
			wantArgon2Options: Argon2Options{
				memory:      12,
				times:       8,
				parallelism: 4,
				saltLen:     7,
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := &argon2Key{}
			if err := got.UnmarshalText(tt.args.b); (err != nil) != tt.wantErr {
				t.Errorf("argon2Key.UnmarshalText() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !reflect.DeepEqual(got.key, tt.wantKey) {
				t.Errorf("argon2Key.UnmarshalText(): key = %v, want %v", string(got.key), string(tt.wantKey))
			}
			if !reflect.DeepEqual(got.salt, tt.wantSalt) {
				t.Errorf("argon2Key.UnmarshalText(): salt = %v, want %v", string(got.salt), string(tt.wantSalt))
			}
			if diff := cmp.Diff(tt.wantArgon2Options, got.Argon2Options, cmp.AllowUnexported(Argon2Options{})); diff != "" {
				t.Errorf("UnmarshalText() Argon2Options mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
