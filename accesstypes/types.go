package accesstypes

type (
	RoleCollection           map[Domain][]Role
	RolePermissionCollection map[Permission][]Resource
	UserPermissionCollection map[Domain]map[Resource][]Permission
)
