package accesstypes

import (
	"slices"
	"strings"
)

// PermissionMask is an allowlist intersection over the permission axis — the
// attenuation an impersonated session carries. A mask can only narrow: a
// masked check first asks the mask whether the permission is allowed at all
// and only then asks policy, so nothing a mask does can grant what policy
// denies.
//
// The zero PermissionMask is unrestricted: every permission passes (AllowAll).
// A mask built with MaskPermissions allows exactly the listed permissions, and
// DenyAll allows none. The unrestricted mask and the mask that allows nothing
// are distinguished by Permissions, whose nil result is the unrestricted mask's
// persistence form.
type PermissionMask struct {
	allowed map[Permission]struct{}
}

// AllowAll returns the unrestricted mask: the zero value, spelled out.
func AllowAll() PermissionMask {
	return PermissionMask{}
}

// DenyAll returns the mask that allows nothing.
func DenyAll() PermissionMask {
	return PermissionMask{allowed: map[Permission]struct{}{}}
}

// MaskPermissions returns the mask allowing exactly perms. Duplicates and the
// NullPermission are ignored. With no permissions left it returns fallback, so
// the caller states what an empty list means where the list is built:
// AllowAll() for "no mask", DenyAll() for "nothing", or any narrower mask of
// its own. A list that may be empty cannot be spread into the function without
// that choice, which is the point.
func MaskPermissions(fallback PermissionMask, perms ...Permission) PermissionMask {
	allowed := make(map[Permission]struct{}, len(perms))
	for _, perm := range perms {
		if perm == NullPermission {
			continue
		}
		allowed[perm] = struct{}{}
	}
	if len(allowed) == 0 {
		return fallback
	}

	return PermissionMask{allowed: allowed}
}

// Allows reports whether perm passes the mask. The unrestricted mask allows
// every permission.
func (m PermissionMask) Allows(perm Permission) bool {
	if m.allowed == nil {
		return true
	}
	_, ok := m.allowed[perm]

	return ok
}

// IsZero reports whether the mask is unrestricted.
func (m PermissionMask) IsZero() bool {
	return m.allowed == nil
}

// Permissions returns the allowlist sorted, or nil for the unrestricted mask.
// A non-nil empty result is the mask that allows nothing. This is the mask's
// persistence form: MaskPermissions(m.Permissions()...) rebuilds any
// restricted mask.
func (m PermissionMask) Permissions() []Permission {
	if m.allowed == nil {
		return nil
	}

	perms := make([]Permission, 0, len(m.allowed))
	for perm := range m.allowed {
		perms = append(perms, perm)
	}
	slices.Sort(perms)

	return perms
}

// String renders the mask for display only: "unrestricted" for the zero mask,
// otherwise the sorted allowlist joined with commas (empty for the mask that
// allows nothing).
func (m PermissionMask) String() string {
	if m.allowed == nil {
		return "unrestricted"
	}

	perms := m.Permissions()
	names := make([]string, len(perms))
	for i, perm := range perms {
		names[i] = string(perm)
	}

	return strings.Join(names, ",")
}
