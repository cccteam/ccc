package check

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path"
	"strings"

	"github.com/cccteam/access"
	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/impulse/app"
	"github.com/cccteam/ccc/impulse/persona"
)

// personaCast verifies that every Auth registering the persona method (oidc.SignIn) has
// the cast beside it that the method's sign-in links to and the deploy registers with
// the persona provider: pkg/auth/<name>/cast.json exists, parses the way the migrate
// command seeds it and the registration sends it (persona.ReadCast: at least one
// persona, every login lowercase letters, digits, dots, underscores and hyphens starting
// with a letter, no login twice), and every role a persona names is a role the auth's
// role file defines, since SeedCast seats each persona in its roles and a role the file
// does not define grants nothing. An Auth without the persona method holds no cast.
type personaCast struct{}

// personaCastName is the check's name.
const personaCastName = "persona-cast"

func (personaCast) Name() string {
	return personaCastName
}

func (personaCast) Describe() string {
	return "every Auth registering the persona method (oidc.SignIn) has its cast beside it (pkg/auth/<name>/cast.json): the file parses the way the migrate command seeds it and the deploy registers it (at least one persona, logins in the one grammar, no login twice), and every role a persona names is defined in the auth's role file"
}

func (c personaCast) Run(_ context.Context, env *Env) Result {
	a := env.App
	var details, casts []string
	for i := range a.Auths {
		auth := &a.Auths[i]
		if !registersPersona(auth) {
			continue
		}
		dir := path.Dir(auth.File)
		rel := path.Join(dir, persona.CastFile)
		data, err := os.ReadFile(a.Abs(rel))
		switch {
		case errors.Is(err, fs.ErrNotExist):
			details = append(details, fmt.Sprintf("%s does not exist; the %s registers the persona method, whose sign-in links to the personas the cast names and whose deploy registers them with the provider (impulse add method persona writes an empty cast to fill in)", rel, authLabel(auth)))

			continue
		case err != nil:
			details = append(details, fmt.Sprintf("%s: cannot be read: %v", rel, errors.Cause(err)))

			continue
		}
		cast, err := persona.ReadCast(data)
		if err != nil {
			details = append(details, fmt.Sprintf("%s: %s", rel, errors.Cause(err)))

			continue
		}
		details = append(details, undefinedRoles(a, dir, rel, cast)...)
		casts = append(casts, fmt.Sprintf("%s (%d persona(s))", rel, len(cast.Personas)))
	}
	if len(casts) == 0 && len(details) == 0 {
		return skip(c.Name(), "no Auth registers the persona method (oidc.SignIn)")
	}
	if len(details) > 0 {
		return fail(c.Name(), fmt.Sprintf("%d finding(s) in the persona cast(s)", len(details)), details...)
	}

	return pass(c.Name(), fmt.Sprintf("%d cast(s) name their personas and the roles seated: %s", len(casts), strings.Join(casts, ", ")))
}

// registersPersona reports an Auth with the persona method registered on it.
func registersPersona(auth *app.Auth) bool {
	for i := range auth.Methods {
		if auth.Methods[i].Kind == app.MethodPersona {
			return true
		}
	}

	return false
}

// undefinedRoles is one finding per role a persona names that the auth's role file does
// not define, and one when the roles cannot be read at all while a persona names one. A
// cast naming no role reads no role file.
func undefinedRoles(a *app.App, dir, castRel string, cast *persona.Cast) []string {
	named := false
	for i := range cast.Personas {
		named = named || len(cast.Personas[i].Roles) > 0
	}
	if !named {
		return nil
	}
	rolesRel := path.Join(dir, app.RolesFileName)
	defined, err := definedRoles(a.Abs(rolesRel))
	if err != nil {
		return []string{fmt.Sprintf("%s: the personas' roles cannot be checked against %s: %s", castRel, rolesRel, err.Error())}
	}
	var details []string
	for i := range cast.Personas {
		p := &cast.Personas[i]
		for _, role := range p.Roles {
			if !defined[role] {
				details = append(details, fmt.Sprintf("%s: persona %s names role %s, which %s does not define", castRel, p.Login, role, rolesRel))
			}
		}
	}

	return details
}

// definedRoles is the set of role names a role file defines, in either scope.
func definedRoles(abs string) (map[string]bool, error) {
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, errors.Cause(err)
	}
	cfg, err := access.RoleFile(data).Parse()
	if err != nil {
		return nil, errors.New(refusalWords(err))
	}
	defined := map[string]bool{}
	for _, r := range cfg.Roles.Global {
		defined[string(r.Name)] = true
	}
	for _, r := range cfg.Roles.Domain {
		defined[string(r.Name)] = true
	}

	return defined, nil
}
