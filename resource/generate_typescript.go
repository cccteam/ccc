package resource

import (
	"github.com/cccteam/ccc/accesstypes"
)

type TypescriptData struct {
	Permissions         []accesstypes.Permission
	Resources           []accesstypes.Resource
	ResourceTags        map[accesstypes.Resource][]accesstypes.Tag
	ResourcePermissions permissionMap
	Domains             []accesstypes.PermissionScope
}

func (c *Collection) TypescriptData() TypescriptData {
	return TypescriptData{
		Permissions:         c.permissions(),
		Resources:           c.Resources(),
		ResourceTags:        c.tags(),
		ResourcePermissions: c.resourcePermissions(),
		Domains:             c.domains(),
	}
}
