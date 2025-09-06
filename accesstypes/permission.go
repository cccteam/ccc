package accesstypes

import (
	"fmt"
	"strings"
)

const permissionPrefix = "perm:"

// Permission represents a permission in the authorization system
type Permission string

const (
	// NullPermission represents no permission
	NullPermission Permission = ""

	// Create is the create permission in CRUD
	Create Permission = "Create"

	// Read is the read permission in CRUD used to fetch single resource
	Read Permission = "Read"

	// List is the list permission used to fetch multiple resources
	List Permission = "List"

	// Update is the update permission in CRUD
	Update Permission = "Update"

	// Delete is the delete permission in CRUD
	Delete Permission = "Delete"

	// Execute is the execute permission used in RPC
	Execute Permission = "Execute"
)

type (
	// Tag represents the string name of a json tag
	Tag string

	// Field represents the string name of a struct field
	Field string

	// TagPermissions is a map of Tags to a slice of permissions associated with the Tag
	TagPermissions map[Tag][]Permission

	// PermissionScope is the type use to define different scopes
	PermissionScope string

	// ResolvedTagPermissions is a mapping for each domain and resource of which permissions are required for a Tag
	ResolvedTagPermissions map[Domain]map[Resource]map[Tag]map[Permission]bool

	// ResolvedResourcePermissions is a mapping for each domain and resource of which permissions are rquired for the resource
	ResolvedResourcePermissions map[Domain]map[Resource]map[Permission]bool
)

// ResolvedPermissions is a struct that holds the resolved permissions for resources and tags.
type ResolvedPermissions struct {
	Resources ResolvedResourcePermissions
	Tags      ResolvedTagPermissions
}

const (
	// GlobalPermissionScope is the permission scope for global permissions
	GlobalPermissionScope PermissionScope = "global"

	// DomainPermissionScope is the permission scope for domain permissions
	DomainPermissionScope PermissionScope = "domain"
)

// PermissionDetail is a struct that holds the description and scope of a permission
type PermissionDetail struct {
	Description string
	Scope       PermissionScope
}

// UnmarshalPermission unmarshals a permission string into a Permission type.
func UnmarshalPermission(permission string) Permission {
	p := Permission(strings.TrimPrefix(permission, permissionPrefix))
	if !p.isValid() {
		panic(fmt.Sprintf("invalid permission %q", permission))
	}

	return p
}

// Marshal marshals a Permission type into a string.
func (p Permission) Marshal() string {
	if !p.isValid() {
		panic(fmt.Sprintf("invalid permission %q, type can not contain prefix", string(p)))
	}

	return permissionPrefix + string(p)
}

func (p Permission) isValid() bool {
	return !strings.HasPrefix(string(p), permissionPrefix)
}
