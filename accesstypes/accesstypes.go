// package accesstypes provides types for permissions, roles, and domains types for the access package
package accesstypes

import "context"

type Enforcer interface {
	RequireResources(ctx context.Context, user User, domain Domain, perms Permission, resources ...Resource) (ok bool, missing []Resource, err error)
}
