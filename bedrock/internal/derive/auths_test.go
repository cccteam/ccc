package derive

import (
	"slices"
	"strings"
	"testing"

	"github.com/cccteam/ccc/impulse/app"
)

// TestRegistrationVariable spells a directory method's variables under the convention
// impulse writes and bedrock reads: APP_<AUTH>_<REGISTRATION>_<FIELD>, the names in
// upper case with underscores for hyphens.
func TestRegistrationVariable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		auth         string
		registration string
		suffix       string
		want         string
	}{
		{name: "a one-word registration", auth: "members", registration: "directory", suffix: "CLIENT_ID", want: "APP_MEMBERS_DIRECTORY_CLIENT_ID"},
		{name: "a hyphenated registration", auth: "staff", registration: "lab-a", suffix: "TENANTS", want: "APP_STAFF_LAB_A_TENANTS"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := RegistrationVariable(tt.auth, tt.registration, tt.suffix); got != tt.want {
				t.Errorf("RegistrationVariable() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestAdministratorVariable spells the variable naming an auth's first administrator:
// APP_<AUTH>_ADMINISTRATOR.
func TestAdministratorVariable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		auth string
		want string
	}{
		{name: "a one-word auth", auth: "members", want: "APP_MEMBERS_ADMINISTRATOR"},
		{name: "a hyphenated auth", auth: "field-staff", want: "APP_FIELD_STAFF_ADMINISTRATOR"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := AdministratorVariable(tt.auth); got != tt.want {
				t.Errorf("AdministratorVariable() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestAuthAdministrator finds an auth's first-administrator variable among the variables
// by the convention and gives it the administrator role; an auth declaring none has none,
// and one declaring it as a secret is refused.
func TestAuthAdministrator(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		auth      string
		variables []Variable
		want      string
		wantErr   string
	}{
		{
			name:      "the auth's variable, by the convention",
			auth:      "members",
			variables: []Variable{{Name: "APP_COOKIE_KEY", Secret: true}, {Name: "APP_MEMBERS_ADMINISTRATOR", Struct: "dataConfig", Field: "MembersAdministrator"}},
			want:      "APP_MEMBERS_ADMINISTRATOR",
		},
		{
			name:      "an auth declaring none",
			auth:      "staff",
			variables: []Variable{{Name: "APP_MEMBERS_ADMINISTRATOR", Struct: "dataConfig", Field: "MembersAdministrator"}},
		},
		{
			name:      "a sign-in name tagged as a secret is refused",
			auth:      "members",
			variables: []Variable{{Name: "APP_MEMBERS_ADMINISTRATOR", Struct: "dataConfig", Field: "MembersAdministrator", File: "pkg/config/data.go", Line: 12, Secret: true}},
			wantErr:   "pkg/config/data.go:12: dataConfig.MembersAdministrator carries a secret tag: the members auth's first administrator is a sign-in name the placement names per environment, a setting, not a secret; drop the tag",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := &Model{Variables: tt.variables}
			auth := Auth{Name: tt.auth}
			err := m.administrator(&auth)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("administrator() error = %v, want %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("administrator() error = %v", err)
			}
			switch {
			case tt.want == "" && auth.Administrator != nil:
				t.Errorf("Administrator = %s, want none", auth.Administrator.Name)
			case tt.want != "" && (auth.Administrator == nil || auth.Administrator.Name != tt.want):
				t.Errorf("Administrator = %v, want %s", auth.Administrator, tt.want)
			case tt.want != "" && auth.Administrator.Role != RoleAdministrator:
				t.Errorf("Role = %q, want %q", auth.Administrator.Role, RoleAdministrator)
			}
		})
	}
}

// TestRegistrationVariables finds a directory method's settings among the variables by
// the convention and gives each its role: the slot the registration declares, the
// client credentials and the callback, and for a Google registration under the
// directory's authority the role groups, with the development template's defaults. A
// registration declaring no variable for a required setting, both shapes of its slot, or
// its client secret without the secret tag is refused.
func TestRegistrationVariables(t *testing.T) {
	t.Parallel()

	variable := func(name string, secret bool) Variable {
		return Variable{Name: name, Struct: "dataConfig", Field: strings.ToLower(name), Secret: secret}
	}
	tests := []struct {
		name      string
		method    Method
		variables []Variable
		template  map[string]string
		// wantRoles are the roles given, by variable; wantFields the fields found in the
		// stack's order; wantPrefix and wantLookup the defaults.
		wantRoles  map[string]Role
		wantFields []string
		wantPrefix string
		wantLookup string
		wantErr    string
	}{
		{
			name:   "a Microsoft registration admitting one tenant",
			method: Method{Auth: "members", Name: "directory", Kind: app.MethodAzure, Authority: app.AuthorityApplication},
			variables: []Variable{
				variable("APP_MEMBERS_DIRECTORY_TENANT_ID", false), variable("APP_MEMBERS_DIRECTORY_CLIENT_ID", false),
				variable("APP_MEMBERS_DIRECTORY_CLIENT_SECRET", true), variable("APP_MEMBERS_DIRECTORY_REDIRECT_URL", false),
				variable("APP_COOKIE_KEY", true),
			},
			wantRoles:  map[string]Role{"APP_MEMBERS_DIRECTORY_TENANT_ID": RoleTenantID, "APP_MEMBERS_DIRECTORY_CLIENT_ID": RoleClientID, "APP_MEMBERS_DIRECTORY_CLIENT_SECRET": RoleClientSecret, "APP_MEMBERS_DIRECTORY_REDIRECT_URL": RoleRedirectURL, "APP_COOKIE_KEY": RoleNone},
			wantFields: []string{FieldTenantID, FieldClientID, FieldClientSecret, FieldRedirectURL},
		},
		{
			name:   "a Microsoft registration declaring the tenants the deployment administers",
			method: Method{Auth: "members", Name: "directory", Kind: app.MethodAzure, Authority: app.AuthorityApplication},
			variables: []Variable{
				variable("APP_MEMBERS_DIRECTORY_TENANT_ID", false), variable("APP_MEMBERS_DIRECTORY_ADMINISTERED_TENANTS", false), variable("APP_MEMBERS_DIRECTORY_CLIENT_ID", false),
				variable("APP_MEMBERS_DIRECTORY_CLIENT_SECRET", true), variable("APP_MEMBERS_DIRECTORY_REDIRECT_URL", false),
			},
			wantRoles:  map[string]Role{"APP_MEMBERS_DIRECTORY_TENANT_ID": RoleTenantID, "APP_MEMBERS_DIRECTORY_ADMINISTERED_TENANTS": RoleAdministeredTenants},
			wantFields: []string{FieldTenantID, FieldAdministeredTenants, FieldClientID, FieldClientSecret, FieldRedirectURL},
		},
		{
			name:   "a Google registration declaring the Workspaces the deployment administers",
			method: Method{Auth: "staff", Name: "directory", Kind: app.MethodGoogle, Authority: app.AuthorityApplication},
			variables: []Variable{
				variable("APP_STAFF_DIRECTORY_WORKSPACES", false), variable("APP_STAFF_DIRECTORY_ADMINISTERED_WORKSPACES", false), variable("APP_STAFF_DIRECTORY_CLIENT_ID", false),
				variable("APP_STAFF_DIRECTORY_CLIENT_SECRET", true), variable("APP_STAFF_DIRECTORY_REDIRECT_URL", false),
			},
			wantRoles:  map[string]Role{"APP_STAFF_DIRECTORY_WORKSPACES": RoleWorkspaces, "APP_STAFF_DIRECTORY_ADMINISTERED_WORKSPACES": RoleAdministeredWorkspaces},
			wantFields: []string{FieldWorkspaces, FieldAdministeredWorkspaces, FieldClientID, FieldClientSecret, FieldRedirectURL},
		},
		{
			name:   "a Microsoft registration admitting any tenant declares no slot",
			method: Method{Auth: "members", Name: "any", Kind: app.MethodAzure, Authority: app.AuthorityApplication},
			variables: []Variable{
				variable("APP_MEMBERS_ANY_CLIENT_ID", false), variable("APP_MEMBERS_ANY_CLIENT_SECRET", true), variable("APP_MEMBERS_ANY_REDIRECT_URL", false),
			},
			wantFields: []string{FieldClientID, FieldClientSecret, FieldRedirectURL},
		},
		{
			name:   "a Google registration under the directory's authority, with the template's defaults",
			method: Method{Auth: "staff", Name: "directory", Kind: app.MethodGoogle, Authority: app.AuthorityDirectory},
			variables: []Variable{
				variable("APP_STAFF_DIRECTORY_CLIENT_ID", false), variable("APP_STAFF_DIRECTORY_CLIENT_SECRET", true), variable("APP_STAFF_DIRECTORY_REDIRECT_URL", false),
				variable("APP_STAFF_DIRECTORY_HOSTED_DOMAIN", false), variable("APP_STAFF_DIRECTORY_GROUP_PREFIX", false), variable("APP_STAFF_DIRECTORY_GROUP_LOOKUP", false),
			},
			template:   map[string]string{"APP_STAFF_DIRECTORY_GROUP_PREFIX": "staff-", "APP_STAFF_DIRECTORY_GROUP_LOOKUP": "nested"},
			wantRoles:  map[string]Role{"APP_STAFF_DIRECTORY_HOSTED_DOMAIN": RoleHostedDomain, "APP_STAFF_DIRECTORY_GROUP_PREFIX": RoleGroupPrefix, "APP_STAFF_DIRECTORY_GROUP_LOOKUP": RoleGroupLookup},
			wantFields: []string{FieldHostedDomain, FieldClientID, FieldClientSecret, FieldRedirectURL, FieldGroupPrefix, FieldGroupLookup},
			wantPrefix: "staff-",
			wantLookup: "nested",
		},
		{
			name:   "a Google registration under the application's authority reads no role groups",
			method: Method{Auth: "staff", Name: "directory", Kind: app.MethodGoogle, Authority: app.AuthorityApplication},
			variables: []Variable{
				variable("APP_STAFF_DIRECTORY_CLIENT_ID", false), variable("APP_STAFF_DIRECTORY_CLIENT_SECRET", true), variable("APP_STAFF_DIRECTORY_REDIRECT_URL", false),
				variable("APP_STAFF_DIRECTORY_WORKSPACES", false), variable("APP_STAFF_DIRECTORY_GROUP_PREFIX", false),
			},
			wantRoles:  map[string]Role{"APP_STAFF_DIRECTORY_WORKSPACES": RoleWorkspaces, "APP_STAFF_DIRECTORY_GROUP_PREFIX": RoleNone},
			wantFields: []string{FieldWorkspaces, FieldClientID, FieldClientSecret, FieldRedirectURL},
		},
		{
			name:   "the group lookup left to the library's default",
			method: Method{Auth: "staff", Name: "directory", Kind: app.MethodGoogle, Authority: app.AuthorityDirectory},
			variables: []Variable{
				variable("APP_STAFF_DIRECTORY_CLIENT_ID", false), variable("APP_STAFF_DIRECTORY_CLIENT_SECRET", true), variable("APP_STAFF_DIRECTORY_REDIRECT_URL", false),
				variable("APP_STAFF_DIRECTORY_GROUP_PREFIX", false), variable("APP_STAFF_DIRECTORY_GROUP_LOOKUP", false),
			},
			wantFields: []string{FieldClientID, FieldClientSecret, FieldRedirectURL, FieldGroupPrefix, FieldGroupLookup},
			wantLookup: "direct",
		},
		{
			name:      "a required setting under no variable",
			method:    Method{Auth: "members", Name: "directory", Kind: app.MethodAzure, Authority: app.AuthorityApplication},
			variables: []Variable{variable("APP_MEMBERS_DIRECTORY_CLIENT_ID", false), variable("APP_MEMBERS_DIRECTORY_REDIRECT_URL", false)},
			wantErr:   "the members auth's directory registration (azure) declares no APP_MEMBERS_DIRECTORY_CLIENT_SECRET variable: the data level reads a azure method's settings from the environment as APP_MEMBERS_DIRECTORY_CLIENT_SECRET and the rest of APP_MEMBERS_DIRECTORY_<FIELD>",
		},
		{
			name:   "both shapes of the slot",
			method: Method{Auth: "members", Name: "directory", Kind: app.MethodAzure, Authority: app.AuthorityApplication},
			variables: []Variable{
				variable("APP_MEMBERS_DIRECTORY_TENANT_ID", false), variable("APP_MEMBERS_DIRECTORY_TENANTS", false), variable("APP_MEMBERS_DIRECTORY_CLIENT_ID", false),
				variable("APP_MEMBERS_DIRECTORY_CLIENT_SECRET", true), variable("APP_MEMBERS_DIRECTORY_REDIRECT_URL", false),
			},
			wantErr: "declares both APP_MEMBERS_DIRECTORY_TENANT_ID and APP_MEMBERS_DIRECTORY_TENANTS: a registration admits one organization or a listed set, not both",
		},
		{
			name:   "the client secret without the secret tag",
			method: Method{Auth: "members", Name: "directory", Kind: app.MethodAzure, Authority: app.AuthorityApplication},
			variables: []Variable{
				variable("APP_MEMBERS_DIRECTORY_CLIENT_ID", false), variable("APP_MEMBERS_DIRECTORY_CLIENT_SECRET", false), variable("APP_MEMBERS_DIRECTORY_REDIRECT_URL", false),
			},
			wantErr: `APP_MEMBERS_DIRECTORY_CLIENT_SECRET (dataConfig.app_members_directory_client_secret) is the directory registration's client secret and carries no secret:"true" tag`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m := &Model{Variables: tt.variables}
			method := tt.method
			err := m.registrationVariables(&config{envTemplate: tt.template}, &method)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("registrationVariables() error = %v, wantErr %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("registrationVariables() error = %v", err)
			}
			for name, want := range tt.wantRoles {
				if got := m.variableNamed(name).Role; got != want {
					t.Errorf("%s's role = %q, want %q", name, got, want)
				}
			}
			var fields []string
			for _, f := range method.Fields() {
				fields = append(fields, f.Field)
				if f.Variable != method.Variable(f.Field) {
					t.Errorf("Fields() carries %s with %v, Variable() answers %v", f.Field, f.Variable, method.Variable(f.Field))
				}
			}
			if strings.Join(fields, ",") != strings.Join(tt.wantFields, ",") {
				t.Errorf("Fields() = %v, want %v", fields, tt.wantFields)
			}
			if method.GroupPrefixDefault != tt.wantPrefix || method.GroupLookupDefault != tt.wantLookup {
				t.Errorf("defaults = %q, %q; want %q, %q", method.GroupPrefixDefault, method.GroupLookupDefault, tt.wantPrefix, tt.wantLookup)
			}
		})
	}
}

// TestTenantSlot holds a Microsoft registration to a read tenant shape and its settings
// to that shape: a tenant id setting goes with one tenant, a tenants setting with listed
// tenants, a shape the scan did not read is refused, and either setting beside another shape is refused naming the registration's file and
// line. A shape the scan did not read is held to nothing, and so is a shape declaring
// neither setting.
func TestTenantSlot(t *testing.T) {
	t.Parallel()

	setting := func(field, name string) map[string]*Variable {
		return map[string]*Variable{field: {Name: name}, FieldClientID: {Name: "APP_MEMBERS_DIRECTORY_CLIENT_ID"}}
	}
	registration := &app.Method{File: "pkg/auth/members/directory.go", Line: 58, Kind: app.MethodAzure, Name: "directory"}
	tests := []struct {
		name      string
		tenants   string
		personal  bool
		variables map[string]*Variable
		wantErr   string
	}{
		{name: "one tenant with its tenant id", tenants: app.TenantsOne, variables: setting(FieldTenantID, "APP_MEMBERS_DIRECTORY_TENANT_ID")},
		{name: "one tenant and personal accounts with the tenant id", tenants: app.TenantsOne, personal: true, variables: setting(FieldTenantID, "APP_MEMBERS_DIRECTORY_TENANT_ID")},
		{name: "listed tenants with their list", tenants: app.TenantsListed, variables: setting(FieldTenants, "APP_MEMBERS_DIRECTORY_TENANTS")},
		{name: "any tenant declares neither", tenants: app.TenantsAny, variables: setting(FieldClientID, "APP_MEMBERS_DIRECTORY_CLIENT_ID")},
		{name: "one tenant written in the code declares neither", tenants: app.TenantsOne, variables: setting(FieldClientID, "APP_MEMBERS_DIRECTORY_CLIENT_ID")},
		{
			name:      "a shape the scan did not read is refused",
			variables: setting(FieldTenants, "APP_MEMBERS_DIRECTORY_TENANTS"),
			wantErr:   "pkg/auth/members/directory.go:58: the members auth's directory registration builds its tenants in a way bedrock does not read: build them in the slot function impulse add method writes (tenants, err := directorySlot(settings)), from azure.Tenant, azure.NamedTenant, azure.AnyTenant, azure.PersonalAccounts and azure.Tenants, and name that function's result in the configuration's Tenants field",
		},
		{
			name:      "any tenant with a tenant id",
			tenants:   app.TenantsAny,
			variables: setting(FieldTenantID, "APP_MEMBERS_DIRECTORY_TENANT_ID"),
			wantErr:   "pkg/auth/members/directory.go:58: the members auth's directory registration admits any tenant and declares APP_MEMBERS_DIRECTORY_TENANT_ID: a tenant id setting names the one tenant a registration admits (azure.Tenant); admit the tenant it names, or drop the setting",
		},
		{
			name:      "personal accounts with a tenant id",
			tenants:   app.TenantsPersonal,
			personal:  true,
			variables: setting(FieldTenantID, "APP_MEMBERS_DIRECTORY_TENANT_ID"),
			wantErr:   "the members auth's directory registration admits Microsoft personal accounts and declares APP_MEMBERS_DIRECTORY_TENANT_ID",
		},
		{
			name:      "listed tenants with a tenant id",
			tenants:   app.TenantsListed,
			variables: setting(FieldTenantID, "APP_MEMBERS_DIRECTORY_TENANT_ID"),
			wantErr:   "admits listed tenants and declares APP_MEMBERS_DIRECTORY_TENANT_ID",
		},
		{
			name:      "one tenant with a tenants list",
			tenants:   app.TenantsOne,
			variables: setting(FieldTenants, "APP_MEMBERS_DIRECTORY_TENANTS"),
			wantErr:   "pkg/auth/members/directory.go:58: the members auth's directory registration admits one tenant and declares APP_MEMBERS_DIRECTORY_TENANTS: a tenants setting lists the tenants a registration admits (azure.Tenants of named tenants); list the tenants it names, or drop the setting",
		},
		{
			name:      "any tenant and personal accounts with a tenants list",
			tenants:   app.TenantsAny,
			personal:  true,
			variables: setting(FieldTenants, "APP_MEMBERS_DIRECTORY_TENANTS"),
			wantErr:   "admits any tenant and Microsoft personal accounts and declares APP_MEMBERS_DIRECTORY_TENANTS",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			method := Method{Auth: "members", Name: "directory", Kind: app.MethodAzure, Authority: app.AuthorityApplication, Tenants: tt.tenants, PersonalAccounts: tt.personal, Variables: tt.variables}
			err := method.tenantSlot(registration)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("tenantSlot() error = %v, wantErr %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("tenantSlot() error = %v", err)
			}
		})
	}
}

// TestMethodRoutes settles a directory method's routes from the outlet its auth is bound
// to: the callback under the outlet's prefix, and the front-channel logout for a
// Microsoft registration. An auth bound to no outlet or to two is refused, since a
// registration has one callback.
func TestMethodRoutes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		method       Method
		outlets      []boundOutlet
		wantOutlet   string
		wantCallback string
		wantLogout   string
		wantErr      string
	}{
		{
			name:         "a Google registration on the default outlet",
			method:       Method{Auth: "staff", Name: "directory", Kind: app.MethodGoogle},
			outlets:      []boundOutlet{{site: "harbor", outlet: app.Outlet{Name: "default", Prefix: "api"}}},
			wantOutlet:   "default",
			wantCallback: "/api/user/directory/callback",
		},
		{
			name:         "a Microsoft registration on a second outlet, with its front-channel logout",
			method:       Method{Auth: "members", Name: "directory", Kind: app.MethodAzure},
			outlets:      []boundOutlet{{site: "harbor", outlet: app.Outlet{Name: "portal", Prefix: "portal/api"}}},
			wantOutlet:   "portal",
			wantCallback: "/portal/api/user/directory/callback",
			wantLogout:   "/portal/api/user/directory/logout",
		},
		{
			name:    "no outlet bound",
			method:  Method{Auth: "members", Name: "directory", Kind: app.MethodAzure},
			wantErr: "the members auth registers the directory method (azure) and no outlet binds to it",
		},
		{
			name:    "two outlets bound",
			method:  Method{Auth: "members", Name: "directory", Kind: app.MethodAzure},
			outlets: []boundOutlet{{site: "harbor", outlet: app.Outlet{Name: "default", Prefix: "api"}}, {site: "harbor", outlet: app.Outlet{Name: "portal", Prefix: "portal/api"}}},
			wantErr: "the members auth registers the directory method (azure) and 2 outlets bind to it (default, portal): a directory registration has one callback",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			method := tt.method
			err := method.routes(tt.outlets)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("routes() error = %v, wantErr %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("routes() error = %v", err)
			}
			if method.Outlet != tt.wantOutlet || method.Callback != (Route{Method: "GET", Path: tt.wantCallback}) {
				t.Errorf("outlet %q, callback %+v; want %q, GET %s", method.Outlet, method.Callback, tt.wantOutlet, tt.wantCallback)
			}
			wantLogout := Route{}
			if tt.wantLogout != "" {
				wantLogout = Route{Method: "GET", Path: tt.wantLogout}
			}
			if method.Logout != wantLogout {
				t.Errorf("logout = %+v, want %+v", method.Logout, wantLogout)
			}
		})
	}
}

// TestSignInRoutes lists one outlet's sign-in routes from its declaration: the password
// login for a password method, the pending sign-in's confirmation and cancel for every
// outlet that binds an auth, none for one that binds none, and a prefix of several
// segments or none kept as the router mounts it.
func TestSignInRoutes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		outlet app.Outlet
		want   []string
	}{
		{
			name:   "a password method on the default outlet",
			outlet: app.Outlet{Name: "default", Prefix: "api", Auth: &app.OutletAuth{ImportPath: "example.com/beacon/pkg/auth/keepers", Methods: []app.AuthMethodArg{{Kind: app.MethodPassword, Name: "password"}}}},
			want:   []string{"POST /api/user/password/login: the default outlet's password login", "POST /api/user/pending/confirm: the default outlet's pending sign-in confirmation", "POST /api/user/pending/cancel: the default outlet's pending sign-in cancel"},
		},
		{
			name:   "a directory method alone leaves the pending routes",
			outlet: app.Outlet{Name: "portal", Prefix: "portal/api", Auth: &app.OutletAuth{ImportPath: "example.com/harbor/pkg/auth/members", Methods: []app.AuthMethodArg{{Kind: app.MethodAzure, Name: "directory"}}}},
			want:   []string{"POST /portal/api/user/pending/confirm: the portal outlet's pending sign-in confirmation", "POST /portal/api/user/pending/cancel: the portal outlet's pending sign-in cancel"},
		},
		{
			name:   "a password method beside a directory method, under no prefix",
			outlet: app.Outlet{Name: "default", Auth: &app.OutletAuth{ImportPath: "example.com/lantern/pkg/auth/staff", Methods: []app.AuthMethodArg{{Kind: app.MethodGoogle, Name: "directory"}, {Kind: app.MethodPassword, Name: "password"}}}},
			want:   []string{"POST /user/password/login: the default outlet's password login", "POST /user/pending/confirm: the default outlet's pending sign-in confirmation", "POST /user/pending/cancel: the default outlet's pending sign-in cancel"},
		},
		{name: "an API-key outlet", outlet: app.Outlet{Name: "machines", Prefix: "machines", APIKey: true}},
		{name: "an outlet declaring no auth", outlet: app.Outlet{Name: "default", Prefix: "api", ServesSessions: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var got []string
			for _, r := range signInRoutes("site", &tt.outlet) {
				if r.Site != "site" || r.Outlet != tt.outlet.Name {
					t.Errorf("route %s names site %q, outlet %q", r.Path, r.Site, r.Outlet)
				}
				got = append(got, signInLine(&r))
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("signInRoutes() = %v, want %v", got, tt.want)
			}
		})
	}
}
