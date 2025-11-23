package securehash

import (
	"testing"
)

func newHash(b string) *Hash {
	h := &Hash{}
	if err := h.UnmarshalText([]byte(b)); err != nil {
		panic(err)
	}

	return h
}

func TestSecureHasher_Compare(t *testing.T) {
	t.Parallel()

	type fields struct {
		kdf    string
		bcrypt *BcryptOptions
		argon2 *Argon2Options
	}
	type args struct {
		hash      *Hash
		plaintext string
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		want    bool
		wantErr bool
	}{
		{
			name: "bcrypt match",
			fields: fields{
				kdf:    bcryptKdf,
				bcrypt: Bcrypt(),
			},
			args: args{
				hash:      newHash("$2a$15$lNp5edkiKI3BoUguAhJLnu4Ge26n7SZS.F6kTGIDnjNpMOinzYSbK"),
				plaintext: "password",
			},
			want:    false,
			wantErr: false,
		},
		{
			name: "bcrypt match, upgrade cost",
			fields: fields{
				kdf:    bcryptKdf,
				bcrypt: &BcryptOptions{cost: 10},
			},
			args: args{
				hash:      newHash("$2a$15$lNp5edkiKI3BoUguAhJLnu4Ge26n7SZS.F6kTGIDnjNpMOinzYSbK"),
				plaintext: "password",
			},
			want:    true,
			wantErr: false,
		},
		{
			name: "bcrypt no match",
			fields: fields{
				kdf:    bcryptKdf,
				bcrypt: Bcrypt(),
			},
			args: args{
				hash:      newHash("$2a$15$lNp5edkiKI3BoUguAhJLnu4Ge26n7SZS.F6kTGIDnjNpMOinzYSbK"),
				plaintext: "wrongpassword",
			},
			want:    false,
			wantErr: true,
		},
		{
			name: "bcrypt match, upgrade to argon2",
			fields: fields{
				kdf:    argon2Kdf,
				argon2: Argon2(),
			},
			args: args{
				hash:      newHash("$2a$15$lNp5edkiKI3BoUguAhJLnu4Ge26n7SZS.F6kTGIDnjNpMOinzYSbK"),
				plaintext: "password",
			},
			want:    true,
			wantErr: false,
		},
		{
			name: "argon2 match",
			fields: fields{
				kdf:    argon2Kdf,
				argon2: Argon2(),
			},
			args: args{
				hash:      newHash("1$12288$3$1$53ANbCHo8otMACWky7sewg==.uswWZnTgaa6IuIxTlGNOfPaoUDfU3mZIcr3MLzjawdA="),
				plaintext: "password",
			},
			want:    false,
			wantErr: false,
		},
		{
			name: "argon2 match, upgrade memory",
			fields: fields{
				kdf:    argon2Kdf,
				argon2: argon2WithOptions(13*1024, 3, 1, 16, 32),
			},
			args: args{
				hash:      newHash("1$12288$3$1$53ANbCHo8otMACWky7sewg==.uswWZnTgaa6IuIxTlGNOfPaoUDfU3mZIcr3MLzjawdA="),
				plaintext: "password",
			},
			want:    true,
			wantErr: false,
		},
		{
			name: "argon2 match, upgrade times",
			fields: fields{
				kdf:    argon2Kdf,
				argon2: argon2WithOptions(12*1024, 4, 1, 16, 32),
			},
			args: args{
				hash:      newHash("1$12288$3$1$53ANbCHo8otMACWky7sewg==.uswWZnTgaa6IuIxTlGNOfPaoUDfU3mZIcr3MLzjawdA="),
				plaintext: "password",
			},
			want:    true,
			wantErr: false,
		},
		{
			name: "argon2 match, upgrade parallelism",
			fields: fields{
				kdf:    argon2Kdf,
				argon2: argon2WithOptions(12*1024, 3, 2, 16, 32),
			},
			args: args{
				hash:      newHash("1$12288$3$1$53ANbCHo8otMACWky7sewg==.uswWZnTgaa6IuIxTlGNOfPaoUDfU3mZIcr3MLzjawdA="),
				plaintext: "password",
			},
			want:    true,
			wantErr: false,
		},
		{
			name: "argon2 match, upgrade salt length",
			fields: fields{
				kdf:    argon2Kdf,
				argon2: argon2WithOptions(12*1024, 3, 1, 17, 32),
			},
			args: args{
				hash:      newHash("1$12288$3$1$53ANbCHo8otMACWky7sewg==.uswWZnTgaa6IuIxTlGNOfPaoUDfU3mZIcr3MLzjawdA="),
				plaintext: "password",
			},
			want:    true,
			wantErr: false,
		},
		{
			name: "argon2 match, upgrade key length",
			fields: fields{
				kdf:    argon2Kdf,
				argon2: argon2WithOptions(12*1024, 3, 1, 16, 33),
			},
			args: args{
				hash:      newHash("1$12288$3$1$53ANbCHo8otMACWky7sewg==.uswWZnTgaa6IuIxTlGNOfPaoUDfU3mZIcr3MLzjawdA="),
				plaintext: "password",
			},
			want:    true,
			wantErr: false,
		},
		{
			name: "argon2 no match",
			fields: fields{
				kdf:    argon2Kdf,
				argon2: Argon2(),
			},
			args: args{
				hash:      newHash("1$12288$3$1$53ANbCHo8otMACWky7sewg==.uswWZnTgaa6IuIxTlGNOfPaoUDfU3mZIcr3MLzjawdA="),
				plaintext: "wrongpassword",
			},
			want:    false,
			wantErr: true,
		},
		{
			name: "argon2 match, upgrade to bcrypt",
			fields: fields{
				kdf:    bcryptKdf,
				bcrypt: Bcrypt(),
			},
			args: args{
				hash:      newHash("1$12288$3$1$53ANbCHo8otMACWky7sewg==.uswWZnTgaa6IuIxTlGNOfPaoUDfU3mZIcr3MLzjawdA="),
				plaintext: "password",
			},
			want:    true,
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			kh := &SecureHasher{
				kdf:    tt.fields.kdf,
				bcrypt: tt.fields.bcrypt,
				argon2: tt.fields.argon2,
			}

			got, err := kh.Compare(tt.args.hash, tt.args.plaintext)
			if (err != nil) != tt.wantErr {
				t.Errorf("SecureHasher.Compare() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("SecureHasher.Compare() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSecureHasher_Hash(t *testing.T) {
	t.Parallel()

	type fields struct {
		kdf    string
		bcrypt *BcryptOptions
		argon2 *Argon2Options
	}
	type args struct {
		plaintext string
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr bool
	}{
		{
			name: "bcrypt",
			fields: fields{
				kdf:    bcryptKdf,
				bcrypt: Bcrypt(),
			},
			args: args{
				plaintext: "password",
			},
		},
		{
			name: "argon2",
			fields: fields{
				kdf:    argon2Kdf,
				argon2: Argon2(),
			},
			args: args{
				plaintext: "password",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			kh := &SecureHasher{
				kdf:    tt.fields.kdf,
				bcrypt: tt.fields.bcrypt,
				argon2: tt.fields.argon2,
			}
			got, err := kh.Hash(tt.args.plaintext)
			if (err != nil) != tt.wantErr {
				t.Errorf("SecureHasher.Hash() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err == nil && got == nil {
				t.Errorf("SecureHasher.Hash() returned nil hash and nil error")
				return
			}

			if err == nil {
				// Check that the hash can be compared successfully
				upgrade, err := kh.Compare(got, tt.args.plaintext)
				if err != nil {
					t.Errorf("SecureHasher.Compare() with correct password failed, error = %v", err)
				}
				if upgrade {
					t.Errorf("SecureHasher.Compare() with correct password indicated upgrade needed")
				}

				// Check that the hash fails with an incorrect password
				_, err = kh.Compare(got, "wrongpassword")
				if err == nil {
					t.Errorf("SecureHasher.Compare() with incorrect password succeeded, but should have failed")
				}
			}
		})
	}
}
