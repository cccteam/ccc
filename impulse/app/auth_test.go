package app

import (
	"strings"
	"testing"
)

// registrationFile renders an auth package's file constructing one Auth and registering
// a method on it: the statements given are the registering function's body after the
// construction, the last of them the registration call.
func registrationFile(statements string, decls ...string) string {
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
` + strings.Join(decls, "\n")
}

// TestReadTenants reads a Microsoft registration's tenant shape from the slot function
// its configuration's Tenants slot calls, as impulse add method writes one for each slot
// kind: one tenant, listed tenants parsed out of their setting, any tenant, personal
// accounts, and personal accounts beside the tenants, with the administered tenants
// declared attesting inside the function. A slot written any other way, a building block
// in the configuration itself included, leaves the shape empty, and a Google registration
// has none.
func TestReadTenants(t *testing.T) {
	t.Parallel()

	const (
		// The registering statements as impulse writes them: a fallible slot function,
		// and one that cannot fail.
		fallible = `	tenants, err := directorySlot(settings)
	if err != nil {
		return
	}
	_, _ = azure.SignIn(authSession, "directory", nil, &azure.Config{Tenants: tenants, RoleSync: signin.DisableRoleSync()})`
		infallible = `	tenants := directorySlot(settings)
	_, _ = azure.SignIn(authSession, "directory", nil, &azure.Config{Tenants: tenants, RoleSync: signin.DisableRoleSync()})`
		// The administered tenants' parser every slot function but the personal one calls.
		parseAdministered = `func parseDirectoryAdministered(value string) []string {
	var ids []string
	for _, id := range strings.Split(value, ",") {
		if id = strings.ToLower(strings.TrimSpace(id)); id != "" {
			ids = append(ids, id)
		}
	}

	return ids
}`
		// The one-tenant slot function, returning the tenant alone or beside personal accounts.
		oneTenant = `func directorySlot(settings *Settings) (azure.TenantSet, error) {
	tenant := azure.Tenant(settings.Directory.TenantID)
	for _, id := range parseDirectoryAdministered(settings.Directory.AdministeredTenants) {
		if !strings.EqualFold(id, settings.Directory.TenantID) {
			return azure.TenantSet{}, errors.Newf("DirectoryRegistration: the administered tenant %q is not the tenant admitted, %s", id, settings.Directory.TenantID)
		}
		tenant = tenant.Attesting()
	}

	return RETURN, nil
}`
		// The open slot function, admitting any tenant with the administered ones attesting beside it.
		anyTenant = `func directorySlot(settings *Settings) azure.TenantSet {
	administered := parseDirectoryAdministered(settings.Directory.AdministeredTenants)
	sets := make([]azure.TenantSet, 0, len(administered)+1)
	sets = append(sets, OPEN)
	for _, id := range administered {
		sets = append(sets, azure.Tenant(id).Attesting())
	}

	return azure.Tenants(sets...)
}`
		// The listed slot function and the parser of its setting.
		listedTenants = `func directorySlot(settings *Settings) (azure.TenantSet, error) {
	return parseDirectoryTenants(settings.Directory.Tenants, parseDirectoryAdministered(settings.Directory.AdministeredTenants))
}

func parseDirectoryTenants(value string, administered []string) (azure.TenantSet, error) {
	var sets []azure.TenantSet
	for _, entry := range strings.Split(value, ",") {
		name, id, ok := strings.Cut(entry, "=")
		if !ok {
			return azure.TenantSet{}, errors.Newf("DirectoryRegistration: tenant %q is not <name>=<tenant id>", entry)
		}
		tenant := azure.NamedTenant(id, name)
		if slices.Contains(administered, strings.ToLower(id)) {
			tenant = tenant.Attesting()
		}
		sets = append(sets, tenant)
	}

	return azure.Tenants(sets...), nil
}`
	)
	tests := []struct {
		name          string
		statements    string
		decls         []string
		wantTenants   string
		wantPersonal  bool
		wantAuthority string
	}{
		{
			name:          "one tenant from its setting",
			statements:    fallible,
			decls:         []string{strings.Replace(oneTenant, "RETURN", "tenant", 1), parseAdministered},
			wantTenants:   TenantsOne,
			wantAuthority: AuthorityApplication,
		},
		{
			name:          "one tenant and personal accounts",
			statements:    fallible,
			decls:         []string{strings.Replace(oneTenant, "RETURN", "azure.Tenants(tenant, azure.PersonalAccounts())", 1), parseAdministered},
			wantTenants:   TenantsOne,
			wantPersonal:  true,
			wantAuthority: AuthorityApplication,
		},
		{
			name:          "listed tenants parsed out of their setting",
			statements:    fallible,
			decls:         []string{listedTenants, parseAdministered},
			wantTenants:   TenantsListed,
			wantAuthority: AuthorityApplication,
		},
		{
			name:          "any tenant",
			statements:    infallible,
			decls:         []string{strings.Replace(anyTenant, "OPEN", "azure.AnyTenant()", 1), parseAdministered},
			wantTenants:   TenantsAny,
			wantAuthority: AuthorityApplication,
		},
		{
			name:          "any tenant and personal accounts",
			statements:    infallible,
			decls:         []string{strings.Replace(anyTenant, "OPEN", "azure.AnyTenant(), azure.PersonalAccounts()", 1), parseAdministered},
			wantTenants:   TenantsAny,
			wantPersonal:  true,
			wantAuthority: AuthorityApplication,
		},
		{
			name: "personal accounts alone",
			statements: `	tenants := personalSlot()
	_, _ = azure.SignIn(authSession, "personal", nil, &azure.Config{Tenants: tenants, RoleSync: signin.DisableRoleSync()})`,
			decls: []string{`func personalSlot() azure.TenantSet {
	return azure.PersonalAccounts()
}`},
			wantTenants:   TenantsPersonal,
			wantPersonal:  true,
			wantAuthority: AuthorityApplication,
		},
		{
			name: "the configuration assigned to an identifier first",
			statements: `	tenants := directorySlot(settings)
	cfg := &azure.Config{Tenants: tenants, RoleSync: signin.RoleSync(nil)}
	_, _ = azure.SignIn(authSession, "directory", nil, cfg)`,
			decls:         []string{strings.Replace(anyTenant, "OPEN", "azure.AnyTenant()", 1), parseAdministered},
			wantTenants:   TenantsAny,
			wantAuthority: AuthorityDirectory,
		},
		{
			name:       "a slot function listing the tenants beside any tenant admits any",
			statements: infallible,
			decls: []string{`func directorySlot(settings *Settings) azure.TenantSet {
	return azure.Tenants(azure.NamedTenant("0b3b2a5c-1111-4ec5-9c15-0e2a2a1d4b55", "contoso"), azure.AnyTenant())
}`},
			wantTenants:   TenantsAny,
			wantAuthority: AuthorityApplication,
		},
		{
			name:          "a block in the configuration itself is not read",
			statements:    `	_, _ = azure.SignIn(authSession, "directory", nil, &azure.Config{Tenants: azure.Tenant(settings.TenantID), RoleSync: signin.DisableRoleSync()})`,
			wantAuthority: AuthorityApplication,
		},
		{
			name: "a slot assigned from the library's blocks is not read",
			statements: `	set := azure.Tenants(azure.NamedTenant("0b3b2a5c-1111-4ec5-9c15-0e2a2a1d4b55", "contoso"), azure.NamedTenant("5d7e6f80-2222-4b2b-8c3d-1f2e3d4c5b6a", "fabrikam"))
	_, _ = azure.SignIn(authSession, "directory", nil, &azure.Config{Tenants: set})`,
		},
		{
			name:       "a slot function building no block is not read",
			statements: infallible,
			decls: []string{`func directorySlot(settings *Settings) azure.TenantSet {
	return settings.Directory.Tenants
}`},
			wantAuthority: AuthorityApplication,
		},
		{
			name:       "a slot function of nothing is not read",
			statements: infallible,
			decls: []string{`func directorySlot(settings *Settings) azure.TenantSet {
	return azure.Tenants()
}`},
			wantAuthority: AuthorityApplication,
		},
		{
			name:          "a slot function the file does not declare is not read",
			statements:    infallible,
			wantAuthority: AuthorityApplication,
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

			auths, unattached, err := parseAuths("pkg/auth/members/members.go", []byte(registrationFile(tt.statements, tt.decls...)))
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
