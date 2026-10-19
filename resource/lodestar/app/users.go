package app

import (
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/lodestar/pkg/router"
)

// The user-management surfaces. The generator serves each auth's users, seats and roles
// as resources (CrewUsers, CrewSectorUsers, MembersUsers and their seats and roles) and
// the targeted methods beside them, their handlers generated into zz_gen_users.go as
// delegations to the surface the configuration builds for each auth: the crew's over
// the crew session's accounts and the crew engine's memberships, the members' over the
// members'. The console serves both (the generator program's Manages), so headquarters
// administers both populations from one place; the portal serves the members' alone.
// What a role may do is the crew role file's to say, in the same grammar as every other
// resource: SectorMarshal seats and unseats crew in its sector in every role but its own
// (new.role != 'SectorMarshal'), Headquarters holds both populations in the global
// partition. A crew member who signs in through the Google directory has their seats
// written by the directory at every sign-in (crew.go's RoleSync, declared SyncsRoles on
// the registration), so the surface refuses to seat or unseat them by hand and says so
// on the row (rolesSynced).
//
// Demonstrates: auth.user-management.

// CrewUserSurface is the crew auth's surface, which the generated crew handlers call.
func (a *App) CrewUserSurface() *resource.UserSurface[router.CrewAuth] {
	return a.crewUsers
}

// MembersUserSurface is the members auth's surface, which the generated members handlers
// call on the console and the portal alike.
func (a *App) MembersUserSurface() *resource.UserSurface[router.MembersAuth] {
	return a.membersUsers
}
