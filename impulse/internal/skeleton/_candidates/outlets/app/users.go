package app

import (
	"github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/outlets/pkg/router"
	"github.com/cccteam/ccc/resource"
)

// The user-management surfaces. The generator serves each auth's users, seats and roles
// as resources, with the targeted methods beside them, and generates their handlers into
// zz_gen_users.go as delegations to the surface New builds for each auth with the
// generated constructor, over the stores the configuration hands it: the staff's over the
// staff session's accounts and the staff engine's seats, served on the console; the
// members' over the members', served on the portal. What a role may do with them is each
// auth's role file's to say, in the same grammar as every other resource.

// StaffUserSurface is the staff auth's surface, which the generated staff handlers call.
func (a *App) StaffUserSurface() *resource.UserSurface[router.StaffAuth] {
	return a.staffUsers
}

// MembersUserSurface is the members auth's surface, which the generated members handlers
// call on the portal.
func (a *App) MembersUserSurface() *resource.UserSurface[router.MembersAuth] {
	return a.membersUsers
}
