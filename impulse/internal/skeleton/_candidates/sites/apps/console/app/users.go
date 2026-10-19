package app

import (
	"github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/sites/apps/console/pkg/router"
	"github.com/cccteam/ccc/resource"
)

// The user-management surface. The generator serves the staff auth's users, seats and
// roles as resources, with the targeted methods beside them, and generates their handlers
// into zz_gen_users.go as delegations to this surface, which New builds with the generated
// constructor over the stores the configuration hands it: the staff session's accounts and
// the staff engine's seats. What a role may do with them is the staff role file's to say,
// in the same grammar as every other resource.

// StaffUserSurface is the staff auth's surface, which the generated staff handlers call.
func (a *App) StaffUserSurface() *resource.UserSurface[router.StaffAuth] {
	return a.staffUsers
}
