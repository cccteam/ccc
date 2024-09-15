package accesstypes

import (
	"fmt"
	"strings"
)

const permissionPrefix = "perm:"

type Permission string

type PermissionScope string

const (
	GlobalPermissionScope PermissionScope = "global"
	DomainPermissionScope PermissionScope = "domain"
)

type PermissionDetail struct {
	Description string
	Scope       PermissionScope
}

func UnmarshalPermission(permission string) Permission {
	return Permission(strings.TrimPrefix(permission, permissionPrefix))
}

func (p Permission) Marshal() string {
	if !p.IsValid() {
		panic(fmt.Sprintf("invalid permission %q, type can not contain prefix", string(p)))
	}

	return permissionPrefix + string(p)
}

func (p Permission) IsValid() bool {
	return !strings.HasPrefix(string(p), permissionPrefix)
}
