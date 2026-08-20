package app

import (
	"context"
	"net/http"

	"github.com/cccteam/access"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/session/sessioninfo"
	"github.com/go-playground/errors/v5"
)

// NewAccessUserPermissions adapts the access engine to the resource package's
// UserPermissions seam, resolving the user from the request's session (established by
// session middleware; the request panics without one, like the generated mutation
// handlers). It is the production counterpart of the scriptable fakes the integration
// tests inject via Config.UserPermissions.
func NewAccessUserPermissions(controller access.Controller) func(*http.Request) resource.UserPermissions {
	return func(r *http.Request) resource.UserPermissions {
		return &accessUserPermissions{
			controller: controller,
			user:       accesstypes.User(sessioninfo.FromRequest(r).Username),
		}
	}
}

var _ resource.UserPermissions = &accessUserPermissions{}

// accessUserPermissions binds one request's user to the access engine. The domain is
// bound per check by the caller (the generated handlers), per the UserPermissions
// contract.
type accessUserPermissions struct {
	controller access.Controller
	user       accesstypes.User
}

// Check implements resource.UserPermissions over access.Controller.RequireResources,
// which returns the exhaustive missing set the contract requires.
func (u *accessUserPermissions) Check(ctx context.Context, domain accesstypes.Domain, perm accesstypes.Permission, resources ...accesstypes.Resource) ([]accesstypes.Resource, error) {
	_, missing, err := u.controller.RequireResources(ctx, u.user, domain, perm, resources...)
	if err != nil {
		return nil, errors.Wrap(err, "access.Controller.RequireResources()")
	}

	return missing, nil
}

// User implements resource.UserPermissions.
func (u *accessUserPermissions) User() accesstypes.User {
	return u.user
}
