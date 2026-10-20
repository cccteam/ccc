package staff

import (
	"context"

	cloudspanner "cloud.google.com/go/spanner"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/session"
	"github.com/go-playground/errors/v5"
)

// Administrator is the username of the auth's first administrator: the foothold a deployed
// environment starts from. The migrate command seeds it while the auth's users table holds
// no rows, with the password the stack generates for it (a generated secret the data level
// declares), and never again while a user exists; the first sign-in changes the password,
// and every user after the first comes from the administration surface. The development
// bootstrap creates its own logins instead (cmd/bootstrap/users.json).
const Administrator = "administrator"

// AdministratorRoles are the roles the first administrator holds, by where they are held:
// what the development identities hold (cmd/bootstrap/users.json), a domain role in every
// tenant domain, so the foothold can do what the development logins can.
func AdministratorRoles() (global, everyDomain []accesstypes.Role) {
	return []accesstypes.Role{"Administrator_Global", "FeatureAdministrator"}, []accesstypes.Role{"Administrator_Domain"}
}

// SeedAdministrator creates the first administrator when the auth's users table holds no
// rows and reports whether it did. The memberships are written first, since writing one
// that exists is a no-op, then the user: a run that fails between the two leaves the table
// empty, so the next deploy finishes the job. An empty password is refused while the table
// is empty: a deployment carries the generated one, and nothing seeds a user without a
// password.
func (a *Auth) SeedAdministrator(ctx context.Context, password string) (bool, error) {
	empty, err := a.usersTableEmpty(ctx)
	if err != nil {
		return false, err
	}
	if !empty {
		return false, nil
	}
	if password == "" {
		return false, errors.Newf("the %s auth's users table holds no rows and no administrator password was given: a deployment generates one as a secret, and the migrate command seeds nothing without it", Name)
	}
	if err := a.assignAdministrator(ctx, Administrator); err != nil {
		return false, err
	}
	if _, err := a.handlers.API().CreateSessionUser(ctx, &session.CreateUserRequest{Username: Administrator, Password: &password}); err != nil {
		return false, errors.Wrapf(err, "session.AuthAPI.CreateSessionUser(): %s", Administrator)
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
