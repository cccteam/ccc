package deploy_test

import (
	"reflect"
	"testing"

	"github.com/cccteam/access"
	"github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/solo/pkg/auth/staff"
	"github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/solo/pkg/router"
)

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
