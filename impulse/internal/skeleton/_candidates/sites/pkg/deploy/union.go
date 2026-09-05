package deploy

import (
	"slices"

	"github.com/cccteam/access"
	"github.com/cccteam/ccc/accesstypes"
)

// unionCollection presents several generated collections as one permission registry.
// List merges the registries; every question about a resource is answered by the first
// collection that registers it, which is exact when the sites declare a shared table
// identically (the generator derives everything from the same schema and struct).
type unionCollection []access.PermissionCollection

var _ access.PermissionCollection = unionCollection{}

func (u unionCollection) List() map[accesstypes.Permission][]accesstypes.Resource {
	merged := make(map[accesstypes.Permission][]accesstypes.Resource)
	for _, c := range u {
		for perm, resources := range c.List() {
			for _, res := range resources {
				if !slices.Contains(merged[perm], res) {
					merged[perm] = append(merged[perm], res)
				}
			}
		}
	}
	for perm := range merged {
		slices.Sort(merged[perm])
	}

	return merged
}

// owner returns the first collection registering res under any permission, or the first
// collection when none does (its answer for an unknown resource is as good as any).
func (u unionCollection) owner(res accesstypes.Resource) access.PermissionCollection {
	for _, c := range u {
		for _, resources := range c.List() {
			if slices.Contains(resources, res) {
				return c
			}
		}
	}

	return u[0]
}

func (u unionCollection) Scope(res accesstypes.Resource) accesstypes.PermissionScope {
	return u.owner(res).Scope(res)
}

func (u unionCollection) IsResourceImmutable(scope accesstypes.PermissionScope, res accesstypes.Resource) bool {
	return u.owner(res).IsResourceImmutable(scope, res)
}

func (u unionCollection) AttributeComparisonType(scope accesstypes.PermissionScope, res accesstypes.Resource, name string) (accesstypes.AttributeType, bool) {
	return u.owner(res).AttributeComparisonType(scope, res, name)
}

func (u unionCollection) AttributeIsColumn(scope accesstypes.PermissionScope, res accesstypes.Resource, name string) bool {
	return u.owner(res).AttributeIsColumn(scope, res, name)
}

func (u unionCollection) DeclaresSubjectSet(name string) bool {
	for _, c := range u {
		if c.DeclaresSubjectSet(name) {
			return true
		}
	}

	return false
}

func (u unionCollection) DeclaresSubjectValue(name string) bool {
	for _, c := range u {
		if c.DeclaresSubjectValue(name) {
			return true
		}
	}

	return false
}

func (u unionCollection) IsComputedResource(scope accesstypes.PermissionScope, res accesstypes.Resource) bool {
	return u.owner(res).IsComputedResource(scope, res)
}

func (u unionCollection) MethodTarget(scope accesstypes.PermissionScope, method accesstypes.Resource) (accesstypes.Resource, bool) {
	return u.owner(method).MethodTarget(scope, method)
}
