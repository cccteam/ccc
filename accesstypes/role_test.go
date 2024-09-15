package accesstypes

import (
	"testing"
)

func TestRoleFromStringAndBack(t *testing.T) {
	t.Parallel()

	type args struct {
		role string
	}
	tests := []struct {
		name string
		args args
		want Role
	}{
		{
			name: "Administrator",
			args: args{
				role: "role:Administrator",
			},
			want: "Administrator",
		},
		{
			name: "Bad",
			args: args{
				role: "role:Bad",
			},
			want: Role("Bad"),
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := UnmarshalRole(tt.args.role)
			if got != tt.want {
				t.Errorf("RoleFromString() = %v, want %v", got, tt.want)
			}
			if gotString := got.Marshal(); gotString != tt.args.role {
				t.Errorf("Role.String() = %v, want %v", gotString, tt.args.role)
			}
		})
	}
}

func TestRole_Marshal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		r         Role
		want      string
		wantPanic bool
	}{
		{
			name: "Success",
			r:    "MyRole",
			want: rolePrefix + "MyRole",
		},
		{
			name:      "Panic",
			r:         rolePrefix + "MyRole",
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
			if got := tt.r.Marshal(); got != tt.want {
				t.Errorf("Role.Marshal() = %v, want %v", got, tt.want)
			}
		})
	}
}
