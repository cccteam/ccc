package resource

import (
	"context"
	"time"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/go-playground/errors/v5"
)

// The self-service creation of a tenant (@tenant(creation: self-service) or both): a
// signed-in person who holds no tenant creates one through the generated
// create-your-tenant route and becomes its first administrator. The generated route
// writes the row in its own transaction and then seats the person; this is the seating.

// seatVisibleTimeout bounds the wait for the permission engine to answer a membership
// it was just handed: the engine's snapshot swaps asynchronously after a write, and the
// route answers only once the person's next request would see the tenant.
const seatVisibleTimeout = 10 * time.Second

// seatVisiblePoll is how often the engine is asked during the wait.
const seatVisiblePoll = 25 * time.Millisecond

// SeatFirstAdministrator seats the session's user as the first administrator of the
// tenant its row was just committed for: the roles are written in the tenant's domain
// scope through the auth's seats (the engine's user manager, which signals the policy
// change to every instance), and the call returns once the engine answers the
// person a foothold in the tenant, bounded by seatVisibleTimeout, so the answer the
// route gives is one the person's next request stands on. The generated route calls
// it after the commit and removes the row when it fails: the permission store writes
// memberships outside the row's transaction, so this is the one write that cannot
// ride it.
func SeatFirstAdministrator(ctx context.Context, seats UserSeats, perms UserPermissions, domain accesstypes.Domain, roles []accesstypes.Role) error {
	if len(roles) == 0 {
		return errors.Newf("seating the first administrator of %s: no role to seat", domain)
	}
	user := perms.User()
	if user == "" {
		return errors.Newf("seating the first administrator of %s: the session names no user", domain)
	}
	if err := seats.AddUserRoles(ctx, accesstypes.DomainPolicyScope(domain), user, roles...); err != nil {
		return errors.Wrapf(err, "resource.UserSeats.AddUserRoles(): seating %s in %s", user, domain)
	}

	deadline := time.Now().Add(seatVisibleTimeout)
	for {
		has, err := perms.HasGrants(ctx, accesstypes.DomainScope(domain))
		if err != nil {
			return errors.Wrap(err, "resource.UserPermissions.HasGrants()")
		}
		if has {
			return nil
		}
		if time.Now().After(deadline) {
			return errors.Newf("seating the first administrator of %s: the membership was written but the permission engine does not answer it after %s", domain, seatVisibleTimeout)
		}
		select {
		case <-ctx.Done():
			return errors.Wrap(ctx.Err(), "seating the first administrator")
		case <-time.After(seatVisiblePoll):
		}
	}
}
