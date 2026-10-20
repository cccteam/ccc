package authpkg

import (
	"path"
	"strings"

	"github.com/cccteam/ccc/impulse/app"
	"github.com/cccteam/ccc/impulse/internal/names"
)

// view is what the Auth file's template renders from.
type view struct {
	*Spec
	// Pascal is the auth's table prefix (Staff).
	Pascal string
	// Package is the import path of the application's auth package (<module>/pkg/auth),
	// which holds what the auths share.
	Package string
	// Doc is the package comment's first sentence after "Package <name> is the <name>
	// auth:".
	Doc string
	// Directory reports an auth with a directory method: it takes a LoginURL and the
	// development environment simulates the directory.
	Directory bool
	// Methods are the methods with their rendering helpers.
	Methods []*methodView
	// Foothold is the form of the auth's first administrator the deployment seeds
	// (Foothold), FootholdCreatesUser whether the membership form also creates the user
	// row, AdministratorAuthored whether the spec names the roles, and
	// AdministratorGlobal and AdministratorEveryDomain the role lists as Go literals.
	Foothold                 string
	FootholdCreatesUser      bool
	AdministratorAuthored    bool
	AdministratorGlobal      string
	AdministratorEveryDomain string
	// Imports are the import paths the Auth file needs beyond the fixed ones.
	Imports []string
}

// methodView is what a method file's template renders from.
type methodView struct {
	*Method
	// Auth is the auth's name and AuthPascal its table prefix.
	Auth       string
	AuthPascal string
	// Field is the Handlers field holding the method (directory, entraOne).
	Field string
	// Type is the method's Go type (*azure.Method).
	Type string
	// SlotExpr is the organization slot as the registration's Config takes it.
	SlotExpr string
	// AccountsExpr is the account rule as the Config takes it.
	AccountsExpr string
	// RoleSyncExpr is the role synchronization as the Config takes it.
	RoleSyncExpr string
	// Fields are the settings the method reads.
	Fields []Field
	// Variables are the fields' environment variables, in order.
	Variables []string
	// Imports are the method file's imports beyond the standard library's, which
	// StdImports holds, rendered as their own group.
	Imports []string
	// StdImports are the standard library packages the method file imports.
	StdImports []string
	// Simulated are the names the simulated directory signs the registration in with:
	// APP_<REGISTRATION>_USERNAME and _ROLES.
	SimulatedUsername, SimulatedRoles string
}

func newView(spec *Spec) *view {
	v := &view{Spec: spec, Pascal: names.Pascal(spec.Name), Package: path.Join(spec.Module, spec.AuthDir)}
	v.Doc = spec.Doc
	if v.Doc == "" {
		v.Doc = "the people who sign in to the application"
	}
	for i := range spec.Methods {
		mv := newMethodView(spec, &spec.Methods[i])
		v.Methods = append(v.Methods, mv)
		if mv.Directory() {
			v.Directory = true
		}
	}
	v.Foothold = Foothold(spec)
	v.FootholdCreatesUser = v.Foothold == FootholdMembership && !provisions(spec)
	v.AdministratorAuthored = len(spec.AdministratorRoles.Global)+len(spec.AdministratorRoles.EveryDomain) > 0
	v.AdministratorGlobal = roleLiteral(spec.AdministratorRoles.Global)
	v.AdministratorEveryDomain = roleLiteral(spec.AdministratorRoles.EveryDomain)
	seen := map[string]bool{}
	for _, mv := range v.Methods {
		var imp string
		switch mv.Kind {
		case app.MethodAzure:
			imp = "github.com/cccteam/session/azure"
		case app.MethodGoogle:
			imp = "github.com/cccteam/session/google"
		}
		if imp != "" && !seen[imp] {
			seen[imp] = true
			v.Imports = append(v.Imports, imp)
		}
	}

	return v
}

// The forms of the first administrator the deployment seeds while the auth's users table
// holds no rows: a user named administrator with the password the stack generates, for an
// auth with a password method; the administrator role memberships under the sign-in name
// the placement names, for an auth whose directory methods leave role membership to the
// application. An auth whose directory methods all follow the directory's roles needs none.
const (
	FootholdPassword   = "password"
	FootholdMembership = "membership"
	footholdFile       = "foothold.go"
)

// Foothold is the form of the first administrator the deployment seeds for the auth, or
// empty when the directory ranks its people.
func Foothold(spec *Spec) string {
	membership := false
	for i := range spec.Methods {
		m := &spec.Methods[i]
		switch {
		case m.Kind == app.MethodPassword:
			return FootholdPassword
		case m.Directory() && m.Authority == app.AuthorityApplication:
			membership = true
		}
	}
	if membership {
		return FootholdMembership
	}

	return ""
}

// provisions reports whether any directory method creates a user at a first sign-in
// (members of the directory, open sign-up); invited users only alone means the first
// administrator's user row is created beforehand.
func provisions(spec *Spec) bool {
	for i := range spec.Methods {
		m := &spec.Methods[i]
		if m.Directory() && m.accounts() != AccountsInvited {
			return true
		}
	}

	return false
}

// roleLiteral is the roles as a Go slice literal, nil for none.
func roleLiteral(roles []string) string {
	if len(roles) == 0 {
		return "nil"
	}
	quoted := make([]string, 0, len(roles))
	for _, r := range roles {
		quoted = append(quoted, `"`+r+`"`)
	}

	return "[]accesstypes.Role{" + strings.Join(quoted, ", ") + "}"
}

func newMethodView(spec *Spec, m *Method) *methodView {
	mv := &methodView{Method: m, Auth: spec.Name, AuthPascal: names.Pascal(spec.Name), Fields: m.Fields()}
	mv.Field = lowerFirst(m.Pascal())
	for i := range mv.Fields {
		mv.Variables = append(mv.Variables, m.Variable(spec.Name, &mv.Fields[i]))
	}
	simulated := strings.ToUpper(strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		default:
			return '_'
		}
	}, m.Name))
	mv.SimulatedUsername, mv.SimulatedRoles = "APP_"+simulated+"_USERNAME", "APP_"+simulated+"_ROLES"
	settings := "settings." + m.Pascal()
	switch m.Kind {
	case app.MethodPassword:
		mv.Type = "*session.PasswordMethod"
		mv.Imports = []string{netHTTP, "github.com/cccteam/session"}
	case app.MethodAzure:
		azureView(mv, m, settings)
	case app.MethodGoogle:
		googleView(mv, m, settings)
	}
	switch m.accounts() {
	case AccountsInvited:
		mv.AccountsExpr = "signin.InvitedUsersOnly"
	case AccountsOpen:
		mv.AccountsExpr = "signin.OpenSignUp"
	default:
		mv.AccountsExpr = "signin.MembersOfDirectory"
	}
	mv.StdImports, mv.Imports = splitImports(mv.Imports)

	return mv
}

// splitImports separates the standard library's packages from the rest, so the file
// renders them as gofumpt groups them: the standard library first, then everything else.
func splitImports(imports []string) (std, other []string) {
	for _, imp := range imports {
		if first, _, _ := strings.Cut(imp, "/"); strings.Contains(first, ".") {
			other = append(other, imp)
		} else {
			std = append(std, imp)
		}
	}

	return std, other
}

// netHTTP is the import every method's handlers need.
const netHTTP = "net/http"

// azureView fills the view of a Microsoft Entra method: its slot expression, its
// role-synchronization expression and its imports.
func azureView(mv *methodView, m *Method, settings string) {
	mv.Type = "*azure.Method"
	switch m.slot() {
	case SlotOne:
		mv.SlotExpr = "azure.Tenant(" + settings + ".TenantID)"
	case SlotListed:
		mv.SlotExpr = "tenants"
	case SlotAny:
		mv.SlotExpr = "azure.AnyTenant()"
	case SlotPersonal:
		mv.SlotExpr = "azure.PersonalAccounts()"
	case SlotOnePersonal:
		mv.SlotExpr = "azure.Tenants(azure.Tenant(" + settings + ".TenantID), azure.PersonalAccounts())"
	case SlotAnyPersonal:
		mv.SlotExpr = "azure.Tenants(azure.AnyTenant(), azure.PersonalAccounts())"
	}
	if m.Authority == app.AuthorityDirectory {
		mv.RoleSyncExpr = "signin.RoleSync(accessClient.UserManager())"
	} else {
		mv.RoleSyncExpr = "signin.DisableRoleSync()"
	}
	mv.Imports = []string{netHTTP, "github.com/cccteam/access", "github.com/cccteam/session/azure", "github.com/cccteam/session/signin"}
	if m.slot() == SlotListed {
		mv.Imports = append(mv.Imports, "strings", "github.com/go-playground/errors/v5")
	}
}

// googleView fills the view of a Google Workspace method: its slot expression, its
// role-synchronization expression and its imports.
func googleView(mv *methodView, m *Method, settings string) {
	mv.Type = "*google.Method"
	workspace := "google.Workspace(" + m.Registration() + ", " + settings + ".HostedDomain)"
	switch m.slot() {
	case SlotOne:
		mv.SlotExpr = workspace
	case SlotListed:
		mv.SlotExpr = "workspaces"
	case SlotAnyWorkspace:
		mv.SlotExpr = "google.AnyWorkspace()"
	case SlotAny:
		mv.SlotExpr = "google.AnyGoogleAccount()"
	case SlotMixed:
		mv.SlotExpr = "google.Workspaces(" + workspace + ", google.AnyGoogleAccount())"
	}
	if m.Authority == app.AuthorityDirectory {
		mv.RoleSyncExpr = "google.RoleSync(accessClient.UserManager(), " + settings + ".GroupPrefix, lookup)"
	} else {
		mv.RoleSyncExpr = "google.DisableRoleSync()"
	}
	mv.Imports = []string{netHTTP, "github.com/cccteam/access", "github.com/cccteam/session/google", "github.com/cccteam/session/signin"}
	if m.slot() == SlotListed {
		mv.Imports = append(mv.Imports, "strings")
	}
	if m.slot() == SlotListed || m.Authority == app.AuthorityDirectory {
		mv.Imports = append(mv.Imports, "github.com/go-playground/errors/v5")
	}
}

func lowerFirst(s string) string {
	if s == "" {
		return ""
	}

	return strings.ToLower(s[:1]) + s[1:]
}
