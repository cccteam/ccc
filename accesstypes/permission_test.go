package accesstypes

import (
	"testing"
)

func TestPermissionFromStringAndBack(t *testing.T) {
	t.Parallel()

	type args struct {
		permission string
	}
	tests := []struct {
		name    string
		args    args
		want    Permission
		IsValid bool
	}{
		{
			name: "AddUser",
			args: args{
				permission: "perm:AddUser",
			},
			want:    Permission("AddUser"),
			IsValid: true,
		},
		{
			name: "ViewUser",
			args: args{
				permission: "perm:ViewUser",
			},
			want:    Permission("ViewUser"),
			IsValid: true,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := UnmarshalPermission(tt.args.permission)
			if got != tt.want {
				t.Errorf("PermissionFromString() = %v, want %v", got, tt.want)
			}
			if gotString := got.Marshal(); gotString != tt.args.permission {
				t.Errorf("Permission.String() = %v, want %v", gotString, tt.args.permission)
			}
		})
	}
}

func TestPermission_Marshal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		p         Permission
		want      string
		wantPanic bool
	}{
		{
			name: "Success",
			p:    "MyPermission",
			want: permissionPrefix + "MyPermission",
		},
		{
			name:      "Panic",
			p:         permissionPrefix + "MyPermission",
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
					t.Errorf("Permission.Marshal() panic = %v, wantPanic %v", didPanic, tt.wantPanic)
				}
			}()
			if got := tt.p.Marshal(); got != tt.want {
				t.Errorf("Permission.Marshal() = %v, want %v", got, tt.want)
			}
		})
	}
}
