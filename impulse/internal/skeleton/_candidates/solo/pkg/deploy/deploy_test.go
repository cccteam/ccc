package deploy_test

import (
	"reflect"
	"testing"

	"github.com/cccteam/access"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/solo/pkg/auth/staff"
	"github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/solo/pkg/router"
)

// TestAdministratorRoles_areInTheRoleFile holds the foothold to the role file: every role
// the first administrator is seeded with is one the auth's file defines, at the scope the
// membership is held in, so the seed names nothing the engine would refuse.
func TestAdministratorRoles_areInTheRoleFile(t *testing.T) {
	t.Parallel()

	staffGlobal, _ := staff.AdministratorRoles()
	tests := []struct {
		name  string
		roles access.RoleFile
		// global and domain are the roles seeded in the global partition and in every
		// domain.
		global, domain []accesstypes.Role
	}{
		{name: "staff", roles: staff.Roles(), global: staffGlobal},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			parsed, err := tt.roles.Parse()
			if err != nil {
				t.Fatalf("access.RoleFile.Parse() error = %v", err)
			}
			defined := func(roles []*access.Role) map[accesstypes.Role]bool {
				names := make(map[accesstypes.Role]bool, len(roles))
				for _, r := range roles {
					names[r.Name] = true
				}

				return names
			}
			global, domain := defined(parsed.Roles.Global), defined(parsed.Roles.Domain)
			for _, r := range tt.global {
				if !global[r] {
					t.Errorf("%s is seeded in the global partition and the role file defines no global role by that name", r)
				}
			}
			for _, r := range tt.domain {
				if !domain[r] {
					t.Errorf("%s is seeded in every domain and the role file defines no domain role by that name", r)
				}
			}
		})
	}
}

// TestRoles_validateAgainstTheCollection runs each auth's embedded role file through the
// validation access.New performs before the engine opens (the grammar, every condition
// against the generated collection, each grant at its role's scope) and pins the warnings
// the deploy would print. Every expected set is empty: a warning here is accepted by
// adding its typed value (access.GrantWarning, access.ConcealingKeyWarning) to the row's
// wantWarnings, so the acceptance is recorded in code, or removed by fixing the grant it
// names. A new auth adds a row.
func TestRoles_validateAgainstTheCollection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		roles        access.RoleFile
		wantWarnings []access.Warning
	}{
		{name: "staff", roles: staff.Roles()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			roles, err := tt.roles.Parse()
			if err != nil {
				t.Fatalf("access.RoleFile.Parse() error = %v", err)
			}

			warnings, err := access.ValidateRoles(router.Collection(), roles)
			if err != nil {
				t.Fatalf("access.ValidateRoles() error = %v", err)
			}
			if len(warnings) == 0 && len(tt.wantWarnings) == 0 {
				return
			}
			if !reflect.DeepEqual(tt.wantWarnings, warnings) {
				t.Errorf("access.ValidateRoles() warnings = %d, want %d:", len(warnings), len(tt.wantWarnings))
				for _, w := range warnings {
					t.Logf("got:  %s", w)
				}
				for _, w := range tt.wantWarnings {
					t.Logf("want: %s", w)
				}
			}
		})
	}
}
