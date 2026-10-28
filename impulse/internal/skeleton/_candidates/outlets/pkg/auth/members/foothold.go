package members

import (
	"context"

	cloudspanner "cloud.google.com/go/spanner"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/go-playground/errors/v5"
)

// AdministratorRoles are the roles the first administrator holds, by where they are held:
// what the development identities hold (cmd/bootstrap/users.json), a domain role in every
// tenant domain, so the foothold can do what the development logins can.
func AdministratorRoles() (global, everyDomain []accesstypes.Role) {
	return nil, []accesstypes.Role{"TenantAdministrator"}
}

// SeedAdministrator writes the administrator role memberships under signInName when the
// auth's users table holds no rows and reports whether it did: the foothold a deployed
// environment starts from. The directory proves who someone is and the application assigns
// the roles (signin.DisableRoleSync), so until a membership exists the first person in holds
// nothing; the memberships wait for them under the sign-in name the directory presents (the
// user principal name on Entra, the email on Google), which the placement names per
// environment. The directory provisions the person at their first sign-in, which finds the
// roles. Writing a membership that exists is a no-op, so a deploy before anyone has signed
// in writes them again harmlessly, and once a user exists nothing is seeded: every user
// after the first comes from the administration surface. An empty name is refused while the
// table is empty. The development bootstrap seeds the development identities instead.
func (a *Auth) SeedAdministrator(ctx context.Context, signInName string) (bool, error) {
	empty, err := a.usersTableEmpty(ctx)
	if err != nil {
		return false, err
	}
	if !empty {
		return false, nil
	}
	if signInName == "" {
		return false, errors.Newf("the %s auth's users table holds no rows and no first administrator is named: a deployment names one per environment, and the migrate command seeds nothing without it", Name)
	}
	if err := a.assignAdministrator(ctx, signInName); err != nil {
		return false, err
	}

	return true, nil
}

// assignAdministrator writes the first administrator's memberships under username, each
// where its roles are held; a membership that exists is left as it is.
func (a *Auth) assignAdministrator(ctx context.Context, username string) error {
	global, everyDomain := AdministratorRoles()
	manager := a.access.UserManager()
	if len(global) > 0 {
		if err := manager.AddUserRoles(ctx, accesstypes.GlobalPolicyScope(), accesstypes.User(username), global...); err != nil {
			return errors.Wrapf(err, "access.UserManager.AddUserRoles(): %s in the global partition", username)
		}
	}
	if len(everyDomain) > 0 {
		if err := manager.AddUserRoles(ctx, accesstypes.EveryDomainPolicyScope(), accesstypes.User(username), everyDomain...); err != nil {
			return errors.Wrapf(err, "access.UserManager.AddUserRoles(): %s in every domain", username)
		}
	}

	return nil
}

// usersTableEmpty reads one row of the users table, any row, and reports there is none.
func (a *Auth) usersTableEmpty(ctx context.Context) (bool, error) {
	rows := 0
	iter := a.db.Single().ReadWithOptions(ctx, usersTable, cloudspanner.AllKeys(), []string{"Id"}, &cloudspanner.ReadOptions{Limit: 1})
	if err := iter.Do(func(*cloudspanner.Row) error {
		rows++

		return nil
	}); err != nil {
		return false, errors.Wrapf(err, "spanner.RowIterator.Do(): %s", usersTable)
	}

	return rows == 0, nil
}
