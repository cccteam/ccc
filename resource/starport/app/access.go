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
// handlers). Test suites script permissions by supplying a fake access.Controller
// through the Configurer's Access seam.
func NewAccessUserPermissions(controller access.Controller) func(*http.Request) resource.UserPermissions {
	return func(r *http.Request) resource.UserPermissions {
		return &accessUserPermissions{
			controller: controller,
			user:       accesstypes.User(sessioninfo.FromRequest(r).Username),
		}
	}
}

var _ resource.UserPermissions = &accessUserPermissions{}

// accessUserPermissions binds one request's user to the access engine. The scope is
// bound per check by the caller (the generated handlers), per the UserPermissions
// contract.
type accessUserPermissions struct {
	controller access.Controller
	user       accesstypes.User
}

// Check implements resource.UserPermissions over access.Controller.CheckUserResources.
// The Environment is empty until request-decode sampling lands; with no conditions in
// the engine every Decision is Granted or Denied, so the denied set is exactly the
// missing set the UserPermissions contract requires.
func (u *accessUserPermissions) Check(ctx context.Context, scope accesstypes.Scope, perm accesstypes.Permission, resources ...accesstypes.Resource) ([]accesstypes.Resource, error) {
	decisions, err := u.controller.CheckUserResources(ctx, accesstypes.NewEnvironment(), u.user, scope, perm, resources...)
	if err != nil {
		return nil, errors.Wrap(err, "access.Controller.CheckUserResources()")
	}

	return decisions.DeniedResources(), nil
}

// User implements resource.UserPermissions.
func (u *accessUserPermissions) User() accesstypes.User {
	return u.user
}
