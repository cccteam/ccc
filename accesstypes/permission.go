package accesstypes

import (
	"fmt"
	"strings"
)

const permissionPrefix = "perm:"

type Permission string

const NullPermission Permission = ""

type (
	Field                       string
	FieldPermission             map[Field]Permission
	PermissionScope             string
	ResolvedFieldPermissions    map[Domain]map[Resource]map[Field]map[Permission]bool
	ResolvedResourcePermissions map[Domain]map[Resource]map[Permission]bool
)

type ResolvedPermissions struct {
	Resources ResolvedResourcePermissions
	Fields    ResolvedFieldPermissions
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
