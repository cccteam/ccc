// persona.go holds the persona method's part of the model: the cast the auth embeds,
// which the pipeline registers with the persona provider for each deployment, and the
// placement's persona block, which names the provider and the application's client ID
// root, held to each other.

package derive

import (
	"io/fs"
	"os"
	"path"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/impulse/app"
	"github.com/cccteam/ccc/impulse/persona"
)

// personaCast reads the auth's cast beside its package (persona.CastFile) the way the
// migrate command seeds it and the pipeline registers it, and refuses a missing or
// malformed one, or a persona method whose registration synchronizes roles from the
// provider: the provider asserts who a persona is, and the cast seeds what it may do.
func (m *Method) personaCast(a *app.App, pkg *app.AuthPackage, registration *app.Method) error {
	if m.Authority == app.AuthorityDirectory {
		return errors.Newf("%s:%d: the %s auth's %s registration (persona) synchronizes roles from the provider (RoleSync): the persona provider asserts who a persona is, and the cast seeds what it may do; register it with signin.DisableRoleSync()", registration.File, registration.Line, m.Auth, m.Name)
	}
	rel := path.Join(pkg.Dir, persona.CastFile)
	data, err := os.ReadFile(a.Abs(rel))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return errors.Newf("%s:%d: the %s auth's %s registration (persona) has no cast at %s: the file names the personas the pipeline registers with the provider and the migrate command seeds (impulse add method persona writes an empty one to fill in)", registration.File, registration.Line, m.Auth, m.Name, rel)
		}

		return errors.Wrapf(err, "os.ReadFile(): %s", rel)
	}
	if _, err := persona.ReadCast(data); err != nil {
		return errors.Newf("%s: %s", rel, errors.Cause(err).Error())
	}
	m.CastFile = rel

	return nil
}

// personaPlacement holds the placement's persona block and the persona methods to each
// other: an auth registering the method needs the provider named, since the method is
// registered where the stack sets the issuer and nowhere else, and a provider named needs
// an auth that registers the method, since the stack would otherwise carry an issuer
// nothing reads.
func (m *Model) personaPlacement() error {
	personas := m.Personas()
	switch {
	case len(personas) > 0 && m.Placement.Persona == nil:
		labels := make([]string, 0, len(personas))
		for _, p := range personas {
			labels = append(labels, "the "+p.Auth+" auth's "+p.Name+" registration")
		}

		return errors.Newf("%s is the persona method and placement.json names no persona provider: add persona {\"issuer\": the provider's URL, \"clientId\": the application's client ID root}, both from Redgrade's registration of the application; the method is registered where the stack sets the issuer and nowhere else", strings.Join(labels, " and "))
	case len(personas) == 0 && m.Placement.Persona != nil:
		return errors.New("placement.json names a persona provider (persona) and no auth registers the persona method (oidc.SignIn): drop the block, or add the method (impulse add method <auth> persona)")
	}

	return nil
}
