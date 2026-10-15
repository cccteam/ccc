package crew

import (
	"context"
	"strings"

	"github.com/cccteam/access"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/go-playground/errors/v5"
)

// directoryRoles is the role writer the crew's Google method reconciles roles through.
//
// A Google group is an email address, which the directory reads in lowercase, so the
// role names the sync derives from the groups are lowercase (crew-pilot@example.com
// names pilot, and the simulated directory lowercases APP_GOOGLE_ROLES the same way),
// while the crew's roles are named as the role file spells them (Pilot). The writer
// stands between: it presents the store to the sync in the sync's lowercase, so the
// roles a person holds and the roles the directory names compare as the same names,
// and spells each name the role file's way before it asks or writes the store. A
// population whose roles are lowercase, as the members' are, needs none of this.
//
// Demonstrates: auth.multi-method.
type directoryRoles struct {
	manager access.UserManager
	// spelling maps a role name in lowercase to the role file's spelling of it.
	spelling map[string]accesstypes.Role
}

// newDirectoryRoles reads the role file's names, so a name the sync derives that the file
// does not spell is passed through as it came and found not to exist, as any unknown
// name is.
func newDirectoryRoles(manager access.UserManager, file access.RoleFile) (*directoryRoles, error) {
	config, err := file.Parse()
	if err != nil {
		return nil, errors.Wrap(err, "access.RoleFile.Parse()")
	}
	spelling := make(map[string]accesstypes.Role)
	for _, role := range config.Roles.Global {
		spelling[strings.ToLower(string(role.Name))] = role.Name
	}
	for _, role := range config.Roles.Domain {
		spelling[strings.ToLower(string(role.Name))] = role.Name
	}

	return &directoryRoles{manager: manager, spelling: spelling}, nil
}

// UserRoles lists the roles the user holds, in the sync's lowercase.
func (d *directoryRoles) UserRoles(ctx context.Context, user accesstypes.User, scopes ...accesstypes.PolicyScope) (accesstypes.RoleCollection, error) {
	held, err := d.manager.UserRoles(ctx, user, scopes...)
	if err != nil {
		return nil, errors.Wrap(err, "access.UserManager.UserRoles()")
	}
	lowered := make(accesstypes.RoleCollection, len(held))
	for scope, roles := range held {
		names := make([]accesstypes.Role, 0, len(roles))
		for _, role := range roles {
			names = append(names, accesstypes.Role(strings.ToLower(string(role))))
		}
		lowered[scope] = names
	}

	return lowered, nil
}

// RoleExists asks the store under the role file's spelling.
func (d *directoryRoles) RoleExists(ctx context.Context, scope accesstypes.PolicyScope, role accesstypes.Role) (bool, error) {
	exists, err := d.manager.RoleExists(ctx, scope, d.spell(role))
	if err != nil {
		return false, errors.Wrap(err, "access.UserManager.RoleExists()")
	}

	return exists, nil
}

// AddUserRoles writes the memberships under the role file's spelling.
func (d *directoryRoles) AddUserRoles(ctx context.Context, scope accesstypes.PolicyScope, user accesstypes.User, roles ...accesstypes.Role) error {
	if err := d.manager.AddUserRoles(ctx, scope, user, d.spellAll(roles)...); err != nil {
		return errors.Wrap(err, "access.UserManager.AddUserRoles()")
	}

	return nil
}

// DeleteUserRoles removes the memberships under the role file's spelling.
func (d *directoryRoles) DeleteUserRoles(ctx context.Context, scope accesstypes.PolicyScope, user accesstypes.User, roles ...accesstypes.Role) error {
	if err := d.manager.DeleteUserRoles(ctx, scope, user, d.spellAll(roles)...); err != nil {
		return errors.Wrap(err, "access.UserManager.DeleteUserRoles()")
	}

	return nil
}

// spell returns the role file's spelling of a lowercase name, or the name as it came
// when the file does not spell it.
func (d *directoryRoles) spell(role accesstypes.Role) accesstypes.Role {
	if spelled, ok := d.spelling[strings.ToLower(string(role))]; ok {
		return spelled
	}

	return role
}

func (d *directoryRoles) spellAll(roles []accesstypes.Role) []accesstypes.Role {
	spelled := make([]accesstypes.Role, 0, len(roles))
	for _, role := range roles {
		spelled = append(spelled, d.spell(role))
	}

	return spelled
}
