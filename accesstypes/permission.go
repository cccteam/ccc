package accesstypes

import (
	"fmt"
	"strings"
)

const permissionPrefix = "perm:"

type Permission string

const (
	NullPermission Permission = ""
	Create         Permission = "Create"
	Read           Permission = "Read"
	List           Permission = "List"
	Update         Permission = "Update"
	Delete         Permission = "Delete"
)

type (
	Tag                         string
	Field                       string
	TagPermission               map[Tag]Permission
	PermissionScope             string
	ResolvedTagPermissions      map[Domain]map[Resource]map[Tag]map[Permission]bool
	ResolvedResourcePermissions map[Domain]map[Resource]map[Permission]bool
)

type ResolvedPermissions struct {
	Resources ResolvedResourcePermissions
	Tags      ResolvedTagPermissions
}

const (
	GlobalPermissionScope PermissionScope = "global"
	DomainPermissionScope PermissionScope = "domain"
)

type PermissionDetail struct {
	Description string
	Scope       PermissionScope
}

func UnmarshalPermission(permission string) Permission {
	p := Permission(strings.TrimPrefix(permission, permissionPrefix))
	if !p.isValid() {
		panic(fmt.Sprintf("invalid permission %q", permission))
	}

	return p
}

func (p Permission) Marshal() string {
	if !p.isValid() {
		panic(fmt.Sprintf("invalid permission %q, type can not contain prefix", string(p)))
	}

	return permissionPrefix + string(p)
}

func (p Permission) isValid() bool {
	return !strings.HasPrefix(string(p), permissionPrefix)
}
