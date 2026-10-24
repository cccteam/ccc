package recipe

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/impulse/app"
	"github.com/cccteam/ccc/impulse/internal/check"
	"github.com/cccteam/ccc/impulse/internal/transition"
)

// SyncsRoles chains .SyncsRoles() on the declaration of every directory method whose
// registration writes the person's roles from the directory (RoleSync) and whose
// declaration on an outlet's Auth option lacks it. impulse add auth and add method wrote
// the method file's RoleSync without the flag, so the generated user-management surface
// showed such a person's roles as seats an administrator may remove, which the directory
// put back at the next sign-in; the two now read the same rule. The flag is chained only
// where the detector finds a declaration without it, so the recipe is safe to run twice,
// and the walk's regeneration carries it into the users surface.
type SyncsRoles struct{}

// Name is the recipe's short name.
func (SyncsRoles) Name() string { return "syncs-roles" }

// Meaning says what the change means in this framework.
func (SyncsRoles) Meaning() string {
	return "A directory method whose registration writes the person's roles at every sign-in (RoleSync: the application roles of a Microsoft Entra token, or the Google Groups the group prefix names) declares .SyncsRoles() on every outlet's Auth option, so the generated user-management surface shows such a person's roles as the directory's and never writes them; a seat removed by hand there came back at the next sign-in. The method file and the declaration read the same rule, and impulse check (auths-wired) holds them to it."
}

// Detect names each outlet declaration of a role-syncing method that lacks the flag.
func (SyncsRoles) Detect(_ context.Context, a *app.App) ([]string, error) {
	undeclared := check.RoleSyncUndeclaredMethods(a)
	if len(undeclared) == 0 {
		return nil, nil
	}
	found := make([]string, 0, len(undeclared))
	for i := range undeclared {
		u := &undeclared[i]
		found = append(found, fmt.Sprintf("%s: outlet %s declares the %s method %s on the %s auth without .SyncsRoles(), and %s:%d registers it with RoleSync", u.Outlet.Pos, u.Outlet.Name, u.Method.Kind, u.Method.Name, u.Package.Name, u.Method.File, u.Method.Line))
	}

	return found, nil
}

// Apply chains the flag on every declaration the detector names, in every generator
// program, one edit per method.
func (SyncsRoles) Apply(_ context.Context, a *app.App, _ check.Execer) (*transition.Change, error) {
	ch := &transition.Change{}
	seen := map[string]bool{}
	undeclared := check.RoleSyncUndeclaredMethods(a)
	for i := range undeclared {
		u := &undeclared[i]
		key := u.Package.Path + " " + u.Method.Name
		if seen[key] {
			continue
		}
		seen[key] = true
		edited, files, err := chainSyncsRoles(a, u.Package.Path, u.Method.Name)
		if err != nil {
			return nil, err
		}
		if edited == 0 {
			continue
		}
		ch.Did = append(ch.Did, fmt.Sprintf("%s: .SyncsRoles() chained on %s on %d outlet Auth option(s) bound to the %s auth, since %s:%d registers the method with RoleSync, so the regenerated users surface shows these people's roles as the directory's and never writes them", strings.Join(files, ", "), app.MethodDeclaration(u.Method.Kind, u.Method.Name, ""), edited, u.Package.Name, u.Method.File, u.Method.Line))
	}

	return ch, nil
}

// chainSyncsRoles chains .SyncsRoles() on the method's declaration in every generator
// program that declares it on an Auth option naming the package, keeping each file's
// mode; it reports how many declarations were edited and the files edited.
func chainSyncsRoles(a *app.App, importPath, name string) (edited int, files []string, err error) {
	for _, g := range a.SiteGenerators() {
		src, mode, err := readProgram(a, g.File)
		if err != nil {
			return 0, nil, err
		}
		out, n, err := app.ChainMethodFlag(g.File, src, importPath, name, "SyncsRoles")
		if err != nil {
			return 0, nil, err
		}
		if n == 0 {
			continue
		}
		if err := os.WriteFile(a.Abs(g.File), out, mode); err != nil {
			return 0, nil, errors.Wrap(err, "os.WriteFile()")
		}
		edited += n
		files = append(files, g.File)
	}

	return edited, files, nil
}

// readProgram reads a root-relative generator program with its mode, so the edit keeps
// it.
func readProgram(a *app.App, rel string) (data []byte, mode os.FileMode, err error) {
	info, err := os.Stat(a.Abs(rel))
	if err != nil {
		return nil, 0, errors.Wrap(err, "os.Stat()")
	}
	data, err = os.ReadFile(a.Abs(rel))
	if err != nil {
		return nil, 0, errors.Wrap(err, "os.ReadFile()")
	}

	return data, info.Mode().Perm(), nil
}
