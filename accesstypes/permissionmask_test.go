package accesstypes

import (
	"slices"
	"testing"
)

func TestPermissionMask(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		mask            PermissionMask
		wantZero        bool
		wantAllowed     []Permission
		wantDenied      []Permission
		wantPermissions []Permission
		wantString      string
	}{
		{
			name:            "zero mask is unrestricted",
			mask:            PermissionMask{},
			wantZero:        true,
			wantAllowed:     []Permission{List, Read, Create, Update, Delete, Execute, "Custom"},
			wantPermissions: nil,
			wantString:      "unrestricted",
		},
		{
			name:            "read-only mask",
			mask:            MaskPermissions(List, Read),
			wantAllowed:     []Permission{List, Read},
			wantDenied:      []Permission{Create, Update, Delete, Execute, "Custom"},
			wantPermissions: []Permission{List, Read},
			wantString:      "List,Read",
		},
		{
			name:            "mask with no permissions allows nothing",
			mask:            MaskPermissions(),
			wantDenied:      []Permission{List, Read, Create, Update, Delete, Execute},
			wantPermissions: []Permission{},
			wantString:      "",
		},
		{
			name:            "duplicates and the null permission are ignored",
			mask:            MaskPermissions(Read, Read, NullPermission, Execute),
			wantAllowed:     []Permission{Read, Execute},
			wantDenied:      []Permission{List, NullPermission},
			wantPermissions: []Permission{Execute, Read},
			wantString:      "Execute,Read",
		},
		{
			name:            "the null permission alone allows nothing",
			mask:            MaskPermissions(NullPermission),
			wantDenied:      []Permission{NullPermission, Read},
			wantPermissions: []Permission{},
			wantString:      "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.mask.IsZero(); got != tt.wantZero {
				t.Errorf("IsZero() = %v, want %v", got, tt.wantZero)
			}
			for _, perm := range tt.wantAllowed {
				if !tt.mask.Allows(perm) {
					t.Errorf("Allows(%q) = false, want true", perm)
				}
			}
			for _, perm := range tt.wantDenied {
				if tt.mask.Allows(perm) {
					t.Errorf("Allows(%q) = true, want false", perm)
				}
			}
			got := tt.mask.Permissions()
			if (got == nil) != (tt.wantPermissions == nil) || !slices.Equal(got, tt.wantPermissions) {
				t.Errorf("Permissions() = %#v, want %#v", got, tt.wantPermissions)
			}
			if got := tt.mask.String(); got != tt.wantString {
				t.Errorf("String() = %q, want %q", got, tt.wantString)
			}
		})
	}
}

func TestPermissionMask_PermissionsRoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		mask PermissionMask
	}{
		{name: "read-only", mask: MaskPermissions(Read, List)},
		{name: "single permission", mask: MaskPermissions(Execute)},
		{name: "allows nothing", mask: MaskPermissions()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rebuilt := MaskPermissions(tt.mask.Permissions()...)
			if !slices.Equal(tt.mask.Permissions(), rebuilt.Permissions()) {
				t.Errorf("round trip: rebuilt %#v, want %#v", rebuilt.Permissions(), tt.mask.Permissions())
			}
			if rebuilt.IsZero() {
				t.Error("rebuilt restricted mask reads as unrestricted")
			}
		})
	}
}
