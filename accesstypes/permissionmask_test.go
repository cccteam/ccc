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
			mask:            MaskPermissions(DenyAll(), List, Read),
			wantAllowed:     []Permission{List, Read},
			wantDenied:      []Permission{Create, Update, Delete, Execute, "Custom"},
			wantPermissions: []Permission{List, Read},
			wantString:      "List,Read",
		},
		{
			name:            "mask with no permissions allows nothing",
			mask:            DenyAll(),
			wantDenied:      []Permission{List, Read, Create, Update, Delete, Execute},
			wantPermissions: []Permission{},
			wantString:      "",
		},
		{
			name:            "duplicates and the null permission are ignored",
			mask:            MaskPermissions(DenyAll(), Read, Read, NullPermission, Execute),
			wantAllowed:     []Permission{Read, Execute},
			wantDenied:      []Permission{List, NullPermission},
			wantPermissions: []Permission{Execute, Read},
			wantString:      "Execute,Read",
		},
		{
			name:            "the null permission alone allows nothing",
			mask:            MaskPermissions(DenyAll(), NullPermission),
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
		{name: "read-only", mask: MaskPermissions(DenyAll(), Read, List)},
		{name: "single permission", mask: MaskPermissions(DenyAll(), Execute)},
		{name: "allows nothing", mask: DenyAll()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rebuilt := MaskPermissions(DenyAll(), tt.mask.Permissions()...)
			if !slices.Equal(tt.mask.Permissions(), rebuilt.Permissions()) {
				t.Errorf("round trip: rebuilt %#v, want %#v", rebuilt.Permissions(), tt.mask.Permissions())
			}
			if rebuilt.IsZero() {
				t.Error("rebuilt restricted mask reads as unrestricted")
			}
		})
	}
}

// TestMaskPermissions_fallback pins the empty-list rule: with no permissions
// left after filtering, the caller's fallback is the mask — whatever it is —
// and a non-empty list ignores the fallback entirely.
func TestMaskPermissions_fallback(t *testing.T) {
	t.Parallel()

	readOnly := MaskPermissions(DenyAll(), List, Read)

	tests := []struct {
		name     string
		fallback PermissionMask
		perms    []Permission
		want     PermissionMask
	}{
		{name: "no list falls back to unrestricted", fallback: AllowAll(), perms: nil, want: AllowAll()},
		{name: "no list falls back to nothing", fallback: DenyAll(), perms: []Permission{}, want: DenyAll()},
		{name: "no list falls back to a narrower default", fallback: readOnly, perms: nil, want: readOnly},
		{name: "only the null permission is an empty list", fallback: AllowAll(), perms: []Permission{NullPermission}, want: AllowAll()},
		{name: "a list ignores the fallback", fallback: AllowAll(), perms: []Permission{Execute}, want: MaskPermissions(DenyAll(), Execute)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := MaskPermissions(tt.fallback, tt.perms...)
			if got.IsZero() != tt.want.IsZero() || !slices.Equal(got.Permissions(), tt.want.Permissions()) {
				t.Errorf("MaskPermissions() = %v, want %v", got, tt.want)
			}
		})
	}
}
