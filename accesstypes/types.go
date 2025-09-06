package accesstypes

type (
	// RoleCollection is a map of domain to roles defind in that domain
	RoleCollection map[Domain][]Role

	// RolePermissionCollection is a map of permissions a Role has on resources
	RolePermissionCollection map[Permission][]Resource

	// UserPermissionCollection is a mapping of permissions a user has for domain and resource
	UserPermissionCollection map[Domain]map[Resource][]Permission
)
