package app

import (
	"testing"
)

// registrationFile renders an auth package's file constructing one Auth and registering
// a method on it: the statements given are the registering function's body after the
// construction, the last of them the registration call.
func registrationFile(statements string) string {
	return `package members

import (
	"github.com/cccteam/session"
	"github.com/cccteam/session/azure"
	"github.com/cccteam/session/google"
	"github.com/cccteam/session/sessionstorage"
	"github.com/cccteam/session/signin"
)

func register(client any, key string, settings *Settings) {
	authSession, _ := session.NewAuth[session.NoCustomData, session.NoCustomData](sessionstorage.NewSpannerAuth(client), key)
` + statements + `
}
`
}

// TestReadTenants reads a Microsoft registration's tenant shape from the Tenants slot of
// its configuration, the way the azure package builds one: one tenant, listed tenants,
// any tenant, personal accounts, and personal accounts beside the tenants; in the call
// itself, or through the identifier the slot or the configuration was assigned to
// first. A slot the scan cannot read, such as the tenants a function parses out of a
// setting at startup, leaves the shape empty, and a Google registration has none.
func TestReadTenants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		statements    string
		wantTenants   string
		wantPersonal  bool
		wantAuthority string
	}{
		{
			name:          "one tenant from its setting",
			statements:    `	_, _ = azure.SignIn(authSession, "directory", nil, &azure.Config{Tenants: azure.Tenant(settings.TenantID), RoleSync: signin.DisableRoleSync()})`,
			wantTenants:   TenantsOne,
			wantAuthority: AuthorityApplication,
		},
		{
			name:        "one named tenant",
			statements:  `	_, _ = azure.SignIn(authSession, "directory", nil, &azure.Config{Tenants: azure.NamedTenant("0b3b2a5c-1111-4ec5-9c15-0e2a2a1d4b55", "contoso")})`,
			wantTenants: TenantsOne,
		},
		{
			name:        "listed tenants",
			statements:  `	_, _ = azure.SignIn(authSession, "directory", nil, &azure.Config{Tenants: azure.Tenants(azure.NamedTenant("0b3b2a5c-1111-4ec5-9c15-0e2a2a1d4b55", "contoso"), azure.Tenant("5d7e6f80-2222-4b2b-8c3d-1f2e3d4c5b6a"))})`,
			wantTenants: TenantsListed,
		},
		{
			name:        "any tenant",
			statements:  `	_, _ = azure.SignIn(authSession, "directory", nil, &azure.Config{Tenants: azure.AnyTenant()})`,
			wantTenants: TenantsAny,
		},
		{
			name:         "personal accounts alone",
			statements:   `	_, _ = azure.SignIn(authSession, "personal", nil, &azure.Config{Tenants: azure.PersonalAccounts()})`,
			wantTenants:  TenantsPersonal,
			wantPersonal: true,
		},
		{
			name:         "any tenant and personal accounts",
			statements:   `	_, _ = azure.SignIn(authSession, "anyone", nil, &azure.Config{Tenants: azure.Tenants(azure.AnyTenant(), azure.PersonalAccounts())})`,
			wantTenants:  TenantsAny,
			wantPersonal: true,
		},
		{
			name:         "one tenant and personal accounts",
			statements:   `	_, _ = azure.SignIn(authSession, "directory", nil, &azure.Config{Tenants: azure.Tenants(azure.Tenant(settings.TenantID), azure.PersonalAccounts())})`,
			wantTenants:  TenantsOne,
			wantPersonal: true,
		},
		{
			name:         "listed tenants and personal accounts",
			statements:   `	_, _ = azure.SignIn(authSession, "partners", nil, &azure.Config{Tenants: azure.Tenants(azure.NamedTenant("0b3b2a5c-1111-4ec5-9c15-0e2a2a1d4b55", "contoso"), azure.NamedTenant("5d7e6f80-2222-4b2b-8c3d-1f2e3d4c5b6a", "fabrikam"), azure.PersonalAccounts())})`,
			wantTenants:  TenantsListed,
			wantPersonal: true,
		},
		{
			name:        "listed tenants beside any tenant admit any",
			statements:  `	_, _ = azure.SignIn(authSession, "directory", nil, &azure.Config{Tenants: azure.Tenants(azure.NamedTenant("0b3b2a5c-1111-4ec5-9c15-0e2a2a1d4b55", "contoso"), azure.AnyTenant())})`,
			wantTenants: TenantsAny,
		},
		{
			name: "the slot assigned to an identifier first",
			statements: `	set := azure.Tenants(azure.NamedTenant("0b3b2a5c-1111-4ec5-9c15-0e2a2a1d4b55", "contoso"), azure.NamedTenant("5d7e6f80-2222-4b2b-8c3d-1f2e3d4c5b6a", "fabrikam"))
	_, _ = azure.SignIn(authSession, "directory", nil, &azure.Config{Tenants: set})`,
			wantTenants: TenantsListed,
		},
		{
			name: "the configuration assigned to an identifier first",
			statements: `	cfg := &azure.Config{Tenants: azure.AnyTenant(), RoleSync: signin.RoleSync(nil)}
	_, _ = azure.SignIn(authSession, "directory", nil, cfg)`,
			wantTenants:   TenantsAny,
			wantAuthority: AuthorityDirectory,
		},
		{
			name: "tenants parsed out of a setting at startup are not read",
			statements: `	tenants, err := parseDirectoryTenants(settings.Tenants)
	if err != nil {
		return
	}
	_, _ = azure.SignIn(authSession, "directory", nil, &azure.Config{Tenants: tenants, RoleSync: signin.DisableRoleSync()})`,
			wantAuthority: AuthorityApplication,
		},
		{
			name: "a slice spread into the slot is not read",
			statements: `	var sets []azure.TenantSet
	_, _ = azure.SignIn(authSession, "directory", nil, &azure.Config{Tenants: azure.Tenants(sets...)})`,
		},
		{
			name:       "a slot of nothing is not read",
			statements: `	_, _ = azure.SignIn(authSession, "directory", nil, &azure.Config{Tenants: azure.Tenants()})`,
		},
		{
			name:       "a configuration built elsewhere is not read",
			statements: `	_, _ = azure.SignIn(authSession, "directory", nil, directoryConfig(settings))`,
		},
		{
			name:          "a Google registration has no tenant shape",
			statements:    `	_, _ = google.SignIn(authSession, "workspace", nil, &google.Config{Workspaces: google.AnyWorkspace(), RoleSync: google.RoleSync(nil, "", nil)})`,
			wantAuthority: AuthorityDirectory,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			auths, unattached, err := parseAuths("pkg/auth/members/members.go", []byte(registrationFile(tt.statements)))
			if err != nil {
				t.Fatalf("parseAuths() error = %v", err)
			}
			if len(auths) != 1 || len(auths[0].Methods) != 1 || len(unattached) != 0 {
				t.Fatalf("parseAuths() = %d auths, %d unattached; want one auth with one method", len(auths), len(unattached))
			}
			m := auths[0].Methods[0]
			if m.Tenants != tt.wantTenants || m.PersonalAccounts != tt.wantPersonal {
				t.Errorf("Tenants = %q, PersonalAccounts = %v; want %q, %v", m.Tenants, m.PersonalAccounts, tt.wantTenants, tt.wantPersonal)
			}
			if m.Authority != tt.wantAuthority {
				t.Errorf("Authority = %q, want %q", m.Authority, tt.wantAuthority)
			}
		})
	}
}
