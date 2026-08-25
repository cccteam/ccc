package accesstypes

type (
	// RoleCollection maps each scope to the roles defined in it.
	RoleCollection map[Scope][]Role

	// PermissionGrants describes how a role holds one permission within a
	// scope: scope-wide (attached to no resource), on specific resources, or
	// both.
	PermissionGrants struct {
		ScopeWide bool
		Resources []Resource
	}

	// RolePermissionCollection maps each permission a role holds within one
	// scope to how it is granted.
	RolePermissionCollection map[Permission]PermissionGrants

	// UserScopePermissions describes what a user holds within one scope: the
	// permissions held scope-wide and the permissions held per resource.
	UserScopePermissions struct {
		ScopeWide []Permission
		Resources map[Resource][]Permission
	}

	// UserPermissionCollection maps each scope to the permissions a user
	// holds in it.
	UserPermissionCollection map[Scope]UserScopePermissions
)
