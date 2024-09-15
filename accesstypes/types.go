package accesstypes

import (
	"maps"
	"slices"
)

type RoleCollection map[Domain][]Role

type RolePermissionCollection map[Permission][]Resource

func (r RolePermissionCollection) Permissions() []Permission {
	perms := slices.Collect(maps.Keys(r))
	slices.Sort(perms)

	return perms
}

type UserPermissionCollection map[Domain]map[Permission][]Resource

func (p UserPermissionCollection) Domains() []Domain {
	return slices.Collect(maps.Keys(p))
}

func (p UserPermissionCollection) GlobalPermissions() map[Domain][]Permission {
	return p.DomainPermissions(slices.Collect(maps.Keys(p))...)
}

func (p UserPermissionCollection) DomainPermissions(domains ...Domain) map[Domain][]Permission {
	permissions := make(map[Domain][]Permission)
	for _, domain := range domains {
		perms := make([]Permission, 0, len(p[domain]))
		for perm, resource := range p[domain] {
			if slices.Contains(resource, GlobalResource) {
				perms = append(perms, perm)
			}
		}
		permissions[domain] = perms
	}

	return permissions
}
