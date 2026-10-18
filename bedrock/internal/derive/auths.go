// auths.go reads the auths: every Auth the application constructs, with the sign-in
// methods registered on it, and for each directory method the registration's variables,
// found by the naming convention impulse writes and bedrock reads, and the callback the
// generated router registers for it under the outlet the auth is bound to.

package derive

import (
	"net/http"
	"path"
	"sort"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/impulse/app"
)

// The settings a directory method reads from the environment, by the field of the
// method's settings struct as impulse renders it (the suffix of the variable under the
// convention APP_<AUTH>_<REGISTRATION>_<FIELD>).
const (
	FieldTenantID     = "TenantID"
	FieldTenants      = "Tenants"
	FieldHostedDomain = "HostedDomain"
	FieldWorkspaces   = "Workspaces"
	FieldClientID     = "ClientID"
	FieldClientSecret = "ClientSecret"
	FieldRedirectURL  = "RedirectURL"
	FieldGroupPrefix  = "GroupPrefix"
	FieldGroupLookup  = "GroupLookup"
)

// registrationField is one setting of a directory method under the convention: the
// field, the variable's suffix, the role the variable takes, which method kinds read it,
// whether the registration must declare it, and whether it is a secret.
type registrationField struct {
	Field    string
	Suffix   string
	Role     Role
	Kinds    []string
	Required bool
	Secret   bool
	// DirectoryAuthority marks a setting only a registration under the directory's
	// authority for role membership reads.
	DirectoryAuthority bool
}

// registrationFields lists the settings in the order the stack carries them: the
// organization slot first (one of two shapes per kind, or none for a registration that
// admits any organization), then the client credentials and the callback every
// directory method reads, then the role groups of a Google registration under the
// directory's authority.
var registrationFields = []registrationField{
	{Field: FieldTenantID, Suffix: "TENANT_ID", Role: RoleTenantID, Kinds: []string{app.MethodAzure}},
	{Field: FieldTenants, Suffix: "TENANTS", Role: RoleTenants, Kinds: []string{app.MethodAzure}},
	{Field: FieldHostedDomain, Suffix: "HOSTED_DOMAIN", Role: RoleHostedDomain, Kinds: []string{app.MethodGoogle}},
	{Field: FieldWorkspaces, Suffix: "WORKSPACES", Role: RoleWorkspaces, Kinds: []string{app.MethodGoogle}},
	{Field: FieldClientID, Suffix: "CLIENT_ID", Role: RoleClientID, Kinds: []string{app.MethodAzure, app.MethodGoogle}, Required: true},
	{Field: FieldClientSecret, Suffix: "CLIENT_SECRET", Role: RoleClientSecret, Kinds: []string{app.MethodAzure, app.MethodGoogle}, Required: true, Secret: true},
	{Field: FieldRedirectURL, Suffix: "REDIRECT_URL", Role: RoleRedirectURL, Kinds: []string{app.MethodAzure, app.MethodGoogle}, Required: true},
	{Field: FieldGroupPrefix, Suffix: "GROUP_PREFIX", Role: RoleGroupPrefix, Kinds: []string{app.MethodGoogle}, Required: true, DirectoryAuthority: true},
	{Field: FieldGroupLookup, Suffix: "GROUP_LOOKUP", Role: RoleGroupLookup, Kinds: []string{app.MethodGoogle}, DirectoryAuthority: true},
}

// slotPairs are the two shapes of a kind's organization slot, which a registration
// declares one of at most: one tenant or listed tenants, one Workspace or listed
// Workspaces.
var slotPairs = [][2]string{{FieldTenantID, FieldTenants}, {FieldHostedDomain, FieldWorkspaces}}

// reads reports whether a method of the kind, under the authority, reads the setting.
func (f *registrationField) reads(kind, authority string) bool {
	if f.DirectoryAuthority && authority != app.AuthorityDirectory {
		return false
	}
	for _, k := range f.Kinds {
		if k == kind {
			return true
		}
	}

	return false
}

// RegistrationVariable is the environment variable of a directory method's setting
// under the convention: APP_<AUTH>_<REGISTRATION>_<SUFFIX>, the auth's name and the
// registration's name in upper case with underscores for hyphens.
func RegistrationVariable(auth, registration, suffix string) string {
	return appPrefix + upperSnake(auth) + "_" + upperSnake(registration) + "_" + suffix
}

// AdministratorVariable is the environment variable naming an auth's first
// administrator under the convention: APP_<AUTH>_ADMINISTRATOR.
func AdministratorVariable(auth string) string {
	return appPrefix + upperSnake(auth) + "_ADMINISTRATOR"
}

// upperSnake is a name in upper case with underscores for its hyphens.
func upperSnake(name string) string {
	return strings.ToUpper(strings.ReplaceAll(name, "-", "_"))
}

// auths settles the auths: one per auth package, with the methods registered on the
// Auth the package constructs, each directory method's variables and callback among them.
func (m *Model) auths(a *app.App, cfg *config) error {
	if len(a.AuthPackages) == 0 {
		return errors.New("the application constructs no auth")
	}
	profile := a.Profile()
	for i := range a.AuthPackages {
		pkg := &a.AuthPackages[i]
		auth, err := m.auth(a, cfg, &profile, pkg)
		if err != nil {
			return err
		}
		m.Auths = append(m.Auths, auth)
	}
	sort.Slice(m.Auths, func(i, j int) bool {
		return m.Auths[i].Name < m.Auths[j].Name
	})

	return nil
}

// auth reads one auth package: the Auth it constructs and the methods on it.
func (m *Model) auth(a *app.App, cfg *config, profile *app.Profile, pkg *app.AuthPackage) (Auth, error) {
	auth := Auth{Name: pkg.Name, Dir: pkg.Dir}
	var constructions []*app.Auth
	for j := range a.Auths {
		if path.Dir(a.Auths[j].File) == pkg.Dir {
			constructions = append(constructions, &a.Auths[j])
		}
	}
	if len(constructions) != 1 {
		return Auth{}, errors.Newf("the %s auth package (%s) constructs %d session Auths; an auth constructs one (session.NewAuth) and registers its sign-in methods on it", pkg.Name, pkg.Dir, len(constructions))
	}
	outlets := boundOutlets(profile, pkg.Path)
	for _, registration := range constructions[0].Methods {
		method := Method{Auth: pkg.Name, Name: registration.Name, Kind: registration.Kind, Authority: registration.Authority}
		if method.Directory() {
			if err := m.registrationVariables(cfg, &method); err != nil {
				return Auth{}, err
			}
			if err := method.routes(outlets); err != nil {
				return Auth{}, err
			}
		}
		auth.Methods = append(auth.Methods, method)
	}
	if err := m.administrator(&auth); err != nil {
		return Auth{}, err
	}
	if err := m.administratorPassword(&auth); err != nil {
		return Auth{}, err
	}

	return auth, nil
}

// administrator finds the auth's first-administrator variable by the convention and
// gives it its role. One tagged as a secret is refused: a sign-in name is a setting the
// placement names per environment, not a value Secret Manager holds.
func (m *Model) administrator(auth *Auth) error {
	v := m.variableNamed(AdministratorVariable(auth.Name))
	if v == nil {
		return nil
	}
	if v.Secret {
		return errors.Newf("%s:%d: %s carries a secret tag: the %s auth's first administrator is a sign-in name the placement names per environment, a setting, not a secret; drop the tag", v.File, v.Line, v.Declaration(), auth.Name)
	}
	v.Role = RoleAdministrator
	auth.Administrator = v

	return nil
}

// AdministratorPasswordVariable is the variable an auth's first-administrator password
// goes under: APP_<AUTH>_ADMINISTRATOR_PASSWORD.
func AdministratorPasswordVariable(auth string) string {
	return AdministratorVariable(auth) + "_PASSWORD"
}

// administratorPassword finds the auth's first-administrator password by the convention
// and gives it its role. One that is not a generated secret is refused: the password is a
// value the stack makes and the migrate command seeds with, never one a person types.
func (m *Model) administratorPassword(auth *Auth) error {
	v := m.variableNamed(AdministratorPasswordVariable(auth.Name))
	if v == nil {
		return nil
	}
	if !v.Secret || !v.Generated {
		return errors.Newf("%s:%d: %s is not tagged secret:\"generated\": the %s auth's first administrator's password is a value the stack generates and the migrate command seeds with, never one a person types; tag it", v.File, v.Line, v.Declaration(), auth.Name)
	}
	v.Role = RoleAdministratorPassword
	auth.AdministratorPassword = v

	return nil
}

// boundOutlets lists the names and prefixes of the outlets bound to the auth package.
func boundOutlets(profile *app.Profile, importPath string) []app.Outlet {
	var bound []app.Outlet
	for i := range profile.Sites {
		outlets := profile.Sites[i].AllOutlets()
		for j := range outlets {
			if outlets[j].Auth != nil && outlets[j].Auth.ImportPath == importPath {
				bound = append(bound, outlets[j])
			}
		}
	}

	return bound
}

// registrationVariables finds the directory method's settings among the variables, by
// the convention, gives each its role, and refuses a registration that declares a
// required setting under no variable, both shapes of its organization slot, or its
// client secret without the secret tag.
func (m *Model) registrationVariables(cfg *config, method *Method) error {
	method.Variables = map[string]*Variable{}
	for i := range registrationFields {
		f := &registrationFields[i]
		if !f.reads(method.Kind, method.Authority) {
			continue
		}
		name := RegistrationVariable(method.Auth, method.Name, f.Suffix)
		v := m.variableNamed(name)
		if v == nil {
			if f.Required {
				return errors.Newf("the %s auth's %s registration (%s) declares no %s variable: the data level reads a %s method's settings from the environment as %s and the rest of %s (impulse add method writes them)", method.Auth, method.Name, method.Kind, name, method.Kind, name, RegistrationVariable(method.Auth, method.Name, "<FIELD>"))
			}

			continue
		}
		if f.Secret && !v.Secret {
			return errors.Newf("%s (%s) is the %s registration's client secret and carries no secret:\"true\" tag: the stack serves it from Secret Manager, never from a plain variable", v.Name, v.Declaration(), method.Name)
		}
		v.Role = f.Role
		method.Variables[f.Field] = v
	}
	for _, pair := range slotPairs {
		if method.Variables[pair[0]] != nil && method.Variables[pair[1]] != nil {
			return errors.Newf("the %s auth's %s registration declares both %s and %s: a registration admits one organization or a listed set, not both", method.Auth, method.Name, method.Variables[pair[0]].Name, method.Variables[pair[1]].Name)
		}
	}
	if prefix := method.Variable(FieldGroupPrefix); prefix != nil {
		method.GroupPrefixDefault = cfg.envTemplate[prefix.Name]
	}
	if lookup := method.Variable(FieldGroupLookup); lookup != nil {
		method.GroupLookupDefault = defaultGroupLookup
		if cfg.envTemplate[lookup.Name] != "" {
			method.GroupLookupDefault = cfg.envTemplate[lookup.Name]
		}
	}

	return nil
}

// variableNamed returns the variable by its name, or nil.
func (m *Model) variableNamed(name string) *Variable {
	for i := range m.Variables {
		if m.Variables[i].Name == name {
			return &m.Variables[i]
		}
	}

	return nil
}

// routes settles the directory method's callback, and its front-channel logout for a
// Microsoft registration: the routes the generated router registers for the
// registration under the one outlet the auth is bound to, /<prefix>/user/<name>/callback
// and /logout. A directory method has one redirect URL, so an auth with one is bound to
// one outlet exactly.
func (m *Method) routes(outlets []app.Outlet) error {
	switch len(outlets) {
	case 1:
	case 0:
		return errors.Newf("the %s auth registers the %s method (%s) and no outlet binds to it: the registration's callback is a route of the outlet the auth is bound to (generation.Auth on the outlet)", m.Auth, m.Name, m.Kind)
	default:
		names := make([]string, 0, len(outlets))
		for i := range outlets {
			names = append(names, outlets[i].Name)
		}

		return errors.Newf("the %s auth registers the %s method (%s) and %d outlets bind to it (%s): a directory registration has one callback, so its auth is bound to one outlet", m.Auth, m.Name, m.Kind, len(outlets), strings.Join(names, ", "))
	}
	m.Outlet = outlets[0].Name
	base := "/" + strings.Trim(outlets[0].Prefix, "/") + "/user/" + m.Name
	m.Callback = Route{Method: http.MethodGet, Path: base + "/callback"}
	if m.FrontChannelLogout() {
		m.Logout = Route{Method: http.MethodGet, Path: base + "/logout"}
	}

	return nil
}
