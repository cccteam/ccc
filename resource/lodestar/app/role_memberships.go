package app

import (
	"net/http"
	"time"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource/lodestar/pkg/resources"
	"github.com/cccteam/ccc/resource/lodestar/pkg/router"
	"github.com/cccteam/httpio"
	"github.com/go-chi/chi/v5"
	"github.com/go-playground/errors/v5"
)

// accessDomainParam is the route parameter the access library's user-management
// handlers read the tenant domain from. The console's sector routes carry the sector as
// router.Domain, the parameter every sector route shares and the domain guard reads, so
// the membership routes copy it across before delegating.
const accessDomainParam = "domain"

// RoleUsers lists the users holding the role in the sector: List on RoleMemberships.
//
// The console's role-membership routes (this one, AddRoleUsers and DeleteRoleUsers) seat
// and unseat crew in a sector and list who holds a role there. They are the access
// library's user-management handlers (access.Handlers) mounted behind the App's own
// permission checks, since the handlers check none: List, Create and Delete on
// RoleMemberships in the URL's sector, declared to the generator through
// @manualAddResource on resources.RoleMemberships and held by SectorMarshal. The crew's
// roles are the application's (a password auth: application authority), so seating crew
// is the application's to serve, where a directory auth's memberships are the directory's
// and no route writes them. A membership written here is a policy write: the crew engine
// that wrote it announces the policy kind through the live service, and every other
// instance's engine rereads at the signal, so the seated crew member is served everywhere
// at the next request, with no restart and without waiting for the heartbeat.
//
// Demonstrates: auth.user-management, live.signals, @manualAddResource.scope.
func (a *App) RoleUsers() http.HandlerFunc {
	return a.roleMembership(accesstypes.List, a.management.RoleUsers())
}

// AddRoleUsers seats the body's users in the role in the sector: Create on
// RoleMemberships.
func (a *App) AddRoleUsers() http.HandlerFunc {
	return a.roleMembership(accesstypes.Create, a.management.AddRoleUsers())
}

// DeleteRoleUsers unseats the body's users from the role in the sector: Delete on
// RoleMemberships.
func (a *App) DeleteRoleUsers() http.HandlerFunc {
	return a.roleMembership(accesstypes.Delete, a.management.DeleteRoleUsers())
}

// roleMembership guards one of the library's handlers with the permission on
// RoleMemberships in the URL's sector, Granted alone (a hand-written surface has no
// bindings for a condition to reference), and hands the sector to the handler under the
// parameter it reads.
func (a *App) roleMembership(perm accesstypes.Permission, next http.HandlerFunc) http.HandlerFunc {
	return httpio.Log(func(w http.ResponseWriter, r *http.Request) error {
		ctx := r.Context()
		domain := httpio.Param[accesstypes.Domain](r, router.Domain)

		env := accesstypes.NewEnvironment().WithNow(time.Now())
		decisions, err := a.UserPermissions(r).Check(ctx, env, accesstypes.DomainScope(domain), perm, resources.RoleMemberships)
		if err != nil {
			return httpio.NewEncoder(w).ClientMessage(ctx, errors.Wrap(err, "resource.UserPermissions.Check()"))
		}
		if !decisions[resources.RoleMemberships].IsGranted() {
			return httpio.NewEncoder(w).ClientMessage(ctx, httpio.NewForbiddenMessagef(
				"user %s does not have %s on %s in %s", a.UserPermissions(r).User(), perm, resources.RoleMemberships, domain,
			))
		}

		chi.RouteContext(ctx).URLParams.Add(accessDomainParam, string(domain))
		next.ServeHTTP(w, r)

		return nil
	})
}
