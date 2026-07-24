//go:build collect_resource_permissions

// This file exists solely for the V1 migration and is deleted in V2 along with the rest
// of the collect_resource_permissions build.
//
// Importing github.com/cccteam/access pins resource's go.mod to the first access release
// whose MigrateRoles accepts a PermissionCollection, so consumers upgrading resource
// automatically lift access past the versions that only accept *resource.Collection.
// The assertions double as a compile-time check that both collection implementations
// satisfy the contract access consumes; access carries its own long-lived copy of that
// check, so nothing is lost when this file is removed.
package resource_test

import (
	"github.com/cccteam/access"
	"github.com/cccteam/ccc/resource"
)

var (
	_ access.PermissionCollection = (*resource.Collection)(nil)
	_ access.PermissionCollection = (*resource.GeneratedCollection)(nil)
)
