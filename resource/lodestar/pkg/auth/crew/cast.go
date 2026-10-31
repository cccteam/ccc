package crew

import (
	"context"
	_ "embed" // the cast rides in the binary

	"github.com/cccteam/access"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/impulse/persona"
	"github.com/cccteam/httpio"
	"github.com/cccteam/session"
	"github.com/go-playground/errors/v5"
)

// castFile is the cast of the persona sign-in: the personas the test environments offer,
// declared beside this package in cast.json (login, name, address and roles, each role
// one the role file defines). The pipeline registers the cast with the persona provider
// after traffic (bedrock deploy persona register), and the application seeds its
// accounts at start where the method is registered (SeedCast). The logins are on no
// manifest: hq holds Headquarters, the fleet's global administration; warden holds
// SectorMarshal in every sector; rookie holds Cadet in every sector.
//
// Demonstrates: auth.persona.
//
//go:embed cast.json
var castFile []byte

// Cast reads the cast file.
func Cast() (*persona.Cast, error) {
	cast, err := persona.ReadCast(castFile)
	if err != nil {
		return nil, errors.Wrap(err, "persona.ReadCast(): cast.json")
	}

	return cast, nil
}

// SeedCast seeds the cast: a password-less crew login under each persona's login, with
// the persona's address, seated in the roles the cast names, a global role in the global
// partition and a domain role in every sector, as the administrator roles are. A persona
// whose login exists is left as it is, so every start seeds the cast again harmlessly and
// a persona added to the cast arrives at the next one. The memberships are written first,
// since writing one that exists is a no-op, then the login: a run that fails between the
// two leaves no login, so the next start finishes the job; a login another instance made
// meanwhile is found and left. The persona sign-in links the provider's identity to the
// login of the same name at the first sign-in (invited users only), so a persona the seed
// never made is refused there. It answers how many logins it created.
func (a *Auth) SeedCast(ctx context.Context) (int, error) {
	cast, err := Cast()
	if err != nil {
		return 0, err
	}
	roles, err := Roles().Parse()
	if err != nil {
		return 0, errors.Wrap(err, "access.RoleFile.Parse()")
	}
	api := a.handlers.API()
	created := 0
	for i := range cast.Personas {
		p := &cast.Personas[i]
		if _, err := api.UserByUsername(ctx, p.Login); err == nil {
			continue
		} else if !httpio.HasNotFound(err) {
			return created, errors.Wrapf(err, "session.AuthAPI.UserByUsername(): %s", p.Login)
		}
		if err := a.seatPersona(ctx, roles, p); err != nil {
			return created, err
		}
		req := &session.CreateUserRequest{Username: p.Login}
		if p.Email != "" {
			req.Email = &p.Email
		}
		if _, err := api.CreateSessionUser(ctx, req); err != nil {
			if _, found := api.UserByUsername(ctx, p.Login); found == nil {
				continue
			}

			return created, errors.Wrapf(err, "session.AuthAPI.CreateSessionUser(): %s", p.Login)
		}
		created++
	}

	return created, nil
}

// seatPersona writes a persona's memberships, each role where the role file holds it: a
// global role in the global partition, a domain role in every sector. A role the file
// does not define is refused, since the seed names nothing the release does not.
func (a *Auth) seatPersona(ctx context.Context, roles *access.RoleConfig, p *persona.Persona) error {
	var global, everyDomain []accesstypes.Role
	for _, name := range p.Roles {
		role := accesstypes.Role(name)
		switch {
		case definesRole(roles.Roles.Global, role):
			global = append(global, role)
		case definesRole(roles.Roles.Domain, role):
			everyDomain = append(everyDomain, role)
		default:
			return errors.Newf("cast.json: the persona %s holds the role %q, which the %s auth's role file does not define", p.Login, name, Name)
		}
	}
	manager := a.access.UserManager()
	if len(global) > 0 {
		if err := manager.AddUserRoles(ctx, accesstypes.GlobalPolicyScope(), accesstypes.User(p.Login), global...); err != nil {
			return errors.Wrapf(err, "access.UserManager.AddUserRoles(): %s in the global partition", p.Login)
		}
	}
	if len(everyDomain) > 0 {
		if err := manager.AddUserRoles(ctx, accesstypes.EveryDomainPolicyScope(), accesstypes.User(p.Login), everyDomain...); err != nil {
			return errors.Wrapf(err, "access.UserManager.AddUserRoles(): %s in every sector", p.Login)
		}
	}

	return nil
}

// definesRole reports whether the role file defines the role at the scope.
func definesRole(defined []*access.Role, role accesstypes.Role) bool {
	for _, r := range defined {
		if r.Name == role {
			return true
		}
	}

	return false
}
