package accesstypes

import (
	"testing"
)

func TestUserFromStringAndBack(t *testing.T) {
	t.Parallel()

	type args struct {
		user string
	}
	tests := []struct {
		name string
		args args
		want User
	}{
		{
			name: "Administrator",
			args: args{
				user: "user:Administrator",
			},
			want: "Administrator",
		},
		{
			name: "Bad",
			args: args{
				user: "user:Bad",
			},
			want: User("Bad"),
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := UnmarshalUser(tt.args.user)
			if got != tt.want {
				t.Errorf("UserFromString() = %v, want %v", got, tt.want)
			}
			if gotString := got.Marshal(); gotString != tt.args.user {
				t.Errorf("User.String() = %v, want %v", gotString, tt.args.user)
			}
		})
	}
}

func TestUser_Marshal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		u         User
		want      string
		wantPanic bool
	}{
		{
			name: "Success",
			u:    "MyUser",
			want: userPrefix + "MyUser",
		},
		{
			name:      "Panic",
			u:         userPrefix + "MyUser",
			wantPanic: true,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			defer func() {
				didPanic := recover()
				if tt.wantPanic != (didPanic != nil) {
					t.Errorf("User.Mashal() panic = %v, wantPanic %v", didPanic, tt.wantPanic)
				}
			}()
			if got := tt.u.Marshal(); got != tt.want {
				t.Errorf("User.Marshal() = %v, want %v", got, tt.want)
			}
		})
	}
}
