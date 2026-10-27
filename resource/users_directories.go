package resource

import (
	"context"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/logger"
	"github.com/cccteam/session/signin"
	"github.com/go-playground/errors/v5"
)

// SeatDeclared returns the application's half of a directory's default seat
// (session.Auth.SetDirectoryAdmission): the hook that seats the user a first sign-in
// provisioned with the default seats of the directories the tenants declared for the
// identity's connection, each a role in a tenant, through the auth's seats, once the
// resolution that created the user has committed and before the sign-in policy decides,
// so the policy finds the memberships. An application passes its engine's user manager,
// as it does to SeatInvited; the hook runs outside any transaction for the same reason.
func SeatDeclared(seats UserSeats) func(ctx context.Context, userID ccc.UUID, username string, declared []signin.DirectorySeat) error {
	return func(ctx context.Context, _ ccc.UUID, username string, declared []signin.DirectorySeat) error {
		for _, seat := range declared {
			if err := seats.AddUserRoles(ctx, accesstypes.DomainPolicyScope(seat.Tenant), userKey(username), seat.Role); err != nil {
				return errors.Wrapf(err, "resource.UserSeats.AddUserRoles(): seating %q in %s as %s", username, seat.Tenant, seat.Role)
			}
			logger.FromCtx(ctx).Infof("user surface: %q entered %s through its directory and holds %s", username, seat.Tenant, seat.Role)
		}

		return nil
	}
}
